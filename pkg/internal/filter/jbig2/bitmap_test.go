package jbig2

import (
	"bytes"
	"math/rand/v2"
	"testing"
)

// combinePixels is the reading of ITU-T T.88, 7.4.1.5 pixel by pixel, the
// reference [Bitmap.combine] must reproduce a byte at a time.
func combinePixels(b, src *Bitmap, x, y int64, op int) {
	for sy := range src.Height {
		for sx := range src.Width {
			dx, dy := x+int64(sx), y+int64(sy)
			if dx < 0 || dy < 0 || dx >= int64(b.Width) || dy >= int64(b.Height) {
				continue
			}
			s, d := src.pixel(sx, sy), b.pixel(int(dx), int(dy))
			var v uint32
			switch op {
			case combineOr:
				v = d | s
			case combineAnd:
				v = d & s
			case combineXor:
				v = d ^ s
			case combineXnor:
				v = 1 ^ (d ^ s)
			case combineReplace:
				v = s
			}
			b.setPixel(int(dx), int(dy), v)
		}
	}
}

// TestBitmapCombine_MatchesPixelReading draws random bitmaps at random
// offsets, past every edge and across byte boundaries, with every operator,
// and requires the bytes [Bitmap.combine] writes to equal the pixel by pixel
// reading's. The source's padding bits are set, as fill sets them, so a draw
// that let them through would show.
func TestBitmapCombine_MatchesPixelReading(t *testing.T) {
	t.Parallel()

	rng := rand.New(rand.NewPCG(1, 2))
	random := func(w, h int) *Bitmap {
		bm, err := newBitmap(w, h)
		if err != nil {
			t.Fatal(err)
		}
		for i := range bm.Data {
			bm.Data[i] = byte(rng.UintN(256))
		}
		return bm
	}
	for i := range 5000 {
		page := random(1+rng.IntN(40), 1+rng.IntN(12))
		src := random(rng.IntN(30), rng.IntN(10))
		src.fill(1)
		for j := range src.Data {
			src.Data[j] &= byte(rng.UintN(256)) | byte(rng.UintN(256))
		}
		x := int64(rng.IntN(page.Width+40)) - 20
		y := int64(rng.IntN(page.Height+12)) - 6
		op := rng.IntN(combineReplace + 1)

		want := &Bitmap{Width: page.Width, Height: page.Height, Stride: page.Stride, Data: bytes.Clone(page.Data)}
		combinePixels(want, src, x, y, op)
		if err := page.combine(src, x, y, op); err != nil {
			t.Fatalf("case %d: combine: %v", i, err)
		}
		if !bytes.Equal(page.Data, want.Data) {
			t.Fatalf("case %d: %dx%d onto %dx%d at (%d, %d), op %d: got % x, want % x",
				i, src.Width, src.Height, page.Width, page.Height, x, y, op, page.Data, want.Data)
		}
	}
}

// TestBudget_ChargesBuffersByBytes pins that a bitmap the decoder only
// allocates and draws into is charged its bytes, while a bitmap it decodes is
// charged its pixels: four page-sized text regions of 800 million pixels each
// fit the default budget, where one decoded region of that size does not.
func TestBudget_ChargesBuffersByBytes(t *testing.T) {
	t.Parallel()

	b := &budget{}
	for i := range 4 {
		if err := b.chargeBuffer(80_000, 10_000); err != nil {
			t.Fatalf("buffer %d of 100 MB refused: %v", i+1, err)
		}
	}
	if err := b.chargeBuffer(8, 1); err == nil {
		t.Error("byte past four 100 MB buffers admitted")
	}
	if err := (&budget{}).charge(80_000, 10_000); err == nil {
		t.Error("decoded region of 800 million pixels admitted")
	}
}
