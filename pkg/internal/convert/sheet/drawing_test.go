package sheet

import (
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/chart"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
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
