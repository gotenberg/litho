package remediation

import (
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// ocgViolation returns a violation carrying just the rule, which is all the
// three gated repairs read.
func ocgViolation(rule compliance.Rule) []compliance.Violation {
	return []compliance.Violation{{Rule: rule}}
}

// TestMatchCatalogCleanup_StillDeclinesPart4EmbeddedFileRules is the headline
// negative control for this change.
//
// The optional-content rules are spelled 6.9 in parts 2 and 3, but EVERY part-4
// 6.9 test is an embedded-file rule: t2 the /F and /UF keys, t3 embedded-file
// conformance, t4 AFRelationship. An arm written on the clause and test number
// without the specification would claim all three and report them repaired
// while they survive.
//
// # Corpus evidence
//
//   - All corpora: forty baseline-mode cells hang on t3 alone.
func TestMatchCatalogCleanup_StillDeclinesPart4EmbeddedFileRules(t *testing.T) {
	t.Parallel()

	for _, rule := range []compliance.Rule{
		compliance.Rule_19005_4_6_9_t02, // file spec /F and /UF keys
		compliance.Rule_19005_4_6_9_t03, // embedded files shall be compliant
		compliance.Rule_19005_4_6_9_t04, // AFRelationship
	} {
		if matchCatalogCleanup(compliance.Violation{Rule: rule}) {
			t.Errorf("must not claim %s/%s t%d: clause 6.9 names an embedded-file rule there",
				rule.Specification, rule.Clause, rule.TestNumber)
		}
	}
}

// TestFixCatalogCleanup_RemovesForbiddenAS covers the /AS ban.
func TestFixCatalogCleanup_RemovesForbiddenAS(t *testing.T) {
	doc, alloc := newTestDoc(t, minimalCatalogPDF(
		"/OCProperties << /D << /Name (Conf1) /AS [<< /Event /View /Category [/View] >>] >> /OCGs [3 0 R] >>"))

	descs, err := fixCatalogCleanup(context.Background(), alloc, doc, Options{},
		ocgViolation(compliance.Rule_19005_2_6_9_t04), "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixCatalogCleanup: %v", err)
	}
	if len(descs) == 0 {
		t.Fatal("a configuration carrying /AS must be repaired")
	}

	ocp, ok := pendingDict(t, alloc, 1)["OCProperties"].(ast.PdfDict)
	if !ok {
		t.Fatalf("/OCProperties: got %T, want an inline ast.PdfDict", pendingDict(t, alloc, 1)["OCProperties"])
	}
	d, ok := ocp["D"].(ast.PdfDict)
	if !ok {
		t.Fatalf("/D: got %T, want ast.PdfDict", ocp["D"])
	}
	if _, has := d["AS"]; has {
		t.Error("/AS should have been removed from the default configuration")
	}
	if got := d["Name"]; got != ast.PdfString("Conf1") {
		t.Errorf("/Name = %v, want the untouched (Conf1)", got)
	}
}

// TestFixCatalogCleanup_KeepsASWithoutTheViolation is the guarantee that makes
// the repair safe to run unconditionally on every profile.
//
// ISO 19005-4 6.10 states no AS test, so a PDF/A-4 file may legitimately carry
// an /AS. [fixCatalogCleanup] repairs whatever it finds once it runs, so
// without this gate a part-4 run that fired for some unrelated catalog reason
// would strip a key that profile permits.
func TestFixCatalogCleanup_KeepsASWithoutTheViolation(t *testing.T) {
	doc, alloc := newTestDoc(t, minimalCatalogPDF(
		"/NeedsRendering true /OCProperties << /D << /Name (Conf1) /AS [<< /Event /View >>] >> /OCGs [3 0 R] >>"))

	// A violation the fixer does handle, so it certainly runs, but not the /AS
	// one. The /NeedsRendering removal proves it ran.
	descs, err := fixCatalogCleanup(context.Background(), alloc, doc, Options{},
		ocgViolation(compliance.Rule_19005_4_6_4_2_t02), "PDF/A-4")
	if err != nil {
		t.Fatalf("fixCatalogCleanup: %v", err)
	}
	var ranAtAll bool
	for _, d := range descs {
		if strings.Contains(d, "NeedsRendering") {
			ranAtAll = true
		}
		if strings.Contains(d, "/AS") {
			t.Errorf("must not touch /AS without the violation: %q", d)
		}
	}
	if !ranAtAll {
		t.Fatal("the fixer did not run, so this proves nothing about /AS")
	}

	catalog := pendingDict(t, alloc, 1)
	ocp, ok := catalog["OCProperties"].(ast.PdfDict)
	if !ok {
		t.Fatalf("/OCProperties: got %T, want ast.PdfDict", catalog["OCProperties"])
	}
	d, ok := ocp["D"].(ast.PdfDict)
	if !ok {
		t.Fatalf("/D: got %T, want ast.PdfDict", ocp["D"])
	}
	if _, has := d["AS"]; !has {
		t.Error("/AS is permitted in PDF/A-4 and must survive when no rule reported it")
	}
}

