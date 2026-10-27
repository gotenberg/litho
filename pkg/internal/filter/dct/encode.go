package dct

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
)

// MaxDimension is the largest width or height [Encode] writes: the frame
// header of ITU-T T.81, B.2.2 holds each in 16 bits, and a height of zero
// would defer it to a DNL marker, which Encode does not write.
const MaxDimension = 65535

// maxRestartInterval is the largest restart interval the DRI segment holds
// (T.81, B.2.4.4: Ri is 16 bits).
const maxRestartInterval = 65535

// The coefficient ranges of baseline coding of 8-bit samples (T.81, F.1.2.1
// and F.1.2.2, Tables F.1 and F.2): a DC difference of category 11 at most,
// so a DC coefficient within 11 bits of range, and an AC coefficient of
// size 10 at most. A forward DCT of 8-bit samples stays inside them; the
// clamp keeps the rounding of a quantizer of 1 from stepping out.
const (
	minDC = -1024
	maxDC = 1023
	maxAC = 1023
)

// Subsampling selects the sampling of the chroma components of a
// three-component image [Encode] writes as YCbCr.
type Subsampling uint8

const (
	// Subsample444 samples every component at full resolution.
	Subsample444 Subsampling = iota
	// Subsample420 samples Cb and Cr at half the resolution in both
	// directions: the luminance component has sampling factors 2 x 2 and
	// the chroma ones 1 x 1 (T.81, A.1.1).
	Subsample420
)

// EncodeOptions configures [Encode].
type EncodeOptions struct {
	// Quality scales the quantization tables, 1 to 100: 50 takes the
	// tables of T.81, Annex K.1 as they are, lower values coarsen them and
	// higher ones refine them, 100 quantizing every coefficient by 1. The
	// scaling is the one the Independent JPEG Group's libjpeg defines
	// (jcparam.c, jpeg_quality_scaling), which most encoders follow, so a
	// quality means what it means elsewhere.
	Quality int
	// Subsampling is the chroma sampling of a three-component image. It is
	// ignored for one or four components, which are sampled in full.
	Subsampling Subsampling
	// OptimizeHuffman codes the image with Huffman tables built from its
	// own symbol counts (T.81, Annex K.2) instead of the example tables of
	// Annex K.3, which takes a second pass over the coefficients and
	// shrinks the output, typically by a few percent.
	OptimizeHuffman bool
	// RestartInterval is the number of MCUs between restart markers (T.81,
	// B.2.4.4 and E.1.4), 0 for none.
	RestartInterval int
}

// Encode writes samples as a baseline sequential DCT JPEG (ITU-T T.81,
// Annex F, the SOF0 process: 8-bit samples, Huffman coding, at most two
// tables of each kind), the data a DCTDecode filter decodes (ISO 32000-1,
// 7.4.8). samples holds rows of width pixels of comps components, one byte
// each: grey for one component, RGB for three and CMYK for four.
//
// Three components are written as YCbCr, converted from RGB by the equations
// of JFIF (ITU-T T.871, 7), with an Adobe APP14 marker of transform 1
// saying so (Adobe Technical Note #5116), which ISO 32000-1, 7.4.8 makes
// override /ColorTransform. Four components are written as they are, with
// an APP14 marker of transform 0, so no reader takes them for YCCK, and
// uninverted: a DCTDecode filter hands on the samples the data codes, and
// ISO 32000-1, 7.4.8 leaves the inversion some producers apply to /Decode,
// so data written uninverted reads as written under the default /Decode.
// One component carries no marker.
//
// The luminance table quantizes grey, CMYK and the Y component, the
// chrominance table Cb and Cr (T.81, Annex K.1), and each takes its own
// Huffman tables likewise. The output is a function of the input and the
// options alone.
func Encode(ctx context.Context, samples []byte, width, height, comps int, opts EncodeOptions) ([]byte, error) {
	if err := checkEncode(samples, width, height, comps, opts); err != nil {
		return nil, err
	}
	e := newEncoder(samples, width, height, comps, opts)
	if err := e.transform(ctx); err != nil {
		return nil, err
	}
	dc, ac := e.standardTables()
	if opts.OptimizeHuffman {
		var err error
		if dc, ac, err = e.optimalTables(ctx); err != nil {
			return nil, err
		}
	}
	return e.write(ctx, dc, ac)
}

