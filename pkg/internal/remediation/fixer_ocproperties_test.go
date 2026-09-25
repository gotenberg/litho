package remediation

import (
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// ocPropertiesViolation is the part-1 ban litho emits from
// compliance.checkForbiddenOCProperties.
func ocPropertiesViolation() compliance.Violation {
	return compliance.Violation{
		Rule: compliance.Rule{
			Specification: "ISO_19005_1",
			Clause:        "6.1.13",
			TestNumber:    1,
			Object:        "CosDocument",
			Description:   "The document catalog dictionary shall not contain a key with the name OCProperties",
		},
		Context: "catalog contains /OCProperties (optional content forbidden in PDF/A-1)",
	}
}

// TestFixCatalog_RemovesOCPropertiesWhenPartOneFlagsIt covers the repair.
//
// ISO 19005-1 6.1.13 test 1 is `isOptionalContentPresent == false`, and litho's
// detector reads the catalog key's presence alone, so deleting the key is the
// whole repair.
func TestFixCatalog_RemovesOCPropertiesWhenPartOneFlagsIt(t *testing.T) {
	doc, alloc := newTestDoc(t, ocPropertiesPDF(""))

	fixes, err := fixCatalogCleanup(context.Background(), alloc, doc, Options{},
		[]compliance.Violation{ocPropertiesViolation()}, "PDF/A-1b")
	if err != nil {
		t.Fatalf("fixCatalogCleanup: %v", err)
	}

	cat := pendingDict(t, alloc, 1)
	if cat == nil {
		t.Fatal("the catalog was never written")
	}
	if _, still := cat["OCProperties"]; still {
		t.Error("/OCProperties survived in the catalog")
	}
	if _, ok := cat["Pages"]; !ok {
		t.Error("the rewrite dropped /Pages: sibling keys must survive")
	}
	if !strings.Contains(strings.Join(fixes, "; "), "OCProperties") {
		t.Errorf("no fix was reported, got %v", fixes)
	}
}

// TestFixCatalog_RemovesOCPropertiesEvenWhenContentUsesIt pins the decision.
//
// Real carriers attach /OC entries to content, so deletion makes that content
// unconditionally visible. PDF/A-1 forbids optional content outright, exactly
// as it forbids transparency, and [fixTransparency] ignores PreserveContent for
// the same reason: refusing would mean declining to produce a conforming file.
//
// # Corpus evidence
//
//   - All corpora: 8 of the 19 carriers carry /OC entries.
func TestFixCatalog_RemovesOCPropertiesEvenWhenContentUsesIt(t *testing.T) {
	for _, preserve := range []bool{false, true} {
		doc, alloc := newTestDoc(t, ocPropertiesPDF("/OC 30 0 R"))

		_, err := fixCatalogCleanup(context.Background(), alloc, doc,
			Options{PreserveContent: preserve},
			[]compliance.Violation{ocPropertiesViolation()}, "PDF/A-1b")
		if err != nil {
			t.Fatalf("PreserveContent=%v: %v", preserve, err)
		}
		cat := pendingDict(t, alloc, 1)
		if cat == nil {
			t.Fatalf("PreserveContent=%v: the catalog was never written", preserve)
		}
		if _, still := cat["OCProperties"]; still {
			t.Errorf("PreserveContent=%v: /OCProperties survived", preserve)
		}
	}
}

// TestFixCatalog_SecondRepairDoesNotRestoreOCProperties pins the stale-dict
// trap, which mutation testing found uncovered.
//
// fixCatalogCleanup performs several catalog repairs in sequence, each copying
// the local catalogDict. Deleting /OCProperties without reassigning
// catalogDict leaves that local copy holding the key, so the NEXT repair
// copies from it and writes the key straight back. The output would then carry
// /OCProperties while the fixer reported removing it.
//
// /NeedsRendering is the companion here because it is also a bare catalog key
// removed by this same function, and it is removed BEFORE the /OCProperties
// branch, so the sequence is genuinely exercised in both directions.
func TestFixCatalog_SecondRepairDoesNotRestoreOCProperties(t *testing.T) {
	doc, alloc := newTestDoc(t, ocPropertiesAndNeedsRenderingPDF())

	needsRendering := compliance.Violation{
		Rule: compliance.Rule{
			Specification: "ISO_19005_2",
			Clause:        "6.4.2",
			TestNumber:    1,
			Object:        "CosDocument",
			Description:   "The document catalog dictionary shall not include a NeedsRendering key",
		},
		Context: "catalog contains /NeedsRendering",
	}

	if _, err := fixCatalogCleanup(context.Background(), alloc, doc, Options{},
		[]compliance.Violation{ocPropertiesViolation(), needsRendering}, "PDF/A-1b"); err != nil {
		t.Fatalf("fixCatalogCleanup: %v", err)
	}

	cat := pendingDict(t, alloc, 1)
	if cat == nil {
		t.Fatal("the catalog was never written")
	}
	if _, still := cat["OCProperties"]; still {
		t.Error("/OCProperties came back: a later repair copied from a stale catalog dict")
	}
	if _, still := cat["NeedsRendering"]; still {
		t.Error("/NeedsRendering survived")
	}
	if _, ok := cat["Pages"]; !ok {
		t.Error("the rewrite dropped /Pages: sibling keys must survive")
	}
}

// TestFixCatalog_KeepsOCPropertiesForOtherParts is the scope control.
//
// Parts 2, 3 and 4 permit optional content and never state this rule, so the
// same file keeps its /OCProperties when validated under them. The branch is
// gated on the violation rather than the profile string precisely so this
// holds without the fixer knowing which part it is repairing.
func TestFixCatalog_KeepsOCPropertiesForOtherParts(t *testing.T) {
	// A part-2 implementation-limits violation at the SAME clause number.
	partTwoLimits := compliance.Violation{
		Rule: compliance.Rule{
			Specification: "ISO_19005_2",
			Clause:        "6.1.13",
			TestNumber:    1,
			Object:        "CosInteger",
			Description:   "A conforming file shall not contain any integer greater than 2147483647",
		},
		Context: "object 4: integer 99999999999 out of range",
	}

	for _, tc := range []struct {
		name string
		vs   []compliance.Violation
	}{
		{"no violations at all", nil},
		{"part-2 limits at the same clause", []compliance.Violation{partTwoLimits}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, alloc := newTestDoc(t, ocPropertiesPDF(""))

			if _, err := fixCatalogCleanup(context.Background(), alloc, doc, Options{},
				tc.vs, "PDF/A-2b"); err != nil {
				t.Fatalf("fixCatalogCleanup: %v", err)
			}
			if cat, ok := alloc.Pending()[1].(ast.PdfDict); ok {
				if _, gone := cat["OCProperties"]; !gone {
					t.Error("/OCProperties was removed for a part that permits it")
				}
			}
		})
	}
}

