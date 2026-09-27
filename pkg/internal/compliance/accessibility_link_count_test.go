package compliance

import (
	"context"
	"testing"
)

// TestCheckLinkContents_JudgedAnnotations pins ISO 14289-1, 7.18.5 t2 counted
// in the unit veraPDF judges a Link in: once per Link, a Link written inline
// once more for every page whose /Annots holds it, and a Link that is an
// object of its own once whatever lists it. Each case measured against veraPDF
// 1.30.2 at PDF/UA-1.
func TestCheckLinkContents_JudgedAnnotations(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	link := func(contents string) string {
		return "<< /Type /Annot /Subtype /Link /Rect [50 600 200 620]" + contents + " >>"
	}
	docOf := func(page1, page2 string, extra ...string) []string {
		return append([]string{
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792]" + page1 + " >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792]" + page2 + " >>",
		}, extra...)
	}
	for _, tc := range []struct {
		name string
		objs []string
		want int
	}{
		{"one Link written inline", docOf(" /Annots ["+link("")+"]", ""), 1},
		{"two Links written inline", docOf(" /Annots ["+link("")+" "+link("")+"]", ""), 2},
		{"an inline Link two pages share", docOf(" /Annots 5 0 R", " /Annots 5 0 R", "["+link("")+"]"), 2},
		{"a Link object two pages list", docOf(" /Annots [5 0 R]", " /Annots [5 0 R]", link("")), 1},
		{"an inline Link off the second page's crop box", docOf(" /Annots 5 0 R", " /CropBox [300 300 612 792] /Annots 5 0 R", "["+link("")+"]"), 1},
		{"a Link with /Contents", docOf(" /Annots ["+link(" /Contents (a link)")+"]", ""), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, catalog := objsDocPDF(t, tc.objs...)
			got := 0
			for _, v := range checkLinkContents(ctx, doc, catalog, "PDF/UA-1") {
				if v.Rule == Rule_14289_1_7_18_5_t02 {
					got++
				}
			}
			if got != tc.want {
				t.Errorf("7.18.5 t2 = %d, want %d", got, tc.want)
			}
		})
	}
}
