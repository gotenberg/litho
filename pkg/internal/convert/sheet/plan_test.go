package sheet

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
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
//   - probe, ods: three flat ODS sheets of 150 rows of 12.8 points by 20
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
			sp := b.planOf(t, 0, doc.Sheets[0])
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
			sp := b.planOf(t, 0, sh)
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
	if !errors.Is(err, limits.ErrTooManyPages) {
		t.Errorf("MaxPages 3: %v, want ErrTooManyPages", err)
	}
	if _, err := Convert(context.Background(), nil, pinnedFonts(t), office.Options{}); !errors.Is(err, typeset.ErrInvalidOptions) {
		t.Errorf("nil document: %v", err)
	}
	var empty *text.Fonts
	if _, err := Convert(context.Background(), two(), empty, office.Options{}); !errors.Is(err, typeset.ErrInvalidOptions) {
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
// alone: Excel prints it, filled or bordered; Calc prints it when a cell
// draws a border and reaches the print range, which a visible style only
// does below the last value of its column, the first row standing for it
// in a column without one.
//
// # Corpus evidence
//
//   - poi/xlsx: 61605.xlsx prints a second page holding a yellow bordered
//     cell without a value; 53798.xlsx and 60384.xlsx alike.
//   - probe, ods: see [grid.empty] and [grid.usedRange].
func TestPlan_ExcelPrintsFormattedPages(t *testing.T) {
	filled := &Style{Fill: Fill{Color: css.Color{R: 255, G: 255, A: 255}}}
	bordered := &Style{Border: [4]Border{{Line: LineSolid, Width: 1}}}
	for _, tc := range []struct {
		name  string
		row   int
		st    *Style
		excel bool
		want  int
	}{
		{"Calc, a fill", 1, filled, false, 1},
		{"Excel, a fill", 1, filled, true, 2},
		{"Calc, a border", 1, bordered, false, 2},
		{"Calc, a border in the first row", 0, bordered, false, 1},
		{"Excel, a border in the first row", 0, bordered, true, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sh := &Sheet{Name: "S", DefaultColumnWidth: 100, Rows: []Row{{Index: 0, Cells: []Cell{numCell(0, 1, nil)}}}}
			if tc.row == 0 {
				sh.Rows[0].Cells = append(sh.Rows[0].Cells, Cell{Column: 8, Style: tc.st})
			} else {
				sh.Rows = append(sh.Rows, Row{Index: tc.row, Cells: []Cell{{Column: 8, Style: tc.st}}})
			}
			doc := newDoc(sh)
			doc.ExcelRules = tc.excel
			if n := len(convert(t, doc, office.Options{}).Pages); n != tc.want {
				t.Errorf("%d pages, want %d", n, tc.want)
			}
		})
	}
}

// TestPlan_ExcelPrintsMergedPages locks the pages of a merged cell that a
// manual break cuts: under Excel's rules the page of its lower rows
// prints, though no cell of it holds a value; Calc prints one page.
func TestPlan_ExcelPrintsMergedPages(t *testing.T) {
	for _, tc := range []struct {
		excel bool
		order PageOrder
		want  int
	}{{false, DownThenOver, 1}, {true, DownThenOver, 2}, {true, OverThenDown, 2}} {
		sh := &Sheet{Name: "S", Page: a4(), Rows: []Row{{Index: 14, Cells: []Cell{numCell(0, 1, nil)}}}, Merges: []Range{{FirstRow: 14, LastRow: 17, FirstColumn: 0, LastColumn: 1}}}
		sh.Page.RowBreaks = []int{16}
		sh.Page.Order = tc.order
		doc := newDoc(sh)
		doc.ExcelRules = tc.excel
		if n := len(convert(t, doc, office.Options{}).Pages); n != tc.want {
			t.Errorf("Excel's rules %v, order %v: %d pages, want %d", tc.excel, tc.order, n, tc.want)
		}
	}
}

