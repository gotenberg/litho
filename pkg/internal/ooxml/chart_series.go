package ooxml

import (
	"math"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/chart"
)

// group reads one chart type element of the plot area, nil for an element
// that is not one.
func (p *chartReader) group(e *Element) *chart.Group {
	g := &chart.Group{XAxis: -1, YAxis: -1, GapWidth: 150, BubbleScale: 100}
	switch e.Local {
	case "barChart", "bar3DChart":
		g.Kind = chart.Bar
		g.Horizontal = childVal(e, "barDir") == "bar"
	case "lineChart", "line3DChart", "stockChart":
		g.Kind = chart.Line
	case "areaChart", "area3DChart":
		g.Kind = chart.Area
	case "pieChart", "pie3DChart", "ofPieChart":
		g.Kind = chart.Pie
		g.VaryColors = true
		if e.Local == "ofPieChart" {
			p.warn(CodeChartUnsupported, "a pie-of-pie or bar-of-pie chart is drawn as a pie")
		}
	case "doughnutChart":
		g.Kind = chart.Doughnut
		g.VaryColors = true
		g.HoleSize = 50
	case "scatterChart":
		g.Kind = chart.Scatter
	case "bubbleChart":
		g.Kind = chart.Bubble
	case "radarChart":
		g.Kind = chart.Radar
		g.Filled = childVal(e, "radarStyle") == "filled"
	case "surfaceChart", "surface3DChart":
		p.warn(CodeChartUnsupported, "a surface chart is not drawn")
		return nil
	default:
		return nil
	}
	if strings.HasSuffix(e.Local, "3DChart") {
		g.ThreeD = true
		p.warn(CodeChartUnsupported, "a 3D "+g.Kind.String()+" chart is drawn flat")
	}
	if e.Local == "stockChart" {
		p.warn(CodeChartUnsupported, "a stock chart is drawn as a line chart")
	}
	switch childVal(e, "grouping") {
	case "stacked":
		g.Grouping = chart.Stacked
	case "percentStacked":
		g.Grouping = chart.PercentStacked
	}
	g.VaryColors = boolVal(e, "varyColors", g.VaryColors && !p.mso2007)
	if v := floatVal(e, "gapWidth"); !math.IsNaN(v) {
		g.GapWidth = v
	}
	if v := floatVal(e, "overlap"); !math.IsNaN(v) {
		g.Overlap = v
	}
	if v := floatVal(e, "holeSize"); !math.IsNaN(v) {
		g.HoleSize = v
	}
	if v := floatVal(e, "firstSliceAng"); !math.IsNaN(v) {
		g.FirstSliceAngle = v
	}
	if v := floatVal(e, "bubbleScale"); !math.IsNaN(v) {
		g.BubbleScale = v
	}
	var ids []string
	for _, k := range e.Kids() {
		if k.Is(NSChart, "axId") {
			ids = append(ids, attrOr(k, "val"))
		}
	}
	if len(ids) >= 2 {
		if i, ok := p.axes[ids[0]]; ok {
			g.XAxis = i
		}
		if i, ok := p.axes[ids[1]]; ok {
			g.YAxis = i
		}
	}
	if g.Kind == chart.Bar && g.Horizontal {
		// A horizontal bar group draws its category axis on the left.
		if a := p.c.Axes; g.XAxis >= 0 && g.XAxis < len(a) && a[g.XAxis].Position == chart.AxisBottom {
			a[g.XAxis].Position = chart.AxisLeft
		}
	}
	g.Labels = p.labels(e.Child(NSChart, "dLbls"), chart.Labels{})
	if ln := e.Child(NSChart, "hiLowLines"); ln != nil {
		p.warn(CodeChartUnsupported, "a chart's high-low lines are not drawn")
	}
	scatterLines := true
	if g.Kind == chart.Scatter {
		switch childVal(e, "scatterStyle") {
		case "marker", "none":
			scatterLines = childVal(e, "scatterStyle") == "none"
		}
	}
	lineMarkers := boolVal(e, "marker", true)
	var idx []int
	for _, se := range e.Kids() {
		if !se.Is(NSChart, "ser") {
			continue
		}
		if len(g.Series) >= chart.MaxSeries {
			p.warn(CodeChartUnsupported, "series past the chart's limit are not drawn")
			break
		}
		s := p.series(se, g, scatterLines, lineMarkers)
		g.Series = append(g.Series, s)
		i, ok := Int(childVal(se, "idx"))
		if !ok {
			i = s.Index
		}
		idx = append(idx, max(0, min(i, chart.MaxSeries*8)))
	}
	// The series draw in their c:order, and take their automatic colour
	// and marker from their c:idx (oox's SeriesConverter formats a series
	// by its mnIndex, the idx).
	sortSeries(g.Series, idx)
	return g
}