// checkEncode refuses what [Encode] cannot write.
func checkEncode(samples []byte, width, height, comps int, opts EncodeOptions) error {
	if comps != 1 && comps != 3 && comps != 4 {
		return fmt.Errorf("%w: %d components, not 1, 3 or 4", ErrInvalidInput, comps)
	}
	if width < 1 || height < 1 || width > MaxDimension || height > MaxDimension {
		return fmt.Errorf("%w: %dx%d pixels, outside 1 to %d", ErrInvalidInput, width, height, MaxDimension)
	}
	if want := int64(width) * int64(height) * int64(comps); int64(len(samples)) != want {
		return fmt.Errorf("%w: %d bytes of samples for %dx%d pixels of %d components", ErrInvalidInput, len(samples), width, height, comps)
	}
	if opts.Quality < 1 || opts.Quality > 100 {
		return fmt.Errorf("%w: quality %d, outside 1 to 100", ErrInvalidInput, opts.Quality)
	}
	if opts.RestartInterval < 0 || opts.RestartInterval > maxRestartInterval {
		return fmt.Errorf("%w: restart interval %d, outside 0 to %d", ErrInvalidInput, opts.RestartInterval, maxRestartInterval)
	}
	if opts.Subsampling != Subsample444 && opts.Subsampling != Subsample420 {
		return fmt.Errorf("%w: subsampling %d", ErrInvalidInput, opts.Subsampling)
	}
	return nil
}

// encComp is one component of the frame [Encode] writes.
type encComp struct {
	h, v int
	// table is 0 for the luminance quantization and Huffman tables, 1 for
	// the chrominance ones.
	table int
	// plane holds the component's samples at its own resolution, cw x ch
	// (T.81, A.1.1).
	plane  []byte
	cw, ch int
	// bw and bh count the blocks a row and a column, padded to whole MCUs
	// in an interleaved scan, and coeffs holds bw*bh quantized blocks in
	// zig-zag order.
	bw, bh int
	coeffs []int16
}

// encoder holds the state of one [Encode] call.
type encoder struct {
	width, height int
	comps         []*encComp
	hmax, vmax    int
	mcusX, mcusY  int
	quant         [2][64]int
	restart       int
	// adobe reports whether an APP14 marker is written, and
	// adobeTransform its colour transform.
	adobe          bool
	adobeTransform byte
}

func newEncoder(samples []byte, width, height, n int, opts EncodeOptions) *encoder {
	e := &encoder{width: width, height: height, restart: opts.RestartInterval}
	e.quant[0] = scaledQuant(&lumaQuant, opts.Quality)
	e.quant[1] = scaledQuant(&chromaQuant, opts.Quality)
	planes := splitPlanes(samples, width*height, n)
	switch n {
	case 1:
		e.comps = []*encComp{{h: 1, v: 1, plane: planes[0], cw: width, ch: height}}
	case 3:
		e.adobe, e.adobeTransform = true, 1
		toYCbCr(planes)
		f := 1
		if opts.Subsampling == Subsample420 {
			f = 2
		}
		e.comps = []*encComp{{h: f, v: f, plane: planes[0], cw: width, ch: height}}
		for _, p := range planes[1:] {
			cw, ch := ceilDiv(width, f), ceilDiv(height, f)
			e.comps = append(e.comps, &encComp{h: 1, v: 1, table: 1, plane: downsample(p, width, height, f), cw: cw, ch: ch})
		}
	case 4:
		e.adobe, e.adobeTransform = true, 0
		for _, p := range planes {
			e.comps = append(e.comps, &encComp{h: 1, v: 1, plane: p, cw: width, ch: height})
		}
	}
	for _, c := range e.comps {
		e.hmax, e.vmax = max(e.hmax, c.h), max(e.vmax, c.v)
	}
	e.mcusX, e.mcusY = ceilDiv(width, 8*e.hmax), ceilDiv(height, 8*e.vmax)
	for _, c := range e.comps {
		if len(e.comps) == 1 {
			// A scan of one component is not interleaved: its MCU is one
			// block, and it covers only the blocks the samples reach
			// (T.81, A.2.2).
			c.bw, c.bh = ceilDiv(c.cw, 8), ceilDiv(c.ch, 8)
		} else {
			c.bw, c.bh = e.mcusX*c.h, e.mcusY*c.v
		}
	}
	return e
}

