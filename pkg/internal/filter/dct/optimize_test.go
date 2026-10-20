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

// TestOptimizeHuffmanKeepsRestartMarkers checks that each restart marker of
// the source is written again, in sequence.
func TestOptimizeHuffmanKeepsRestartMarkers(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/restart.jpg")
	if err != nil {
		t.Fatal(err)
	}
	out, ok, err := OptimizeHuffman(context.Background(), data)
	if err != nil || !ok {
		t.Fatalf("OptimizeHuffman = %v, %v", ok, err)
	}
	markers := func(b []byte) []byte {
		var ms []byte
		for i := 0; i+1 < len(b); i++ {
			if b[i] == 0xFF && b[i+1] >= 0xD0 && b[i+1] <= 0xD7 {
				ms = append(ms, b[i+1])
			}
		}
		return ms
	}
	if want, got := markers(data), markers(out); len(want) == 0 || !bytes.Equal(want, got) {
		t.Fatalf("restart markers %x, want %x", got, want)
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
