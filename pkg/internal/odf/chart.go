package odf

import (
	"context"
	"fmt"
	"math"
	"path"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/chart"
	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
)

// The namespaces of OpenDocument charts (ODF 1.3 Part 3, 11 and
// LibreOffice's extensions).
const (
	NSChart    = "urn:oasis:names:tc:opendocument:xmlns:chart:1.0"
	NSChartOOO = "http://openoffice.org/2010/chart"
)

// Warning codes [ParseChart] reports.
const (
	// CodeChartUnsupported flags a chart construct drawn approximately or
	// not at all: a 3D chart drawn flat, regression curves, error
	// indicators, a stock chart.
	CodeChartUnsupported = "chart-unsupported"
	// CodeChartNoData flags a series the embedded table holds no values
	// for, drawn empty unless the converter resolves its range.
	CodeChartNoData = "chart-no-cache"
)

// maxTableCells caps the cells of a chart's embedded table read.
// Anti-DoS guard, not spec-mandated: a chart draws at most a few thousand
// points, and a repeated row or column expands to that many cells.
const maxTableCells = 1 << 18

// chartFont is the font family LibreOffice gives chart text that names
// none.
const chartFont = "Liberation Sans"

// ParseChart reads the chart object stored in the package directory dir
// (ODF 1.3 Part 3, 11: the object's content.xml with an office:chart body,
// and its styles.xml) into a chart model. dir is the draw:object's
// xlink:href, "./Object 1" or "Object 1".
//
// The data comes from the table the chart embeds (11.3 table:table), the
// cache LibreOffice writes of its source ranges; a series the table does
// not cover keeps its range reference and is reported with
// [CodeChartNoData]. What the styles leave unset takes LibreOffice's chart
// defaults: Liberation Sans text, its twelve series colours.
//
// It fails with an error [IsNotExist] reports when the object has no
// content.xml, office.ErrMalformed when it holds no chart, and wraps the
// errors of [Package.ReadFile] and [ParseXML].
func ParseChart(ctx context.Context, pkg *Package, dir string) (*chart.Chart, error) {
	dir = strings.TrimSuffix(cleanName(dir), "/")
	data, err := pkg.ReadFile(ctx, path.Join(dir, "content.xml"))
	if err != nil {
		return nil, err
	}
	content, err := ParseXML(ctx, data)
	if err != nil {
		return nil, fmt.Errorf("parsing the chart %s: %w", dir, err)
	}
	var styles *Element
	if sdata, err := pkg.ReadFile(ctx, path.Join(dir, "styles.xml")); err == nil {
		if s, err := ParseXML(ctx, sdata); err == nil {
			styles = s
		}
	}
	return ChartFromDocument(content, styles)
}

// ChartFromDocument reads a chart from its parsed content document, an
// office:document-content root or the office:document a flat document
// embeds in its draw:object, and its styles document, nil for none.
func ChartFromDocument(content, styles *Element) (*chart.Chart, error) {
	ch := Section(content, "body").Child(NSOffice, "chart").Child(NSChart, "chart")
	if ch == nil {
		return nil, fmt.Errorf("%w: the object holds no chart:chart", office.ErrMalformed)
	}
	r := &chartReader{styles: map[string]*Element{}, formats: DataStylesOf(content, styles), c: &chart.Chart{}}
	for _, doc := range []*Element{styles, content} {
		for _, sec := range []string{"styles", "automatic-styles"} {
			for _, el := range kids(Section(doc, sec)) {
				if el.Is(NSStyle, "style") && el.A(NSStyle, "family") == "chart" {
					r.styles[el.A(NSStyle, "name")] = el
				}
			}
		}
	}
	r.read(ch)
	return r.c, nil
}

// chartReader holds the state of one chart's reading.
type chartReader struct {
	c      *chart.Chart
	styles map[string]*Element
	// formats indexes the chart document's data styles, which name the
	// formats of its axes and labels.
	formats *DataStyles
	table   *localTable
	warned  map[string]bool
	// textDefault is the chart's own text format, which every text
	// inherits.
	textDefault chart.TextStyle
}

// warn records a warning once per message.
func (r *chartReader) warn(code, msg string) {
	if r.warned == nil {
		r.warned = map[string]bool{}
	}
	if !r.warned[msg] {
		r.warned[msg] = true
		r.c.Warnings = append(r.c.Warnings, compose.Warning{Code: code, Message: msg})
	}
}

// maxChartStyleChain bounds the parent chain of a chart style. Anti-DoS
// guard, not spec-mandated: chart styles are automatic and rarely have a
// parent.
const maxChartStyleChain = 16

// prop returns the attribute ns:local of the property element kind
// ("chart-properties", "graphic-properties", "text-properties") of the
// chart style name, following its parents, and whether it is set.
func (r *chartReader) prop(name, kind, ns, local string) (string, bool) {
	for range maxChartStyleChain {
		st := r.styles[name]
		if st == nil {
			return "", false
		}
		if v, ok := st.Child(NSStyle, kind).Lookup(ns, local); ok {
			return v, true
		}
		name = st.A(NSStyle, "parent-style-name")
		if name == "" {
			return "", false
		}
	}
	return "", false
}

