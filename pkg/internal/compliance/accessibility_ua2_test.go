package compliance

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestPageActionObjects covers the §8.8 page additional-action scan: only the
// /O and /C triggers name a page action (ISO 32000-2 Table 197). Annotation
// triggers such as /X on a page are not actions and must be ignored, which is
// what keeps the structure-destination check from false-firing.
func TestPageActionObjects(t *testing.T) {
	t.Parallel()
	ctx := testCtx()
	doc := nopDoc()
	act := func(n int) ast.PdfIndirectRef { return ast.PdfIndirectRef{ObjectNum: n} }

	tests := []struct {
		name string
		page ast.PdfDict
		want int
	}{
		{"no AA", ast.PdfDict{}, 0},
		{"open trigger", ast.PdfDict{"AA": ast.PdfDict{"O": act(1)}}, 1},
		{"open and close", ast.PdfDict{"AA": ast.PdfDict{"O": act(1), "C": act(2)}}, 2},
		{"annotation trigger /X ignored", ast.PdfDict{"AA": ast.PdfDict{"X": act(1)}}, 0},
		{"only /O and /C of a mixed AA", ast.PdfDict{"AA": ast.PdfDict{"O": act(1), "X": act(2), "C": act(3)}}, 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := len(pageActionObjects(ctx, doc, tc.page)); got != tc.want {
				t.Errorf("pageActionObjects(%v) = %d actions, want %d", tc.page, got, tc.want)
			}
		})
	}
}
