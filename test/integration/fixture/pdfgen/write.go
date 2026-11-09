package pdfgen

import (
	"bytes"
	"crypto/md5" //nolint:gosec // qpdf derives the deterministic /ID from MD5; no security role.
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen/zdeflate"
)

// SaveOptions selects the pikepdf save options a builder used. The zero
// value is pikepdf's save with deterministic_id=True,
// compress_streams=False and stream_decode_level=none, which every builder
// passes, and no minimum version.
type SaveOptions struct {
	// MinVersion is pikepdf's min_version, such as "1.7". Empty sets no
	// minimum: the header then shows 1.3, or 1.5 with object streams.
	MinVersion string
	// ExtensionLevel is the Adobe extension level paired with MinVersion
	// (pikepdf's min_version=("1.7", 8)). Above zero, qpdf writes an
	// /Extensions /ADBE entry in the catalog.
	ExtensionLevel int
	// DisablePreservePDFA is pikepdf's preserve_pdfa=False. pikepdf's
	// default, preserve_pdfa=True, makes qpdf write a newline before every
	// endstream keyword.
	DisablePreservePDFA bool
	// DisableFixMetadataVersion is pikepdf's fix_metadata_version=False.
	// pikepdf's default re-serializes the catalog's XMP packet through lxml
	// on save (see [PikepdfXMP]) and replaces the /Metadata stream with a
	// new one holding only /Type and /Subtype.
	DisableFixMetadataVersion bool
	// GenerateObjectStreams is pikepdf's
	// object_stream_mode=ObjectStreamMode.generate: every eligible object goes
	// into an uncompressed object stream and the file ends in an
	// uncompressed cross-reference stream.
	GenerateObjectStreams bool
	// Linearize is pikepdf's linearize=True.
	Linearize bool
	// CompressStreams is pikepdf's default compress_streams=True: a stream
	// with no /Filter is written Flate-compressed (C zlib's default level,
	// see [zdeflate]); a /FlateDecode stream is left as is. Other filters
	// return an error, since qpdf would decode and recompress them.
	CompressStreams bool
	// StaticID is pikepdf's static_id=True: the second /ID element is
	// qpdf's fixed test value instead of the deterministic digest.
	StaticID bool
	// Encryption, when set, encrypts the file with the standard security
	// handler (see [Encryption]). It requires StaticID, as qpdf refuses
	// deterministic IDs with encryption.
	Encryption *Encryption
}

// Output is a saved file.
type Output struct {
	// Bytes is the file.
	Bytes []byte
	// Size is the /Size the trailer states: the highest object number plus
	// one.
	Size int

	nums map[*indirect]int
}

// Num returns the object number obj received in the file, or 0 when obj is
// direct or was not written.
func (o *Output) Num(obj Object) int {
	ind := indOf(obj)
	if ind == nil {
		return 0
	}
	if n := o.nums[ind]; n > 0 {
		return n
	}
	return 0
}

// Save writes the document the way qpdf's QPDFWriter does for opts. Save
// may mutate the document as pikepdf does: it replaces the catalog's
// /Metadata stream unless DisableFixMetadataVersion is set.
func (d *Doc) Save(opts SaveOptions) (*Output, error) {
	w := &writer{
		doc:       d,
		opts:      opts,
		out:       &bytes.Buffer{},
		renumber:  map[*indirect]int{},
		xref:      map[int]xrefEntry{},
		ostreamOf: map[*indirect]*ostream{},
		nextObjID: 1,
		newlineBE: !opts.DisablePreservePDFA,
	}
	if opts.MinVersion != "" {
		w.setMinVersion(opts.MinVersion, opts.ExtensionLevel)
	}
	if opts.GenerateObjectStreams {
		w.generateObjectStreams()
	}
	if len(w.ostreams) > 0 {
		w.setMinVersion("1.5", 0)
	}
	w.setMinVersion("1.3", 0)
	if opts.Encryption != nil {
		if err := w.setupEncryption(); err != nil {
			return nil, err
		}
	}
	if !opts.DisableFixMetadataVersion {
		if err := d.rewriteXMP(); err != nil {
			return nil, err
		}
	}
	w.prepare()
	if opts.Linearize {
		return w.writeLinearized()
	}
	if err := w.writeStandard(); err != nil {
		return nil, err
	}
	return &Output{Bytes: w.out.Bytes(), Size: w.nextObjID, nums: w.renumber}, nil
}

