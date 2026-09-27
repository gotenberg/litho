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

// TestAnnotOutsideCropBoxNormalizesRectangles pins that the isOutsideCropBox
// exemption of ISO 14289-1, 7.18 compares rectangles normalized, as ISO
// 32000-1, 7.9.5 has a processor read one written from any two diagonally
// opposite corners. A Link with no /Contents written reversed across an edge
// of its page, or lying on a page whose media or crop box is written reversed,
// is on the page and fails 7.18.1 t2 and 7.18.5 t1 and t2. veraPDF 1.30.2,
// which compares the corners as written, exempts all of them (see
// [isAnnotOutsideCropBox]); it agrees on the rectangles wholly on and wholly
// off the page.
func TestAnnotOutsideCropBoxNormalizesRectangles(t *testing.T) {
	t.Parallel()
	const (
		onPage  = "7.18.1/2:1 7.18.5/1:1 7.18.5/2:1"
		offPage = "7.18.1/2:0 7.18.5/1:0 7.18.5/2:0"
	)
	for _, tc := range []struct {
		name, boxes, rect, want string
	}{
		{"reversed across the left edge", "/MediaBox [0 0 612 792]", "100 100 -50 50", onPage},
		{"reversed across the right edge", "/MediaBox [0 0 612 792]", "700 150 500 100", onPage},
		{"reversed across the bottom edge", "/MediaBox [0 0 612 792]", "100 50 150 -50", onPage},
		{"reversed across the top edge", "/MediaBox [0 0 612 792]", "100 850 150 700", onPage},
		{"reversed on the page", "/MediaBox [0 0 612 792]", "200 200 100 100", onPage},
		{"reversed off the page", "/MediaBox [0 0 612 792]", "-10 150 -50 100", offPage},
		{"on a page whose media box is reversed", "/MediaBox [612 792 0 0]", "100 100 200 200", onPage},
		{"on a page whose media box is reversed in x", "/MediaBox [612 0 0 792]", "100 100 200 200", onPage},
		{"on a page whose crop box is reversed", "/MediaBox [0 0 612 792] /CropBox [612 792 0 0]", "100 100 200 200", onPage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t,
				"<< /Type /Catalog /Pages 2 0 R /MarkInfo << /Marked true >> /Lang (en) >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R "+tc.boxes+" /Resources << >> /Contents 4 0 R /Tabs /S /Annots [5 0 R] >>",
				exponentStream("", ""),
				"<< /Type /Annot /Subtype /Link /Rect ["+tc.rect+"] /F 4 /Border [0 0 0] >>",
			)
			viols, err := ValidatePDFUA(context.Background(), doc, models.PDFUA1)
			if err != nil {
				t.Fatalf("ValidatePDFUA: %v", err)
			}
			var got []string
			for _, rc := range []struct {
				clause string
				test   int
			}{{"7.18.1", 2}, {"7.18.5", 1}, {"7.18.5", 2}} {
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
