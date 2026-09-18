package remediation

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// nestedFieldTreePDF builds a widget two hops below the field that states
// everything about it: object 6 is the widget, object 5 a kid field carrying
// only /T, and object 4 the root field holding /FT, /V and /Ff.
//
// Every inheritable entry lives on the root, so a walk that stops short finds
// nothing (ISO 32000-1, 12.7.3.1).
func nestedFieldTreePDF() []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [6 0 R] >>",
		"<< /FT /Tx /V (hello) /Ff 2 /T (root) >>",
		"<< /T (kid) /Parent 4 0 R >>",
		"<< /Type /Annot /Subtype /Widget /Parent 5 0 R /Rect [0 0 10 10] >>",
	}

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
	return buf.Bytes()
}

// widgetOfNestedTree returns the widget dictionary at object 6.
func widgetOfNestedTree(t *testing.T) (context.Context, *document.Document, ast.PdfDict) {
	t.Helper()
	doc, _ := newTestDoc(t, nestedFieldTreePDF())
	ctx := context.Background()
	widget, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 6})
	if err != nil || widget == nil {
		t.Fatalf("resolving the widget: %v", err)
	}
	return ctx, doc, widget
}

// TestFieldAncestorWalksClimbPastAnIntermediateField pins that each of the four
// inheritable lookups still ascends the whole /Parent chain.
//
// These walks share one hop helper, so a mis-wiring shows up here rather than
// in any cycle test: on a cyclic chain all four answer the same guarded or not,
// but a walk that stops one hop short finds the kid field, which states none of
// these entries, and every assertion below fails at once.
func TestFieldAncestorWalksClimbPastAnIntermediateField(t *testing.T) {
	t.Parallel()

	ctx, doc, widget := widgetOfNestedTree(t)

	if ft := inheritedFieldType(ctx, doc, widget); ft != "Tx" {
		t.Errorf("inheritedFieldType = %q, want Tx from the root field two hops up", ft)
	}
	if v := fieldValueText(ctx, doc, widget); v != "hello" {
		t.Errorf("fieldValueText = %q, want hello from the root field two hops up", v)
	}
	if f := fieldFlags(ctx, doc, widget); f != 2 {
		t.Errorf("fieldFlags = %d, want 2 from the root field two hops up", f)
	}
	if !rendersOwnValue(ctx, doc, widget, "Widget") {
		t.Error("rendersOwnValue = false, want true: the root field is /Tx with a non-empty /V")
	}
}

// TestFieldAncestorWalksStopOnACycle pins termination only. Each of these
// returns its zero answer on a cyclic chain whether or not the visited set is
// present, so this would not have failed before it was added.
func TestFieldAncestorWalksStopOnACycle(t *testing.T) {
	t.Parallel()

	data := []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [5 0 R] >>\nendobj\n" +
		"4 0 obj\n<< /T (a) /Parent 5 0 R >>\nendobj\n" +
		"5 0 obj\n<< /Type /Annot /Subtype /Widget /Parent 4 0 R /Rect [0 0 10 10] >>\nendobj\n" +
		"trailer\n<< /Size 6 /Root 1 0 R >>\n%%EOF\n")

	doc, _ := newTestDoc(t, data)
	ctx := context.Background()
	widget, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 5})
	if err != nil || widget == nil {
		t.Fatalf("resolving the widget: %v", err)
	}

	if ft := inheritedFieldType(ctx, doc, widget); ft != "" {
		t.Errorf("inheritedFieldType = %q, want empty: no field in the cycle states /FT", ft)
	}
	if f := fieldFlags(ctx, doc, widget); f != 0 {
		t.Errorf("fieldFlags = %d, want 0: no field in the cycle states /Ff", f)
	}
}
