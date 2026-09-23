package jbig2

import (
	"encoding/binary"
	"math/rand/v2"
	"sort"
)

// This file holds a test-only JBIG2 generic region encoder. It exists so the
// decoder's templates can be checked against data this package did not decode
// itself: the round trip in generic_test.go only passes if the decoder visits
// the same template pixels and the same SLTP pattern as this encoder.
//
// Nothing here is shared with the decoder. The probability table and every
// template are transcribed again, and the context bits are gathered in the
// reverse order. T.88, 6.2.5.7 leaves that order to the implementation: two
// orders that are consistent within themselves name the same contexts under a
// relabelling, so the round trip is blind to the order and sensitive only to
// which pixels are gathered.

// encState mirrors one row of ITU-T T.88, Table E.1.
type encState struct {
	qe        uint32
	nmps      uint8
	nlps      uint8
	switchMPS bool
}

//nolint:gochecknoglobals // test-only transcription of ITU-T T.88 Table E.1
var encTable = [47]encState{
	{0x5601, 1, 1, true}, {0x3401, 2, 6, false}, {0x1801, 3, 9, false}, {0x0AC1, 4, 12, false},
	{0x0521, 5, 29, false}, {0x0221, 38, 33, false}, {0x5601, 7, 6, true}, {0x5401, 8, 14, false},
	{0x4801, 9, 14, false}, {0x3801, 10, 14, false}, {0x3001, 11, 17, false}, {0x2401, 12, 18, false},
	{0x1C01, 13, 20, false}, {0x1601, 29, 21, false}, {0x5601, 15, 14, true}, {0x5401, 16, 14, false},
	{0x5101, 17, 15, false}, {0x4801, 18, 16, false}, {0x3801, 19, 17, false}, {0x3401, 20, 18, false},
	{0x3001, 21, 19, false}, {0x2801, 22, 19, false}, {0x2401, 23, 20, false}, {0x2201, 24, 21, false},
	{0x1C01, 25, 22, false}, {0x1801, 26, 23, false}, {0x1601, 27, 24, false}, {0x1401, 28, 25, false},
	{0x1201, 29, 26, false}, {0x1101, 30, 27, false}, {0x0AC1, 31, 28, false}, {0x09C1, 32, 29, false},
	{0x08A1, 33, 30, false}, {0x0521, 34, 31, false}, {0x0441, 35, 32, false}, {0x02A1, 36, 33, false},
	{0x0221, 37, 34, false}, {0x0141, 38, 35, false}, {0x0111, 39, 36, false}, {0x0085, 40, 37, false},
	{0x0049, 41, 38, false}, {0x0025, 42, 39, false}, {0x0015, 43, 40, false}, {0x0009, 44, 41, false},
	{0x0005, 45, 42, false}, {0x0001, 45, 43, false}, {0x5601, 46, 46, false},
}

type encContext struct {
	index uint8
	mps   uint32
}

// mqEncoder is the MQ encoder of ITU-T T.88, E.2. out[0] stands for the byte
// before the first output byte, which H.2 takes to be 0x00.
type mqEncoder struct {
	out []byte
	a   uint32
	c   uint32
	ct  int
}

// newMQEncoder is INITENC, Figure E.10.
func newMQEncoder() *mqEncoder {
	return &mqEncoder{out: []byte{0x00}, a: 0x8000, ct: 12}
}

// encode codes decision d in cx: ENCODE, CODE0 and CODE1, Figures E.3 to E.5.
func (e *mqEncoder) encode(cx *encContext, d uint32) {
	s := &encTable[cx.index]
	if d == cx.mps {
		e.codeMPS(cx, s)
		return
	}
	e.codeLPS(cx, s)
}

// codeLPS is CODELPS, Figure E.6.
func (e *mqEncoder) codeLPS(cx *encContext, s *encState) {
	e.a -= s.qe
	if e.a < s.qe {
		e.c += s.qe
	} else {
		e.a = s.qe
	}
	if s.switchMPS {
		cx.mps = 1 - cx.mps
	}
	cx.index = s.nlps
	e.renormalize()
}