// propOr returns a property or def.
func (r *chartReader) propOr(name, kind, ns, local, def string) string {
	if v, ok := r.prop(name, kind, ns, local); ok {
		return v
	}
	return def
}

// boolProp reads a boolean chart property.
func (r *chartReader) boolProp(name, local string, def bool) bool {
	v, ok := r.prop(name, "chart-properties", NSChart, local)
	if !ok {
		return def
	}
	return v == "true"
}

// numProp reads a numeric chart property, NaN when unset.
func (r *chartReader) numProp(name, local string) float64 {
	v, ok := r.prop(name, "chart-properties", NSChart, local)
	if !ok {
		return math.NaN()
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil || math.IsInf(f, 0) {
		return math.NaN()
	}
	return f
}

// color converts an ODF colour.
func color(s string) (compose.Color, bool) {
	c, ok := ParseColor(s)
	if !ok {
		return compose.Color{}, false
	}
	return compose.Color{R: float64(c.R) / 255, G: float64(c.G) / 255, B: float64(c.B) / 255, A: 1}, true
}

// fill reads a style's area fill (ODF 1.3 Part 3, 20.110 draw:fill and
// 20.111 draw:fill-color): none, or a colour; a gradient, hatch or bitmap
// fills with draw:fill-color. def is the fill when the style sets none.
func (r *chartReader) fill(name string, def *chart.Paint) *chart.Paint {
	kind, hasKind := r.prop(name, "graphic-properties", NSDraw, "fill")
	if hasKind && kind == "none" {
		return &chart.Paint{None: true}
	}
	col, hasCol := r.prop(name, "graphic-properties", NSDraw, "fill-color")
	if c, ok := color(col); ok && hasCol {
		if t, ok := r.prop(name, "graphic-properties", NSDraw, "opacity"); ok {
			if f, ok := Percent(t); ok {
				c.A = math.Max(0, math.Min(1, f))
			}
		}
		return &chart.Paint{Color: c}
	}
	return def
}

// stroke reads a style's line (20.163 draw:stroke, 20.398
// svg:stroke-color, 20.400 svg:stroke-width): none, or its colour, width
// and a dash for draw:stroke dash. def is the line when the style sets
// none.
func (r *chartReader) stroke(name string, def *chart.Stroke) *chart.Stroke {
	kind, hasKind := r.prop(name, "graphic-properties", NSDraw, "stroke")
	if hasKind && kind == "none" {
		return &chart.Stroke{None: true}
	}
	s := chart.Stroke{Color: compose.Color{R: 0xb3 / 255.0, G: 0xb3 / 255.0, B: 0xb3 / 255.0, A: 1}}
	if def != nil {
		s = *def
	}
	set := hasKind
	if v, ok := r.prop(name, "graphic-properties", NSSVG, "stroke-color"); ok {
		if c, ok := color(v); ok {
			s.Color, set = c, true
		}
	}
	if v, ok := r.prop(name, "graphic-properties", NSSVG, "stroke-width"); ok {
		if w, ok := Length(v); ok && w >= 0 {
			s.Width, set = w, true
		}
	}
	if kind == "dash" {
		w := math.Max(s.Width, 0.75)
		s.Dash = []float64{4 * w, 3 * w}
	}
	if !set && def == nil {
		return nil
	}
	if !set && def != nil && def.None {
		return def
	}
	s.None = false
	return &s
}

// lineOf returns a style's line, nil for none.
func (r *chartReader) lineOf(name string, def *chart.Stroke) *chart.Stroke {
	s := r.stroke(name, def)
	if s != nil && s.None {
		return nil
	}
	return s
}

// textStyle reads a style's character format over the inherited one
// (ODF 1.3 Part 3, 20.190 fo:font-size, 20.192 fo:font-weight, 20.191
// fo:font-style, 20.181 fo:color, 20.275 style:font-name) and the chart
// text's rotation (20.312 style:rotation-angle).
func (r *chartReader) textStyle(name string, inherit chart.TextStyle) chart.TextStyle {
	st := inherit
	if v, ok := r.prop(name, "text-properties", NSFO, "font-size"); ok {
		if pt, ok := Length(v); ok && pt > 0 {
			st.Size = pt
		}
	}
	if v, ok := r.prop(name, "text-properties", NSFO, "font-weight"); ok {
		st.Bold = v == "bold" || v == "600" || v == "700" || v == "800" || v == "900"
	}
	if v, ok := r.prop(name, "text-properties", NSFO, "font-style"); ok {
		st.Italic = v == "italic" || v == "oblique"
	}
	if v, ok := r.prop(name, "text-properties", NSFO, "color"); ok {
		if c, ok := color(v); ok {
			st.Color = c
		}
	}
	if v, ok := r.prop(name, "text-properties", NSFO, "font-family"); ok && Unquote(v) != "" {
		st.Families = []string{Unquote(v)}
	} else if v, ok := r.prop(name, "text-properties", NSStyle, "font-name"); ok && v != "" {
		st.Families = []string{v}
	}
	if v, ok := r.prop(name, "chart-properties", NSStyle, "rotation-angle"); ok {
		if f, err := strconv.ParseFloat(strings.TrimSuffix(v, "deg"), 64); err == nil && !math.IsInf(f, 0) && !math.IsNaN(f) {
			st.Rotation = math.Mod(f, 360)
		}
	} else if v, ok := r.prop(name, "text-properties", NSStyle, "rotation-angle"); ok {
		if f, err := strconv.ParseFloat(strings.TrimSuffix(v, "deg"), 64); err == nil && !math.IsInf(f, 0) && !math.IsNaN(f) {
			st.Rotation = math.Mod(f, 360)
		}
	}
	return st
}

// paraText returns the text of an element's text:p children, a line
// each.
func paraText(e *Element) string {
	paras := e.All(NSText, "p")
	lines := make([]string, 0, len(paras))
	for _, p := range paras {
		lines = append(lines, p.TextContent())
	}
	return strings.Join(lines, "\n")
}

// read fills the chart from its chart:chart element (ODF 1.3 Part 3,
// 11.1).
func (r *chartReader) read(ch *Element) {
	c := r.c
	chStyle := ch.A(NSChart, "style-name")
	r.textDefault = r.textStyle(chStyle, chart.TextStyle{Families: []string{chartFont}, Size: 10, Color: compose.Black})
	c.Area = chart.Frame{Fill: r.fill(chStyle, &chart.Paint{Color: compose.Color{R: 1, G: 1, B: 1, A: 1}}), Border: r.lineOf(chStyle, nil)}
	r.table = readLocalTable(ch.Child(NSTable, "table"))
	if t := ch.Child(NSChart, "title"); t != nil {
		c.Title = r.title(t, 13, false)
	}
	if t := ch.Child(NSChart, "subtitle"); t != nil {
		c.Subtitle = r.title(t, 11, false)
	}
	if lg := ch.Child(NSChart, "legend"); lg != nil {
		r.legend(lg)
	}
	pa := ch.Child(NSChart, "plot-area")
	if pa == nil {
		r.warn(CodeChartUnsupported, "the chart has no plot area")
		return
	}
	r.plotArea(ch, pa)
}

// title reads a chart:title or chart:subtitle.
func (r *chartReader) title(t *Element, size float64, bold bool) *chart.Title {
	text := paraText(t)
	if strings.TrimSpace(text) == "" {
		return nil
	}
	name := t.A(NSChart, "style-name")
	base := r.textDefault
	base.Size, base.Bold = size, bold
	return &chart.Title{
		Text:  text,
		Style: r.textStyle(name, base),
		Frame: chart.Frame{Fill: noneToNil(r.fill(name, nil)), Border: r.lineOf(name, nil)},
	}
}

// noneToNil drops a fill that paints nothing.
func noneToNil(p *chart.Paint) *chart.Paint {
	if p != nil && p.None {
		return nil
	}
	return p
}

// legend reads chart:legend (ODF 1.3 Part 3, 11.3; 19.15
// chart:legend-position).
func (r *chartReader) legend(lg *Element) {
	l := &chart.Legend{}
	switch lg.A(NSChart, "legend-position") {
	case "start":
		l.Position = chart.LegendLeft
	case "top", "top-start":
		l.Position = chart.LegendTop
	case "bottom", "bottom-start", "bottom-end":
		l.Position = chart.LegendBottom
	case "top-end":
		l.Position = chart.LegendTopRight
	}
	l.Overlay = lg.A(NSLOExt, "overlay") == "true"
	name := lg.A(NSChart, "style-name")
	l.Style = r.textStyle(name, r.textDefault)
	l.Frame = chart.Frame{Fill: noneToNil(r.fill(name, nil)), Border: r.lineOf(name, nil)}
	r.c.Legend = l
}

// plotArea reads chart:plot-area: its groups, axes, series and wall
// (ODF 1.3 Part 3, 11.4).
func (r *chartReader) plotArea(ch, pa *Element) {
	c := r.c
	pName := pa.A(NSChart, "style-name")
	class := strings.TrimPrefix(ch.A(NSChart, "class"), "chart:")
	if r.boolProp(pName, "three-dimensional", false) {
		r.warn(CodeChartUnsupported, "a 3D chart is drawn flat")
	}
	// The axes first: the groups name them.
	axisIndex := map[string]int{}
	for _, ax := range pa.All(NSChart, "axis") {
		a := r.axis(ax)
		if a == nil {
			continue
		}
		key := ax.A(NSChart, "name")
		if key == "" {
			key = "primary-" + ax.A(NSChart, "dimension")
		}
		if _, dup := axisIndex[key]; dup {
			continue
		}
		axisIndex[key] = len(c.Axes)
		c.Axes = append(c.Axes, a)
	}
	xAxis, yAxis := -1, -1
	if i, ok := axisIndex["primary-x"]; ok {
		xAxis = i
	}
	if i, ok := axisIndex["primary-y"]; ok {
		yAxis = i
	}
	var cats chart.Data
	for _, ax := range pa.All(NSChart, "axis") {
		if cat := ax.Child(NSChart, "categories"); cat != nil && ax.A(NSChart, "dimension") == "x" {
			addr := cat.A(NSTable, "cell-range-address")
			cats = r.data(addr, true)
			if cols, rows, ok := rangeShape(addr); ok && cols > 1 && rows > 1 && xAxis >= 0 && xAxis < len(c.Axes) {
				// Categories of several levels join on one label, which
				// breaks between them as LibreOffice stacks the levels.
				c.Axes[xAxis].NoBreak = false
			}
		}
	}
	if cats.Values == nil && r.table != nil {
		cats = r.table.categories()
	}
	if w := pa.Child(NSChart, "wall"); w != nil {
		name := w.A(NSChart, "style-name")
		c.PlotArea = chart.Frame{Fill: noneToNil(r.fill(name, nil)), Border: r.lineOf(name, nil)}
	}
	groups := map[string]*chart.Group{}
	var order []string
	for i, se := range pa.All(NSChart, "series") {
		if len(order) > 0 && len(groups[order[0]].Series) >= chart.MaxSeries {
			r.warn(CodeChartUnsupported, "series past the chart's limit are not drawn")
			break
		}
		sClass := strings.TrimPrefix(se.A(NSChart, "class"), "chart:")
		if sClass == "" {
			sClass = class
		}
		key := sClass + "|" + se.A(NSChart, "attached-axis")
		g := groups[key]
		if g == nil {
			g = r.group(sClass, pName, xAxis, yAxis)
			if g == nil {
				r.warn(CodeChartUnsupported, "a chart of class "+sClass+" is not drawn")
				continue
			}
			ax := se.A(NSChart, "attached-axis")
			if ax != "" {
				if i, ok := axisIndex[ax]; ok && strings.HasSuffix(ax, "-y") {
					g.YAxis = i
				}
			} else {
				ax = "primary-y"
			}
			// LibreOffice writes a bar group's gap and overlap on the
			// style of the value axis it is drawn against (its
			// GapwidthSequence and OverlapSequence are per axis).
			for _, a := range pa.All(NSChart, "axis") {
				if a.A(NSChart, "name") != ax {
					continue
				}
				aName := a.A(NSChart, "style-name")
				if v := r.numProp(aName, "gap-width"); !math.IsNaN(v) {
					g.GapWidth = v
				}
				if v := r.numProp(aName, "overlap"); !math.IsNaN(v) {
					g.Overlap = v
				}
			}
			groups[key] = g
			order = append(order, key)
		}
		g.Series = append(g.Series, r.series(se, g, i, cats))
	}
	for _, k := range order {
		c.Groups = append(c.Groups, groups[k])
	}
	if len(c.Groups) > 0 {
		g := c.Groups[0]
		if (g.Kind == chart.Pie || g.Kind == chart.Doughnut) && xAxis >= 0 {
			// A reversed angle axis turns a pie counter-clockwise, as
			// LibreOffice draws its own pies by default.
			g.CounterClockwise = c.Axes[xAxis].Reverse
		}
		if g.Kind == chart.Bar && g.Horizontal && xAxis >= 0 {
			c.Axes[xAxis].Position = chart.AxisLeft
		}
	}
	if xAxis >= 0 {
		// LibreOffice shifts the categories between the ticks only when a
		// bar group shares the plot area (its Diagram's
		// ShiftedCategoryPosition).
		shifted := false
		for _, g := range c.Groups {
			shifted = shifted || g.Kind == chart.Bar
		}
		c.Axes[xAxis].BetweenCategories = shifted
	}
}

// group returns a new group for the chart class (ODF 1.3 Part 3, 19.12
// chart:class) with the plot area's properties, nil for a class the
// renderer does not draw.
func (r *chartReader) group(class, pName string, xAxis, yAxis int) *chart.Group {
	g := &chart.Group{XAxis: xAxis, YAxis: yAxis, GapWidth: 100, BubbleScale: 100}
	switch class {
	case "bar":
		g.Kind = chart.Bar
		g.Horizontal = r.boolProp(pName, "vertical", false)
	case "line":
		g.Kind = chart.Line
	case "area":
		g.Kind = chart.Area
	case "circle":
		g.Kind = chart.Pie
		g.VaryColors = true
	case "ring":
		g.Kind = chart.Doughnut
		g.VaryColors = true
		g.HoleSize = 50
	case "scatter":
		g.Kind = chart.Scatter
	case "bubble":
		g.Kind = chart.Bubble
	case "radar":
		g.Kind = chart.Radar
	case "filled-radar":
		g.Kind = chart.Radar
		g.Filled = true
	case "stock":
		r.warn(CodeChartUnsupported, "a stock chart is drawn as a line chart")
		g.Kind = chart.Line
	default:
		return nil
	}
	switch {
	case r.boolProp(pName, "percentage", false):
		g.Grouping = chart.PercentStacked
	case r.boolProp(pName, "stacked", false):
		g.Grouping = chart.Stacked
	}
	if v := r.numProp(pName, "gap-width"); !math.IsNaN(v) {
		g.GapWidth = v
	}
	if v := r.numProp(pName, "overlap"); !math.IsNaN(v) {
		g.Overlap = v
	}
	if v := r.numProp(pName, "angle-offset"); !math.IsNaN(v) {
		g.FirstSliceAngle = 90 - v
	}
	if v, ok := r.prop(pName, "chart-properties", NSChart, "hole-size"); ok {
		if f, ok := Percent(v); ok {
			g.HoleSize = f * 100
		}
	}
	g.Labels = r.labels(pName, chart.Labels{}, g)
	return g
}

// axis reads a chart:axis (ODF 1.3 Part 3, 11.6), nil for a z axis.
func (r *chartReader) axis(ax *Element) *chart.Axis {
	dimension := ax.A(NSChart, "dimension")
	if dimension == "z" {
		return nil
	}
	name := ax.A(NSChart, "style-name")
	a := &chart.Axis{Kind: chart.ValueAxis, Min: math.NaN(), Max: math.NaN(), MajorUnit: math.NaN(), MinorUnit: math.NaN()}
	if dimension == "x" {
		a.Kind = chart.CategoryAxis
		a.Position = chart.AxisBottom
	} else {
		a.Position = chart.AxisLeft
	}
	if strings.HasPrefix(ax.A(NSChart, "name"), "secondary") {
		a.Crosses = chart.CrossMax
		if dimension == "x" {
			a.Position = chart.AxisTop
		} else {
			a.Position = chart.AxisRight
		}
	}
	a.Deleted = !r.boolProp(name, "visible", true)
	if !r.boolProp(name, "display-label", true) {
		a.Labels = chart.LabelNone
	}
	if r.boolProp(name, "logarithmic", false) {
		a.Log = 10
	}
	a.Min, a.Max = r.numProp(name, "minimum"), r.numProp(name, "maximum")
	a.MajorUnit = r.numProp(name, "interval-major")
	if div := r.numProp(name, "interval-minor-divisor"); div >= 1 && a.MajorUnit > 0 {
		a.MinorUnit = a.MajorUnit / div
	}
	a.Reverse = r.boolProp(name, "reverse-direction", false)
	switch v, _ := r.prop(name, "chart-properties", NSChart, "axis-position"); v {
	case "start", "":
	case "end":
		a.Crosses = chart.CrossMax
	default:
		if f, err := strconv.ParseFloat(v, 64); err == nil && !math.IsInf(f, 0) && !math.IsNaN(f) && f != 0 {
			a.Crosses, a.CrossAt = chart.CrossValue, f
		}
	}
	switch v, _ := r.prop(name, "chart-properties", NSChart, "axis-label-position"); v {
	case "outside-start":
		a.Labels = keepNone(a.Labels, chart.LabelLow)
	case "outside-end":
		a.Labels = keepNone(a.Labels, chart.LabelHigh)
	}
	a.MajorTicks = ticks(r.boolProp(name, "tick-marks-major-inner", false), r.boolProp(name, "tick-marks-major-outer", true))
	a.MinorTicks = ticks(r.boolProp(name, "tick-marks-minor-inner", false), r.boolProp(name, "tick-marks-minor-outer", false))
	a.Line = r.lineOf(name, &chart.Stroke{Color: compose.Color{R: 0xb3 / 255.0, G: 0xb3 / 255.0, B: 0xb3 / 255.0, A: 1}})
	a.Style = r.textStyle(name, r.textDefault)
	// Labels break only when the style asks (ODF 1.3 Part 3, 20.25
	// text:line-break; chart2's axis default is off) and overlap only when
	// it allows it (20.21 chart:text-overlap).
	lb, _ := r.prop(name, "chart-properties", NSText, "line-break")
	a.NoBreak = lb != "true"
	a.Overlap = r.boolProp(name, "text-overlap", false)
	// An axis linked to its source's format carries the format the
	// application saved for it, which a chart's own table, holding bare
	// numbers, cannot override (ODF 1.3 Part 3, 20.20
	// chart:link-data-style-to-source).
	if ds := r.styles[name].A(NSStyle, "data-style-name"); ds != "" {
		a.Format = r.numberFormat(ds)
	}
	for _, g := range ax.All(NSChart, "grid") {
		gName := g.A(NSChart, "style-name")
		line := r.lineOf(gName, &chart.Stroke{Color: compose.Color{R: 0xb3 / 255.0, G: 0xb3 / 255.0, B: 0xb3 / 255.0, A: 1}})
		if g.A(NSChart, "class") == "minor" {
			a.MinorGrid = line
		} else {
			a.MajorGrid = line
		}
	}
	if t := ax.Child(NSChart, "title"); t != nil {
		a.Title = r.title(t, 9, false)
		if a.Title != nil && dimension != "x" {
			if _, ok := r.prop(t.A(NSChart, "style-name"), "chart-properties", NSStyle, "rotation-angle"); !ok {
				a.Title.Style.Rotation = 90
			}
		}
	}
	return a
}

// keepNone keeps LabelNone over a label position.
func keepNone(cur, pos chart.LabelPosition) chart.LabelPosition {
	if cur == chart.LabelNone {
		return cur
	}
	return pos
}

// ticks combines inner and outer tick flags.
func ticks(inner, outer bool) chart.TickMark {
	switch {
	case inner && outer:
		return chart.TickCross
	case inner:
		return chart.TickIn
	case outer:
		return chart.TickOut
	}
	return chart.TickNone
}

// series reads a chart:series (ODF 1.3 Part 3, 11.11).
func (r *chartReader) series(se *Element, g *chart.Group, i int, cats chart.Data) *chart.Series {
	name := se.A(NSChart, "style-name")
	s := &chart.Series{Index: i}
	addr := se.A(NSChart, "values-cell-range-address")
	s.Values = r.data(addr, false)
	if s.Values.Values == nil && r.table != nil {
		// A table without the range marks takes the series in column
		// order.
		s.Values = r.table.column(r.table.firstData + i)
		s.Values.Ref = addr
	}
	if !s.Values.Cached && s.Values.Ref != "" {
		r.warn(CodeChartNoData, "a series has no values in the chart's table and is drawn empty")
	}
	s.Categories = cats
	if r.table != nil {
		if row, col, ok := r.table.find(addr); ok && row >= r.table.headerRows {
			name, rowCats := r.table.headers(row, col, addr)
			if s.Name == "" {
				s.Name = name
			}
			if rowCats.Values != nil {
				s.Categories = rowCats
			}
		}
	}
	if se.A(NSChart, "label-cell-address") == "" {
		// LibreOffice names a series without a label cell after its range:
		// "Column B" for a range down a column, "Row 3" across a row.
		s.Name = rangeName(addr)
	}
	if label := se.A(NSChart, "label-cell-address"); label != "" {
		if d := r.data(label, true); len(d.Values) > 0 && d.Values[0].Text != "" {
			s.Name = d.Values[0].Text
		}
	}
	if s.Name == "" {
		s.Name = "Series" + strconv.Itoa(i+1)
	}
	domains := se.All(NSChart, "domain")
	if g.Kind == chart.Bubble {
		s.Sizes = s.Values
		s.Values = chart.Data{}
		if len(domains) > 0 {
			s.Values = r.data(domains[0].A(NSTable, "cell-range-address"), false)
		}
		if len(domains) > 1 {
			s.Categories = r.data(domains[1].A(NSTable, "cell-range-address"), false)
		}
	} else if len(domains) > 0 {
		s.Categories = r.data(domains[0].A(NSTable, "cell-range-address"), false)
	}
	labelFormat := ""
	if st := r.styles[name]; st != nil && !r.boolProp(name, "link-data-style-to-source", false) {
		// The series' data style formats its data labels, not the axis.
		labelFormat = r.numberFormat(st.A(NSStyle, "data-style-name"))
	}
	switch g.Kind {
	case chart.Line, chart.Scatter, chart.Radar:
		s.Border = r.stroke(name, nil)
		if s.Border != nil && s.Border.None && g.Kind != chart.Radar {
			s.Border = &chart.Stroke{None: true}
		}
		if g.Filled {
			s.Fill = r.fill(name, nil)
		}
		s.Marker = r.marker(name, g.Kind)
		if v, ok := r.prop(name, "chart-properties", NSChart, "interpolation"); ok && v != "none" {
			s.Smooth = true
		}
	case chart.Bar, chart.Area, chart.Pie, chart.Doughnut, chart.Bubble, chart.Unsupported:
		s.Fill = r.fill(name, nil)
		s.Border = r.lineOf(name, nil)
	}
	if v := r.numProp(name, "pie-offset"); v > 0 {
		s.Explosion = v
	}
	s.Labels = r.labels(name, g.Labels, g)
	s.Labels.Format = labelFormat
	r.labelFrame(se, &s.Labels)
	if st := r.styles[name]; st != nil {
		if pf := st.A(NSStyle, "percentage-data-style-name"); pf != "" {
			s.Labels.PercentFormat = r.numberFormat(pf)
		}
	}
	idx := 0
	for _, dp := range se.All(NSChart, "data-point") {
		rep := 1
		if v, err := strconv.Atoi(dp.A(NSChart, "repeated")); err == nil && v > 1 {
			rep = min(v, chart.MaxPoints)
		}
		dName := dp.A(NSChart, "style-name")
		if dName != "" && r.styles[dName] != nil {
			for k := 0; k < rep && idx+k < chart.MaxPoints; k++ {
				pt := r.point(dName, idx+k, s, g)
				if pt.HasLabels {
					r.labelFrame(dp, &pt.Labels)
				}
				s.Points = append(s.Points, pt)
			}
		}
		idx += rep
		if idx >= chart.MaxPoints {
			break
		}
	}
	for _, rc := range se.All(NSChart, "regression-curve") {
		if t, ok := r.trendline(rc.A(NSChart, "style-name")); ok && len(s.Trendlines) < maxTrendlines {
			s.Trendlines = append(s.Trendlines, t)
		}
		if eq := rc.Child(NSChart, "equation"); eq != nil && (eq.A(NSChart, "display-equation") == "true" || eq.A(NSChart, "display-r-square") == "true") {
			r.warn(CodeChartUnsupported, "a regression curve's equation is not drawn")
		}
	}
	if se.Child(NSChart, "error-indicator") != nil {
		r.warn(CodeChartUnsupported, "error indicators are not drawn")
	}
	return s
}

// point reads the format of a chart:data-point (ODF 1.3 Part 3, 11.12).
func (r *chartReader) point(name string, idx int, s *chart.Series, g *chart.Group) chart.Point {
	p := chart.Point{Index: idx}
	if _, ok := r.prop(name, "graphic-properties", NSDraw, "fill-color"); ok {
		p.Fill = r.fill(name, nil)
	} else if v, ok := r.prop(name, "graphic-properties", NSDraw, "fill"); ok && v == "none" {
		p.Fill = &chart.Paint{None: true}
	}
	if b := r.stroke(name, nil); b != nil {
		p.Border = b
		if b.None && (g.Kind != chart.Line && g.Kind != chart.Scatter) {
			p.Border = &chart.Stroke{None: true}
		}
	}
	if v := r.numProp(name, "pie-offset"); !math.IsNaN(v) {
		p.Explosion, p.HasExplosion = v, true
	}
	_, number := r.prop(name, "chart-properties", NSChart, "data-label-number")
	_, text := r.prop(name, "chart-properties", NSChart, "data-label-text")
	linked := r.boolProp(name, "link-data-style-to-source", false)
	ds := r.styles[name].A(NSStyle, "data-style-name")
	if number || text || linked {
		p.Labels, p.HasLabels = r.labels(name, s.Labels, g), true
		if linked {
			// A point linked to its source's format shows the value as
			// the cell does, General in the embedded table.
			p.Labels.Format = "General"
		}
	} else if ds != "" {
		// A point with a number format of its own shows its series'
		// labels in it (ODF 1.3 Part 3, 16.29 style:data-style-name on a
		// chart:data-point's style).
		p.Labels, p.HasLabels = s.Labels, true
		p.Labels.Format = r.numberFormat(ds)
		if p.Labels.Format == "" {
			p.Labels.Format = "General"
		}
	}
	if _, ok := r.prop(name, "chart-properties", NSChart, "symbol-type"); ok {
		p.Marker, p.HasMarker = r.marker(name, g.Kind), true
	}
	return p
}

// symbols maps chart:symbol-name (ODF 1.3 Part 3, 20.55) to the marker
// symbols.
var symbols = map[string]chart.MarkerSymbol{
	"square": chart.MarkerSquare, "diamond": chart.MarkerDiamond, "arrow-down": chart.MarkerArrowDown,
	"arrow-up": chart.MarkerArrowUp, "arrow-right": chart.MarkerArrowRight, "arrow-left": chart.MarkerArrowLeft,
	"bow-tie": chart.MarkerBowTie, "hourglass": chart.MarkerSandglass, "circle": chart.MarkerCircle,
	"star": chart.MarkerStar, "x": chart.MarkerX, "plus": chart.MarkerPlus, "asterisk": chart.MarkerAsterisk,
	"horizontal-bar": chart.MarkerHorizontalBar, "vertical-bar": chart.MarkerVerticalBar,
}

// marker reads a series' symbol (ODF 1.3 Part 3, 20.56 chart:symbol-type,
// 20.57 chart:symbol-width).
func (r *chartReader) marker(name string, kind chart.Kind) chart.Marker {
	m := chart.Marker{Symbol: chart.MarkerNone, Size: 250 * 72 / 2540.0}
	switch v, _ := r.prop(name, "chart-properties", NSChart, "symbol-type"); v {
	case "automatic":
		m.Symbol = chart.MarkerAuto
	case "named-symbol":
		if s, ok := symbols[r.propOr(name, "chart-properties", NSChart, "symbol-name", "")]; ok {
			m.Symbol = s
		}
	case "":
		if kind == chart.Scatter {
			m.Symbol = chart.MarkerAuto
		}
	}
	if v, ok := r.prop(name, "chart-properties", NSChart, "symbol-width"); ok {
		if w, ok := Length(v); ok && w > 0 {
			m.Size = w
		}
	}
	return m
}

// maxTrendlines caps the regression curves of one series. Anti-DoS guard,
// not spec-mandated: LibreOffice offers one a series, Office a few.
const maxTrendlines = 16

// trendline reads the regression curve of style name (ODF 1.3 Part 3,
// 11.15 chart:regression-curve; 20.86 chart:regression-type and the
// chart:regression-* properties), false for none.
func (r *chartReader) trendline(name string) (chart.Trendline, bool) {
	var t chart.Trendline
	v, _ := r.prop(name, "chart-properties", NSChart, "regression-type")
	switch v {
	case "linear":
		t.Kind = chart.TrendLinear
	case "logarithmic":
		t.Kind = chart.TrendLogarithmic
	case "exponential":
		t.Kind = chart.TrendExponential
	case "power":
		t.Kind = chart.TrendPower
	case "polynomial":
		t.Kind = chart.TrendPolynomial
	case "moving-average":
		t.Kind = chart.TrendMovingAverage
	default:
		return t, false
	}
	if v := r.numProp(name, "regression-max-degree"); v >= 2 && v <= 6 {
		t.Order = int(v) // fits int: at most 6
	}
	if v := r.numProp(name, "regression-period"); v >= 2 && v <= chart.MaxPoints {
		t.Period = int(v) // fits int: at most MaxPoints
	}
	switch v, _ := r.prop(name, "chart-properties", NSChart, "regression-moving-type"); v {
	case "central":
		t.Moving = chart.MovingCentral
	case "averaged-abscissa":
		t.Moving = chart.MovingAveragedAbscissa
	}
	ext := func(local string) float64 {
		for _, ns := range []string{NSChart, NSLOExt} {
			if v, ok := r.prop(name, "chart-properties", ns, local); ok {
				if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil && f > 0 && !math.IsInf(f, 0) {
					return f
				}
			}
		}
		return 0
	}
	t.Forward, t.Backward = ext("regression-extrapolate-forward"), ext("regression-extrapolate-backward")
	if r.boolProp(name, "regression-force-intercept", false) {
		if v := r.numProp(name, "regression-intercept-value"); !math.IsNaN(v) {
			t.Intercept, t.HasIntercept = v, true
		}
	}
	t.Name, _ = r.prop(name, "chart-properties", NSChart, "regression-name")
	if s := r.stroke(name, nil); s != nil {
		t.Line = s
	}
	return t, true
}

// labelFrame reads the fill and border of the data labels of a
// chart:series or chart:data-point el from the style of its
// chart:data-label child (ODF 1.3 Part 3, 11.13), into l.
func (r *chartReader) labelFrame(el *Element, l *chart.Labels) {
	name := el.Child(NSChart, "data-label").A(NSChart, "style-name")
	if name == "" || r.styles[name] == nil {
		return
	}
	if f := r.fill(name, nil); f != nil {
		l.Frame.Fill = f
	}
	if b := r.stroke(name, nil); b != nil {
		l.Frame.Border = b
	}
}

// labels reads a style's data labels over the inherited ones (ODF 1.3
// Part 3, 20.38 chart:data-label-number, 20.39 chart:data-label-symbol,
// 20.40 chart:data-label-text, 20.60 chart:label-position).
func (r *chartReader) labels(name string, inherit chart.Labels, g *chart.Group) chart.Labels {
	l := inherit
	if v, ok := r.prop(name, "chart-properties", NSChart, "data-label-number"); ok {
		l.ShowValue = v == "value" || v == "value-and-percentage"
		l.ShowPercent = v == "percentage" || v == "value-and-percentage"
	}
	if _, ok := r.prop(name, "chart-properties", NSChart, "data-label-text"); ok {
		l.ShowCategory = r.boolProp(name, "data-label-text", false)
	}
	if _, ok := r.prop(name, "chart-properties", NSChart, "data-label-series"); ok {
		l.ShowSeries = r.boolProp(name, "data-label-series", false)
	}
	if _, ok := r.prop(name, "chart-properties", NSChart, "data-label-symbol"); ok {
		l.ShowKey = r.boolProp(name, "data-label-symbol", false)
	}
	switch v, _ := r.prop(name, "chart-properties", NSChart, "label-position"); v {
	case "center":
		l.Placement = chart.LabelCenter
	case "top":
		l.Placement = chart.LabelAbove
	case "bottom":
		l.Placement = chart.LabelBelow
	case "left":
		l.Placement = chart.LabelLeft
	case "right":
		l.Placement = chart.LabelRight
	case "inside":
		l.Placement = chart.LabelInsideEnd
	case "outside":
		l.Placement = chart.LabelOutsideEnd
	case "near-origin":
		l.Placement = chart.LabelInsideBase
	case "avoid-overlap":
		l.Placement = chart.LabelBestFit
	}
	if st := r.styles[name]; st != nil {
		if sep := st.Child(NSStyle, "chart-properties").Child(NSChart, "label-separator"); sep != nil {
			l.Separator = paraText(sep)
		} else if l.Separator == "" {
			l.Separator = " "
		}
	}
	l.Style = r.textStyle(name, r.textDefault)
	if g.Kind == chart.Pie || g.Kind == chart.Doughnut {
		if l.Placement == chart.LabelAuto {
			l.Placement = chart.LabelBestFit
		}
	}
	return l
}

// data returns the data of a cell range address from the embedded table,
// with the address as the reference; nil values when the table does not
// hold the range.
func (r *chartReader) data(addr string, labels bool) chart.Data {
	d := chart.Data{Ref: addr}
	if addr == "" || r.table == nil {
		return d
	}
	row, col, ok := r.table.find(addr)
	if !ok {
		return d
	}
	if labels && row < r.table.headerRows {
		d.Values, d.Cached = []chart.Value{r.table.rows[row][col].value}, true
		return d
	}
	d = r.table.run(row, col, addr)
	d.Ref = addr
	return d
}

// kids returns e's children, nil for a nil e.
func kids(e *Element) []*Element {
	if e == nil {
		return nil
	}
	return e.Children
}
