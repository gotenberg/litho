package annotation

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// widgetDoc builds a one-page document from the given objects.
func widgetDoc(t *testing.T, objs []string) (context.Context, *document.Document) {
	t.Helper()

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)

	data := buf.Bytes()
	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	return ctx, doc
}

// TestHasPageWidgetFieldFindsAnUnregisteredField pins that a field reachable
// only from the page is found.
//
// A summary that answers "does this document have a form" from the catalog
// alone says no for a document whose fields litho lists.
//
// # Corpus evidence
//
//   - pdfjs issue12963.pdf: carries 27 fields that litho lists and states no
//     /AcroForm at all.
func TestHasPageWidgetFieldFindsAnUnregisteredField(t *testing.T) {
	t.Parallel()

	ctx, doc := widgetDoc(t, []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (username) /Rect [0 0 10 10] >>",
	})

	if !HasPageWidgetField(ctx, doc) {
		t.Error("the widget on the page was not recognised as a form field")
	}
}

// TestHasPageWidgetFieldIgnoresPlainAnnotations is the control: a page carrying
// annotations that are not form fields has no form.
func TestHasPageWidgetFieldIgnoresPlainAnnotations(t *testing.T) {
	t.Parallel()

	ctx, doc := widgetDoc(t, []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R 5 0 R] >>",
		"<< /Type /Annot /Subtype /Square /Rect [0 0 10 10] >>",
		// A widget naming no field type is decoration, not a form field.
		"<< /Type /Annot /Subtype /Widget /T (no-type) /Rect [0 0 10 10] >>",
	})

	if HasPageWidgetField(ctx, doc) {
		t.Error("a page with no form field was reported as having one")
	}
}

// TestHasPageWidgetFieldFindsAnInheritedType pins that a widget whose /FT comes
// from its parent counts (ISO 32000-1, 12.7.3.1).
func TestHasPageWidgetFieldFindsAnInheritedType(t *testing.T) {
	t.Parallel()

	ctx, doc := widgetDoc(t, []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /Parent 5 0 R /Rect [0 0 10 10] >>",
		"<< /FT /Tx /T (parented) >>",
	})

	if !HasPageWidgetField(ctx, doc) {
		t.Error("a widget inheriting its field type was not recognised")
	}
}

// TestHasPageWidgetFieldOnADocumentWithNoPages is the control for a document
// the page tree cannot be read from.
func TestHasPageWidgetFieldOnADocumentWithNoPages(t *testing.T) {
	t.Parallel()

	ctx, doc := widgetDoc(t, []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [] /Count 0 >>",
	})

	if HasPageWidgetField(ctx, doc) {
		t.Error("a document with no pages was reported as having a form field")
	}
}