// TestPlan_ExcelColumnCap locks Excel's bound on a column's width: a
// column wider than the page prints as wide as the page, at the sheet's
// scale, and a text overflows from it into the columns after it; Calc
// keeps the width.
func TestPlan_ExcelColumnCap(t *testing.T) {
	for _, tc := range []struct {
		name  string
		excel bool
		scale int
		want  float64
		pages int
	}{
		{"Calc", false, 100, 999.98, 2},
		{"Excel", true, 100, 523.32, 3},
		{"Excel at 50%", true, 50, 1000, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sh := &Sheet{Name: "S", Page: a4(), Columns: []Column{{First: 0, Last: 0, Width: 1000}, {First: 1, Last: 1, Width: 600}}}
			sh.Page.Scale = tc.scale
			sh.Rows = []Row{{Index: 0, Cells: []Cell{textCell(0, strings.Repeat("x", 220), nil)}}}
			doc := newDoc(sh)
			doc.ExcelRules = tc.excel
			b := newBuilder(context.Background(), doc, pinnedFonts(t), office.Options{})
			sp := b.planOf(t, 0, sh)
			if got := sp.g.colWidth(0); math.Abs(got-tc.want) > 0.01 {
				t.Errorf("column A prints %.2f wide, want %.2f", got, tc.want)
			}
			if n := len(sp.pages); n != tc.pages {
				t.Errorf("%d pages, want %d", n, tc.pages)
			}
		})
	}
}

// TestPlan_ExcelPrintsBlankPages locks the pages of a range Excel prints:
// every page in the page order up to the last that holds something, the
// blank ones between included, where Calc prints the pages that hold
// something alone; and a page of repeated rows that hold a value holds
// something.
func TestPlan_ExcelPrintsBlankPages(t *testing.T) {
	type page struct{ rows, cols [2]int }
	tests := []struct {
		name  string
		excel bool
		order PageOrder
		rep   bool
		want  []page
	}{
		{"Calc", false, DownThenOver, false, []page{{[2]int{0, 59}, [2]int{0, 9}}, {[2]int{120, 149}, [2]int{0, 9}}, {[2]int{0, 59}, [2]int{10, 15}}}},
		{"Excel, down then over", true, DownThenOver, false, []page{
			{[2]int{0, 59}, [2]int{0, 9}}, {[2]int{60, 119}, [2]int{0, 9}}, {[2]int{120, 149}, [2]int{0, 9}}, {[2]int{0, 59}, [2]int{10, 15}},
		}},
		{"Excel, over then down", true, OverThenDown, false, []page{
			{[2]int{0, 59}, [2]int{0, 9}}, {[2]int{0, 59}, [2]int{10, 15}}, {[2]int{60, 119}, [2]int{0, 9}}, {[2]int{60, 119}, [2]int{10, 15}}, {[2]int{120, 149}, [2]int{0, 9}},
		}},
		{"Excel, repeated rows", true, DownThenOver, true, []page{
			{[2]int{0, 59}, [2]int{0, 9}}, {[2]int{60, 118}, [2]int{0, 9}}, {[2]int{119, 149}, [2]int{0, 9}},
			{[2]int{0, 59}, [2]int{10, 15}}, {[2]int{60, 118}, [2]int{10, 15}}, {[2]int{119, 149}, [2]int{10, 15}},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sh := &Sheet{Name: "S", DefaultColumnWidth: 50, DefaultRowHeight: 12.8, Page: a4()}
			sh.Page.Order = tt.order
			sh.Rows = []Row{
				{Index: 0, Cells: []Cell{numCell(0, 1, nil), numCell(15, 1, nil)}},
				{Index: 149, Cells: []Cell{numCell(0, 1, nil)}},
			}
			if !tt.rep {
				sh.Rows[0].Cells = sh.Rows[0].Cells[:1]
				sh.Rows = append(sh.Rows[:1], Row{Index: 10, Cells: []Cell{numCell(15, 1, nil)}}, sh.Rows[1])
			} else {
				sh.Page.RepeatRows = &Range{FirstRow: 0, LastRow: 0}
			}
			doc := newDoc(sh)
			doc.ExcelRules = tt.excel
			b := newBuilder(context.Background(), doc, pinnedFonts(t), office.Options{})
			sp := b.planOf(t, 0, sh)
			var got []page
			for _, p := range sp.pages {
				got = append(got, page{[2]int{p.rows.first, p.rows.last}, [2]int{p.cols.first, p.cols.last}})
			}
			if fmt.Sprint(got) != fmt.Sprint(tt.want) {
				t.Errorf("pages %v, want %v", got, tt.want)
			}
		})
	}
}

