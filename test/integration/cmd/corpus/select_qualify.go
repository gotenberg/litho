package main

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"regexp"
	"slices"
	"strings"
	"unicode/utf16"
)

// The rules that decide whether an Office document is in scope for a
// converter corpus: a well-formed package or compound file of its format,
// with no macros, ActiveX controls or embedded objects (charts aside), no
// picture of a format litho does not decode (EMF, WMF, SVM, TIFF), and no
// audio or video; a binary document must also be unencrypted.

var (
	docxDeniedDirs    = []string{"word/embeddings/", "word/charts/", "word/diagrams/", "word/activex/"}
	officeDeniedPics  = []string{".emf", ".wmf", ".svm", ".tif", ".tiff"}
	odfDeniedPics     = []string{".wmf", ".emf", ".svm"}
	xlsxDeniedDirs    = []string{"xl/embeddings/", "xl/activex/"}
	mediaExtensions   = []string{".mp4", ".mov", ".avi", ".wmv", ".mp3", ".wav", ".m4a"}
	chartMediaType    = []byte("application/vnd.oasis.opendocument.chart")
	fileEntryPattern  = regexp.MustCompile(`<manifest:file-entry\b[^>]*>`)
	attrPattern       = regexp.MustCompile(`manifest:(full-path|media-type)="([^"]*)"`)
	embeddedType      = regexp.MustCompile(`<office:document\b[^>]*office:mimetype="([^"]*)"`)
	flatMedia         = regexp.MustCompile(`(?i)<draw:plugin\b|xlink:href="[^"]*\.(?:wmf|emf|svm|mp4|mov|avi|wmv|mp3|wav|m4a)"|draw:mime-type="(?:audio|video)/`)
	embeddingTarget   = regexp.MustCompile(`(?i)Target="[^"]*embeddings/`)
	officeQualifiesBy = map[string]func(string) bool{
		"docx": docxQualifies,
		"odt":  odtQualifies,
		"xlsx": xlsxQualifies,
		"ods":  odsQualifies,
		"csv":  func(string) bool { return true },
		"pptx": pptxQualifies,
		"odp":  odpQualifies,
		"doc":  docQualifies,
		"xls":  xlsQualifies,
		"ppt":  pptQualifies,
	}
)

// maxContentBytes bounds an XML part read to look for a text body or an
// embedding. Anti-DoS guard, not spec-mandated: a document within the
// selectors' size caps rarely inflates past it, and one that does is left out
// rather than read whole.
const maxContentBytes = 64 << 20

// maxCFBBytes bounds a compound file read whole; the selectors' own size caps
// keep a candidate far below it.
const maxCFBBytes = 64 << 20

func hasAnySuffix(s string, suffixes []string) bool {
	for _, x := range suffixes {
		if strings.HasSuffix(s, x) {
			return true
		}
	}
	return false
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, x := range prefixes {
		if strings.HasPrefix(s, x) {
			return true
		}
	}
	return false
}

// zipNames opens the package at path and returns it and its entry names,
// nil when it is not a zip package.
func zipNames(path string) (*zip.ReadCloser, []string) {
	z, err := zip.OpenReader(path)
	if err != nil {
		return nil, nil
	}
	names := make([]string, len(z.File))
	for i, f := range z.File {
		names[i] = f.Name
	}
	return z, names
}

// zipPart reads the named part of z, at most maxContentBytes, and reports
// whether it is there and read.
func zipPart(z *zip.ReadCloser, name string) ([]byte, bool) {
	for _, f := range z.File {
		if f.Name != name {
			continue
		}
		if f.UncompressedSize64 > maxContentBytes {
			return nil, false
		}
		rc, err := f.Open()
		if err != nil {
			return nil, false
		}
		defer rc.Close()
		data, err := io.ReadAll(io.LimitReader(rc, maxContentBytes))
		if err != nil {
			return nil, false
		}
		return data, true
	}
	return nil, false
}

func docxQualifies(path string) bool {
	z, names := zipNames(path)
	if z == nil {
		return false
	}
	defer z.Close()
	if !containsString(names, "word/document.xml") {
		return false
	}
	for _, n := range names {
		low := strings.ToLower(n)
		if hasAnyPrefix(low, docxDeniedDirs) || strings.HasSuffix(low, "vbaproject.bin") || hasAnySuffix(low, officeDeniedPics) {
			return false
		}
	}
	return true
}

