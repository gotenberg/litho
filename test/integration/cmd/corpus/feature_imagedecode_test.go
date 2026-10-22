package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func writeImageGolden(t *testing.T, dir string, g imageGolden) {
	t.Helper()
	data, err := json.Marshal(g)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, g.ID+".json"), data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// twoImagePDF returns a one-page PDF painting Im0, a 2x1 DeviceRGB image
// whose left pixel is red and right pixel blue.
func twoImagePDF(t *testing.T) []byte {
	t.Helper()
	content := "q 10 0 0 10 0 0 cm /Im0 Do Q"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 10 10] /Resources << /XObject << /Im0 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /XObject /Subtype /Image /Width 2 /Height 1 /ColorSpace /DeviceRGB /BitsPerComponent 8 /Length 6 >>\nstream\n\xff\x00\x00\x00\x00\xff\nendstream",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objects))
	for i, body := range objects {
		offsets[i] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, off := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return buf.Bytes()
}

func TestImageDecodeFeature_LoadGoldens(t *testing.T) {
	dir := t.TempDir()
	writeImageGolden(t, dir, imageGolden{ID: "a"})
	f := newImageDecodeFeature()
	if err := f.LoadGoldens(dir); err != nil {
		t.Fatalf("LoadGoldens: %v", err)
	}
	if !f.HasGolden("a") {
		t.Errorf("HasGolden(a): got false, want true")
	}
	if f.HasGolden("ghost") {
		t.Errorf("HasGolden(ghost): got true, want false")
	}
}

func TestImageDecodeFeature_LoadGoldens_Empty(t *testing.T) {
	f := newImageDecodeFeature()
	if err := f.LoadGoldens(t.TempDir()); !errors.Is(err, ErrNoGoldens) {
		t.Errorf("err: got %v, want errors.Is(_, ErrNoGoldens)", err)
	}
}

func TestSignatureOf(t *testing.T) {
	// Five columns fall into four cells as gen-images.mjs splits them:
	// floor(x*4/5) puts columns 0 and 1 in the first cell.
	img := image.NewGray(image.Rect(0, 0, 5, 1))
	for x, v := range []uint8{10, 20, 30, 40, 50} {
		img.SetGray(x, 0, color.Gray{Y: v})
	}
	sig := signatureOf(img)
	if sig.Width != 5 || sig.Height != 1 || sig.Grid != [2]int{4, 1} {
		t.Fatalf("size: got %dx%d grid %v, want 5x1 grid [4 1]", sig.Width, sig.Height, sig.Grid)
	}
	want := []float64{15, 15, 15, 30, 30, 30, 40, 40, 40, 50, 50, 50}
	if !slices.Equal(sig.Sig, want) {
		t.Errorf("sig: got %v, want %v", sig.Sig, want)
	}
}

// opaqueImage hides an image's concrete type, so signatureOf reads it through
// At.
type opaqueImage struct{ image.Image }

// TestSignatureOf_PixMatchesAt pins that the Pix reads of Gray and RGBA images
// give the signature At gives, sub-images with an offset origin and a
// translucent RGBA pixel included.
func TestSignatureOf_PixMatchesAt(t *testing.T) {
	gray := image.NewGray(image.Rect(0, 0, 9, 7))
	rgba := image.NewRGBA(image.Rect(0, 0, 9, 7))
	for y := range 7 {
		for x := range 9 {
			gray.SetGray(x, y, color.Gray{Y: uint8(x*29 + y*13)})
			rgba.SetRGBA(x, y, color.RGBA{R: uint8(x * 27), G: uint8(y * 31), B: uint8(x*y*5 + 3), A: 255})
		}
	}
	rgba.SetRGBA(4, 3, color.RGBA{R: 40, G: 30, B: 20, A: 128})
	for _, img := range []image.Image{
		gray, rgba,
		gray.SubImage(image.Rect(2, 1, 8, 6)),
		rgba.SubImage(image.Rect(3, 2, 9, 7)),
	} {
		got, want := signatureOf(img), signatureOf(opaqueImage{img})
		if !slices.Equal(got.Sig, want.Sig) || got.Grid != want.Grid {
			t.Errorf("%T %v: Pix read %v %v, At read %v %v", img, img.Bounds(), got.Grid, got.Sig, want.Grid, want.Sig)
		}
	}
}

