package ods

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
	"github.com/gotenberg/litho/pkg/internal/css"
)

// corpusSources is where the ods corpus keeps Gotenberg's spreadsheets.
const corpusSources = "../../../../test/integration/corpus/gotenberg/ods/sources"

// TestParity_Figures reads Gotenberg's figures.ods and figures.fods: two
// sheets, Revenue and Costs, with their markers, dates, numbers and Calc's
// Default page style.
//
// # Corpus evidence
//
//   - ods: figures and figures-flat print 2 Letter pages, the sheet name in
//     the header, "Page 1" and "Page 2" in the footer, "REVENUE-SH" on
//     page 1 and "COSTS-SHEE" on page 2, 2026-01-31 in ISO form.
func TestParity_Figures(t *testing.T) {
	for _, p := range []string{"figures/figures.ods", "figures-flat/figures.fods"} {
		t.Run(p, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(corpusSources, p))
			if err != nil {
				t.Skipf("corpus source not present: %v", err)
			}
			doc, _ := read(t, data)
			if len(doc.Sheets) != 2 || doc.Sheets[0].Name != "Revenue" || doc.Sheets[1].Name != "Costs" {
				t.Fatalf("sheets = %+v", doc.Sheets)
			}
			rev := doc.Sheets[0]
			for _, c := range []struct {
				row, col int
				want     sheet.Value
				format   string
			}{
				{0, 0, sheet.Value{Kind: sheet.Text, Text: "Month"}, ""},
				{1, 0, sheet.Value{Kind: sheet.Number, Number: 46053, Text: "2026-01-31"}, `yyyy\-mm\-dd`},
				{2, 1, sheet.Value{Kind: sheet.Number, Number: 2500.5, Text: "2500.5"}, ""},
				{3, 0, sheet.Value{Kind: sheet.Text, Text: "REVENUE-SHEET-MARKER"}, ""},
				{3, 1, sheet.Value{Kind: sheet.Number, Number: 3500.5, Text: "3500.5"}, ""},
			} {
				got := cellAt(rev, c.row, c.col)
				if got == nil || got.Value != c.want {
					t.Errorf("R%dC%d = %+v, want %+v", c.row, c.col, got, c.want)
					continue
				}
				format := ""
				if got.Style != nil {
					format = got.Style.Format
				}
				if format != c.format {
					t.Errorf("R%dC%d format = %q, want %q", c.row, c.col, format, c.format)
				}
			}
			if c := cellAt(doc.Sheets[1], 1, 0); c == nil || c.Value.Text != "COSTS-SHEET-MARKER" {
				t.Errorf("Costs A2 = %+v", c)
			}
			for _, sh := range doc.Sheets {
				pg, hf := sh.Page, sh.HeaderFooter
				if pg.Width != 612 || pg.Height != 792 || !near(pg.Margin[css.Left], defaultMargin) || !near(pg.Margin[css.Top], defaultMargin+defaultHFHeight) ||
					pg.Scale != 100 || pg.FirstPageNumber != 0 {
					t.Errorf("%s page = %+v", sh.Name, pg)
				}
				if len(hf.Odd.Header[1]) != 1 || hf.Odd.Header[1][0].Field != sheet.FieldSheet || len(hf.Odd.Footer[1]) != 2 || hf.Odd.Footer[1][1].Field != sheet.FieldPage ||
					hf.DifferentOddEven || hf.DifferentFirst {
					t.Errorf("%s header and footer = %+v", sh.Name, hf)
				}
				w := sh.DefaultColumnWidth
				if len(sh.Columns) > 0 {
					w = sh.Columns[0].Width
				}
				if !near(w, 64) && !near(w, 64.008) {
					t.Errorf("%s columns = %+v", sh.Name, sh.Columns)
				}
			}
		})
	}
}
