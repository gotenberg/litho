package compliance

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// appearanceOpPDF builds a one-page document carrying an operator ISO 32000-1
// does not define. When inAppearance is set the operator sits inside the
// annotation's appearance stream, otherwise in the page's own content.
func appearanceOpPDF(t *testing.T, inAppearance bool) []byte {
	t.Helper()

	page := "BT /F1 12 Tf 20 700 Td (body) Tj ET\n"
	ap := "q 1 0 0 RG 0 0 100 50 re S Q\n"
	if inAppearance {
		ap = "q 1 0 0 RG 0 0 100 50 re S zz Q\n"
	} else {
		page += "zz\n"
	}

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R" +
			" /Resources << /Font << /F1 5 0 R >> >> /Annots [6 0 R] >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(page), page),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		"<< /Type /Annot /Subtype /Square /Rect [100 100 200 150] /F 4 /AP << /N 7 0 R >> >>",
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 100 50] /Length %d >>\nstream\n%s\nendstream",
			len(ap), ap),
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

// appearanceScan runs the drawn-stream walk over the pages of the document
// whose catalog is cat and returns what [checkAppearanceStreamOps] states for
// the annotation appearance streams it reaches, at part.
func appearanceScan(ctx context.Context, doc *document.Document, cat ast.PdfDict, part int) []Violation {
	vctx := &validationCtx{doc: doc, catalog: cat, part: part, pages: pageCtxsFor(ctx, doc, cat)}
	var got []Violation
	walk := newDrawnStreamWalk(vctx, func(s *drawnStream) {
		if s.kind == drawnAppearance {
			got = append(got, checkAppearanceStreamOps(ctx, doc, s, part, nil)...)
		}
	})
	for i := range vctx.pages {
		walk.walkPage(ctx, &vctx.pages[i], i+1)
	}
	return got
}

// appearanceOpViolations runs the appearance-stream operator scan over the
// fixture's single page and keeps the 6.2.2 findings.
func appearanceOpViolations(t *testing.T, inAppearance bool) []Violation {
	t.Helper()

	ctx := context.Background()
	data := appearanceOpPDF(t, inAppearance)
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}

	got := appearanceScan(ctx, doc, catalog.Dict, 2)
	var found []Violation
	for _, v := range got {
		if strings.HasPrefix(v.Rule.Clause, "6.2.2") {
			found = append(found, v)
		}
	}
	return found
}

// TestUndefinedOperatorInAppearanceStream pins that an operator ISO 32000-1
// does not define is reported when it sits in an annotation appearance stream.
//
// An appearance stream is a content stream (ISO 32000-1, 12.5.5), so the ISO
// 19005-2, 6.2.2 prohibition reaches it, and veraPDF 1.30.2 fails the rule on
// exactly this shape. litho scanned only a page's own /Contents and the Form
// XObjects its resources name, so the operator went unreported: a false
// negative against the reference validator.
func TestUndefinedOperatorInAppearanceStream(t *testing.T) {
	t.Parallel()

	got := appearanceOpViolations(t, true)
	if len(got) == 0 {
		t.Fatal("no 6.2.2 violation reported for an undefined operator in an appearance stream")
	}
	if !strings.Contains(got[0].Context, "appearance") {
		t.Errorf("context = %q, want it to name the appearance stream", got[0].Context)
	}
}

// TestUndefinedOperatorOutsideAppearanceIsNotReportedTwice is the control: with
// the operator in the page content instead, the appearance scan stays silent so
// the page-level scan remains the single reporter.
func TestUndefinedOperatorOutsideAppearanceIsNotReportedTwice(t *testing.T) {
	t.Parallel()

	if got := appearanceOpViolations(t, false); len(got) != 0 {
		t.Errorf("the appearance scan reported %d violation(s) for a clean appearance stream", len(got))
	}
}

// TestUndefinedOperatorInAppearanceReachesValidation pins that the appearance
// scan is registered in the check groups, not merely present.
//
// The direct-call tests above exercise the function; they would keep passing if
// nobody ever invoked it. A detector that works but is not wired in is a dead
// rule, so this goes through the public validation entry point instead.
func TestUndefinedOperatorInAppearanceReachesValidation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := appearanceOpPDF(t, true)
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	violations, err := ValidatePDFA(ctx, doc, models.PDFA2b)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}
	for _, v := range violations {
		if strings.Contains(v.Error(), "undefined operator") &&
			strings.Contains(v.Error(), "appearance") {
			return
		}
	}
	t.Error("a full PDF/A-2b run did not report the appearance-stream operator: the check is not wired in")
}
