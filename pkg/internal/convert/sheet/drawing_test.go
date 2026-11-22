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
