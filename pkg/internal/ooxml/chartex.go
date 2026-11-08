package ooxml

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/chart"
	"github.com/gotenberg/litho/pkg/internal/compose"
)

// NSChartEx is the namespace of the charts Office 2016 added (MS-ODRAWXML,
// 2.24.3 chartex: histograms, Pareto, box and whisker, funnel, waterfall,
// sunburst, treemap and region map charts).
const NSChartEx = "http://schemas.microsoft.com/office/drawing/2014/chartex"

// nsSpreadsheet is SpreadsheetML's main namespace (ECMA-376 Part 1,
// 18), whose defined names a workbook's chartex data refer to.
const nsSpreadsheet = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"

// NoticeUnsupported is the text [ParseChartEx] puts over a chart whose kind
// the renderer does not draw: the words LibreOffice prints there
// (chart2's STR_UNSUPPORTED_CHART_TYPE).
const NoticeUnsupported = "One or more chart types are unsupported in this version of LibreOffice"

// chartExLineWidth is the width of the automatic axis lines and gridlines
// of an Office 2016 chart, in points: 18 hundredths of a millimetre.
//
// # Corpus evidence
//
//   - libreoffice/pptx: chart2/funnel-pp1.pptx prints its category axis 0.510 points
//     wide; libreoffice/xlsx: chart2/SimpleHistogram.xlsx and chart2/boxWhisker.xlsx
//     0.509 at the sheet's 98% print scale.
const chartExLineWidth = 18 * 72.0 / 2540

