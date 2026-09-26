package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestCheckFormXObject_PostScriptByPart covers the split between the parts that
// prohibit a PostScript form XObject and the one that does not. ISO 19005-2 and
// -3, 6.2.9 fold /OPI, /Subtype2 /PS and /PS into a single rule; ISO 19005-4,
// 6.2.8.1 covers /OPI alone, because ISO 32000-2 removed PostScript XObjects
// from the format. Reporting a PostScript form under the part 4 rule accused it
// of carrying an /OPI key it does not have. veraPDF states the folded rule as
// one test on the form, so a form carrying several of the keys fails it once.
func TestCheckFormXObject_PostScriptByPart(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc := nopDoc()

	partRule := map[int]Rule{
		2: Rule_19005_2_6_2_9_t01,
		3: Rule_19005_3_6_2_9_t01,
		4: Rule_19005_4_6_2_8_1_t01,
	}

	for _, tc := range []struct {
		name string
		dict ast.PdfDict
		want map[int]int
	}{
		{
			name: "a PostScript form answers to parts 2 and 3 only",
			dict: ast.PdfDict{"Subtype2": ast.PdfName("PS")},
			want: map[int]int{2: 1, 3: 1, 4: 0},
		},
		{
			name: "a form carrying the PS key answers to parts 2 and 3 only",
			dict: ast.PdfDict{"PS": ast.PdfDict{}},
			want: map[int]int{2: 1, 3: 1, 4: 0},
		},
		{
			name: "an OPI key answers to every part",
			dict: ast.PdfDict{"OPI": ast.PdfDict{}},
			want: map[int]int{2: 1, 3: 1, 4: 1},
		},
		{
			name: "a PostScript form carrying OPI answers once at every part",
			dict: ast.PdfDict{"OPI": ast.PdfDict{}, "Subtype2": ast.PdfName("PS")},
			want: map[int]int{2: 1, 3: 1, 4: 1},
		},
		{
			name: "a plain form answers to nothing",
			dict: ast.PdfDict{},
			want: map[int]int{2: 0, 3: 0, 4: 0},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for part, want := range tc.want {
				got := checkFormXObject(ctx, doc, tc.dict, "Fm0", 1, part, "")
				var n int
				for _, v := range got {
					if v.Rule == partRule[part] {
						n++
					}
				}
				if n != want {
					t.Errorf("part %d: %d violations of %s, want %d (%v)",
						part, n, partRule[part].Clause, want, got)
				}
			}
		})
	}
}