// scaledQuant scales a table of T.81, Annex K.1 to quality q as libjpeg
// does (jcparam.c, jpeg_quality_scaling and jpeg_add_quant_table): a
// percentage of 5000/q below 50 and 200-2q from 50, each entry rounded and
// held to 1..255, the range of an 8-bit quantizer (T.81, B.2.4.1).
func scaledQuant(base *[64]int, q int) [64]int {
	scale := 200 - 2*q
	if q < 50 {
		scale = 5000 / q
	}
	var out [64]int
	for i, b := range base {
		out[i] = min(max((b*scale+50)/100, 1), 255)
	}
	return out
}

// splitPlanes deinterleaves pixels of n components into one plane each.
func splitPlanes(samples []byte, pixels, n int) [][]byte {
	planes := make([][]byte, n)
	for c := range planes {
		planes[c] = make([]byte, pixels)
	}
	for i := range pixels {
		for c := range n {
			planes[c][i] = samples[i*n+c]
		}
	}
	return planes
}

// toYCbCr converts RGB planes to YCbCr in place (ITU-T T.871, 7).
func toYCbCr(p [][]byte) {
	clamp := func(v float64) byte { return byte(min(max(math.Round(v), 0), 255)) }
	for i := range p[0] {
		r, g, b := float64(p[0][i]), float64(p[1][i]), float64(p[2][i])
		p[0][i] = clamp(0.299*r + 0.587*g + 0.114*b)
		p[1][i] = clamp(-0.299/1.772*r - 0.587/1.772*g + 0.5*b + 128)
		p[2][i] = clamp(0.5*r - 0.587/1.402*g - 0.114/1.402*b + 128)
	}
}

// downsample averages f x f squares of a width x height plane, the squares
// at the right and bottom edges over the samples they hold.
func downsample(p []byte, width, height, f int) []byte {
	if f == 1 {
		return p
	}
	cw, ch := ceilDiv(width, f), ceilDiv(height, f)
	out := make([]byte, cw*ch)
	for y := range ch {
		for x := range cw {
			sum, n := 0, 0
			for dy := range f {
				for dx := range f {
					sx, sy := x*f+dx, y*f+dy
					if sx < width && sy < height {
						sum += int(p[sy*width+sx])
						n++
					}
				}
			}
			out[y*cw+x] = byte((sum + n/2) / n) //nolint:gosec // An average of bytes.
		}
	}
	return out
}

// transform computes the quantized coefficients of every block: the
// samples level shifted (T.81, A.3.1), the forward DCT (A.3.3) and the
// quantization (A.3.4). A block past the samples repeats the last column and
// row, which keeps the padding out of the visible edge's coefficients.
func (e *encoder) transform(ctx context.Context) error {
	var in [64]float64
	blocks := 0
	for _, c := range e.comps {
		div := fdctDivisors(&e.quant[c.table])
		c.coeffs = make([]int16, c.bw*c.bh*64)
		for by := range c.bh {
			for bx := range c.bw {
				if blocks++; blocks%ctxBlocks == 0 {
					if err := ctx.Err(); err != nil {
						return err
					}
				}
				for y := range 8 {
					sy := min(by*8+y, c.ch-1)
					row := c.plane[sy*c.cw : (sy+1)*c.cw]
					for x := range 8 {
						in[y*8+x] = float64(row[min(bx*8+x, c.cw-1)]) - 128
					}
				}
				fdct(&in, &div, c.coeffs[(by*c.bw+bx)*64:(by*c.bw+bx+1)*64])
			}
		}
		// The plane is not read again.
		c.plane = nil
	}
	return nil
}

// The factors of the Arai-Agui-Nakajima factorisation of the 8-point DCT
// (Arai, Agui and Nakajima, "A fast DCT-SQ scheme for images", Trans. IEICE
// E71(11), 1988), as the Independent JPEG Group's jfdctflt.c applies them:
// five multiplications a row, the output of each scaled by aanScale.
var (
	aanC4    = math.Cos(4 * math.Pi / 16)
	aanC6    = math.Cos(6 * math.Pi / 16)
	aanC2mC6 = math.Cos(2*math.Pi/16) - math.Cos(6*math.Pi/16)
	aanC2pC6 = math.Cos(2*math.Pi/16) + math.Cos(6*math.Pi/16)
	// aanScale holds the factor the factorisation leaves on output k, 1 for
	// k = 0 and sqrt(2) cos(k pi/16) otherwise.
	aanScale = func() (s [8]float64) {
		s[0] = 1
		for k := 1; k < 8; k++ {
			s[k] = math.Sqrt2 * math.Cos(float64(k)*math.Pi/16)
		}
		return s
	}()
)