// codeMPS is CODEMPS, Figure E.7.
func (e *mqEncoder) codeMPS(cx *encContext, s *encState) {
	e.a -= s.qe
	if e.a&0x8000 != 0 {
		e.c += s.qe
		return
	}
	if e.a < s.qe {
		e.a = s.qe
	} else {
		e.c += s.qe
	}
	cx.index = s.nmps
	e.renormalize()
}

// renormalize is RENORME, Figure E.8.
func (e *mqEncoder) renormalize() {
	for {
		e.a <<= 1
		e.c <<= 1
		e.ct--
		if e.ct == 0 {
			e.byteOut()
		}
		if e.a&0x8000 != 0 {
			return
		}
	}
}

// byteOut is BYTEOUT, Figure E.9.
func (e *mqEncoder) byteOut() {
	last := len(e.out) - 1
	if e.out[last] != 0xFF {
		if e.c < 0x8000000 {
			e.emit8()
			return
		}
		e.out[last]++
		if e.out[last] != 0xFF {
			e.emit8()
			return
		}
		e.c &= 0x7FFFFFF
	}
	e.out = append(e.out, byte(e.c>>20))
	e.c &= 0xFFFFF
	e.ct = 7
}

func (e *mqEncoder) emit8() {
	e.out = append(e.out, byte(e.c>>19))
	e.c &= 0x7FFFF
	e.ct = 8
}

// flush is FLUSH with SETBITS, Figures E.11 and E.12, and returns the coded
// bytes, which end with the marker 0xFF 0xAC.
func (e *mqEncoder) flush() []byte {
	tempc := e.c + e.a
	e.c |= 0xFFFF
	if e.c >= tempc {
		e.c -= 0x8000
	}
	e.c <<= uint(e.ct)
	e.byteOut()
	e.c <<= uint(e.ct)
	e.byteOut()
	if e.out[len(e.out)-1] != 0xFF {
		e.out = append(e.out, 0xFF)
	}
	e.out = append(e.out, 0xAC)
	return e.out[1:]
}

// encTemplates lists every fixed pixel of each template as an (x, y) offset,
// ITU-T T.88 Figures 3 to 6, and encNominalAT the AT pixels of Table 5.
//
//nolint:gochecknoglobals // test-only transcription of ITU-T T.88 Figures 3 to 6
var encTemplates = [4][][2]int{
	{{-1, -2}, {0, -2}, {1, -2}, {-2, -1}, {-1, -1}, {0, -1}, {1, -1}, {2, -1}, {-4, 0}, {-3, 0}, {-2, 0}, {-1, 0}},
	{{-1, -2}, {0, -2}, {1, -2}, {2, -2}, {-2, -1}, {-1, -1}, {0, -1}, {1, -1}, {2, -1}, {-3, 0}, {-2, 0}, {-1, 0}},
	{{-1, -2}, {0, -2}, {1, -2}, {-2, -1}, {-1, -1}, {0, -1}, {1, -1}, {-2, 0}, {-1, 0}},
	{{-3, -1}, {-2, -1}, {-1, -1}, {0, -1}, {1, -1}, {-4, 0}, {-3, 0}, {-2, 0}, {-1, 0}},
}

//nolint:gochecknoglobals // test-only transcription of ITU-T T.88 Table 5
var encNominalAT = [4][][2]int{
	{{3, -1}, {-3, -1}, {2, -2}, {-2, -2}},
	{{3, -1}},
	{{2, -1}},
	{{2, -1}},
}

