package jbig2

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"math/rand/v2"
	"os"
	"testing"
)

// TestDecode_AnnexH1FullPages decodes pages 1 and 2 of the Annex H.1
// datastream whole: a text region over two symbol dictionaries, a generic
// region, and a halftone region over a pattern dictionary. Page 1 codes every
// part with Huffman tables or MMR, page 2 with arithmetic coding; T.88 says
// the two pages are identical. The golden is jbig2dec 0.20's decode.
func TestDecode_AnnexH1FullPages(t *testing.T) {
	t.Parallel()

	raw := annexH1(t)
	want := readPBM(t, "testdata/t88_annex_h1_page1.pbm")
	for _, tc := range []struct {
		name string
		segs []int
	}{
		{"page 1, Huffman and MMR", []int{0, 1, 2, 3, 4, 5, 6, 7}},
		{"page 2, arithmetic", []int{0, 8, 9, 10, 11, 12, 13, 14}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var stream []byte
			for _, i := range tc.segs {
				stream = append(stream, raw[i]...)
			}
			got, err := Decode(context.Background(), stream, nil, DefaultMaxBitmapBytes)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if got.Width != want.Width || got.Height != want.Height || !bytes.Equal(got.Data, want.Data) {
				t.Errorf("page is %dx%d and differs from the golden in %d bytes", got.Width, got.Height, differingBytes(got.Data, want.Data))
			}
		})
	}
}

// mmrPlanes reads two 23x9 MMR blocks coded by libtiff's Group 4 encoder, each
// ending with EOFB, back to back and followed by two stray bytes, with the
// planes they hold.
func mmrPlanes(t *testing.T) ([]byte, [2]*Bitmap) {
	t.Helper()
	data, err := os.ReadFile("testdata/mmr_planes.g4")
	if err != nil {
		t.Fatalf("reading MMR planes: %v", err)
	}
	return data, [2]*Bitmap{readPBM(t, "testdata/mmr_plane0.pbm"), readPBM(t, "testdata/mmr_plane1.pbm")}
}

// TestDecodeMMRBlock_ConsumesWholeBlocks pins the byte accounting the
// gray-scale MMR path depends on: each block must report exactly the bytes it
// occupies, 60 and 55 here, or the next plane starts in the wrong place.
func TestDecodeMMRBlock_ConsumesWholeBlocks(t *testing.T) {
	t.Parallel()

	data, planes := mmrPlanes(t)
	off := 0
	for k, want := range []int{60, 55} {
		got, used, err := decodeMMRBlock(context.Background(), 23, 9, data[off:])
		if err != nil {
			t.Fatalf("plane %d: %v", k, err)
		}
		if used != want {
			t.Errorf("plane %d used %d bytes, want %d", k, used, want)
		}
		if !bytes.Equal(got.Data, planes[k].Data) {
			t.Errorf("plane %d differs from libtiff's source in %d bytes", k, differingBytes(got.Data, planes[k].Data))
		}
		off += used
	}
}

// TestDecodeGrayScale_MMR decodes the two libtiff blocks as a two-bit
// gray-scale image, C.5: the first block is the most significant plane and the
// second is Gray-coded against it.
func TestDecodeGrayScale_MMR(t *testing.T) {
	t.Parallel()

	data, planes := mmrPlanes(t)
	got, err := decodeGrayScale(context.Background(), grayParams{width: 23, height: 9, bits: 2, mmr: true}, data, &budget{})
	if err != nil {
		t.Fatalf("decodeGrayScale: %v", err)
	}
	for y := range 9 {
		for x := range 23 {
			msb := planes[0].pixel(x, y)
			lsb := msb ^ planes[1].pixel(x, y)
			if want := int(msb<<1 | lsb); got[y*23+x] != want {
				t.Fatalf("value at (%d,%d) = %d, want %d", x, y, got[y*23+x], want)
			}
		}
	}
}

// halftonePatterns returns five 4x3 patterns of increasing darkness.
func halftonePatterns() []*Bitmap {
	patterns := make([]*Bitmap, 5)
	for i := range patterns {
		bm, err := newBitmap(4, 3)
		if err != nil {
			panic(err)
		}
		for k := range i * 3 {
			bm.setPixel((k*7)%4, (k*5)%3, 1)
		}
		patterns[i] = bm
	}
	return patterns
}

