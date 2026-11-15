package imagetest

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
)

// JPEGOptions is how [JPEG] writes a file.
type JPEGOptions struct {
	// Progressive writes the progressive process with spectral selection
	// (ITU-T T.81, G.1.1): a DC scan of every component, then one AC scan
	// a component. Otherwise one baseline scan (T.81, 4.10).
	Progressive bool
	// Components is 1 (gray), 3 (YCbCr from RGB) or 4 (CMYK, written as
	// Adobe writes it: inverted, under an APP14 Adobe segment of transform
	// 0, Adobe Technical Note 5116, 18).
	Components int
	// JFIF writes a JFIF APP0 segment of this density in pixels an inch;
	// 0 writes none.
	JFIFDPI int
	// EXIF is the TIFF structure of an APP1 EXIF segment, nil for none
	// (see [EXIF]).
	EXIF []byte
	// ICC is a profile written in APP2 segments, nil for none.
	ICC []byte
}

// jpegQuant is the one quantization table every component uses: 4 for
// every coefficient, which keeps the picture close to its samples and
// every coefficient within the categories of the Annex K tables.
const jpegQuant = 4

// fdctTable holds round(8192 * C(u) * cos((2x + 1) u pi / 16)), C(0) =
// 1/sqrt(2), C(u) = 1 otherwise (ITU-T T.81, A.3.3).
var fdctTable = [8][8]int64{
	{5793, 5793, 5793, 5793, 5793, 5793, 5793, 5793},
	{8035, 6811, 4551, 1598, -1598, -4551, -6811, -8035},
	{7568, 3135, -3135, -7568, -7568, -3135, 3135, 7568},
	{6811, -1598, -8035, -4551, 4551, 8035, 1598, -6811},
	{5793, -5793, -5793, 5793, 5793, -5793, -5793, 5793},
	{4551, -8035, 1598, 6811, -6811, -1598, 8035, -4551},
	{3135, -7568, 7568, -3135, -3135, 7568, -7568, 3135},
	{1598, -4551, 6811, -8035, 8035, -6811, 4551, -1598},
}

// zigzag maps the zig-zag index of a coefficient to its natural index
// (T.81, Figure A.6).
var zigzag = [64]int{
	0, 1, 8, 16, 9, 2, 3, 10, 17, 24, 32, 25, 18, 11, 4, 5,
	12, 19, 26, 33, 40, 48, 41, 34, 27, 20, 13, 6, 7, 14, 21, 28,
	35, 42, 49, 56, 57, 50, 43, 36, 29, 22, 15, 23, 30, 37, 44, 51,
	58, 59, 52, 45, 38, 31, 39, 46, 53, 60, 61, 54, 47, 55, 62, 63,
}

// The Annex K.3 luminance tables (T.81, Tables K.3 and K.5), which every
// component uses: their counts of codes of each length and their values.
var (
	dcBits   = [16]byte{0, 1, 5, 1, 1, 1, 1, 1, 1, 0, 0, 0, 0, 0, 0, 0}
	dcValues = []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}
	acBits   = [16]byte{0, 2, 1, 3, 3, 2, 4, 3, 5, 5, 4, 4, 0, 0, 1, 125}
	acValues = []byte{
		0x01, 0x02, 0x03, 0x00, 0x04, 0x11, 0x05, 0x12, 0x21, 0x31, 0x41, 0x06, 0x13, 0x51, 0x61, 0x07,
		0x22, 0x71, 0x14, 0x32, 0x81, 0x91, 0xa1, 0x08, 0x23, 0x42, 0xb1, 0xc1, 0x15, 0x52, 0xd1, 0xf0,
		0x24, 0x33, 0x62, 0x72, 0x82, 0x09, 0x0a, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x25, 0x26, 0x27, 0x28,
		0x29, 0x2a, 0x34, 0x35, 0x36, 0x37, 0x38, 0x39, 0x3a, 0x43, 0x44, 0x45, 0x46, 0x47, 0x48, 0x49,
		0x4a, 0x53, 0x54, 0x55, 0x56, 0x57, 0x58, 0x59, 0x5a, 0x63, 0x64, 0x65, 0x66, 0x67, 0x68, 0x69,
		0x6a, 0x73, 0x74, 0x75, 0x76, 0x77, 0x78, 0x79, 0x7a, 0x83, 0x84, 0x85, 0x86, 0x87, 0x88, 0x89,
		0x8a, 0x92, 0x93, 0x94, 0x95, 0x96, 0x97, 0x98, 0x99, 0x9a, 0xa2, 0xa3, 0xa4, 0xa5, 0xa6, 0xa7,
		0xa8, 0xa9, 0xaa, 0xb2, 0xb3, 0xb4, 0xb5, 0xb6, 0xb7, 0xb8, 0xb9, 0xba, 0xc2, 0xc3, 0xc4, 0xc5,
		0xc6, 0xc7, 0xc8, 0xc9, 0xca, 0xd2, 0xd3, 0xd4, 0xd5, 0xd6, 0xd7, 0xd8, 0xd9, 0xda, 0xe1, 0xe2,
		0xe3, 0xe4, 0xe5, 0xe6, 0xe7, 0xe8, 0xe9, 0xea, 0xf1, 0xf2, 0xf3, 0xf4, 0xf5, 0xf6, 0xf7, 0xf8,
		0xf9, 0xfa,
	}
)

