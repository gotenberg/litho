package remediation

import (
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// incompleteOCGOrderPDF builds a document with three OCGs, objects 4 to 6, the
// last without a /Name, whose every optional content configuration has an
// /Order leaving some out: the default /D, a direct /Configs entry with an
// empty /Order, and an indirect one, object 7, nesting a labelled grouping.
func incompleteOCGOrderPDF() []byte {
	return assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /OCProperties << /OCGs [4 0 R 5 0 R 6 0 R]" +
			" /D << /Name (Default) /Order [4 0 R] >>" +
			" /Configs [<< /Name (Alt) /Order [] >> 7 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /OCG /Name (Layer 1) >>",
		"<< /Type /OCG /Name (Layer 2) >>",
		"<< /Type /OCG >>",
		"<< /Name (Grouped) /Order [[(Group) 5 0 R]] >>",
	})
}

// ocgOrderFindings counts the /Order completeness violations doc carries at
// profile: ISO 19005-2/3 §6.9 t3 and ISO 19005-4 §6.10 t3.
func ocgOrderFindings(t *testing.T, doc *document.Document, profile models.PDFAProfile) int {
	t.Helper()

	errs, err := compliance.ValidatePDFA(context.Background(), doc, profile)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}
	n := 0
	for _, v := range toViolations(errs) {
		if ocgOrderCompleteViolation(v.Rule) {
			n++
		}
	}
	return n
}

// TestRemediatePDFA_CompletesOCGOrderInEveryConfiguration pins that the
// /Order repair reaches every optional content configuration, not only /D.
//
// ISO 19005-2 §6.9 t3 and ISO 19005-4 §6.10 t3 apply to any configuration
// dictionary carrying /Order. An earlier repair completed /D alone and was
// removed as a false fix: a /Configs entry kept its own incomplete /Order, so
// veraPDF kept reporting while litho, which read /D alone, went quiet.
//
// # Corpus evidence
//
//   - litho complex_ua2.pdf: /Configs [ << /Order [ ] >> ] beside an
//     incomplete /D.
//   - litho complex_2b.pdf: its OCG has no /Name, and veraPDF matches /Order
//     to the OCGs by /Name, so a completed /Order still failed there.
func TestRemediatePDFA_CompletesOCGOrderInEveryConfiguration(t *testing.T) {
	t.Parallel()

	for _, profile := range []models.PDFAProfile{models.PDFA2b, models.PDFA3b, models.PDFA4} {
		src, _ := newTestDoc(t, incompleteOCGOrderPDF())
		if got := ocgOrderFindings(t, src, profile); got != 3 {
			t.Fatalf("%s precondition: %d /Order findings, want 3, one per configuration", profile, got)
		}
		for _, preserve := range []bool{true, false} {
			out := remediateAndReopen(t, incompleteOCGOrderPDF(), profile, Options{PreserveContent: preserve})
			if got := ocgOrderFindings(t, out, profile); got != 0 {
				t.Errorf("%s preserve=%v: %d /Order findings survive, want 0", profile, preserve, got)
			}
			assertOCGNames(t, out, profile)
		}
	}
}

// assertOCGNames checks that the named OCGs kept their names and that the
// nameless one gained a text string, which is what veraPDF matches /Order on.
func assertOCGNames(t *testing.T, doc *document.Document, profile models.PDFAProfile) {
	t.Helper()

	ctx := context.Background()
	cat, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	ocp, err := document.ResolveDict(ctx, doc, cat.Dict["OCProperties"])
	if err != nil {
		t.Fatalf("/OCProperties: %v", err)
	}
	ocgs := compliance.DocumentOCGs(ctx, doc, ocp)
	if len(ocgs) != 3 {
		t.Fatalf("%s: %d OCGs, want 3", profile, len(ocgs))
	}
	var names []string
	for _, ref := range ocgs {
		d, err := document.ResolveDict(ctx, doc, ref)
		if err != nil {
			t.Fatalf("OCG %d: %v", ref.ObjectNum, err)
		}
		s, err := document.ResolveString(ctx, doc, d["Name"])
		if err != nil {
			t.Errorf("%s: OCG %d has no string /Name", profile, ref.ObjectNum)
			continue
		}
		names = append(names, string(s))
	}
	if len(names) == 3 && (names[0] != "Layer 1" || names[1] != "Layer 2" || names[2] == "") {
		t.Errorf("%s: OCG names = %q, want the two originals kept and the third named", profile, names)
	}
}

// TestFixCatalogCleanup_KeepsOrderWithoutTheViolation pins that /Order is
// rewritten only when the run reported it incomplete, so a fixer that runs
// for another catalog rule leaves the configurations as they are.
func TestFixCatalogCleanup_KeepsOrderWithoutTheViolation(t *testing.T) {
	t.Parallel()

	doc, alloc := newTestDoc(t, incompleteOCGOrderPDF())
	descs, err := fixCatalogCleanup(context.Background(), alloc, doc, Options{},
		ocgViolation(compliance.Rule_19005_2_6_9_t04), "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixCatalogCleanup: %v", err)
	}
	for _, d := range descs {
		if strings.Contains(d, "Order") || strings.Contains(d, "OCG object") {
			t.Errorf("must not touch /Order or an OCG without the violation: %q", d)
		}
	}
	if _, ok := alloc.Pending()[6]; ok {
		t.Error("the nameless OCG was rewritten without the /Order violation")
	}
}

// TestFixCatalogCleanup_LeavesAnUnreadableOrderAlone pins that the repair adds
// only what the check reports missing. An /Order nested past the read bound is
// one the check abstains on, so the repair must not append to it either.
func TestFixCatalogCleanup_LeavesAnUnreadableOrderAlone(t *testing.T) {
	t.Parallel()

	deep := strings.Repeat("[", 3*25) + "4 0 R" + strings.Repeat("]", 3*25)
	doc, alloc := newTestDoc(t, assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /OCProperties << /OCGs [4 0 R 5 0 R] /D << /Name (D) /Order " + deep + " >> >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /OCG /Name (Layer 1) >>",
		"<< /Type /OCG /Name (Layer 2) >>",
	}))
	descs, err := fixCatalogCleanup(context.Background(), alloc, doc, Options{},
		ocgViolation(compliance.Rule_19005_2_6_9_t03), "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixCatalogCleanup: %v", err)
	}
	for _, d := range descs {
		if strings.Contains(d, "/Order") {
			t.Errorf("appended to an /Order the check cannot read whole: %q", d)
		}
	}
}

// TestMatchCatalogCleanup_ClaimsOCGOrder pins the claim on every spelling of
// the /Order completeness rule, now that [fixOCGConfigs] completes /Order in
// every configuration.
func TestMatchCatalogCleanup_ClaimsOCGOrder(t *testing.T) {
	t.Parallel()

	for _, rule := range []compliance.Rule{
		compliance.Rule_19005_2_6_9_t03,
		compliance.Rule_19005_3_6_9_t03,
		compliance.Rule_19005_4_6_10_t03,
	} {
		if !matchCatalogCleanup(compliance.Violation{Rule: rule}) {
			t.Errorf("must claim %s/%s t%d", rule.Specification, rule.Clause, rule.TestNumber)
		}
	}
	// The part-4 6.9 t3 spelling is an embedded-file rule and stays unclaimed.
	if matchCatalogCleanup(compliance.Violation{Rule: compliance.Rule_19005_4_6_9_t03}) {
		t.Error("must not claim ISO_19005_4/6.9 t3: it is an embedded-file rule")
	}
}
