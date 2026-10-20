package dct

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"testing"
)

// encodeJPEG encodes a w x h test pattern with image/jpeg, whose encoder
// writes baseline JPEG with the example tables of T.81, Annex K.3, so its
// output always has room for optimized tables.
func encodeJPEG(t *testing.T, w, h int, gray bool) []byte {
	t.Helper()
	var img image.Image
	if gray {
		g := image.NewGray(image.Rect(0, 0, w, h))
		for y := range h {
			for x := range w {
				g.SetGray(x, y, color.Gray{Y: byte(x*7 ^ y*3)})
			}
		}
		img = g
	} else {
		c := image.NewRGBA(image.Rect(0, 0, w, h))
		for y := range h {
			for x := range w {
				c.SetRGBA(x, y, color.RGBA{R: byte(x * 5), G: byte(y * 3), B: byte(x ^ y), A: 255})
			}
		}
		img = c
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// samePixels fails unless a and b decode to the same samples, with both
// image/jpeg and the lenient decoder.
func samePixels(t *testing.T, a, b []byte) {
	t.Helper()
	ra, _, _, _ := reference(t, a)
	rb, _, _, _ := reference(t, b)
	if !bytes.Equal(ra, rb) {
		t.Fatal("image/jpeg decodes the re-coded data to other samples")
	}
	da, _, _, _, err := Decode(context.Background(), a, -1, 0, DefaultMaxSamples)
	if err != nil {
		t.Fatal(err)
	}
	db, _, _, _, err := Decode(context.Background(), b, -1, 0, DefaultMaxSamples)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(da, db) {
		t.Fatal("Decode decodes the re-coded data to other samples")
	}
}

func TestOptimizeHuffmanKeepsSamplesAndShrinks(t *testing.T) {
	t.Parallel()
	restart, err := os.ReadFile("testdata/restart.jpg")
	if err != nil {
		t.Fatal(err)
	}
	cmyk, err := os.ReadFile("testdata/cmyk_adobe.jpg")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		data []byte
	}{
		{"rgb 4:2:0", encodeJPEG(t, 67, 45, false)},
		{"gray", encodeJPEG(t, 33, 17, true)},
		{"one block", encodeJPEG(t, 8, 8, true)},
		{"restart intervals", restart},
		{"adobe cmyk", cmyk},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, ok, err := OptimizeHuffman(context.Background(), tc.data)
			if err != nil || !ok {
				t.Fatalf("OptimizeHuffman = %v, %v; want a re-coding", ok, err)
			}
			if len(out) >= len(tc.data) {
				t.Fatalf("re-coded to %d bytes from %d", len(out), len(tc.data))
			}
			samePixels(t, tc.data, out)
			again, ok, err := OptimizeHuffman(context.Background(), tc.data)
			if err != nil || !ok || !bytes.Equal(again, out) {
				t.Fatal("a second re-coding of the same data differs")
			}
		})
	}
}

// restartMarkers returns the restart markers of b, and whether it holds a
// DRI marker. Neither byte pair occurs inside entropy-coded data, where 0xFF
// is followed by a stuffed zero (ITU-T T.81, F.1.2.3).
func restartMarkers(b []byte) ([]byte, bool) {
	var (
		ms  []byte
		dri bool
	)
	for i := 0; i+1 < len(b); i++ {
		if b[i] == 0xFF && b[i+1] >= 0xD0 && b[i+1] <= 0xD7 {
			ms = append(ms, b[i+1])
		}
		if b[i] == 0xFF && b[i+1] == 0xDD {
			dri = true
		}
	}
	return ms, dri
}

// sameCoefficients fails unless a and b code the same quantized
// coefficients, by the digest of their walks.
func sameCoefficients(t *testing.T, a, b []byte) {
	t.Helper()
	sumA, err := walkSequential(context.Background(), a, nopVisitor{})
	if err != nil {
		t.Fatal(err)
	}
	sumB, err := walkSequential(context.Background(), b, nopVisitor{})
	if err != nil {
		t.Fatal(err)
	}
	if sumA != sumB {
		t.Fatal("the re-coding codes other coefficients")
	}
}