// TestDrawingCells_ExcelEndCell locks the cells of a drawing that ends at
// a cell's top-left corner: Calc's rules stop before that cell, Excel's
// take it in.
func TestDrawingCells_ExcelEndCell(t *testing.T) {
	for _, tc := range []struct {
		excel        bool
		lastR, lastC int
	}{{false, 1, 2}, {true, 2, 3}} {
		sh := &Sheet{Name: "S", Drawings: []Drawing{{From: Anchor{}, To: &Anchor{Row: 2, Column: 3}}}}
		doc := newDoc(sh)
		doc.ExcelRules = tc.excel
		r, ok := newGrid(t.Context(), doc, sh).drawingCells(&sh.Drawings[0])
		if !ok || r.LastRow != tc.lastR || r.LastColumn != tc.lastC {
			t.Errorf("Excel's rules %v: cells %+v, want to row %d and column %d", tc.excel, r, tc.lastR, tc.lastC)
		}
	}
}

// TestDrawingCells_InCell locks the cells of a drawing that fills its
// cell, a sparkline: that cell alone under Excel's rules, though its
// anchors reach the next cell's corner.
func TestDrawingCells_InCell(t *testing.T) {
	sh := &Sheet{Name: "S", Drawings: []Drawing{{From: Anchor{Row: 4, Column: 6}, To: &Anchor{Row: 5, Column: 7}, InCell: true}}}
	doc := newDoc(sh)
	doc.ExcelRules = true
	r, ok := newGrid(t.Context(), doc, sh).drawingCells(&sh.Drawings[0])
	if want := (Range{FirstRow: 4, LastRow: 4, FirstColumn: 6, LastColumn: 6}); !ok || r != want {
		t.Errorf("cells %+v, want %+v", r, want)
	}
}

// TestConvert_FallbackPaper prints a sheet that names no paper on A4 when
// the conversion names no default paper, and on the conversion's default
// paper when it names one.
func TestConvert_FallbackPaper(t *testing.T) {
	doc := func() *Document {
		return &Document{Sheets: []*Sheet{{
			Name: "S", Page: PageSetup{Scale: 100},
			Rows: []Row{{Index: 0, Cells: []Cell{textCell(0, "a", nil)}}},
		}}}
	}
	for _, tt := range []struct {
		name string
		opts office.Options
		w, h float64
	}{
		{"none", office.Options{}, FallbackWidth, FallbackHeight},
		{"letter", office.Options{DefaultPageWidth: 612, DefaultPageHeight: 792}, 612, 792},
	} {
		t.Run(tt.name, func(t *testing.T) {
			out := convert(t, doc(), tt.opts)
			if pg := out.Pages[0]; math.Abs(pg.Width-tt.w) > 1e-9 || math.Abs(pg.Height-tt.h) > 1e-9 {
				t.Errorf("page %vx%v, want %vx%v", pg.Width, pg.Height, tt.w, tt.h)
			}
		})
	}
}

