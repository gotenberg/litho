package typeset_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"regexp"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
	"github.com/gotenberg/litho/pkg/pdf"
)

// solidPNG encodes a w by h picture of one colour.
func solidPNG(t testing.TB, w, h int, c color.NRGBA) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetNRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// colourFaces builds two colour faces over the glyphs A to D of 1000
// units: a bitmap face whose A is a red square and B a blue one, 10
// pixels at 10 pixels per em from the baseline up, C without a bitmap; and
// a layered face whose D draws glyph A in palette entry 0, green, then
// glyph B in the text colour.
func colourFaces(t testing.TB) (bitmaps, layers *typeset.Face) {
	t.Helper()
	glyphs := func() []fontbuild.Glyph {
		var g []fontbuild.Glyph
		for _, r := range "ABCD" {
			g = append(g, fontbuild.Glyph{Name: string(r), Unicode: r, Advance: 1000, Empty: true})
		}
		return g
	}
	cblc, cbdt := fontbuild.ColorBitmaps(10, map[uint16]fontbuild.BitmapGlyph{
		1: {PNG: solidPNG(t, 10, 10, color.NRGBA{R: 255, A: 255}), Width: 10, Height: 10, Top: 10, Advance: 10},
		2: {PNG: solidPNG(t, 10, 10, color.NRGBA{B: 255, A: 255}), Width: 10, Height: 10, Top: 10, Advance: 10},
	})
	bm := fontbuild.TrueType{PostScriptName: "BitmapColour", Family: "BitmapColour", Subfamily: "Regular", Glyphs: glyphs(), NotdefAdvance: 1000,
		Extra: map[string][]byte{"CBLC": cblc, "CBDT": cbdt}}.Build()

	lg := glyphs()
	lg[0].Empty, lg[1].Empty = false, false
	colr, cpal := fontbuild.ColorLayers(map[uint16][]fontbuild.Layer{4: {{GID: 1, Palette: 0}, {GID: 2, Palette: 0xFFFF}}},
		[][][4]uint8{{{0, 255, 0, 255}}})
	ly := fontbuild.TrueType{PostScriptName: "LayerColour", Family: "LayerColour", Subfamily: "Regular", Glyphs: lg, NotdefAdvance: 1000,
		Extra: map[string][]byte{"COLR": colr, "CPAL": cpal}}.Build()
	return &typeset.Face{Program: bm, Key: "bitmaps"}, &typeset.Face{Program: ly, Key: "layers"}
}

// TestColourGlyphs_BitmapsAsType3Images locks how a colour bitmap glyph is
// written: a glyph of a Type 3 font whose description draws its image
// (ISO 32000-1, 9.6.5), described and stored once however often it is
// drawn, extracted as the run's text, and painted in its own colours.
func TestColourGlyphs_BitmapsAsType3Images(t *testing.T) {
	bitmaps, _ := colourFaces(t)
	txt := run(t, bitmaps, 72, typeset.Point{X: 72, Y: 144}, "ABAB")
	out := write(t, &typeset.Document{Pages: []*typeset.Page{letter(txt)}})
	if n := bytes.Count(out, []byte("/Subtype /Type3")); n != 1 {
		t.Errorf("%d Type 3 fonts, want 1", n)
	}
	if n := bytes.Count(out, []byte("/Subtype /Image")); n != 2 {
		t.Errorf("%d image XObjects, want 2: each bitmap stored once", n)
	}
	content := inflated(t, out)
	if n := bytes.Count(content, []byte(" d0\n")); n != 2 {
		t.Errorf("%d coloured glyph descriptions, want 2", n)
	}
	if !bytes.Contains(content, []byte("<00010001> TJ")) && !bytes.Contains(content, []byte("[<00010001>] TJ")) {
		t.Errorf("the run's codes are not shown by TJ:\n%s", content)
	}
	r := open(t, out)
	got, err := r.ExtractText(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "ABAB") {
		t.Errorf("extracted %q, want ABAB", got)
	}
	img := render(t, r, 1)
	// A is a red square of one em, 72 points, from the baseline up.
	if !near(img, 72+36, 144-36, color.NRGBA{R: 255, A: 255}, 8) {
		t.Errorf("A at (108, 108) = %v, want red", img.At(108, 108))
	}
	if !near(img, 144+36, 144-36, color.NRGBA{B: 255, A: 255}, 8) {
		t.Errorf("B at (180, 108) = %v, want blue", img.At(180, 108))
	}
}