// TestOptimizeHuffmanDropsRestartIntervals checks that the DRI segment and
// every restart marker go, and the coefficients stay.
func TestOptimizeHuffmanDropsRestartIntervals(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/restart.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if ms, dri := restartMarkers(data); len(ms) == 0 || !dri {
		t.Fatal("the source has no restart intervals")
	}
	out, ok, err := OptimizeHuffman(context.Background(), data)
	if err != nil || !ok {
		t.Fatalf("OptimizeHuffman = %v, %v", ok, err)
	}
	if ms, dri := restartMarkers(out); len(ms) > 0 || dri {
		t.Fatalf("restart markers %x and DRI %v kept", ms, dri)
	}
	sameCoefficients(t, data, out)
	samePixels(t, data, out)
}

// overflowJPEG returns a 16x8 grayscale JPEG of two blocks, one per restart
// interval, whose DC coefficients are -2047 and 2047. Each is coded from a
// zero prediction, in category 11; their difference, 4094, has no category
// for 8-bit samples (ITU-T T.81, Table F.1). Its DC table gives every
// category a 4-bit code, so optimized tables code it smaller.
func overflowJPEG() []byte {
	out := []byte{0xFF, 0xD8}
	dqt := []byte{0xFF, 0xDB, 0x00, 0x43, 0x00}
	for range 64 {
		dqt = append(dqt, 1)
	}
	out = append(out, dqt...)
	out = append(out, 0xFF, 0xC0, 0x00, 0x0B, 8, 0x00, 0x08, 0x00, 0x10, 1, 1, 0x11, 0)
	// DC table 0: categories 0 to 11, 4 bits each; AC table 0: EOB alone,
	// in 2 bits.
	dht := []byte{0xFF, 0xC4, 0x00, 0x00, 0x00, 0, 0, 0, 12, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	for v := range 12 {
		dht = append(dht, byte(v))
	}
	dht = append(dht, 0x10, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x00)
	dht[2], dht[3] = 0, byte(len(dht)-2)
	out = append(out, dht...)
	out = append(out, 0xFF, 0xDD, 0x00, 0x04, 0x00, 0x01)
	out = append(out, 0xFF, 0xDA, 0x00, 0x08, 1, 1, 0x00, 0, 63, 0)
	block := func(extra uint32) []byte {
		e := &encodeVisitor{}
		e.put(0b1011, 4) // Category 11.
		e.put(extra, 11)
		e.put(0b00, 2) // EOB.
		e.flush()
		return e.out
	}
	out = append(out, block(0)...) // -2047.
	out = append(out, 0xFF, 0xD0)
	out = append(out, block(2047)...)
	return append(out, 0xFF, 0xD9)
}

// TestOptimizeHuffmanKeepsRestartsWhenDCOverflows checks that restart
// intervals whose drop would need a DC difference past Table F.1 are kept,
// and the data still re-coded.
func TestOptimizeHuffmanKeepsRestartsWhenDCOverflows(t *testing.T) {
	t.Parallel()
	data := overflowJPEG()
	out, ok, err := OptimizeHuffman(context.Background(), data)
	if err != nil || !ok {
		t.Fatalf("OptimizeHuffman = %v, %v; want a re-coding", ok, err)
	}
	wantMs, _ := restartMarkers(data)
	if ms, dri := restartMarkers(out); !bytes.Equal(ms, wantMs) || !dri {
		t.Fatalf("restart markers %x and DRI %v, want %x and DRI", ms, dri, wantMs)
	}
	sameCoefficients(t, data, out)
	samePixels(t, data, out)
}

// TestDCCoderRoundTrips checks that every difference of 8-bit samples is
// coded in its category and extended back to itself (ITU-T T.81, F.1.2.1.1
// and F.2.2.1).
func TestDCCoderRoundTrips(t *testing.T) {
	t.Parallel()
	for diff := -2047; diff <= 2047; diff++ {
		var c dcCoder
		sym, extra := c.code(0, diff)
		if c.overflow {
			t.Fatalf("difference %d overflows", diff)
		}
		if got := extendDC(int(sym), extra); got != diff {
			t.Fatalf("difference %d coded as %d, %d extends to %d", diff, sym, extra, got)
		}
	}
	var c dcCoder
	c.code(0, 2048)
	if !c.overflow {
		t.Fatal("a difference of 2048 does not overflow")
	}
}

func TestOptimizeHuffmanLeavesOtherCodings(t *testing.T) {
	t.Parallel()
	progressive, err := os.ReadFile("testdata/progressive.jpg")
	if err != nil {
		t.Fatal(err)
	}
	base := encodeJPEG(t, 40, 24, false)
	arithmetic := bytes.Clone(base)
	for i := 0; i+1 < len(arithmetic); i++ {
		if arithmetic[i] == 0xFF && arithmetic[i+1] == 0xC0 {
			arithmetic[i+1] = 0xC9
			break
		}
	}
	// Bytes inserted before the EOI marker are data no unit reads.
	padded := append(bytes.Clone(base[:len(base)-2]), 0x12, 0x34, 0xFF, 0xD9)
	cases := []struct {
		name string
		data []byte
	}{
		{"progressive", progressive},
		{"arithmetic", arithmetic},
		{"truncated", base[:len(base)/2]},
		{"unread data before EOI", padded},
		{"no EOI", base[:len(base)-2]},
		{"empty", nil},
		{"not a JPEG", []byte("%PDF-1.7")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, ok, err := OptimizeHuffman(context.Background(), tc.data)
			if err != nil || ok || out != nil {
				t.Fatalf("OptimizeHuffman = %d bytes, %v, %v; want it left alone", len(out), ok, err)
			}
		})
	}
}