// encSLTP holds the pixel values of Figures 8 to 11 in encTemplates order,
// followed by the AT pixels in encNominalAT order.
//
//nolint:gochecknoglobals // test-only transcription of ITU-T T.88 Figures 8 to 11
var encSLTP = [4][]uint32{
	{0, 0, 1, 1, 1, 0, 0, 1, 0, 1, 0, 1, 0, 0, 1, 1},
	{0, 0, 1, 1, 1, 1, 0, 0, 1, 1, 0, 1, 0},
	{0, 0, 1, 1, 1, 0, 0, 0, 1, 1},
	{0, 1, 1, 0, 0, 0, 1, 0, 1, 1},
}

// encGather forms a context from values listed in template order, placing the
// first value in the lowest bit: the reverse of the decoder's order.
func encGather(values []uint32) uint32 {
	var cx uint32
	for i, v := range values {
		cx |= v << uint(i)
	}
	return cx
}

// encodeGeneric codes bm with template-based arithmetic coding, T.88 6.2.5,
// and returns the coded bytes.
func encodeGeneric(bm *Bitmap, template int, tpgdon bool, at [][2]int) []byte {
	e := newMQEncoder()
	encodeGenericWith(e, bm, template, tpgdon, at, make([]encContext, 1<<(len(encTemplates[template])+len(at))))
	return e.flush()
}

// encodeGenericWith codes bm into e with the given contexts, which a symbol
// dictionary shares across its bitmaps and may hand to the next dictionary.
func encodeGenericWith(e *mqEncoder, bm *Bitmap, template int, tpgdon bool, at [][2]int, contexts []encContext) {
	offsets := append(append([][2]int{}, encTemplates[template]...), at...)
	sltp := encGather(encSLTP[template])
	var ltp uint32
	values := make([]uint32, len(offsets))
	for y := range bm.Height {
		if tpgdon {
			typical := uint32(1)
			for x := range bm.Width {
				if bm.pixel(x, y) != bm.pixel(x, y-1) {
					typical = 0
					break
				}
			}
			e.encode(&contexts[sltp], typical^ltp)
			ltp = typical
			if ltp == 1 {
				continue
			}
		}
		for x := range bm.Width {
			for i, o := range offsets {
				values[i] = bm.pixel(x+o[0], y+o[1])
			}
			e.encode(&contexts[encGather(values)], bm.pixel(x, y))
		}
	}
}

// genericStream wraps coded generic region data in an embedded JBIG2 stream: a
// page information segment for a page the region's size, then an immediate
// generic region segment drawn at the origin.
func genericStream(width, height, template int, tpgdon bool, at [][2]int, coded []byte) []byte {
	page := binary.BigEndian.AppendUint32(nil, uint32(width))
	page = binary.BigEndian.AppendUint32(page, uint32(height))
	page = append(page, make([]byte, 11)...)

	region := binary.BigEndian.AppendUint32(nil, uint32(width))
	region = binary.BigEndian.AppendUint32(region, uint32(height))
	region = append(region, make([]byte, 9)...)
	flags := byte(template << 1)
	if tpgdon {
		flags |= 8
	}
	region = append(region, flags)
	for _, a := range at {
		region = append(region, byte(int8(a[0])), byte(int8(a[1])))
	}
	region = append(region, coded...)

	return concat(segmentBytes(0, typePageInformation, 1, page), segmentBytes(1, typeImmediateGeneric, 1, region))
}

// testImage draws a bitmap with the structures a generic region coder meets:
// random noise, solid runs, rows repeating the row above (typical rows for
// TPGD), and diagonal edges, so most template pixels vary.
func testImage(width, height int, seed uint64) *Bitmap {
	bm, err := newBitmap(width, height)
	if err != nil {
		panic(err)
	}
	r := rand.New(rand.NewPCG(seed, seed^0x9E3779B97F4A7C15))
	for y := range height {
		switch {
		case y%9 == 4 && y > 0:
			copy(bm.Data[y*bm.Stride:(y+1)*bm.Stride], bm.Data[(y-1)*bm.Stride:y*bm.Stride])
		case y%7 == 0:
			for x := range width {
				if (x+y)%11 < 5 {
					bm.setPixel(x, y, 1)
				}
			}
		default:
			for x := range width {
				if r.IntN(3) == 0 || (x > y && x < y+4) {
					bm.setPixel(x, y, 1)
				}
			}
		}
	}
	return bm
}