type xrefEntry struct {
	typ    int
	offset int
	stream int
	index  int
}

type ostream struct {
	ind     *indirect
	members []Object
}

type writer struct {
	doc       *Doc
	opts      SaveOptions
	out       *bytes.Buffer
	renumber  map[*indirect]int
	queue     []any
	nextObjID int
	xref      map[int]xrefEntry
	ostreamOf map[*indirect]*ostream
	ostreams  []*ostream
	version   string
	extLevel  int
	newlineBE bool
	err       error

	// linearized is QPDFWriter's m->linearized: unparsing a child no
	// longer enqueues it, since the linearization parts numbered every
	// object up front.
	linearized bool

	// enc is the encryption state, nil for a plain file.
	enc *encryptState
	// dataKey is the current object's key (QPDFWriter's cur_data_key),
	// empty outside an object or when not encrypting.
	dataKey []byte
	// curObj and ivCounter seed the deterministic AES IVs.
	curObj, ivCounter int

	// maxOstreamIndex is qpdf's max_ostream_index: the largest object
	// stream's member count minus one.
	maxOstreamIndex int
}

const (
	flagStream = 1 << iota
	flagFiltered
	flagHexString
	flagCompressed
)

func parseVersion(v string) (int, int) {
	maj, minor, _ := strings.Cut(v, ".")
	a, _ := strconv.Atoi(maj)
	b, _ := strconv.Atoi(minor)
	return a, b
}

// setMinVersion is QPDFWriter::setMinimumPDFVersion.
func (w *writer) setMinVersion(v string, ext int) {
	if w.version == "" {
		w.version, w.extLevel = v, ext
		return
	}
	a1, b1 := parseVersion(v)
	a2, b2 := parseVersion(w.version)
	switch {
	case a1 > a2 || (a1 == a2 && b1 > b2):
		w.version, w.extLevel = v, ext
	case a1 == a2 && b1 == b2 && ext > w.extLevel:
		w.extLevel = ext
	}
}

// prepare is QPDFWriter::prepareFileForWrite: an indirect /Extensions, and
// an indirect /ADBE inside it, become direct.
func (w *writer) prepare() {
	ext, ok := w.doc.Root.Get("Extensions").(*Dict)
	if !ok {
		return
	}
	if ext.ind != nil {
		ext = ext.shallowCopy()
		w.doc.Root.Set("Extensions", ext)
	}
	if adbe, ok := ext.Get("ADBE").(*Dict); ok && adbe.ind != nil {
		ext.Set("ADBE", adbe.shallowCopy())
	}
}

func (x *Dict) shallowCopy() *Dict {
	c := &Dict{m: make(map[string]Object, len(x.m))}
	for k, v := range x.m {
		c.m[k] = v
	}
	return c
}

// trimmedTrailer is QPDFWriter::getTrimmedTrailer.
func (w *writer) trimmedTrailer() *Dict {
	t := w.doc.Trailer.shallowCopy()
	for _, k := range []string{"ID", "Encrypt", "Prev", "Index", "W", "Length", "Filter", "DecodeParms", "Type", "XRefStm"} {
		t.Del(k)
	}
	return t
}