// TestConvert_ScaledHeaderRoom shrinks the header's room in the top
// margin with the print scale under Calc's rules, the cells starting
// higher, and keeps the margin whole under Excel's.
func TestConvert_ScaledHeaderRoom(t *testing.T) {
	y := map[bool]float64{}
	for _, excel := range []bool{false, true} {
		sh := &Sheet{Name: "S", Page: a4(), Rows: []Row{{Index: 0, Cells: []Cell{textCell(0, "cell", nil)}}}}
		sh.Page.Scale = 50
		sh.Page.HeaderTop, sh.Page.Margin[css.Top] = 21.6, 54
		sh.HeaderFooter.Odd.Header[1] = []Span{{Text: "H"}}
		doc := newDoc(sh)
		doc.ExcelRules = excel
		y[excel] = find(t, convert(t, doc, office.Options{}).Pages[0], "cell").y
	}
	if d := y[false] - y[true]; math.Abs(d-(54-21.6)*0.5) > 0.5 {
		t.Errorf("Excel's cells %.2f points below Calc's, want %.2f", d, (54-21.6)*0.5)
	}
}

// TestPlan_ExcelRowsAPage locks how many rows Excel prints a page: as
// many as fit the paper less its margins and the 8 printer pixels of the
// grid line below the last row, each row at a zoom other than 100 rounded
// to a whole printer pixel.
func TestPlan_ExcelRowsAPage(t *testing.T) {
	for _, tc := range []struct {
		name  string
		scale int
		// room is the paper's height less its margins, in printer pixels.
		room float64
		// hair is added to the bottom margin, in points.
		hair float64
		want int
	}{
		// 36 rows of 121 pixels take 4,356 of 4,361: the grid line's 8
		// leave room for 35.
		{"at 100%", 100, 4361, 0, 35},
		{"the grid line's room left", 100, 4364, 0, 36},
		// A bottom margin written to a millionth of an inch prints in whole
		// pixels: the room is still 4,364.
		{"a margin a hair past the pixel", 100, 4364, 3e-5, 36},
		// At 60% a row prints 73 pixels, 72.6 rounded: 77 take 5,621 of
		// the 5,692 the grid line leaves, 78 would take 5,694.
		{"at 60%", 60, 5700, 0, 77},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sh := &Sheet{Name: "S", DefaultColumnWidth: 50}
			for r := range 200 {
				sh.Rows = append(sh.Rows, Row{Index: r, Cells: []Cell{textCell(0, fmt.Sprint(r), nil)}})
			}
			doc := excelDoc(sh)
			sh.Page = PageSetup{Width: 500, Height: 72 + tc.room*excelPixel, Margin: [4]float64{36, 36, 36 + tc.hair, 36}, Scale: tc.scale}
			b := newBuilder(context.Background(), doc, pinnedFonts(t), office.Options{})
			sp := b.planOf(t, 0, sh)
			if got := sp.pages[0].rows.last - sp.pages[0].rows.first + 1; got != tc.want {
				t.Errorf("%d rows on the first page, want %d", got, tc.want)
			}
		})
	}
}

// TestConvert_ExcelPaper locks the paper Excel prints on: each side in
// whole printer pixels, the nearest, A4 on 4,961 by 7,016 pixels; Calc's
// paper stays as the sheet states it, and the options' paper as given.
func TestConvert_ExcelPaper(t *testing.T) {
	for _, excel := range []bool{false, true} {
		sh := &Sheet{Name: "S", Rows: []Row{{Index: 0, Cells: []Cell{textCell(0, "a", nil)}}}}
		doc := newDoc(sh)
		if excel {
			doc = excelDoc(sh)
		}
		pg := convert(t, doc, office.Options{}).Pages[0]
		w, h := 595.28, 841.89
		if excel {
			w, h = 4961*excelPixel, 7016*excelPixel
		}
		if !near(pg.Width, w, 1e-9) || !near(pg.Height, h, 1e-9) {
			t.Errorf("Excel's rules %v: paper %v by %v, want %v by %v", excel, pg.Width, pg.Height, w, h)
		}
		// A paper the options give is the caller's size, kept as given.
		pg = convert(t, doc, office.Options{PageWidth: 419.53, PageHeight: 595.28}).Pages[0]
		if !near(pg.Width, 419.53, 1e-9) || !near(pg.Height, 595.28, 1e-9) {
			t.Errorf("Excel's rules %v: the options' paper printed %v by %v, want 419.53 by 595.28", excel, pg.Width, pg.Height)
		}
	}
}
