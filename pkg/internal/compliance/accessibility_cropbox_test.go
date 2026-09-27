package compliance

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestAnnotOutsideCropBoxAsVeraPDFReadsIt pins the isOutsideCropBox exemption
// of ISO 14289-1, 7.18 on the crop box [pageCropBox] reads: one the page
// inherits from the page tree, a short one whose missing corners read as 0,
// and the crop box of the page listing a widget rather than the one its /P
// names. Every count was measured against veraPDF 1.30.2 at PDF/UA-1.
func TestAnnotOutsideCropBoxAsVeraPDFReadsIt(t *testing.T) {
	t.Parallel()
	const link = "<< /Type /Annot /Subtype /Link /Rect [%s] /F 4 /Border [0 0 0] >>"
	onePage := func(page, pages, rect string) []string {
		return []string{
			"<< /Type /Catalog /Pages 2 0 R /MarkInfo << /Marked true >> /Lang (en) >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1" + pages + " >>",
			"<< /Type /Page /Parent 2 0 R /Resources << >> /Contents 4 0 R /Tabs /S /Annots [5 0 R]" + page + " >>",
			exponentStream("", ""),
			fmt.Sprintf(link, rect),
		}
	}
	for _, tc := range []struct {
		name string
		objs []string
		want string
	}{
		{"a Link on a page inheriting its media box", onePage("", " /MediaBox [0 0 612 792]", "50 700 200 720"),
			"7.18.1/2:1 7.18.1/3:0 7.18.4/1:0 7.18.5/1:1 7.18.5/2:1"},
		{"a Link off a page inheriting its media box", onePage("", " /MediaBox [0 0 612 792]", "700 700 800 720"),
			"7.18.1/2:0 7.18.1/3:0 7.18.4/1:0 7.18.5/1:0 7.18.5/2:0"},
		{"a Link off its page's own media box", onePage(" /MediaBox [0 0 612 792]", "", "700 700 800 720"),
			"7.18.1/2:0 7.18.1/3:0 7.18.4/1:0 7.18.5/1:0 7.18.5/2:0"},
		{"a Link above a crop box of three numbers", onePage(" /MediaBox [0 0 612 792] /CropBox [0 0 300]", "", "50 700 200 720"),
			"7.18.1/2:0 7.18.1/3:0 7.18.4/1:0 7.18.5/1:0 7.18.5/2:0"},
		{"a widget whose /P names a page it lies off", []string{
			"<< /Type /Catalog /Pages 2 0 R /MarkInfo << /Marked true >> /Lang (en) /AcroForm << /Fields [6 0 R] >> >>",
			"<< /Type /Pages /Kids [3 0 R 5 0 R] /Count 2 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << >> /Contents 4 0 R /Tabs /S /Annots [6 0 R] >>",
			exponentStream("", ""),
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /CropBox [0 0 10 10] /Resources << >> /Contents 4 0 R /Tabs /S >>",
			"<< /Type /Annot /Subtype /Widget /FT /Tx /T (f) /Rect [50 600 200 620] /F 4 /P 5 0 R >>",
		}, "7.18.1/2:0 7.18.1/3:1 7.18.4/1:1 7.18.5/1:0 7.18.5/2:0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t, tc.objs...)
			viols, err := ValidatePDFUA(context.Background(), doc, models.PDFUA1)
			if err != nil {
				t.Fatalf("ValidatePDFUA: %v", err)
			}
			var got []string
			for _, rc := range []struct {
				clause string
				test   int
			}{{"7.18.1", 2}, {"7.18.1", 3}, {"7.18.4", 1}, {"7.18.5", 1}, {"7.18.5", 2}} {
				n := 0
				for _, e := range viols {
					if v, ok := e.(Violation); ok && v.Rule.Clause == rc.clause && v.Rule.TestNumber == rc.test {
						n++
					}
				}
				got = append(got, fmt.Sprintf("%s/%d:%d", rc.clause, rc.test, n))
			}
			if strings.Join(got, " ") != tc.want {
				t.Errorf("got %s, want %s", strings.Join(got, " "), tc.want)
			}
		})
	}
}