// huffCode is one Huffman code: its bits and its length.
type huffCode struct {
	code uint32
	size uint8
}

// huffTable returns the codes of the values of a table of these counts,
// assigned as T.81, Annex C does.
func huffTable(bits [16]byte, values []byte) [256]huffCode {
	var t [256]huffCode
	code, k := uint32(0), 0
	for l := range 16 {
		for range bits[l] {
			t[values[k]] = huffCode{code: code, size: uint8(l + 1)}
			code++
			k++
		}
		code <<= 1
	}
	return t
}

// bitWriter writes the entropy-coded segments of a scan, a 0x00 stuffed
// after each 0xFF (T.81, B.1.1.5).
type bitWriter struct {
	out   *bytes.Buffer
	acc   uint32
	nbits uint
}

func (w *bitWriter) write(code uint32, n uint) {
	for i := int(n) - 1; i >= 0; i-- {
		w.acc = w.acc<<1 | code>>uint(i)&1
		w.nbits++
		if w.nbits == 8 {
			b := byte(w.acc) //nolint:gosec // G115: the low eight bits, the byte just filled
			w.out.WriteByte(b)
			if b == 0xFF {
				w.out.WriteByte(0)
			}
			w.acc, w.nbits = 0, 0
		}
	}
}

// flush pads the last byte with 1 bits (T.81, F.1.2.3).
func (w *bitWriter) flush() {
	for w.nbits != 0 {
		w.write(1, 1)
	}
}

// category returns the size category of v and its additional bits
// (T.81, F.1.2.1).
func category(v int) (uint, uint32) {
	a := v
	if a < 0 {
		a = -a
	}
	n := uint(0)
	for a > 0 {
		n++
		a >>= 1
	}
	if v < 0 {
		v += 1<<n - 1
	}
	return n, uint32(v) //nolint:gosec // G115: v is in [0, 2^n) now
}