// sltpImage tiles the neighbourhood of T.88 Figures 8 to 11 for template
// around many pixels, with the AT pixels at their nominal locations, so that
// those pixels are coded in the same context as SLTP.
//
// Without this a wrong SLTP context goes unnoticed: the SLTP neighbourhood is
// one fixed 10 to 16 pixel pattern that noise almost never draws, so both the
// right and the wrong context stay otherwise unused and adapt identically.
// Real scans draw it, which is how a corpus image exposed the difference.
func sltpImage(template int) *Bitmap {
	const width, height = 101, 64
	bm, err := newBitmap(width, height)
	if err != nil {
		panic(err)
	}
	offsets := append(append([][2]int{}, encTemplates[template]...), encNominalAT[template]...)
	for y := 3; y < height; y += 5 {
		for x := 5; x+4 < width; x += 10 {
			for i, o := range offsets {
				bm.setPixel(x+o[0], y+o[1], encSLTP[template][i])
			}
			bm.setPixel(x, y, uint32(x/10+y/5)&1)
		}
	}
	return bm
}

// encInt is a test-only arithmetic integer encoder, ITU-T T.88 Table A.1: a
// sign bit, then a prefix naming the range, then the offset within it.
type encInt struct {
	cx [512]encContext
}

// encIntRanges transcribes Table A.1 by magnitude: values from lo upward are
// coded with prefix, then n bits of the magnitude minus lo.
//
//nolint:gochecknoglobals // test-only transcription of ITU-T T.88 Table A.1
var encIntRanges = []struct {
	lo     int64
	prefix []uint32
	n      int
}{
	{0, []uint32{0}, 2},
	{4, []uint32{1, 0}, 4},
	{20, []uint32{1, 1, 0}, 6},
	{84, []uint32{1, 1, 1, 0}, 8},
	{340, []uint32{1, 1, 1, 1, 0}, 12},
	{4436, []uint32{1, 1, 1, 1, 1}, 32},
}

func (ei *encInt) put(e *mqEncoder, prev *int, bit uint32) {
	e.encode(&ei.cx[*prev], bit)
	if *prev < 256 {
		*prev = *prev<<1 | int(bit)
	} else {
		*prev = (*prev<<1|int(bit))&511 | 256
	}
}

// encode codes v, or OOB when oob is true (Table A.1: OOB is 1000).
func (ei *encInt) encode(e *mqEncoder, v int64, oob bool) {
	prev := 1
	if oob {
		for _, b := range []uint32{1, 0, 0, 0} {
			ei.put(e, &prev, b)
		}
		return
	}
	var sign uint32
	mag := v
	if v < 0 {
		sign, mag = 1, -v
	}
	ei.put(e, &prev, sign)
	r := encIntRanges[0]
	for _, candidate := range encIntRanges {
		if mag >= candidate.lo {
			r = candidate
		}
	}
	for _, b := range r.prefix {
		ei.put(e, &prev, b)
	}
	off := mag - r.lo
	for i := r.n - 1; i >= 0; i-- {
		ei.put(e, &prev, uint32(off>>uint(i))&1)
	}
}

// encIAID codes fixed-length symbol IDs, ITU-T T.88 A.3.
type encIAID struct {
	codeLen int
	cx      []encContext
}

func newEncIAID(codeLen int) *encIAID {
	return &encIAID{codeLen: codeLen, cx: make([]encContext, 1<<(codeLen+1))}
}

func (ea *encIAID) encode(e *mqEncoder, id int) {
	prev := 1
	for i := ea.codeLen - 1; i >= 0; i-- {
		bit := uint32(id>>uint(i)) & 1
		e.encode(&ea.cx[prev], bit)
		prev = prev<<1 | int(bit)
	}
}