// fdctDivisors returns what [fdct] divides each output of the
// factorisation by, in natural order: the quantizer of q (T.81, A.3.4) times
// the scale the factorisation leaves on the row and the column, times the 8
// of the two C(u)C(v)/4 normalisations it leaves out.
func fdctDivisors(q *[64]int) [64]float64 {
	var div [64]float64
	for i := range div {
		div[i] = float64(q[i]) * aanScale[i/8] * aanScale[i%8] * 8
	}
	return div
}

// fdct computes the forward DCT of one level-shifted block (T.81, A.3.3),
// S(v,u) = C(u)C(v)/4 sum s(y,x) cos((2x+1)u pi/16) cos((2y+1)v pi/16), whose
// factors are those of the inverse transform, and quantizes it by the
// quantizers div folds in (A.3.4, see [fdctDivisors]), writing the
// coefficients in zig-zag order (Figure A.6). It computes the transform
// through the Arai-Agui-Nakajima factorisation, in place, rows then columns:
// the same coefficients as the sums of the definition up to floating-point
// rounding, at a fraction of the multiplications.
func fdct(in *[64]float64, div *[64]float64, out []int16) {
	for r := range 8 {
		aan8(in[r*8:r*8+8], 1)
	}
	for c := range 8 {
		aan8(in[c:], 8)
	}
	for k := range 64 {
		i := zigzag[k]
		c := int(math.Round(in[i] / div[i]))
		if k == 0 {
			c = min(max(c, minDC), maxDC)
		} else {
			c = min(max(c, -maxAC), maxAC)
		}
		out[k] = int16(c) //nolint:gosec // Clamped to the 11-bit range of baseline coefficients.
	}
}

// aan8 transforms the eight values of d stride apart in place, each output k
// left scaled by aanScale[k] (jfdctflt.c's even and odd parts).
func aan8(d []float64, stride int) {
	_ = d[7*stride] // One bounds check for the eight accesses.
	d0, d1, d2, d3 := d[0], d[stride], d[2*stride], d[3*stride]
	d4, d5, d6, d7 := d[4*stride], d[5*stride], d[6*stride], d[7*stride]
	t0, t7 := d0+d7, d0-d7
	t1, t6 := d1+d6, d1-d6
	t2, t5 := d2+d5, d2-d5
	t3, t4 := d3+d4, d3-d4

	t10, t13 := t0+t3, t0-t3
	t11, t12 := t1+t2, t1-t2
	d[0] = t10 + t11
	d[4*stride] = t10 - t11
	z1 := (t12 + t13) * aanC4
	d[2*stride] = t13 + z1
	d[6*stride] = t13 - z1

	t10, t11, t12 = t4+t5, t5+t6, t6+t7
	z5 := (t10 - t12) * aanC6
	z2 := aanC2mC6*t10 + z5
	z4 := aanC2pC6*t12 + z5
	z3 := t11 * aanC4
	z11, z13 := t7+z3, t7-z3
	d[5*stride] = z13 + z2
	d[3*stride] = z13 - z2
	d[stride] = z11 + z4
	d[7*stride] = z11 - z4
}

// symbolSink takes the symbols and extra bits a scan codes, in order.
type symbolSink interface {
	// symbol takes one Huffman-coded value of class 0 (DC) or 1 (AC) in
	// table slot.
	symbol(class, slot int, sym byte)
	// bits takes the n low bits of v, written as they are.
	bits(v uint32, n int)
	// restartMarker takes the restart marker that ends an interval.
	restartMarker(marker byte)
}