// generateObjectStreams is QPDFWriter::generateObjectStreams over
// QPDF::getCompressibleObjGens: a depth-first walk from the trailer lists
// the non-stream objects, which are split evenly into streams of at most
// 100, each stream ordering its members by source object number.
func (w *writer) generateObjectStreams() {
	var eligible []Object
	visited := map[*indirect]bool{}
	stack := []Object{w.doc.Trailer}
	for len(stack) > 0 {
		o := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if ind := indOf(o); ind != nil && o != Object(w.doc.Trailer) {
			if visited[ind] {
				continue
			}
			visited[ind] = true
			if _, isStream := o.(*Stream); !isStream && !isSigWithContents(o) {
				eligible = append(eligible, o)
			}
		}
		switch x := resolve(o).(type) {
		case *Stream:
			keys := x.Keys()
			for i := len(keys) - 1; i >= 0; i-- {
				if v := x.m[keys[i]]; !isNull(v) && keys[i] != "Length" {
					stack = append(stack, v)
				}
			}
		case *Dict:
			keys := x.Keys()
			for i := len(keys) - 1; i >= 0; i-- {
				if v := x.m[keys[i]]; !isNull(v) {
					stack = append(stack, v)
				}
			}
		case *Array:
			for i := len(x.Items) - 1; i >= 0; i-- {
				stack = append(stack, x.Items[i])
			}
		}
	}
	if len(eligible) == 0 {
		return
	}
	nStreams := (len(eligible) + 99) / 100
	nPer := len(eligible) / nStreams
	if nPer*nStreams < len(eligible) {
		nPer++
	}
	var cur *ostream
	n := 0
	for _, o := range eligible {
		if cur == nil || n == nPer {
			cur = &ostream{ind: &indirect{id: w.doc.next()}}
			w.ostreams = append(w.ostreams, cur)
			n = 0
		}
		cur.members = append(cur.members, o)
		w.ostreamOf[indOf(o)] = cur
		n++
	}
	for _, os := range w.ostreams {
		sort.SliceStable(os.members, func(i, j int) bool {
			return indOf(os.members[i]).id < indOf(os.members[j]).id
		})
		if len(os.members)-1 > w.maxOstreamIndex {
			w.maxOstreamIndex = len(os.members) - 1
		}
	}
}

func isSigWithContents(o Object) bool {
	d, ok := resolve(o).(*Dict)
	if !ok {
		return false
	}
	return d.Get("Type") == Object(Name("Sig")) && d.Has("ByteRange") && d.Has("Contents")
}

// enqueue is QPDFWriter::enqueueObject.
func (w *writer) enqueue(o Object) {
	if ind := indOf(o); ind != nil {
		if w.renumber[ind] != 0 {
			return
		}
		if os := w.ostreamOf[ind]; os != nil {
			w.renumber[ind] = -1
			w.enqueueOstream(os)
			return
		}
		w.queue = append(w.queue, o)
		w.renumber[ind] = w.nextObjID
		w.nextObjID++
		return
	}
	switch x := o.(type) {
	case *Array:
		for _, it := range x.Items {
			w.enqueue(it)
		}
	case *Dict:
		for _, k := range x.Keys() {
			if v := x.m[k]; !isNull(v) {
				w.enqueue(v)
			}
		}
	}
}

func (w *writer) enqueueOstream(os *ostream) {
	if w.renumber[os.ind] != 0 {
		return
	}
	w.queue = append(w.queue, os)
	w.renumber[os.ind] = w.nextObjID
	w.nextObjID++
	for _, m := range os.members {
		w.renumber[indOf(m)] = w.nextObjID
		w.nextObjID++
	}
}

func (w *writer) writeString(s string) { w.out.WriteString(s) }

func (w *writer) writeHeader() {
	w.writeString("%PDF-" + w.version + "\n%\xbf\xf7\xa2\xfe\n")
}

func (w *writer) enqueueStandard() {
	t := w.trimmedTrailer()
	w.enqueue(t.Get("Root"))
	for _, k := range t.Keys() {
		if v := t.m[k]; !isNull(v) {
			w.enqueue(v)
		}
	}
}

