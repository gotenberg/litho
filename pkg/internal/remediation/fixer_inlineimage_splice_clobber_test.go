package remediation

import (
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// textShown returns the strings the Tj operations of ops show, in order.
func textShown(ops []content.Operation) []string {
	var shown []string
	for _, op := range ops {
		if op.Operator != "Tj" || len(op.Operands) == 0 {
			continue
		}
		if s, ok := op.Operands[len(op.Operands)-1].(ast.PdfString); ok {
			shown = append(shown, string(s))
		}
	}
	return shown
}

// TestFixStringLimit_SpliceKeepsTheRenamedNames is the clobber guard against
// the real peer that rewrites the same stream first: name-utf8 (registry
// ahead of string-limit) renames an over-long colour space an inline image
// names, and the string truncation builds on the renamed stream, so both
// repairs land and the image data survives both.
func TestFixStringLimit_SpliceKeepsTheRenamedNames(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	long := strings.Repeat("a", pdfA2StringLimit+10)
	data := "q BT /F1 12 Tf (" + long + ") Tj ET BI /W 4 /H 1 /BPC 8 /CS " + longKeySpelled + spliceImageData + " Q"
	doc, alloc := newTestDoc(t, assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R" +
			" /Resources << /Font << /F1 5 0 R >> /ColorSpace << " + longKeySpelled + " /DeviceGray >> >> >>",
		streamBody("", data),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}))
	if _, err := fixNameUTF8(ctx, alloc, doc, Options{}, nameLengthViolations, "PDF/A-2b"); err != nil {
		t.Fatalf("fixNameUTF8: %v", err)
	}
	if body := string(pendingStreamBytes(t, alloc, 4)); strings.Contains(body, longKeySpelled) {
		t.Fatalf("control: name-utf8 did not rename the colour space: %q", body)
	}

	if _, err := fixStringLimit(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixStringLimit: %v", err)
	}
	body := pendingStreamBytes(t, alloc, 4)
	ops := validationOps(t, data, body)
	if strings.Contains(string(body), longKeySpelled) {
		t.Errorf("the truncation rebuilt the page from the document: %q", body)
	}
	if shown := textShown(ops); len(shown) != 1 || len(shown[0]) != pdfA2StringLimit {
		t.Errorf("want one string of %d bytes, got %d strings", pdfA2StringLimit, len(shown))
	}
}

// TestFixMarkedContentLang_SpliceKeepsTheTruncatedStrings is the clobber
// guard against the real peer that rewrites the same stream first under an
// aggressive PDF/A-2a remediation: string-limit (resource tier, ahead of the
// content tier) truncates an over-long string on a page holding an inline
// image, and the marked-content /Lang repair builds on the truncated stream,
// so both repairs land and the image data survives both.
func TestFixMarkedContentLang_SpliceKeepsTheTruncatedStrings(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	long := strings.Repeat("a", pdfA2StringLimit+10)
	data := "/P <</Lang (-pt) /MCID 0>> BDC BT /F1 12 Tf (" + long + ") Tj ET " + spliceImage + " EMC"
	doc, alloc := newTestDoc(t, inlineSplicePage(data))
	if _, err := fixStringLimit(ctx, alloc, doc, Options{}, nil, "PDF/A-2a"); err != nil {
		t.Fatalf("fixStringLimit: %v", err)
	}
	if body := pendingStreamBytes(t, alloc, 4); strings.Contains(string(body), long) {
		t.Fatalf("control: string-limit did not truncate the string: %d bytes", len(body))
	}

	fixes, err := fixMarkedContentLang(ctx, alloc, doc, Options{}, nil, "PDF/A-2a")
	if err != nil {
		t.Fatalf("fixMarkedContentLang: %v", err)
	}
	if len(fixes) != 1 {
		t.Errorf("fixes = %q, want the /Lang removed", fixes)
	}
	body := pendingStreamBytes(t, alloc, 4)
	ops := validationOps(t, data, body)
	if strings.Contains(string(body), "/Lang") {
		t.Errorf("the marked-content /Lang survived: %q", body[:min(len(body), 80)])
	}
	if shown := textShown(ops); len(shown) != 1 || len(shown[0]) != pdfA2StringLimit {
		t.Errorf("the /Lang repair rebuilt the page from the document: want one string of %d bytes, got %d strings", pdfA2StringLimit, len(shown))
	}
}

// TestFixTextControl_SpliceKeepsTheUnknownOpsRepair is the clobber guard
// against the real peer that splices the same stream first: unknown-ops
// (registry ahead of text-control) removes an undefined operator from a page
// holding an inline image, and the control-byte strip builds on the spliced
// stream, so the operator stays removed and the image data survives both.
func TestFixTextControl_SpliceKeepsTheUnknownOpsRepair(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := "q BT /F1 12 Tf (a\x01b) Tj ET zz " + spliceImage + " Q"
	doc, alloc := newTestDoc(t, inlineSplicePage(data))
	if _, err := fixUnknownOps(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixUnknownOps: %v", err)
	}
	if body := string(pendingStreamBytes(t, alloc, 4)); strings.Contains(body, "zz") {
		t.Fatalf("control: unknown-ops did not remove zz: %q", body)
	}

	if _, err := fixTextControl(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixTextControl: %v", err)
	}
	if got, want := string(pendingStreamBytes(t, alloc, 4)), "q BT /F1 12 Tf (ab) Tj\n ET  "+spliceImage+" Q"; got != want {
		t.Errorf("page = %q, want %q", got, want)
	}
	validationOps(t, data, pendingStreamBytes(t, alloc, 4))
}

// TestFixAggressiveLimits_SpliceKeepsTheAggressiveRepair is the clobber guard
// against the real peer that splices the same stream first: the aggressive
// content rewrite (registry ahead of aggressive-limits) replaces an invalid
// rendering intent on a page holding an inline image, and the q/Q nesting
// repair builds on the spliced stream, so both repairs land and the image
// data survives both.
func TestFixAggressiveLimits_SpliceKeepsTheAggressiveRepair(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := strings.Repeat("q ", pdfAGraphicsDepth+1) + "/Foo ri " + spliceImage + strings.Repeat(" Q", pdfAGraphicsDepth+1)
	doc, alloc := newTestDoc(t, inlineSplicePage(data))
	if _, err := fixAggressive(ctx, alloc, doc, Options{}, aggressiveIntent, "PDF/A-2b"); err != nil {
		t.Fatalf("fixAggressive: %v", err)
	}
	if body := string(pendingStreamBytes(t, alloc, 4)); strings.Contains(body, "/Foo") {
		t.Fatalf("control: the aggressive rewrite did not replace the intent: %q", body)
	}

	if _, err := fixAggressiveLimits(ctx, alloc, doc, Options{}, qNestingViolation, "PDF/A-2b"); err != nil {
		t.Fatalf("fixAggressiveLimits: %v", err)
	}
	body := pendingStreamBytes(t, alloc, 4)
	ops := validationOps(t, data, body)
	if bad := invalidIntents(t, body); len(bad) > 0 {
		t.Errorf("the nesting repair rebuilt the page from the document: validation reads the intents %v", bad)
	}
	if d := maxQDepth(ops); d != pdfAGraphicsDepth {
		t.Errorf("validation reads a q/Q nesting of %d, want %d", d, pdfAGraphicsDepth)
	}
}