// TestMatchRealLimits_DeclinesThePartOneOCPropertiesRule pins the narrowing.
//
// classify() collects matching violations for EVERY fixer, and each claimed
// index is marked handled, so an unconditional clause-prefix match reported
// this rule handled while real-limits cannot touch it.
//
// # Corpus evidence
//
//   - All corpora: narrowing the matcher with contextContains("out of range")
//     silently stopped claiming 14 cells of 6.1.12 test 2.
func TestMatchRealLimits_DeclinesThePartOneOCPropertiesRule(t *testing.T) {
	if matchRealLimits(ocPropertiesViolation()) {
		t.Error("real-limits claims the part-1 /OCProperties rule it cannot repair")
	}

	// Every numeric limits violation it DOES repair must still match, whatever
	// its context says. These Context strings are the real ones from
	// compliance/file_limits.go.
	//
	// REGRESSION GUARD: narrowing this matcher with
	// contextContains("out of range") silently stopped claiming 6.1.12
	// test 2, whose violations read "real %g exceeds +/-%g". The matcher
	// names the rules instead.
	for _, tc := range []struct {
		name string
		v    compliance.Violation
		want bool
	}{
		{"part-1 real exceeds", compliance.Violation{
			Rule:    compliance.Rule_19005_1_6_1_12_t02,
			Context: "object 4: real 1e+40 exceeds +/-32767",
		}, true},
		{"part-1 integer out of range", compliance.Violation{
			Rule:    compliance.Rule_19005_1_6_1_12_t01,
			Context: "object 4: integer 99999999999 out of range",
		}, true},
		{"part-2 integer out of range", compliance.Violation{
			Rule:    compliance.Rule_19005_2_6_1_13_t01,
			Context: "object 4: integer 99999999999 out of range",
		}, true},
		{"part-2 real closer to zero", compliance.Violation{
			Rule:    compliance.Rule_19005_2_6_1_13_t05,
			Context: "object 4: real 1e-40 is closer to zero than +/-1.175e-38",
		}, true},
		{"part-3 real exceeds", compliance.Violation{
			Rule:    compliance.Rule_19005_3_6_1_13_t02,
			Context: "trailer: real 1e+40 exceeds +/-3.403e+38",
		}, true},
		// The numbers of a content stream are the content-limits repair's.
		{"part-1 real in a content stream", compliance.Violation{
			Rule:    compliance.Rule_19005_1_6_1_12_t02,
			Context: "object 4 (stream content): real 40000 exceeds +/-32767",
		}, false},
		// The other tests of the clauses are other repairs'.
		{"part-1 array capacity", compliance.Violation{
			Rule:    compliance.Rule_19005_1_6_1_12_t05,
			Context: "object 4: array capacity 9000 exceeds 8191",
		}, false},
		{"part-2 string length", compliance.Violation{
			Rule:    compliance.Rule_19005_2_6_1_13_t03,
			Context: "object 4: string length 40000 exceeds 32767",
		}, false},
		{"part-2 name length", compliance.Violation{
			Rule:    compliance.Rule_19005_2_6_1_13_t04,
			Context: "object 4: name length 200 exceeds 127",
		}, false},
		{"part-2 permissions", compliance.Violation{Rule: compliance.Rule_19005_2_6_1_12_t01}, false},
		{"part-4 catalog version", compliance.Violation{Rule: compliance.Rule_19005_4_6_1_12_t01}, false},
	} {
		if got := matchRealLimits(tc.v); got != tc.want {
			t.Errorf("matchRealLimits(%s) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestMatchCatalogCleanup_ClaimsOnlyPartOneAtThisClause pins the matcher arm.
func TestMatchCatalogCleanup_ClaimsOnlyPartOneAtThisClause(t *testing.T) {
	if !matchCatalogCleanup(ocPropertiesViolation()) {
		t.Error("catalog-cleanup does not claim the part-1 /OCProperties rule")
	}
	for _, spec := range []string{"ISO_19005_2", "ISO_19005_3", "ISO_19005_4"} {
		v := compliance.Violation{
			Rule:    compliance.Rule{Specification: spec, Clause: "6.1.13", TestNumber: 1},
			Context: "object 4: integer 99999999999 out of range",
		}
		if matchCatalogCleanup(v) {
			t.Errorf("%s 6.1.13 t1 is an implementation-limits rule and must not be claimed", spec)
		}
	}
}

// ocPropertiesAndNeedsRenderingPDF carries two catalog-level defects this
// fixer removes in sequence.
func ocPropertiesAndNeedsRenderingPDF() []byte {
	return []byte(
		"%PDF-1.4\n" +
			"1 0 obj\n<< /Type /Catalog /Pages 2 0 R /NeedsRendering true " +
			"/OCProperties << /OCGs [30 0 R] /D << /Order [] /ON [30 0 R] >> >> >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>\nendobj\n" +
			"30 0 obj\n<< /Type /OCG /Name (layer) >>\nendobj\n" +
			"trailer\n<< /Size 31 /Root 1 0 R >>\n%%EOF\n")
}

// ocPropertiesPDF builds a one-page PDF whose catalog (obj 1) declares
// /OCProperties, optionally with an /OC reference on the page's form XObject.
func ocPropertiesPDF(pageOC string) []byte {
	return []byte(
		"%PDF-1.4\n" +
			"1 0 obj\n<< /Type /Catalog /Pages 2 0 R " +
			"/OCProperties << /OCGs [30 0 R] /D << /Order [] /ON [30 0 R] >> >> >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
			"/Resources << /XObject << /Fm0 20 0 R >> >> >>\nendobj\n" +
			"20 0 obj\n<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] " + pageOC +
			" /Length 0 >>\nstream\n\nendstream\nendobj\n" +
			"30 0 obj\n<< /Type /OCG /Name (layer) >>\nendobj\n" +
			"trailer\n<< /Size 31 /Root 1 0 R >>\n%%EOF\n")
}