func (w *writer) writeStandard() error {
	w.writeHeader()
	w.enqueueStandard()
	for i := 0; i < len(w.queue); i++ {
		w.writeQueued(w.queue[i])
	}
	if w.enc != nil {
		w.writeEncryptionDictionary()
	}
	if w.err != nil {
		return w.err
	}
	xrefOffset := w.out.Len()
	if len(w.ostreams) == 0 {
		w.writeXRefTable(xrefOffset)
	} else {
		xrefID := w.nextObjID
		w.nextObjID++
		w.writeXRefStream(xrefID, xrefOffset)
	}
	w.writeString("startxref\n" + strconv.Itoa(xrefOffset) + "\n%%EOF\n")
	return w.err
}

func (w *writer) writeQueued(q any) {
	switch x := q.(type) {
	case *ostream:
		w.writeObjectStream(x)
	case Object:
		id := w.renumber[indOf(x)]
		w.openObject(id)
		w.setDataKey(id)
		w.unparseObject(x, 0)
		w.dataKey = nil
		w.closeObject()
	}
}

func (w *writer) openObject(id int) {
	w.xref[id] = xrefEntry{typ: 1, offset: w.out.Len()}
	w.writeString(strconv.Itoa(id) + " 0 obj\n")
}

func (w *writer) closeObject() {
	w.writeString("\nendobj\n")
}

func (w *writer) unparseChild(child Object, flags int) {
	if !w.linearized {
		w.enqueue(child)
	}
	if ind := indOf(child); ind != nil {
		w.writeString(strconv.Itoa(w.renumber[ind]) + " 0 R")
		return
	}
	w.unparseObject(child, flags)
}

// unparseObject is QPDFWriter::unparseObject for one object, written in
// place: o is the object itself, not a reference to it.
func (w *writer) unparseObject(o Object, flags int) {
	childFlags := flags &^ flagStream
	switch x := o.(type) {
	case *Ref:
		w.unparseObject(x.Obj, flags)
	case *Array:
		w.writeString("[")
		for _, it := range x.Items {
			w.writeString(" ")
			w.unparseChild(it, childFlags)
		}
		w.writeString(" ]")
	case *Stream:
		w.writeStream(x)
	case *Dict:
		w.writeDict(x, flags, 0)
	case String:
		// A signature's /Contents (flagHexString) is never encrypted, as
		// qpdf's f_no_encryption has it.
		switch {
		case flags&flagHexString != 0:
			w.writeString(hexString(x))
		case w.dataKey != nil:
			w.writeString(hexString(w.encryptAES(x)))
		default:
			w.writeString(unparseString(x))
		}
	default:
		w.writeString(unparseScalar(o))
	}
}

func (w *writer) writeDict(x *Dict, flags, streamLength int) {
	childFlags := flags &^ flagStream
	d := x.shallowCopy()
	if x.ind != nil && x.ind == w.doc.Root.ind {
		w.fixExtensions(d)
	}
	if flags&flagStream != 0 {
		d.Del("Length")
		if a, ok := d.Get("DecodeParms").(*Array); ok && len(a.Items) == 0 {
			d.Del("DecodeParms")
		}
		if flags&(flagFiltered|flagCompressed) != 0 {
			d.Del("Filter")
			d.Del("DecodeParms")
		} else if d.Get("Filter") == Object(Name("Crypt")) {
			d.Del("Filter")
			d.Del("DecodeParms")
		}
	}
	isSig := d.Get("Type") == Object(Name("Sig")) && d.Has("ByteRange")
	w.writeString("<<")
	for _, k := range d.Keys() {
		v := d.m[k]
		if isNull(v) {
			continue
		}
		w.writeString(" " + normalizeName(k) + " ")
		if k == "Contents" && isSig {
			w.unparseChild(v, childFlags|flagHexString)
		} else {
			w.unparseChild(v, childFlags)
		}
	}
	if flags&flagStream != 0 {
		w.writeString(" /Length " + strconv.Itoa(streamLength))
		if flags&flagCompressed != 0 {
			w.writeString(" /Filter /FlateDecode")
		}
	}
	w.writeString(" >>")
}

