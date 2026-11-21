package sheet

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// islandBook returns a book of one sheet of 50 by 12.8 point cells on A4,
// 60 rows and 10 columns a page, holding "first" in A1, linked to A150,
// "last" in A150 and "far" in P11, its header counting the pages. Excel
// prints it on four pages down then over, the second blank; Calc on the
// three that hold a cell.
func islandBook(excel bool) *Document {
	sh := &Sheet{Name: "S", DefaultColumnWidth: 50, DefaultRowHeight: 12.8, Page: a4()}
	first := textCell(0, "first", nil)
	first.Href = "#S!A150"
	sh.Rows = []Row{
		{Index: 0, Cells: []Cell{first}},
		{Index: 10, Cells: []Cell{textCell(15, "far", nil)}},
		{Index: 149, Cells: []Cell{textCell(0, "last", nil)}},
	}
	sh.HeaderFooter.Odd.Header[1] = []Span{{Text: "P:"}, {Field: FieldPage}, {Text: "/"}, {Field: FieldPages}}
	doc := newDoc(sh)
	doc.ExcelRules = excel
	return doc
}

// pageTexts returns the text of each page, its runs joined by spaces.
func pageTexts(out *typeset.Document) []string {
	var texts []string
	for _, pg := range out.Pages {
		var words []string
		for _, r := range runs(pg) {
			words = append(words, r.text)
		}
		texts = append(texts, strings.Join(words, " "))
	}
	return texts
}