// maxTrendlines caps the trendlines of one series. Anti-DoS guard, not
// spec-mandated: Office offers a few a series.
const maxTrendlines = 16

// trendline reads a c:trendline (ECMA-376 Part 1, 21.2.2.211), false for
// an unknown type. Its equation and R² label are not drawn.
func (p *chartReader) trendline(e *Element) (chart.Trendline, bool) {
	var t chart.Trendline
	switch childVal(e, "trendlineType") {
	case "linear", "":
		t.Kind = chart.TrendLinear
	case "exp":
		t.Kind = chart.TrendExponential
	case "log":
		t.Kind = chart.TrendLogarithmic
	case "poly":
		t.Kind = chart.TrendPolynomial
	case "power":
		t.Kind = chart.TrendPower
	case "movingAvg":
		t.Kind = chart.TrendMovingAverage
	default:
		return t, false
	}
	// 21.2.2.130 order defaults to 2, 21.2.2.136 period to 2.
	t.Order, t.Period = 2, 2
	if v, ok := Int(childVal(e, "order")); ok && v >= 2 && v <= 6 {
		t.Order = v
	}
	if v, ok := Int(childVal(e, "period")); ok && v >= 2 && v <= chart.MaxPoints {
		t.Period = v
	}
	if v := floatVal(e, "forward"); v > 0 {
		t.Forward = v
	}
	if v := floatVal(e, "backward"); v > 0 {
		t.Backward = v
	}
	if v := floatVal(e, "intercept"); !math.IsNaN(v) {
		t.Intercept, t.HasIntercept = v, true
	}
	t.Name = textOf(e.Child(NSChart, "name"))
	if ln := e.Child(NSChart, "spPr").Child(NSA, "ln"); ln != nil {
		t.Line = p.stroke(ln, nil)
	}
	if boolVal(e, "dispEq", false) || boolVal(e, "dispRSqr", false) {
		p.warn(CodeChartUnsupported, "a trendline's equation is not drawn")
	}
	return t, true
}

// sortSeries orders series by their c:order, which their Index holds,
// stable (ECMA-376 Part 1, 21.2.2.128 order), then gives each its c:idx,
// idx in the series' file order, as Index (21.2.2.84 idx).
func sortSeries(ss []*chart.Series, idx []int) {
	of := make(map[*chart.Series]int, len(ss))
	for i, s := range ss {
		if i < len(idx) {
			of[s] = idx[i]
		}
	}
	for i := 1; i < len(ss); i++ {
		for j := i; j > 0 && ss[j].Index < ss[j-1].Index; j-- {
			ss[j], ss[j-1] = ss[j-1], ss[j]
		}
	}
	for _, s := range ss {
		if v, ok := of[s]; ok {
			s.Index = v
		}
	}
}

// filled reports a kind whose series are areas rather than lines.
func filled(g *chart.Group) bool {
	return g.Kind == chart.Bar || g.Kind == chart.Area || g.Kind == chart.Pie || g.Kind == chart.Doughnut || g.Kind == chart.Bubble || g.Kind == chart.Radar && g.Filled
}

