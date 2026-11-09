package pdfgen

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// This file ports qpdf 12.2's linearized writing for the configuration the
// corpus uses: no encryption, no object streams, uncompressed streams
// (so an uncompressed hint stream), deterministic /ID. The sources are
// QPDFWriter::writeLinearized, QPDF::optimize (QPDF_optimization.cc) and
// QPDF::calculateLinearizationData and the hint table writers
// (QPDF_linearization.cc).

var errLinearizeObjectStreams = errors.New("pdfgen: linearization with object streams is not implemented")

// User types, in qpdf's ObjUser::user_e order.
const (
	ouPage = iota + 1
	ouThumb
	ouTrailerKey
	ouRootKey
	ouRoot
)

// objUser is qpdf's QPDF::ObjUser.
type objUser struct {
	typ    int
	pageno int
	key    string
}

// linData holds what QPDF::optimize and calculateLinearizationData compute.
type linData struct {
	w *writer

	pages      []*Dict
	userToObjs map[objUser]map[*indirect]bool
	objToUsers map[*indirect]map[objUser]bool
	objOf      map[*indirect]Object

	part4, part6, part7, part8, part9 []Object

	// c_page_offset_data entries.
	pageNObjects []int
	pageShared   [][]int
	// c_shared_object_data.
	sharedObjs      []*indirect
	nsharedFirst    int
	firstSharedObj  *indirect
	outlineFirstObj *indirect
	outlineNObjects int
}