// fixExtensions applies qpdf's catalog /Extensions rule to the copy d: the
// /ADBE entry must state the output version and extension level, and is
// removed when the level is zero, taking /Extensions with it when nothing
// else is left.
func (w *writer) fixExtensions(d *Dict) {
	ext, _ := d.Get("Extensions").(*Dict)
	haveADBE, haveOther := false, false
	if ext != nil {
		for _, k := range ext.Keys() {
			if k == "ADBE" {
				haveADBE = true
			} else {
				haveOther = true
			}
		}
	}
	needADBE := w.extLevel > 0
	if needADBE {
		if !haveOther && !haveADBE {
			ext = D()
			d.Set("Extensions", ext)
		}
	} else if !haveOther && haveADBE {
		d.Del("Extensions")
		ext = nil
	}
	if ext == nil {
		return
	}
	ext = ext.shallowCopy()
	d.Set("Extensions", ext)
	adbe, _ := ext.Get("ADBE").(*Dict)
	if adbe != nil && adbe.Get("BaseVersion") == Object(Name(w.version)) && adbe.Get("ExtensionLevel") == Object(Int(w.extLevel)) {
		return
	}
	if needADBE {
		ext.Set("ADBE", D("BaseVersion", Name(w.version), "ExtensionLevel", w.extLevel))
	} else {
		ext.Del("ADBE")
	}
}

func (w *writer) isRootMetadata(s *Stream) bool {
	m, ok := w.doc.Root.Get("Metadata").(*Stream)
	return ok && m == s
}

func (w *writer) writeStream(s *Stream) {
	data := s.Data
	flags := flagStream
	if w.isRootMetadata(s) {
		decoded, ok, err := decodeAll(&s.Dict, s.Data)
		if err != nil && w.err == nil {
			w.err = fmt.Errorf("decoding the catalog /Metadata stream: %w", err)
		}
		if ok {
			data = decoded
			flags |= flagFiltered
		}
	} else if w.opts.CompressStreams {
		switch f := s.Get("Filter"); f {
		case nil, Null{}:
			data = zdeflate.Compress(data, -1)
			flags |= flagCompressed
		case Name("FlateDecode"), Name("Fl"):
		default:
			if w.err == nil {
				w.err = fmt.Errorf("compressing a stream filtered with %s: %w", Unparse(f), errUnsupportedFilter)
			}
		}
	}
	if w.dataKey != nil {
		data = w.encryptAES(data)
	}
	w.writeDict(&s.Dict, flags, len(data))
	w.writeString("\nstream\n")
	w.out.Write(data)
	if w.newlineBE {
		w.writeString("\n")
	}
	w.writeString("endstream")
}

// writeObjectStream is QPDFWriter::writeObjectStream without compression.
func (w *writer) writeObjectStream(os *ostream) {
	id := w.renumber[os.ind]
	saved := w.out
	pass1 := &bytes.Buffer{}
	w.out = pass1
	offsets := make([]int, 0, len(os.members))
	first := w.renumber[indOf(os.members[0])]
	for i, m := range os.members {
		offsets = append(offsets, pass1.Len())
		w.unparseObject(m, 0)
		w.writeString("\n")
		w.xref[w.renumber[indOf(m)]] = xrefEntry{typ: 2, stream: id, index: i}
	}
	w.out = saved
	var head strings.Builder
	for i, off := range offsets {
		if i > 0 {
			head.WriteString(" ")
		}
		head.WriteString(strconv.Itoa(first+i) + " " + strconv.Itoa(off))
	}
	head.WriteString("\n")
	data := head.String() + pass1.String()
	w.openObject(id)
	w.writeString("<< /Type /ObjStm /Length " + strconv.Itoa(len(data)) +
		" /N " + strconv.Itoa(len(offsets)) + " /First " + strconv.Itoa(head.Len()) + " >>\nstream\n")
	w.writeString(data)
	if w.newlineBE {
		w.writeString("\n")
	}
	w.writeString("endstream")
	w.closeObject()
}