// ParseChartEx reads the Office 2016 chart part of pkg (MS-ODRAWXML,
// 2.24.3: a cx:chartSpace root) into a chart model, with the package's
// theme for the scheme colours and fonts, as LibreOffice prints such a
// chart:
//
//   - A histogram (a clusteredColumn series with cx:binning) is a bar
//     chart of the counts of its values in bins, the bins' ranges as its
//     categories (see [chart.Binning]).
//   - Every other kind (funnel, box and whisker, waterfall, treemap,
//     sunburst, region map, Pareto, a clusteredColumn series without
//     bins) keeps its title, legend and axes, draws no series, and shows
//     [NoticeUnsupported] over its middle ([chart.Unsupported]).
//
// A series' data are the cx:lvl caches of its cx:numDim and cx:strDim; a
// formula naming a workbook's defined name (Excel names chartex data
// "_xlchart.v1.0" and alike) is replaced by the range the name holds, so
// the converter's [chart.Resolver] can read it. A series without cache keeps
// its range and is reported with [CodeChartNoCache].
//
// It fails with [ErrNoPart] for a missing part and wraps the parse errors
// of [Package.ParseXML]; a part whose root is not cx:chartSpace fails
// through the error message. ctx's error stops it.
func ParseChartEx(ctx context.Context, pkg *Package, part string) (*chart.Chart, error) {
	root, err := pkg.ParseXML(ctx, part, nil)
	if err != nil {
		return nil, err
	}
	theme := officeTheme
	opts := ChartOptions{}
	var names map[string]string
	if main, err := pkg.MainPart(); err == nil {
		opts.Presentation = strings.HasSuffix(main, "/presentation.xml")
		if name := pkg.RelatedPart(main, RelTheme); name != "" {
			if troot, err := pkg.ParseXML(ctx, name, nil); err == nil {
				if t := ParseTheme(troot); len(t.Colors) > 0 {
					theme = t
				}
			}
		}
		if wb, err := pkg.ParseXML(ctx, main, nil); err == nil && wb.Is(nsSpreadsheet, "workbook") {
			names = definedNames(wb)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c, err := ChartExFromElement(root, chartTheme(ctx, pkg, part, theme), opts, names)
	if err != nil {
		return nil, fmt.Errorf("reading the chart %s: %w", part, err)
	}
	return c, nil
}

// maxDefinedNames caps the defined names read from a workbook. Anti-DoS
// guard, not spec-mandated: a workbook holds a few hundred names.
const maxDefinedNames = 65536

// definedNames returns the workbook-wide defined names of a workbook
// element by name (ECMA-376 Part 1, 18.2.5 definedName).
func definedNames(wb *Element) map[string]string {
	out := map[string]string{}
	for _, d := range wb.Child(nsSpreadsheet, "definedNames").Kids() {
		if len(out) >= maxDefinedNames {
			break
		}
		if !d.Is(nsSpreadsheet, "definedName") {
			continue
		}
		if _, local := d.Attr("localSheetId"); local {
			continue
		}
		if name := attrOr(d, "name"); name != "" {
			out[name] = strings.TrimSpace(d.Text)
		}
	}
	return out
}

// errNotChartEx reports a part whose root is not a chartex chart space.
var errNotChartEx = fmt.Errorf("not a cx:chartSpace")

// cxChild returns the first child cx:local of e.
func cxChild(e *Element, local string) *Element { return e.Child(NSChartEx, local) }

// ChartExFromElement reads a parsed cx:chartSpace element as [ParseChartEx]
// does, with theme for the scheme colours (nil for Office's default), opts
// for the package's defaults and names the workbook's defined names, nil
// for none.
func ChartExFromElement(root *Element, theme *Theme, opts ChartOptions, names map[string]string) (*chart.Chart, error) {
	if !root.Is(NSChartEx, "chartSpace") {
		return nil, errNotChartEx
	}
	if theme == nil || len(theme.Colors) == 0 {
		theme = officeTheme
	}
	p := &chartReader{theme: theme, style: defaultChartStyle, c: &chart.Chart{}, impress: opts.Presentation}
	p.global = p.textProps(cxChild(root, "txPr"))
	x := &chartExReader{chartReader: p, names: names, data: map[string]*Element{}}
	x.read(root)
	return p.c, nil
}

// chartExReader holds the state of one chartex chart's reading.
type chartExReader struct {
	*chartReader
	names map[string]string
	// data maps a cx:data id to its element.
	data map[string]*Element
}

// read fills the chart from the chart space root.
func (x *chartExReader) read(root *Element) {
	c := x.c
	for _, d := range cxChild(root, "chartData").Kids() {
		if d.Is(NSChartEx, "data") {
			if id := attrOr(d, "id"); id != "" {
				if _, dup := x.data[id]; !dup {
					x.data[id] = d
				}
			}
		}
	}
	ch := cxChild(root, "chart")
	c.Area = x.frame(cxChild(root, "spPr"), x.autoFrame(true))
	accents := []string{"accent1", "accent2", "accent3", "accent4", "accent5", "accent6"}
	c.Palette, c.PaletteCycleTint = x.palette(accents), true
	pa := cxChild(ch, "plotArea")
	c.PlotArea = x.frame(cxChild(pa, "spPr"), x.autoFrame(false))
	var series []*Element
	pareto := false
	for _, s := range cxChild(pa, "plotAreaRegion").Kids() {
		if s.Is(NSChartEx, "series") && len(series) < chart.MaxSeries {
			series = append(series, s)
			pareto = pareto || attrOr(s, "layoutId") == "paretoLine"
		}
	}
	var catAx, valAx *Element
	for _, a := range pa.Kids() {
		if !a.Is(NSChartEx, "axis") {
			continue
		}
		switch {
		case catAx == nil && cxChild(a, "catScaling") != nil:
			catAx = a
		case valAx == nil && cxChild(a, "valScaling") != nil:
			valAx = a
		}
	}
	g := &chart.Group{Kind: chart.Unsupported, GapWidth: 150, XAxis: 0, YAxis: 1}
	histogram := !pareto && len(series) > 0
	for _, s := range series {
		histogram = histogram && attrOr(s, "layoutId") == "clusteredColumn" && cxChild(cxChild(s, "layoutPr"), "binning") != nil
	}
	if histogram {
		g.Kind = chart.Bar
	}
	if v := parseFloat(attrOr(cxChild(catAx, "catScaling"), "gapWidth")); v >= 0 && v <= 5 {
		// oox's AxisConverter: the gap as a fraction of a bar's width.
		g.GapWidth = math.Floor(v*100 + 0.5)
	}
	for i, s := range series {
		g.Series = append(g.Series, x.series(s, i, histogram))
	}
	if len(series) > 0 {
		c.Groups = append(c.Groups, g)
	}
	// The axes belong to the last series; a chart of several series draws
	// them as secondary axes, whose gridlines LibreOffice does not draw.
	grid := len(series) <= 1 || histogram
	c.Axes = []*chart.Axis{x.axis(catAx, chart.CategoryAxis, grid), x.axis(valAx, chart.ValueAxis, grid)}
	c.Axes[0].BetweenCategories = histogram
	c.Axes[1].Position = chart.AxisLeft
	if !histogram && len(series) > 0 {
		// chart2's UnsupportedChart: 10-point text in the default face, in
		// a white box with a white hairline.
		white := compose.Color{R: 1, G: 1, B: 1, A: 1}
		c.Notice = &chart.Title{
			Text:  NoticeUnsupported,
			Style: chart.TextStyle{Families: []string{"Liberation Serif"}, Size: 10, Color: compose.Black},
			Frame: chart.Frame{Fill: &chart.Paint{Color: white}, Border: &chart.Stroke{Color: white}},
		}
		x.warn(CodeChartUnsupported, "an Office 2016 chart of a kind other than a histogram is drawn without its series")
	}
	x.readTitle(cxChild(ch, "title"))
	for _, s := range g.Series {
		if s.Name == "" && !x.impress {
			s.Name = rangeName(s.Values.Ref)
		}
		if s.Name == "" {
			s.Name = "Series" + strconv.Itoa(s.Index+1)
		}
	}
	if lg := cxChild(ch, "legend"); lg != nil {
		x.readLegend(lg)
	}
}

// series reads one cx:series (MS-ODRAWXML, 2.24.3.47 CT_Series), the i-th.
func (x *chartExReader) series(e *Element, i int, histogram bool) *chart.Series {
	s := &chart.Series{Index: i}
	s.Name = txData(cxChild(e, "tx"))
	if fill, ok := x.fill(cxChild(e, "spPr")); ok && fill != nil && !fill.None {
		s.Fill = fill
	}
	if ln := cxChild(e, "spPr").Child(NSA, "ln"); ln != nil {
		s.Border = x.stroke(ln, nil)
	}
	if d := x.data[attrOr(cxChild(e, "dataId"), "val")]; d != nil {
		for _, dim := range d.Kids() {
			switch {
			case dim.Is(NSChartEx, "numDim") && (attrOr(dim, "type") == "val" || attrOr(dim, "type") == "size"):
				if s.Values.Ref == "" && !s.Values.Cached {
					s.Values = x.dimData(dim, true)
				}
			case dim.Is(NSChartEx, "strDim") && attrOr(dim, "type") == "cat", dim.Is(NSChartEx, "numDim") && attrOr(dim, "type") == "cat":
				if s.Categories.Ref == "" && !s.Categories.Cached {
					s.Categories = x.dimData(dim, dim.Local == "numDim")
				}
			}
		}
	}
	if histogram {
		b := &chart.Binning{}
		bin := cxChild(cxChild(e, "layoutPr"), "binning")
		if v := parseFloat(childText(bin, "binSize")); v > 0 {
			b.Width = v
		} else if v, ok := Int(childText(bin, "binCount")); ok && v > 0 {
			b.Count = v
		}
		s.Bins = b
	}
	return s
}

// childText returns the val attribute of e's child cx:local, else its
// text: chartex writes a bin size either way.
func childText(e *Element, local string) string {
	c := cxChild(e, local)
	if c == nil {
		return ""
	}
	if v, ok := c.Attr("val"); ok {
		return v
	}
	return c.Text
}

// dimData reads a cx:numDim or cx:strDim (MS-ODRAWXML, 2.24.3.36
// CT_NumericDimension and 2.24.3.57 CT_StringDimension): its formula, a
// workbook's defined name resolved to its range, and its first level's
// cache.
func (x *chartExReader) dimData(dim *Element, numeric bool) chart.Data {
	var d chart.Data
	d.Ref = strings.TrimSpace(textOf(cxChild(dim, "f")))
	if r, ok := x.names[d.Ref]; ok {
		d.Ref = r
	}
	if lvl := cxChild(dim, "lvl"); lvl != nil {
		d.Cached = true
		d.Format = attrOr(lvl, "formatCode")
		if strings.EqualFold(d.Format, "General") {
			d.Format = ""
		}
		n, ok := Int(attrOr(lvl, "ptCount"))
		if !ok || n < 0 {
			n = 0
		}
		for _, pt := range lvl.Kids() {
			if pt.Is(NSChartEx, "pt") {
				if i, ok := Int(attrOr(pt, "idx")); ok && i >= n && i < chart.MaxPoints {
					n = i + 1
				}
			}
		}
		n = min(n, chart.MaxPoints)
		d.Values = make([]chart.Value, n)
		next := 0
		for _, pt := range lvl.Kids() {
			if !pt.Is(NSChartEx, "pt") {
				continue
			}
			i, ok := Int(attrOr(pt, "idx"))
			if !ok {
				i = next
			}
			next = i + 1
			if i < 0 || i >= n {
				continue
			}
			if v := parseFloat(pt.Text); numeric && !math.IsNaN(v) {
				d.Values[i] = chart.Value{Kind: chart.Number, Number: v, Text: pt.Text}
				continue
			}
			d.Values[i] = chart.Str(pt.Text)
		}
	}
	if !d.Cached && d.Ref != "" {
		x.warn(CodeChartNoCache, "an Office 2016 chart's data have no cached values in the file and are drawn empty")
	}
	return d
}

// txData returns the text of a cx:tx: its cx:txData's cached cx:v, or its
// cx:rich's paragraphs, a paragraph each line.
func txData(tx *Element) string {
	if tx == nil {
		return ""
	}
	if td := cxChild(tx, "txData"); td != nil {
		return textOf(cxChild(td, "v"))
	}
	return paragraphsText(cxChild(tx, "rich"))
}

// paragraphsText returns the runs' text of a DrawingML text body, a
// paragraph each line, "" when it holds none.
func paragraphsText(body *Element) string {
	var lines []string
	any := false
	for _, para := range body.Kids() {
		if !para.Is(NSA, "p") {
			continue
		}
		var b strings.Builder
		for _, r := range para.Kids() {
			switch {
			case r.Is(NSA, "r"), r.Is(NSA, "fld"):
				t := textOf(r.Child(NSA, "t"))
				any = any || t != ""
				b.WriteString(t)
			case r.Is(NSA, "br"):
				b.WriteString("\n")
			}
		}
		lines = append(lines, b.String())
	}
	if !any {
		return ""
	}
	return strings.Join(lines, "\n")
}

// readTitle reads a cx:title (MS-ODRAWXML, 2.24.3.62 CT_ChartTitle): its
// text from cx:tx, else from the runs of its cx:txPr, as LibreOffice reads
// a title Excel wrote that way, else "Chart Title".
func (x *chartExReader) readTitle(t *Element) {
	if t == nil {
		return
	}
	text := txData(cxChild(t, "tx"))
	if text == "" {
		text = paragraphsText(cxChild(t, "txPr"))
	}
	if text == "" {
		text = "Chart Title"
	}
	own := x.textProps(cxChild(t, "txPr"))
	if rich := cxChild(cxChild(t, "tx"), "rich"); rich != nil {
		own = x.textProps(rich)
	}
	x.c.Title = &chart.Title{
		Text:    text,
		Style:   x.textStyle(own, 18, true, 1.2),
		Frame:   x.frame(cxChild(t, "spPr"), chart.Frame{}),
		Overlay: boolAttr(t, "overlay"),
	}
}

// boolAttr reads an xsd:boolean attribute, false when absent.
func boolAttr(e *Element, name string) bool {
	v := attrOr(e, name)
	return v == "1" || v == "true"
}

// readLegend reads a cx:legend (MS-ODRAWXML, 2.24.3.31 CT_Legend).
func (x *chartExReader) readLegend(lg *Element) {
	l := &chart.Legend{Overlay: boolAttr(lg, "overlay")}
	switch attrOr(lg, "pos") {
	case "l":
		l.Position = chart.LegendLeft
	case "t":
		l.Position = chart.LegendTop
	case "b":
		l.Position = chart.LegendBottom
	}
	l.Style = x.textStyle(x.textProps(cxChild(lg, "txPr")), 10, false, 1)
	l.Frame = x.frame(cxChild(lg, "spPr"), chart.Frame{})
	x.c.Legend = l
}

// axis reads a cx:axis (MS-ODRAWXML, 2.24.3.6 CT_Axis) as oox's
// CxAxisContext does: its scaling, tick marks (crossing by default),
// labels, number format, gridlines, line and text. A nil e is a missing
// axis, which is not drawn. grid false leaves the gridlines out.
func (x *chartExReader) axis(e *Element, kind chart.AxisKind, grid bool) *chart.Axis {
	a := &chart.Axis{Kind: kind, Min: math.NaN(), Max: math.NaN(), MajorUnit: math.NaN(), MinorUnit: math.NaN(), MajorTicks: chart.TickCross, MinorTicks: chart.TickCross}
	if e == nil {
		a.Deleted = true
		return a
	}
	if sc := cxChild(e, "valScaling"); sc != nil {
		a.Min, a.Max = parseFloat(attrOr(sc, "min")), parseFloat(attrOr(sc, "max"))
		a.MajorUnit, a.MinorUnit = parseFloat(attrOr(sc, "majorUnit")), parseFloat(attrOr(sc, "minorUnit"))
	}
	if t := cxChild(e, "majorTickMarks"); t != nil {
		a.MajorTicks = tickMark(attrOr(t, "type"), chart.TickCross)
	}
	if t := cxChild(e, "minorTickMarks"); t != nil {
		a.MinorTicks = tickMark(attrOr(t, "type"), chart.TickCross)
	}
	if nf := cxChild(e, "numFmt"); nf != nil {
		if code := attrOr(nf, "formatCode"); code != "" && (attrOr(nf, "sourceLinked") != "1" || !strings.EqualFold(code, "General")) {
			a.Format = code
		}
	}
	auto := &chart.Stroke{Color: x.autoLine().Color, Width: chartExLineWidth}
	if g := cxChild(e, "majorGridlines"); g != nil && grid {
		a.MajorGrid = x.lineOf(cxChild(g, "spPr"), auto)
	}
	if g := cxChild(e, "minorGridlines"); g != nil && grid {
		a.MinorGrid = x.lineOf(cxChild(g, "spPr"), auto)
	}
	a.Line = x.lineOf(cxChild(e, "spPr"), auto)
	a.Style = x.textStyle(x.textProps(cxChild(e, "txPr")), 10, false, 1)
	if t := cxChild(e, "title"); t != nil {
		text := txData(cxChild(t, "tx"))
		if text == "" {
			text = "Axis Title"
		}
		a.Title = &chart.Title{Text: text, Style: x.textStyle(x.textProps(cxChild(t, "txPr")), 10, true, 1), Frame: x.frame(cxChild(t, "spPr"), chart.Frame{})}
	}
	return a
}