// TestSignatureOf_NRGBAStraight pins that an NRGBA image, a JPEG 2000 image
// with the opacity of its data as alpha, is summed by its straight colour, as
// gen-images.mjs sums PDF.js's RGBA buffer: a transparent pixel keeps its
// colour rather than reading black.
func TestSignatureOf_NRGBAStraight(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	img.SetNRGBA(0, 0, color.NRGBA{R: 200, G: 100, B: 50, A: 0})
	img.SetNRGBA(1, 0, color.NRGBA{R: 200, G: 100, B: 50, A: 128})
	sig := signatureOf(img.SubImage(image.Rect(0, 0, 2, 1)))
	want := []float64{200, 100, 50, 200, 100, 50}
	if !slices.Equal(sig.Sig, want) {
		t.Errorf("sig: got %v, want %v", sig.Sig, want)
	}
}

func TestImageDecodeFeature_RunAndCompare(t *testing.T) {
	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "doc.pdf")
	if err := os.WriteFile(pdfPath, twoImagePDF(t), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	goldens := filepath.Join(dir, "goldens")
	if err := os.MkdirAll(goldens, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	red, blue := []float64{255, 0, 0}, []float64{0, 0, 255}
	tests := []struct {
		name  string
		sig   []float64
		match bool
	}{
		{name: "within tolerance", sig: slices.Concat([]float64{253, 1, 0}, blue), match: true},
		{name: "colours swapped", sig: slices.Concat(blue, red), match: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writeImageGolden(t, goldens, imageGolden{ID: "doc", Pages: []imagePageGroup{{
				Page:   1,
				Images: []imageSignature{{Width: 2, Height: 1, Grid: [2]int{2, 1}, Sig: tt.sig}},
			}}})
			f := newImageDecodeFeature()
			if err := f.LoadGoldens(goldens); err != nil {
				t.Fatalf("LoadGoldens: %v", err)
			}
			out, err := f.Run(context.Background(), pdfPath, "doc")
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			v := f.Compare("doc", out)
			if v.Skipped {
				t.Fatalf("Skipped: %s", v.Reason)
			}
			if v.Match != tt.match {
				t.Errorf("Match: got %v, want %v (%s, %s)", v.Match, tt.match, v.Reason, v.Mismatches)
			}
			if v.Metrics["decode_failed"] != 0 {
				t.Errorf("decode_failed: got %d, want 0", v.Metrics["decode_failed"])
			}
		})
	}
}

