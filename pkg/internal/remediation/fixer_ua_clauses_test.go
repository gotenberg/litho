package remediation

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
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
// numbers left the repair idle on PDF/UA documents, including a non-symbolic
// TrueType font declaring /BaseEncoding /Identity, which the fixer knows how to
// normalise.
//
// # Corpus evidence
//
//   - verapdf PDF_UA-2__8.4.5.7-t02: declares /BaseEncoding /Identity on a
//     non-symbolic TrueType font, which the fixer knows how to normalise.
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
// width rule, which ISO 14289-1, 7.21.5 and ISO 14289-2, 8.4.5.6 both restate
// from ISO 19005-2, 6.2.11.5.
//
// Claiming is necessary and NOT sufficient: this test passed for three months
// while the repair was dead, because [fixFontIssues] gated the rewrite on its
// own clause list that knew only the PDF/A spellings. See
// TestFixFontIssues_RepairsWidthsReportedUnderPDFUA for the arm that covers it.
func TestMatchFontFixesClaimsThePDFUAWidthClause(t *testing.T) {
	t.Parallel()

	for _, clause := range []string{"6.3.6", "6.2.11.5", "6.2.10.5", "7.21.5", "8.4.5.6"} {
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

// TestMatchFontFixesClaimsThePDFUAToUnicodeClauses pins that the Unicode
// mapping repair is offered to PDF/UA as well.
//
// ISO 14289-1, 7.21.7 test 1 and ISO 14289-2, 8.4.5.8 test 1 state the same
// requirement as ISO 19005-1, 6.3.8 and ISO 19005-2, 6.2.11.7.2: the font
// dictionary shall map every character code the page uses to a Unicode value.
// Test 2 of each restates the U+0000/FEFF/FFFE restriction this matcher already
// claims as 6.2.10.7, so claiming the clause prefix reaches both tests at once.
func TestMatchFontFixesClaimsThePDFUAToUnicodeClauses(t *testing.T) {
	t.Parallel()

	for _, clause := range []string{"6.3.8", "6.2.11.7.2", "7.21.7", "8.4.5.8"} {
		v := compliance.Violation{
			Rule:    compliance.Rule{Clause: clause, TestNumber: 1},
			Context: "font F1 on page 1",
		}
		if !matchFontFixes(v) {
			t.Errorf("clause %s is not claimed by the font fixer", clause)
		}
	}
}

// uaToUnicodeDrawnPDF builds a one-page document that DRAWS with a Type 1 font
// stating /WinAnsiEncoding and carrying no /ToUnicode.
//
// Three properties make it the right fixture for this arm. The page has a
// content stream, because [fixFontIssues] holds the repair to the codes the
// page actually shows and passes it shown[fontName]: a page that draws nothing
// yields an empty set and [fixSimpleFontToUnicode] returns at its first guard.
// The font declares no /FontDescriptor, so [fixSimpleFont] returns at ITS first
// guard and the /ToUnicode path is the only one that can produce a fix, which
// keeps the assertion below unambiguous. And WinAnsiEncoding maps both 'A' and
// 'B', so the all-shown-codes-must-map gate is satisfied.
func uaToUnicodeDrawnPDF() []byte {
	const content = "BT /F1 12 Tf 20 700 Td (AB) Tj ET\n"
	return assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 5 0 R" +
			" /Resources << /Font << /F1 4 0 R >> >> >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
	})
}

// TestClassifyRoutesThePDFUAToUnicodeClauseToTheFontFixer is the arm that
// covers what claiming alone does not.
//
// The two halves here fail for opposite reasons, which is why both belong in
// one test. ROUTING is what [matchFontFixes] decides: [classify] builds the
// plan by asking every registry entry whether it claims the violation, so a
// clause no matcher claims never reaches [fixFontIssues] and the document keeps
// its missing mapping. REPAIR is deliberately clause-independent: the
// /ToUnicode path self-gates on [needsToUnicode] and never reads the violation
// list, unlike the width and /CIDSet repairs which are gated on flags computed
// from it. Calling fixFontIssues directly therefore attaches a mapping whatever
// clause is passed, and a test written that way would pass while the repair
// stayed unreachable in production. Asserting both together is what shows the
// routing leads somewhere that works.
//
// # Corpus evidence
//
//   - verapdf PDF_UA-1__7.21.7-t01-fail-a and PDF_UA-2__8.4.5.8-t01-fail-a:
//     measured before this was claimed, both are repaired in full when
//     validated as PDF/A-2u, where the clause is spelled 6.2.11.7.2, and come
//     back "Fixed: 0, Residual: 1" under their own profile.
func TestClassifyRoutesThePDFUAToUnicodeClauseToTheFontFixer(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		clause  string
		profile string
		routed  bool
	}{
		{"ISO 19005-1 6.3.8, the spelling that always worked", "6.3.8", "PDF/A-1a", true},
		{"ISO 14289-1 7.21.7", "7.21.7", "PDF/UA-1", true},
		{"ISO 14289-2 8.4.5.8", "8.4.5.8", "PDF/UA-2", true},
		// The TrueType encoding rule is matchTrueTypeEncoding's work. Without
		// this arm the test would pass just as well if the matcher claimed
		// every clause it is shown.
		{"an unrelated UA font clause is not routed here", "8.4.5.7", "PDF/UA-2", false},
	} {
		v := compliance.Violation{
			Rule:    compliance.Rule{Clause: tc.clause, TestNumber: 1},
			Context: "font F1 on page 1",
		}

		var routed bool
		for _, entry := range classify([]compliance.Violation{v}) {
			if entry.f.name == "fonts" {
				routed = true
			}
		}
		if routed != tc.routed {
			t.Errorf("%s: clause %s routed to the font fixer = %v, want %v",
				tc.name, tc.clause, routed, tc.routed)
		}
		if !tc.routed {
			continue
		}

		doc, alloc := newTestDoc(t, uaToUnicodeDrawnPDF())
		if _, err := fixFontIssues(context.Background(), alloc, doc, Options{},
			[]compliance.Violation{v}, tc.profile); err != nil {
			t.Fatalf("%s: fixFontIssues: %v", tc.name, err)
		}

		font, _ := alloc.Pending()[4].(ast.PdfDict)
		if font == nil {
			t.Errorf("%s: the font at object 4 was never rewritten, so the routing "+
				"asserted above reaches a repair that does nothing", tc.name)
			continue
		}
		if _, ok := font["ToUnicode"].(ast.PdfIndirectRef); !ok {
			t.Errorf("%s: no /ToUnicode was attached; font = %v", tc.name, font)
		}
	}
}