// TestColourGlyphs_FontDescriptorByVersion locks which files give a Type
// 3 font its font descriptor (ISO 32000-1, Table 112): all but a PDF 1.4
// file, tagged or not, whose Type 3 font dictionary has no such entry.
func TestColourGlyphs_FontDescriptorByVersion(t *testing.T) {
	bitmaps, _ := colourFaces(t)
	for _, tc := range []struct {
		name    string
		version typeset.Version
		tagged  bool
		want    int
	}{
		{"PDF 1.4", typeset.Version14, false, 0},
		{"PDF 1.4, tagged", typeset.Version14, true, 0},
		{"PDF 1.7, tagged", typeset.Version17, true, 1},
		{"PDF 1.7", typeset.Version17, false, 1},
		{"PDF 2.0", typeset.Version20, false, 1},
	} {
		txt := run(t, bitmaps, 72, typeset.Point{X: 72, Y: 144}, "AB")
		doc := &typeset.Document{Pages: []*typeset.Page{letter(txt)}}
		if tc.tagged {
			p := &typeset.StructElem{Role: "P"}
			txt.Parent = p
			doc.Metadata = typeset.Metadata{Title: "Tagged", Lang: "en-US"}
			doc.Structure = &typeset.StructElem{Role: "Document", Lang: "en-US", Kids: []*typeset.StructElem{p}}
		}
		out := writeVersion(t, doc, tc.version)
		if n := bytes.Count(out, []byte("/Subtype /Type3")); n != 1 {
			t.Fatalf("%s: %d Type 3 fonts, want 1", tc.name, n)
		}
		if n := bytes.Count(out, []byte("/Type /FontDescriptor")); n != tc.want {
			t.Errorf("%s: %d font descriptors, want %d", tc.name, n, tc.want)
		}
	}
}

// TestColourGlyphs_GlyphWithoutBitmapAdvances locks that a glyph of a
// bitmap face without a bitmap draws nothing and keeps its advance and its
// text.
func TestColourGlyphs_GlyphWithoutBitmapAdvances(t *testing.T) {
	bitmaps, _ := colourFaces(t)
	txt := run(t, bitmaps, 72, typeset.Point{X: 72, Y: 144}, "CA")
	out := write(t, &typeset.Document{Pages: []*typeset.Page{letter(txt)}})
	r := open(t, out)
	img := render(t, r, 1)
	if !near(img, 144+36, 144-36, color.NRGBA{R: 255, A: 255}, 8) {
		t.Errorf("A after C at (180, 108) = %v, want red", img.At(180, 108))
	}
	if near(img, 72+36, 144-36, color.NRGBA{R: 255, A: 255}, 8) {
		t.Error("C draws ink")
	}
	got, err := r.ExtractText(context.Background(), 1)
	if err != nil || !strings.Contains(got, "CA") {
		t.Errorf("extracted %q, %v, want CA", got, err)
	}
}

// TestColourGlyphs_LayersInPaletteAndTextColour locks the drawing of a
// COLR glyph (OpenType, COLR version 0): its layers bottom first, entry 0
// of the face's first palette or of the run's palette, the foreground
// entry in the run's colour, and a glyph without layers through the Type
// 0 font.
func TestColourGlyphs_LayersInPaletteAndTextColour(t *testing.T) {
	_, layers := colourFaces(t)
	plain := run(t, layers, 72, typeset.Point{X: 72, Y: 144}, "DA")
	plain.Color = typeset.Color{R: 1, A: 1}
	repainted := run(t, layers, 72, typeset.Point{X: 72, Y: 288}, "D")
	repainted.Palette = []typeset.Color{{B: 1, A: 1}}
	out := write(t, &typeset.Document{Pages: []*typeset.Page{letter(plain, repainted)}})
	if !bytes.Contains(out, []byte("/Subtype /Type0")) {
		t.Error("the glyph without layers is not drawn through the Type 0 font")
	}
	if n := bytes.Count(out, []byte("/Subtype /Type3")); n != 2 {
		t.Errorf("%d Type 3 fonts, want 2: one a palette", n)
	}
	img := render(t, open(t, out), 1)
	// Glyph A, a rectangle from the baseline to 500 units, is the lower
	// layer; glyph B, of the same box, covers it in the text colour.
	if !near(img, 72+20, 144-10, color.NRGBA{R: 255, A: 255}, 8) {
		t.Errorf("D's top layer at (92, 134) = %v, want the run's red", img.At(92, 134))
	}
	if !near(img, 144+20, 144-10, color.NRGBA{R: 255, A: 255}, 8) {
		t.Errorf("A after D at (164, 134) = %v, want the run's red", img.At(164, 134))
	}
	content := inflated(t, out)
	if !regexp.MustCompile(`0 1 0 rg`).Match(content) || !regexp.MustCompile(`0 0 1 rg`).Match(content) {
		t.Errorf("the layer is not filled in green, then in the run's palette blue:\n%s", content)
	}
}

