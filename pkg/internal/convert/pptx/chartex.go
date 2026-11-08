package pptx

import (
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/chart"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
)

// chartExChoices are the namespaces of the mc:Choice of an Office 2016
// chart the reader takes over its Fallback picture (ECMA-376 Part 3,
// 10.2): the cx1 and cx2 namespaces PowerPoint's mc:AlternateContent
// requires, as Impress draws the chart, not the picture (chart2/funnel-pp1
// prints its funnel's title, legend and axis).
//
//nolint:gochecknoglobals // static list of namespaces
var chartExChoices = []string{
	"http://schemas.microsoft.com/office/drawing/2015/9/8/chartex",
	"http://schemas.microsoft.com/office/drawing/2015/10/21/chartex",
}

// chartExOf returns the Office 2016 chart (funnel, waterfall, sunburst,
// treemap, histogram, box and whisker) of a frame of part whose graphic
// data is data (MS-ODRAWXML, 2.24.3 cx:chart, its r:id naming the chartEx
// part), read by [ooxml.ParseChartEx]; nil with a warning when the part
// is missing or malformed, the frame then printing nothing. The chart's
// own warnings join the reader's.
func (r *reader) chartExOf(part string, data *ooxml.Element) *chart.Chart {
	id, _ := data.Child(ooxml.NSChartEx, "chart").AttrNS(ooxml.NSR, "id")
	rel, ok := r.pkg.RelationshipByID(part, id)
	if !ok || rel.External || !r.pkg.Has(rel.Target) {
		r.warn("unsupported-content", "an Office 2016 chart (funnel, waterfall, sunburst, treemap, histogram, box and whisker) without its chart part is left out")
		return nil
	}
	ch, err := ooxml.ParseChartEx(r.ctx, r.pkg, rel.Target)
	if err != nil {
		r.warn("unsupported-content", fmt.Sprintf("the Office 2016 chart part %q is left out: %v", rel.Target, err))
		return nil
	}
	for _, w := range ch.Warnings {
		r.warn(w.Code, w.Message)
	}
	return ch
}
