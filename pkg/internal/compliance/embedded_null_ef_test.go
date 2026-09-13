package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestPDFANullEFEmbedsNothing pins that a file specification whose /EF is
// written as null embeds nothing for the PDF/A embedded-file rules, as veraPDF
// reads it (GFCosFileSpecification containsEF, a knownKey on /EF): an entry
// whose value is null is the same as an absent one (ISO 32000-1, 7.3.7). A null
// /EF used to fire ISO 19005-1, 6.1.11 t1, ISO 19005-2 and -3, 6.8 t2, and
// ISO 19005-4, 6.9 t2, which veraPDF does not; an /EF naming a file still
// fires them. Every case was measured against veraPDF.
func TestPDFANullEFEmbedsNothing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	profiles := []struct {
		profile models.PDFAProfile
		rule    Rule
	}{
		{models.PDFA1b, Rule_19005_1_6_1_11_t01},
		{models.PDFA2b, Rule_19005_2_6_8_t02},
		{models.PDFA3b, Rule_19005_3_6_8_t02},
		{models.PDFA4, Rule_19005_4_6_9_t02},
		{models.PDFA4f, Rule_19005_4_6_9_t02},
	}
	specs := []struct {
		name string
		ef   string
		want int
	}{
		{"an /EF written as null", " /EF null", 0},
		{"an /EF naming an embedded file", " /EF << /F 5 0 R >>", 1},
		{"no /EF", "", 0},
	}
	for _, p := range profiles {
		for _, sp := range specs {
			t.Run(string(p.profile)+": "+sp.name, func(t *testing.T) {
				t.Parallel()
				doc, _ := objsDocPDF(t,
					"<< /Type /Catalog /Pages 2 0 R /Names << /EmbeddedFiles << /Names [(a.txt) 4 0 R] >> >> >>",
					"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
					"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
					"<< /Type /Filespec /F (a.txt) /AFRelationship /Data /Desc (d)"+sp.ef+" >>",
					streamObj("hello"),
				)
				viols, err := ValidatePDFA(ctx, doc, p.profile)
				if err != nil {
					t.Fatalf("ValidatePDFA: %v", err)
				}
				if got := countRuleErrs(t, viols, p.rule); got != sp.want {
					t.Errorf("%s t%d fired %d times, want %d", p.rule.Clause, p.rule.TestNumber, got, sp.want)
				}
			})
		}
	}
}
