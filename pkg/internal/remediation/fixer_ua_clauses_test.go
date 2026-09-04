package remediation

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
)

// uaClauseViolation builds a violation for a clause with the given context.
func uaClauseViolation(clause, context string) compliance.Violation {
	return compliance.Violation{
		Rule: compliance.Rule{
			Clause:      clause,
			TestNumber:  2,
			Description: "All non-symbolic TrueType fonts shall have either MacRomanEncoding or WinAnsiEncoding",
		},
		Context: context,
	}
}

// TestMatchTrueTypeEncodingClaimsThePDFUAClauses pins that the TrueType
// encoding repair is offered to PDF/UA as well.
//
// ISO 14289-1, 7.21.6 test 2 and ISO 14289-2, 8.4.5.7 test 2 restate
// ISO 19005-2, 6.2.11.6 test 2 word for word. Claiming only the PDF/A clause
// numbers left the repair idle on PDF/UA documents: PDF_UA-2__8.4.5.7-t02
// declares /BaseEncoding /Identity on a non-symbolic TrueType font, which the
// fixer knows how to normalise.
func TestMatchTrueTypeEncodingClaimsThePDFUAClauses(t *testing.T) {
	t.Parallel()

	for _, clause := range []string{"6.3.7", "6.2.11.6", "6.2.10.6", "7.21.6", "8.4.5.7"} {
		v := uaClauseViolation(clause, "font TT0 on page 1 BaseEncoding=/Identity")
		if !matchTrueTypeEncoding(v) {
			t.Errorf("clause %s is not claimed by the TrueType encoding fixer", clause)
		}
	}
}

// TestMatchFontFixesClaimsThePDFUAWidthClause pins the same for the glyph
// width rule, which ISO 14289-1, 7.21.5 restates from ISO 19005-2, 6.2.11.5.
func TestMatchFontFixesClaimsThePDFUAWidthClause(t *testing.T) {
	t.Parallel()

	for _, clause := range []string{"6.3.6", "6.2.11.5", "6.2.10.5", "7.21.5"} {
		v := compliance.Violation{
			Rule:    compliance.Rule{Clause: clause, TestNumber: 1},
			Context: "font F1 on page 1: glyph width mismatch",
		}
		if !matchFontFixes(v) {
			t.Errorf("clause %s is not claimed by the font fixer", clause)
		}
	}
}

// TestMatchTrueTypeEncodingIgnoresAnUnrelatedClause is the control against a
// fixer claiming work it cannot do.
func TestMatchTrueTypeEncodingIgnoresAnUnrelatedClause(t *testing.T) {
	t.Parallel()

	v := uaClauseViolation("6.1.13", "page 1 /MediaBox: width=2.0 height=2.0")
	if matchTrueTypeEncoding(v) {
		t.Error("a page boundary violation was claimed by the TrueType encoding fixer")
	}
}