func (w *writer) writeXRefTable(_ int) {
	size := w.nextObjID
	w.writeString("xref\n0 " + strconv.Itoa(size) + "\n")
	for i := 0; i < size; i++ {
		if i == 0 {
			w.writeString("0000000000 65535 f \n")
			continue
		}
		w.writeString(fmt.Sprintf("%010d 00000 n \n", w.xref[i].offset))
	}
	w.writeString("trailer <<")
	w.writeTrailerBody(size)
	w.writeString(" >>\n")
}

// writeTrailerBody writes the trimmed trailer's entries and the /ID array.
func (w *writer) writeTrailerBody(size int) {
	t := w.trimmedTrailer()
	for _, k := range t.Keys() {
		v := t.m[k]
		if isNull(v) {
			continue
		}
		w.writeString(" " + normalizeName(k) + " ")
		if k == "Size" {
			w.writeString(strconv.Itoa(size))
		} else {
			w.unparseChild(v, 0)
		}
	}
	w.writeString(" /ID [")
	id1, id2 := w.idPair(w.out.Bytes())
	w.writeString(hexString(id1) + hexString(id2) + "]")
	if w.enc != nil {
		w.writeString(" /Encrypt " + strconv.Itoa(w.enc.dictID) + " 0 R")
	}
}

// staticID is qpdf's static_id value (QPDFWriter::generateID), meant for
// its test suite.
var staticID = String{0x31, 0x41, 0x59, 0x26, 0x53, 0x58, 0x97, 0x93, 0x23, 0x84, 0x62, 0x64, 0x33, 0x83, 0x27, 0x95}

// idPair is the rest of QPDFWriter::generateID: the second element is the
// static or deterministic value, and the first keeps the source trailer's
// /ID[0] when it has one, as a reopened file does.
func (w *writer) idPair(written []byte) (String, String) {
	if w.enc != nil {
		return w.enc.id1, w.enc.id2
	}
	var id2 String
	if w.opts.StaticID {
		id2 = staticID
	} else {
		id2 = w.deterministicID(written)
	}
	id1 := w.originalID1()
	if len(id1) == 0 {
		id1 = id2
	}
	return id1, id2
}

// deterministicID is QPDFWriter::generateID with deterministic_id: the MD5
// of the hex MD5 of every byte written so far, " QPDF ", and each string
// value of the document information dictionary, the seed cut at its first
// NUL as qpdf hands it to MD5 as a C string.
func (w *writer) deterministicID(written []byte) String {
	sum := md5.Sum(written) //nolint:gosec // see the import.
	seed := []byte(hex.EncodeToString(sum[:]) + " QPDF ")
	if info, ok := resolve(w.doc.Trailer.Get("Info")).(*Dict); ok {
		for _, k := range info.Keys() {
			if s, ok := resolve(info.m[k]).(String); ok {
				seed = append(seed, ' ')
				seed = append(seed, s...)
			}
		}
	}
	if i := bytes.IndexByte(seed, 0); i >= 0 {
		seed = seed[:i]
	}
	id := md5.Sum(seed) //nolint:gosec // see the import.
	return String(id[:])
}

func bytesNeeded(n int) int {
	b := 0
	for n != 0 {
		b++
		n >>= 8
	}
	return b
}

func appendBinary(dst []byte, v, n int) []byte {
	for i := n - 1; i >= 0; i-- {
		dst = append(dst, byte(v>>(8*i))) //nolint:gosec // taking one byte of v on purpose.
	}
	return dst
}

