package mutations

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// extentOfPage1 builds a one-page document stating no media box, drawing ops,
// with the page entries entries, and measures its page.
func extentOfPage1(t *testing.T, ops, entries string) document.ContentExtent {
	t.Helper()
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /Resources << /Font << /F1 << /Type /Font /Subtype /Type1 /BaseFont /Helvetica >> >> " +
			"/XObject << /Im 5 0 R /Fm 6 0 R >> >> /Contents 4 0 R " + entries + " >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(ops), ops),
		"<< /Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8 /Length 1 >>\nstream\n\x00\nendstream",
		"<< /Type /XObject /Subtype /Form /BBox [0 0 100 100] /Length 18 >>\nstream\n0 0 100 100 re f\nendstream",
	}
	doc := attachDoc(t, objs...)
	ctx := context.Background()
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil || len(pages) != 1 {
		t.Fatalf("ResolvePages: %v, %d pages", err, len(pages))
	}
	extent, err := PageContentExtent(ctx, nil, doc, pages[0])
	if err != nil {
		t.Fatalf("PageContentExtent: %v", err)
	}
	return extent
}

// TestPageContentExtent pins what a page paints as the default media box
// reads it: text, paths, images, forms and annotations, cut to the US Letter
// a viewer shows a page with no media box on, and unknown wherever something
// cannot be placed.
func TestPageContentExtent(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, ops, entries string
		known, painted     bool
		// inside and outside are points the extent holds and does not.
		inside, outside [][2]float64
	}{
		{name: "nothing", ops: "", known: true},
		{name: "text", ops: "BT /F1 20 Tf 100 700 Td (Hello) Tj ET", known: true, painted: true,
			inside: [][2]float64{{100, 700}, {140, 715}}, outside: [][2]float64{{50, 600}}},
		// Reflected glyphs advancing rightward ink below the baseline (ISO
		// 32000-1, 9.4.4): a mirrored CTM, and a negative Th with a negative
		// Tfs, which reverse the advance twice and reflect the glyph once.
		{name: "text the CTM mirrors", ops: "q 1 0 0 -1 0 792 cm BT /F1 20 Tf 100 92 Td (Hello) Tj ET Q", known: true, painted: true,
			inside: [][2]float64{{100, 700}, {140, 682}}, outside: [][2]float64{{50, 600}}},
		{name: "text a negative Th mirrors", ops: "BT /F1 -20 Tf -100 Tz 100 700 Td (Hello) Tj ET", known: true, painted: true,
			inside: [][2]float64{{100, 700}, {140, 682}}, outside: [][2]float64{{50, 600}}},
		{name: "a path", ops: "10 20 30 40 re f", known: true, painted: true,
			inside: [][2]float64{{10, 20}, {40, 60}}, outside: [][2]float64{{100, 100}}},
		{name: "an image", ops: "q 50 0 0 50 300 400 cm /Im Do Q", known: true, painted: true,
			inside: [][2]float64{{300, 400}, {350, 450}}, outside: [][2]float64{{200, 200}}},
		{name: "a form", ops: "q 1 0 0 1 400 500 cm /Fm Do Q", known: true, painted: true,
			inside: [][2]float64{{400, 500}, {500, 600}}, outside: [][2]float64{{200, 200}}},
		{name: "an annotation", ops: "", entries: "/Annots [<< /Type /Annot /Subtype /Square /Rect [500 700 550 750] >>]", known: true, painted: true,
			inside: [][2]float64{{500, 700}, {550, 750}}, outside: [][2]float64{{100, 100}}},
		{name: "cut to what a viewer shows", ops: "0 0 2000 2000 re f", known: true, painted: true,
			inside: [][2]float64{{0, 0}, {612, 792}}, outside: [][2]float64{{700, 700}}},
		{name: "an XObject that does not resolve", ops: "/Missing Do", known: false},
		{name: "a shading", ops: "/Sh sh", known: false},
		// The extraction substitutes a font for one the page does not name,
		// as a viewer does, so the text is still placed.
		{name: "text in a font the page does not name", ops: "BT /F9 20 Tf 100 700 Td (Hello) Tj ET", known: true, painted: true,
			inside: [][2]float64{{100, 700}}, outside: [][2]float64{{50, 600}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := extentOfPage1(t, tc.ops, tc.entries)
			if e.Known != tc.known || e.Painted != tc.painted {
				t.Fatalf("extent %+v, want known %v, painted %v", e, tc.known, tc.painted)
			}
			for _, p := range tc.inside {
				if p[0] < e.Box[0] || p[1] < e.Box[1] || p[0] > e.Box[2] || p[1] > e.Box[3] {
					t.Errorf("extent %v misses %v", e.Box, p)
				}
			}
			for _, p := range tc.outside {
				if p[0] >= e.Box[0] && p[1] >= e.Box[1] && p[0] <= e.Box[2] && p[1] <= e.Box[3] {
					t.Errorf("extent %v holds %v, which nothing paints", e.Box, p)
				}
			}
		})
	}
}

// TestPageContentExtent_Canceled pins that a cancelled context stops the
// measure.
func TestPageContentExtent_Canceled(t *testing.T) {
	t.Parallel()
	doc := attachDoc(t,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R >>",
	)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dict := ast.PdfDict{"Type": ast.PdfName("Page")}
	if _, err := PageDictContentExtent(ctx, nil, doc, ast.PdfIndirectRef{ObjectNum: 3}, dict, nil); err == nil {
		t.Error("PageDictContentExtent on a cancelled context returned no error")
	}
}
