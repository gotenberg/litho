package imagetest

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"sort"
)

// lsbWriter writes bits least significant first, as a VP8L bitstream
// reads them (RFC 9649, 3.1).
type lsbWriter struct {
	out   []byte
	acc   uint64
	nbits uint
}

func (w *lsbWriter) write(v uint64, n uint) {
	w.acc |= v << w.nbits
	w.nbits += n
	for w.nbits >= 8 {
		w.out = append(w.out, byte(w.acc)) //nolint:gosec // G115: the low eight bits
		w.acc >>= 8
		w.nbits -= 8
	}
}

func (w *lsbWriter) bytes() []byte {
	if w.nbits > 0 {
		w.out = append(w.out, byte(w.acc)) //nolint:gosec // G115: the bits left, fewer than eight
		w.acc, w.nbits = 0, 0
	}
	return w.out
}

// prefixCode is a canonical prefix code over an alphabet: each symbol's
// length and code.
type prefixCode struct {
	lengths []int
	codes   []uint32
}

// completeCode returns a canonical prefix code of n symbols whose code
// lengths differ by one at most and make a complete code: 2^k - n symbols
// of k - 1 bits and the rest of k, k the bits n symbols need.
func completeCode(n int) prefixCode {
	k := 0
	for 1<<k < n {
		k++
	}
	short := 1<<k - n
	lengths := make([]int, n)
	for i := range lengths {
		lengths[i] = k
		if i < short {
			lengths[i] = k - 1
		}
	}
	return canonical(lengths)
}

// canonical assigns canonical codes to the lengths, shorter codes first
// and symbols in order within a length (RFC 9649, 3.7.2.1.1, as DEFLATE
// does).
func canonical(lengths []int) prefixCode {
	type sym struct{ s, l int }
	var syms []sym
	for s, l := range lengths {
		if l > 0 {
			syms = append(syms, sym{s, l})
		}
	}
	sort.SliceStable(syms, func(i, j int) bool { return syms[i].l < syms[j].l })
	codes := make([]uint32, len(lengths))
	code, prev := uint32(0), 0
	for i, s := range syms {
		if i > 0 {
			code = (code + 1) << uint(s.l-prev)
		}
		prev = s.l
		codes[s.s] = code
	}
	return prefixCode{lengths: lengths, codes: codes}
}

// put writes symbol s, its code's bits most significant first, each read
// as one bit of the stream.
func (c prefixCode) put(w *lsbWriter, s int) {
	l := c.lengths[s]
	for i := l - 1; i >= 0; i-- {
		w.write(uint64(c.codes[s]>>uint(i)&1), 1)
	}
}

// codeLengthOrder is the order a normal prefix code's code length code
// lengths are written in (RFC 9649, 3.7.2.1.2).
var codeLengthOrder = [19]int{17, 18, 0, 1, 2, 3, 4, 5, 16, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}

// writeNormalCode writes c as a normal prefix code (RFC 9649,
// 3.7.2.1.2): its two distinct lengths, at most, coded by a code length
// code of one bit each.
func writeNormalCode(w *lsbWriter, c prefixCode) {
	var used []int
	for _, l := range c.lengths {
		found := false
		for _, u := range used {
			found = found || u == l
		}
		if !found {
			used = append(used, l)
		}
	}
	sort.Ints(used)
	clLengths := make([]int, 19)
	for _, u := range used {
		clLengths[u] = 1
	}
	if len(used) == 1 {
		// A code of one length alone: the code length code holds that
		// length and a second, unused one, so that it is complete.
		other := 0
		if used[0] == 0 {
			other = 1
		}
		clLengths[other] = 1
	}
	cl := canonical(clLengths)
	last := 4
	for i, s := range codeLengthOrder {
		if clLengths[s] > 0 {
			last = max(last, i+1)
		}
	}
	w.write(0, 1)              // a normal code
	w.write(uint64(last-4), 4) //nolint:gosec // G115: last is 4 to 19
	for i := range last {
		w.write(uint64(clLengths[codeLengthOrder[i]]), 3) //nolint:gosec // G115: a code length code length of 0 or 1
	}
	w.write(0, 1) // max_symbol: every symbol's length is written
	for _, l := range c.lengths {
		cl.put(w, l)
	}
}

// writeSimpleCode writes a simple prefix code of the one symbol s (RFC
// 9649, 3.7.2.1.1): it takes no bit to read.
func writeSimpleCode(w *lsbWriter, s int) {
	w.write(1, 1) // a simple code
	w.write(0, 1) // of one symbol
	if s < 2 {
		w.write(0, 1)
		w.write(uint64(s), 1) //nolint:gosec // G115: s is 0 or 1
		return
	}
	w.write(1, 1)
	w.write(uint64(s), 8)
}

