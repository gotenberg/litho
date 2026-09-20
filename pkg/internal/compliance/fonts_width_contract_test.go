package compliance

import (
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestWidthMismatchContext_IsTheWordingTheFixerMatches pins a free-text
// contract that spans two packages.
//
// remediation's matchFontWidth is the only matcher in that registry carrying no
// rule identity: it claims a violation because the Context contains
// [WidthMismatchContext], and nothing else. Nine emit sites, eight here and one
// in fonts.go, are the only producers of that wording, so rewording any of them
// retires the fixer silently.
//
// A matcher-level assertion cannot catch that. remediation's own
// TestMatchFontWidth feeds a HARDCODED context string and would keep passing
// after the checker stopped emitting it, which is the same blind spot
// TestFixFontIssues_RepairsWidthsReportedUnderPDFUA documents for the repair
// gate. So this drives the real checker to a real mismatch and asserts the
// LITERAL wording: changing the constant fails here rather than in production.
func TestWidthMismatchContext_IsTheWordingTheFixerMatches(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	rule := Rule_19005_1_6_3_6_t01

	// GID 32 (space) measures 488 in the program; /Widths declares 226 for the
	// same code, so a rendered space is a genuine mismatch.
	binaryWidths := map[uint16]float64{0: 500, 32: 488}
	var unitsPerEm uint16 = 1000

	widths := make(ast.PdfArray, 1)
	widths[0] = ast.PdfReal(226.0)
	fontDict := ast.PdfDict{
		"FirstChar": ast.PdfInt(32),
		"Widths":    widths,
	}
	identMap := map[uint16]uint16{32: 32}
	usage := &fontUsageInfo{visible: true, charCodes: map[byte]bool{32: true}}

	results := checkSimpleFontWidths(ctx, nopDoc(), fontDict, binaryWidths,
		unitsPerEm, "TestFont", 1, rule, identMap, usage)
	if len(results) == 0 {
		t.Fatal("expected a width violation for the declared 226 against the program's 488")
	}

	// The literal, not the constant: asserting against the constant would be a
	// tautology that survives any rewording.
	const wording = "width mismatch"
	if WidthMismatchContext != wording {
		t.Errorf("WidthMismatchContext = %q, want %q: remediation's matchFontWidth claims on this substring, so changing it retires the fixer",
			WidthMismatchContext, wording)
	}
	if !strings.Contains(results[0].Context, wording) {
		t.Errorf("emitted Context %q does not carry %q, so matchFontWidth would not claim it",
			results[0].Context, wording)
	}
}