// series reads one c:ser (ECMA-376 Part 1, 21.2.2.167 and its kind's
// variants).
func (p *chartReader) series(e *Element, g *chart.Group, scatterLines, lineMarkers bool) *chart.Series {
	s := &chart.Series{}
	idx, _ := Int(childVal(e, "idx"))
	order, ok := Int(childVal(e, "order"))
	if !ok {
		order = idx
	}
	s.Index = max(0, min(order, chart.MaxSeries*8))
	s.Name = richText(e.Child(NSChart, "tx"))
	switch g.Kind {
	case chart.Scatter, chart.Bubble:
		s.Categories = p.data(e.Child(NSChart, "xVal"), "x values")
		s.Values = p.data(e.Child(NSChart, "yVal"), "y values")
		s.Sizes = p.data(e.Child(NSChart, "bubbleSize"), "bubble sizes")
	case chart.Bar, chart.Line, chart.Area, chart.Pie, chart.Doughnut, chart.Radar, chart.Unsupported:
		s.Categories = p.data(e.Child(NSChart, "cat"), "categories")
		s.Values = p.data(e.Child(NSChart, "val"), "values")
	}
	spPr := e.Child(NSChart, "spPr")
	if filled(g) {
		f := p.frame(spPr, chart.Frame{})
		s.Fill, s.Border = f.Fill, f.Border
		if s.Border == nil {
			s.Border = p.autoSeriesBorder()
		}
		if s.Border != nil && s.Border.None {
			s.Border = nil
		}
	} else {
		width := themeLineWidth * float64(seriesLineScale(p.style)) / 100
		s.Border = p.stroke(spPr.Child(NSA, "ln"), &chart.Stroke{Color: p.c.Palette[0], Width: width})
		if spPr.Child(NSA, "ln").Child(NSA, "solidFill") == nil && !s.Border.None {
			// The automatic colour, from the palette once every series is
			// known.
			p.autoLines = append(p.autoLines, s)
		}
		if g.Kind == chart.Scatter && !scatterLines && spPr.Child(NSA, "ln") == nil {
			s.Border = &chart.Stroke{None: true}
		}
		if f, ok := p.fill(spPr); ok && g.Kind == chart.Radar {
			s.Fill = f
		}
	}
	if g.Kind == chart.Line || g.Kind == chart.Scatter || g.Kind == chart.Radar {
		s.Marker = p.marker(e.Child(NSChart, "marker"), lineMarkers)
	}
	s.Smooth = boolVal(e, "smooth", g.Kind == chart.Line && !p.mso2007)
	if v := floatVal(e, "explosion"); v > 0 {
		s.Explosion = v
	}
	s.Labels = p.labels(e.Child(NSChart, "dLbls"), g.Labels)
	for _, dp := range e.Kids() {
		if dp.Is(NSChart, "dPt") && len(s.Points) < chart.MaxPoints {
			pt := p.dataPoint(dp, g)
			if filled(g) && spPr != nil && dp.Child(NSChart, "spPr") == nil {
				// A point without its own shape properties takes the
				// series' over the varied colour (oox's DataPointConverter).
				pt.Fill, pt.Border = s.Fill, s.Border
			}
			s.Points = append(s.Points, pt)
		}
	}
	p.pointLabels(e.Child(NSChart, "dLbls"), s)
	for _, tl := range e.Kids() {
		if tl.Is(NSChart, "trendline") && len(s.Trendlines) < maxTrendlines {
			if t, ok := p.trendline(tl); ok {
				s.Trendlines = append(s.Trendlines, t)
			}
		}
	}
	if e.Child(NSChart, "errBars") != nil {
		p.warn(CodeChartUnsupported, "error bars are not drawn")
	}
	return s
}

// seriesLineScale returns the width of a line series in percent of the
// theme's thinnest line for a chart style (oox's spLinearSeriesLines).
func seriesLineScale(style int) int {
	switch {
	case style <= 8:
		return 300
	case style <= 24, style >= 33:
		return 500
	}
	return 700
}

