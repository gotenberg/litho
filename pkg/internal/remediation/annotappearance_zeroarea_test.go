package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// zeroAreaAnnotPDF builds a page holding one annotation whose /Rect encloses no
// area, the shape an invisible signature field takes. subtype and extra choose
// the kind of annotation under test.
func zeroAreaAnnotPDF(subtype, extra string) []byte {
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /"+subtype+" "+extra+" /Rect [0 0 0 0] /F 4 >>",
	)
}

// annotGainedAP reports whether the fixer queued an appearance for object 4.
func annotGainedAP(t *testing.T, pdf []byte, profile string) bool {
	t.Helper()
	doc, alloc := newTestDoc(t, pdf)
	if _, err := fixAnnotAppearance(context.Background(), alloc, doc, Options{}, nil, profile); err != nil {
		t.Fatalf("fixAnnotAppearance: %v", err)
	}
	queued, ok := alloc.Pending()[4].(ast.PdfDict)
	if !ok {
		return false
	}
	_, gained := queued["AP"]
	return gained
}

// TestFixAnnotAppearanceZeroAreaWidgetPart1 pins the PDF/A-1 case: a widget
// annotation enclosing no area still needs an appearance dictionary.
//
// ISO 19005-1, 6.9 t2 is keyed to veraPDF's PDWidgetAnnot object and its whole
// test is "AP != null": unlike ISO 19005-2/3/4, 6.3.3 t1, which opens with
// "width == 0 && height == 0", part 1 grants no exemption for a zero-area
// rectangle. Invisible signature fields are written /Rect [0 0 0 0], so
// exempting them here left the validator reporting a violation remediation
// could never answer.
func TestFixAnnotAppearanceZeroAreaWidgetPart1(t *testing.T) {
	t.Parallel()

	pdf := zeroAreaAnnotPDF("Widget", "/FT /Sig /T (sig)")
	if !annotGainedAP(t, pdf, "PDF/A-1b") {
		t.Error("a zero-area widget gained no /AP; ISO 19005-1 6.9 t2 requires one whatever the /Rect area")
	}
}

// TestFixAnnotAppearanceZeroAreaWidgetExemptAbovePart1 pins the other side of
// the split: parts 2 and up do exempt a zero-area annotation, so the same
// widget keeps no appearance there and the file gains no object it does not
// need.
func TestFixAnnotAppearanceZeroAreaWidgetExemptAbovePart1(t *testing.T) {
	t.Parallel()

	pdf := zeroAreaAnnotPDF("Widget", "/FT /Sig /T (sig)")
	for _, profile := range []string{"PDF/A-2b", "PDF/A-3b", "PDF/A-4"} {
		if annotGainedAP(t, pdf, profile) {
			t.Errorf("%s: a zero-area widget gained an /AP, but 6.3.3 t1 exempts width == 0 && height == 0", profile)
		}
	}
}

// TestFixAnnotAppearanceZeroAreaNonWidgetPart1 pins the scope of the part-1
// rule. ISO 19005-1 states the appearance requirement on the widget alone;
// its 6.5.3 tests 4 to 6 each open with "AP != N", constraining an appearance
// that exists rather than demanding one. A zero-area annotation that is not a
// widget therefore stays exempt, and inventing an appearance for it would
// answer no rule.
func TestFixAnnotAppearanceZeroAreaNonWidgetPart1(t *testing.T) {
	t.Parallel()

	pdf := zeroAreaAnnotPDF("Square", "")
	if annotGainedAP(t, pdf, "PDF/A-1b") {
		t.Error("a zero-area non-widget gained an /AP; no part-1 rule requires one")
	}
}

// TestFixAnnotAppearanceZeroAreaWidgetUnknownProfile pins the default. A caller
// that names no profile gets the exempting behaviour, which is what every
// caller outside a PDF/A-1 run relies on.
func TestFixAnnotAppearanceZeroAreaWidgetUnknownProfile(t *testing.T) {
	t.Parallel()

	pdf := zeroAreaAnnotPDF("Widget", "/FT /Sig /T (sig)")
	if annotGainedAP(t, pdf, "") {
		t.Error("an unnamed profile must keep the zero-area exemption")
	}
}
