package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// embeddedSpecDoc builds a document whose EmbeddedFiles name tree holds one
// file specification (object 4) with the given entries; object 5 is a stream.
func embeddedSpecDoc(t *testing.T, entries string) []string {
	t.Helper()
	return []string{
		"<< /Type /Catalog /Pages 2 0 R /Names << /EmbeddedFiles << /Names [(a.txt) 4 0 R] >> >>" +
			" /MarkInfo << /Marked true >> /Lang (en) >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
		"<< /Type /Filespec /F (a.txt) /AFRelationship /Data" + entries + " >>",
		streamObj("hello"),
	}
}

// TestEmbeddedFileSpecNullEF pins which file specifications each profile
// judges, as veraPDF picks them. ISO 14289-1, 7.11 t1 judges one that embeds a
// file (GFCosFileSpecification containsEF, a knownKey on /EF), so an /EF written
// as null embeds nothing (ISO 32000-1, 7.3.7) and its /UF is not required.
// ISO 14289-2, 8.14.1 t1 judges every specification in the EmbeddedFiles name
// tree, whatever /EF holds (containsDesc || presentInEmbeddedFiles == false).
// The verdicts were measured against veraPDF, which reports an embedded
// specification twice because it reaches it through two paths; litho reports
// it once.
func TestEmbeddedFileSpecNullEF(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name    string
		ua2     bool
		entries string
		rule    Rule
		want    int
	}{
		{"UA-1: an /EF written as null", false, " /Desc (d) /EF null", Rule_14289_1_7_11_t01, 0},
		{"UA-1: an /EF naming an embedded file", false, " /Desc (d) /EF << /F 5 0 R >>", Rule_14289_1_7_11_t01, 1},
		{"UA-1: no /EF", false, " /Desc (d)", Rule_14289_1_7_11_t01, 0},
		{"UA-2: no /EF and no /Desc", true, " /UF (a.txt)", Rule_14289_2_8_14_1_t01, 1},
		{"UA-2: an /EF written as null and no /Desc", true, " /UF (a.txt) /EF null", Rule_14289_2_8_14_1_t01, 1},
		{"UA-2: an /EF naming an embedded file and no /Desc", true, " /UF (a.txt) /EF << /F 5 0 R >>", Rule_14289_2_8_14_1_t01, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t, embeddedSpecDoc(t, tt.entries)...)
			profile := models.PDFUA1
			if tt.ua2 {
				profile = models.PDFUA2
			}
			viols, err := ValidatePDFUA(ctx, doc, profile)
			if err != nil {
				t.Fatalf("ValidatePDFUA: %v", err)
			}
			if got := countRuleErrs(t, viols, tt.rule); got != tt.want {
				t.Errorf("%s t%d fired %d times, want %d", tt.rule.Clause, tt.rule.TestNumber, got, tt.want)
			}
		})
	}
}
