package compliance

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// drawnImageObjs assembles the objects of a PDF whose pages are pages, page i
// being object 10+i and the catalog and page tree objects 1 and 2, with extra
// placing further objects by number.
func drawnImageObjs(pages []string, extra map[int]string) []string {
	kids := make([]string, len(pages))
	for i := range pages {
		kids[i] = fmt.Sprintf("%d 0 R", 10+i)
	}
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), len(pages)),
	}
	for len(objs) < 9 {
		objs = append(objs, "null")
	}
	objs = append(objs, pages...)
	for n, body := range extra {
		objs = withObject(objs, n, body)
	}
	return objs
}

// TestImageRulesJudgeDrawnXObjects pins that the XObject rules judge the
// images a Do operator draws, each object once, and not the entries of a
// form's /Resources its content never draws; see [scanXObjectResources].
//
// Every count was measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b, on an
// image whose /Intent is /Bogus.
func TestImageRulesJudgeDrawnXObjects(t *testing.T) {
	t.Parallel()

	image := func(sample string) string {
		return exponentStream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray "+
			"/BitsPerComponent 8 /Intent /Bogus", sample)
	}
	form := func(res, body string) string {
		return exponentStream("/Type /XObject /Subtype /Form /BBox [0 0 10 10] /Resources "+res, body)
	}
	page := func(res, contents string) string {
		return "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources " + res + " /Contents " + contents + " >>"
	}
	const draw = "q 10 0 0 10 0 0 cm /I1 Do Q"
	pageImage := page("<< /XObject << /I1 30 0 R >> >>", "20 0 R")
	pageForm := page("<< /XObject << /X1 31 0 R >> >>", "20 0 R")

	for _, tc := range []struct {
		name  string
		pages []string
		extra map[int]string
		want  int
	}{
		{"drawn twice on a page", []string{pageImage},
			map[int]string{20: exponentStream("", draw+" "+draw), 30: image("\x80")}, 1},
		{"drawn on two pages", []string{pageImage, page("<< /XObject << /I1 30 0 R >> >>", "21 0 R")},
			map[int]string{20: exponentStream("", draw), 21: exponentStream("", draw), 30: image("\x80")}, 1},
		{"drawn under two names", []string{page("<< /XObject << /I1 30 0 R /I2 30 0 R >> >>", "20 0 R")},
			map[int]string{20: exponentStream("", draw+" q /I2 Do Q"), 30: image("\x80")}, 1},
		{"listed by the page and not drawn", []string{pageImage},
			map[int]string{20: exponentStream("", "0 0 1 1 re f"), 30: image("\x80")}, 0},
		{"listed by a form the page draws and not drawn", []string{pageForm},
			map[int]string{20: exponentStream("", "/X1 Do"), 30: image("\x80"), 31: form("<< /XObject << /I1 30 0 R >> >>", "0 0 1 1 re f")}, 0},
		{"listed by a nested form and not drawn", []string{pageForm},
			map[int]string{
				20: exponentStream("", "/X1 Do"), 30: image("\x80"),
				31: form("<< /XObject << /X2 33 0 R >> >>", "/X2 Do"),
				33: form("<< /XObject << /I1 30 0 R >> >>", "0 0 1 1 re f"),
			}, 0},
		{"drawn by a form two pages draw", []string{pageForm, page("<< /XObject << /X1 31 0 R >> >>", "20 0 R")},
			map[int]string{20: exponentStream("", "/X1 Do"), 30: image("\x80"), 31: form("<< /XObject << /I1 30 0 R >> >>", draw)}, 1},
		{"listed by a form the page does not draw", []string{pageForm},
			map[int]string{20: exponentStream("", "0 0 1 1 re f"), 30: image("\x80"), 31: form("<< /XObject << /I1 30 0 R >> >>", draw)}, 0},
		// The second page shares the first page's content stream, which
		// veraPDF judges once, through the first page's resources.
		{"a second image drawn by a shared content stream", []string{pageImage, page("<< /XObject << /I1 32 0 R >> >>", "20 0 R")},
			map[int]string{20: exponentStream("", draw), 30: image("\x80"), 32: image("\x40")}, 1},
	} {
		for _, p := range []struct {
			profile models.PDFAProfile
			rule    Rule
		}{
			{models.PDFA1b, Rule_19005_1_6_2_9_t01},
			{models.PDFA2b, Rule_19005_2_6_2_6_t01},
		} {
			t.Run(string(p.profile)+" "+tc.name, func(t *testing.T) {
				t.Parallel()
				doc, _ := objsDocPDF(t, drawnImageObjs(tc.pages, tc.extra)...)
				viols, err := ValidatePDFA(context.Background(), doc, p.profile)
				if err != nil {
					t.Fatalf("ValidatePDFA: %v", err)
				}
				if got := countRuleErrs(t, viols, p.rule); got != tc.want {
					t.Errorf("%s test %d fired %d times, want %d", p.rule.Clause, p.rule.TestNumber, got, tc.want)
				}
			})
		}
	}
}