// encRefineTemplates transcribes ITU-T T.88 Figures 12 and 13 again: the
// refined bitmap's pixels, then the reference's, as (x, y) offsets, with the
// AT pixels RA1 and RA2 of template 0 listed last in each group.
//
//nolint:gochecknoglobals // test-only transcription of ITU-T T.88 Figures 12 and 13
var encRefineTemplates = [2]struct{ refined, reference [][2]int }{
	{
		refined:   [][2]int{{0, -1}, {1, -1}, {-1, 0}},
		reference: [][2]int{{0, -1}, {1, -1}, {-1, 0}, {0, 0}, {1, 0}, {-1, 1}, {0, 1}, {1, 1}},
	},
	{
		refined:   [][2]int{{-1, -1}, {0, -1}, {1, -1}, {-1, 0}},
		reference: [][2]int{{0, -1}, {-1, 0}, {0, 0}, {1, 0}, {0, 1}, {1, 1}},
	},
}

// encodeRefinement codes target as a refinement of reference, aligned at the
// origin, T.88 6.3.5.6, and returns the coded bytes. at holds RA1 and RA2 for
// template 0.
func encodeRefinement(target, reference *Bitmap, template int, tpgron bool, at [2][2]int) []byte {
	t := encRefineTemplates[template]
	refined := append([][2]int{}, t.refined...)
	ref := append([][2]int{}, t.reference...)
	if template == 0 {
		refined = append(refined, at[0])
		ref = append(ref, at[1])
	}
	gather := func(x, y int) uint32 {
		values := make([]uint32, 0, len(refined)+len(ref))
		for _, o := range refined {
			values = append(values, target.pixel(x+o[0], y+o[1]))
		}
		for _, o := range ref {
			values = append(values, reference.pixel(x+o[0], y+o[1]))
		}
		return encGather(values)
	}
	// Figures 14 and 15: every template pixel 0 but the reference pixel under
	// the one being coded.
	sltpValues := make([]uint32, len(refined)+len(ref))
	for i, o := range ref {
		if o == [2]int{0, 0} && i < len(t.reference) {
			sltpValues[len(refined)+i] = 1
		}
	}
	sltp := encGather(sltpValues)

	// predicted reports the TPGR prediction of Figure 16: the value of a
	// uniform 3x3 reference block centred on (x, y).
	predicted := func(x, y int) (uint32, bool) {
		v := reference.pixel(x, y)
		for dy := -1; dy <= 1; dy++ {
			for dx := -1; dx <= 1; dx++ {
				if reference.pixel(x+dx, y+dy) != v {
					return 0, false
				}
			}
		}
		return v, true
	}

	contexts := make([]encContext, 1<<(len(refined)+len(ref)))
	e := newMQEncoder()
	var ltp uint32
	for y := range target.Height {
		if tpgron {
			// A row may be declared typical when every predictable pixel in it
			// has its predicted value.
			typical := uint32(1)
			for x := range target.Width {
				if v, ok := predicted(x, y); ok && target.pixel(x, y) != v {
					typical = 0
					break
				}
			}
			e.encode(&contexts[sltp], typical^ltp)
			ltp = typical
		}
		for x := range target.Width {
			if ltp == 1 {
				if _, ok := predicted(x, y); ok {
					continue
				}
			}
			e.encode(&contexts[gather(x, y)], target.pixel(x, y))
		}
	}
	return e.flush()
}

