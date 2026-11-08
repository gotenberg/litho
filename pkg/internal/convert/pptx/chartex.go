package pptx

import (
	"github.com/gotenberg/litho/pkg/internal/chart"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
)

// chartExChoices are the namespaces of the mc:Choice of an Office 2016
// chart the reader takes over its Fallback picture (ECMA-376 Part 3,
// 10.2): none while [reader.chartExOf] reads no chart; once it does, the
// cx1 and cx2 namespaces PowerPoint's mc:AlternateContent requires
// ("http://schemas.microsoft.com/office/drawing/2015/9/8/chartex" and
// "http://schemas.microsoft.com/office/drawing/2015/10/21/chartex"), as
// Impress draws the chart, not the picture (chart2/funnel-pp1.pptx
// prints its funnel's title, legend and axis).
var chartExChoices []string

// chartExOf returns the Office 2016 chart (funnel, waterfall, sunburst,
// treemap, histogram, box and whisker) of a frame of part whose graphic
// data is data (MS-ODRAWXML, 2.24.3 cx:chart, its r:id naming the
// chartEx part), nil with a warning when it is not drawn. It draws none
// yet: the frame is left out, and a frame wrapped in PowerPoint's
// mc:AlternateContent prints its Fallback picture, chartExChoices being
// empty.
func (r *reader) chartExOf(_ string, _ *ooxml.Element) *chart.Chart {
	r.warn("unsupported-content", "an Office 2016 chart (funnel, waterfall, sunburst, treemap, histogram, box and whisker) without a fallback picture is left out")
	return nil
}
