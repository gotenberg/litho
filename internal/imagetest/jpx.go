package imagetest

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"math/bits"
)

// mqState is one row of the MQ coder's probability estimation table
// (ITU-T T.800, Table C.2).
type mqState struct {
	qe         uint32
	nmps, nlps uint8
	switchMPS  bool
}

var mqTable = [47]mqState{
	{0x5601, 1, 1, true},
	{0x3401, 2, 6, false},
	{0x1801, 3, 9, false},
	{0x0AC1, 4, 12, false},
	{0x0521, 5, 29, false},
	{0x0221, 38, 33, false},
	{0x5601, 7, 6, true},
	{0x5401, 8, 14, false},
	{0x4801, 9, 14, false},
	{0x3801, 10, 14, false},
	{0x3001, 11, 17, false},
	{0x2401, 12, 18, false},
	{0x1C01, 13, 20, false},
	{0x1601, 29, 21, false},
	{0x5601, 15, 14, true},
	{0x5401, 16, 14, false},
	{0x5101, 17, 15, false},
	{0x4801, 18, 16, false},
	{0x3801, 19, 17, false},
	{0x3401, 20, 18, false},
	{0x3001, 21, 19, false},
	{0x2801, 22, 19, false},
	{0x2401, 23, 20, false},
	{0x2201, 24, 21, false},
	{0x1C01, 25, 22, false},
	{0x1801, 26, 23, false},
	{0x1601, 27, 24, false},
	{0x1401, 28, 25, false},
	{0x1201, 29, 26, false},
	{0x1101, 30, 27, false},
	{0x0AC1, 31, 28, false},
	{0x09C1, 32, 29, false},
	{0x08A1, 33, 30, false},
	{0x0521, 34, 31, false},
	{0x0441, 35, 32, false},
	{0x02A1, 36, 33, false},
	{0x0221, 37, 34, false},
	{0x0141, 38, 35, false},
	{0x0111, 39, 36, false},
	{0x0085, 40, 37, false},
	{0x0049, 41, 38, false},
	{0x0025, 42, 39, false},
	{0x0015, 43, 40, false},
	{0x0009, 44, 41, false},
	{0x0005, 45, 42, false},
	{0x0001, 45, 43, false},
	{0x5601, 46, 46, false},
}

// mqContext is a context's state index and MPS.
type mqContext struct {
	index, mps uint8
}

// mqEncoder is the MQ arithmetic encoder (T.800, C.2).
type mqEncoder struct {
	a, c uint32
	ct   int
	// out holds the bytes written, the first a placeholder before the
	// codeword (BP starts one byte before it, C.2.8).
	out []byte
}

func newMQEncoder() *mqEncoder {
	return &mqEncoder{a: 0x8000, ct: 12, out: []byte{0}}
}

func (e *mqEncoder) encode(cx *mqContext, d int) {
	s := &mqTable[cx.index]
	if d == int(cx.mps) {
		e.a -= s.qe
		if e.a&0x8000 == 0 {
			if e.a < s.qe {
				e.a = s.qe
			} else {
				e.c += s.qe
			}
			cx.index = s.nmps
			e.renorm()
		} else {
			e.c += s.qe
		}
		return
	}
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
	e.renorm()
}