// JPEG writes img as a JPEG file (ITU-T T.81) as o says, every component
// sampled at the full resolution.
func JPEG(img image.Image, o JPEGOptions) []byte {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	nc := o.Components
	planes := make([][]int, nc)
	for c := range planes {
		planes[c] = make([]int, w*h)
	}
	for y := range h {
		for x := range w {
			i := y*w + x
			switch nc {
			case 1:
				g := color.GrayModel.Convert(img.At(b.Min.X+x, b.Min.Y+y)).(color.Gray) //nolint:forcetypeassert // GrayModel returns color.Gray
				planes[0][i] = int(g.Y)
			case 3:
				c := color.NRGBAModel.Convert(img.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA) //nolint:forcetypeassert // NRGBAModel returns color.NRGBA
				yy, cb, cr := color.RGBToYCbCr(c.R, c.G, c.B)
				planes[0][i], planes[1][i], planes[2][i] = int(yy), int(cb), int(cr)
			case 4:
				c := color.CMYKModel.Convert(img.At(b.Min.X+x, b.Min.Y+y)).(color.CMYK) //nolint:forcetypeassert // CMYKModel returns color.CMYK
				planes[0][i], planes[1][i], planes[2][i], planes[3][i] = 255-int(c.C), 255-int(c.M), 255-int(c.Y), 255-int(c.K)
			}
		}
	}
	bw, bh := (w+7)/8, (h+7)/8
	// coeffs holds, for each component, each block's quantized
	// coefficients in zig-zag order.
	coeffs := make([][][64]int, nc)
	for c := range nc {
		coeffs[c] = make([][64]int, bw*bh)
		for by := range bh {
			for bx := range bw {
				coeffs[c][by*bw+bx] = fdct(planes[c], w, h, bx*8, by*8)
			}
		}
	}

	var out bytes.Buffer
	out.Write([]byte{0xFF, 0xD8})
	if o.JFIFDPI > 0 {
		seg := []byte("JFIF\x00\x01\x02\x01")
		seg = binary.BigEndian.AppendUint16(seg, uint16(o.JFIFDPI)) //nolint:gosec // G115: a test's density
		seg = binary.BigEndian.AppendUint16(seg, uint16(o.JFIFDPI)) //nolint:gosec // G115: as above
		seg = append(seg, 0, 0)
		segment(&out, 0xE0, seg)
	}
	if o.EXIF != nil {
		segment(&out, 0xE1, append([]byte("Exif\x00\x00"), o.EXIF...))
	}
	if o.ICC != nil {
		const chunk = 65519 - 14
		n := (len(o.ICC) + chunk - 1) / chunk
		for i := range n {
			part := o.ICC[i*chunk : min(len(o.ICC), (i+1)*chunk)]
			segment(&out, 0xE2, append([]byte{'I', 'C', 'C', '_', 'P', 'R', 'O', 'F', 'I', 'L', 'E', 0, byte(i + 1), byte(n)}, part...)) //nolint:gosec // G115: a test profile of fewer than 256 chunks
		}
	}
	if nc == 4 {
		// DCTEncode version 100, no flags, transform 0: the components
		// are written as they are.
		segment(&out, 0xEE, []byte{'A', 'd', 'o', 'b', 'e', 0, 100, 0, 0, 0, 0, 0})
	}
	dqt := make([]byte, 65)
	for i := 1; i < len(dqt); i++ {
		dqt[i] = jpegQuant
	}
	segment(&out, 0xDB, dqt)
	sof := byte(0xC0)
	if o.Progressive {
		sof = 0xC2
	}
	frame := make([]byte, 0, 6+3*nc)
	frame = append(frame, 8, byte(h>>8), byte(h), byte(w>>8), byte(w), byte(nc)) //nolint:gosec // G115: a test picture of at most 65535 a side, 1 to 4 components
	for c := range nc {
		frame = append(frame, byte(c+1), 0x11, 0)
	}
	segment(&out, sof, frame)
	dht := make([]byte, 0, 2+len(dcBits)+len(dcValues)+len(acBits)+len(acValues))
	dht = append(dht, 0x00)
	dht = append(dht, dcBits[:]...)
	dht = append(dht, dcValues...)
	dht = append(dht, 0x10)
	dht = append(dht, acBits[:]...)
	dht = append(dht, acValues...)
	segment(&out, 0xC4, dht)
	dc, ac := huffTable(dcBits, dcValues), huffTable(acBits, acValues)

	if !o.Progressive {
		scan(&out, nc, 0, 63, func(bw2 *bitWriter) {
			pred := make([]int, nc)
			for i := range bw * bh {
				for c := range nc {
					blk := &coeffs[c][i]
					encodeDC(bw2, dc, blk[0]-pred[c])
					pred[c] = blk[0]
					encodeAC(bw2, ac, blk, 1, 63)
				}
			}
		})
	} else {
		scan(&out, nc, 0, 0, func(bw2 *bitWriter) {
			pred := make([]int, nc)
			for i := range bw * bh {
				for c := range nc {
					encodeDC(bw2, dc, coeffs[c][i][0]-pred[c])
					pred[c] = coeffs[c][i][0]
				}
			}
		})
		for c := range nc {
			scanOne(&out, c, func(bw2 *bitWriter) {
				for i := range bw * bh {
					encodeAC(bw2, ac, &coeffs[c][i], 1, 63)
				}
			})
		}
	}
	out.Write([]byte{0xFF, 0xD9})
	return out.Bytes()
}