// marker reads a c:marker (ECMA-376 Part 1, 21.2.2.106), the symbols
// mapped as LibreOffice maps them; a line series without one shows the
// automatic symbol when its group shows markers.
func (p *chartReader) marker(e *Element, show bool) chart.Marker {
	m := chart.Marker{Symbol: chart.MarkerAuto, Size: 5}
	if !show {
		m.Symbol = chart.MarkerNone
	}
	if e == nil {
		return m
	}
	switch childVal(e, "symbol") {
	case "none":
		m.Symbol = chart.MarkerNone
	case "square":
		m.Symbol = chart.MarkerSquare
	case "diamond":
		m.Symbol = chart.MarkerDiamond
	case "triangle":
		m.Symbol = chart.MarkerArrowUp
	case "x":
		m.Symbol = chart.MarkerX
	case "star":
		m.Symbol = chart.MarkerAsterisk
	case "dot":
		m.Symbol = chart.MarkerArrowRight
	case "dash":
		m.Symbol = chart.MarkerHorizontalBar
	case "circle":
		m.Symbol = chart.MarkerCircle
	case "plus":
		m.Symbol = chart.MarkerPlus
	case "auto":
		m.Symbol = chart.MarkerAuto
	}
	if v, ok := Int(childVal(e, "size")); ok && v >= 2 && v <= 72 {
		m.Size = float64(v)
	}
	if sp := e.Child(NSChart, "spPr"); sp != nil {
		f := p.frame(sp, chart.Frame{})
		m.Fill, m.Border = f.Fill, f.Border
		if m.Fill != nil && m.Fill.None {
			m.Fill = nil
			if m.Border != nil && !m.Border.None {
				m.Fill = &chart.Paint{Color: m.Border.Color}
			}
		}
	}
	return m
}

// dataPoint reads a c:dPt.
func (p *chartReader) dataPoint(e *Element, g *chart.Group) chart.Point {
	var pt chart.Point
	if v, ok := Int(childVal(e, "idx")); ok && v >= 0 {
		pt.Index = v
	}
	sp := e.Child(NSChart, "spPr")
	if filled(g) {
		f := p.frame(sp, chart.Frame{})
		pt.Fill, pt.Border = f.Fill, f.Border
	} else if ln := sp.Child(NSA, "ln"); ln != nil {
		pt.Border = p.stroke(ln, nil)
	}
	if m := e.Child(NSChart, "marker"); m != nil {
		pt.Marker, pt.HasMarker = p.marker(m, true), true
	}
	if v := floatVal(e, "explosion"); !math.IsNaN(v) {
		pt.Explosion, pt.HasExplosion = math.Max(0, v), true
	}
	return pt
}

// labels reads a c:dLbls over the inherited labels (ECMA-376 Part 1,
// 21.2.2.49): a flag the element leaves out keeps the inherited value.
func (p *chartReader) labels(e *Element, inherit chart.Labels) chart.Labels {
	l := inherit
	if e == nil {
		return l
	}
	if boolVal(e, "delete", false) {
		return chart.Labels{}
	}
	p.labelFlags(e, &l)
	if l.Style.Size == 0 {
		l.Style = p.textStyle(textProps{}, 10, false, 1)
	}
	if tp := e.Child(NSChart, "txPr"); tp != nil {
		l.Style = p.textStyle(p.textProps(tp), 10, false, 1)
	}
	if sp := e.Child(NSChart, "spPr"); sp != nil {
		l.Frame = p.frame(sp, chart.Frame{})
	}
	return l
}

// labelFlags reads the label parts, position, separator and number format
// of a c:dLbls or c:dLbl into l.
func (p *chartReader) labelFlags(e *Element, l *chart.Labels) {
	flag := func(name string, dst *bool) {
		if e.Child(NSChart, name) != nil {
			*dst = boolVal(e, name, false)
		}
	}
	flag("showVal", &l.ShowValue)
	flag("showCatName", &l.ShowCategory)
	flag("showSerName", &l.ShowSeries)
	flag("showPercent", &l.ShowPercent)
	flag("showLegendKey", &l.ShowKey)
	if sep := e.Child(NSChart, "separator"); sep != nil {
		l.Separator = sep.Text
	}
	if nf := e.Child(NSChart, "numFmt"); nf != nil {
		l.Format = attrOr(nf, "formatCode")
	}
	switch childVal(e, "dLblPos") {
	case "bestFit":
		l.Placement = chart.LabelBestFit
	case "b":
		l.Placement = chart.LabelBelow
	case "ctr":
		l.Placement = chart.LabelCenter
	case "inBase":
		l.Placement = chart.LabelInsideBase
	case "inEnd":
		l.Placement = chart.LabelInsideEnd
	case "l":
		l.Placement = chart.LabelLeft
	case "outEnd":
		l.Placement = chart.LabelOutsideEnd
	case "r":
		l.Placement = chart.LabelRight
	case "t":
		l.Placement = chart.LabelAbove
	}
}

