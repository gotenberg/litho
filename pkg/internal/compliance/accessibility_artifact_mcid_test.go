package compliance

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestArtifactSequenceWithOwnedMCID pins ISO 14289-1, 7.1 t1 and t2 on an
// /Artifact marked-content sequence that carries an /MCID: when the ParentTree
// maps the MCID to a structure element reaching the root, the content is both
// marked as an Artifact and tagged, so it is an Artifact inside tagged content
// and tagged content inside an Artifact. With the MCID left out of the
// ParentTree it is not tagged and neither test fires. Every case was measured
// against veraPDF.
//
// # Corpus evidence
//
//   - govdocs thread_000_000632.pdf: carries an /Artifact sequence with an
//     /MCID the ParentTree maps, so its content is both an Artifact inside
//     tagged content and tagged content inside an Artifact.
func TestArtifactSequenceWithOwnedMCID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	const body = "/Artifact <</MCID 0>> BDC 0 0 10 10 re f EMC"
	for _, tt := range []struct {
		name       string
		owner      string
		parentTree string
		want       int
	}{
		{"an MCID an Artifact element owns", "Artifact", "/Nums [0 [6 0 R]]", 1},
		{"an MCID a P element owns", "P", "/Nums [0 [6 0 R]]", 1},
		{"an MCID the ParentTree does not map", "P", "/Nums []", 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t,
				"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 5 0 R /MarkInfo << /Marked true >> /Lang (en)"+
					" /ViewerPreferences << /DisplayDocTitle true >> >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R /StructParents 0 /Resources << >> >>",
				fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(body), body),
				"<< /Type /StructTreeRoot /K [7 0 R] /ParentTree << "+tt.parentTree+" >> >>",
				"<< /Type /StructElem /S /"+tt.owner+" /P 7 0 R /Pg 3 0 R /K 0 >>",
				"<< /Type /StructElem /S /Document /P 5 0 R /K [6 0 R] >>",
			)
			viols, err := ValidatePDFUA(ctx, doc, models.PDFUA1)
			if err != nil {
				t.Fatalf("ValidatePDFUA: %v", err)
			}
			for _, rule := range []Rule{Rule_14289_1_7_1_t01, Rule_14289_1_7_1_t02} {
				if got := countRuleErrs(t, viols, rule); got != tt.want {
					t.Errorf("%s t%d fired %d times, want %d", rule.Clause, rule.TestNumber, got, tt.want)
				}
			}
		})
	}
}
