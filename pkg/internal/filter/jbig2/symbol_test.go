package jbig2

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/filter/mq"
)

// readPBM reads a binary PBM, whose rows share the layout of [Bitmap.Data].
func readPBM(t *testing.T, path string) *Bitmap {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	fields := strings.Fields(string(b[:min(len(b), 32)]))
	if len(fields) < 3 || fields[0] != "P4" {
		t.Fatalf("%s is not a binary PBM", path)
	}
	w, errW := strconv.Atoi(fields[1])
	h, errH := strconv.Atoi(fields[2])
	if errW != nil || errH != nil {
		t.Fatalf("%s has a malformed header", path)
	}
	header := len(fmt.Sprintf("P4\n%d %d\n", w, h))
	bm := &Bitmap{Width: w, Height: h, Stride: (w + 7) / 8, Data: b[header:]}
	if len(bm.Data) != bm.Stride*h {
		t.Fatalf("%s holds %d bytes of pixels, want %d", path, len(bm.Data), bm.Stride*h)
	}
	return bm
}

// TestDecode_AnnexH1Page3 decodes page 3 of the ITU-T T.88 Annex H.1
// datastream: a global symbol dictionary, a dictionary refining and
// aggregating its symbols, and a text region that refines one of them. The
// golden is jbig2dec 0.20's decode of the same stream.
func TestDecode_AnnexH1Page3(t *testing.T) {
	t.Parallel()

	raw := annexH1(t)
	want := readPBM(t, "testdata/t88_annex_h1_page3.pbm")
	got, err := Decode(context.Background(), concat(raw[15], raw[16], raw[17], raw[18], raw[19]), nil)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Width != want.Width || got.Height != want.Height || !bytes.Equal(got.Data, want.Data) {
		t.Errorf("page 3 is %dx%d and differs from the golden in %d bytes", got.Width, got.Height, differingBytes(got.Data, want.Data))
	}
}

// TestDecode_ISO32000Example decodes the JBIG2Decode example of ISO 32000-1,
// 7.4.7: a symbol dictionary held in the JBIG2Globals stream and a page
// holding one text region. The golden is jbig2dec 0.20's decode.
func TestDecode_ISO32000Example(t *testing.T) {
	t.Parallel()

	page, errP := os.ReadFile("testdata/iso32000_jbig2_page.jb2")
	globals, errG := os.ReadFile("testdata/iso32000_jbig2_globals.jb2")
	if errP != nil || errG != nil {
		t.Fatalf("reading the example: %v %v", errP, errG)
	}
	want := readPBM(t, "testdata/iso32000_jbig2.pbm")
	got, err := Decode(context.Background(), page, globals)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Width != 52 || got.Height != 66 || !bytes.Equal(got.Data, want.Data) {
		t.Errorf("image is %dx%d and differs from the golden in %d bytes", got.Width, got.Height, differingBytes(got.Data, want.Data))
	}
}

// TestIntDecoder_RoundTrip pins the ranges of ITU-T T.88 Table A.1 against a
// separate transcription in the test encoder, at every range boundary and for
// OOB, so a wrong prefix length, bit count or offset shows up.
func TestIntDecoder_RoundTrip(t *testing.T) {
	t.Parallel()

	values := []int64{0, 1, 3, 4, 19, 20, 83, 84, 339, 340, 4435, 4436, 1 << 20, 4436 + 1<<32 - 1}
	var all []int64
	for _, v := range values {
		all = append(all, v)
		if v != 0 {
			all = append(all, -v)
		}
	}
	e := newMQEncoder()
	var enc encInt
	for _, v := range all {
		enc.encode(e, v, false)
		enc.encode(e, 0, true)
	}
	dec := mq.NewDecoder(e.flush())
	var d intDecoder
	for _, want := range all {
		if got, ok := d.decode(dec); !ok || got != want {
			t.Errorf("decoded %d (ok %v), want %d", got, ok, want)
		}
		if got, ok := d.decode(dec); ok {
			t.Errorf("decoded %d where OOB was coded", got)
		}
	}
}

