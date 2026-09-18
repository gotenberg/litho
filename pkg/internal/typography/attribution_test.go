package typography

import (
	"bytes"
	"context"
	"strconv"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// attributionPDF builds a one-page document whose /Font resources hold two
// fonts, object 4 as /F1 and object 8 as /F2, so a test can watch attribution
// follow a Tf change. body is the page's content stream.
func attributionPDF(body string) []byte {
	return []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
		"8 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Courier >>\nendobj\n" +
		"5 0 obj\n<< /Font << /F1 4 0 R /F2 8 0 R >> >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 5 0 R /Contents 6 0 R >>\nendobj\n" +
		"6 0 obj\n<< /Length " + strconv.Itoa(len(body)) + " >>\nstream\n" + body + "\nendstream\nendobj\n" +
		"trailer\n<< /Size 9 /Root 1 0 R >>\n%%EOF\n")
}

// attribute runs the extractor over data's first page and returns the elements
// with their origins and font references.
func attribute(t *testing.T, data []byte) ([]string, []int, []int) {
	t.Helper()
	ctx := context.Background()

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

	machine := graphics.NewMachine([4]float64{0, 0, 612, 792})
	elements, origins, fontRefs, err := ExtractElementsWithAttribution(ctx, doc, machine, pages[0], ops)
	if err != nil {
		t.Fatalf("ExtractElementsWithAttribution: %v", err)
	}

	texts := make([]string, 0, len(elements))
	for _, el := range elements {
		texts = append(texts, el.Text)
	}
	return texts, origins, fontRefs
}

func TestAttribution_NamesTheFontObjectThatDrewTheGlyph(t *testing.T) {
	// The load-bearing case: the reference has to be the font's real object
	// number, not the sentinel. Two paths resolve fonts (syncFont, which runs
	// first because Execute applies the Tf before the loop dispatches it, and
	// handleTfOp in the Tf case), and a reference captured in neither compiles
	// perfectly well while reporting FontUnattributable for every glyph on the
	// page. Asserting the number rather than "something came back" is what
	// separates those two outcomes.
	texts, _, fontRefs := attribute(t, attributionPDF("BT /F1 12 Tf 100 700 Td (AB) Tj ET"))

	if len(texts) == 0 {
		t.Fatalf("no elements extracted, so nothing was attributed")
	}
	if len(fontRefs) != len(texts) {
		t.Fatalf("fontRefs = %d entries, elements = %d", len(fontRefs), len(texts))
	}
	for i, ref := range fontRefs {
		if ref != 4 {
			t.Errorf("glyph %d (%q) attributed to font %d, want object 4", i, texts[i], ref)
		}
	}
}

func TestAttribution_FollowsAFontChange(t *testing.T) {
	// /F1 is object 4 and /F2 is object 8. The attribution has to move with the
	// Tf, not stay on whichever font was resolved first.
	texts, _, fontRefs := attribute(t, attributionPDF(
		"BT /F1 12 Tf 100 700 Td (A) Tj /F2 12 Tf (B) Tj ET"))

	if len(fontRefs) != len(texts) || len(texts) < 2 {
		t.Fatalf("elements %q, fontRefs %v", texts, fontRefs)
	}
	first, last := fontRefs[0], fontRefs[len(fontRefs)-1]
	if first != 4 {
		t.Errorf("first glyph attributed to %d, want object 4", first)
	}
	if last != 8 {
		t.Errorf("last glyph attributed to %d, want object 8 after the second Tf", last)
	}
}

func TestAttribution_SlicesAreParallel(t *testing.T) {
	// The contract the accessor documents: all three the same length, so a
	// caller can index elements, origins and fontRefs together.
	texts, origins, fontRefs := attribute(t, attributionPDF(
		"BT /F1 12 Tf 100 700 Td (AB) Tj /F2 12 Tf (CD) Tj ET"))

	if len(origins) != len(texts) || len(fontRefs) != len(texts) {
		t.Errorf("elements %d, origins %d, fontRefs %d: all three must match",
			len(texts), len(origins), len(fontRefs))
	}
}