func odtQualifies(path string) bool {
	if strings.HasSuffix(path, ".fodt") {
		// flatQualifies admits a chart; a text document keeps none, as the
		// packaged ones keep no Object* entry.
		content, ok := flatQualifies(path, []byte("<office:text"))
		return ok && len(embeddedType.FindAllIndex(content, 2)) < 2 && !flatMedia.Match(content)
	}
	z, names := zipNames(path)
	if z == nil {
		return false
	}
	defer z.Close()
	for _, n := range names {
		if strings.HasPrefix(n, "Object") || strings.HasPrefix(n, "Basic/") || hasAnySuffix(strings.ToLower(n), odfDeniedPics) {
			return false
		}
	}
	content, ok := zipPart(z, "content.xml")
	return ok && bytes.Contains(content, []byte("<office:text"))
}

func xlsxQualifies(path string) bool {
	z, names := zipNames(path)
	if z == nil {
		return false
	}
	defer z.Close()
	if !containsString(names, "xl/workbook.xml") {
		return false
	}
	for _, n := range names {
		low := strings.ToLower(n)
		if hasAnyPrefix(low, xlsxDeniedDirs) || low == "xl/vbaproject.bin" || hasAnySuffix(low, officeDeniedPics) {
			return false
		}
	}
	return true
}

func odsQualifies(path string) bool {
	if strings.HasSuffix(path, ".fods") {
		_, ok := flatQualifies(path, []byte("<office:spreadsheet"))
		return ok
	}
	return packageQualifies(path, []byte("<office:spreadsheet"), odfDeniedPics)
}

func odpQualifies(path string) bool {
	if strings.HasSuffix(path, ".fodp") {
		content, ok := flatQualifies(path, []byte("<office:presentation"))
		return ok && !flatMedia.Match(content)
	}
	return packageQualifies(path, []byte("<office:presentation"), append(append([]string{}, odfDeniedPics...), mediaExtensions...))
}

// packageQualifies reports whether the OpenDocument package at path holds
// body in its content.xml, no macros, no file of a denied extension, and only
// charts as embedded objects.
func packageQualifies(path string, body []byte, denied []string) bool {
	z, names := zipNames(path)
	if z == nil {
		return false
	}
	defer z.Close()
	objects := map[string]bool{}
	for _, n := range names {
		if strings.HasPrefix(n, "Basic/") || hasAnySuffix(strings.ToLower(n), denied) {
			return false
		}
		// ObjectReplacements/ holds the pictures LibreOffice shows in place
		// of the objects, not objects of its own.
		if top, _, sep := strings.Cut(n, "/"); sep && strings.HasPrefix(top, "Object") && top != "ObjectReplacements" {
			objects[top+"/"] = true
		}
	}
	content, ok := zipPart(z, "content.xml")
	if !ok {
		return false
	}
	if len(objects) > 0 {
		manifest, ok := zipPart(z, "META-INF/manifest.xml")
		if !ok {
			return false
		}
		types := map[string][]byte{}
		for _, entry := range fileEntryPattern.FindAll(manifest, -1) {
			attrs := map[string][]byte{}
			for _, m := range attrPattern.FindAllSubmatch(entry, -1) {
				attrs[string(m[1])] = m[2]
			}
			types[string(attrs["full-path"])] = attrs["media-type"]
		}
		for o := range objects {
			if !bytes.Equal(types[o], chartMediaType) {
				return false
			}
		}
	}
	return bytes.Contains(content, body)
}

// flatQualifies reports whether the flat OpenDocument file at path holds body
// and embeds charts alone, and returns its content. The selectors' size caps
// hold a flat file to a size read whole.
func flatQualifies(path string, body []byte) ([]byte, bool) {
	content, err := os.ReadFile(path) //nolint:gosec // a candidate document of a source checkout
	if err != nil || !bytes.Contains(content, body) {
		return nil, false
	}
	for i, m := range embeddedType.FindAllSubmatch(content, -1) {
		if i > 0 && !bytes.Equal(m[1], chartMediaType) {
			return nil, false
		}
	}
	return content, true
}

func pptxQualifies(path string) bool {
	z, names := zipNames(path)
	if z == nil {
		return false
	}
	defer z.Close()
	if !containsString(names, "ppt/presentation.xml") {
		return false
	}
	embedded := false
	for _, n := range names {
		low := strings.ToLower(n)
		if strings.HasPrefix(low, "ppt/activex/") || low == "ppt/vbaproject.bin" || hasAnySuffix(low, officeDeniedPics) || hasAnySuffix(low, mediaExtensions) {
			return false
		}
		embedded = embedded || strings.HasPrefix(low, "ppt/embeddings/")
	}
	if !embedded {
		return true
	}
	// A chart's relationships target its data workbook (ECMA-376 Part 1,
	// 21.2.2.63 externalData), which no viewer draws; any other embedding
	// is an object.
	for _, n := range names {
		if !strings.HasSuffix(n, ".rels") || strings.HasPrefix(n, "ppt/charts/_rels/") {
			continue
		}
		rels, ok := zipPart(z, n)
		if !ok || embeddingTarget.Match(rels) {
			return false
		}
	}
	return true
}