// refinementStream wraps a refinement in an embedded JBIG2 stream: a page the
// reference's size, an immediate generic region drawing the reference, then
// an immediate refinement region referring to no segment, which T.88 7.4.7.4
// refines against the page area it covers and 8.2 step 5 c draws with REPLACE.
func refinementStream(reference *Bitmap, template int, tpgron bool, at [2][2]int, coded []byte) []byte {
	w, h := reference.Width, reference.Height
	page := binary.BigEndian.AppendUint32(nil, uint32(w))
	page = binary.BigEndian.AppendUint32(page, uint32(h))
	page = append(page, make([]byte, 11)...)

	genericAT := encNominalAT[0]
	generic := binary.BigEndian.AppendUint32(nil, uint32(w))
	generic = binary.BigEndian.AppendUint32(generic, uint32(h))
	generic = append(generic, make([]byte, 9)...)
	generic = append(generic, 0)
	for _, a := range genericAT {
		generic = append(generic, byte(int8(a[0])), byte(int8(a[1])))
	}
	generic = append(generic, encodeGeneric(reference, 0, false, genericAT)...)

	region := binary.BigEndian.AppendUint32(nil, uint32(w))
	region = binary.BigEndian.AppendUint32(region, uint32(h))
	region = append(region, make([]byte, 8)...)
	region = append(region, combineReplace)
	flags := byte(template)
	if tpgron {
		flags |= 2
	}
	region = append(region, flags)
	if template == 0 {
		for _, a := range at {
			region = append(region, byte(int8(a[0])), byte(int8(a[1])))
		}
	}
	region = append(region, coded...)

	return concat(
		segmentBytes(0, typePageInformation, 1, page),
		segmentBytes(1, typeImmediateGeneric, 1, generic),
		segmentBytes(2, typeImmediateRefinement, 1, region),
	)
}

// dotsImage draws isolated dots three pixels apart. Around a lone dot the
// refinement template sees only the reference pixel under the one being coded,
// which is exactly the SLTP pattern of Figures 14 and 15, so a refinement of
// this image codes pixels in the SLTP context.
func dotsImage(width, height int) *Bitmap {
	bm, err := newBitmap(width, height)
	if err != nil {
		panic(err)
	}
	for y := 1; y < height; y += 3 {
		for x := 1 + y%2; x < width; x += 3 {
			bm.setPixel(x, y, 1)
		}
	}
	return bm
}

// refinedImage derives a refinement target from ref: most of it unchanged,
// so TPGR rows occur, with a band of flipped pixels, a filled block and a few
// dots removed.
func refinedImage(ref *Bitmap, seed uint64) *Bitmap {
	out, err := newBitmap(ref.Width, ref.Height)
	if err != nil {
		panic(err)
	}
	copy(out.Data, ref.Data)
	r := rand.New(rand.NewPCG(seed, ^seed))
	for y := ref.Height / 3; y < ref.Height/3+4 && y < ref.Height; y++ {
		for x := range ref.Width {
			if r.IntN(4) == 0 {
				out.setPixel(x, y, 1^out.pixel(x, y))
			}
		}
	}
	for y := 2 * ref.Height / 3; y < 2*ref.Height/3+6 && y < ref.Height; y++ {
		for x := 5; x < 20 && x < ref.Width; x++ {
			out.setPixel(x, y, 1)
		}
	}
	for range 20 {
		out.setPixel(r.IntN(ref.Width), r.IntN(ref.Height), 0)
	}
	return out
}

// encInstance is one symbol instance to code: a symbol and the (S, T)
// coordinates of its reference corner.
type encInstance struct {
	id   int
	s, t int64
}

// encAdvance returns how far T.88 6.4.5 moves CURS for a symbol before placing
// it (step 3 c vi) and after (step 3 c x), from its reference corner and the
// strip direction: the gap between instances is coded, not the distance
// between their corners.
func encAdvance(p textParams, w, h int64) (before, after int64) {
	right := p.refCorner == cornerTopRight || p.refCorner == cornerBottomRight
	bottom := p.refCorner == cornerBottomLeft || p.refCorner == cornerBottomRight
	if !p.transposed {
		if right {
			return w - 1, 0
		}
		return 0, w - 1
	}
	if bottom {
		return h - 1, 0
	}
	return 0, h - 1
}

