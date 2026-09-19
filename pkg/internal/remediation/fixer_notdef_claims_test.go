package remediation

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
)

// notdefRule() in the compliance package maps ONE requirement onto five spec
// spellings. These are those five, with the description text each carries.
var notdefSpellings = []compliance.Rule{
	{Specification: "ISO_19005_2", Clause: "6.2.11.8", TestNumber: 1, Description: "text-showing operators render the .notdef glyph"},
	{Specification: "ISO_19005_3", Clause: "6.2.11.8", TestNumber: 1, Description: "text-showing operators render the .notdef glyph"},
	{Specification: "ISO_19005_4", Clause: "6.2.10.9", TestNumber: 1, Description: "text-showing operators render the .notdef glyph"},
	{Specification: "ISO_14289_1", Clause: "7.21.8", TestNumber: 1, Description: "shall not contain a reference to the .notdef glyph"},
	{Specification: "ISO_14289_2", Clause: "8.4.5.9", TestNumber: 1, Description: "shall not contain a reference to the .notdef glyph"},
}

// TestMatchTextControl_ClaimsEveryNotdefSpelling pins that the .notdef repair
// claims the rule in all five spellings.
//
// It previously claimed only the three PDF/A ones, so the PDF/UA-2 spelling
// (8.4.5.9) went UNCLAIMED and the PDF/UA-1 spelling (7.21.8) was swept up by
// the /Lang fixers through a prefix collision. Both left the rule looking
// handled while nothing repaired it.
func TestMatchTextControl_ClaimsEveryNotdefSpelling(t *testing.T) {
	t.Parallel()

	for _, rule := range notdefSpellings {
		if !matchTextControl(compliance.Violation{Rule: rule}) {
			t.Errorf("must claim the .notdef rule as %s/%s t%d",
				rule.Specification, rule.Clause, rule.TestNumber)
		}
	}
}

// TestMatchTextControl_NeedsTheNotdefEvidence is the negative control that
// matters: the clause alone is not enough. Clause 6.2.11.8 also states rules
// that have nothing to do with .notdef, and stripping bytes for those would be
// content loss for no compliance gain.
func TestMatchTextControl_NeedsTheNotdefEvidence(t *testing.T) {
	t.Parallel()

	for _, rule := range notdefSpellings {
		bare := rule
		bare.Description = "some other requirement of the same clause"
		if matchTextControl(compliance.Violation{Rule: bare}) {
			t.Errorf("must not claim %s/%s t%d without .notdef evidence",
				rule.Specification, rule.Clause, rule.TestNumber)
		}
	}
}

// TestMatchTextControl_IgnoresUnrelatedClauses keeps the widened claim honest:
// the neighbouring glyph clauses are a different repair (ToUnicode), and this
// fixer must not take them.
func TestMatchTextControl_IgnoresUnrelatedClauses(t *testing.T) {
	t.Parallel()

	for _, rule := range []compliance.Rule{
		{Specification: "ISO_14289_2", Clause: "8.4.5.8", TestNumber: 1, Description: "map of all used character codes to Unicode values"},
		{Specification: "ISO_19005_2", Clause: "6.2.11.7.2", TestNumber: 1, Description: "map of all used character codes to Unicode values"},
		{Specification: "ISO_14289_1", Clause: "7.21.7", TestNumber: 1, Description: "map of all used character codes to Unicode values"},
		{Specification: "ISO_14289_1", Clause: "7.2", TestNumber: 2, Description: "Natural language in the Outline entries shall be determined"},
	} {
		if matchTextControl(compliance.Violation{Rule: rule}) {
			t.Errorf("must not claim %s/%s t%d", rule.Specification, rule.Clause, rule.TestNumber)
		}
	}
}

// TestMatchCatalogLang_DoesNotClaimTheFontClauses pins the prefix-collision fix.
//
// [clausePrefix] is a string prefix test, so the literal "7.2" matched 7.20 and
// every 7.21.x clause: ten rules, the whole PDF/UA-1 font family. Clause 7.21.8
// was claimed by the /Lang fixers and by nothing else, which is how a rule with
// no repairer looked handled.
func TestMatchCatalogLang_DoesNotClaimTheFontClauses(t *testing.T) {
	t.Parallel()

	for _, clause := range []string{
		"7.20",     // reference XObjects
		"7.21.3.1", // composite fonts
		"7.21.3.2", // CIDFont
		"7.21.3.3", // CMaps
		"7.21.4.1", // font embedding
		"7.21.4.2", // FontDescriptor
		"7.21.5",   // glyph coverage
		"7.21.6",   // TrueType encoding
		"7.21.7",   // ToUnicode
		"7.21.8",   // .notdef
	} {
		v := compliance.Violation{Rule: compliance.Rule{
			Specification: "ISO_14289_1", Clause: clause, TestNumber: 1,
		}}
		if matchCatalogLang(v) {
			t.Errorf("/Lang fixer must not claim ISO 14289-1 clause %s", clause)
		}
		if matchMarkedContentLang(v) {
			t.Errorf("marked-content /Lang fixer must not claim ISO 14289-1 clause %s", clause)
		}
	}
}

// TestMatchCatalogLang_StillClaimsItsOwnClauses is the other half of the
// mirror. Narrowing a matcher must not drop what it legitimately repaired:
// ISO 14289-1 states its language requirements in clause 7.2 exactly.
func TestMatchCatalogLang_StillClaimsItsOwnClauses(t *testing.T) {
	t.Parallel()

	for _, rule := range []compliance.Rule{
		{Specification: "ISO_14289_1", Clause: "7.2", TestNumber: 2},  // Outline language
		{Specification: "ISO_14289_1", Clause: "7.2", TestNumber: 29}, // catalog /Lang
		{Specification: "ISO_14289_1", Clause: "7.2", TestNumber: 34}, // page-content language
		{Specification: "ISO_19005_1", Clause: "6.7.4", TestNumber: 1},
		{Specification: "ISO_19005_2", Clause: "6.8.4", TestNumber: 1},
		{Specification: "ISO_14289_2", Clause: "8.4.4", TestNumber: 1},
	} {
		if !matchCatalogLang(compliance.Violation{Rule: rule}) {
			t.Errorf("must still claim %s/%s t%d",
				rule.Specification, rule.Clause, rule.TestNumber)
		}
	}
}
