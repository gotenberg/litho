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

// TestClampToMediaBox pins that no page boundary exceeds the page.
//
// ISO 32000-1, 14.11.2.1 reduces the crop, bleed, trim and art boxes to their
// intersection with the media box when they extend beyond it, so a producer
// writing a larger one describes a region that does not exist. Taking it
// verbatim overstates the page size, and the crop box size drives stamp
// placement and clipping. The default is the boundary's Table 30 default: the
// media box for the crop box, the crop box for the other three.
//
// # Corpus evidence
//
//   - govdocs thread_200_200062: declares a 612x792 media box with a 614x794
//     crop box. Taken verbatim, litho answered 614x794 where poppler answers
//     612x792.
func TestClampToMediaBox(t *testing.T) {
	media := rect(0, 0, 612, 792)
	crop := rect(10, 10, 602, 782)

	cases := []struct {
		name  string
		box   ast.PdfArray
		media ast.PdfArray
		def   ast.PdfArray
		want  ast.PdfArray
	}{
		{"no crop box falls back to media", nil, media, media, media},
		{"crop inside media is kept", rect(10, 10, 100, 100), media, media, rect(10, 10, 100, 100)},
		{"crop equal to media is kept", rect(0, 0, 612, 792), media, media, rect(0, 0, 612, 792)},
		{"crop larger than media is clamped", rect(0, 0, 614, 794), media, media, rect(0, 0, 612, 792)},
		{"crop offset beyond media is clamped", rect(0, 0, 735, 798), rect(39, 39, 696, 759), rect(39, 39, 696, 759), rect(39, 39, 696, 759)},
		{"crop disjoint from media falls back", rect(900, 900, 1000, 1000), media, media, media},
		{"corners written backwards still clamp", rect(614, 794, 0, 0), media, media, rect(0, 0, 612, 792)},
		{"malformed crop falls back", rect(1, 2), media, media, media},

		{"absent bleed box takes the crop box", nil, media, crop, crop},
		{"bleed box larger than media is clamped to media, not crop", rect(-10, -10, 622, 802), media, crop, rect(0, 0, 612, 792)},
		{"bleed box wider than crop but inside media is kept", rect(5, 5, 607, 787), media, crop, rect(5, 5, 607, 787)},
		{"trim box beyond one edge is clamped on that edge", rect(0, 0, 700, 700), media, crop, rect(0, 0, 612, 700)},
		{"disjoint trim box takes the crop box, not the media box", rect(900, 900, 1000, 1000), media, crop, crop},
		{"art box meeting media only on an edge takes the crop box", rect(612, 0, 700, 792), media, crop, crop},
		{"zero-area art box takes the crop box", rect(0, 0, 0, 0), media, crop, crop},
		{"five-entry bleed box counts as absent", rect(1, 1, 600, 700, 9), media, crop, crop},
		{"no media box leaves the box as written", rect(0, 0, 700, 900), nil, crop, rect(0, 0, 700, 900)},
	}

	for _, c := range cases {
		got := clampToMediaBox(c.box, c.media, c.def)
		if !sameRect(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
