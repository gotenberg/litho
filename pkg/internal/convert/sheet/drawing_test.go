package sheet

import (
	"context"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/chart"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// TestConvert_SplitChartText locks the text of a chart that crosses a page
// break: under Excel's rules each page writes the chart's text that
// reaches it, so the title at the top of the chart is written on the first
// page alone; under Calc's every page the chart crosses writes all of it,
// under the clip.
//
// # Corpus evidence
//
//   - libreoffice/xlsx: chart2/fdo54361.xlsx, a chart cut by the right
//     edge of page 1: Excel writes the categories 2008 to 2011 and the
//     value axis labels on page 1, 2013 and 2014 on page 2, and the cut
//     2012 on both.
//   - probe, xlsx: Calc, printing chart2/external_str_ref.xlsx and
//     chart2/chart-text-can-overlap.xlsx, writes every label and title of
//     the chart on both pages.
func TestConvert_SplitChartText(t *testing.T) {
	for _, excel := range []bool{true, false} {
		sh := &Sheet{Name: "S", Drawings: []Drawing{{
			From: Anchor{}, Width: 300, Height: 1200, Name: "Chart 1",
			Chart: &chart.Chart{Title: &chart.Title{Text: "Sales", Style: chart.TextStyle{Size: 14}}},
		}}}
		doc := newDoc(sh)
		doc.ExcelRules = excel
		out := convert(t, doc, office.Options{})
		if len(out.Pages) != 2 {
			t.Fatalf("excel %v: %d pages, want 2", excel, len(out.Pages))
		}
		first, second := slices.Contains(texts(out.Pages[0]), "Sales"), slices.Contains(texts(out.Pages[1]), "Sales")
		if !first || second != !excel {
			t.Errorf("excel %v: title on page 1 %v, page 2 %v", excel, first, second)
		}
	}
}

// TestConvert_LineDrawings locks that a shape of no height or no width, a
// line along a row or a column, is drawn, and one of neither is not; a
// picture of no width shows nothing and is not drawn.
func TestConvert_LineDrawings(t *testing.T) {
	for _, tc := range []struct {
		w, h  float64
		shape bool
		want  bool
	}{{100, 0, true, true}, {0, 100, true, true}, {0, 0, true, false}, {0, 100, false, false}} {
		drawn := false
		pic := &layout.Picture{Width: 1, Height: 1, Vector: func(context.Context, typeset.Rect) []typeset.Fragment {
			drawn = true
			return nil
		}}
		sh := &Sheet{Name: "S", Rows: []Row{{Index: 0, Cells: []Cell{textCell(0, "cell", nil)}}}, Drawings: []Drawing{{
			From: Anchor{Row: 1}, Width: tc.w, Height: tc.h, Name: "Line", Picture: pic, Shape: tc.shape,
		}}}
		convert(t, excelDoc(sh), office.Options{})
		if drawn != tc.want {
			t.Errorf("a drawing %v by %v, shape %v, drawn %v, want %v", tc.w, tc.h, tc.shape, drawn, tc.want)
		}
	}
}

// TestConvert_DrawingLinks locks the link a drawing carries: one link
// annotation over the drawing's box on each page it prints on, each in a
// Link element, the first holding the drawing's Figure.
func TestConvert_DrawingLinks(t *testing.T) {
	sh := &Sheet{Name: "S", Drawings: []Drawing{{
		From: Anchor{}, Width: 300, Height: 1200, Name: "Chart 1", Href: "https://example.com/",
		Chart: &chart.Chart{Title: &chart.Title{Text: "Sales", Style: chart.TextStyle{Size: 14}}},
	}}}
	out := convert(t, excelDoc(sh), office.Options{})
	if len(out.Pages) != 2 {
		t.Fatalf("%d pages, want 2", len(out.Pages))
	}
	for i, pg := range out.Pages {
		if len(pg.Links) != 1 {
			t.Fatalf("page %d: %d links, want 1", i+1, len(pg.Links))
		}
		l := pg.Links[0]
		if l.URI != "https://example.com/" || l.Rect.Width < 200 || l.Rect.Height < 200 {
			t.Errorf("page %d: link %+v", i+1, l)
		}
		if l.Parent == nil || l.Parent.Role != "Link" {
			t.Errorf("page %d: link parent %+v, want a Link", i+1, l.Parent)
		} else if i == 0 && (len(l.Parent.Kids) != 1 || l.Parent.Kids[0].Role != "Figure") {
			t.Errorf("page 1: link kids %+v, want the Figure", l.Parent.Kids)
		}
	}
}

// TestConvert_ShapeTextOnThePagesItShows locks that a shape cut by a page
// break writes on each page only the text that shows there: a run under a
// clip that ends before the break is written on the first page alone, a
// run at the shape's foot on the second alone. A run the page's clip
// hides draws nothing, and a reader of that page must not find it.
//
// # Corpus evidence
//
//   - poi/xlsx spreadsheet/56514.xlsx: a text box crossing the right edge
//     of page 1 wrote its last letter, "t", on page 2 outside the cells
//     the page shows; Excel's page 2 holds no text.
//   - litho/xlsx groups_connectors: the "A" and "End" of shapes on page 1
//     were written again on page 2, where Excel writes "B" and "Grouped
//     shapes" alone.
func TestConvert_ShapeTextOnThePagesItShows(t *testing.T) {
	run := func(s string, x, y float64) *typeset.Text {
		tx := &typeset.Text{Size: 12, Color: typeset.Black, Origin: typeset.Point{X: x, Y: y}}
		for i, r := range s {
			tx.Glyphs = append(tx.Glyphs, typeset.Glyph{GID: 1, X: float64(i) * 6, Text: string(r)})
		}
		return tx
	}
	pic := &layout.Picture{Width: 1, Height: 1, Vector: func(_ context.Context, r typeset.Rect) []typeset.Fragment {
		return []typeset.Fragment{
			&typeset.Group{
				Transform: typeset.Identity, Opacity: 1,
				Clip:    &typeset.Path{Ops: rectOps(r.X, r.Y, r.Width, 100), Fill: &typeset.Fill{}},
				Content: []typeset.Fragment{run("Head", r.X+10, r.Y+50)},
			},
			run("Foot", r.X+10, r.Y+r.Height-20),
		}
	}}
	sh := &Sheet{Name: "S", Drawings: []Drawing{{
		From: Anchor{}, Width: 300, Height: 1200, Name: "Box", Alt: "Box", Picture: pic, Shape: true,
	}}}
	out := convert(t, excelDoc(sh), office.Options{})
	if len(out.Pages) != 2 {
		t.Fatalf("%d pages, want 2", len(out.Pages))
	}
	for i, want := range [][]string{{"Head"}, {"Foot"}} {
		if got := texts(out.Pages[i]); !slices.Equal(got, want) {
			t.Errorf("page %d: text %q, want %q", i+1, got, want)
		}
	}
}
