package sheet

import (
	"context"
	"fmt"
	"image"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/chart"
	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/layout"
)

// tableWidths returns the columns each row of the Table tbl counts, the
// cells a rowspan above carries down included.
func tableWidths(tbl *compose.StructElem) []int {
	carry := map[int]int{}
	out := make([]int, len(tbl.Kids))
	for i, tr := range tbl.Kids {
		n := carry[i]
		for _, td := range tr.Kids {
			cs, rs := max(td.ColSpan, 1), max(td.RowSpan, 1)
			n += cs
			for k := 1; k < rs; k++ {
				carry[i+k] += cs
			}
		}
		out[i] = n
	}
	return out
}

// cellText returns the text of the fragments of pg whose parent is el or
// one of its kids.
func cellText(pg *compose.Page, el *compose.StructElem) string {
	in := map[*compose.StructElem]bool{el: true}
	for _, k := range el.Kids {
		in[k] = true
	}
	var sb strings.Builder
	var walk func([]compose.Fragment)
	walk = func(fs []compose.Fragment) {
		for _, f := range fs {
			switch v := f.(type) {
			case *compose.Group:
				walk(v.Content)
			case *compose.Text:
				if in[v.Parent] {
					for _, g := range v.Glyphs {
						sb.WriteString(g.Text)
					}
				}
			}
		}
	}
	walk(pg.Content)
	return sb.String()
}

// TestConvert_TableIsRegular locks the structure of a printed page: a
// Table of a TR for every printed row that holds content, holding a
// cell for every printed column, a merge one cell with its spans, a run
// of empty cells one cell spanning them, the
// repeated rows TH with a column scope, the headings artifacts and a
// cell's link inside its cell.
func TestConvert_TableIsRegular(t *testing.T) {
	sh := &Sheet{Name: "S", Rows: []Row{
		{Index: 0, Cells: []Cell{textCell(0, "Head", nil), textCell(2, "H2", nil)}},
		{Index: 1, Cells: []Cell{textCell(1, "b", nil)}},
		{Index: 3, Cells: []Cell{textCell(0, "merged", nil), {Column: 3, Value: Value{Kind: Text, Text: "link"}, Href: "https://example.com/"}}},
	}}
	sh.Merges = []Range{{FirstRow: 3, LastRow: 4, FirstColumn: 0, LastColumn: 1}}
	doc := newDoc(sh)
	sh.Page.Headings = true
	sh.Page.RepeatRows = &Range{FirstRow: 0, LastRow: 0}
	out := convert(t, doc, office.Options{})
	if len(out.Structure.Kids) != 1 || out.Structure.Kids[0].Role != "Table" {
		t.Fatalf("structure %+v", out.Structure.Kids)
	}
	tbl := out.Structure.Kids[0]
	// The empty third row is left out.
	if len(tbl.Kids) != 4 {
		t.Fatalf("%d rows, want 4", len(tbl.Kids))
	}
	for i, w := range tableWidths(tbl) {
		if w != 4 {
			t.Errorf("row %d counts %d columns, want 4", i, w)
		}
	}
	for _, th := range tbl.Kids[0].Kids {
		if th.Role != "TH" || th.Scope != compose.ScopeColumn {
			t.Errorf("first row cell %s scope %d, want TH column", th.Role, th.Scope)
		}
	}
	pg := out.Pages[0]
	if got := cellText(pg, tbl.Kids[0].Kids[0]); got != "Head" {
		t.Errorf("A1 holds %q", got)
	}
	m := tbl.Kids[2].Kids[0]
	if m.Role != "TD" || m.ColSpan != 2 || m.RowSpan != 2 || cellText(pg, m) != "merged" {
		t.Errorf("merge %s %dx%d %q", m.Role, m.ColSpan, m.RowSpan, cellText(pg, m))
	}
	// The empty cells of a row are one cell spanning them.
	if k := tbl.Kids[3].Kids; len(k) != 1 || k[0].ColSpan != 2 {
		t.Errorf("row under the merge holds %d cells, want 1 of two columns", len(k))
	}
	l := tbl.Kids[2].Kids[2]
	if len(l.Kids) != 1 || l.Kids[0].Role != "Link" || len(pg.Links) != 1 || pg.Links[0].Parent != l.Kids[0] {
		t.Errorf("link cell %+v, links %+v", l, pg.Links)
	}
	// The headings print "A" and "1": no structure holds them.
	var walk func([]compose.Fragment)
	walk = func(fs []compose.Fragment) {
		for _, f := range fs {
			switch v := f.(type) {
			case *compose.Group:
				walk(v.Content)
			case *compose.Text:
				if v.Parent == tbl {
					t.Error("text hangs from the Table")
				}
			}
		}
	}
	walk(pg.Content)
}

