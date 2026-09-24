package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// cidWidthAfterFix reads back the CID 1 width the fixer persisted onto the
// descendant CIDFont, and reports whether that object was written at all.
func cidWidthAfterFix(t *testing.T, alloc *writer.ObjAllocator) (float64, bool) {
	t.Helper()

	pending, ok := alloc.Pending()[5]
	if !ok {
		return 0, false
	}
	cid, ok := pending.(ast.PdfDict)
	if !ok {
		return 0, false
	}
	w, ok := cid["W"].(ast.PdfArray)
	if !ok || len(w) != 2 {
		return 0, false
	}
	widths, ok := w[1].(ast.PdfArray)
	if !ok || len(widths) != 1 {
		return 0, false
	}
	return document.Float(widths[0]), true
}

// TestFixFontIssues_RepairsWidthsReportedUnderPDFUA drives the whole fixer,
// because the defect was in the violation scan rather than in the width math.
//
// ISO 14289-1, 7.21.5 and ISO 14289-2, 8.4.5.6 restate the ISO 19005-2, 6.2.11.5
// requirement that /Widths agree with the embedded program. [matchFontFixes]
// learned 7.21.5 in 2eb351fa, so the fixer ran on PDF/UA documents, but
// [fixFontIssues] gated the rewrite on its own clause list, written in 39a382e2,
// which knew only the PDF/A spellings. The repair was invoked and did nothing
// under a PDF/UA profile while the same file repaired under PDF/A.
//
// A matcher-level assertion cannot catch that. TestMatchFontFixesClaimsThePDFUAWidthClause
// passed throughout, which is why this arm drives [fixFontIssues] end to end and
// asserts the persisted /W value.
//
// # Corpus evidence
//
//   - verapdf PDF_A-2b__veraPDF_test_suite_6-2-11-5-t01-fail-a: reported
//     "corrected /Widths" under PDF/A-2b while the same file under PDF/UA-1 and
//     PDF/UA-2 reported no width fix and kept reporting the mismatch.
func TestFixFontIssues_RepairsWidthsReportedUnderPDFUA(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		clause  string
		profile string
		repair  bool
	}{
		{"ISO 19005-2 6.2.11.5, the spelling that always worked", "6.2.11.5", "PDF/A-2b", true},
		{"ISO 14289-1 7.21.5, claimed but gate-blocked", "7.21.5", "PDF/UA-1", true},
		{"ISO 14289-2 8.4.5.6, in neither matcher nor gate", "8.4.5.6", "PDF/UA-2", true},
		// The gate exists so widths are rewritten ONLY where a mismatch is
		// reported: rewriting uninvited turned conforming /Widths into fresh
		// violations, which is what fixFontIssues documents at its head. An
		// unrelated font clause must therefore leave /W alone. Without this arm
		// the test would also pass if the gate were deleted outright.
		{"an unrelated font clause leaves /W untouched", "8.4.5.7", "PDF/UA-2", false},
	} {
		doc, alloc := newTestDoc(t, directCIDFontPDF(false))

		violation := compliance.Violation{
			Rule:    compliance.Rule{Clause: tc.clause, TestNumber: 1},
			Context: "font F0 on page 1: glyph width mismatch",
		}
		if _, err := fixFontIssues(context.Background(), alloc, doc, Options{},
			[]compliance.Violation{violation}, tc.profile); err != nil {
			t.Fatalf("%s: fixFontIssues: %v", tc.name, err)
		}

		got, written := cidWidthAfterFix(t, alloc)
		if !tc.repair {
			if written {
				t.Errorf("%s: /W was rewritten to %v for a clause that reports no width mismatch",
					tc.name, got)
			}
			continue
		}
		if !written {
			t.Errorf("%s: clause %s reported a width mismatch and nothing was rewritten",
				tc.name, tc.clause)
			continue
		}
		// directCIDFontPDF declares 999 for CID 1 where the program advances 500.
		if got != 500 {
			t.Errorf("%s: /W for CID 1 is %v, want 500 (the embedded program's advance)",
				tc.name, got)
		}
	}
}
