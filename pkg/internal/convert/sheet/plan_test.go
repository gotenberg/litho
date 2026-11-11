package sheet

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// grid150 returns a sheet of 150 rows by 20 columns of 50 points, each
// cell holding text.
func grid150(ps PageSetup) *Sheet {
	sh := &Sheet{Name: "S", DefaultColumnWidth: 50, DefaultRowHeight: 12.8, Page: ps}
	for r := range 150 {
		row := Row{Index: r}
		for c := range 20 {
			row.Cells = append(row.Cells, textCell(c, fmt.Sprintf("r%dc%d", r, c), nil))
		}
		sh.Rows = append(sh.Rows, row)
	}
	return sh
}

// TestPlan_FitToPages locks the zoom Calc finds for a sheet fitted to
// pages and the pages it prints at it.
//
// # Corpus evidence
//
//   - litho/pdf: three flat ODS sheets of 150 rows of 12.8 points by 20
//     columns of 50 points on A4 with half-inch margins, converted by
//     Gotenberg 9.0.0: fitted one page wide they print at 52% (5.2 point
//     text) on 2 pages, one page wide and tall at 40% on 1 page, two
//     pages tall at 80% on 4 pages.
func TestPlan_FitToPages(t *testing.T) {
	tests := []struct {
		name       string
		fitW, fitH int
		zoom       float64
		pages      int
	}{
		{"one page wide", 1, 0, 0.52, 2},
		{"one page", 1, 1, 0.40, 1},
		{"two pages tall", 0, 2, 0.80, 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ps := a4()
			ps.Scale, ps.FitWidth, ps.FitHeight = 0, tt.fitW, tt.fitH
			doc := newDoc(grid150(ps))
			b := newBuilder(context.Background(), doc, pinnedFonts(t), office.Options{})
			sp := b.planSheet(0, doc.Sheets[0])
			if !near(sp.zoom, tt.zoom, 1e-9) || len(sp.pages) != tt.pages {
				t.Errorf("zoom %v on %d pages, want %v on %d", sp.zoom, len(sp.pages), tt.zoom, tt.pages)
			}
		})
	}
}

