package typography

import (
	"bytes"
	"context"
	"strconv"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/models"
)

// nestedType3PDF shows, at 100 400 and Tf 10, the glyphs a and b of a Type 3
// font, object 7, whose /FontBBox spans ten of its thousandth-of-an-em
// units. Glyph a shows a Helvetica x two ems right of its origin; glyph b
// shows itself, a cycle the renderer stops only at its nesting bound.
func nestedType3PDF() []byte {
	stream := func(num int, data string) string {
		return strconv.Itoa(num) + " 0 obj\n<< /Length " + strconv.Itoa(len(data)) + " >>\nstream\n" + data + "\nendstream\nendobj\n"
	}
	return []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /T3 7 0 R >> >> /Contents 6 0 R >>\nendobj\n" +
		"4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
		stream(6, "BT /T3 10 Tf 100 400 Td (ab) Tj ET") +
		"7 0 obj\n<< /Type /Font /Subtype /Type3 /FontBBox [0 0 10 10] /FontMatrix [0.001 0 0 0.001 0 0] " +
		"/CharProcs << /a 8 0 R /b 9 0 R >> /Encoding << /Type /Encoding /Differences [97 /a /b] >> " +
		"/FirstChar 97 /LastChar 98 /Widths [1000 1000] /Resources << /Font << /H 4 0 R /T 7 0 R >> >> >>\nendobj\n" +
		stream(8, "1000 0 d0 BT /H 1000 Tf 2000 0 Td (x) Tj ET") +
		stream(9, "1000 0 d0 BT /T 1000 Tf (b) Tj ET") +
		"trailer\n<< /Size 10 /Root 1 0 R >>\n%%EOF\n")
}

// mutualType3PDF shows one glyph of each of two Type 3 fonts, objects 7 and
// 8, of glyphs glyphs each, every glyph of either showing text in both, so
// that a walk resolving each font again for every glyph that names it costs
// glyphs to the power of its nesting bound.
func mutualType3PDF(glyphs int) []byte {
	var objs, procs, diffs, widths string
	next := 10
	for i := range glyphs {
		name := "g" + strconv.Itoa(i)
		data := "1000 0 d0 0 0 500 500 re f BT /A 1000 Tf <01> Tj /B 1000 Tf <02> Tj ET"
		objs += strconv.Itoa(next) + " 0 obj\n<< /Length " + strconv.Itoa(len(data)) + " >>\nstream\n" + data + "\nendstream\nendobj\n"
		procs += "/" + name + " " + strconv.Itoa(next) + " 0 R "
		diffs += "/" + name + " "
		widths += "1000 "
		next++
	}
	font := func(num int) string {
		return strconv.Itoa(num) + " 0 obj\n<< /Type /Font /Subtype /Type3 /FontBBox [0 0 1000 1000] /FontMatrix [0.001 0 0 0.001 0 0] " +
			"/CharProcs << " + procs + ">> /Encoding << /Type /Encoding /Differences [1 " + diffs + "] >> " +
			"/FirstChar 1 /LastChar " + strconv.Itoa(glyphs) + " /Widths [" + widths + "] /Resources << /Font << /A 7 0 R /B 8 0 R >> >> >>\nendobj\n"
	}
	body := "BT /A 10 Tf 100 400 Td <01> Tj /B 10 Tf <01> Tj ET"
	return []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /A 7 0 R /B 8 0 R >> >> /Contents 6 0 R >>\nendobj\n" +
		"6 0 obj\n<< /Length " + strconv.Itoa(len(body)) + " >>\nstream\n" + body + "\nendstream\nendobj\n" +
		font(7) + font(8) + objs +
		"trailer\n<< /Size " + strconv.Itoa(next) + " /Root 1 0 R >>\n%%EOF\n")
}

// TestType3MutualFontsResolveOnce bounds the glyphs of two Type 3 fonts that
// show text in each other, 200 glyphs a font. Resolving each font once per
// glyph that names it would take 200^4 resolutions before the nesting bound
// stopped it; the text pass reads only fonts the cache holds, which makes it
// a few resolutions each. The glyphs show themselves, so they paint anywhere
// on the page.
func TestType3MutualFontsResolveOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := mutualType3PDF(200)
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil || len(pages) == 0 {
		t.Fatalf("ResolvePages: %v (%d pages)", err, len(pages))
	}
	ops, err := content.ParsePageContent(ctx, doc, pages[0].Dict)
	if err != nil {
		t.Fatalf("ParsePageContent: %v", err)
	}
	elements, _, _, err := ExtractElementsWithAttribution(ctx, doc, graphics.NewMachine([4]float64{0, 0, 612, 792}), pages[0], ops)
	if err != nil {
		t.Fatalf("ExtractElementsWithAttribution: %v", err)
	}
	if len(elements) != 2 {
		t.Fatalf("got %d elements, want 2", len(elements))
	}
	for _, el := range elements {
		if el.InkBox != [4]float64{0, 0, 612, 792} {
			t.Errorf("glyph ink %v, want the page", el.InkBox)
		}
	}
}

// TestType3GlyphBoundedByTheTextItShows bounds a Type 3 glyph by the text its
// description shows, through the font that text names (ISO 32000-1, 9.6.5),
// not by the font's /FontBBox, and a glyph that shows itself by the clip it
// is shown under, the page.
func TestType3GlyphBoundedByTheTextItShows(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := nestedType3PDF()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil || len(pages) == 0 {
		t.Fatalf("ResolvePages: %v (%d pages)", err, len(pages))
	}
	ops, err := content.ParsePageContent(ctx, doc, pages[0].Dict)
	if err != nil {
		t.Fatalf("ParsePageContent: %v", err)
	}
	elements, _, _, err := ExtractElementsWithAttribution(ctx, doc, graphics.NewMachine([4]float64{0, 0, 612, 792}), pages[0], ops)
	if err != nil {
		t.Fatalf("ExtractElementsWithAttribution: %v", err)
	}
	byText := make(map[string]models.TextElement, len(elements))
	for _, el := range elements {
		byText[el.Text] = el
	}

	// The x sits 2 ems of the nested font right of glyph a's origin, at
	// Tf 1000 in glyph space: 20 points right at Tf 10, about 5 wide, and an
	// em tall, all past the 0.1-point box the font states.
	a := byText["a"].InkBox
	if a[0] > 100.01 || a[2] < 122 || a[2] > 128 || a[3] < 409 || a[3] > 411 {
		t.Errorf("glyph a ink %v, want it to reach the x it shows", a)
	}
	if b := byText["b"].InkBox; b != [4]float64{0, 0, 612, 792} {
		t.Errorf("glyph b ink %v, want the page it may paint anywhere on", b)
	}
}
