package xlsx

import (
	"bytes"
	"context"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
)

// paritySources returns the directory Gotenberg's workbooks of the xlsx
// corpus sit under: LITHO_XLSX_SOURCES, else the corpus's sources.
func paritySources() string {
	if dir := os.Getenv("LITHO_XLSX_SOURCES"); dir != "" {
		return dir
	}
	return filepath.Join("..", "..", "..", "..", "test", "integration", "corpus", "gotenberg", "xlsx", "sources")
}

// findSource returns the path of the file name under dir, "" when absent.
func findSource(dir, name string) string {
	found := ""
	_ = filepath.WalkDir(dir, func(path string, e fs.DirEntry, err error) error { //nolint:errcheck // a missing corpus skips the test
		if err == nil && !e.IsDir() && e.Name() == name {
			found = path
			return fs.SkipAll
		}
		return nil
	})
	return found
}

// TestParity_GotenbergWorkbooks reads Gotenberg's workbooks with the
// pinned fixture fonts and checks the sheets, the cells at known
// addresses and the page setup LibreOffice prints them with.
//
// # Corpus evidence
//
//   - gotenberg/xlsx: budget prints Summary on a Letter portrait page and Monthly on
//     two landscape pages repeating its first row, column A of both 48.13
//     points wide; figures-lo prints Revenue and Costs on A4 pages with
//     0.7875-inch margins, a Times New Roman header with the sheet name
//     and a footer with the page number, its columns 63.97 points wide.
func TestParity_GotenbergWorkbooks(t *testing.T) {
	t.Parallel()
	fonts := pinnedFonts(t)
	for _, tc := range []struct {
		file  string
		check func(t *testing.T, doc *sheet.Document)
	}{
		{"budget.xlsx", func(t *testing.T, doc *sheet.Document) {
			if names := sheetNames(doc); !slices.Equal(names, []string{"Summary", "Monthly", "Notes"}) {
				t.Errorf("sheets %q", names)
			}
			sum, monthly := doc.Sheets[0], doc.Sheets[1]
			title, _ := cellAt(sum, 0, 0)
			if title.Value.Text != "FY2026 Budget" || title.Style == nil || title.Style.Font.Families[0] != "Cambria" || !title.Style.Font.Bold || title.Style.Font.Size != 16 {
				t.Errorf("A1 %+v", title)
			}
			if v, _ := cellAt(sum, 3, 1); v.Value != (sheet.Value{Kind: sheet.Number, Number: 120000}) {
				t.Errorf("B4 %+v", v.Value)
			}
			if len(sum.Merges) != 1 || sum.Merges[0] != (sheet.Range{LastColumn: 3}) {
				t.Errorf("merges %+v", sum.Merges)
			}
			if p := sum.Page; p.Width != 612 || p.Height != 792 || p.Margin != [4]float64{72, 54, 72, 54} {
				t.Errorf("Summary page %+v", p)
			}
			if p := monthly.Page; p.Width != 792 || p.Height != 612 || p.RepeatRows == nil || p.RepeatRows.FirstRow != 0 || p.RepeatRows.LastRow != 0 {
				t.Errorf("Monthly page %+v", p)
			}
			if c, _ := cellAt(monthly, 1, 1); c.Style == nil || c.Style.Format != "#,##0.00" {
				t.Errorf("B2 format %+v", c.Style)
			}
			if math.Abs(sum.DefaultColumnWidth-48.15) > 1e-9 || sum.DefaultRowHeight != 15 {
				t.Errorf("defaults %v by %v", sum.DefaultColumnWidth, sum.DefaultRowHeight)
			}
		}},
		{"figures-lo.xlsx", func(t *testing.T, doc *sheet.Document) {
			if names := sheetNames(doc); !slices.Equal(names, []string{"Revenue", "Costs"}) {
				t.Errorf("sheets %q", names)
			}
			rev := doc.Sheets[0]
			if c, _ := cellAt(rev, 1, 0); c.Value.Number != 46053 || c.Style == nil || c.Style.Format != `yyyy\-mm\-dd` {
				t.Errorf("A2 %+v", c)
			}
			p := rev.Page
			if math.Abs(p.Width-595.28) > 0.01 || math.Abs(p.Height-841.89) > 0.01 || math.Abs(p.Margin[3]-56.69) > 0.01 || p.FirstPageNumber != 1 {
				t.Errorf("page %+v", p)
			}
			h := rev.HeaderFooter.Odd.Header[1]
			if len(h) != 1 || h[0].Field != sheet.FieldSheet || h[0].Font.Families[0] != "Times New Roman" || h[0].Font.Size != 12 {
				t.Errorf("header %+v", h)
			}
			if rev.DefaultColumnWidth != 64 {
				t.Errorf("column width %v", rev.DefaultColumnWidth)
			}
		}},
		{"singlepagesheets-scrolled.xlsx", func(t *testing.T, doc *sheet.Document) {
			if len(doc.Sheets) == 0 || !sheetHolds(doc.Sheets[0], "Meteor") {
				t.Error("Meteor is not in the first sheet")
			}
			if sh := doc.Sheets[0]; sh.Rows[0].Height != 0 || sh.Columns[0].Width != 122.1 {
				t.Errorf("row 1 %v high, column A %v wide: want the optimal height and 22 digits", sh.Rows[0].Height, sh.Columns[0].Width)
			}
		}},
	} {
		t.Run(tc.file, func(t *testing.T) {
			t.Parallel()
			path := findSource(paritySources(), tc.file)
			if path == "" {
				t.Skipf("%s not in the xlsx corpus sources", tc.file)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			doc, _, err := load(context.Background(), bytes.NewReader(data), fonts, office.Options{})
			if err != nil {
				t.Fatal(err)
			}
			tc.check(t, doc)
		})
	}
}

// sheetNames returns the names of doc's sheets.
func sheetNames(doc *sheet.Document) []string {
	var out []string
	for _, s := range doc.Sheets {
		out = append(out, s.Name)
	}
	return out
}

// sheetHolds reports whether the text of a cell of sh holds s.
func sheetHolds(sh *sheet.Sheet, s string) bool {
	for _, r := range sh.Rows {
		for _, c := range r.Cells {
			if strings.Contains(c.Value.Text, s) {
				return true
			}
		}
	}
	return false
}
