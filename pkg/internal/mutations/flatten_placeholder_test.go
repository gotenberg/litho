package mutations

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// placeholderFormPDF builds a form that relies on /NeedAppearances: the field
// carries a value and stores an appearance that paints nothing. When draws is
// set the stored appearance paints the value, so flattening it loses nothing.
//
// # Corpus evidence
//
//   - pdfjs issue12233.pdf: has the placeholder shape.
func placeholderFormPDF(draws bool) []byte {
	body := "/Tx BMC\nEMC\n"
	if draws {
		body = "/Tx BMC\nBT /Helv 9 Tf (150) Tj ET\nEMC\n"
	}

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] /NeedAppearances true " +
			"/DA (/Helv 9 Tf 0 g) >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] /Contents 6 0 R >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (qty) /V (150) /Rect [10 700 90 720] " +
			"/AP << /N 5 0 R >> >>",
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 80 20] /Length %d >>\nstream\n%sendstream",
			len(body), body),
		"<< /Length 20 >>\nstream\nBT (page) Tj ET\nendstream",
	}

	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	start := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, start)
	return b.Bytes()
}

func flattenPlaceholder(t *testing.T, draws bool) (kept bool, keptHasAP bool) {
	t.Helper()
	ctx := context.Background()
	data := placeholderFormPDF(draws)
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))
	if err := FlattenForm(ctx, alloc, doc); err != nil {
		t.Fatalf("FlattenForm: %v", err)
	}

	// The page is only rewritten when something was baked into it. A page left
	// alone means every widget was kept, which is the outcome under test, so
	// read the page's /Annots from whichever version exists.
	pageDict := ast.PdfDict(nil)
	if queued, ok := alloc.Pending()[3].(ast.PdfDict); ok {
		pageDict = queued
	} else {
		catalog, cerr := document.ResolveCatalog(ctx, doc)
		if cerr != nil {
			t.Fatalf("ResolveCatalog: %v", cerr)
		}
		pages, perr := document.ResolvePages(ctx, doc, catalog)
		if perr != nil || len(pages) == 0 {
			t.Fatalf("ResolvePages: %v", perr)
		}
		pageDict = pages[0].Dict
	}
	annots, err := document.ResolveArray(ctx, doc, pageDict["Annots"])
	if err != nil || len(annots) == 0 {
		return false, false
	}
	widget, ok := alloc.Pending()[4].(ast.PdfDict)
	if !ok {
		// Kept unchanged, so it still carries its appearance.
		return true, true
	}
	_, hasAP := widget["AP"]
	return true, hasAP
}

// TestFlattenDrawsAValueTheAppearanceCannotCarry pins that a field whose
// stored appearance paints nothing is drawn from its value, and leaves the
// document with the form.
//
// A form asking viewers to build appearances (ISO 32000-1, 12.7.3.3) stores
// placeholders. Baking one in writes an empty stream into the page and deletes
// the widget, so the value leaves the document. Keeping the widget was not
// enough either, because the placeholder is then what a viewer draws once the
// form is gone, and the widget outlived the form that gave it meaning: the
// value is drawn as a viewer generating the appearance would draw it.
//
// # Corpus evidence
//
//   - pdfjs issue12233.pdf: baking the placeholders lost "Man", "150" and
//     "Red".
func TestFlattenDrawsAValueTheAppearanceCannotCarry(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := placeholderFormPDF(false)
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))
	if err := FlattenForm(ctx, alloc, doc); err != nil {
		t.Fatalf("FlattenForm: %v", err)
	}
	page, drawn := flattenedPage(t, alloc)
	if annots, _ := page["Annots"].(ast.PdfArray); len(annots) != 0 {
		t.Errorf("the widget was kept: %v", annots)
	}
	if len(drawn) != 1 || !strings.Contains(drawn[0], " Tj") {
		t.Fatalf("the value was not drawn into the page: %v", drawn)
	}
	// The appearance draws the value with the form's default appearance,
	// which the field inherits (ISO 32000-1, 12.7.3.3), form or no form.
	//
	// # Corpus evidence
	//
	//   - litho sigcms_2b.pdf: has a text field carrying "not a signature" and
	//     no /DA of its own, and removing the form alone dropped it from the
	//     page.
	if !strings.Contains(drawn[0], " 9 Tf") {
		t.Errorf("the value is not drawn at the form's default size: %q", drawn[0])
	}
}

// TestFlattenBakesAnAppearanceThatCarriesItsValue is the control: a stored
// appearance that does show the value is baked as it is.
func TestFlattenBakesAnAppearanceThatCarriesItsValue(t *testing.T) {
	t.Parallel()
	if kept, _ := flattenPlaceholder(t, true); kept {
		t.Error("a widget whose appearance shows its value was kept")
	}
}