// pointLabels reads the c:dLbl elements of a series' c:dLbls into point
// overrides.
func (p *chartReader) pointLabels(e *Element, s *chart.Series) {
	for _, d := range e.Kids() {
		if !d.Is(NSChart, "dLbl") {
			continue
		}
		idx, ok := Int(childVal(d, "idx"))
		if !ok || idx < 0 {
			continue
		}
		l := s.Labels
		if boolVal(d, "delete", false) {
			l = chart.Labels{}
		} else {
			p.labelFlags(d, &l)
			if tp := d.Child(NSChart, "txPr"); tp != nil {
				l.Style = p.textStyle(p.textProps(tp), 10, false, 1)
			}
			if rich := d.Child(NSChart, "tx").Child(NSChart, "rich"); rich != nil {
				l.Custom = labelFields(rich, d)
				l.Style = p.textStyle(p.textProps(rich), l.Style.Size, l.Style.Bold, 1)
			}
		}
		pt := s.Points
		found := false
		for i := range pt {
			if pt[i].Index == idx {
				pt[i].Labels, pt[i].HasLabels, found = l, true, true
			}
		}
		if !found && len(s.Points) < chart.MaxPoints {
			s.Points = append(s.Points, chart.Point{Index: idx, Labels: l, HasLabels: true})
		}
	}
}

// data reads a data source element (c:cat, c:val, c:xVal, c:yVal,
// c:bubbleSize; ECMA-376 Part 1, 21.2.2.24 and 21.2.2.224): its reference
// and its cache, or its literal values.
func (p *chartReader) data(e *Element, what string) chart.Data {
	if e == nil {
		return chart.Data{}
	}
	d := readData(e)
	if !d.Cached && d.Ref != "" {
		p.warn(CodeChartNoCache, "a series' "+what+" have no cached values in the file and are drawn empty")
	}
	return d
}

// readData reads the reference and cache of a data source element.
func readData(e *Element) chart.Data {
	var d chart.Data
	for _, src := range e.Kids() {
		if src.Space != NSChart {
			continue
		}
		var cache *Element
		switch src.Local {
		case "numRef", "strRef":
			d.Ref = textOf(src.Child(NSChart, "f"))
			cache = src.Child(NSChart, "numCache")
			if cache == nil {
				cache = src.Child(NSChart, "strCache")
			}
		case "multiLvlStrRef":
			d.Ref = textOf(src.Child(NSChart, "f"))
			if mc := src.Child(NSChart, "multiLvlStrCache"); mc != nil {
				// The innermost level labels the categories.
				if lvl := mc.Child(NSChart, "lvl"); lvl != nil {
					cache = &Element{Space: NSChart, Local: "strCache", Children: append([]*Element{mc.Child(NSChart, "ptCount")}, lvl.Children...)}
				}
			}
		case "numLit", "strLit":
			cache = src
		}
		if cache != nil {
			d.Cached = true
			d.Values, d.Format = readCache(cache)
		}
		break
	}
	return d
}

