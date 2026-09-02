package compliance

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// uaAnnotPDF builds a tagged one-page document carrying a single annotation
// with the given dictionary body. The annotation is absent from the structure
// tree, which ISO 14289-1, 7.18.1 reports unless the annotation is invisible.
func uaAnnotPDF(annot string) []byte {
	return cyclePDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 9 0 R /Lang (en-US)"+
			" /MarkInfo << /Marked true >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /CropBox [0 0 612 792]"+
			" /Annots [4 0 R] >>",
		annot,
		"0", "0", "100", "100",
		"<< /Length 0 >>\nstream\n\nendstream",
		"<< /Type /StructElem /S /Document /P 9 0 R /K [] >>",
		"<< /Type /StructTreeRoot /K [8 0 R] >>",
	)
}

// uaClauses returns the set of ISO 14289-1 clauses reported for the document.
func uaClauses(t *testing.T, data []byte) map[string]int {
	t.Helper()

	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	viols, err := ValidatePDFUA(ctx, doc, models.PDFUA1)
	if err != nil {
		t.Fatalf("ValidatePDFUA: %v", err)
	}

	out := map[string]int{}
	for _, e := range viols {
		if v, ok := e.(Violation); ok {
			out[v.Rule.Clause]++
		}
	}
	return out
}

// TestAnnotExemptionIgnoresIndirectRect pins that an annotation drawn on the
// page is held to the annotation rules however its rectangle is written.
//
// litho exempts an annotation whose /Rect falls entirely outside the crop box,
// since nothing it draws can be seen. ISO 32000-1, 7.3.9 allows an indirect
// reference wherever a direct object may appear, and reading those references as
// numbers puts every corner at the origin, which sits outside a crop box
// starting there. Annotations squarely on the page were therefore skipped, and
// ISO 14289-1 7.18.1 tests 1 and 2 went unreported.
func TestAnnotExemptionIgnoresIndirectRect(t *testing.T) {
	t.Parallel()

	const body = "<< /Type /Annot /Subtype /Square /F 4 /Rect "
	direct := uaClauses(t, uaAnnotPDF(body+"[0 0 100 100] >>"))
	if direct["7.18.1"] == 0 {
		t.Fatal("the direct rectangle reported no 7.18.1 violation, so the fixture proves nothing")
	}

	indirect := uaClauses(t, uaAnnotPDF(body+"[5 0 R 6 0 R 7 0 R 8 0 R] >>"))
	if indirect["7.18.1"] != direct["7.18.1"] {
		t.Errorf("the same annotation written with indirect coordinates reported %d 7.18.1 "+
			"violations, want %d: it was treated as sitting off the page",
			indirect["7.18.1"], direct["7.18.1"])
	}
}

// TestFieldExemptionIgnoresIndirectRect pins the same for the form field
// description rule, which reaches the crop box through its own comparison.
func TestFieldExemptionIgnoresIndirectRect(t *testing.T) {
	t.Parallel()

	const body = "<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /F 4 /P 3 0 R /Rect "
	direct := uaClauses(t, uaAnnotPDF(body+"[0 0 100 100] >>"))
	if direct["7.18.4"] == 0 {
		t.Fatal("the direct rectangle reported no 7.18.4 violation, so the fixture proves nothing")
	}

	indirect := uaClauses(t, uaAnnotPDF(body+"[5 0 R 6 0 R 7 0 R 8 0 R] >>"))
	if indirect["7.18.4"] != direct["7.18.4"] {
		t.Errorf("the widget written with indirect coordinates reported %d 7.18.4 violations, "+
			"want %d", indirect["7.18.4"], direct["7.18.4"])
	}
}

// TestAnnotOffThePageStaysExempt is the control: the exemption still applies to
// an annotation that really does fall outside the crop box.
func TestAnnotOffThePageStaysExempt(t *testing.T) {
	t.Parallel()

	onPage := uaClauses(t, uaAnnotPDF(
		"<< /Type /Annot /Subtype /Square /F 4 /Rect [0 0 100 100] >>"))
	offPage := uaClauses(t, uaAnnotPDF(
		"<< /Type /Annot /Subtype /Square /F 4 /Rect [900 900 1000 1000] >>"))

	if offPage["7.18.1"] >= onPage["7.18.1"] {
		t.Errorf("an annotation beyond the crop box reported %d 7.18.1 violations and one on the "+
			"page reported %d: the exemption stopped working",
			offPage["7.18.1"], onPage["7.18.1"])
	}
}

// TestResolveRectReadsIndirectCoordinates covers the shared helper directly, so
// a future caller inherits the fix rather than repeating the bug.
func TestResolveRectReadsIndirectCoordinates(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := uaAnnotPDF("<< /Type /Annot /Subtype /Square /F 4 /Rect [5 0 R 6 0 R 7 0 R 8 0 R] >>")
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	annot, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 4})
	if err != nil {
		t.Fatalf("resolving the annotation: %v", err)
	}

	r := resolveRect(ctx, doc, annot["Rect"])
	if len(r) != 4 {
		t.Fatalf("resolveRect returned %v", r)
	}
	if r[2] != 100 || r[3] != 100 {
		t.Errorf("resolveRect = %v, want the upper right at (100, 100)", r)
	}
}

// TestViolationMessagesMentionTheClause guards the assumption the tests above
// rely on: clauses are reported in the structured Rule, not only in prose.
func TestViolationMessagesMentionTheClause(t *testing.T) {
	t.Parallel()

	data := uaAnnotPDF("<< /Type /Annot /Subtype /Square /F 4 /Rect [0 0 100 100] >>")
	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	viols, err := ValidatePDFUA(ctx, doc, models.PDFUA1)
	if err != nil {
		t.Fatalf("ValidatePDFUA: %v", err)
	}
	if len(viols) == 0 {
		t.Fatal("the fixture reported no violations at all")
	}
	for _, e := range viols {
		v, ok := e.(Violation)
		if !ok {
			continue
		}
		if strings.TrimSpace(v.Rule.Clause) == "" {
			t.Errorf("a violation carries no clause: %v", e)
		}
	}
}
