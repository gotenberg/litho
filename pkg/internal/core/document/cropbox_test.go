package document

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

func rect(v ...float64) ast.PdfArray {
	out := make(ast.PdfArray, len(v))
	for i, x := range v {
		out[i] = ast.PdfReal(x)
	}
	return out
}

func sameRect(a, b ast.PdfArray) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if Float(a[i]) != Float(b[i]) {
			return false
		}
	}
	return true
}

// TestClampToMediaBox pins that the visible region never exceeds the page.
//
// ISO 32000-1, 14.11.2 requires the crop box to lie within the media box, so a
// producer writing a larger one describes a region that does not exist. Taking
// it verbatim overstates the page size, and the size drives stamp placement and
// clipping.
//
// # Corpus evidence
//
//   - govdocs thread_200_200062: declares a 612x792 media box with a 614x794
//     crop box. Taken verbatim, litho answered 614x794 where poppler answers
//     612x792.
func TestClampToMediaBox(t *testing.T) {
	media := rect(0, 0, 612, 792)

	cases := []struct {
		name  string
		crop  ast.PdfArray
		media ast.PdfArray
		want  ast.PdfArray
	}{
		{"no crop box falls back to media", nil, media, media},
		{"crop inside media is kept", rect(10, 10, 100, 100), media, rect(10, 10, 100, 100)},
		{"crop equal to media is kept", rect(0, 0, 612, 792), media, rect(0, 0, 612, 792)},
		{"crop larger than media is clamped", rect(0, 0, 614, 794), media, rect(0, 0, 612, 792)},
		{"crop offset beyond media is clamped", rect(0, 0, 735, 798), rect(39, 39, 696, 759), rect(39, 39, 696, 759)},
		{"crop disjoint from media falls back", rect(900, 900, 1000, 1000), media, media},
		{"corners written backwards still clamp", rect(614, 794, 0, 0), media, rect(0, 0, 612, 792)},
		{"malformed crop falls back", rect(1, 2), media, media},
	}

	for _, c := range cases {
		got := clampToMediaBox(c.crop, c.media)
		if !sameRect(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