// VP8L returns img as a lossless VP8L bitstream (RFC 9649, section 3): no
// transform, no colour cache, one prefix code group, every pixel a
// literal. The alpha code is a single symbol for an opaque picture.
func VP8L(img image.Image) []byte {
	b := img.Bounds()
	width, height := b.Dx(), b.Dy()
	opaque := true
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA).A != 0xFF { //nolint:forcetypeassert // NRGBAModel returns color.NRGBA
				opaque = false
			}
		}
	}
	w := &lsbWriter{}
	w.write(0x2f, 8)
	w.write(uint64(width-1), 14)  //nolint:gosec // G115: a VP8L width, 1 to 16384
	w.write(uint64(height-1), 14) //nolint:gosec // G115: a VP8L height, 1 to 16384
	alphaUsed := uint64(0)
	if !opaque {
		alphaUsed = 1
	}
	w.write(alphaUsed, 1)
	w.write(0, 3) // version
	w.write(0, 1) // no transform
	w.write(0, 1) // no colour cache
	w.write(0, 1) // no meta prefix codes
	green := completeCode(256 + 24)
	byteCode := completeCode(256)
	writeNormalCode(w, green)
	writeNormalCode(w, byteCode) // red
	writeNormalCode(w, byteCode) // blue
	if opaque {
		writeSimpleCode(w, 255)
	} else {
		writeNormalCode(w, byteCode)
	}
	writeSimpleCode(w, 0) // distance, never read
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA) //nolint:forcetypeassert // NRGBAModel returns color.NRGBA
			green.put(w, int(c.G))
			byteCode.put(w, int(c.R))
			byteCode.put(w, int(c.B))
			if !opaque {
				byteCode.put(w, int(c.A))
			}
		}
	}
	return w.bytes()
}

// Chunk is one chunk of a RIFF container: its FourCC and its body.
type Chunk struct {
	FourCC string
	Body   []byte
}

// RIFFWebP returns a WebP file of the chunks (RFC 9649, 2.4), each padded
// to an even length.
func RIFFWebP(chunks ...Chunk) []byte {
	var b bytes.Buffer
	b.WriteString("RIFF\x00\x00\x00\x00WEBP")
	for _, c := range chunks {
		b.WriteString(c.FourCC)
		b.Write(binary.LittleEndian.AppendUint32(nil, uint32(len(c.Body)))) //nolint:gosec // G115: a test chunk
		b.Write(c.Body)
		if len(c.Body)%2 == 1 {
			b.WriteByte(0)
		}
	}
	data := b.Bytes()
	binary.LittleEndian.PutUint32(data[4:], uint32(len(data)-8)) //nolint:gosec // G115: a test file
	return data
}

// VP8X returns the body of a VP8X chunk of these flags and canvas size
// (RFC 9649, 2.7): ICC 0x20, alpha 0x10, EXIF 0x08, XMP 0x04, animation
// 0x02.
func VP8X(flags byte, width, height int) []byte {
	return []byte{flags, 0, 0, 0, byte(width - 1), byte((width - 1) >> 8), byte((width - 1) >> 16), byte(height - 1), byte((height - 1) >> 8), byte((height - 1) >> 16)} //nolint:gosec // G115: the three bytes of a 24-bit canvas size
}

// ANMF returns the body of an ANMF chunk (RFC 9649, 2.7.1.2): a frame at
// x, y (even), w by h pixels, shown for duration milliseconds, its
// bitstream chunks after the header.
func ANMF(x, y, w, h, duration int, chunks ...Chunk) []byte {
	u24 := func(v int) []byte { return []byte{byte(v), byte(v >> 8), byte(v >> 16)} } //nolint:gosec // G115: the three bytes of a 24-bit field
	body := append(append(append(append(u24(x/2), u24(y/2)...), u24(w-1)...), u24(h-1)...), u24(duration)...)
	body = append(body, 0)
	sub := RIFFWebP(chunks...)
	return append(body, sub[12:]...)
}

// AnimatedWebP returns an animated WebP file whose frames, each a
// lossless picture of the canvas's size, show in turn.
func AnimatedWebP(frames ...image.Image) []byte {
	b := frames[0].Bounds()
	chunks := make([]Chunk, 0, 2+len(frames))
	chunks = append(chunks,
		Chunk{"VP8X", VP8X(0x02|0x10, b.Dx(), b.Dy())},
		Chunk{"ANIM", []byte{0, 0, 0, 0, 0, 0}},
	)
	for _, f := range frames {
		fb := f.Bounds()
		chunks = append(chunks, Chunk{"ANMF", ANMF(0, 0, fb.Dx(), fb.Dy(), 100, Chunk{"VP8L", VP8L(f)})})
	}
	return RIFFWebP(chunks...)
}
