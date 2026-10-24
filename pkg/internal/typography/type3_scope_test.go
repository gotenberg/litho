package typography

import (
	"bytes"
	"context"
	"strconv"
	"sync"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/models"
)

// scopedType3PDF shows, at 100 400 and Tf 10, the glyphs a, b and c of a
// Type 3 font, object 7, stating no /Resources, on page 1 and again inside a
// form XObject whose resources name only the Type 3 font. Glyph a shows a
// Helvetica x the page names /H, two ems right of its origin; glyph b shows
// itself through the page's /T; glyph c paints a square through the page's
// /XO form.
func scopedType3PDF() []byte {
	stream := func(num int, dict, data string) string {
		return strconv.Itoa(num) + " 0 obj\n<< " + dict + " /Length " + strconv.Itoa(len(data)) + " >>\nstream\n" + data + "\nendstream\nendobj\n"
	}
	return []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
		"/Resources << /Font << /T3 7 0 R /H 4 0 R /T 7 0 R >> /XObject << /XO 5 0 R /F 11 0 R >> >> /Contents 6 0 R >>\nendobj\n" +
		"4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
		stream(5, "/Type /XObject /Subtype /Form /BBox [0 0 500 500]", "0 0 500 500 re f") +
		stream(6, "", "BT /T3 10 Tf 100 400 Td (abc) Tj ET /F Do") +
		"7 0 obj\n<< /Type /Font /Subtype /Type3 /FontBBox [0 0 10 10] /FontMatrix [0.001 0 0 0.001 0 0] " +
		"/CharProcs << /a 8 0 R /b 9 0 R /c 10 0 R >> /Encoding << /Type /Encoding /Differences [97 /a /b /c] >> " +
		"/FirstChar 97 /LastChar 99 /Widths [1000 1000 1000] >>\nendobj\n" +
		stream(8, "", "1000 0 d0 BT /H 1000 Tf 2000 0 Td (x) Tj ET") +
		stream(9, "", "1000 0 d0 BT /T 1000 Tf (b) Tj ET") +
		stream(10, "", "1000 0 d0 /XO Do") +
		stream(11, "/Type /XObject /Subtype /Form /BBox [0 0 612 792] /Resources << /Font << /T3 7 0 R >> >>",
			"BT /T3 10 Tf 100 200 Td (ac) Tj ET") +
		"trailer\n<< /Size 12 /Root 1 0 R >>\n%%EOF\n")
}

// scopedType3Elements extracts the elements of scopedType3PDF's page.
func scopedType3Elements(doc *document.Document) ([]models.TextElement, error) {
	ctx := context.Background()
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return nil, err
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		return nil, err
	}
	ops, err := content.ParsePageContent(ctx, doc, pages[0].Dict)
	if err != nil {
		return nil, err
	}
	elements, _, _, err := ExtractElementsWithAttribution(ctx, doc, graphics.NewMachine([4]float64{0, 0, 612, 792}), pages[0], ops)
	return elements, err
}

// TestType3GlyphWithoutResourcesBoundedWhereShown bounds the glyphs of a
// Type 3 font stating no /Resources against the resources of the stream
// showing them (ISO 32000-1, 9.6.5, Table 112): on the page, glyph a by the
// x it shows and glyph c by the form it paints, not by the page. Glyph b
// shows itself, and a glyph shown from a form whose resources name neither
// the x's font nor the form paints what nothing bounds: both keep the page.
func TestType3GlyphWithoutResourcesBoundedWhereShown(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := scopedType3PDF()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	elements, err := scopedType3Elements(doc)
	if err != nil {
		t.Fatalf("extracting: %v", err)
	}
	if len(elements) != 5 {
		t.Fatalf("got %d elements, want 5", len(elements))
	}
	page := [4]float64{0, 0, 612, 792}
	a, b, c := elements[0].InkBox, elements[1].InkBox, elements[2].InkBox
	// The x sits 2 ems of the nested font right of glyph a's origin, at
	// Tf 1000 in glyph space: 20 points right at Tf 10, about 5 wide.
	if a[0] > 100.01 || a[2] < 122 || a[2] > 128 || a[3] < 409 || a[3] > 411 {
		t.Errorf("glyph a ink %v, want it to reach the x it shows", a)
	}
	if b != page {
		t.Errorf("glyph b ink %v, want the page it may paint anywhere on", b)
	}
	// The form's 500-unit square is 5 points at Tf 10, from c's origin 120.
	if c != [4]float64{120, 400, 125, 405} {
		t.Errorf("glyph c ink %v, want the form it paints", c)
	}
	for _, el := range elements[3:] {
		if el.InkBox != page {
			t.Errorf("glyph %q shown from the form: ink %v, want the page", el.Text, el.InkBox)
		}
	}
}

// TestType3ScopedInkConcurrent extracts the page from several goroutines
// sharing one document, and so one font and its cache of scoped bounds.
func TestType3ScopedInkConcurrent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := scopedType3PDF()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	want, err := scopedType3Elements(doc)
	if err != nil {
		t.Fatalf("extracting: %v", err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			got, err := scopedType3Elements(doc)
			if err != nil || len(got) != len(want) {
				t.Errorf("extracting: %v (%d elements)", err, len(got))
				return
			}
			for i := range got {
				if got[i].InkBox != want[i].InkBox {
					t.Errorf("element %d ink %v, want %v", i, got[i].InkBox, want[i].InkBox)
				}
			}
		})
	}
	wg.Wait()
}