// scan codes every MCU of the one scan [Encode] writes into sink, in the
// order of T.81, A.2: block by block for one component, else MCU by MCU,
// each MCU holding the h x v blocks of each component in turn.
func (e *encoder) scan(ctx context.Context, sink symbolSink) error {
	preds := make([]int, len(e.comps))
	mcu, rst := 0, 0
	startMCU := func() error {
		if mcu%ctxBlocks == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		if e.restart > 0 && mcu > 0 && mcu%e.restart == 0 {
			// Each interval starts its DC predictions from zero (F.1.1.5.1).
			sink.restartMarker(0xD0 + byte(rst%8)) //nolint:gosec // rst%8 is below 8.
			rst++
			clear(preds)
		}
		mcu++
		return nil
	}
	if len(e.comps) == 1 {
		c := e.comps[0]
		for b := range c.bw * c.bh {
			if err := startMCU(); err != nil {
				return err
			}
			codeBlock(sink, c.coeffs[b*64:(b+1)*64], &preds[0], c.table)
		}
		return nil
	}
	for my := range e.mcusY {
		for mx := range e.mcusX {
			if err := startMCU(); err != nil {
				return err
			}
			for ci, c := range e.comps {
				for v := range c.v {
					for h := range c.h {
						b := (my*c.v+v)*c.bw + mx*c.h + h
						codeBlock(sink, c.coeffs[b*64:(b+1)*64], &preds[ci], c.table)
					}
				}
			}
		}
	}
	return nil
}

// codeBlock codes one block's coefficients, in zig-zag order (T.81, F.1.2):
// the DC difference from the previous block of the component as a category
// and its extra bits, then the AC coefficients as runs of zeros and sizes,
// 0xF0 for sixteen zeros and 0x00 to end the block early.
func codeBlock(sink symbolSink, coef []int16, pred *int, slot int) {
	diff := int(coef[0]) - *pred
	*pred = int(coef[0])
	s := magnitude(diff)
	sink.symbol(0, slot, byte(s)) //nolint:gosec // A category of at most 11.
	if s > 0 {
		sink.bits(extraBits(diff, s), s)
	}
	run := 0
	for k := 1; k < 64; k++ {
		v := int(coef[k])
		if v == 0 {
			run++
			continue
		}
		for ; run > 15; run -= 16 {
			sink.symbol(1, slot, 0xF0)
		}
		s := magnitude(v)
		sink.symbol(1, slot, byte(run<<4|s)) //nolint:gosec // A run below 16 and a size of at most 10.
		sink.bits(extraBits(v, s), s)
		run = 0
	}
	if run > 0 {
		sink.symbol(1, slot, 0x00)
	}
}

// magnitude returns the category of v, the bits of its magnitude (T.81,
// Tables F.1 and F.2).
func magnitude(v int) int {
	if v < 0 {
		v = -v
	}
	n := 0
	for ; v > 0; v >>= 1 {
		n++
	}
	return n
}

// extraBits returns the s bits that follow the category of v: v itself
// when positive, v - 1 in s bits when negative, the one's complement of its
// magnitude (T.81, F.1.2.1).
func extraBits(v, s int) uint32 {
	if v < 0 {
		v += 1<<s - 1
	}
	return uint32(v) //nolint:gosec // Non-negative and below 1 << s.
}

// standardTables returns the Huffman tables of T.81, Annex K.3, by slot:
// luminance in 0, chrominance in 1.
func (e *encoder) standardTables() (dc, ac [2]*codeTable) {
	dc[0], ac[0] = stdLumaDC.table(), stdLumaAC.table()
	if len(e.comps) == 3 {
		dc[1], ac[1] = stdChromaDC.table(), stdChromaAC.table()
	}
	return dc, ac
}

// symbolCounter counts the symbols of a scan per class and slot.
type symbolCounter struct {
	freq [2][2][256]int64
}

func (s *symbolCounter) symbol(class, slot int, sym byte) { s.freq[class][slot][sym]++ }

func (*symbolCounter) bits(uint32, int) {}

func (*symbolCounter) restartMarker(byte) {}

// optimalTables builds Huffman tables from the image's own symbol counts
// (T.81, Annex K.2), by slot.
func (e *encoder) optimalTables(ctx context.Context) (dc, ac [2]*codeTable, _ error) {
	var counter symbolCounter
	if err := e.scan(ctx, &counter); err != nil {
		return dc, ac, err
	}
	for slot := range dc {
		if slot == 1 && len(e.comps) != 3 {
			// Only the chroma of a YCbCr image takes the second slot.
			break
		}
		dcTable, err := optimalTable(&counter.freq[0][slot])
		if err != nil {
			return dc, ac, fmt.Errorf("building a DC table: %w", err)
		}
		acTable, err := optimalTable(&counter.freq[1][slot])
		if err != nil {
			return dc, ac, fmt.Errorf("building an AC table: %w", err)
		}
		dc[slot], ac[slot] = dcTable, acTable
	}
	return dc, ac, nil
}

