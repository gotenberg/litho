package compliance

import (
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestInlineImagesJudgedAsVeraPDFReadsThem pins that every rule sees the
// inline images of a content stream as veraPDF reads them (see
// [content.ParseStreamData]): an image veraPDF reads as part of the check of
// the EI before it answers to no rule, and an EI or an ID read where an
// operator stands is an undefined operator. Every case was measured with
// veraPDF 1.30 on a one-page file of this content at PDF/A-1b and PDF/A-2b.
func TestInlineImagesJudgedAsVeraPDFReadsThem(t *testing.T) {
	t.Parallel()

	const g = "BI /W 1 /H 1 /BPC 8 /CS /G "
	type count struct {
		clause string
		test   int
		want   int
	}
	for _, tc := range []struct {
		name    string
		content string
		// oneB and twoB are the rules counted at PDF/A-1b and PDF/A-2b.
		oneB, twoB []count
	}{
		{
			"a DeviceRGB image after a DeviceGray one", g + "ID xyz\nEI\nBI /W 1 /H 1 /BPC 8 /CS /RGB ID xyz\nEI\n",
			[]count{{"6.2.3.3", 1, 0}, {"6.2.3.3", 3, 1}},
			[]count{{"6.2.4.3", 2, 0}, {"6.2.4.3", 4, 1}},
		},
		{
			"the same with an operator between", g + "ID xyz\nEI\nq Q\nBI /W 1 /H 1 /BPC 8 /CS /RGB ID xyz\nEI\n",
			[]count{{"6.2.3.3", 1, 1}, {"6.2.3.3", 3, 1}},
			[]count{{"6.2.4.3", 2, 1}, {"6.2.4.3", 4, 1}},
		},
		{
			"an interpolated image after another", g + "ID xyz\nEI\n" + g + "/I true ID xyz\nEI\n",
			[]count{{"6.2.4", 3, 0}},
			[]count{{"6.2.8", 3, 0}},
		},
		{
			"an interpolated image alone", g + "/I true ID xyz\nEI\n",
			[]count{{"6.2.4", 3, 1}},
			[]count{{"6.2.8", 3, 1}},
		},
		{
			"a first image shorter than the second", "BI /F /LZW ID x EI\nBI /F /LZW ID yyyy EI\n",
			[]count{{"6.2.10", 1, 1}, {"6.1.10", 2, 1}},
			[]count{{"6.2.2", 1, 1}, {"6.1.10", 1, 1}},
		},
		{
			"a first image longer than the second", "BI /F /LZW ID xxxxxxxxxx EI\nBI /F /LZW ID y EI\nzz zz zz ww\n",
			[]count{{"6.2.10", 1, 1}},
			[]count{{"6.2.2", 1, 1}},
		},
		{
			"a stray EI", "q EI Q\n",
			[]count{{"6.2.10", 1, 1}},
			[]count{{"6.2.2", 1, 1}},
		},
		{
			"an ID no BI begins", "q ID xx EI\nQ zz\n",
			[]count{{"6.2.10", 1, 2}},
			[]count{{"6.2.2", 1, 2}},
		},
		{
			"an EI inside the data", g + "ID x EI zz EI\n",
			[]count{{"6.2.10", 1, 0}, {"6.2.3.3", 3, 1}},
			[]count{{"6.2.2", 1, 0}, {"6.2.4.3", 4, 1}},
		},
		{
			"an EI after data", g + "ID xxEI Q zz\n",
			[]count{{"6.2.10", 1, 1}, {"6.2.3.3", 3, 1}},
			[]count{{"6.2.2", 1, 1}, {"6.2.4.3", 4, 1}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data := cyclePDF(
				"<< /Type /Catalog /Pages 2 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources << >> /Contents 4 0 R >>",
				exponentStream("", tc.content),
			)
			for _, p := range []struct {
				profile models.PDFAProfile
				counts  []count
			}{{models.PDFA1b, tc.oneB}, {models.PDFA2b, tc.twoB}} {
				for _, c := range p.counts {
					if got := countRulePDFA(t, data, p.profile, c.clause, c.test); got != c.want {
						t.Errorf("%s: %s t%d fails %d times, want %d", p.profile, c.clause, c.test, got, c.want)
					}
				}
			}
		})
	}
}