// TestIAID_RoundTrip pins A.3: IDs of every code length decode to themselves.
func TestIAID_RoundTrip(t *testing.T) {
	t.Parallel()

	for codeLen := range 6 {
		e := newMQEncoder()
		enc := newEncIAID(codeLen)
		for id := range 1 << codeLen {
			enc.encode(e, id)
		}
		dec := mq.NewDecoder(e.flush())
		d := newIAID(codeLen)
		for want := range 1 << codeLen {
			if got := d.decode(dec); got != int64(want) {
				t.Errorf("code length %d: decoded %d, want %d", codeLen, got, want)
			}
		}
	}
}

// TestDecode_RefinementRoundTrip decodes refinement regions coded by the
// test-only encoder, with both templates, with and without TPGR, and with the
// AT pixels at their nominal and at moved locations.
//
// The dotted image puts pixels in the SLTP context of Figures 14 and 15. These
// streams were also decoded by PDFium (through pdf.js 6.3) when this test was
// written, and it reproduced every target. poppler and CoreGraphics were not
// used: on TPGR fixtures both disagree with PDFium and litho.
//
// # Corpus evidence
//
//   - pdfjs: on the TPGR fixtures, poppler and CoreGraphics both disagree
//     with PDFium, litho and the image every other fixture of that set
//     encodes.
func TestDecode_RefinementRoundTrip(t *testing.T) {
	t.Parallel()

	for _, ref := range []struct {
		name string
		bm   *Bitmap
	}{
		{"textured", testImage(83, 61, 11)},
		{"dotted", dotsImage(83, 61)},
	} {
		for template := range 2 {
			for _, tpgron := range []bool{false, true} {
				for _, at := range [][2][2]int{{{-1, -1}, {-1, -1}}, {{3, -2}, {-5, 4}}} {
					if template == 1 && at != [2][2]int{{-1, -1}, {-1, -1}} {
						continue
					}
					name := fmt.Sprintf("%s, template %d, TPGRON %v, AT %v", ref.name, template, tpgron, at)
					t.Run(name, func(t *testing.T) {
						t.Parallel()

						target := refinedImage(ref.bm, 5)
						stream := refinementStream(ref.bm, template, tpgron, at, encodeRefinement(target, ref.bm, template, tpgron, at))
						got, err := Decode(context.Background(), stream, nil)
						if err != nil {
							t.Fatalf("Decode: %v", err)
						}
						if !bytes.Equal(got.Data, target.Data) {
							t.Errorf("refined image differs from the target in %d bytes", differingBytes(got.Data, target.Data))
						}
					})
				}
			}
		}
	}
}

// textSymbols returns three small, asymmetric symbols, so a symbol placed by
// the wrong corner or transposed lands visibly elsewhere.
func textSymbols() []*Bitmap {
	shapes := []struct {
		w, h int
		rows []string
	}{
		{5, 7, []string{"11111", "1....", "1....", "1111.", "1....", "1....", "1...."}},
		{3, 4, []string{"111", "..1", ".1.", "1.."}},
		{6, 2, []string{"1.1.11", "11.1.."}},
	}
	syms := make([]*Bitmap, len(shapes))
	for i, s := range shapes {
		bm, err := newBitmap(s.w, s.h)
		if err != nil {
			panic(err)
		}
		for y, row := range s.rows {
			for x, c := range row {
				if c == '1' {
					bm.setPixel(x, y, 1)
				}
			}
		}
		syms[i] = bm
	}
	return syms
}

// drawInstances draws each instance with its reference corner at (S, T), or at
// (T, S) when transposed, as T.88 6.4.5 step 3 c viii defines placement.
func drawInstances(p textParams, instances []encInstance) *Bitmap {
	out, err := newBitmap(p.width, p.height)
	if err != nil {
		panic(err)
	}
	out.fill(p.defPixel)
	for _, in := range instances {
		sym := p.syms[in.id]
		cx, cy := in.s, in.t
		if p.transposed {
			cx, cy = in.t, in.s
		}
		// The symbol pixel sitting on (cx, cy) is its reference corner.
		var kx, ky int64
		if p.refCorner == cornerTopRight || p.refCorner == cornerBottomRight {
			kx = int64(sym.Width - 1)
		}
		if p.refCorner == cornerBottomLeft || p.refCorner == cornerBottomRight {
			ky = int64(sym.Height - 1)
		}
		for sy := range sym.Height {
			for sx := range sym.Width {
				x, y := cx-kx+int64(sx), cy-ky+int64(sy)
				if x < 0 || y < 0 || x >= int64(p.width) || y >= int64(p.height) {
					continue
				}
				v := sym.pixel(sx, sy)
				if p.combineOp == combineXor {
					v ^= out.pixel(int(x), int(y))
				} else {
					v |= out.pixel(int(x), int(y))
				}
				out.setPixel(int(x), int(y), v)
			}
		}
	}
	return out
}