// TestOptimizeHuffmanKeepsTrailingBytes checks that bytes after the EOI
// marker, which decoders do not read, are kept.
func TestOptimizeHuffmanKeepsTrailingBytes(t *testing.T) {
	t.Parallel()
	data := append(encodeJPEG(t, 24, 24, false), "trailer"...)
	out, ok, err := OptimizeHuffman(context.Background(), data)
	if err != nil || !ok {
		t.Fatalf("OptimizeHuffman = %v, %v", ok, err)
	}
	if !bytes.HasSuffix(out, []byte("\xFF\xD9trailer")) {
		t.Fatal("the bytes after EOI were not kept")
	}
}

func TestOptimizeHuffmanHonoursContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// Enough blocks to reach a context check.
	data := encodeJPEG(t, 640, 512, false)
	if _, ok, err := OptimizeHuffman(ctx, data); ok || !errors.Is(err, context.Canceled) {
		t.Fatalf("OptimizeHuffman = %v, %v; want the context's error", ok, err)
	}
}

// TestOptimalTableLimitsCodeLengths feeds counts that grow like the
// Fibonacci numbers, whose unlimited Huffman tree is as deep as there are
// symbols, and checks the table stays a valid 16-bit prefix code with no
// all-ones code (ITU-T T.81, C.2 and K.3).
func TestOptimalTableLimitsCodeLengths(t *testing.T) {
	t.Parallel()
	var freq [256]int64
	a, b := int64(1), int64(1)
	for i := range 60 {
		freq[i] = a
		a, b = b, a+b
	}
	table, err := optimalTable(&freq)
	if err != nil {
		t.Fatal(err)
	}
	if len(table.values) != 60 {
		t.Fatalf("%d values, want 60", len(table.values))
	}
	// Kraft sum, in units of 2^-16, strictly below one: the all-ones code
	// of the longest length stays unused.
	kraft := 0
	for l, c := range table.counts {
		kraft += int(c) << (maxCodeLength - (l + 1))
	}
	if kraft >= 1<<maxCodeLength {
		t.Fatalf("Kraft sum %d of %d", kraft, 1<<maxCodeLength)
	}
	if _, err := newHuffman(table.counts, table.values); err != nil {
		t.Fatalf("the table does not decode: %v", err)
	}
}