// writeXRefStream is QPDFWriter::writeXRefStream without compression.
func (w *writer) writeXRefStream(xrefID, maxOffset int) {
	size := w.nextObjID
	f1 := max(bytesNeeded(maxOffset), bytesNeeded(xrefID))
	f2 := bytesNeeded(w.maxOstreamIndex)
	w.xref[xrefID] = xrefEntry{typ: 1, offset: w.out.Len()}
	var data []byte
	for i := 0; i < size; i++ {
		e := w.xref[i]
		switch e.typ {
		case 1:
			data = append(data, 1)
			data = appendBinary(data, e.offset, f1)
			data = appendBinary(data, 0, f2)
		case 2:
			data = append(data, 2)
			data = appendBinary(data, e.stream, f1)
			data = appendBinary(data, e.index, f2)
		default:
			data = append(data, 0)
			data = appendBinary(data, 0, f1)
			data = appendBinary(data, 0, f2)
		}
	}
	w.openObject(xrefID)
	w.writeString("<< /Type /XRef /Length " + strconv.Itoa(len(data)) +
		" /W [ 1 " + strconv.Itoa(f1) + " " + strconv.Itoa(f2) + " ]")
	w.writeTrailerBody(size)
	w.writeString(" >>\nstream\n")
	w.out.Write(data)
	w.writeString("\nendstream")
	w.closeObject()
}

// normalizeName is qpdf's Name::normalize for a name held without its
// slash.
func normalizeName(n string) string {
	var b strings.Builder
	b.WriteByte('/')
	for i := 0; i < len(n); i++ {
		ch := n[i]
		switch {
		case ch == 0:
			b.WriteByte('#')
		case ch < 33 || ch > 126 || strings.IndexByte("#/(){}<>[]%", ch) >= 0:
			fmt.Fprintf(&b, "#%02x", ch)
		default:
			b.WriteByte(ch)
		}
	}
	return b.String()
}

func hexString(s String) string {
	return "<" + hex.EncodeToString(s) + ">"
}

// useHexString is QPDF_String::useHexString, which reads each byte as a
// signed char.
func useHexString(s String) bool {
	nonASCII := 0
	for _, b := range s {
		ch := int8(b) //nolint:gosec // qpdf reads the byte as a signed char on purpose.
		switch {
		case ch > 126:
			nonASCII++
		case ch >= 32:
		case ch < 0 || ch >= 24:
			nonASCII++
		case ch != '\n' && ch != '\r' && ch != '\t' && ch != '\b' && ch != '\f':
			return true
		}
	}
	return 5*nonASCII > len(s)
}

// unparseString is QPDF_String::unparse(false).
func unparseString(s String) string {
	if useHexString(s) {
		return hexString(s)
	}
	var b strings.Builder
	b.WriteByte('(')
	for _, ch := range s {
		switch ch {
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '(':
			b.WriteString(`\(`)
		case ')':
			b.WriteString(`\)`)
		case '\\':
			b.WriteString(`\\`)
		default:
			if (ch >= 32 && ch <= 126) || ch >= 160 {
				b.WriteByte(ch)
			} else {
				fmt.Fprintf(&b, "\\%03o", ch)
			}
		}
	}
	b.WriteByte(')')
	return b.String()
}

func unparseScalar(o Object) string {
	switch x := o.(type) {
	case Null:
		return "null"
	case Bool:
		if x {
			return "true"
		}
		return "false"
	case Int:
		return strconv.FormatInt(int64(x), 10)
	case Real:
		return string(x)
	case Name:
		return normalizeName(string(x))
	case String:
		return unparseString(x)
	case nil:
		return "null"
	}
	return "null"
}

// Unparse returns o as qpdf writes it inside an object, indirect references
// excepted: it is meant for checking a direct value.
func Unparse(o Object) string {
	w := &writer{out: &bytes.Buffer{}, renumber: map[*indirect]int{}, doc: New()}
	w.unparseObject(o, 0)
	return w.out.String()
}
