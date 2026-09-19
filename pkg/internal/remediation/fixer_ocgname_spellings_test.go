package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// TestMatchCatalogCleanup_ClaimsEveryOCGNameSpelling pins the gap this closes.
//
// "Each optional content configuration dictionary ... shall contain the Name
// key" carries four clause numbers: ISO 19005-4 6.10, ISO 19005-2 and 19005-3
// 6.9, ISO 14289-1 7.10 and ISO 14289-2 8.7. Only 6.10 reached
// [fixOCGConfigs], so one veraPDF fixture was repaired as PDF/A-4 ("added
// /Name to OCG default config /D") and left flagged as PDF/A-2b.
func TestMatchCatalogCleanup_ClaimsEveryOCGNameSpelling(t *testing.T) {
	t.Parallel()

	for _, rule := range []compliance.Rule{
		compliance.Rule_19005_4_6_10_t01,
		compliance.Rule_19005_2_6_9_t01,
		compliance.Rule_19005_3_6_9_t01,
		compliance.Rule_14289_1_7_10_t01,
		compliance.Rule_14289_2_8_7_t01,
	} {
		if !matchCatalogCleanup(compliance.Violation{Rule: rule}) {
			t.Errorf("must claim %s/%s t%d", rule.Specification, rule.Clause, rule.TestNumber)
		}
	}
}

// TestEnsureOCGConfigNameTreatsAnEmptyNameAsMissing pins the second half of
// this repair.
//
// The check tested key PRESENCE (`if _, ok := d["Name"]; ok`), while
// compliance calls hasNonEmptyName, which resolves the value and rejects an
// empty string. veraPDF's 7.10-t01-fail-a carries `/D << /Name () >>`, so the
// violation was reported and the repair declined: routing the clause to this
// fixer cleared 32 of 46 cells and left the 14 empty-name ones flagged.
func TestEnsureOCGConfigNameTreatsAnEmptyNameAsMissing(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		config ast.PdfDict
		want   bool
	}{
		{"empty string", ast.PdfDict{"Name": ast.PdfString("")}, true},
		{"missing key", ast.PdfDict{"Order": ast.PdfArray{}}, true},
		{"non-empty string", ast.PdfDict{"Name": ast.PdfString("D")}, false},
	} {
		alloc := writer.NewObjAllocator(10)
		replacement, fixed := ensureOCGConfigName(context.Background(), alloc, nil, tc.config, "D")
		if fixed != tc.want {
			t.Errorf("%s: fixed = %v, want %v", tc.name, fixed, tc.want)
			continue
		}
		if !tc.want {
			continue
		}
		if replacement == nil {
			t.Errorf("%s: a direct dictionary must be returned to install", tc.name)
			continue
		}
		if got, ok := replacement["Name"].(ast.PdfString); !ok || string(got) == "" {
			t.Errorf("%s: /Name = %v, want a non-empty string", tc.name, replacement["Name"])
		}
	}
}

// TestMatchCatalogCleanup_DeclinesWhatItCannotRepair is the negative control
// for the claim's scope.
//
// [fixOCGConfigs] adds a missing /Name and nothing else. It does not make the
// names unique (test 2), does not complete the /Order array (test 3) and does
// not remove /AS. Claiming those rules would mark them handled while they
// survive, the failure mode [FullRewriteNote] already demonstrates elsewhere.
func TestMatchCatalogCleanup_DeclinesWhatItCannotRepair(t *testing.T) {
	t.Parallel()

	for _, rule := range []compliance.Rule{
		compliance.Rule_19005_2_6_9_t02, // unique /Name
		compliance.Rule_19005_2_6_9_t03, // /Order must list every OCG
		compliance.Rule_19005_2_6_9_t04, // /AS shall not appear
		compliance.Rule_14289_1_7_10_t02,
		compliance.Rule_14289_2_8_7_t02,
	} {
		if matchCatalogCleanup(compliance.Violation{Rule: rule}) {
			t.Errorf("must not claim %s/%s t%d: fixOCGConfigs cannot repair it",
				rule.Specification, rule.Clause, rule.TestNumber)
		}
	}
}

// TestMatchCatalogCleanup_StillClaimsPermsAfterTheOCGArm is a regression pin.
//
// The OCG arm was first written as `return v.Rule.Clause == "6.9"` inside a
// switch on the specification. That claimed the OCG rule and silently
// UN-claimed every other test-1 violation in ISO 19005-2 and 19005-3, because
// returning false short-circuited the 6.1.12 Perms and Signature-Reference
// block that follows it. Twelve corpus cells stopped being repaired.
//
// The corpus check could not see it: 46 newly matched cells hid the 12 newly
// broken ones behind a net improvement, and only a comparison against the
// previous capture's per-fixture verdicts surfaced it.
func TestMatchCatalogCleanup_StillClaimsPermsAfterTheOCGArm(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		v    compliance.Violation
	}{
		{"Perms dictionary, by context", compliance.Violation{
			Rule:    compliance.Rule{Specification: "ISO_19005_2", Clause: "6.1.12", TestNumber: 1},
			Context: "Perms dictionary contains a key other than UR3 and DocMDP",
		}},
		{"Signature Reference, by context", compliance.Violation{
			Rule:    compliance.Rule{Specification: "ISO_19005_3", Clause: "6.1.12", TestNumber: 1},
			Context: "Signature Reference dictionary contains DigestLocation",
		}},
		{"permissions dictionary, by description", compliance.Violation{
			Rule: compliance.Rule{
				Specification: "ISO_19005_2", Clause: "6.1.12", TestNumber: 1,
				Description: "The permissions dictionary shall not contain any key other than UR3 or DocMDP",
			},
		}},
	} {
		if !matchCatalogCleanup(tc.v) {
			t.Errorf("%s: must still be claimed; the OCG arm must not short-circuit it", tc.name)
		}
	}
}

// TestMatchCatalogCleanup_DoesNotSwallowOtherPartsClause69 guards the literals.
//
// A clause number is not a rule id. ISO 19005-1 spells an AcroForm
// NeedAppearances rule 6.9 and ISO 19005-4 spells an embedded-file rule 6.9;
// neither is about optional content, and neither is repaired by adding a
// /Name to an OCG configuration. Writing this claim as clausePrefix(v, "6.9")
// would take all three.
func TestMatchCatalogCleanup_DoesNotSwallowOtherPartsClause69(t *testing.T) {
	t.Parallel()

	for _, rule := range []compliance.Rule{
		compliance.Rule_19005_1_6_9_t01, // PDAcroForm NeedAppearances
		compliance.Rule_19005_1_6_9_t02, // PDWidgetAnnot appearance
		compliance.Rule_19005_4_6_9_t01, // EmbeddedFile MIME subtype
		compliance.Rule_19005_4_6_9_t02, // file spec F and UF keys
	} {
		if matchCatalogCleanup(compliance.Violation{Rule: rule}) {
			t.Errorf("must not claim %s/%s t%d: clause 6.9 names a different rule there",
				rule.Specification, rule.Clause, rule.TestNumber)
		}
	}
}