// TestConvert_TableDropsRowsAMergeCovers locks that a row every printed
// column of which continues a merge from the row above is left out, the
// merges spanning one row fewer, so that no TR is left without a cell.
//
// # Corpus evidence
//
//   - litho/xlsx: real_dashboard's title merge over two rows and every
//     printed column left its second row a TR without a cell, which
//     veraPDF counts as no column.
func TestConvert_TableDropsRowsAMergeCovers(t *testing.T) {
	sh := &Sheet{Name: "S", Rows: []Row{
		{Index: 0, Cells: []Cell{textCell(0, "Title", nil)}},
		{Index: 2, Cells: []Cell{textCell(0, "a", nil), textCell(1, "b", nil), textCell(2, "c", nil)}},
	}}
	sh.Merges = []Range{{FirstRow: 0, LastRow: 1, FirstColumn: 0, LastColumn: 2}}
	out := convert(t, newDoc(sh), office.Options{})
	tbl := out.Structure.Kids[0]
	if len(tbl.Kids) != 2 {
		t.Fatalf("%d rows, want 2", len(tbl.Kids))
	}
	if m := tbl.Kids[0].Kids; len(m) != 1 || m[0].ColSpan != 3 || m[0].RowSpan != 0 {
		t.Errorf("title row %+v, want one cell of three columns and one row", m)
	}
	for i, w := range tableWidths(tbl) {
		if w != 3 {
			t.Errorf("row %d counts %d columns, want 3", i, w)
		}
	}
}

// TestConvert_TableRepeatsTitlesPerPage locks that every page of a sheet
// cut over pages is its own regular Table, its repeated rows first.
func TestConvert_TableRepeatsTitlesPerPage(t *testing.T) {
	sh := grid150(a4())
	sh.Page.RepeatRows = &Range{FirstRow: 0, LastRow: 1}
	out := convert(t, newDoc(sh), office.Options{})
	if len(out.Pages) < 4 || len(out.Structure.Kids) != len(out.Pages) {
		t.Fatalf("%d pages, %d tables", len(out.Pages), len(out.Structure.Kids))
	}
	for p, tbl := range out.Structure.Kids {
		ws := tableWidths(tbl)
		for i, w := range ws {
			if w != ws[0] {
				t.Fatalf("page %d row %d counts %d columns, row 0 %d", p+1, i, w, ws[0])
			}
		}
		if got := cellText(out.Pages[p], tbl.Kids[0].Kids[0]); !strings.HasPrefix(got, "r0c") {
			t.Errorf("page %d starts with %q", p+1, got)
		}
		if tbl.Kids[2].Kids[0].Role != "TD" || tbl.Kids[1].Kids[0].Role != "TH" {
			t.Errorf("page %d: titles not TH", p+1)
		}
	}
	// Page 2 continues the rows of page 1.
	if got, want := cellText(out.Pages[1], out.Structure.Kids[1].Kids[2].Kids[0]), fmt.Sprintf("r%dc0", len(out.Structure.Kids[0].Kids)); got != want {
		t.Errorf("page 2 first band row %q, want %q", got, want)
	}
}

