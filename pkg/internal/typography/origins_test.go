package typography

import (
	"bytes"
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// originsPage loads a document from data and returns it with its first page.
func originsPage(t *testing.T, data []byte) (*document.Document, *document.Page) {
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
	return doc, pages[0]
}

// formXObjectPDF is a page whose only content invokes a form XObject that
// paints text from its own stream.
func formXObjectPDF() []byte {
	form := "BT /F1 12 Tf (ZZ) Tj ET"
	return []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
		"5 0 obj\n<< /Font << /F1 4 0 R >> /XObject << /Fm1 7 0 R >> >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 5 0 R /Contents 6 0 R >>\nendobj\n" +
		"6 0 obj\n<< /Length 7 >>\nstream\n/Fm1 Do\nendstream\nendobj\n" +
		"7 0 obj\n<< /Type /XObject /Subtype /Form /BBox [0 0 200 200] " +
		"/Resources << /Font << /F1 4 0 R >> >> /Length " +
		strconv.Itoa(len(form)) + " >>\nstream\n" + form + "\nendstream\nendobj\n" +
		"trailer\n<< /Size 8 /Root 1 0 R >>\n%%EOF\n")
}

func TestExtractElementsWithOrigins_NamesTheOperatorThatDrewTheElement(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc, page := originsPage(t, minimalFontPDF())

	// Parsed with offsets, so the ranges run parallel to the operations and an
	// origin indexes straight into them. The same bytes serve as the parse
	// input and the offset basis, which is the pairing the API documents.
	body := []byte("BT /F1 12 Tf 100 700 Td (AB) Tj 200 0 Td (CD) Tj ET")
	ops, ranges, err := content.ParseStreamDataWithOffsets(ctx, body)
	if err != nil {
		t.Fatalf("ParseStreamDataWithOffsets: %v", err)
	}
	if len(ops) != len(ranges) {
		t.Fatalf("ops %d, ranges %d: the parser must report one range per operation", len(ops), len(ranges))
	}

	machine := graphics.NewMachine([4]float64{0, 0, 612, 792})
	elements, origins, err := ExtractElementsWithOrigins(ctx, doc, machine, page, ops)
	if err != nil {
		t.Fatalf("ExtractElementsWithOrigins: %v", err)
	}
	if len(elements) != len(origins) {
		t.Fatalf("elements %d, origins %d: the slices must be parallel", len(elements), len(origins))
	}
	if len(elements) == 0 {
		t.Fatal("no elements extracted; the fixture cannot prove anything")
	}

	for i, el := range elements {
		if strings.TrimSpace(el.Text) == "" {
			continue
		}
		origin := origins[i]
		if origin < 0 || origin >= len(ops) {
			t.Errorf("element %d (%q) origin %d is not an index into %d operations", i, el.Text, origin, len(ops))
			continue
		}
		r := ranges[origin]
		drawn := string(body[r.Start:r.End])
		// The bytes an origin names must be the operator that drew the glyph,
		// so the glyph's own characters appear in its string operand. Deriving
		// that from the element rather than naming an expected operand keeps
		// the assertion honest whatever granularity extraction returns.
		if !strings.Contains(drawn, el.Text) {
			t.Errorf("element %d (%q) origin names %q, which does not draw it", i, el.Text, drawn)
		}
		if !strings.Contains(drawn, "Tj") {
			t.Errorf("element %d (%q) origin names %q, want a show-text operator", i, el.Text, drawn)
		}
	}
}

func TestExtractElementsWithOrigins_FormElementsAreUnattributable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc, page := originsPage(t, formXObjectPDF())

	body := []byte("/Fm1 Do")
	ops, _, err := content.ParseStreamDataWithOffsets(ctx, body)
	if err != nil {
		t.Fatalf("ParseStreamDataWithOffsets: %v", err)
	}

	machine := graphics.NewMachine([4]float64{0, 0, 612, 792})
	elements, origins, err := ExtractElementsWithOrigins(ctx, doc, machine, page, ops)
	if err != nil {
		t.Fatalf("ExtractElementsWithOrigins: %v", err)
	}
	if len(elements) != len(origins) {
		t.Fatalf("elements %d, origins %d: the slices must be parallel", len(elements), len(origins))
	}
	if len(elements) == 0 {
		t.Fatal("the form painted no elements; the fixture cannot prove anything")
	}

	// The glyphs live in the form's own stream, so no offset into this one
	// addresses them. Naming the Do would invite a caller to remove the whole
	// form to reach a single glyph inside it.
	for i, origin := range origins {
		if origin != OriginUnattributable {
			t.Errorf("element %d (%q) origin = %d, want OriginUnattributable", i, elements[i].Text, origin)
		}
	}
}

func TestExtractElements_UnchangedByOriginRecording(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc, page := originsPage(t, minimalFontPDF())

	ops, err := content.ParsePageContent(ctx, doc, page.Dict)
	if err != nil {
		t.Fatalf("ParsePageContent: %v", err)
	}

	plain, err := ExtractElements(ctx, doc, graphics.NewMachine([4]float64{0, 0, 612, 792}), page, ops)
	if err != nil {
		t.Fatalf("ExtractElements: %v", err)
	}
	withOrigins, origins, err := ExtractElementsWithOrigins(ctx, doc, graphics.NewMachine([4]float64{0, 0, 612, 792}), page, ops)
	if err != nil {
		t.Fatalf("ExtractElementsWithOrigins: %v", err)
	}

	if len(plain) != len(withOrigins) {
		t.Fatalf("ExtractElements returned %d elements, ExtractElementsWithOrigins %d: recording must not change extraction", len(plain), len(withOrigins))
	}
	if len(origins) != len(withOrigins) {
		t.Fatalf("origins %d, elements %d", len(origins), len(withOrigins))
	}
	for i := range plain {
		if plain[i] != withOrigins[i] {
			t.Errorf("element %d differs: %+v vs %+v", i, plain[i], withOrigins[i])
		}
	}
}
