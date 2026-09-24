package jbig2

import (
	"context"
	"os"
	"testing"
)

// fuzzSmallCap is the lowered bitmap cap [FuzzDecode] also decodes under. It
// admits the 64x56 pages of the seeds.
const fuzzSmallCap = 1 << 12

// FuzzDecode feeds arbitrary page and global segment data to the decoder. The
// contract is crash-freedom: every input either decodes to a bitmap whose
// buffer matches its dimensions or returns an error.
func FuzzDecode(f *testing.F) {
	b, err := os.ReadFile("testdata/t88_annex_h1.jb2")
	if err != nil {
		f.Fatalf("reading seed: %v", err)
	}
	// Seeds reach the parser and both generic region codings: the Annex H.1
	// page 1 (MMR) and page 2 (arithmetic, TPGD) regions, each behind its
	// page information segment, whole and truncated, plus an encoder round
	// trip with moved AT pixels.
	var raw [][]byte
	for off := 13; off < len(b); {
		_, n, err := parseSegment(b[off:])
		if err != nil {
			f.Fatalf("seed segment: %v", err)
		}
		raw = append(raw, b[off:off+n])
		off += n
	}
	mmr := concat(raw[1], raw[4])
	arith := concat(raw[8], raw[11])
	f.Add(mmr, []byte(nil))
	f.Add(arith, []byte(nil))
	f.Add(arith[:len(arith)-9], []byte(nil))
	f.Add(mmr[:len(mmr)-20], raw[0])
	src := testImage(40, 24, 7)
	at := [][2]int{{4, -2}, {-5, -1}, {1, -3}, {-6, -3}}
	f.Add(genericStream(src.Width, src.Height, 0, true, at, encodeGeneric(src, 0, true, at)), []byte(nil))
	// Symbol dictionaries, text regions with refinement, and globals: Annex
	// H.1 page 3 and the ISO 32000-1 example.
	f.Add(concat(raw[15], raw[16], raw[17], raw[18], raw[19]), []byte(nil))
	if page, err := os.ReadFile("testdata/iso32000_jbig2_page.jb2"); err == nil {
		if globals, err := os.ReadFile("testdata/iso32000_jbig2_globals.jb2"); err == nil {
			f.Add(page, globals)
		}
	}
	ref := testImage(24, 16, 3)
	f.Add(refinementStream(ref, 0, true, [2][2]int{{-1, -1}, {-1, -1}},
		encodeRefinement(refinedImage(ref, 1), ref, 0, true, [2][2]int{{-1, -1}, {-1, -1}})), []byte(nil))
	// The Huffman variants: Annex H.1 page 1's dictionaries and text region.
	f.Add(concat(raw[1], raw[0], raw[2], raw[3]), []byte(nil))
	// Pattern dictionaries and halftone regions: Annex H.1 pages 1 (MMR) and
	// 2 (arithmetic) whole.
	f.Add(concat(raw[0], raw[1], raw[2], raw[3], raw[4], raw[5], raw[6], raw[7]), []byte(nil))
	f.Add(concat(raw[0], raw[8], raw[9], raw[10], raw[11], raw[12], raw[13], raw[14]), []byte(nil))
	f.Add([]byte{}, []byte{})

	f.Fuzz(func(t *testing.T, data, globals []byte) {
		// A small cap takes the refusals a lowered image budget moves: whatever
		// decodes under it stays inside it.
		if small, err := Decode(context.Background(), data, globals, fuzzSmallCap); err == nil && int64(small.Width)*int64(small.Height) > fuzzSmallCap {
			t.Fatalf("%dx%d page decoded under a %d-pixel cap", small.Width, small.Height, int64(fuzzSmallCap))
		}
		bm, err := Decode(context.Background(), data, globals, DefaultMaxBitmapPixels)
		if err != nil {
			return
		}
		if len(bm.Data) != bm.Stride*bm.Height || bm.Stride != (bm.Width+7)/8 {
			t.Fatalf("%dx%d bitmap with stride %d holds %d bytes", bm.Width, bm.Height, bm.Stride, len(bm.Data))
		}
	})
}