func TestAttribution_FormXObjectIsUnattributable(t *testing.T) {
	// A form selects its fonts from its own /Resources, so the reference held
	// on the page names a different font than the one that drew these glyphs.
	form := "BT /F1 12 Tf (Z) Tj ET"
	body := "/Fm1 Do"
	data := []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
		"5 0 obj\n<< /Font << /F1 4 0 R >> /XObject << /Fm1 7 0 R >> >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 5 0 R /Contents 6 0 R >>\nendobj\n" +
		"6 0 obj\n<< /Length " + strconv.Itoa(len(body)) + " >>\nstream\n" + body + "\nendstream\nendobj\n" +
		"7 0 obj\n<< /Type /XObject /Subtype /Form /BBox [0 0 50 50] " +
		"/Resources << /Font << /F1 4 0 R >> >> /Length " + strconv.Itoa(len(form)) +
		" >>\nstream\n" + form + "\nendstream\nendobj\n" +
		"trailer\n<< /Size 8 /Root 1 0 R >>\n%%EOF\n")

	texts, origins, fontRefs := attribute(t, data)
	if len(texts) == 0 {
		t.Fatalf("the form drew no elements, so this proves nothing")
	}
	for i := range texts {
		if fontRefs[i] != FontUnattributable {
			t.Errorf("form glyph %d (%q) attributed to font %d, want FontUnattributable",
				i, texts[i], fontRefs[i])
		}
		if origins[i] != OriginUnattributable {
			t.Errorf("form glyph %d reports origin %d, want OriginUnattributable", i, origins[i])
		}
	}
}

func TestAttribution_DirectFontDictionaryIsUnattributable(t *testing.T) {
	// The resources hold the font dictionary itself rather than naming it, so
	// there is no object number to report and none may be invented.
	body := "BT /F1 12 Tf 100 700 Td (A) Tj ET"
	data := []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"5 0 obj\n<< /Font << /F1 << /Type /Font /Subtype /Type1 /BaseFont /Helvetica >> >> >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 5 0 R /Contents 6 0 R >>\nendobj\n" +
		"6 0 obj\n<< /Length " + strconv.Itoa(len(body)) + " >>\nstream\n" + body + "\nendstream\nendobj\n" +
		"trailer\n<< /Size 7 /Root 1 0 R >>\n%%EOF\n")

	texts, _, fontRefs := attribute(t, data)
	if len(texts) == 0 {
		t.Fatalf("no elements extracted, so nothing was attributed")
	}
	for i, ref := range fontRefs {
		if ref != FontUnattributable {
			t.Errorf("glyph %d (%q) attributed to object %d, want FontUnattributable for a direct font dict",
				i, texts[i], ref)
		}
	}
}

func TestAttribution_WithOriginsStillReturnsTwoSlices(t *testing.T) {
	// ExtractElementsWithOrigins keeps its contract: mutations depends on it,
	// and this change must not have altered what it returns.
	ctx := context.Background()
	data := attributionPDF("BT /F1 12 Tf 100 700 Td (A) Tj ET")

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
		t.Fatalf("ResolvePages: %v", err)
	}
	ops, err := content.ParsePageContent(ctx, doc, pages[0].Dict)
	if err != nil {
		t.Fatalf("ParsePageContent: %v", err)
	}

	machine := graphics.NewMachine([4]float64{0, 0, 612, 792})
	elements, origins, err := ExtractElementsWithOrigins(ctx, doc, machine, pages[0], ops)
	if err != nil {
		t.Fatalf("ExtractElementsWithOrigins: %v", err)
	}
	if len(elements) != len(origins) {
		t.Errorf("elements %d, origins %d: the documented contract is equal lengths",
			len(elements), len(origins))
	}
}