// sortedInds returns the set ordered by source object number, as qpdf's
// std::set<QPDFObjGen> iterates.
func sortedInds(set map[*indirect]bool) []*indirect {
	out := make([]*indirect, 0, len(set))
	for ind := range set {
		out = append(out, ind)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	return out
}

func isDictOfType(o Object, typ string) bool {
	d, ok := resolve(o).(*Dict)
	return ok && resolve(d.Get("Type")) == Object(Name(typ))
}

// allPages is QPDF::getAllPages for a well-formed tree: the leaves of the
// /Pages tree, in order.
func (l *linData) allPages() {
	l.pages = nil
	visited := map[*Dict]bool{}
	var walk func(node *Dict)
	walk = func(node *Dict) {
		if visited[node] {
			return
		}
		visited[node] = true
		kids, _ := resolve(node.Get("Kids")).(*Array)
		if kids == nil {
			return
		}
		for _, k := range kids.Items {
			kid, ok := resolve(k).(*Dict)
			if !ok {
				continue
			}
			if kid.Has("Kids") {
				walk(kid)
			} else {
				l.pages = append(l.pages, kid)
			}
		}
	}
	if root, ok := resolve(l.w.doc.Root.Get("Pages")).(*Dict); ok {
		walk(root)
	}
}

var inheritableKeys = map[string]bool{"MediaBox": true, "CropBox": true, "Resources": true, "Rotate": true}

// pushInheritedAttributes is QPDF::pushInheritedAttributesToPage: the
// inheritable attributes of every /Pages node move down to the pages that
// lack them. qpdf does it once per document, on the first page insertion
// (flattenPagesTree) or in optimize, whichever comes first; a page tree node
// attribute set after that stays where it is.
func (d *Doc) pushInheritedAttributes() {
	if pages, ok := resolve(d.Root.Get("Pages")).(*Dict); ok {
		d.pushInherited(pages, map[string][]Object{})
	}
	d.pushedInherited = true
}

// pushInherited is QPDF::pushInheritedAttributesToPageInternal with
// allow_changes.
func (d *Doc) pushInherited(cur *Dict, ancestors map[string][]Object) {
	var inherited []string
	for _, k := range cur.Keys() {
		if !inheritableKeys[k] {
			continue
		}
		inherited = append(inherited, k)
		oh := cur.Get(k)
		if indOf(oh) == nil {
			switch oh.(type) {
			case *Dict, *Array:
				// Shared direct non-scalars become indirect.
				oh = d.MakeIndirect(oh)
			}
		}
		ancestors[k] = append(ancestors[k], oh)
		cur.Del(k)
	}
	if kids, ok := resolve(cur.Get("Kids")).(*Array); ok {
		for _, k := range kids.Items {
			kid, ok := resolve(k).(*Dict)
			if !ok {
				continue
			}
			if isDictOfType(kid, "Pages") {
				d.pushInherited(kid, ancestors)
				continue
			}
			keys := make([]string, 0, len(ancestors))
			for key := range ancestors {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				if !kid.Has(key) || isNull(kid.Get(key)) {
					stack := ancestors[key]
					kid.Set(key, stack[len(stack)-1])
				}
			}
		}
	}
	for _, k := range inherited {
		ancestors[k] = ancestors[k][:len(ancestors[k])-1]
		if len(ancestors[k]) == 0 {
			delete(ancestors, k)
		}
	}
}

// skipStreamParameters is the writer's skip_stream_parameters callback: 2
// when the writer filters the stream (and so drops /Filter and
// /DecodeParms), 1 otherwise (it always rewrites /Length).
func (l *linData) skipStreamParameters(s *Stream) int {
	if l.w.isRootMetadata(s) {
		if _, ok, err := decodeAll(&s.Dict, s.Data); err == nil && ok {
			return 2
		}
	}
	return 1
}

func (l *linData) addUser(ou objUser, ind *indirect, o Object) {
	if l.userToObjs[ou] == nil {
		l.userToObjs[ou] = map[*indirect]bool{}
	}
	l.userToObjs[ou][ind] = true
	if l.objToUsers[ind] == nil {
		l.objToUsers[ind] = map[objUser]bool{}
	}
	l.objToUsers[ind][ou] = true
	l.objOf[ind] = o
}

// updateObjectMaps is QPDF::updateObjectMaps.
func (l *linData) updateObjectMaps(firstOU objUser, first Object) {
	type frame struct {
		ou  objUser
		o   Object
		top bool
	}
	visited := map[*indirect]bool{}
	pending := []frame{{firstOU, first, true}}
	for len(pending) > 0 {
		cur := pending[len(pending)-1]
		pending = pending[:len(pending)-1]

		isPage := false
		if _, isDict := cur.o.(*Dict); isDict && isDictOfType(cur.o, "Page") {
			isPage = true
			if !cur.top {
				continue
			}
		}
		if ind := indOf(cur.o); ind != nil {
			if visited[ind] {
				continue
			}
			visited[ind] = true
			l.addUser(cur.ou, ind, cur.o)
		}

		var dict *Dict
		ssp := 0
		switch x := resolve(cur.o).(type) {
		case *Array:
			for _, it := range x.Items {
				pending = append(pending, frame{cur.ou, it, false})
			}
			continue
		case *Dict:
			dict = x
		case *Stream:
			dict = &x.Dict
			ssp = l.skipStreamParameters(x)
		default:
			continue
		}
		for _, k := range dict.Keys() {
			v := dict.m[k]
			if isNull(v) {
				continue
			}
			switch {
			case isPage && k == "Thumb":
				pending = append(pending, frame{objUser{typ: ouThumb, pageno: cur.ou.pageno}, v, false})
			case isPage && k == "Parent":
			case ssp >= 1 && k == "Length":
			case ssp >= 2 && (k == "Filter" || k == "DecodeParms"):
			default:
				pending = append(pending, frame{cur.ou, v, false})
			}
		}
	}
}

// optimize is QPDF::optimize_internal without object streams.
func (l *linData) optimize() {
	root := l.w.doc.Root
	if o, ok := root.Get("Outlines").(*Dict); ok && o.ind == nil {
		root.Set("Outlines", l.w.doc.MakeIndirect(o))
	}

	if !l.w.doc.pushedInherited {
		l.w.doc.pushInheritedAttributes()
	}
	l.allPages()

	for i, p := range l.pages {
		l.updateObjectMaps(objUser{typ: ouPage, pageno: i}, p)
	}
	t := l.w.doc.Trailer
	for _, k := range t.Keys() {
		if k == "Root" || isNull(t.m[k]) {
			continue
		}
		l.updateObjectMaps(objUser{typ: ouTrailerKey, key: k}, t.m[k])
	}
	for _, k := range root.Keys() {
		if isNull(root.m[k]) {
			continue
		}
		l.updateObjectMaps(objUser{typ: ouRootKey, key: k}, root.m[k])
	}
	l.addUser(objUser{typ: ouRoot}, root.ind, root)
}

var openDocumentKeys = map[string]bool{
	"ViewerPreferences": true, "PageMode": true, "Threads": true, "OpenAction": true, "AcroForm": true,
}

// calculate is QPDF::calculateLinearizationData without object streams.
func (l *linData) calculate() error {
	root := l.w.doc.Root
	outlinesInFirstPage := false
	if pm, ok := resolve(root.Get("PageMode")).(Name); ok && pm == "UseOutlines" && root.Has("Outlines") {
		outlinesInFirstPage = true
	}

	lcOpenDocument, lcFirstPagePrivate, lcFirstPageShared, lcOtherPagePrivate,
		lcOtherPageShared, lcThumbnailPrivate, lcThumbnailShared, lcOther, lcOutlines,
		lcRoot := map[*indirect]bool{}, map[*indirect]bool{}, map[*indirect]bool{}, map[*indirect]bool{},
		map[*indirect]bool{}, map[*indirect]bool{}, map[*indirect]bool{}, map[*indirect]bool{}, map[*indirect]bool{},
		map[*indirect]bool{}

	for ind, ous := range l.objToUsers {
		inOpenDocument, inFirstPage, inOutlines, isRoot := false, false, false, false
		otherPages, thumbs, others := 0, 0, 0
		for ou := range ous {
			switch ou.typ {
			case ouTrailerKey:
				if ou.key == "Encrypt" {
					inOpenDocument = true
				} else {
					others++
				}
			case ouThumb:
				thumbs++
			case ouRootKey:
				switch {
				case openDocumentKeys[ou.key]:
					inOpenDocument = true
				case ou.key == "Outlines":
					inOutlines = true
				default:
					others++
				}
			case ouPage:
				if ou.pageno == 0 {
					inFirstPage = true
				} else {
					otherPages++
				}
			case ouRoot:
				isRoot = true
			}
		}
		switch {
		case isRoot:
			lcRoot[ind] = true
		case inOutlines:
			lcOutlines[ind] = true
		case inOpenDocument:
			lcOpenDocument[ind] = true
		case inFirstPage && others == 0 && otherPages == 0 && thumbs == 0:
			lcFirstPagePrivate[ind] = true
		case inFirstPage:
			lcFirstPageShared[ind] = true
		case otherPages == 1 && others == 0 && thumbs == 0:
			lcOtherPagePrivate[ind] = true
		case otherPages > 1:
			lcOtherPageShared[ind] = true
		case thumbs == 1 && others == 0:
			lcThumbnailPrivate[ind] = true
		case thumbs > 1:
			lcThumbnailShared[ind] = true
		default:
			lcOther[ind] = true
		}
	}

	npages := len(l.pages)
	l.pageNObjects = make([]int, npages)
	l.pageShared = make([][]int, npages)

	if len(lcRoot) != 1 {
		return errors.New("pdfgen: found other than one root while calculating linearization data")
	}
	l.part4 = append(l.part4, root)
	for _, ind := range sortedInds(lcOpenDocument) {
		l.part4 = append(l.part4, l.objOf[ind])
	}

	if npages == 0 {
		return errors.New("pdfgen: no pages found while calculating linearization data")
	}
	first := l.pages[0]
	if !lcFirstPagePrivate[first.ind] {
		return errors.New("pdfgen: first page object not in lc_first_page_private")
	}
	delete(lcFirstPagePrivate, first.ind)
	l.part6 = append(l.part6, first)
	for _, ind := range sortedInds(lcFirstPagePrivate) {
		l.part6 = append(l.part6, l.objOf[ind])
	}
	for _, ind := range sortedInds(lcFirstPageShared) {
		l.part6 = append(l.part6, l.objOf[ind])
	}
	if outlinesInFirstPage {
		l.part6 = l.pushOutlines(l.part6, lcOutlines)
	}
	l.pageNObjects[0] = len(l.part6)

	for i := 1; i < npages; i++ {
		p := l.pages[i]
		if !lcOtherPagePrivate[p.ind] {
			return fmt.Errorf("pdfgen: page object for page %d not in lc_other_page_private", i)
		}
		delete(lcOtherPagePrivate, p.ind)
		l.part7 = append(l.part7, p)
		l.pageNObjects[i] = 1
		objs := l.userToObjs[objUser{typ: ouPage, pageno: i}]
		if objs == nil {
			return errors.New("pdfgen: found unreferenced page while calculating linearization data")
		}
		for _, ind := range sortedInds(objs) {
			if lcOtherPagePrivate[ind] {
				delete(lcOtherPagePrivate, ind)
				l.part7 = append(l.part7, l.objOf[ind])
				l.pageNObjects[i]++
			}
		}
	}
	if len(lcOtherPagePrivate) != 0 {
		return errors.New("pdfgen: lc_other_page_private is not empty after generation of part7")
	}

	for _, ind := range sortedInds(lcOtherPageShared) {
		l.part8 = append(l.part8, l.objOf[ind])
	}

	pagesOgs := l.userToObjs[objUser{typ: ouRootKey, key: "Pages"}]
	if len(pagesOgs) == 0 {
		return errors.New("pdfgen: found empty pages tree while calculating linearization data")
	}
	for _, ind := range sortedInds(pagesOgs) {
		if lcOther[ind] {
			delete(lcOther, ind)
			l.part9 = append(l.part9, l.objOf[ind])
		}
	}
	for i, p := range l.pages {
		thumb := p.Get("Thumb")
		if isNull(thumb) {
			continue
		}
		if ind := indOf(thumb); ind != nil && lcThumbnailPrivate[ind] {
			delete(lcThumbnailPrivate, ind)
			l.part9 = append(l.part9, thumb)
		}
		for _, ind := range sortedInds(l.userToObjs[objUser{typ: ouThumb, pageno: i}]) {
			if lcThumbnailPrivate[ind] {
				delete(lcThumbnailPrivate, ind)
				l.part9 = append(l.part9, l.objOf[ind])
			}
		}
	}
	if len(lcThumbnailPrivate) != 0 {
		return errors.New("pdfgen: lc_thumbnail_private not empty after placing thumbnails")
	}
	for _, ind := range sortedInds(lcThumbnailShared) {
		l.part9 = append(l.part9, l.objOf[ind])
	}
	if !outlinesInFirstPage {
		l.part9 = l.pushOutlines(l.part9, lcOutlines)
	}
	for _, ind := range sortedInds(lcOther) {
		l.part9 = append(l.part9, l.objOf[ind])
	}

	placed := len(l.part4) + len(l.part6) + len(l.part7) + len(l.part8) + len(l.part9)
	if placed != len(l.objToUsers) {
		return fmt.Errorf("pdfgen: wrong number of objects placed (num_placed = %d; number of objects: %d)", placed, len(l.objToUsers))
	}

	// Shared object hint table: all of part 6, then all of part 8.
	objToIndex := map[*indirect]int{}
	l.nsharedFirst = len(l.part6)
	for _, o := range l.part6 {
		objToIndex[indOf(o)] = len(l.sharedObjs)
		l.sharedObjs = append(l.sharedObjs, indOf(o))
	}
	if len(l.part8) > 0 {
		l.firstSharedObj = indOf(l.part8[0])
		for _, o := range l.part8 {
			objToIndex[indOf(o)] = len(l.sharedObjs)
			l.sharedObjs = append(l.sharedObjs, indOf(o))
		}
	}
	for i := 1; i < npages; i++ {
		for _, ind := range sortedInds(l.userToObjs[objUser{typ: ouPage, pageno: i}]) {
			if idx, ok := objToIndex[ind]; ok && len(l.objToUsers[ind]) > 1 {
				l.pageShared[i] = append(l.pageShared[i], idx)
			}
		}
	}
	return nil
}

// pushOutlines is QPDF::pushOutlinesToPart.
func (l *linData) pushOutlines(part []Object, lcOutlines map[*indirect]bool) []Object {
	outlines := l.w.doc.Root.Get("Outlines")
	if isNull(outlines) {
		return part
	}
	ind := indOf(outlines)
	l.outlineFirstObj = ind
	l.outlineNObjects = 1
	delete(lcOutlines, ind)
	part = append(part, outlines)
	for _, o := range sortedInds(lcOutlines) {
		part = append(part, l.objOf[o])
		l.outlineNObjects++
	}
	return part
}

// bitWriter is qpdf's BitWriter: most significant bit first, flush pads the
// last byte with zero bits.
type bitWriter struct {
	buf    []byte
	ch     byte
	bitOff int
}

func newBitWriter() *bitWriter { return &bitWriter{bitOff: 7} }

func (b *bitWriter) writeBits(val uint64, bits int) {
	for i := bits - 1; i >= 0; i-- {
		if (val>>uint(i))&1 != 0 {
			b.ch |= 1 << uint(b.bitOff)
		}
		if b.bitOff == 0 {
			b.buf = append(b.buf, b.ch)
			b.ch = 0
			b.bitOff = 7
		} else {
			b.bitOff--
		}
	}
}

func (b *bitWriter) flush() {
	if b.bitOff < 7 {
		b.buf = append(b.buf, b.ch)
		b.ch = 0
		b.bitOff = 7
	}
}

func nbits(v int) int {
	n := 0
	for v != 0 {
		n++
		v >>= 1
	}
	return n
}

// generateHintStream is QPDF::generateHintStream uncompressed, fed with the
// first pass's offsets and lengths. It returns the stream data, /S and /O.
func (l *linData) generateHintStream(offsets func(int) int, lengths map[int]int) ([]byte, int, int, error) {
	renum := l.w.renumber
	var lerr error
	lengthNextN := func(in *indirect, n int) int {
		first := renum[in]
		if first <= 0 {
			lerr = errors.New("pdfgen: found object that is not renumbered while writing linearization data")
			return 0
		}
		total := 0
		for i := first; i < first+n; i++ {
			if lengths[i] == 0 {
				lerr = errors.New("pdfgen: found item with unknown length while writing linearization data")
			}
			total += lengths[i]
		}
		return total
	}

	// Page offset hint table (calculateHPageOffset).
	npages := len(l.pages)
	minNObjects := l.pageNObjects[0]
	maxNObjects := minNObjects
	minLength := lengthNextN(l.pages[0].ind, minNObjects)
	maxLength := minLength
	maxShared := len(l.pageShared[0])
	deltaNObjects := make([]int, npages)
	deltaLength := make([]int, npages)
	for i := range npages {
		nobj := l.pageNObjects[i]
		length := lengthNextN(l.pages[i].ind, nobj)
		minNObjects = min(minNObjects, nobj)
		maxNObjects = max(maxNObjects, nobj)
		minLength = min(minLength, length)
		maxLength = max(maxLength, length)
		maxShared = max(maxShared, len(l.pageShared[i]))
		deltaNObjects[i] = nobj
		deltaLength[i] = length
	}
	for i := range npages {
		deltaNObjects[i] -= minNObjects
		deltaLength[i] -= minLength
	}
	nbitsDeltaNObjects := nbits(maxNObjects - minNObjects)
	nbitsDeltaLength := nbits(maxLength - minLength)
	nbitsNShared := nbits(maxShared)
	nbitsSharedID := nbits(len(l.sharedObjs))

	w := newBitWriter()
	w.writeBits(u64(minNObjects), 32)
	w.writeBits(u64(offsets(renum[l.pages[0].ind])), 32)
	w.writeBits(u64(nbitsDeltaNObjects), 16)
	w.writeBits(u64(minLength), 32)
	w.writeBits(u64(nbitsDeltaLength), 16)
	w.writeBits(0, 32) // min_content_offset
	w.writeBits(0, 16) // nbits_delta_content_offset
	w.writeBits(u64(minLength), 32)
	w.writeBits(u64(nbitsDeltaLength), 16)
	w.writeBits(u64(nbitsNShared), 16)
	w.writeBits(u64(nbitsSharedID), 16)
	w.writeBits(0, 16) // nbits_shared_numerator
	w.writeBits(4, 16) // shared_denominator
	for i := range npages {
		w.writeBits(u64(deltaNObjects[i]), nbitsDeltaNObjects)
	}
	w.flush()
	for i := range npages {
		w.writeBits(u64(deltaLength[i]), nbitsDeltaLength)
	}
	w.flush()
	for i := range npages {
		w.writeBits(u64(len(l.pageShared[i])), nbitsNShared)
	}
	w.flush()
	for i := range npages {
		for _, id := range l.pageShared[i] {
			w.writeBits(u64(id), nbitsSharedID)
		}
	}
	w.flush()
	// Shared numerators: zero bits each.
	w.flush()
	// Delta content offsets: zero bits each.
	w.flush()
	for i := range npages {
		w.writeBits(u64(deltaLength[i]), nbitsDeltaLength)
	}
	w.flush()
	s := len(w.buf)

	// Shared object hint table (calculateHSharedObject).
	nshared := len(l.sharedObjs)
	soMin := lengthNextN(l.sharedObjs[0], 1)
	soMax := soMin
	groupLength := make([]int, nshared)
	for i, ind := range l.sharedObjs {
		length := lengthNextN(ind, 1)
		soMin = min(soMin, length)
		soMax = max(soMax, length)
		groupLength[i] = length
	}
	firstSharedObj, firstSharedOffset := 0, 0
	if nshared > l.nsharedFirst {
		firstSharedObj = renum[l.firstSharedObj]
		firstSharedOffset = offsets(firstSharedObj)
	}
	nbitsGroup := nbits(soMax - soMin)
	w.writeBits(u64(firstSharedObj), 32)
	w.writeBits(u64(firstSharedOffset), 32)
	w.writeBits(u64(l.nsharedFirst), 32)
	w.writeBits(u64(nshared), 32)
	w.writeBits(0, 16) // nbits_nobjects
	w.writeBits(u64(soMin), 32)
	w.writeBits(u64(nbitsGroup), 16)
	for i := range nshared {
		w.writeBits(u64(groupLength[i]-soMin), nbitsGroup)
	}
	w.flush()
	for range nshared {
		w.writeBits(0, 1) // signature_present
	}
	w.flush()
	// nobjects_minus_one: zero bits each.
	w.flush()

	// Outline hint table (calculateHOutline, writeHGeneric).
	o := 0
	if l.outlineNObjects > 0 {
		o = len(w.buf)
		first := renum[l.outlineFirstObj]
		w.writeBits(u64(first), 32)
		w.writeBits(u64(offsets(first)), 32)
		w.writeBits(u64(l.outlineNObjects), 32)
		w.writeBits(u64(lengthNextN(l.outlineFirstObj, l.outlineNObjects)), 32)
	}
	return w.buf, s, o, lerr
}

const (
	trailerLinFirst = iota + 1
	trailerLinSecond
)

// originalID1 is QPDFWriter::getOriginalID1.
func (w *writer) originalID1() String {
	if a, ok := resolve(w.doc.Trailer.Get("ID")).(*Array); ok && len(a.Items) > 0 {
		if s, ok := resolve(a.Items[0]).(String); ok {
			return s
		}
	}
	return nil
}

// linTrailer is QPDFWriter::writeTrailer for a cross-reference table in a
// linearized file.
func (w *writer) linTrailer(which, size, prev, pass int, id1, id2 String) {
	w.writeString("trailer <<")
	if which == trailerLinSecond {
		w.writeString(" /Size " + strconv.Itoa(size))
	} else {
		t := w.trimmedTrailer()
		for _, k := range t.Keys() {
			v := t.m[k]
			if isNull(v) {
				continue
			}
			w.writeString(" " + normalizeName(k) + " ")
			if k == "Size" {
				w.writeString(strconv.Itoa(size))
				if which == trailerLinFirst {
					w.writeString(" /Prev ")
					p := strconv.Itoa(prev)
					w.writeString(p)
					if pad := 21 - len(p); pad > 0 {
						w.writeString(strings.Repeat(" ", pad))
					}
				}
			} else {
				w.unparseChild(v, 0)
			}
		}
	}
	w.writeString(" /ID [")
	if pass == 1 {
		if orig := w.originalID1(); len(orig) == 0 {
			w.writeString("<00000000000000000000000000000000>")
		} else {
			w.writeString("<" + strings.Repeat("0", 2*len(orig)) + ">")
		}
		w.writeString("<00000000000000000000000000000000>")
	} else {
		w.writeString(hexString(id1) + hexString(id2))
	}
	w.writeString("]")
	w.writeString(" >>")
}

// linXRefTable is QPDFWriter::writeXRefTable with its linearization
// arguments. It returns the offset of the space before entry zero.
func (w *writer) linXRefTable(which, first, last, size, prev int, suppress bool, hintID, hintOffset, hintLength, pass int, id1, id2 String) int {
	w.writeString("xref\n" + strconv.Itoa(first) + " " + strconv.Itoa(last-first+1))
	spaceBeforeZero := w.out.Len()
	w.writeString("\n")
	for i := first; i <= last; i++ {
		if i == 0 {
			w.writeString("0000000000 65535 f \n")
			continue
		}
		offset := 0
		if !suppress {
			offset = w.xref[i].offset
			if hintID != 0 && i != hintID && offset >= hintOffset {
				offset += hintLength
			}
		}
		w.writeString(fmt.Sprintf("%010d 00000 n \n", offset))
	}
	w.linTrailer(which, size, prev, pass, id1, id2)
	w.writeString("\n")
	return spaceBeforeZero
}

// writeLinearized is QPDFWriter::writeLinearized for a file without object
// streams or encryption.
func (w *writer) writeLinearized() (*Output, error) {
	if len(w.ostreams) > 0 {
		return nil, errLinearizeObjectStreams
	}
	w.linearized = true

	l := &linData{
		w:          w,
		userToObjs: map[objUser]map[*indirect]bool{},
		objToUsers: map[*indirect]map[objUser]bool{},
		objOf:      map[*indirect]Object{},
	}
	l.optimize()
	if err := l.calculate(); err != nil {
		return nil, err
	}

	// Object numbers: the second half (parts 7 to 9) from 1, then the
	// linearization dictionary, part 4, the hint stream and part 6.
	secondHalfEnd := len(l.part7) + len(l.part8) + len(l.part9)
	secondTrailerSize := secondHalfEnd + 1
	lindictID := secondTrailerSize
	part4First := lindictID + 1
	hintID := part4First + len(l.part4)
	part6First := hintID + 1
	firstTrailerSize := part6First + len(l.part6)
	firstHalfEnd := firstTrailerSize - 1

	var queue []Object
	number := func(part []Object, next int) {
		for _, o := range part {
			ind := indOf(o)
			if w.renumber[ind] != 0 {
				continue
			}
			w.renumber[ind] = next
			next++
			queue = append(queue, o)
		}
	}
	// Enqueue order is part 4, part 6, then parts 7 to 9.
	number(l.part4, part4First)
	number(l.part6, part6First)
	number(append(append(append([]Object(nil), l.part7...), l.part8...), l.part9...), 1)
	w.nextObjID = firstTrailerSize

	part4End := indOf(l.part4[len(l.part4)-1])
	part6End := indOf(l.part6[len(l.part6)-1])

	var (
		spaceBeforeZero, fileSize, part6EndOffset, secondXrefOffset int
		hintLength                                                  int
		hintBuf                                                     []byte
		id1, id2                                                    String
	)
	lengths := map[int]int{}
	for pass := 1; pass <= 2; pass++ {
		w.out = &bytes.Buffer{}
		w.writeHeader()

		pos := w.out.Len()
		w.openObject(lindictID)
		w.writeString("<<")
		if pass == 2 {
			w.writeString(" /Linearized 1 /L " + strconv.Itoa(fileSize+hintLength))
			w.writeString(" /H [ " + strconv.Itoa(w.xref[hintID].offset) + " " + strconv.Itoa(hintLength))
			w.writeString(" ] /O " + strconv.Itoa(w.renumber[l.pages[0].ind]))
			w.writeString(" /E " + strconv.Itoa(part6EndOffset+hintLength))
			w.writeString(" /N " + strconv.Itoa(len(l.pages)))
			w.writeString(" /T " + strconv.Itoa(spaceBeforeZero+hintLength))
		}
		w.writeString(" >>")
		w.closeObject()
		if pad := pos - w.out.Len() + 200; pad > 0 {
			w.writeString(strings.Repeat(" ", pad))
		}
		w.writeString("\n")

		firstXrefOffset := w.out.Len()
		hintOffset := 0
		if pass == 2 {
			hintOffset = w.xref[hintID].offset
		}
		w.linXRefTable(trailerLinFirst, lindictID, firstHalfEnd, firstTrailerSize,
			hintLength+secondXrefOffset, pass == 1, hintID, hintOffset, hintLength, pass, id1, id2)
		w.writeString("startxref\n0\n%%EOF\n")

		for _, o := range queue {
			start := w.out.Len()
			w.writeQueued(o)
			lengths[w.renumber[indOf(o)]] = w.out.Len() - start
			if indOf(o) == part4End {
				if pass == 1 {
					w.xref[hintID] = xrefEntry{typ: 1, offset: w.out.Len()}
				} else {
					w.out.Write(hintBuf)
				}
			}
			if indOf(o) == part6End {
				part6EndOffset = w.out.Len()
			}
		}
		if w.err != nil {
			return nil, w.err
		}

		secondXrefOffset = w.out.Len()
		spaceBeforeZero = w.linXRefTable(trailerLinSecond, 0, secondHalfEnd, secondTrailerSize,
			0, false, 0, 0, 0, pass, id1, id2)
		w.writeString("startxref\n" + strconv.Itoa(firstXrefOffset) + "\n%%EOF\n")

		if pass == 1 {
			// The deterministic /ID digests the whole first pass.
			id2 = w.deterministicID(w.out.Bytes())
			id1 = w.originalID1()
			if len(id1) == 0 {
				id1 = id2
			}
			fileSize = w.out.Len()

			data, s, o, err := l.generateHintStream(func(id int) int { return w.xref[id].offset }, lengths)
			if err != nil {
				return nil, err
			}
			var hb strings.Builder
			hb.WriteString(strconv.Itoa(hintID) + " 0 obj\n<< /S " + strconv.Itoa(s))
			if o != 0 {
				hb.WriteString(" /O " + strconv.Itoa(o))
			}
			hb.WriteString(" /Length " + strconv.Itoa(len(data)) + " >>\nstream\n")
			hb.Write(data)
			if len(data) == 0 || data[len(data)-1] != '\n' {
				hb.WriteString("\n")
			}
			hb.WriteString("endstream\nendobj\n")
			hintBuf = []byte(hb.String())
			hintLength = len(hintBuf)
		}
	}
	return &Output{Bytes: w.out.Bytes(), Size: firstTrailerSize, nums: w.renumber}, nil
}

// u64 widens a hint table value. Object counts, offsets, lengths and bit
// widths are never negative.
func u64(v int) uint64 {
	return uint64(v) //nolint:gosec // see above: v >= 0.
}
