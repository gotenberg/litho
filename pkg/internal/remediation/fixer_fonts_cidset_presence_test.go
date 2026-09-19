package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// descriptorAfterType0Repair runs the Type0 repair over the shared
// cidfont_notdef_min.ttf fixture and returns the rewritten FontDescriptor,
// which is object 6 of [cidFontDWPDF]. The descriptor there carries no
// /CIDSet, which is exactly the shape ISO 19005-1, 6.3.5 t3 reports.
func descriptorAfterType0Repair(t *testing.T, createCIDSet bool) (ast.PdfDict, []string) {
	t.Helper()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, cidFontDWPDF(t, ""))
	fontDict, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 4})
	if err != nil {
		t.Fatalf("resolving the Type0 font: %v", err)
	}
	fixes := fixType0Font(ctx, doc, alloc, fontDict, ast.PdfIndirectRef{ObjectNum: 4},
		false, false, createCIDSet)
	fd, _ := alloc.Pending()[6].(ast.PdfDict)
	return fd, fixes
}

// TestFixType0Font_AddsAMissingCIDSet pins the repair of a presence rule.
//
// ISO 19005-1, 6.3.5 t3 requires a rendered CIDFont subset to INCLUDE a
// /CIDSet, so the violation reports the key's absence. The repair was reachable
// only through "if _, ok := fdDict[\"CIDSet\"]; ok", which skips exactly that
// population, and clause 6.3.5 raised the /CharSet flag besides, which reaches
// simple fonts only. 240 corpus cells stayed unfixed across PDF_A-1a and
// PDF_A-1b with litho and veraPDF agreeing on every one of them.
func TestFixType0Font_AddsAMissingCIDSet(t *testing.T) {
	t.Parallel()

	fd, fixes := descriptorAfterType0Repair(t, true)
	if fd == nil {
		t.Fatal("the font descriptor was not rewritten")
	}
	if _, ok := fd["CIDSet"]; !ok {
		t.Errorf("no /CIDSet was added, got descriptor keys %v", keysOf(fd))
	}
	if len(fixes) == 0 {
		t.Error("the repair reported nothing while writing a /CIDSet")
	}
}

// TestFixType0Font_LeavesAMissingCIDSetAloneWhenUnreported is the negative
// control for the test above: without the presence flag nothing may be written.
//
// Without this arm, "a /CIDSet appeared" would not distinguish the new gate
// from some other path supplying one, and the conditional rules
// (6.2.11.4.2 t2, 7.21.4.2 t2) govern only a /CIDSet that already exists, so
// creating one uninvited would answer a rule nobody reported.
func TestFixType0Font_LeavesAMissingCIDSetAloneWhenUnreported(t *testing.T) {
	t.Parallel()

	fd, fixes := descriptorAfterType0Repair(t, false)
	if fd != nil {
		if _, ok := fd["CIDSet"]; ok {
			t.Error("a /CIDSet was written although no presence violation was reported")
		}
	}
	for _, f := range fixes {
		if f == "" {
			continue
		}
		t.Logf("unrelated repair reported: %s", f)
	}
}

// TestMatchFontFixes_ClaimsBothTestsOfClause635 pins the discrimination the
// CIDSet flag depends on: clausePrefix cannot tell t2 from t3 because both live
// under clause 6.3.5, so the scan keys on TestNumber instead.
//
// Only t3 drives a repair. Supplying the /CharSet t2 asks for was measured as a
// false fix, litho accepting the written string while veraPDF kept reporting
// the rule, so t2 stays claimed by the matcher but reaches the regeneration
// path alone.
func TestMatchFontFixes_ClaimsBothTestsOfClause635(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		rule compliance.Rule
	}{
		{"CharSet presence t02", compliance.Rule_19005_1_6_3_5_t02},
		{"CIDSet presence t03", compliance.Rule_19005_1_6_3_5_t03},
	} {
		v := compliance.Violation{Rule: tc.rule}
		if !matchFontFixes(v) {
			t.Errorf("%s: the font fixer no longer claims it", tc.name)
		}
		if v.Rule.Clause != "6.3.5" {
			t.Errorf("%s: clause is %q, want 6.3.5", tc.name, v.Rule.Clause)
		}
	}

	if compliance.Rule_19005_1_6_3_5_t02.TestNumber == compliance.Rule_19005_1_6_3_5_t03.TestNumber {
		t.Fatal("t02 and t03 share a TestNumber, so the flags cannot be separated")
	}
}

// TestFixFontIssues_WiresTheCIDSetPresenceViolation drives the whole fixer,
// not just the Type0 repair, because the change is in the violation scan.
//
// The arms above call [fixType0Font] directly and so would pass even if the
// scan never set the flag or never carried it to the call site, which is the
// part that was broken: clause 6.3.5 raised only the /CharSet flag.
func TestFixFontIssues_WiresTheCIDSetPresenceViolation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, cidFontDWPDF(t, ""))

	fixes, err := fixFontIssues(ctx, alloc, doc, Options{},
		[]compliance.Violation{{Rule: compliance.Rule_19005_1_6_3_5_t03}}, "PDF/A-1b")
	if err != nil {
		t.Fatalf("fixFontIssues: %v", err)
	}

	fd, _ := alloc.Pending()[6].(ast.PdfDict)
	if fd == nil {
		t.Fatalf("the font descriptor was not rewritten; fixes=%v", fixes)
	}
	if _, ok := fd["CIDSet"]; !ok {
		t.Errorf("the reported 6.3.5 t3 violation produced no /CIDSet; fixes=%v", fixes)
	}
}

// TestFixFontIssues_DoesNotInventACIDSetForTheConditionalRule is the companion
// negative control. ISO 19005-2, 6.2.11.4.2 t2 governs a /CIDSet that already
// exists, so a descriptor without one must come back untouched: answering it by
// writing a set would repair a rule nobody reported.
func TestFixFontIssues_DoesNotInventACIDSetForTheConditionalRule(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, cidFontDWPDF(t, ""))

	if _, err := fixFontIssues(ctx, alloc, doc, Options{},
		[]compliance.Violation{{Rule: compliance.Rule_19005_2_6_2_11_4_2_t02}}, "PDF/A-2b"); err != nil {
		t.Fatalf("fixFontIssues: %v", err)
	}

	if fd, _ := alloc.Pending()[6].(ast.PdfDict); fd != nil {
		if _, ok := fd["CIDSet"]; ok {
			t.Error("a /CIDSet was invented for a rule that only governs an existing one")
		}
	}
}

// keysOf lists a dictionary's keys for a failure message.
func keysOf(d ast.PdfDict) []string {
	out := make([]string, 0, len(d))
	for k := range d {
		out = append(out, string(k))
	}
	return out
}
