package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestPageWithoutResourcesFailsUndefinedResources pins ISO 19005-4, 6.2.2 t3
// on a page whose content has no resource dictionary at all: the page states
// no /Resources and no ancestor in the page tree lends one, which ISO 32000-2,
// 7.7.3.3, Table 31 forbids (a page needing no resources states an empty
// dictionary). Every count was measured against veraPDF 1.30.2 at PDF/A-4,
// which fails the test once for such a content stream whatever it paints, and
// reports DeviceGray, the initial colour space, as the undefined name.
func TestPageWithoutResourcesFailsUndefinedResources(t *testing.T) {
	t.Parallel()
	const catalog = "<< /Type /Catalog /Pages 2 0 R >>"
	onePage := func(pageExtra, pagesExtra string, rest ...string) []string {
		return append([]string{
			catalog,
			"<< /Type /Pages /Kids [3 0 R] /Count 1" + pagesExtra + " >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792]" + pageExtra + " >>",
		}, rest...)
	}
	qQ := exponentStream("", "q Q")
	for _, tc := range []struct {
		name    string
		objs    []string
		profile models.PDFAProfile
		want    int
	}{
		{"content painting nothing", onePage(" /Contents 4 0 R", "", qQ), models.PDFA4, 1},
		{"content painting in DeviceGray", onePage(" /Contents 4 0 R", "", exponentStream("", "0 g 0 0 10 10 re f")), models.PDFA4, 1},
		{"an empty content stream", onePage(" /Contents 4 0 R", "", exponentStream("", "")), models.PDFA4, 1},
		{"a content stream holding a comment", onePage(" /Contents 4 0 R", "", exponentStream("", "% nothing")), models.PDFA4, 1},
		{"a content stream that does not decode", onePage(" /Contents 4 0 R", "", exponentStream("/Filter /FlateDecode", "garbage")), models.PDFA4, 1},
		{"an empty /Contents array", onePage(" /Contents []", ""), models.PDFA4, 1},
		{"a /Contents array holding null", onePage(" /Contents [null]", ""), models.PDFA4, 1},
		{"content naming a font as well", onePage(" /Contents 4 0 R", "", exponentStream("", "BT /F1 12 Tf ET")), models.PDFA4, 1},
		{"a page stating /Resources null", onePage(" /Contents 4 0 R /Resources null", "", qQ), models.PDFA4, 1},
		{"a page tree stating /Resources null", onePage(" /Contents 4 0 R", " /Resources null", qQ), models.PDFA4, 1},
		{"a page with an empty dictionary", onePage(" /Contents 4 0 R /Resources << >>", "", qQ), models.PDFA4, 0},
		{"a page inheriting an empty dictionary", onePage(" /Contents 4 0 R", " /Resources << >>", qQ), models.PDFA4, 0},
		{"a page stating a number", onePage(" /Contents 4 0 R /Resources 5", "", qQ), models.PDFA4, 0},
		{"a page naming an undefined object", onePage(" /Contents 4 0 R /Resources 40 0 R", "", qQ), models.PDFA4, 0},
		{"a page naming a null object", onePage(" /Contents 4 0 R /Resources 5 0 R", "", qQ, "null"), models.PDFA4, 0},
		{"a page with no /Contents", onePage("", ""), models.PDFA4, 0},
		{"a /Contents that is null", onePage(" /Contents null", ""), models.PDFA4, 0},
		{"a /Contents that is a dictionary", onePage(" /Contents << /Length 0 >>", ""), models.PDFA4, 0},
		{"two pages sharing one content stream", []string{
			catalog,
			"<< /Type /Pages /Kids [3 0 R 5 0 R] /Count 2 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R >>",
			qQ,
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R >>",
		}, models.PDFA4, 1},
		{"two pages with a content stream each", []string{
			catalog,
			"<< /Type /Pages /Kids [3 0 R 5 0 R] /Count 2 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R >>",
			qQ,
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 6 0 R >>",
			qQ,
		}, models.PDFA4, 2},
		{"PDF/A-2b, which has no test 3", onePage(" /Contents 4 0 R", "", qQ), models.PDFA2b, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t, tc.objs...)
			viols, err := ValidatePDFA(context.Background(), doc, tc.profile)
			if err != nil {
				t.Fatalf("ValidatePDFA: %v", err)
			}
			got := 0
			for _, e := range viols {
				if v, ok := e.(Violation); ok && v.Rule.Clause == "6.2.2" && v.Rule.TestNumber == 3 {
					got++
				}
			}
			if got != tc.want {
				t.Errorf("6.2.2 t3 fired %d times, want %d: %v", got, tc.want, viols)
			}
		})
	}
}