// TestConvert_BlankPagesPastTheLimit locks what a book prints when its
// pages by Excel's rule pass the page cap: the pages that hold something,
// numbered and counted among themselves as Calc numbers the pages it
// prints when it leaves empty ones out, with one warning saying how many
// were left out; the refusal when those pages pass the cap by
// themselves; and every page, blank ones included, under the cap. A
// link into the book goes to the page of its cell. Calc prints the same
// pages at every cap, and a cap under them refuses the book.
//
// # Corpus evidence
//
//   - probe, xlsx and ods: B2 and GT3002 of a sheet whose header shows
//     "Page &P of &N", converted by Gotenberg 9.4.1, print two A4 pages,
//     "Page 1 of 2" over B2 and "Page 2 of 2" over GT3002, as XLSX and as
//     ODS: Calc leaves out the pages that hold nothing
//     (ScPrintFunc::CountPages and DoPrint count only the pages
//     lcl_SetHidden keeps, sc/source/ui/view/printfun.cxx).
//   - openxmlsdk/xlsx: FilterByCF.xlsx, ten values in FOO100000:FOO100009,
//     prints about a million pages by Excel's rule; Excel for the web
//     refuses it (XLSPageLimitExceeded) and Gotenberg 9.4.1 prints its one
//     Letter page of values.
func TestConvert_BlankPagesPastTheLimit(t *testing.T) {
	tests := []struct {
		name     string
		excel    bool
		maxPages int
		// firstPage is the sheet's first page number, 0 for none.
		firstPage int
		want      []string
		warning   string
		refused   bool
	}{
		{"Excel under the cap", true, 4, 0, []string{"P: 1 / 4 first", "P: 2 / 4", "P: 3 / 4 last", "P: 4 / 4 far"}, "", false},
		{"Excel past the cap", true, 3, 0, []string{"P: 1 / 3 first", "P: 2 / 3 last", "P: 3 / 3 far"},
			"the book prints 4 pages by Excel's rule, more than 3; the 1 that hold nothing are left out", false},
		{"Excel past the cap from page 5", true, 3, 5, []string{"P: 5 / 3 first", "P: 6 / 3 last", "P: 7 / 3 far"},
			"the book prints 4 pages by Excel's rule, more than 3; the 1 that hold nothing are left out", false},
		{"Excel's pages that hold something past the cap", true, 2, 0, nil, "", true},
		{"Calc", false, 3, 0, []string{"P: 1 / 3 first", "P: 2 / 3 last", "P: 3 / 3 far"}, "", false},
		{"Calc past the cap", false, 2, 0, nil, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := islandBook(tt.excel)
			doc.Sheets[0].Page.FirstPageNumber = tt.firstPage
			out, err := Convert(context.Background(), doc, pinnedFonts(t), office.Options{MaxPages: tt.maxPages})
			if tt.refused {
				if !errors.Is(err, limits.ErrTooManyPages) {
					t.Fatalf("Convert = %v, want limits.ErrTooManyPages", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := pageTexts(out); !slices.Equal(got, tt.want) {
				t.Errorf("pages %q, want %q", got, tt.want)
			}
			var got []string
			for _, w := range out.Warnings {
				if w.Code == warn.LimitReached {
					got = append(got, w.Message)
					if w.Effect != warn.Omitted {
						t.Errorf("effect %v, want omitted", w.Effect)
					}
				}
			}
			if want := []string{tt.warning}; tt.warning == "" && len(got) > 0 || tt.warning != "" && !slices.Equal(got, want) {
				t.Errorf("warnings %q, want %q", got, tt.warning)
			}
			var dests []int
			for _, l := range out.Pages[0].Links {
				if l.Dest != nil {
					dests = append(dests, l.Dest.Page)
				}
			}
			last := slices.IndexFunc(tt.want, func(s string) bool { return strings.HasSuffix(s, " last") }) + 1
			if !slices.Equal(dests, []int{last}) {
				t.Errorf("link to A150 goes to pages %v, want %d", dests, last)
			}
			if len(out.Outline) != 1 || out.Outline[0].Dest.Page != 1 {
				t.Errorf("outline %+v, want S on page 1", out.Outline)
			}
		})
	}
}

// TestEmit_ContentPages locks the pages that hold something, kept when
// blank ones are left out: a page whose own cells hold a value, text
// overflowing from the left or a drawing, in the sheet's page order. A
// page whose repeated rows hold a value and whose own cells hold none
// holds nothing, as Calc judges a page by its own range (lcl_SetHidden
// calls ScDocument::IsPrintEmpty on each page's own rows and columns,
// sc/source/ui/view/printfun.cxx), though Excel prints it.
func TestEmit_ContentPages(t *testing.T) {
	sh := &Sheet{Name: "S", DefaultColumnWidth: 50, DefaultRowHeight: 12.8, Page: a4()}
	sh.Page.Order = OverThenDown
	sh.Page.RepeatRows = &Range{FirstRow: 0, LastRow: 0}
	sh.Rows = []Row{
		{Index: 0, Cells: []Cell{textCell(0, "first", nil)}},
		{Index: 69, Cells: []Cell{textCell(9, "a text that overflows two columns", nil)}},
	}
	sh.Drawings = []Drawing{{From: Anchor{Row: 139, Column: 10}, To: &Anchor{Row: 141, Column: 11}, Name: "box"}}
	doc := newDoc(sh)
	doc.ExcelRules = true
	type page struct{ rows, cols [2]int }
	for _, sparse := range []bool{false, true} {
		b := newBuilder(context.Background(), doc, pinnedFonts(t), office.Options{})
		sp, err := b.planSheet(0, sh)
		if err != nil {
			t.Fatal(err)
		}
		held, err := b.emitSheet(sp, sparse)
		if err != nil {
			t.Fatal(err)
		}
		var got []page
		for _, p := range sp.pages {
			got = append(got, page{[2]int{p.rows.first, p.rows.last}, [2]int{p.cols.first, p.cols.last}})
		}
		want := []page{
			{[2]int{0, 59}, [2]int{0, 9}}, {[2]int{0, 59}, [2]int{10, 11}},
			{[2]int{60, 118}, [2]int{0, 9}}, {[2]int{60, 118}, [2]int{10, 11}},
			{[2]int{119, 141}, [2]int{0, 9}}, {[2]int{119, 141}, [2]int{10, 11}},
		}
		if sparse {
			want = []page{want[0], want[2], want[3], want[5]}
		}
		if fmt.Sprint(got) != fmt.Sprint(want) || held != int64(len(want)) || sp.full != 6 {
			t.Errorf("sparse %v: pages %v, %d held of %d, want %v", sparse, got, held, sp.full, want)
		}
	}
}

// TestCount_Corners locks the count of the pages Excel prints at the
// corners of the format: the last cell XFD1048576, a print area of the
// whole sheet, a page so small that every row and every column is a band
// of its own (2^34 pages, past a 32-bit int), columns of no width and a
// scale of 10 percent. The count is arithmetic, so planning visits the
// bands and never the pages, and leaving the blank pages out keeps the
// cells' pages alone.
func TestCount_Corners(t *testing.T) {
	tests := []struct {
		name string
		edit func(sh *Sheet)
		full int64
		kept int
	}{
		{"last cell", func(sh *Sheet) {}, 0, 2},
		{"print area of the whole sheet", func(sh *Sheet) {
			sh.Page.PrintAreas = []Range{{LastRow: maxRows - 1, LastColumn: maxColumns - 1}}
		}, 0, 2},
		{"a band a row and a column", func(sh *Sheet) {
			sh.Page.Width, sh.Page.Height, sh.Page.Margin = 20, 20, [4]float64{}
		}, 1 << 34, 2},
		{"columns of no width", func(sh *Sheet) { sh.DefaultColumnWidth = 0 }, 0, 2},
		{"scale of 10 percent", func(sh *Sheet) { sh.Page.Scale = 10 }, 0, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := farCorner(true)
			tt.edit(doc.Sheets[0])
			b := newBuilder(context.Background(), doc, pinnedFonts(t), office.Options{})
			sp, err := b.planSheet(0, doc.Sheets[0])
			if err != nil {
				t.Fatal(err)
			}
			c := sp.cuts[0]
			// Every page of the range prints, down then over, to the last.
			if want := int64(len(c.rows)) * int64(len(c.cols)); sp.full != want || tt.full != 0 && sp.full != tt.full {
				t.Errorf("%d pages of %d by %d bands, want %d", sp.full, len(c.rows), len(c.cols), max(want, tt.full))
			}
			held, err := b.emitSheet(sp, true)
			if err != nil {
				t.Fatal(err)
			}
			if held != int64(tt.kept) || len(sp.pages) != tt.kept {
				t.Errorf("%d pages kept, %d held, want %d", len(sp.pages), held, tt.kept)
			}
		})
	}
}

// farCorner returns a book of Excel's rules whose one sheet holds a value
// at A1 when first is set and one at XFD1048576, the format's last cell.
func farCorner(first bool) *Document {
	sh := &Sheet{Name: "S", DefaultColumnWidth: 48, DefaultRowHeight: 15}
	if first {
		sh.Rows = append(sh.Rows, Row{Index: 0, Cells: []Cell{textCell(0, "first", nil)}})
	}
	sh.Rows = append(sh.Rows, Row{Index: maxRows - 1, Cells: []Cell{textCell(maxColumns-1, "last", nil)}})
	doc := newDoc(sh)
	doc.ExcelRules = true
	return doc
}

func BenchmarkConvert_FarCorner(b *testing.B) {
	doc := farCorner(true)
	b.ReportAllocs()
	for b.Loop() {
		convert(b, doc, office.Options{})
	}
}
