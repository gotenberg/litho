package layout

import "testing"

// TestTable_RowSpanBreaks breaks the rows a row-spanning cell joins
// between them, as Writer breaks them, whether a page could hold them all
// or not: the table starts on the first page, after the block above it,
// the spanning cell sliced at the cut.
func TestTable_RowSpanBreaks(t *testing.T) {
	pb := PageBox{Width: px(4), Height: px(6)}
	tests := []struct {
		name  string
		rows  int
		page2 string
	}{
		{"fits a page", 5, `
			RB__
			____
			____
			____
			____
			____`},
		{"taller than a page", 8, `
			RB__
			RB__
			RB__
			RB__
			____
			____`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := make([]*Node, tt.rows)
			for i := range rows {
				kids := []*Node{td(mods(height(1), width(1), bg(blue)), "")}
				if i == 0 {
					kids = append([]*Node{spans(td(mods(width(1), bg(red)), ""), 1, tt.rows)}, kids...)
				}
				rows[i] = tr(nil, kids...)
			}
			above := el(sty(height(2), width(1), bg(lime)))
			res := run(t, doc(above, table(mods(spacing(0, 0)), rows...)), pb)
			if len(res.Pages) != 2 {
				t.Fatalf("%d pages, want 2", len(res.Pages))
			}
			assertPixels(t, res, 1, `
				G___
				G___
				RB__
				RB__
				RB__
				RB__`)
			assertPixels(t, res, 2, tt.page2)
		})
	}
}
