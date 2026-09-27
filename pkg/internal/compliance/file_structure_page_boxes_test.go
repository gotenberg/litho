package compliance

import (
	"context"
	"testing"
)

// TestCheckPageBoundariesShortBoxes pins ISO 19005-2, 6.1.13 t11 on page
// boundaries that are not four numbers. veraPDF reads a missing corner, and
// one that is no number, as 0 (GFCosBBox.getRequiredValue answers null, which
// the rule's arithmetic takes for 0), and reads the first four of a longer
// array. Every count was measured against veraPDF 1.30.2 at PDF/A-2b and
// PDF/A-3b, except the indirect corner, which veraPDF reads as 0 too: litho
// reads the number it names, see [checkPageBoundaries].
func TestCheckPageBoundariesShortBoxes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, page, pages string
		extra             []string
		want              int
	}{
		{name: "four numbers", page: " /MediaBox [0 0 612 792]", want: 0},
		{name: "three numbers", page: " /MediaBox [0 0 612]", want: 1},
		{name: "two numbers", page: " /MediaBox [0 612]", want: 1},
		{name: "empty", page: " /MediaBox []", want: 1},
		{name: "a name corner", page: " /MediaBox [0 0 612 /Foo]", want: 1},
		{name: "a null corner", page: " /MediaBox [0 0 612 null]", want: 1},
		{name: "five numbers", page: " /MediaBox [0 0 612 792 7]", want: 0},
		{name: "a short crop box", page: " /MediaBox [0 0 612 792] /CropBox [0 0 100]", want: 1},
		{name: "short bleed, trim and art boxes", page: " /MediaBox [0 0 612 792] /BleedBox [0 0] /TrimBox [10 10 300] /ArtBox [0]", want: 2},
		{name: "an inherited short media box", pages: " /MediaBox [0 0 5]", want: 1},
		{name: "no array", page: " /MediaBox 612", want: 0},
		{name: "an indirect corner", page: " /MediaBox [0 0 612 5 0 R]", extra: []string{"792"}, want: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			objs := append([]string{
				"<< /Type /Catalog /Pages 2 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1" + tc.pages + " >>",
				"<< /Type /Page /Parent 2 0 R /Resources << >> /Contents 4 0 R" + tc.page + " >>",
				exponentStream("", ""),
			}, tc.extra...)
			doc, catalog := objsDocPDF(t, objs...)
			for _, profile := range []string{"PDF/A-2b", "PDF/A-3b"} {
				if got := len(checkPageBoundaries(context.Background(), doc, catalog, profile)); got != tc.want {
					t.Errorf("%s: 6.1.13 t11 = %d, want %d", profile, got, tc.want)
				}
			}
		})
	}
}