// drawHalftone draws every grid cell's pattern at its corner, 6.6.5.2, with
// the region's operator, as the expected result of a round trip.
func drawHalftone(h encHalftone, patterns []*Bitmap, values []int) *Bitmap {
	out, err := newBitmap(h.width, h.height)
	if err != nil {
		panic(err)
	}
	out.fill(h.defPixel)
	for mg := range h.gridH {
		for ng := range h.gridW {
			p := patterns[values[mg*h.gridW+ng]]
			cx, cy := encCell(h, ng, mg)
			for py := range p.Height {
				for px := range p.Width {
					x, y := cx+px, cy+py
					if x < 0 || y < 0 || x >= h.width || y >= h.height {
						continue
					}
					d, s := out.pixel(x, y), p.pixel(px, py)
					var v uint32
					switch h.combineOp {
					case combineOr:
						v = d | s
					case combineAnd:
						v = d & s
					case combineXor:
						v = d ^ s
					case combineXnor:
						v = 1 ^ d ^ s
					default:
						v = s
					}
					out.setPixel(x, y, v)
				}
			}
		}
	}
	return out
}

// TestDecode_HalftoneRoundTrip decodes halftone regions coded by the test-only
// encoder: a pattern dictionary, then gray-scale values as Gray-coded
// bitplanes on an axis-aligned grid, and on a skewed grid that overhangs the
// region, with HSKIP enabled so the overhanging cells are skipped. PDFium
// (through pdf.js 6.3) decoded these streams to the expected drawing when this
// test was written, so they are valid JBIG2 and not merely what this package
// expects.
func TestDecode_HalftoneRoundTrip(t *testing.T) {
	t.Parallel()

	patterns := halftonePatterns()
	for _, h := range []encHalftone{
		{width: 32, height: 21, template: 0, combineOp: combineOr, gridW: 8, gridH: 7, vectorX: 4 << 8},
		{
			width: 27, height: 19, template: 1, combineOp: combineXor, defPixel: 1, gridW: 9, gridH: 8,
			gridX: -3<<8 + 17, gridY: 2 << 8, vectorX: 4<<8 + 40, vectorY: 60, enableSkip: true,
		},
		{
			width: 30, height: 20, template: 2, combineOp: combineAnd, defPixel: 1, gridW: 10, gridH: 9,
			gridX: 1 << 7, gridY: -1 << 8, vectorX: 3<<8 + 200, vectorY: 30, enableSkip: true,
		},
		{width: 25, height: 25, template: 3, combineOp: combineReplace, gridW: 7, gridH: 9, vectorX: 3 << 8},
		// The first column and row of cells end exactly on the region's left
		// and top edges (x + HPW = 0, y + HPH = 0), the boundary 6.6.5.1
		// counts as outside.
		{
			width: 20, height: 15, template: 0, combineOp: combineOr, gridW: 7, gridH: 6,
			gridX: -4 << 8, gridY: -3 << 8, vectorX: 4 << 8, enableSkip: true,
		},
	} {
		name := fmt.Sprintf("template %d, op %d, skip %v", h.template, h.combineOp, h.enableSkip)
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			r := rand.New(rand.NewPCG(uint64(h.template), 9))
			values := make([]int, h.gridW*h.gridH)
			for i := range values {
				values[i] = r.IntN(len(patterns))
			}
			page := binary.BigEndian.AppendUint32(nil, uint32(h.width))
			page = binary.BigEndian.AppendUint32(page, uint32(h.height))
			page = append(page, make([]byte, 11)...)
			coded := halftoneSegment(2, 1, h, values, len(patterns), 4, 3)
			stream := concat(
				segmentBytes(0, typePageInformation, 1, page),
				patternDictSegment(1, patterns, h.template),
				coded,
			)
			// Skipped cells hold 0 after coding (halftoneSegment zeroes them),
			// and pattern 0 drawn wholly outside the region changes nothing.
			want := drawHalftone(h, patterns, values)
			got, err := Decode(context.Background(), stream, nil, DefaultMaxBitmapBytes)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			// Pixels, not bytes: a region with default pixel 1 fills its row
			// padding too, and the page it is drawn onto keeps its own.
			if n := differingPixels(got, want); n != 0 {
				t.Errorf("region differs from the patterns drawn on the grid in %d pixels", n)
			}
		})
	}
}

// differingPixels counts the pixels at which two bitmaps of one size differ,
// ignoring the padding bits that end each row.
func differingPixels(a, b *Bitmap) int {
	if a.Width != b.Width || a.Height != b.Height {
		return a.Width*a.Height + b.Width*b.Height
	}
	n := 0
	for y := range a.Height {
		for x := range a.Width {
			if a.pixel(x, y) != b.pixel(x, y) {
				n++
			}
		}
	}
	return n
}