func (e *mqEncoder) renorm() {
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

func (e *mqEncoder) byteOut() {
	last := len(e.out) - 1
	if e.out[last] == 0xFF {
		e.out = append(e.out, byte(e.c>>20)) //nolint:gosec // G115: C holds the byte out in these bits (T.800, C.2.5)
		e.c &= 0xFFFFF
		e.ct = 7
		return
	}
	if e.c < 0x8000000 {
		e.out = append(e.out, byte(e.c>>19)) //nolint:gosec // G115: C holds the byte out in these bits (T.800, C.2.5)
		e.c &= 0x7FFFF
		e.ct = 8
		return
	}
	e.out[last]++
	if e.out[last] == 0xFF {
		e.c &= 0x7FFFFFF
		e.out = append(e.out, byte(e.c>>20)) //nolint:gosec // G115: C holds the byte out in these bits (T.800, C.2.5)
		e.c &= 0xFFFFF
		e.ct = 7
		return
	}
	e.out = append(e.out, byte(e.c>>19)) //nolint:gosec // G115: C holds the byte out in these bits (T.800, C.2.5)
	e.c &= 0x7FFFF
	e.ct = 8
}

// flush ends the codeword (T.800, C.2.9) and returns it, a final 0xFF
// discarded.
func (e *mqEncoder) flush() []byte {
	temp := e.c + e.a
	e.c |= 0xFFFF
	if e.c >= temp {
		e.c -= 0x8000
	}
	e.c <<= uint(e.ct)
	e.byteOut()
	e.c <<= uint(e.ct)
	e.byteOut()
	out := e.out[1:]
	if n := len(out); n > 0 && out[n-1] == 0xFF {
		out = out[:n-1]
	}
	return out
}

// The coefficient flags of the tier-1 coder, as the decoder lays them out.
const (
	jSigN = 1 << iota
	jSigS
	jSigW
	jSigE
	jSigNW
	jSigNE
	jSigSW
	jSigSE
	jNegN
	jNegS
	jNegW
	jNegE
	jSig
	jNeg
	jVisited
	jRefined

	jNeighbours = jSigN | jSigS | jSigW | jSigE | jSigNW | jSigNE | jSigSW | jSigSE
)

// Context labels (T.800, Annex D): 0 to 8 significance, 9 to 13 sign, 14
// to 16 refinement, 17 run-length, 18 uniform.
const (
	jCtxRunLength = 17
	jCtxUniform   = 18
)

// jSigLabel is the LL column of T.800, Table D.1.
func jSigLabel(f int) int {
	h := f>>2&1 + f>>3&1
	v := f&1 + f>>1&1
	d := f>>4&1 + f>>5&1 + f>>6&1 + f>>7&1
	switch {
	case h == 2:
		return 8
	case h == 1 && v >= 1:
		return 7
	case h == 1 && d >= 1:
		return 6
	case h == 1:
		return 5
	case v == 2:
		return 4
	case v == 1:
		return 3
	case d >= 2:
		return 2
	case d == 1:
		return 1
	}
	return 0
}

// jSignContext returns the sign context and the bit the sign is XORed
// with for the flags f (T.800, Tables D.2 and D.3).
func jSignContext(f uint32) (int, int) {
	contribution := func(sig0, neg0, sig1, neg1 uint32) int {
		c := 0
		if sig0 == 1 {
			c += 1 - 2*int(neg0)
		}
		if sig1 == 1 {
			c += 1 - 2*int(neg1)
		}
		return max(-1, min(1, c))
	}
	sN, sS, sW, sE := f&jSigN != 0, f&jSigS != 0, f&jSigW != 0, f&jSigE != 0
	nN, nS, nW, nE := f&jNegN != 0, f&jNegS != 0, f&jNegW != 0, f&jNegE != 0
	b := func(x bool) uint32 {
		if x {
			return 1
		}
		return 0
	}
	vc := contribution(b(sN), b(nN), b(sS), b(nS))
	hc := contribution(b(sW), b(nW), b(sE), b(nE))
	switch {
	case hc == 1:
		return 12 + vc, 0
	case hc == 0 && vc == 0:
		return 9, 0
	case hc == 0:
		if vc < 0 {
			return 10, 1
		}
		return 10, 0
	}
	return 12 - vc, 1
}

// t1Encoder codes one code-block's coefficients (T.800, Annex D), with
// the default code-block style: one codeword for every pass.
type t1Encoder struct {
	w, h, stride int
	flags        []uint32
	mag          []int
	mq           *mqEncoder
	cx           [19]mqContext
}

func (t *t1Encoder) becomeSignificant(i int, negative bool) {
	s := t.stride
	f := t.flags
	f[i] |= jSig
	if negative {
		f[i] |= jNeg
		f[i-s] |= jNegS
		f[i+s] |= jNegN
		f[i-1] |= jNegE
		f[i+1] |= jNegW
	}
	f[i-s-1] |= jSigSE
	f[i-s] |= jSigS
	f[i-s+1] |= jSigSW
	f[i-1] |= jSigE
	f[i+1] |= jSigW
	f[i+s-1] |= jSigNE
	f[i+s] |= jSigN
	f[i+s+1] |= jSigNW
}

func (t *t1Encoder) sign(f uint32, negative bool) {
	label, xor := jSignContext(f)
	d := 0
	if negative {
		d = 1
	}
	t.mq.encode(&t.cx[label], d^xor)
}

func (t *t1Encoder) significance(plane int, neg []bool) {
	for y0 := 0; y0 < t.h; y0 += 4 {
		for x := range t.w {
			for y := y0; y < min(y0+4, t.h); y++ {
				i := (y+1)*t.stride + x + 1
				f := t.flags[i]
				if f&jSig != 0 || f&jNeighbours == 0 {
					continue
				}
				t.flags[i] |= jVisited
				j := y*t.w + x
				bit := t.mag[j] >> plane & 1
				t.mq.encode(&t.cx[jSigLabel(int(f&jNeighbours))], bit)
				if bit == 1 {
					t.sign(f, neg[j])
					t.becomeSignificant(i, neg[j])
				}
			}
		}
	}
}

func (t *t1Encoder) refinement(plane int) {
	for y0 := 0; y0 < t.h; y0 += 4 {
		for x := range t.w {
			for y := y0; y < min(y0+4, t.h); y++ {
				i := (y+1)*t.stride + x + 1
				f := t.flags[i]
				if f&jSig == 0 || f&jVisited != 0 {
					continue
				}
				label := 16
				if f&jRefined == 0 {
					label = 14
					if f&jNeighbours != 0 {
						label = 15
					}
				}
				t.mq.encode(&t.cx[label], t.mag[y*t.w+x]>>plane&1)
				t.flags[i] |= jRefined
			}
		}
	}
}

func (t *t1Encoder) cleanup(plane int, neg []bool) {
	for y0 := 0; y0 < t.h; y0 += 4 {
		for x := range t.w {
			start := y0
			if y0+4 <= t.h && t.runnable(x, y0) {
				first := -1
				for y := y0; y < y0+4; y++ {
					if t.mag[y*t.w+x]>>plane&1 == 1 {
						first = y
						break
					}
				}
				if first < 0 {
					t.mq.encode(&t.cx[jCtxRunLength], 0)
					continue
				}
				t.mq.encode(&t.cx[jCtxRunLength], 1)
				r := first - y0
				t.mq.encode(&t.cx[jCtxUniform], r>>1)
				t.mq.encode(&t.cx[jCtxUniform], r&1)
				i := (first+1)*t.stride + x + 1
				j := first*t.w + x
				t.sign(t.flags[i], neg[j])
				t.becomeSignificant(i, neg[j])
				start = first + 1
			}
			for y := start; y < min(y0+4, t.h); y++ {
				i := (y+1)*t.stride + x + 1
				f := t.flags[i]
				if f&(jSig|jVisited) != 0 {
					continue
				}
				j := y*t.w + x
				bit := t.mag[j] >> plane & 1
				t.mq.encode(&t.cx[jSigLabel(int(f&jNeighbours))], bit)
				if bit == 1 {
					t.sign(f, neg[j])
					t.becomeSignificant(i, neg[j])
				}
			}
		}
	}
	for i := range t.flags {
		t.flags[i] &^= jVisited
	}
}

func (t *t1Encoder) runnable(x, y0 int) bool {
	for y := y0; y < y0+4; y++ {
		if t.flags[(y+1)*t.stride+x+1]&(jSig|jVisited|jNeighbours) != 0 {
			return false
		}
	}
	return true
}

// codeBlock codes the w by h coefficients q of one code-block and returns
// its codeword, its count of coded bit-planes and of passes.
func codeBlock(q []int, w, h int) ([]byte, int, int) {
	mag := make([]int, len(q))
	neg := make([]bool, len(q))
	top := 0
	for i, v := range q {
		if v < 0 {
			mag[i], neg[i] = -v, true
		} else {
			mag[i] = v
		}
		top = max(top, mag[i])
	}
	coded := bits.Len(uint(top))
	if coded == 0 {
		return nil, 0, 0
	}
	t := &t1Encoder{w: w, h: h, stride: w + 2, flags: make([]uint32, (w+2)*(h+2)), mag: mag, mq: newMQEncoder()}
	t.cx[0] = mqContext{index: 4}
	t.cx[jCtxRunLength] = mqContext{index: 3}
	t.cx[jCtxUniform] = mqContext{index: 46}
	for plane := coded - 1; plane >= 0; plane-- {
		if plane < coded-1 {
			t.significance(plane, neg)
			t.refinement(plane)
		}
		t.cleanup(plane, neg)
	}
	return t.mq.flush(), coded, 3*coded - 2
}

// headerWriter writes a packet header, a zero bit stuffed after each 0xFF
// byte (T.800, B.10.1).
type headerWriter struct {
	out    []byte
	cur    byte
	n      int
	lastFF bool
}

func (w *headerWriter) bit(b int) {
	limit := 8
	if w.lastFF {
		limit = 7
	}
	w.cur = w.cur<<1 | byte(b) //nolint:gosec // G115: b is a bit
	w.n++
	if w.n == limit {
		w.out = append(w.out, w.cur)
		w.lastFF = w.cur == 0xFF
		w.cur, w.n = 0, 0
	}
}

func (w *headerWriter) bits(v, n int) {
	for i := n - 1; i >= 0; i-- {
		w.bit(v >> uint(i) & 1)
	}
}

// finish pads the last byte with zeros, and follows a final 0xFF with a
// byte of the zero bit the reader expects after it.
func (w *headerWriter) finish() []byte {
	for w.n != 0 {
		w.bit(0)
	}
	if w.lastFF {
		w.out = append(w.out, 0)
	}
	return w.out
}

// tagNode is one node of a tag tree an encoder codes.
type tagNode struct {
	value, low int
	known      bool
}

// tagEncoder codes a tag tree (T.800, B.10.2) as the decoder reads it.
type tagEncoder struct {
	levels [][]tagNode
	widths []int
}

func newTagEncoder(values []int, w, h int) *tagEncoder {
	t := &tagEncoder{}
	level := make([]tagNode, len(values))
	for i, v := range values {
		level[i].value = v
	}
	for {
		t.levels = append(t.levels, level)
		t.widths = append(t.widths, w)
		if w == 1 && h == 1 {
			return t
		}
		pw, ph := (w+1)/2, (h+1)/2
		parent := make([]tagNode, pw*ph)
		for i := range parent {
			parent[i].value = 1 << 30
		}
		for y := range h {
			for x := range w {
				p := &parent[(y/2)*pw+x/2]
				p.value = min(p.value, level[y*w+x].value)
			}
		}
		level, w, h = parent, pw, ph
	}
}

func (t *tagEncoder) encode(hw *headerWriter, x, y, threshold int) {
	low := 0
	for l := len(t.levels) - 1; l >= 0; l-- {
		n := &t.levels[l][(y>>l)*t.widths[l]+x>>l]
		if n.low < low {
			n.low = low
		}
		for !n.known && n.low < threshold {
			if n.low < n.value {
				hw.bit(0)
				n.low++
			} else {
				hw.bit(1)
				n.known = true
			}
		}
		low = n.low
		if !n.known || low >= threshold {
			return
		}
	}
}

// JPXOptions is how [JPX] writes a file.
type JPXOptions struct {
	// Codestream writes the bare codestream, without the JP2 boxes.
	Codestream bool
}

// jpxBlock is the code-block side, 64 samples (T.800, A.6.1: xcb and ycb
// of 6).
const jpxBlock = 64

// JPX returns img as a lossless JPEG 2000 file (ITU-T T.800): a JP2 file
// of one codestream, its colours sRGB or greyscale by enumeration, of one
// 8-bit component for a gray picture and three otherwise; one tile, no
// decomposition level, no component transform, 64 by 64 code-blocks, one
// quality layer.
func JPX(img image.Image, o JPXOptions) []byte {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	_, gray := img.(*image.Gray)
	nc := 3
	if gray {
		nc = 1
	}
	planes := make([][]int, nc)
	for c := range nc {
		planes[c] = make([]int, w*h)
	}
	for y := range h {
		for x := range w {
			c := color.NRGBAModel.Convert(img.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA) //nolint:forcetypeassert // NRGBAModel returns color.NRGBA
			if gray {
				planes[0][y*w+x] = int(c.R) - 128
				continue
			}
			planes[0][y*w+x], planes[1][y*w+x], planes[2][y*w+x] = int(c.R)-128, int(c.G)-128, int(c.B)-128
		}
	}
	bw, bh := (w+jpxBlock-1)/jpxBlock, (h+jpxBlock-1)/jpxBlock
	var tile bytes.Buffer
	for c := range nc {
		type block struct {
			data          []byte
			coded, passes int
		}
		blocks := make([]block, bw*bh)
		zero := make([]int, bw*bh)
		incl := make([]int, bw*bh)
		const mb = 8 // Mb: one guard bit and an exponent of 8 (T.800, E-2)
		for by := range bh {
			for bx := range bw {
				x0, y0 := bx*jpxBlock, by*jpxBlock
				cw, ch := min(jpxBlock, w-x0), min(jpxBlock, h-y0)
				q := make([]int, cw*ch)
				for y := range ch {
					copy(q[y*cw:(y+1)*cw], planes[c][(y0+y)*w+x0:(y0+y)*w+x0+cw])
				}
				data, coded, passes := codeBlock(q, cw, ch)
				i := by*bw + bx
				blocks[i] = block{data, coded, passes}
				zero[i] = mb - coded
				if coded == 0 {
					incl[i], zero[i] = 1, 0
				}
			}
		}
		inclusion := newTagEncoder(incl, bw, bh)
		zeroPlanes := newTagEncoder(zero, bw, bh)
		hw := &headerWriter{}
		hw.bit(1)
		var body bytes.Buffer
		for i, blk := range blocks {
			x, y := i%bw, i/bw
			inclusion.encode(hw, x, y, 1)
			if blk.coded == 0 {
				continue
			}
			zeroPlanes.encode(hw, x, y, 1<<30)
			switch n := blk.passes; {
			case n == 1:
				hw.bit(0)
			case n == 2:
				hw.bits(2, 2)
			case n <= 5:
				hw.bits(3, 2)
				hw.bits(n-3, 2)
			case n <= 36:
				hw.bits(0xF, 4)
				hw.bits(n-6, 5)
			default:
				hw.bits(0x1FF, 9)
				hw.bits(n-37, 7)
			}
			lblock := 3
			width := lblock + bits.Len(uint(blk.passes)) - 1
			for len(blk.data) >= 1<<width {
				hw.bit(1)
				width++
			}
			hw.bit(0)
			hw.bits(len(blk.data), width)
			body.Write(blk.data)
		}
		tile.Write(hw.finish())
		tile.Write(body.Bytes())
	}

	var cs bytes.Buffer
	cs.Write([]byte{0xFF, 0x4F})
	siz := []byte{0, 0}
	siz = binary.BigEndian.AppendUint32(siz, uint32(w)) //nolint:gosec // G115: a test picture's size
	siz = binary.BigEndian.AppendUint32(siz, uint32(h)) //nolint:gosec // G115: as above
	siz = append(siz, 0, 0, 0, 0, 0, 0, 0, 0)
	siz = binary.BigEndian.AppendUint32(siz, uint32(w)) //nolint:gosec // G115: as above
	siz = binary.BigEndian.AppendUint32(siz, uint32(h)) //nolint:gosec // G115: as above
	siz = append(siz, 0, 0, 0, 0, 0, 0, 0, 0)
	siz = binary.BigEndian.AppendUint16(siz, uint16(nc)) //nolint:gosec // G115: 1 or 3
	for range nc {
		siz = append(siz, 7, 1, 1)
	}
	jpxSegment(&cs, 0x51, siz)
	// COD: LRCP, one layer, no component transform, no decomposition,
	// 64 by 64 code-blocks, the default style, the 5-3 reversible filter.
	jpxSegment(&cs, 0x52, []byte{0, 0, 0, 1, 0, 0, 4, 4, 0, 1})
	// QCD: one guard bit, no quantization, an exponent of 8 for LL.
	jpxSegment(&cs, 0x5C, []byte{0x20, 8 << 3})
	sot := []byte{0, 0}
	sot = binary.BigEndian.AppendUint32(sot, uint32(12+2+tile.Len())) //nolint:gosec // G115: a test tile
	sot = append(sot, 0, 1)
	jpxSegment(&cs, 0x90, sot)
	cs.Write([]byte{0xFF, 0x93})
	cs.Write(tile.Bytes())
	cs.Write([]byte{0xFF, 0xD9})
	if o.Codestream {
		return cs.Bytes()
	}

	var f bytes.Buffer
	f.Write([]byte{0, 0, 0, 0x0C, 'j', 'P', ' ', ' ', 0x0D, 0x0A, 0x87, 0x0A})
	box(&f, "ftyp", []byte("jp2 \x00\x00\x00\x00jp2 "))
	ihdr := binary.BigEndian.AppendUint32(nil, uint32(h))                      //nolint:gosec // G115: as above
	ihdr = binary.BigEndian.AppendUint32(ihdr, uint32(w))                      //nolint:gosec // G115: as above
	ihdr = append(binary.BigEndian.AppendUint16(ihdr, uint16(nc)), 7, 7, 0, 0) //nolint:gosec // G115: 1 or 3
	enum := uint32(16)
	if gray {
		enum = 17
	}
	colr := binary.BigEndian.AppendUint32([]byte{1, 0, 0}, enum)
	var head bytes.Buffer
	box(&head, "ihdr", ihdr)
	box(&head, "colr", colr)
	box(&f, "jp2h", head.Bytes())
	box(&f, "jp2c", cs.Bytes())
	return f.Bytes()
}

// jpxSegment writes a codestream marker segment.
func jpxSegment(out *bytes.Buffer, marker byte, body []byte) {
	out.Write([]byte{0xFF, marker})
	out.Write(binary.BigEndian.AppendUint16(nil, uint16(len(body)+2))) //nolint:gosec // G115: a small segment
	out.Write(body)
}

// box writes a JP2 box (T.800, I.4).
func box(out *bytes.Buffer, kind string, body []byte) {
	out.Write(binary.BigEndian.AppendUint32(nil, uint32(8+len(body)))) //nolint:gosec // G115: a test box
	out.WriteString(kind)
	out.Write(body)
}