// TestFixCatalogCleanup_MakesDuplicateNamesUnique covers test 2.
//
// When /D and a /Configs entry share a name, the checker collects /D first, so
// /D keeps the name and the later configuration is renamed.
//
// # Corpus evidence
//
//   - verapdf 6-10-t01-fail-d: carries /D and /Configs[0] both named
//     (OCConfigName0).
func TestFixCatalogCleanup_MakesDuplicateNamesUnique(t *testing.T) {
	doc, alloc := newTestDoc(t, minimalCatalogPDF(
		"/OCProperties << /Configs [<< /Name (Conf1) /ON [3 0 R] >>] "+
			"/D << /Name (Conf1) /Order [3 0 R] >> /OCGs [3 0 R] >>"))

	descs, err := fixCatalogCleanup(context.Background(), alloc, doc, Options{},
		ocgViolation(compliance.Rule_19005_2_6_9_t02), "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixCatalogCleanup: %v", err)
	}
	if len(descs) == 0 {
		t.Fatal("a duplicate configuration name must be repaired")
	}

	ocp := pendingDict(t, alloc, 1)["OCProperties"].(ast.PdfDict)
	d, ok := ocp["D"].(ast.PdfDict)
	if !ok {
		t.Fatalf("/D: got %T, want ast.PdfDict", ocp["D"])
	}
	if got := d["Name"]; got != ast.PdfString("Conf1") {
		t.Errorf("the first configuration keeps its name: /D /Name = %v, want (Conf1)", got)
	}

	configs, ok := ocp["Configs"].(ast.PdfArray)
	if !ok || len(configs) != 1 {
		t.Fatalf("/Configs: got %v, want one entry", ocp["Configs"])
	}
	cfg, ok := configs[0].(ast.PdfDict)
	if !ok {
		t.Fatalf("/Configs[0]: got %T, want ast.PdfDict", configs[0])
	}
	name, ok := cfg["Name"].(ast.PdfString)
	if !ok {
		t.Fatalf("/Configs[0] /Name: got %T, want ast.PdfString", cfg["Name"])
	}
	if string(name) == "Conf1" {
		t.Error("the duplicate was not renamed")
	}
	if !strings.HasPrefix(string(name), "Conf1") {
		t.Errorf("the new name must derive from the original label, got %q", name)
	}
}

// TestDisambiguateOCGConfigName keeps the derived label predictable.
func TestDisambiguateOCGConfigName(t *testing.T) {
	t.Parallel()

	seen := map[string]bool{"Conf1": true}
	if got := disambiguateOCGConfigName("Conf1", seen); got != "Conf1 (2)" {
		t.Errorf("got %q, want %q", got, "Conf1 (2)")
	}

	seen["Conf1 (2)"] = true
	if got := disambiguateOCGConfigName("Conf1", seen); got != "Conf1 (3)" {
		t.Errorf("a taken candidate must be skipped: got %q, want %q", got, "Conf1 (3)")
	}
}