func containsString(list []string, s string) bool {
	return slices.Contains(list, s)
}

// The binary formats are compound files (MS-CFB): a FAT-chained sector file
// holding a tree of storages and streams. The reader lists the tree and
// reads the head of a stream, which is all the checks need.
const (
	cfbEnd     = 0xFFFFFFFA // ENDOFCHAIN and the special values above it (MS-CFB 2.1)
	cfbStorage = 1          // object types of a directory entry (MS-CFB 2.6.1)
	cfbStream  = 2
	cfbRoot    = 5
)

var (
	cfbMagic = []byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1}
	errCFB   = errors.New("not a readable compound file")
)

type cfbEntry struct {
	name        string
	kind        byte
	start, size uint32
}

// compoundFile is a compound file read whole, its directory listed.
type compoundFile struct {
	data       []byte
	shift      uint
	miniShift  uint
	sector     int
	count      uint32
	cutoff     uint32
	fat        []uint32
	minifat    []uint32
	entries    []cfbEntry
	ministream []byte
}

func le32s(b []byte, n int) ([]uint32, error) {
	if len(b) < 4*n {
		return nil, errCFB
	}
	out := make([]uint32, n)
	for i := range out {
		out[i] = binary.LittleEndian.Uint32(b[4*i:])
	}
	return out, nil
}

func newCompoundFile(data []byte) (*compoundFile, error) {
	if len(data) < 512 || !bytes.Equal(data[:8], cfbMagic) {
		return nil, errCFB
	}
	c := &compoundFile{data: data}
	c.shift = uint(binary.LittleEndian.Uint16(data[0x1E:]))
	c.miniShift = uint(binary.LittleEndian.Uint16(data[0x20:]))
	if (c.shift != 9 && c.shift != 12) || c.miniShift != 6 {
		return nil, errCFB
	}
	c.sector = 1 << c.shift
	c.count = uint32((len(data)-512)/c.sector + 1) //nolint:gosec // fits uint32: data is at most maxCFBBytes
	nFAT := binary.LittleEndian.Uint32(data[0x2C:])
	firstDir := binary.LittleEndian.Uint32(data[0x30:])
	c.cutoff = binary.LittleEndian.Uint32(data[0x38:])
	firstMini := binary.LittleEndian.Uint32(data[0x3C:])
	firstDIFAT := binary.LittleEndian.Uint32(data[0x44:])
	nDIFAT := binary.LittleEndian.Uint32(data[0x48:])
	difat, err := le32s(data[0x4C:], 109)
	if err != nil {
		return nil, err
	}
	sid := firstDIFAT
	for range min(nDIFAT, c.count) {
		if sid >= cfbEnd || sid >= c.count {
			break
		}
		entries, err := le32s(c.sectorBytes(sid), c.sector/4)
		if err != nil {
			return nil, err
		}
		difat = append(difat, entries[:len(entries)-1]...)
		sid = entries[len(entries)-1]
	}
	for _, s := range difat[:min(int(nFAT), len(difat))] { //nolint:gosec // nFAT bounded by the DIFAT's length
		if s >= c.count {
			return nil, errCFB
		}
		entries, err := le32s(c.sectorBytes(s), c.sector/4)
		if err != nil {
			return nil, err
		}
		c.fat = append(c.fat, entries...)
	}
	dir := c.chainBytes(firstDir, c.fat, c.sectorBytes, -1)
	for off := 0; off+128 <= len(dir); off += 128 {
		e := dir[off : off+128]
		nameLen := int(binary.LittleEndian.Uint16(e[0x40:]))
		kind := e[0x42]
		if (kind != cfbStorage && kind != cfbStream && kind != cfbRoot) || nameLen < 2 || nameLen > 64 {
			continue
		}
		u := make([]uint16, (nameLen-2)/2)
		for i := range u {
			u[i] = binary.LittleEndian.Uint16(e[2*i:])
		}
		c.entries = append(c.entries, cfbEntry{
			name:  string(utf16.Decode(u)),
			kind:  kind,
			start: binary.LittleEndian.Uint32(e[0x74:]),
			size:  binary.LittleEndian.Uint32(e[0x78:]),
		})
	}
	for _, e := range c.entries {
		if e.kind == cfbRoot {
			c.ministream = c.chainBytes(e.start, c.fat, c.sectorBytes, -1)
			break
		}
	}
	if firstMini < cfbEnd {
		mf := c.chainBytes(firstMini, c.fat, c.sectorBytes, -1)
		c.minifat, _ = le32s(mf, len(mf)/4)
	}
	return c, nil
}