// scanWriter writes the symbols of a scan with its tables.
type scanWriter struct {
	bitWriter
	dc, ac [2]*codeTable
}

func (w *scanWriter) symbol(class, slot int, sym byte) {
	t := w.dc[slot]
	if class == 1 {
		t = w.ac[slot]
	}
	w.put(uint32(t.code[sym]), t.size[sym])
}

func (w *scanWriter) bits(v uint32, n int) { w.put(v, n) }

func (w *scanWriter) restartMarker(marker byte) {
	w.flush()
	w.out = append(w.out, 0xFF, marker)
}

// write assembles the JPEG: SOI, the APP14 marker, the quantization tables,
// the frame header, the Huffman tables, the restart interval, the scan and
// EOI (T.81, B.2).
func (e *encoder) write(ctx context.Context, dc, ac [2]*codeTable) ([]byte, error) {
	out := []byte{0xFF, 0xD8}
	if e.adobe {
		// Adobe Technical Note #5116: "Adobe", version 100, two flag
		// words, and the colour transform.
		out = append(out, 0xFF, 0xEE, 0x00, 0x0E, 'A', 'd', 'o', 'b', 'e', 0x00, 0x64, 0x00, 0x00, 0x00, 0x00, e.adobeTransform)
	}
	tables := 1
	if len(e.comps) == 3 {
		tables = 2
	}
	out = appendSegment(out, 0xDB, func(b []byte) []byte {
		for t := range tables {
			b = append(b, byte(t)) // 8-bit precision, destination t.
			for k := range 64 {
				b = append(b, byte(e.quant[t][zigzag[k]])) //nolint:gosec // Held to 1..255 by scaledQuant.
			}
		}
		return b
	})
	out = appendSegment(out, 0xC0, func(b []byte) []byte {
		b = append(b, 8)
		b = binary.BigEndian.AppendUint16(b, uint16(e.height)) //nolint:gosec // At most MaxDimension.
		b = binary.BigEndian.AppendUint16(b, uint16(e.width))  //nolint:gosec // At most MaxDimension.
		b = append(b, byte(len(e.comps)))                      //nolint:gosec // At most 4 components.
		for i, c := range e.comps {
			b = append(b, byte(i+1), byte(c.h<<4|c.v), byte(c.table)) //nolint:gosec // Small identifiers and factors.
		}
		return b
	})
	out = appendSegment(out, 0xC4, func(b []byte) []byte {
		for class, set := range [2][2]*codeTable{dc, ac} {
			for slot, t := range set {
				if t == nil {
					continue
				}
				b = append(b, byte(class<<4|slot)) //nolint:gosec // Class and slot are below 2.
				b = append(b, t.counts[:]...)
				b = append(b, t.values...)
			}
		}
		return b
	})
	if e.restart > 0 {
		out = appendSegment(out, 0xDD, func(b []byte) []byte {
			return binary.BigEndian.AppendUint16(b, uint16(e.restart)) //nolint:gosec // At most maxRestartInterval.
		})
	}
	out = appendSegment(out, 0xDA, func(b []byte) []byte {
		b = append(b, byte(len(e.comps))) //nolint:gosec // At most 4 components.
		for i, c := range e.comps {
			b = append(b, byte(i+1), byte(c.table<<4|c.table)) //nolint:gosec // Small identifiers and slots.
		}
		// Ss 0, Se 63, Ah and Al 0: the whole block in one scan (B.2.3).
		return append(b, 0, 63, 0)
	})
	w := &scanWriter{out: out, dc: dc, ac: ac}
	if err := e.scan(ctx, w); err != nil {
		return nil, err
	}
	w.flush()
	return append(w.out, 0xFF, 0xD9), nil
}

// appendSegment appends a marker segment: the marker, its length, which
// counts itself, and the body the callback appends.
func appendSegment(out []byte, marker byte, body func([]byte) []byte) []byte {
	start := len(out)
	out = append(out, 0xFF, marker, 0, 0)
	out = body(out)
	binary.BigEndian.PutUint16(out[start+2:], uint16(len(out)-start-2)) //nolint:gosec // Segments of a few kilobytes.
	return out
}