// TestConvert_DestinationsNameTheTable locks that the outline entry of a
// sheet and a link into the book name the Table of the page they show as
// their structure destination (ISO 14289-2, 8.8).
func TestConvert_DestinationsNameTheTable(t *testing.T) {
	a := &Sheet{Name: "A", Rows: []Row{{Index: 0, Cells: []Cell{{Column: 0, Value: Value{Kind: Text, Text: "to B"}, Href: "#B!A1"}}}}}
	bs := &Sheet{Name: "B", Rows: []Row{{Index: 0, Cells: []Cell{textCell(0, "b", nil)}}}}
	doc := newDoc(a)
	newDoc(bs)
	doc.Sheets = append(doc.Sheets, bs)
	out := convert(t, doc, office.Options{})
	if len(out.Pages) != 2 || len(out.Outline) != 2 || len(out.Structure.Kids) != 2 {
		t.Fatalf("%d pages, %d outline entries, %d tables", len(out.Pages), len(out.Outline), len(out.Structure.Kids))
	}
	for i, o := range out.Outline {
		if o.Dest.Elem != out.Structure.Kids[i] {
			t.Errorf("outline entry %d names %+v", i, o.Dest.Elem)
		}
	}
	if l := out.Pages[0].Links; len(l) != 1 || l[0].Dest == nil || l[0].Dest.Elem != out.Structure.Kids[1] {
		t.Errorf("links %+v", l)
	}
}

// TestConvert_DrawingsAreFigures locks how a sheet's drawings read: a
// picture is a Figure carrying its description, a chart without one its
// title, a decorative picture and an undescribed shape artifacts, and
// the outline entry of a
// page that prints only drawings names its first Figure (ISO 14289-1,
// 7.3; ISO 14289-2, 8.8).
func TestConvert_DrawingsAreFigures(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	pic := &layout.Picture{Source: source(t, img), Width: 2, Height: 2}
	box := &layout.Picture{Width: 2, Height: 2, Vector: func(_ context.Context, r compose.Rect) []compose.Fragment {
		return []compose.Fragment{&compose.Path{Ops: rectOps(r.X, r.Y, r.Width, r.Height), Fill: &compose.Fill{}}}
	}}
	sh := &Sheet{
		Name: "S",
		Drawings: []Drawing{
			{From: Anchor{Row: 2, Column: 3}, Width: 20, Height: 20, Picture: box, Name: "rect", Shape: true},
			{From: Anchor{Row: 0, Column: 0}, Width: 40, Height: 20, Picture: pic, Name: "shown", Alt: "A red square"},
			{From: Anchor{Row: 3, Column: 0}, Width: 40, Height: 20, Picture: pic, Name: "border", Decorative: true},
			{From: Anchor{Row: 6, Column: 0}, Width: 200, Height: 120, Chart: &chart.Chart{Title: &chart.Title{Text: "Sales\n 2026"}}, Name: "chart"},
		},
	}
	out := convert(t, newDoc(sh), office.Options{})
	if len(out.Pages) != 1 {
		t.Fatalf("%d pages, want 1", len(out.Pages))
	}
	var figs []*compose.StructElem
	for _, k := range out.Structure.Kids {
		if k.Role == "Figure" {
			figs = append(figs, k)
		}
	}
	if len(figs) != 2 || figs[0].Alt != "A red square" || figs[1].Alt != "Sales 2026" {
		t.Fatalf("figures %+v, want the picture's and the chart's", figs)
	}
	var tagged, artifacts, shapes int
	var walk func([]compose.Fragment)
	walk = func(fs []compose.Fragment) {
		for _, f := range fs {
			switch v := f.(type) {
			case *compose.Image:
				if v.Parent == figs[0] {
					tagged++
				} else if v.Parent == nil {
					artifacts++
				}
			case *compose.Path:
				if v.Fill != nil && v.Parent == nil && len(v.Ops) == 5 {
					shapes++
				}
			case *compose.Group:
				walk(v.Content)
			}
		}
	}
	walk(out.Pages[0].Content)
	if tagged != 1 || artifacts != 1 || shapes == 0 {
		t.Errorf("%d pictures in the Figure, %d artifacts, %d shape paths untagged, want 1, 1 and some", tagged, artifacts, shapes)
	}
	if len(out.Outline) != 1 || out.Outline[0].Dest.Elem != figs[0] {
		t.Errorf("outline %+v, want an entry naming the first Figure", out.Outline)
	}
}