func TestImageDecodeFeature_Compare(t *testing.T) {
	dir := t.TempDir()
	grey := func(v float64) []float64 { return []float64{v, v, v} }
	writeImageGolden(t, dir, imageGolden{ID: "doc", Pages: []imagePageGroup{{
		Page: 2,
		Images: []imageSignature{
			{Width: 1, Height: 1, Grid: [2]int{1, 1}, Sig: grey(100)},
			{Width: 1, Height: 1, Grid: [2]int{1, 1}, Sig: grey(10)},
			{Width: 3, Height: 3, Grid: [2]int{3, 3}, Sig: slices.Repeat(grey(0), 9)},
		},
	}}})
	writeImageGolden(t, dir, imageGolden{ID: "blank"})
	f := newImageDecodeFeature()
	if err := f.LoadGoldens(dir); err != nil {
		t.Fatalf("LoadGoldens: %v", err)
	}
	// The two 1x1 images match in either order; the 3x3 one failed to decode.
	output, err := json.Marshal(imageGolden{Pages: []imagePageGroup{{
		Page: 2,
		Images: []imageSignature{
			{Width: 1, Height: 1, Grid: [2]int{1, 1}, Sig: grey(11), Name: "Im1"},
			{Width: 1, Height: 1, Grid: [2]int{1, 1}, Sig: grey(99), Name: "Im2"},
			{Width: 3, Height: 3, Name: "Im3", Failed: "unsupported filter"},
		},
	}}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	v := f.Compare("doc", output)
	if v.Match {
		t.Errorf("Match: got true, want false")
	}
	if v.Metrics["images_match"] != 2 || v.Metrics["decode_failed"] != 1 {
		t.Errorf("Metrics: got %v, want images_match 2, decode_failed 1", v.Metrics)
	}
	if len(v.Mismatches) != 1 || !bytes.Contains(v.Mismatches[0], []byte("unsupported filter")) {
		t.Errorf("Mismatches: got %s, want the failed Im3", v.Mismatches)
	}
	if blank := f.Compare("blank", []byte(`{}`)); !blank.Skipped {
		t.Errorf("blank: got Skipped false, want true")
	}
}

// TestPairImages pairs as many golden images as a pairing admits. Golden 12
// and 9 against decoded 11 and 14: taking golden 12's nearest, 11, leaves 9
// with 14, five levels away, while 12 with 14 and 9 with 11 pairs both.
// Identical signatures pair with each other whatever the order.
func TestPairImages(t *testing.T) {
	sig := func(v float64) imageSignature {
		return imageSignature{Width: 1, Height: 1, Grid: [2]int{1, 1}, Sig: []float64{v, v, v}}
	}
	for _, tc := range []struct {
		name      string
		want, got []imageSignature
		pairs     []int
	}{
		{"augmenting", []imageSignature{sig(12), sig(9)}, []imageSignature{sig(11), sig(14)}, []int{1, 0}},
		{"exact first", []imageSignature{sig(10), sig(12)}, []imageSignature{sig(12), sig(10)}, []int{1, 0}},
		{"too far", []imageSignature{sig(10)}, []imageSignature{sig(20)}, []int{-1}},
	} {
		if got := pairImages(tc.want, tc.got); !slices.Equal(got, tc.pairs) {
			t.Errorf("%s: pairs %v, want %v", tc.name, got, tc.pairs)
		}
	}
}

// TestImageDecodeFeature_ShortBuffer scores a golden whose PDF.js buffer held
// 2 of an image's 3 rows: gen-images.mjs stores its last grid row as null.
// Those cells take no part, while a difference in a supplied cell still
// fails, and a null the golden's rows do not explain fails the image.
//
// # Corpus evidence
//
//   - pdfjs issue11678: PDF.js hands Image31 (1192x331) over about 1.2 rows
//     short, and reading its null cells as 0 failed an image litho decodes.
func TestImageDecodeFeature_ShortBuffer(t *testing.T) {
	const supplied = `[10,10,10,20,20,20,30,30,30,40,40,40,null,null,null,null,null,null]`
	decoded := []float64{10, 10, 10, 20, 20, 20, 30, 30, 30, 40, 40, 40, 200, 200, 200, 7, 7, 7}
	for _, tc := range []struct {
		name   string
		golden string
		got    []float64
		match  bool
	}{
		{"unsupplied cells skipped", `{"width":2,"height":3,"grid":[2,3],"rows":2,"sig":` + supplied + `}`, decoded, true},
		{
			"planted difference in a supplied cell", `{"width":2,"height":3,"grid":[2,3],"rows":2,"sig":` + supplied + `}`,
			slices.Concat([]float64{10, 10, 10, 20, 20, 20, 30, 30, 30, 40, 40, 90}, decoded[12:]), false,
		},
		{"null with no rows", `{"width":2,"height":3,"grid":[2,3],"sig":` + supplied + `}`, decoded, false},
		{"null in a supplied grid row", `{"width":2,"height":3,"grid":[2,3],"rows":2,"sig":[null,10,10,20,20,20,30,30,30,40,40,40,null,null,null,null,null,null]}`, decoded, false},
		{"no supplied cell", `{"width":2,"height":3,"grid":[2,1],"rows":2,"sig":[null,null,null,null,null,null]}`, decoded[:6], false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			golden := `{"id":"doc","pages":[{"page":1,"images":[` + tc.golden + `]}]}`
			if err := os.WriteFile(filepath.Join(dir, "doc.json"), []byte(golden), 0o644); err != nil {
				t.Fatalf("write: %v", err)
			}
			f := newImageDecodeFeature()
			if err := f.LoadGoldens(dir); err != nil {
				t.Fatalf("LoadGoldens: %v", err)
			}
			want := f.golden("doc").Pages[0].Images[0]
			output, err := json.Marshal(imageGolden{Pages: []imagePageGroup{{
				Page:   1,
				Images: []imageSignature{{Width: want.Width, Height: want.Height, Grid: want.Grid, Sig: tc.got, Name: "Im0"}},
			}}})
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if v := f.Compare("doc", output); v.Match != tc.match {
				t.Errorf("Match: got %v, want %v (%s, %s)", v.Match, tc.match, v.Reason, v.Mismatches)
			}
		})
	}
}

// TestCellMeans_RoundTrip decodes a null cell as NaN, not 0, and encodes it
// back as null.
func TestCellMeans_RoundTrip(t *testing.T) {
	var m cellMeans
	if err := json.Unmarshal([]byte(`[1.5,null]`), &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(m) != 2 || m[0] != 1.5 || !math.IsNaN(m[1]) {
		t.Fatalf("cells: got %v, want [1.5 NaN]", m)
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if string(data) != `[1.5,null]` {
		t.Errorf("Marshal: got %s, want [1.5,null]", data)
	}
}