// TestColourGlyphs_InvisibleRunDrawsNothing locks that an invisible run of
// a colour face stays text and paints nothing.
func TestColourGlyphs_InvisibleRunDrawsNothing(t *testing.T) {
	bitmaps, _ := colourFaces(t)
	txt := run(t, bitmaps, 72, typeset.Point{X: 72, Y: 144}, "AB")
	txt.Invisible = true
	out := write(t, &typeset.Document{Pages: []*typeset.Page{letter(txt)}})
	if bytes.Contains(out, []byte("/Subtype /Image")) {
		t.Error("an invisible run wrote its bitmaps")
	}
	got, err := open(t, out).ExtractText(context.Background(), 1, pdf.WithInvisibleText())
	if err != nil || !strings.Contains(got, "AB") {
		t.Errorf("extracted %q, %v, want AB", got, err)
	}
}

// TestColourGlyphs_PlainRunTakesNoColourData locks [typeset.Text.Plain]:
// a plain run of a layered face draws each glyph's own outline through the
// Type 0 font in the run's colour, a glyph whose outline is empty drawing
// nothing, and a plain run of a face of bitmaps alone draws blank glyphs
// of their advance; neither writes a glyph image or a layer's colour, and
// both stay text. A run of the same face that is not plain, in the same
// document, keeps its colour glyphs.
func TestColourGlyphs_PlainRunTakesNoColourData(t *testing.T) {
	bitmaps, layers := colourFaces(t)
	blue := typeset.Color{B: 1, A: 1}
	plainLayers := run(t, layers, 72, typeset.Point{X: 72, Y: 144}, "DA")
	plainLayers.Color, plainLayers.Plain = blue, true
	plainBitmaps := run(t, bitmaps, 72, typeset.Point{X: 72, Y: 288}, "AB")
	plainBitmaps.Plain = true
	out := write(t, &typeset.Document{Pages: []*typeset.Page{letter(plainLayers, plainBitmaps)}})
	if bytes.Contains(out, []byte("/Subtype /Image")) {
		t.Error("a plain run wrote a glyph image")
	}
	if !bytes.Contains(out, []byte("/Subtype /Type0")) {
		t.Error("the plain run of the layered face is not drawn through the Type 0 font")
	}
	if content := inflated(t, out); regexp.MustCompile(`0 1 0 rg`).Match(content) {
		t.Errorf("a plain run filled a layer in its palette's green:\n%s", content)
	}
	img := render(t, open(t, out), 1)
	// D, whose own outline is empty, paints nothing where its layers
	// would; A, a rectangle from the baseline to 500 units, paints in the
	// run's colour.
	if !near(img, 72+20, 144-10, color.NRGBA{R: 255, G: 255, B: 255, A: 255}, 8) {
		t.Errorf("plain D at (92, 134) = %v, want the page's white", img.At(92, 134))
	}
	if !near(img, 144+20, 144-10, color.NRGBA{B: 255, A: 255}, 8) {
		t.Errorf("plain A at (164, 134) = %v, want the run's blue", img.At(164, 134))
	}
	if !near(img, 72+20, 288-20, color.NRGBA{R: 255, G: 255, B: 255, A: 255}, 8) {
		t.Errorf("plain bitmap glyph at (92, 268) = %v, want the page's white", img.At(92, 268))
	}
	got, err := open(t, out).ExtractText(context.Background(), 1)
	if err != nil || !strings.Contains(got, "DA") || !strings.Contains(got, "AB") {
		t.Errorf("extracted %q, %v, want DA and AB", got, err)
	}

	mixed := run(t, bitmaps, 72, typeset.Point{X: 72, Y: 432}, "AB")
	out = write(t, &typeset.Document{Pages: []*typeset.Page{letter(plainBitmaps, mixed)}})
	if n := bytes.Count(out, []byte("/Subtype /Image")); n == 0 {
		t.Error("the run that is not plain lost its glyph images beside a plain run of its face")
	}
	img = render(t, open(t, out), 1)
	if !near(img, 72+20, 288-20, color.NRGBA{R: 255, G: 255, B: 255, A: 255}, 8) {
		t.Errorf("plain bitmap glyph at (92, 268) = %v beside a coloured run, want the page's white", img.At(92, 268))
	}
	if !near(img, 72+20, 432-20, color.NRGBA{R: 255, A: 255}, 8) {
		t.Errorf("coloured bitmap glyph at (92, 412) = %v, want its red", img.At(92, 412))
	}
}

// TestColourGlyphs_SameBytes locks that a document with colour glyphs is
// written to the same bytes twice.
func TestColourGlyphs_SameBytes(t *testing.T) {
	bitmaps, layers := colourFaces(t)
	doc := func() *typeset.Document {
		return &typeset.Document{Pages: []*typeset.Page{letter(
			run(t, bitmaps, 20, typeset.Point{X: 20, Y: 40}, "ABCBA"),
			run(t, layers, 20, typeset.Point{X: 20, Y: 80}, "DADA"),
		)}}
	}
	if !bytes.Equal(write(t, doc()), write(t, doc())) {
		t.Error("two writes differ")
	}
}
