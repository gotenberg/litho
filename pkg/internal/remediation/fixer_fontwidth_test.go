package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
)

func TestMatchFontWidth(t *testing.T) {
	t.Parallel()
	hit := compliance.Violation{
		Rule:    compliance.Rule_19005_2_6_2_11_5_t01,
		Context: "font ABC on page 1: char 65 width mismatch (PDF=500.0, font=600.0)",
	}
	if !matchFontWidth(hit) {
		t.Error("must match a glyph-width-mismatch violation")
	}
	if matchFontWidth(compliance.Violation{Rule: compliance.Rule_19005_2_6_2_11_5_t01, Context: "font ABC: missing /Widths array"}) {
		t.Error("must not match the missing-/Widths context (a different repair)")
	}
}

// TestFixFontWidth_NoopWithoutProgram confirms the fixer leaves a font alone when
// the embedded program cannot be read, so it never guesses widths. The correction
// behaviour itself is validated against real fonts by the verapdf remediation
// corpus (synthetic font programs are impractical to hand-author).
func TestFixFontWidth_NoopWithoutProgram(t *testing.T) {
	pdf := fontPDF("<< /Type /Font /Subtype /Type1 /BaseFont /ABCDEF+Helvetica /FirstChar 65 /Widths [500 600] >>")
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixFontWidth(context.Background(), alloc, doc, Options{}, nil, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixFontWidth: %v", err)
	}
	if len(descs) != 0 || len(alloc.Pending()) != 0 {
		t.Errorf("a font with no embedded program must be left alone: descs=%v pending=%d", descs, len(alloc.Pending()))
	}
}

func TestFixFontWidth_NoopNonSimpleFont(t *testing.T) {
	pdf := fontPDF("<< /Type /Font /Subtype /Type0 /BaseFont /ABCDEF+Sans /Encoding /Identity-H >>")
	doc, alloc := newTestDoc(t, pdf)

	descs, err := fixFontWidth(context.Background(), alloc, doc, Options{}, nil, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixFontWidth: %v", err)
	}
	if len(descs) != 0 || len(alloc.Pending()) != 0 {
		t.Errorf("a Type0 font is not a simple-font width target: descs=%v pending=%d", descs, len(alloc.Pending()))
	}
}