// encodeText codes instances as a text region with arithmetic coding, T.88
// 6.4.5 run backwards: instances are grouped into strips of p.strips by T,
// ordered by S within a strip, and each strip ends with an OOB delta S.
func encodeText(p textParams, instances []encInstance) []byte {
	type strip struct {
		base int64
		list []encInstance
	}
	floorDiv := func(a, b int64) int64 {
		q := a / b
		if a%b != 0 && (a < 0) != (b < 0) {
			q--
		}
		return q
	}
	byBase := map[int64]*strip{}
	var strips []*strip
	for _, in := range instances {
		base := floorDiv(in.t, p.strips) * p.strips
		st, ok := byBase[base]
		if !ok {
			st = &strip{base: base}
			byBase[base] = st
			strips = append(strips, st)
		}
		st.list = append(st.list, in)
	}
	sort.Slice(strips, func(i, j int) bool { return strips[i].base < strips[j].base })

	e := newMQEncoder()
	var iadt, iafs, iads, iait encInt
	iaid := newEncIAID(symbolCodeLen(len(p.syms)))
	iadt.encode(e, 0, false)
	var stripT, firstS int64
	for _, st := range strips {
		sort.SliceStable(st.list, func(i, j int) bool { return st.list[i].s < st.list[j].s })
		iadt.encode(e, (st.base-stripT)/p.strips, false)
		stripT = st.base
		var curS int64
		for i, in := range st.list {
			sym := p.syms[in.id]
			before, after := encAdvance(p, int64(sym.Width), int64(sym.Height))
			start := in.s - before
			if i == 0 {
				iafs.encode(e, start-firstS, false)
				firstS = start
			} else {
				iads.encode(e, start-curS-p.dsOffset, false)
			}
			if p.strips > 1 {
				iait.encode(e, in.t-stripT, false)
			}
			iaid.encode(e, in.id)
			curS = in.s + after
		}
		iads.encode(e, 0, true)
	}
	return e.flush()
}

// symbolDictSegment codes syms, which must come grouped by height, as a
// symbol dictionary segment with arithmetic coding and direct bitmaps
// (SDREFAGG 0) over numIn input symbols, exporting exactly the new symbols
// (T.88 6.5.5 and 7.4.2). gb carries the generic contexts in and out, so two
// calls can share them the way E.3.8 lets dictionaries do.
func symbolDictSegment(number uint32, refs []uint32, syms []*Bitmap, numIn int, used, retain bool, gb []encContext) []byte {
	const template = 2
	at := encNominalAT[template]
	e := newMQEncoder()
	var iadh, iadw, iaex encInt
	height := 0
	for i := 0; i < len(syms); {
		h := syms[i].Height
		iadh.encode(e, int64(h-height), false)
		height = h
		width := 0
		for ; i < len(syms) && syms[i].Height == h; i++ {
			iadw.encode(e, int64(syms[i].Width-width), false)
			width = syms[i].Width
			encodeGenericWith(e, syms[i], template, false, at, gb)
		}
		iadw.encode(e, 0, true)
	}
	iaex.encode(e, int64(numIn), false)
	iaex.encode(e, int64(len(syms)), false)

	flags := uint16(template << 10)
	if used {
		flags |= 0x100
	}
	if retain {
		flags |= 0x200
	}
	data := binary.BigEndian.AppendUint16(nil, flags)
	for _, a := range at {
		data = append(data, byte(int8(a[0])), byte(int8(a[1])))
	}
	data = binary.BigEndian.AppendUint32(data, uint32(len(syms)))
	data = binary.BigEndian.AppendUint32(data, uint32(len(syms)))
	data = append(data, e.flush()...)

	h := binary.BigEndian.AppendUint32(nil, number)
	h = append(h, typeSymbolDictionary, byte(len(refs)<<5))
	for _, r := range refs {
		h = append(h, byte(r))
	}
	h = append(h, 1)
	h = binary.BigEndian.AppendUint32(h, uint32(len(data)))
	return concat(h, data)
}