// TestDecodeText_RoundTrip decodes text regions coded by the test-only
// encoder under every reference corner, both strip directions, strip sizes 1
// and 4, OR and XOR, and a negative SBDSOFFSET, and compares them with the
// instances drawn directly at their corners.
func TestDecodeText_RoundTrip(t *testing.T) {
	t.Parallel()

	instances := []encInstance{
		{0, 3, 9},
		{1, 10, 9},
		{2, 15, 8},
		{0, 24, 10},
		{2, 4, 17},
		{1, 12, 18},
		{0, 19, 19},
		{1, 2, 27},
		{0, 9, 26},
		{2, 30, 25},
		{1, 20, 11},
		{0, 26, 9},
	}
	for corner := range 4 {
		for _, transposed := range []bool{false, true} {
			for _, strips := range []int64{1, 4} {
				for _, op := range []int{combineOr, combineXor} {
					for _, dsOffset := range []int64{0, -3} {
						p := textParams{
							width: 40, height: 40,
							numInstances: int64(len(instances)),
							strips:       strips,
							syms:         textSymbols(),
							combineOp:    op,
							transposed:   transposed,
							refCorner:    corner,
							dsOffset:     dsOffset,
						}
						name := fmt.Sprintf("corner %d, transposed %v, strips %d, op %d, dsoffset %d", corner, transposed, strips, op, dsOffset)
						t.Run(name, func(t *testing.T) {
							t.Parallel()

							st := &arithState{
								dec:    mq.NewDecoder(encodeText(p, instances)),
								iaid:   newIAID(symbolCodeLen(len(p.syms))),
								gr:     make([]mq.Context, refineContexts(0)),
								budget: &budget{},
							}
							got, err := decodeText(context.Background(), p, st)
							if err != nil {
								t.Fatalf("decodeText: %v", err)
							}
							want := drawInstances(p, instances)
							if !bytes.Equal(got.Data, want.Data) {
								t.Errorf("region differs from the instances drawn at their corners in %d bytes", differingBytes(got.Data, want.Data))
							}
						})
					}
				}
			}
		}
	}
}

// TestSymbolDict_RetainedContexts pins ITU-T T.88 E.3.8 and 7.4.2.2 steps 3
// and 7: a dictionary whose "bitmap coding context used" bit is set starts
// from the generic statistics the dictionary it refers to retained, not from
// reset ones. The second dictionary here is coded with the first's statistics,
// so it decodes to its own symbols only if they are carried over.
func TestSymbolDict_RetainedContexts(t *testing.T) {
	t.Parallel()

	first := []*Bitmap{testImage(6, 9, 21), testImage(8, 9, 22), testImage(4, 11, 23)}
	second := []*Bitmap{testImage(7, 9, 24), testImage(5, 12, 25), testImage(9, 12, 26)}
	gb := make([]encContext, genericContexts(2))
	segA := symbolDictSegment(0, nil, first, 0, false, true, gb)
	segB := symbolDictSegment(1, []uint32{0}, second, len(first), true, false, gb)

	d := &decoder{}
	for _, raw := range [][]byte{segA, segB} {
		seg, _, err := parseSegment(raw)
		if err != nil {
			t.Fatalf("parseSegment: %v", err)
		}
		if err := d.process(context.Background(), seg); err != nil {
			t.Fatalf("process: %v", err)
		}
	}
	for i, want := range second {
		got := d.dicts[1].exported[i]
		if got.Width != want.Width || got.Height != want.Height || !bytes.Equal(got.Data, want.Data) {
			t.Errorf("second dictionary symbol %d is %dx%d and differs from its source in %d bytes",
				i, got.Width, got.Height, differingBytes(got.Data, want.Data))
		}
	}
}
