package compliance

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// fieldTypeAncestorDoc builds a one-page document from the given objects and
// returns the dictionary at widgetNum.
func fieldTypeAncestorDoc(t *testing.T, objs []string, widgetNum int) (context.Context, *document.Document, ast.PdfDict) {
	t.Helper()

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)

	data := buf.Bytes()
	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	widget, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: widgetNum})
	if err != nil || widget == nil {
		t.Fatalf("resolving object %d: %v", widgetNum, err)
	}
	return ctx, doc, widget
}

// TestResolveFieldType_ClimbsPastAnIntermediateField pins that the ascent keeps
// going past a field that states no /FT.
//
// /FT is inheritable (ISO 32000-1, 12.7.3.1) and here it lives two hops up, on
// the root field, while the widget and the kid between them state none. A walk
// that re-started from the annotation on every pass would see only the kid and
// answer with no type at all.
func TestResolveFieldType_ClimbsPastAnIntermediateField(t *testing.T) {
	t.Parallel()

	ctx, doc, widget := fieldTypeAncestorDoc(t, []string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [6 0 R] >>",
		"<< /FT /Btn /T (root) >>",
		"<< /T (kid) /Parent 4 0 R >>",
		"<< /Type /Annot /Subtype /Widget /Parent 5 0 R /Rect [0 0 10 10] >>",
	}, 6)

	if ft := resolveFieldType(ctx, doc, widget); ft != "Btn" {
		t.Errorf("resolveFieldType = %q, want Btn from the root field two hops up", ft)
	}
}

// TestResolveFieldType_StopsOnACycle pins termination. A chain that names no
// typed field has no answer to give, and the bound alone already stopped the
// walk before the visited set was added, so this is a contract pin rather than
// a regression catch.
func TestResolveFieldType_StopsOnACycle(t *testing.T) {
	t.Parallel()

	ctx, doc, widget := fieldTypeAncestorDoc(t, []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [5 0 R] >>",
		"<< /T (a) /Parent 5 0 R >>",
		"<< /Type /Annot /Subtype /Widget /Parent 4 0 R /Rect [0 0 10 10] >>",
	}, 5)

	if ft := resolveFieldType(ctx, doc, widget); ft != "" {
		t.Errorf("resolveFieldType = %q, want empty: no field in the cycle states /FT", ft)
	}
}