// readCache reads a c:numCache, c:strCache or literal: c:ptCount points,
// each c:pt at its c:idx, missing ones empty.
func readCache(cache *Element) ([]chart.Value, string) {
	numeric := cache.Local == "numCache" || cache.Local == "numLit"
	format := textOf(cache.Child(NSChart, "formatCode"))
	n, ok := Int(childVal(cache, "ptCount"))
	if !ok || n < 0 {
		n = 0
	}
	for _, pt := range cache.Kids() {
		if pt != nil && pt.Is(NSChart, "pt") {
			if i, ok := Int(attrOr(pt, "idx")); ok && i >= n && i < chart.MaxPoints {
				n = i + 1
			}
		}
	}
	n = min(n, chart.MaxPoints)
	vals := make([]chart.Value, n)
	next := 0
	for _, pt := range cache.Kids() {
		if pt == nil || !pt.Is(NSChart, "pt") {
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
		text := textOf(pt.Child(NSChart, "v"))
		if numeric {
			if v := parseFloat(text); !math.IsNaN(v) {
				vals[i] = chart.Value{Kind: chart.Number, Number: v, Text: text}
				continue
			}
		}
		vals[i] = chart.Str(text)
	}
	return vals, format
}

// autoSeriesBorder returns the automatic outline of a filled series for
// the chart's style (oox's spFilledSeriesLines): the light colour for
// styles 9 to 16, the dark colour shaded to half for 33, none otherwise.
func (p *chartReader) autoSeriesBorder() *chart.Stroke {
	switch {
	case p.style >= 9 && p.style <= 16:
		c, _ := p.theme.Color("lt1")
		return &chart.Stroke{Color: toCompose(c), Width: themeLineWidth}
	case p.style == 33:
		c, _ := p.theme.Color("dk1")
		return &chart.Stroke{Color: toCompose(Shade(c, 0.5)), Width: themeLineWidth}
	}
	return nil
}

// maxLabelFields caps the pieces of one data label's own text. Anti-DoS
// guard, not spec-mandated: a label is a few runs and fields.
const maxLabelFields = 256

// labelFields reads a data label's own text, a c:rich (ECMA-376 Part 1,
// 21.2.2.156): its runs as text, its fields (a:fld, 21.1.2.2.4) by type,
// a line break between paragraphs and at a:br. A CELLREF or CELLRANGE
// field shows its cell's cached text from the label's c15:dlblFieldTable,
// else the field's own text.
func labelFields(rich, dLbl *Element) []chart.LabelField {
	cells := map[string]string{}
	for _, ext := range dLbl.Child(NSChart, "extLst").Kids() {
		for _, tbl := range ext.Kids() {
			if tbl.Local != "dlblFieldTable" {
				continue
			}
			for _, ent := range tbl.Kids() {
				id, val := "", ""
				for _, k := range ent.Kids() {
					switch k.Local {
					case "txfldGUID":
						id = strings.TrimSpace(k.Text)
					case "dlblFieldTableCache":
						if d, _ := readCache(k); len(d) > 0 {
							val = d[0].Text
						}
					}
				}
				if id != "" {
					cells[id] = val
				}
			}
		}
	}
	var out []chart.LabelField
	add := func(f chart.LabelField) {
		if len(out) < maxLabelFields {
			out = append(out, f)
		}
	}
	first := true
	for _, para := range rich.Kids() {
		if !para.Is(NSA, "p") {
			continue
		}
		if !first {
			add(chart.LabelField{Kind: chart.FieldBreak})
		}
		first = false
		for _, r := range para.Kids() {
			switch {
			case r.Is(NSA, "r"):
				add(chart.LabelField{Text: textOf(r.Child(NSA, "t"))})
			case r.Is(NSA, "br"):
				add(chart.LabelField{Kind: chart.FieldBreak})
			case r.Is(NSA, "fld"):
				f := chart.LabelField{Text: textOf(r.Child(NSA, "t"))}
				switch attrOr(r, "type") {
				case "VALUE", "Y_VALUE":
					f.Kind = chart.FieldValue
				case "CATEGORYNAME", "X_VALUE":
					f.Kind = chart.FieldCategory
				case "SERIESNAME":
					f.Kind = chart.FieldSeries
				case "PERCENTAGE":
					f.Kind = chart.FieldPercent
				case "CELLREF", "CELLRANGE":
					if v, ok := cells[attrOr(r, "id")]; ok {
						f.Text = v
					}
				}
				add(f)
			}
		}
	}
	return out
}
