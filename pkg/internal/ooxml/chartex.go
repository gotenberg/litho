package ooxml

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/chart"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
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
//   - libreoffice/pptx: chart2/funnel-pp1.pptx prints its category axis
//     0.510 points wide.
//   - libreoffice/xlsx: chart2/SimpleHistogram.xlsx and
//     chart2/boxWhisker.xlsx print it 0.509 points wide at the sheet's 98%
//     print scale.
const chartExLineWidth = 18 * 72.0 / 2540

// ParseChartEx reads the Office 2016 chart part of pkg (MS-ODRAWXML,
// 2.24.3: a cx:chartSpace root) into a chart model, with the package's
// theme for the scheme colours and fonts and its chart style parts for
// the automatic formats, as Office prints such a chart:
//
//   - A histogram (a clusteredColumn series with cx:binning) is a bar
//     chart of the counts of its values in bins, the bins' ranges as its
//     categories (see [chart.Binning]); a clusteredColumn series without
//     bins, a Pareto chart, a waterfall, a box and whisker chart and a
//     funnel are drawn as their kinds ([chart.Waterfall],
//     [chart.BoxWhisker], [chart.Funnel]); a treemap and a sunburst are
//     drawn from the levels of their categories ([chart.Hierarchy]).
//   - A region map, or a mix of kinds, keeps its title, legend and axes,
//     draws no series, and shows [NoticeUnsupported] over its middle
//     ([chart.Unsupported]).
//
// A series' data are the cx:lvl caches of its cx:numDim and cx:strDim; a
// formula naming a workbook's defined name (Excel names chartex data
// "_xlchart.v1.0" and alike) is replaced by the range the name holds, so
// the converter's [chart.Resolver] can read it. A series without cache keeps
// its range and is reported with [warn.ChartNoCache].
//
// It fails with [ErrNoPart] for a missing part and wraps the parse errors
// of [Package.ParseXML]; a part whose root is not cx:chartSpace fails
// with office.ErrMalformed. ctx's error stops it.
func ParseChartEx(ctx context.Context, pkg *Package, part string) (*chart.Chart, error) {
	root, err := pkg.ParseXML(ctx, part, nil)
	if err != nil {
		return nil, err
	}
	theme := officeTheme
	opts := ChartOptions{}
	var names map[string]string
	if main, err := pkg.MainPart(ctx); err == nil {
		opts.Presentation = strings.HasSuffix(main, "/presentation.xml")
		if name := pkg.RelatedPart(ctx, main, RelTheme); name != "" {
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
	opts.ChartStyle, opts.ColorStyle = readChartStyles(ctx, pkg, part)
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
var errNotChartEx = fmt.Errorf("%w: not a cx:chartSpace", office.ErrMalformed)

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
	p := &chartReader{theme: theme, style: defaultChartStyle, c: &chart.Chart{Rules: chart.OfficeRules, Office2016: true}, impress: opts.Presentation}
	p.global = p.textProps(cxChild(root, "txPr"))
	x := &chartExReader{chartReader: p, names: names, data: map[string]*Element{}, style: newChartStyle(opts.ChartStyle, opts.ColorStyle, theme)}
	x.read(root)
	return p.c, nil
}

// chartExReader holds the state of one chartex chart's reading.
type chartExReader struct {
	*chartReader
	names map[string]string
	// data maps a cx:data id to its element.
	data map[string]*Element
	// style is the chart's style parts, nil for none.
	style *chartStyle
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
	c.Area = x.frame(cxChild(root, "spPr"), x.styleFrame("chartArea", x.autoFrame(true)))
	accents := []string{"accent1", "accent2", "accent3", "accent4", "accent5", "accent6"}
	c.Palette, c.PaletteCycleTint = x.palette(accents), true
	if x.style != nil && len(x.style.colors) > 0 {
		c.Palette = make([]typeset.Color, len(x.style.colors))
		for i, col := range x.style.colors {
			c.Palette[i] = toTypeset(col)
		}
		c.PaletteCycleTint = false
	}
	pa := cxChild(ch, "plotArea")
	c.PlotArea = x.frame(cxChild(pa, "spPr"), x.styleFrame("plotArea", x.autoFrame(false)))
	var series []*Element
	for _, s := range cxChild(pa, "plotAreaRegion").Kids() {
		// A hidden series is left out of the layout (MS-ODRAWXML,
		// 2.24.3.82 CT_Series hidden).
		if s.Is(NSChartEx, "series") && !boolAttr(s, "hidden") && len(series) < chart.MaxSeries {
			series = append(series, s)
		}
	}
	var catAx, valAx, pctAx *Element
	for _, a := range pa.Kids() {
		if !a.Is(NSChartEx, "axis") {
			continue
		}
		switch {
		case catAx == nil && cxChild(a, "catScaling") != nil:
			catAx = a
		case pctAx == nil && cxChild(a, "valScaling") != nil && attrOr(cxChild(a, "units"), "unit") == "percentage":
			pctAx = a
		case valAx == nil && cxChild(a, "valScaling") != nil:
			valAx = a
		}
	}
	g := &chart.Group{Kind: x.kindOf(series), GapWidth: 150, XAxis: 0, YAxis: 1}
	var lines []*Element
	if g.Kind == chart.Bar {
		series, lines = splitPareto(series)
	}
	histogram := g.Kind == chart.Bar && binned(series)
	drawn := g.Kind != chart.Unsupported
	if v := parseFloat(attrOr(cxChild(catAx, "catScaling"), "gapWidth")); v >= 0 && v <= 5 {
		// oox's AxisConverter: the gap as a fraction of a bar's width.
		g.GapWidth = math.Floor(float64(v*100) + 0.5)
	}
	for i, s := range series {
		g.Series = append(g.Series, x.series(s, i, histogram))
		if g.Kind == chart.Waterfall {
			x.waterfall(s, g.Series[i])
		}
		if g.Kind == chart.BoxWhisker {
			x.boxWhisker(s, g.Series[i])
		}
		if g.Kind == chart.Treemap || g.Kind == chart.Sunburst {
			x.hierarchy(s, g.Series[i])
		}
		if g.Kind == chart.Bar {
			g.Series[i].Arrange = chart.Arrangement{
				Aggregate:  !histogram && cxChild(cxChild(s, "layoutPr"), "aggregation") != nil,
				Descending: len(lines) > 0,
			}
		}
	}
	x.styleSeries(g.Series)
	if len(series) > 0 {
		c.Groups = append(c.Groups, g)
	}
	if len(lines) > 0 {
		c.Groups = append(c.Groups, x.paretoLines(lines, g.Series))
	}
	// The axes belong to the last series; a chart of several series draws
	// them as secondary axes, whose gridlines LibreOffice does not draw.
	grid := len(series) <= 1 || drawn
	c.Axes = []*chart.Axis{x.axis(catAx, chart.CategoryAxis, grid), x.axis(valAx, chart.ValueAxis, grid)}
	c.Axes[0].BetweenCategories = drawn
	c.Axes[1].Position = chart.AxisLeft
	if g.Kind == chart.Funnel {
		// A funnel's categories run down the plot area, its first at the
		// top, on an axis at its left.
		c.Axes[0].Reverse, c.Axes[0].Position = true, chart.AxisLeft
	}
	if len(lines) > 0 {
		c.Axes = append(c.Axes, x.percentAxis(pctAx))
	}
	if !drawn && len(series) > 0 {
		// chart2's UnsupportedChart: 10-point text in the default face, in
		// a white box with a white hairline.
		white := typeset.Color{R: 1, G: 1, B: 1, A: 1}
		c.Notice = &chart.Title{
			Text:  NoticeUnsupported,
			Style: chart.TextStyle{Families: []string{"Liberation Serif"}, Size: 10, Color: typeset.Black},
			Frame: chart.Frame{Fill: &chart.Paint{Color: white}, Border: &chart.Stroke{Color: white}},
		}
		x.warn(warn.ContentUnsupported, warn.Omitted, "an Office 2016 chart of a kind the reader does not draw is drawn without its series")
	}
	x.readTitle(cxChild(ch, "title"))
	for _, s := range g.Series {
		if s.Name == "" {
			s.Name = unnamedSeries(s.Index)
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
	if dl := cxChild(e, "dataLabels"); dl != nil {
		s.Labels = x.dataLabels(dl)
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
		// MS-ODRAWXML, 2.24.4.14 ST_IntervalClosedSide and 2.24.4.7
		// ST_DoubleOrAutomatic: "auto" or no value sets no bound.
		b.ClosedLeft = attrOr(bin, "intervalClosed") == "l"
		if v := parseFloat(attrOr(bin, "underflow")); numeric.Finite(v) {
			b.Underflow, b.HasUnderflow = v, true
		}
		if v := parseFloat(attrOr(bin, "overflow")); numeric.Finite(v) {
			b.Overflow, b.HasOverflow = v, true
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

// dimData reads a cx:numDim or cx:strDim (MS-ODRAWXML, 2.24.3.68
// CT_NumericDimension and 2.24.3.86 CT_StringDimension): its formula, a
// workbook's defined name resolved to its range, and its first level's
// cache.
func (x *chartExReader) dimData(dim *Element, numeric bool) chart.Data {
	var d chart.Data
	d.Ref = strings.TrimSpace(textOf(cxChild(dim, "f")))
	if r, ok := x.names[d.Ref]; ok {
		d.Ref = r
	}
	if lvl := cxChild(dim, "lvl"); lvl != nil {
		d = levelData(lvl, numeric, d.Ref)
	}
	if !d.Cached && d.Ref != "" {
		x.warn(warn.ChartNoCache, warn.Omitted, "an Office 2016 chart's data have no cached values in the file and are drawn empty")
	}
	return d
}

// parentLevels returns the cached levels of the category dimension dim
// after its first, the points' parents, the nearest first (MS-ODRAWXML,
// 2.24.3.86 CT_StringDimension: a cx:lvl per level of a hierarchy, the
// leaves first), at most chart.MaxLevels.
func parentLevels(dim *Element) []chart.Data {
	var out []chart.Data
	first := true
	for _, lvl := range dim.Kids() {
		if !lvl.Is(NSChartEx, "lvl") {
			continue
		}
		if first {
			first = false
			continue
		}
		if len(out) >= chart.MaxLevels {
			break
		}
		out = append(out, levelData(lvl, false, ""))
	}
	return out
}

// levelData reads the points of one cx:lvl cache (MS-ODRAWXML, 2.24.3.87
// CT_StringLevel and 2.24.3.69 CT_NumericLevel), numbers when numeric,
// with ref its formula.
func levelData(lvl *Element, numeric bool, ref string) chart.Data {
	d := chart.Data{Ref: ref}
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
	seen := false
	for _, para := range body.Kids() {
		if !para.Is(NSA, "p") {
			continue
		}
		var b strings.Builder
		for _, r := range para.Kids() {
			switch {
			case r.Is(NSA, "r"), r.Is(NSA, "fld"):
				t := textOf(r.Child(NSA, "t"))
				seen = seen || t != ""
				b.WriteString(t)
			case r.Is(NSA, "br"):
				b.WriteString("\n")
			}
		}
		lines = append(lines, b.String())
	}
	if !seen {
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
	style := x.textStyle(own, 18, true, 1.2)
	if x.style != nil {
		style = x.styleTextOf("title", own, 14, 18, true)
	}
	x.c.Title = &chart.Title{
		Text:    text,
		Style:   style,
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
	l.Style = x.styleTextOf("legend", x.textProps(cxChild(lg, "txPr")), 9, 10, false)
	l.Frame = x.frame(cxChild(lg, "spPr"), chart.Frame{})
	x.c.Legend = l
}

// axis reads a cx:axis (MS-ODRAWXML, 2.24.3.6 CT_Axis): its scaling,
// tick marks (none when the axis states none, as Office draws it; a
// cx:majorTickMarks without a type crosses, as oox's CxAxisContext reads
// it), labels, number format, gridlines, line and text. A nil e is a
// missing axis, which is not drawn. grid false leaves the gridlines out.
//
// # Corpus evidence
//
//   - libreoffice/pptx, libreoffice/xlsx: PowerPoint draws funnel-pp1's
//     category axis line and Excel SimpleHistogram's axes without tick
//     marks, their axes stating none.
func (x *chartExReader) axis(e *Element, kind chart.AxisKind, grid bool) *chart.Axis {
	a := &chart.Axis{Kind: kind, Min: math.NaN(), Max: math.NaN(), MajorUnit: math.NaN(), MinorUnit: math.NaN(), MajorTicks: chart.TickNone, MinorTicks: chart.TickNone}
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
	styleName := "valueAxis"
	if kind == chart.CategoryAxis {
		styleName = "categoryAxis"
	}
	line, major, minor := auto, auto, auto
	if x.style != nil {
		line, _ = x.styleLine(styleName, css.Color{})
		major, _ = x.styleLine("gridlineMajor", css.Color{})
		minor, _ = x.styleLine("gridlineMinor", css.Color{})
	}
	if g := cxChild(e, "majorGridlines"); g != nil && grid {
		a.MajorGrid = x.lineOf(cxChild(g, "spPr"), major)
	}
	if g := cxChild(e, "minorGridlines"); g != nil && grid {
		a.MinorGrid = x.lineOf(cxChild(g, "spPr"), minor)
	}
	a.Line = x.lineOf(cxChild(e, "spPr"), line)
	a.Style = x.styleTextOf(styleName, x.textProps(cxChild(e, "txPr")), 8, 10, false)
	if t := cxChild(e, "title"); t != nil {
		text := txData(cxChild(t, "tx"))
		if text == "" {
			text = "Axis Title"
		}
		a.Title = &chart.Title{Text: text, Style: x.styleTextOf("axisTitle", x.textProps(cxChild(t, "txPr")), 10, 10, true), Frame: x.frame(cxChild(t, "spPr"), chart.Frame{})}
	}
	return a
}

// styleFrame returns the frame the chart style's entry name gives, def
// without a chart style.
//
// # Corpus evidence
//
//   - libreoffice/pptx: PowerPoint draws funnel-pp1's chart area neither
//     filled nor framed, where its chart style fills it in bg1 and frames
//     it, allowing the no-fill and no-line overrides; libreoffice/xlsx:
//     Excel frames boxWhisker's chart area as its style says.
func (x *chartExReader) styleFrame(name string, def chart.Frame) chart.Frame {
	if x.style == nil {
		return def
	}
	var f chart.Frame
	if fill, ok := x.styleFill(name, css.Color{}); ok && !fill.None {
		f.Fill = fill
	}
	f.Border, _ = x.styleLine(name, css.Color{})
	if x.impress {
		// PowerPoint takes the overrides the entry allows: no fill, no
		// line (MS-ODRAWXML, 2.8.4.7 ST_StyleEntryModifier).
		mods := strings.Fields(attrOr(x.style.entries[name], "mods"))
		if slices.Contains(mods, "allowNoFillOverride") {
			f.Fill = nil
		}
		if slices.Contains(mods, "allowNoLineOverride") {
			f.Border = nil
		}
	}
	return f
}

// styleSeries gives the series whose file states no fill the chart
// style's data point fill: none, or an explicit fill; a fill of the
// series' own colour stays automatic.
func (x *chartExReader) styleSeries(series []*chart.Series) {
	if x.style == nil {
		return
	}
	for _, s := range series {
		if s.Fill != nil {
			continue
		}
		auto := toCSS(x.c.AutoColor(s.Index, len(series)))
		if f, ok := x.styleFill("dataPoint", auto); ok && (f.None || f.Shape != nil || toCSS(f.Color) != auto) {
			s.Fill = f
		}
	}
}

// styleTextOf returns the text style of a chart text: the chart style's
// entry name under own, of def points when neither states a size;
// without a chart style, LibreOffice's automatic text of def0 points,
// bold when bold0.
//
// # Corpus evidence
//
//   - litho/docx, litho/xlsx, litho/pptx: in charts-chartex and chartex,
//     whose chart style parts state no size, Word, Excel and PowerPoint
//     print the title in 14 pt regular, the legend in 9 pt, the axis and
//     data labels in 8 pt, all in tx1.
func (x *chartExReader) styleTextOf(name string, own textProps, def, def0 float64, bold0 bool) chart.TextStyle {
	if x.style == nil {
		return x.textStyle(own, def0, bold0, 1)
	}
	return x.textStyle(x.styleText(name).over(own), def, false, 1)
}

// dataLabels reads a series' cx:dataLabels (MS-ODRAWXML, 2.24.3.24
// CT_DataLabels): the parts its cx:visibility shows (2.24.3.25), its
// position (2.24.4.6 ST_DataLabelPos), number format, separator and text.
func (x *chartExReader) dataLabels(e *Element) chart.Labels {
	var l chart.Labels
	vis := cxChild(e, "visibility")
	l.ShowValue = boolAttr(vis, "value")
	l.ShowCategory = boolAttr(vis, "categoryName")
	l.ShowSeries = boolAttr(vis, "seriesName")
	if sep := cxChild(e, "separator"); sep != nil {
		l.Separator = sep.Text
	}
	switch attrOr(e, "pos") {
	case "ctr":
		l.Placement = chart.LabelCenter
	case "inEnd":
		l.Placement = chart.LabelInsideEnd
	case "inBase":
		l.Placement = chart.LabelInsideBase
	case "outEnd":
		l.Placement = chart.LabelOutsideEnd
	case "t":
		l.Placement = chart.LabelAbove
	case "b":
		l.Placement = chart.LabelBelow
	case "l":
		l.Placement = chart.LabelLeft
	case "r":
		l.Placement = chart.LabelRight
	case "bestFit":
		l.Placement = chart.LabelBestFit
	}
	if nf := cxChild(e, "numFmt"); nf != nil && attrOr(nf, "sourceLinked") != "1" {
		l.Format = attrOr(nf, "formatCode")
	}
	l.Style = x.styleTextOf("dataLabel", x.textProps(cxChild(e, "txPr")), 8, 10, false)
	l.Frame = x.frame(cxChild(e, "spPr"), chart.Frame{})
	// Office sets a label outside a column's end against it.
	l.Flush = true
	return l
}

// kindOf returns the kind a chart of the cx:series series is drawn as
// (MS-ODRAWXML, 2.24.4.21 ST_SeriesLayout): bars when every series is a
// clusteredColumn, binned or not, or the paretoLine of one; a waterfall,
// boxes and whiskers or a funnel when every series is one;
// [chart.Unsupported] for the other kinds and their mixes.
func (x *chartExReader) kindOf(series []*Element) chart.Kind {
	if len(series) == 0 {
		return chart.Unsupported
	}
	columns, waterfall, boxes, funnel, owner := true, true, true, true, false
	treemap, sunburst := true, true
	for _, s := range series {
		id := attrOr(s, "layoutId")
		columns = columns && (id == "clusteredColumn" || id == "paretoLine")
		owner = owner || id == "clusteredColumn"
		waterfall = waterfall && id == "waterfall"
		boxes = boxes && id == "boxWhisker"
		funnel = funnel && id == "funnel"
		treemap = treemap && id == "treemap"
		sunburst = sunburst && id == "sunburst"
	}
	switch {
	case treemap:
		return chart.Treemap
	case sunburst:
		return chart.Sunburst
	case columns && owner:
		return chart.Bar
	case waterfall:
		return chart.Waterfall
	case boxes:
		return chart.BoxWhisker
	case funnel:
		return chart.Funnel
	}
	return chart.Unsupported
}

// hierarchy reads the hierarchy of the treemap or sunburst cx:series e
// into s: the parents its category dimension's further levels name, a
// banner over each parent when its parent labels take one (MS-ODRAWXML,
// 2.24.3.74 CT_ParentLabelLayout), and the width of the line the chart
// style's data point gives each cell or sector, drawn in the cell's own
// colour.
func (x *chartExReader) hierarchy(e *Element, s *chart.Series) {
	h := &chart.Hierarchy{
		Banner: attrOr(cxChild(cxChild(e, "layoutPr"), "parentLabelLayout"), "val") == "banner",
	}
	if d := x.data[attrOr(cxChild(e, "dataId"), "val")]; d != nil {
		for _, dim := range d.Kids() {
			if dim.Is(NSChartEx, "strDim") && attrOr(dim, "type") == "cat" {
				h.Parents = parentLevels(dim)
				break
			}
		}
	}
	if x.style != nil {
		if l, _ := x.styleLine("dataPoint", toCSS(x.c.AutoColor(s.Index, 1))); l != nil && !l.None {
			h.LineWidth = l.Width
		}
	}
	s.Hierarchy = h
}

// binned reports column series that are all histograms, counted in bins
// (MS-ODRAWXML, 2.24.3.11 CT_Binning).
func binned(series []*Element) bool {
	for _, s := range series {
		if cxChild(cxChild(s, "layoutPr"), "binning") == nil {
			return false
		}
	}
	return true
}

// splitPareto returns the columns of a chart of clusteredColumn series and
// its paretoLine series apart.
func splitPareto(series []*Element) ([]*Element, []*Element) {
	var cols, lines []*Element
	for _, s := range series {
		if attrOr(s, "layoutId") == "paretoLine" {
			lines = append(lines, s)
		} else {
			cols = append(cols, s)
		}
	}
	return cols, lines
}

// maxSubtotals caps the subtotal indices read from a waterfall series.
// Anti-DoS guard, not spec-mandated: a subtotal names a point, and a
// series keeps at most chart.MaxPoints.
const maxSubtotals = chart.MaxPoints

// waterfall reads the waterfall properties of the cx:series e into s: its
// subtotal points (MS-ODRAWXML, 2.24.3.90 CT_Subtotals) and its connector
// lines, shown unless its cx:visibility hides them (2.24.3.83
// CT_SeriesElementVisibilities connectorLines).
func (x *chartExReader) waterfall(e *Element, s *chart.Series) {
	lp := cxChild(e, "layoutPr")
	for _, idx := range cxChild(lp, "subtotals").Kids() {
		if len(s.Subtotals) >= maxSubtotals {
			break
		}
		if idx.Is(NSChartEx, "idx") {
			if v, ok := Int(attrOr(idx, "val")); ok && v >= 0 && v < chart.MaxPoints {
				s.Subtotals = append(s.Subtotals, v)
			}
		}
	}
	if v, ok := cxChild(lp, "visibility").Attr("connectorLines"); !ok || v == "1" || v == "true" {
		s.Connector = x.connectorLine()
	}
}

// connectorLine returns the line of a waterfall's connectors: the chart
// style's series line, else a theme line in the text colour.
//
// # Corpus evidence
//
//   - litho/docx: Word draws charts-chartex's connectors 0.75 pt wide in
//     black, its chart style giving the series line no line.
//   - litho/xlsx: Excel draws charts_chartex's 0.38 pt wide at the
//     sheet's 51 per cent print scale.
func (x *chartExReader) connectorLine() *chart.Stroke {
	tx, _ := x.theme.Color("tx1")
	def := &chart.Stroke{Color: toTypeset(tx), Width: themeLineWidth}
	if x.style == nil {
		return def
	}
	if l, ok := x.styleLine("seriesLine", tx); ok && l != nil {
		return l
	}
	return def
}

// paretoLines returns the line group of a Pareto chart's paretoLine series
// (MS-ODRAWXML, 2.24.4.21 ST_SeriesLayout paretoLine): each the running
// share of the column series its ownerIdx names, ordered as the columns
// are, on the percentage axis, the third of the chart's axes. Its line is
// the chart style's line of a line series, else a theme line in the text
// colour.
//
// # Corpus evidence
//
//   - litho/xlsx: Excel draws charts_chartex's Pareto line 0.38 pt wide
//     in black at the sheet's 51 per cent print scale, its chart style
//     giving no line, and lists the columns' series alone in the legend.
func (x *chartExReader) paretoLines(lines []*Element, cols []*chart.Series) *chart.Group {
	g := &chart.Group{Kind: chart.Line, XAxis: 0, YAxis: 2}
	tx, _ := x.theme.Color("tx1")
	ln := &chart.Stroke{Color: toTypeset(tx), Width: themeLineWidth}
	if x.style != nil {
		if l, ok := x.styleLine("dataPointLine", tx); ok && l != nil {
			ln = l
		}
	}
	for i, e := range lines {
		owner, ok := Int(attrOr(e, "ownerIdx"))
		if !ok || owner < 0 || owner >= len(cols) {
			owner = 0
		}
		o := cols[owner]
		s := &chart.Series{Index: len(cols) + i, Name: txData(cxChild(e, "tx")), Categories: o.Categories, Values: o.Values, Bins: o.Bins}
		s.Arrange = chart.Arrangement{Aggregate: o.Arrange.Aggregate, Descending: true, Cumulative: true}
		s.Border = x.lineOf(cxChild(e, "spPr"), ln)
		s.Marker = chart.Marker{Symbol: chart.MarkerNone}
		s.NoLegend = true
		g.Series = append(g.Series, s)
	}
	return g
}

// percentAxis reads the percentage axis of a Pareto chart, e, on the
// right of the plot area from 0 to 1, its labels as percentages unless it
// states a format.
func (x *chartExReader) percentAxis(e *Element) *chart.Axis {
	a := x.axis(e, chart.ValueAxis, false)
	if e == nil {
		a.Deleted = false
	}
	a.Position = chart.AxisRight
	if math.IsNaN(a.Min) {
		a.Min = 0
	}
	if math.IsNaN(a.Max) {
		a.Max = 1
	}
	if a.Format == "" {
		a.Format = "0%"
	}
	return a
}

// boxWhisker reads the box and whisker properties of the cx:series e into
// s: its quartile method (MS-ODRAWXML, 2.24.3.85 CT_Statistics, inclusive
// when absent) and what its cx:visibility shows (2.24.3.83
// CT_SeriesElementVisibilities: the mean's marker and the outliers when
// absent, as Excel shows them); its box outline and lines from the chart
// style's data point, and its markers from the style's marker layout.
func (x *chartExReader) boxWhisker(e *Element, s *chart.Series) {
	lp := cxChild(e, "layoutPr")
	vis := cxChild(lp, "visibility")
	shown := func(name string, def bool) bool {
		v, ok := vis.Attr(name)
		if !ok {
			return def
		}
		return v == "1" || v == "true"
	}
	b := &chart.Box{
		Exclusive: attrOr(cxChild(lp, "statistics"), "quartileMethod") == "exclusive",
		Mean:      shown("meanMarker", true),
		MeanLine:  shown("meanLine", false),
		Outliers:  shown("outliers", true),
		Inner:     shown("nonoutliers", false),
	}
	auto := toCSS(x.c.AutoColor(s.Index, 1))
	b.Line, s.Border = x.boxLine(auto)
	if s.Border == nil && s.Fill == nil {
		// A box the style neither fills nor outlines stays empty.
		s.Border = &chart.Stroke{None: true}
	}
	s.Box = b
	s.Marker = boxMarker(x.styleMarker(auto), b.Line)
}

// boxMarker returns m, the chart style's marker, as Office draws a box
// plot's outliers and inner points with it: its size read in pixels at 96
// to the inch, its outline line, the box's line, inside that size. A
// marker the style leaves unpainted stays so.
//
// # Corpus evidence
//
//   - probe, docx: Word draws the outlier of a box plot whose chart style
//     gives markers a circle of size 5, filled in the series colour and
//     outlined in white, as a circle 3.0 pt across filled in accent1 and
//     outlined 0.75 pt in the box's line colour, 54,91,157: 3.75 pt, five
//     pixels, with its outline.
func boxMarker(m chart.Marker, line *chart.Stroke) chart.Marker {
	if line == nil || m.Border != nil && m.Border.None {
		return m
	}
	if size := float64(m.Size*0.75) - line.Width; size > 0 {
		m.Size = size
	}
	l := *line
	m.Border = &l
	return m
}

// boxLineShade is the factor Office multiplies each sRGB channel of a
// box's line colour by, from the chart style, for its outline, whiskers
// and median, quartile and mean marks.
//
// # Corpus evidence
//
//   - libreoffice/xlsx: Excel draws boxWhisker's boxes, filled in
//     68,114,196 and 237,125,49 by their style's phClr, with lines of
//     54,91,157 and 190,100,39, 0.80 of each channel, 0.75 pt wide.
const boxLineShade = 0.8

// boxLine returns the line of a box's whiskers and marks and its outline,
// for a series of automatic colour auto: the chart style's data point
// line, shaded by boxLineShade, for both; without one, a theme line in the
// text colour for the marks and no outline.
//
// # Corpus evidence
//
//   - litho/docx, litho/pptx: Word and PowerPoint draw charts-chartex's
//     and chartex's whiskers, caps, quartile and median lines and mean
//     crosses 0.75 pt wide in black, and no box outline, their chart
//     style giving the data point no line.
func (x *chartExReader) boxLine(auto css.Color) (*chart.Stroke, *chart.Stroke) {
	tx, _ := x.theme.Color("tx1")
	marks := &chart.Stroke{Color: toTypeset(tx), Width: themeLineWidth}
	if x.style == nil {
		return marks, nil
	}
	l, _ := x.styleLine("dataPoint", auto)
	if l == nil {
		return marks, nil
	}
	shaded := *l
	shaded.Color.R, shaded.Color.G, shaded.Color.B = float64(l.Color.R*boxLineShade), float64(l.Color.G*boxLineShade), float64(l.Color.B*boxLineShade)
	return &shaded, &shaded
}

// styleMarker returns the marker the chart style gives the points of a
// series of automatic colour auto: the symbol and size of its
// dataPointMarkerLayout (MS-ODRAWXML, 2.8.3.5 CT_MarkerLayout), filled
// and outlined by its dataPointMarker entry, which may leave it unpainted.
func (x *chartExReader) styleMarker(auto css.Color) chart.Marker {
	m := chart.Marker{Symbol: chart.MarkerCircle, Size: 5}
	if x.style == nil {
		return m
	}
	if l := x.style.entries["dataPointMarkerLayout"]; l != nil {
		if sym, ok := markerSymbol(attrOr(l, "symbol")); ok && sym != chart.MarkerAuto {
			m.Symbol = sym
		}
		if v, ok := Int(attrOr(l, "size")); ok && v >= 2 && v <= 72 {
			m.Size = float64(v)
		}
	}
	if f, ok := x.styleFill("dataPointMarker", auto); ok {
		m.Fill = f
	}
	if l, ok := x.styleLine("dataPointMarker", auto); ok {
		m.Border = l
		if l == nil {
			m.Border = &chart.Stroke{None: true}
		}
	}
	return m
}