func (c *compoundFile) sectorBytes(sid uint32) []byte {
	off := (int(sid) + 1) << c.shift
	if off >= len(c.data) {
		return nil
	}
	return c.data[off:min(off+c.sector, len(c.data))]
}

// chainBytes follows a sector chain through fat from sid, reading each
// sector with read, until the chain ends, leaves the table, loops past its
// length, or holds limit bytes (limit < 0: no limit).
func (c *compoundFile) chainBytes(sid uint32, fat []uint32, read func(uint32) []byte, limit int) []byte {
	var out []byte
	seen := 0
	for sid < cfbEnd && int(sid) < len(fat) && seen <= len(fat) {
		out = append(out, read(sid)...)
		seen++
		if limit >= 0 && len(out) >= limit {
			break
		}
		sid = fat[sid]
	}
	return out
}

func (c *compoundFile) has(name string) bool {
	for _, e := range c.entries {
		if e.name == name {
			return true
		}
	}
	return false
}

// stream returns the first limit bytes of the stream named name, nil when
// there is none.
func (c *compoundFile) stream(name string, limit int) []byte {
	for _, e := range c.entries {
		if e.name != name || e.kind != cfbStream {
			continue
		}
		var data []byte
		if e.size < c.cutoff {
			mini := 1 << c.miniShift
			read := func(sid uint32) []byte {
				off := int(sid) * mini
				if off >= len(c.ministream) {
					return nil
				}
				return c.ministream[off:min(off+mini, len(c.ministream))]
			}
			data = c.chainBytes(e.start, c.minifat, read, limit)
		} else {
			data = c.chainBytes(e.start, c.fat, c.sectorBytes, limit)
		}
		return data[:min(int(e.size), limit, len(data))]
	}
	return nil
}

// openCompound reads the compound file at path, nil when it is not one or is
// past maxCFBBytes.
func openCompound(path string) *compoundFile {
	f, err := os.Open(path) //nolint:gosec // a candidate document of a source checkout
	if err != nil {
		return nil
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxCFBBytes+1))
	if err != nil || len(data) > maxCFBBytes {
		return nil
	}
	c, err := newCompoundFile(data)
	if err != nil {
		return nil
	}
	return c
}

// binaryClean reports a compound file without the storages of macros and
// embedded objects: Word's and Excel's VBA projects, Word's ObjectPool, and
// the MBD storages Excel keeps an embedded object each in.
func binaryClean(c *compoundFile) bool {
	for _, e := range c.entries {
		if e.name == "Macros" || e.name == "_VBA_PROJECT_CUR" || e.name == "ObjectPool" || strings.HasPrefix(e.name, "MBD") {
			return false
		}
	}
	return true
}

// docQualifies accepts a Word 97 to 2003 document (MS-DOC): a WordDocument
// stream whose FIB names Word 97 or later (nFib 0x00C1 and up) and is not
// encrypted (fEncrypted, MS-DOC 2.5.2), without macros or embedded objects.
func docQualifies(path string) bool {
	c := openCompound(path)
	if c == nil || !binaryClean(c) {
		return false
	}
	fib := c.stream("WordDocument", 64)
	if len(fib) < 12 {
		return false
	}
	ident, nFib, flags := binary.LittleEndian.Uint16(fib), binary.LittleEndian.Uint16(fib[2:]), binary.LittleEndian.Uint16(fib[10:])
	return ident == 0xA5EC && nFib >= 0x00C1 && flags&0x0100 == 0
}

// xlsQualifies accepts an Excel 97 to 2003 workbook (MS-XLS): a Workbook
// stream opening on a BIFF8 BOF record (version 0x0600) not followed by
// FilePass (MS-XLS 2.4.117), without macros or embedded objects.
func xlsQualifies(path string) bool {
	c := openCompound(path)
	if c == nil || !binaryClean(c) {
		return false
	}
	wb := c.stream("Workbook", 256)
	if len(wb) < 8 {
		return false
	}
	rtype, size, version := binary.LittleEndian.Uint16(wb), int(binary.LittleEndian.Uint16(wb[2:])), binary.LittleEndian.Uint16(wb[4:])
	if rtype != 0x0809 || version != 0x0600 || len(wb) < 4+size+4 {
		return false
	}
	return binary.LittleEndian.Uint16(wb[4+size:]) != 0x002F
}

// pptQualifies accepts a PowerPoint 97 to 2003 presentation (MS-PPT): a
// PowerPoint Document and a Current User stream, and no EncryptedSummary
// stream, which an encrypted presentation carries (MS-PPT 2.3.7).
func pptQualifies(path string) bool {
	c := openCompound(path)
	return c != nil && c.has("PowerPoint Document") && c.has("Current User") && !c.has("EncryptedSummary")
}
