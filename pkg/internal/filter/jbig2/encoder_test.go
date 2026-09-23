package jbig2

import (
	"encoding/binary"
	"math/rand/v2"
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
	offsets := append(append([][2]int{}, encTemplates[template]...), at...)
	contexts := make([]encContext, 1<<len(offsets))
	sltp := encGather(encSLTP[template])
	e := newMQEncoder()
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
	return e.flush()
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