// segment writes a marker segment.
func segment(out *bytes.Buffer, marker byte, body []byte) {
	out.Write([]byte{0xFF, marker})
	out.Write(binary.BigEndian.AppendUint16(nil, uint16(len(body)+2))) //nolint:gosec // G115: a segment holds at most 65533 bytes
	out.Write(body)
}

// scan writes a scan of every component from ss to se, its entropy-coded
// data written by code.
func scan(out *bytes.Buffer, nc, ss, se int, code func(*bitWriter)) {
	sos := make([]byte, 0, 4+2*nc)
	sos = append(sos, byte(nc)) //nolint:gosec // G115: 1 to 4 components
	for c := range nc {
		sos = append(sos, byte(c+1), 0x00)
	}
	sos = append(sos, byte(ss), byte(se), 0) //nolint:gosec // G115: spectral positions 0 to 63
	segment(out, 0xDA, sos)
	bw := &bitWriter{out: out}
	code(bw)
	bw.flush()
}

// scanOne writes an AC scan of component c alone, from 1 to 63.
func scanOne(out *bytes.Buffer, c int, code func(*bitWriter)) {
	segment(out, 0xDA, []byte{1, byte(c + 1), 0x00, 1, 63, 0}) //nolint:gosec // G115: a component index of 0 to 3
	bw := &bitWriter{out: out}
	code(bw)
	bw.flush()
}

// encodeDC writes a DC difference (T.81, F.1.2.1).
func encodeDC(w *bitWriter, dc [256]huffCode, diff int) {
	n, bits := category(diff)
	c := dc[n]
	w.write(c.code, uint(c.size))
	w.write(bits, n)
}

// encodeAC writes the AC coefficients ss to se of a block, an end of
// block after its last non-zero one (T.81, F.1.2.2); in a progressive
// scan that end is EOB0, a run of one block (G.1.2.2).
func encodeAC(w *bitWriter, ac [256]huffCode, blk *[64]int, ss, se int) {
	run := 0
	for k := ss; k <= se; k++ {
		v := blk[k]
		if v == 0 {
			run++
			continue
		}
		for run > 15 {
			c := ac[0xF0]
			w.write(c.code, uint(c.size))
			run -= 16
		}
		n, bits := category(v)
		c := ac[byte(run<<4)|byte(n)] //nolint:gosec // G115: a run of 0 to 15 and a size category of 0 to 10
		w.write(c.code, uint(c.size))
		w.write(bits, n)
		run = 0
	}
	if run > 0 {
		c := ac[0x00]
		w.write(c.code, uint(c.size))
	}
}

// fdct returns the quantized coefficients, in zig-zag order, of the block
// of plane (w by h samples, its last row and column repeated past its
// edges) whose top-left sample is x0, y0 (T.81, A.3.3).
func fdct(plane []int, w, h, x0, y0 int) [64]int {
	var f [8][8]int64
	for y := range 8 {
		for x := range 8 {
			sx, sy := min(x0+x, w-1), min(y0+y, h-1)
			f[y][x] = int64(plane[sy*w+sx] - 128)
		}
	}
	var out [64]int
	for v := range 8 {
		for u := range 8 {
			var s int64
			for y := range 8 {
				var row int64
				for x := range 8 {
					row += fdctTable[u][x] * f[y][x]
				}
				s += fdctTable[v][y] * row
			}
			// F(u, v) is s / 2^26 / 4; it is quantized by jpegQuant,
			// rounding half away from zero.
			den := int64(1) << 28 * jpegQuant
			q := (abs64(s) + den/2) / den
			if s < 0 {
				q = -q
			}
			out[v*8+u] = int(q)
		}
	}
	var zz [64]int
	for k, n := range zigzag {
		zz[k] = out[n]
	}
	return zz
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