// TestPlan_Pages locks how a sheet is cut into pages: bands at the page's
// room, manual breaks, the page order, repeated rows, hidden rows and the
// pages without content left out.
func TestPlan_Pages(t *testing.T) {
	type want struct {
		rows, cols [2]int
		rep        bool
	}
	tests := []struct {
		name  string
		edit  func(sh *Sheet)
		pages []want
	}{
		{"down then over", func(sh *Sheet) {}, []want{
			{rows: [2]int{0, 59}, cols: [2]int{0, 9}},
			{rows: [2]int{60, 119}, cols: [2]int{0, 9}},
			{rows: [2]int{120, 149}, cols: [2]int{0, 9}},
			{rows: [2]int{0, 59}, cols: [2]int{10, 19}},
			{rows: [2]int{60, 119}, cols: [2]int{10, 19}},
			{rows: [2]int{120, 149}, cols: [2]int{10, 19}},
		}},
		{"over then down", func(sh *Sheet) { sh.Page.Order = OverThenDown }, []want{
			{rows: [2]int{0, 59}, cols: [2]int{0, 9}},
			{rows: [2]int{0, 59}, cols: [2]int{10, 19}},
			{rows: [2]int{60, 119}, cols: [2]int{0, 9}},
			{rows: [2]int{60, 119}, cols: [2]int{10, 19}},
			{rows: [2]int{120, 149}, cols: [2]int{0, 9}},
			{rows: [2]int{120, 149}, cols: [2]int{10, 19}},
		}},
		{"manual breaks", func(sh *Sheet) {
			sh.Page.RowBreaks = []int{10}
			sh.Page.ColumnBreaks = []int{5}
			sh.Page.PrintAreas = []Range{{FirstRow: 0, LastRow: 19, FirstColumn: 0, LastColumn: 9}}
		}, []want{
			{rows: [2]int{0, 9}, cols: [2]int{0, 4}},
			{rows: [2]int{10, 19}, cols: [2]int{0, 4}},
			{rows: [2]int{0, 9}, cols: [2]int{5, 9}},
			{rows: [2]int{10, 19}, cols: [2]int{5, 9}},
		}},
		{"repeated rows", func(sh *Sheet) {
			sh.Page.RepeatRows = &Range{FirstRow: 0, LastRow: 1}
			sh.Page.PrintAreas = []Range{{FirstRow: 0, LastRow: 119, FirstColumn: 0, LastColumn: 3}}
		}, []want{
			{rows: [2]int{0, 59}, cols: [2]int{0, 3}}, {rows: [2]int{60, 117}, cols: [2]int{0, 3}, rep: true}, {rows: [2]int{118, 119}, cols: [2]int{0, 3}, rep: true},
		}},
		{"empty pages left out", func(sh *Sheet) {
			// Keep the first row and the last: the pages between hold
			// nothing.
			sh.Rows = []Row{sh.Rows[0], sh.Rows[149]}
			sh.Rows[0].Cells = sh.Rows[0].Cells[:1]
			sh.Rows[1].Cells = sh.Rows[1].Cells[:1]
		}, []want{{rows: [2]int{0, 59}, cols: [2]int{0, 0}}, {rows: [2]int{120, 149}, cols: [2]int{0, 0}}}},
		{"hidden rows take no room", func(sh *Sheet) {
			for i := range 30 {
				sh.Rows[i].Hidden = true
			}
			sh.Page.PrintAreas = []Range{{FirstRow: 0, LastRow: 119, FirstColumn: 0, LastColumn: 3}}
		}, []want{{rows: [2]int{0, 89}, cols: [2]int{0, 3}}, {rows: [2]int{90, 119}, cols: [2]int{0, 3}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sh := grid150(a4())
			tt.edit(sh)
			doc := newDoc(sh)
			b := newBuilder(context.Background(), doc, pinnedFonts(t), office.Options{})
			sp := b.planSheet(0, sh)
			var got []want
			for _, p := range sp.pages {
				got = append(got, want{rows: [2]int{p.rows.first, p.rows.last}, cols: [2]int{p.cols.first, p.cols.last}, rep: p.repRows != nil})
			}
			if fmt.Sprint(got) != fmt.Sprint(tt.pages) {
				t.Errorf("pages %v, want %v", got, tt.pages)
			}
		})
	}
}

// TestConvert_Options locks the document options: page ranges keep the
// page numbers of the whole document, the outline names each sheet at its
// first page, the page cap refuses a longer document, the paper and
// landscape options replace each sheet's.
func TestConvert_Options(t *testing.T) {
	two := func() *Document {
		doc := newDoc(grid150(a4()))
		s2 := &Sheet{
			Name: "Second", DefaultColumnWidth: 50, DefaultRowHeight: 12.8, Page: a4(),
			Rows: []Row{{Index: 0, Cells: []Cell{textCell(0, "only", nil)}}},
		}
		doc.Sheets = append(doc.Sheets, s2, &Sheet{
			Name: "Hidden", Hidden: true, Page: a4(),
			Rows: []Row{{Index: 0, Cells: []Cell{textCell(0, "hidden", nil)}}},
		})
		return doc
	}
	out := convert(t, two(), office.Options{})
	if len(out.Pages) != 7 {
		t.Fatalf("%d pages, want 7", len(out.Pages))
	}
	if len(out.Outline) != 2 || out.Outline[0].Title != "S" || out.Outline[1].Title != "Second" || out.Outline[1].Dest.Page != 7 {
		t.Errorf("outline %+v", out.Outline)
	}
	out = convert(t, two(), office.Options{OmitOutline: true})
	if len(out.Pages) != 7 || out.Outline != nil || runs(out.Pages[6])[0].text != "only" {
		t.Errorf("%d pages, outline %v", len(out.Pages), out.Outline)
	}
	out = convert(t, two(), office.Options{PageWidth: 612, PageHeight: 792, Landscape: true})
	if pg := out.Pages[0]; pg.Width != 792 || pg.Height != 612 {
		t.Errorf("page %vx%v, want 792x612", pg.Width, pg.Height)
	}
	_, err := Convert(context.Background(), two(), pinnedFonts(t), office.Options{MaxPages: 3})
	if !errors.Is(err, office.ErrTooManyPages) {
		t.Errorf("MaxPages 3: %v, want ErrTooManyPages", err)
	}
	if _, err := Convert(context.Background(), nil, pinnedFonts(t), office.Options{}); !errors.Is(err, office.ErrInvalidOptions) {
		t.Errorf("nil document: %v", err)
	}
	var empty *text.Fonts
	if _, err := Convert(context.Background(), two(), empty, office.Options{}); !errors.Is(err, office.ErrInvalidOptions) {
		t.Errorf("nil fonts: %v", err)
	}
}

// TestConvert_SinglePageSheets locks LibreOffice's SinglePageSheets
// export: one page per sheet the size of its cells from A1, no margins.
func TestConvert_SinglePageSheets(t *testing.T) {
	doc := newDoc(grid150(a4()))
	out := convert(t, doc, office.Options{SinglePageSheets: true})
	if len(out.Pages) != 1 {
		t.Fatalf("%d pages, want 1", len(out.Pages))
	}
	if pg := out.Pages[0]; !near(pg.Width, 1000, 1e-6) || !near(pg.Height, 1920, 1e-6) {
		t.Errorf("page %vx%v, want 1000x1920", pg.Width, pg.Height)
	}
	if r := find(t, out.Pages[0], "r0c0"); !near(r.x, 1, 0.1) {
		t.Errorf("first cell at x %v, want 1", r.x)
	}
}

// TestPlan_ExcelGridLine locks the room Excel's rules give the grid line
// beside a page's columns: columns that fill the page's width exactly
// print on one page by Calc's rules and two by Excel's.
func TestPlan_ExcelGridLine(t *testing.T) {
	for _, tc := range []struct {
		excel bool
		want  int
	}{{false, 1}, {true, 2}} {
		page := a4()
		// Five columns of a fifth of the page's room each.
		w := (page.Width - page.Margin[1] - page.Margin[3]) / 5
		sh := &Sheet{Name: "S", DefaultColumnWidth: w, Page: page, Rows: []Row{{Index: 0, Cells: []Cell{numCell(0, 1, nil), numCell(4, 5, nil)}}}}
		doc := newDoc(sh)
		doc.ExcelRules = tc.excel
		if n := len(convert(t, doc, office.Options{}).Pages); n != tc.want {
			t.Errorf("Excel's rules %v: %d pages, want %d", tc.excel, n, tc.want)
		}
	}
}

// TestConvert_ExcelClipsWholeText locks the text of a cell its neighbour
// clips: Calc's rules shorten it to the characters that show, Excel's
// write it whole under the clip.
func TestConvert_ExcelClipsWholeText(t *testing.T) {
	for _, tc := range []struct {
		excel bool
		want  string
	}{{false, "Overflo"}, {true, "Overflowing text"}} {
		sh := &Sheet{Name: "S", DefaultColumnWidth: 30, Rows: []Row{{Index: 0, Cells: []Cell{textCell(0, "Overflowing text", nil), textCell(1, "B", nil)}}}}
		doc := newDoc(sh)
		doc.ExcelRules = tc.excel
		pg := convert(t, doc, office.Options{}).Pages[0]
		got := ""
		for _, r := range runs(pg) {
			if strings.HasPrefix(r.text, "Over") {
				got = r.text
			}
		}
		if !strings.HasPrefix(got, tc.want[:4]) || (tc.excel && got != tc.want) || (!tc.excel && got == "Overflowing text") {
			t.Errorf("Excel's rules %v: %q, want %q", tc.excel, got, tc.want)
		}
	}
}

// TestPlan_ExcelPrintsFormattedPages locks the page of formatted cells
// alone: Calc leaves it out, Excel prints it.
//
// # Corpus evidence
//
//   - poi/xlsx: 61605.xlsx prints a second page holding a yellow bordered
//     cell without a value; 53798.xlsx and 60384.xlsx alike.
func TestPlan_ExcelPrintsFormattedPages(t *testing.T) {
	filled := &Style{Fill: Fill{Color: css.Color{R: 255, G: 255, A: 255}}}
	for _, tc := range []struct {
		excel bool
		want  int
	}{{false, 1}, {true, 2}} {
		sh := &Sheet{Name: "S", DefaultColumnWidth: 100, Rows: []Row{{Index: 0, Cells: []Cell{numCell(0, 1, nil), {Column: 8, Style: filled}}}}}
		doc := newDoc(sh)
		doc.ExcelRules = tc.excel
		if n := len(convert(t, doc, office.Options{}).Pages); n != tc.want {
			t.Errorf("Excel's rules %v: %d pages, want %d", tc.excel, n, tc.want)
		}
	}
}
