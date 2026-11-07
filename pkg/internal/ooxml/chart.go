package ooxml

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/chart"
	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
)

// NSChart is the DrawingML chart namespace (ECMA-376 Part 1, 21.2).
const NSChart = "http://schemas.openxmlformats.org/drawingml/2006/chart"

// RelChart is the relationship type of a chart part (ECMA-376 Part 1,
// 14.2.1).
const RelChart = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/chart"

// Warning codes [ParseChart] reports.
const (
	// CodeChartUnsupported flags a chart construct drawn approximately or
	// not at all: a 3D chart drawn flat, trendlines, error bars, user
	// shapes, an unknown chart type.
	CodeChartUnsupported = "chart-unsupported"
	// CodeChartNoCache flags a series whose file holds no cached values,
	// drawn empty unless the converter resolves its range.
	CodeChartNoCache = "chart-no-cache"
)

// defaultChartStyle is the chart style a chart space without c:style takes
// (ECMA-376 Part 1, 21.2.2.202: 2).
const defaultChartStyle = 2

// officeTheme is the colour scheme of Office 2007's default theme, used
// for the scheme colours of a package without a theme.
var officeTheme = &Theme{Colors: map[string]css.Color{
	"dk1": hexColor(0x000000), "lt1": hexColor(0xffffff), "dk2": hexColor(0x1f497d), "lt2": hexColor(0xeeece1),
	"accent1": hexColor(0x4f81bd), "accent2": hexColor(0xc0504d), "accent3": hexColor(0x9bbb59),
	"accent4": hexColor(0x8064a2), "accent5": hexColor(0x4bacc6), "accent6": hexColor(0xf79646),
	"hlink": hexColor(0x0000ff), "folHlink": hexColor(0x800080),
}, Minor: FontScheme{Latin: "Calibri"}, Major: FontScheme{Latin: "Cambria"}}

// hexColor returns the opaque colour 0xRRGGBB.
func hexColor(v uint32) css.Color {
	return css.Color{R: float64(v>>16&0xff) / 255, G: float64(v>>8&0xff) / 255, B: float64(v&0xff) / 255, A: 1}
}

// ParseChart reads the chart part of pkg (ECMA-376 Part 1, 21.2: a
// c:chartSpace root) into a chart model, with the package's theme for the
// scheme colours and fonts. What the file leaves automatic takes
// LibreOffice's defaults for the chart's style (c:style, 2 by default):
// the theme's accent colours for the series, a white chart area with a
// light grey border, 10-point text in the theme's minor font and an
// 18-point bold title, lines three times the theme's thinnest line.
//
// The data comes from the series' caches (c:strCache, c:numCache, literal
// c:strLit and c:numLit); a series with none keeps its range reference and
// is reported with [CodeChartNoCache]. External data and the formatting of
// Office 2013's chart style parts are ignored; 3D charts are read as their
// flat kind.
//
// It fails with [ErrNoPart] for a missing part and wraps the parse errors
// of [Package.ParseXML]; a part whose root is not c:chartSpace fails with
// office.ErrMalformed through the error message. ctx's error stops it.
func ParseChart(ctx context.Context, pkg *Package, part string) (*chart.Chart, error) {
	root, err := pkg.ParseXML(ctx, part, nil)
	if err != nil {
		return nil, err
	}
	theme := officeTheme
	if main, err := pkg.MainPart(); err == nil {
		if name := pkg.RelatedPart(main, RelTheme); name != "" {
			if troot, err := pkg.ParseXML(ctx, name, nil); err == nil {
				if t := ParseTheme(troot); len(t.Colors) > 0 {
					theme = t
				}
			}
		}
	}
	c, err := ChartFromElement(root, theme, isMSO2007(ctx, pkg))
	if err != nil {
		return nil, fmt.Errorf("reading the chart %s: %w", part, err)
	}
	if rels, err := pkg.Relationships(part); err == nil {
		for _, r := range rels {
			if strings.HasSuffix(r.Type, "/chartUserShapes") {
				c.Warnings = append(c.Warnings, compose.Warning{Code: CodeChartUnsupported, Message: "the shapes drawn over a chart are not drawn"})
			}
		}
	}
	return c, nil
}

// errNotChart reports a part whose root is not a chart space.
var errNotChart = fmt.Errorf("not a c:chartSpace")

// relExtendedProperties is the relationship type of the extended
// properties part (ECMA-376 Part 1, 15.2.12.3).
const relExtendedProperties = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/extended-properties"

// isMSO2007 reports a package Office 2007 saved, as its extended
// properties tell (Application "Microsoft ...", AppVersion 12.x): Office
// 2007 wrote the defaults of several chart booleans opposite to the
// specification, and LibreOffice reads such files with Office 2007's
// defaults (oox's XmlFilterBase::isMSO2007Document).
func isMSO2007(ctx context.Context, pkg *Package) bool {
	name := pkg.RelatedPart("", relExtendedProperties)
	if name == "" {
		return false
	}
	root, err := pkg.ParseXML(ctx, name, nil)
	if err != nil {
		return false
	}
	app, ver := "", ""
	for _, c := range root.Kids() {
		switch c.Local {
		case "Application":
			app = c.Text
		case "AppVersion":
			ver = c.Text
		}
	}
	return strings.HasPrefix(app, "Microsoft") && strings.HasPrefix(ver, "12.")
}

// ChartFromElement reads a parsed c:chartSpace element as [ParseChart]
// does, with theme for the scheme colours (nil for Office's default) and
// mso2007 for a file Office 2007 saved.
func ChartFromElement(root *Element, theme *Theme, mso2007 bool) (*chart.Chart, error) {
	if !root.Is(NSChart, "chartSpace") {
		return nil, errNotChart
	}
	if theme == nil || len(theme.Colors) == 0 {
		theme = officeTheme
	}
	p := &chartReader{theme: theme, style: defaultChartStyle, c: &chart.Chart{}, mso2007: mso2007}
	if v, ok := Int(childVal(root, "style")); ok && v >= 1 && v <= 48 {
		p.style = v
	}
	p.global = p.textProps(root.Child(NSChart, "txPr"))
	p.read(root)
	return p.c, nil
}

// chartReader holds the state of one chart's reading.
type chartReader struct {
	theme *Theme
	style int
	c     *chart.Chart
	// mso2007 reads absent booleans with Office 2007's defaults: no
	// varied colours, no smoothing, an automatic title.
	mso2007 bool
	// global is the chart space's text properties, which every text
	// inherits.
	global textProps
	// axes maps an axis ID to its index in the chart's axes.
	axes   map[string]int
	warned map[string]bool
	// autoLines are the line series whose line takes the automatic
	// colour.
	autoLines []*chart.Series
}

// warn records a warning once per message.
func (p *chartReader) warn(code, msg string) {
	if p.warned == nil {
		p.warned = map[string]bool{}
	}
	if p.warned[msg] {
		return
	}
	p.warned[msg] = true
	p.c.Warnings = append(p.c.Warnings, compose.Warning{Code: code, Message: msg})
}

// childVal returns the val attribute of e's child c:local, "" for none.
func childVal(e *Element, local string) string {
	return attrOr(e.Child(NSChart, local), "val")
}

// boolVal reads an on/off child c:local: its val attribute, true when the
// element is present without one (ECMA-376 Part 1, 21.2.2 CT_Boolean),
// def when absent.
func boolVal(e *Element, local string, def bool) bool {
	c := e.Child(NSChart, local)
	if c == nil {
		return def
	}
	v, ok := c.Attr("val")
	if !ok {
		return true
	}
	return v == "1" || v == "true" || v == "on"
}

// floatVal reads a numeric child c:local, NaN when absent or invalid.
func floatVal(e *Element, local string) float64 {
	return parseFloat(childVal(e, local))
}

// parseFloat parses a finite number, NaN otherwise.
func parseFloat(s string) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || math.IsInf(v, 0) {
		return math.NaN()
	}
	return v
}

// toCompose converts a css colour.
func toCompose(c css.Color) compose.Color {
	return compose.Color{R: c.R, G: c.G, B: c.B, A: c.A}
}

// read fills the chart from the chart space root.
func (p *chartReader) read(root *Element) {
	c := p.c
	ch := root.Child(NSChart, "chart")
	c.Area = p.frame(root.Child(NSChart, "spPr"), p.autoFrame(true))
	accents := []string{"accent1", "accent2", "accent3", "accent4", "accent5", "accent6"}
	c.Palette, c.PaletteCycleTint = p.palette(accents)
	pa := ch.Child(NSChart, "plotArea")
	c.PlotArea = p.frame(pa.Child(NSChart, "spPr"), p.autoFrame(false))
	p.readAxes(pa)
	for _, g := range pa.Kids() {
		if g.Space != NSChart {
			continue
		}
		if grp := p.group(g); grp != nil {
			c.Groups = append(c.Groups, grp)
		}
	}
	if len(c.Groups) == 0 {
		p.warn(CodeChartUnsupported, "the chart has no chart type the reader draws")
	}
	n := 0
	for _, g := range c.Groups {
		n += len(g.Series)
	}
	for _, s := range p.autoLines {
		b := *s.Border
		b.Color = c.AutoColor(s.Index, n)
		s.Border = &b
	}
	p.readTitle(ch)
	if lg := ch.Child(NSChart, "legend"); lg != nil {
		p.readLegend(lg)
	}
	if ch.Child(NSChart, "dTable") != nil {
		p.warn(CodeChartUnsupported, "a chart's data table is not drawn")
	}
}

// palette returns the automatic series colours of the chart's style
// (oox's spFilledSeries2dFills): the six accents for the colourful styles
// (2, 10, 18, 26, 34, 42), shades of the dark colour for the grey ones (1,
// 9, 17, 25, 33, 41), and for the others one accent each series faded from
// dark to light.
func (p *chartReader) palette(accents []string) ([]compose.Color, bool) {
	col := func(name string) compose.Color {
		c, _ := p.theme.Color(name)
		return toCompose(c)
	}
	switch m := (p.style - 1) % 8; {
	case m == 1 || p.style == 42:
		out := make([]compose.Color, len(accents))
		for i, a := range accents {
			out[i] = col(a)
		}
		return out, true
	case m == 0:
		dk, _ := p.theme.Color("dk1")
		var out []compose.Color
		for _, t := range []float64{0.885, 0.55, 0.78, 0.925, 0.70, 0.30} {
			out = append(out, toCompose(linearTint(dk, t)))
		}
		return out, true
	default:
		return []compose.Color{col(accents[(m-2)%6])}, true
	}
}

// autoFrame returns the automatic frame of the chart area (white with a
// light grey border, oox's default chart area line) or the plot area (the
// background colour, no border).
func (p *chartReader) autoFrame(area bool) chart.Frame {
	fillName := "bg1"
	switch {
	case p.style >= 41:
		fillName = "dk1"
	case p.style >= 33:
		fillName = "lt1"
	}
	f := chart.Frame{}
	if c, ok := p.theme.Color(fillName); ok {
		f.Fill = &chart.Paint{Color: toCompose(c)}
	}
	if area {
		f.Border = &chart.Stroke{Color: toCompose(hexColor(0xd9d9d9)), Width: 0.75}
	} else if p.style > 32 {
		dk, _ := p.theme.Color("dk1")
		f.Fill = &chart.Paint{Color: toCompose(linearTint(dk, 0.2))}
	}
	return f
}

// themeLineWidth is the width of the theme's thinnest line, 9525 EMUs in
// Office's themes (ECMA-376 Part 1, 20.1.4.1.21 lnStyleLst).
const themeLineWidth = 0.75

// autoLine returns the automatic axis and gridline colour: the text colour
// tinted to 75% (oox's spAxisLines and spMajorGridLines).
func (p *chartReader) autoLine() *chart.Stroke {
	name := "tx1"
	if p.style > 32 {
		name = "dk1"
	}
	c, _ := p.theme.Color(name)
	return &chart.Stroke{Color: toCompose(linearTint(c, 0.75)), Width: themeLineWidth}
}

// linearTint lightens c as oox's colour tint does for the automatic chart
// formats: towards white in linear RGB, keeping the fraction t of the
// colour.
func linearTint(c css.Color, t float64) css.Color {
	f := func(v float64) float64 {
		l := toLinear(v)
		return fromLinear(1 - (1-l)*t)
	}
	return css.Color{R: f(c.R), G: f(c.G), B: f(c.B), A: c.A}
}

// toLinear and fromLinear convert an sRGB channel to and from linear
// light (IEC 61966-2-1).
func toLinear(v float64) float64 {
	if v <= 0.04045 {
		return v / 12.92
	}
	return math.Pow((v+0.055)/1.055, 2.4)
}

func fromLinear(v float64) float64 {
	if v <= 0.0031308 {
		return v * 12.92
	}
	return 1.055*math.Pow(v, 1/2.4) - 0.055
}

// frame reads a shape properties element over the automatic frame def.
func (p *chartReader) frame(spPr *Element, def chart.Frame) chart.Frame {
	f := def
	if fill, ok := p.fill(spPr); ok {
		f.Fill = fill
	}
	if ln := spPr.Child(NSA, "ln"); ln != nil {
		f.Border = p.stroke(ln, f.Border)
	}
	return f
}

// fill reads the fill of a shape properties element and whether it sets
// one: a:noFill, a:solidFill, a:gradFill (its middle stop), a:pattFill
// (its foreground colour) (ECMA-376 Part 1, 20.1.8).
func (p *chartReader) fill(spPr *Element) (*chart.Paint, bool) {
	for _, e := range spPr.Kids() {
		if e.Space != NSA {
			continue
		}
		switch e.Local {
		case "noFill":
			return &chart.Paint{None: true}, true
		case "solidFill":
			if c, ok := SchemeColor(e, p.theme); ok {
				return &chart.Paint{Color: toCompose(c)}, true
			}
			return nil, true
		case "gradFill":
			stops := e.Child(NSA, "gsLst").Kids()
			if len(stops) > 0 {
				if c, ok := SchemeColor(stops[len(stops)/2], p.theme); ok {
					return &chart.Paint{Color: toCompose(c)}, true
				}
			}
			return nil, true
		case "pattFill":
			if c, ok := SchemeColor(e.Child(NSA, "fgClr"), p.theme); ok {
				return &chart.Paint{Color: toCompose(c)}, true
			}
			return nil, true
		case "blipFill", "grpFill":
			return nil, true
		}
	}
	return nil, false
}

// stroke reads an a:ln element over the automatic line def (ECMA-376
// Part 1, 20.1.2.2.24).
func (p *chartReader) stroke(ln *Element, def *chart.Stroke) *chart.Stroke {
	s := chart.Stroke{Width: themeLineWidth, Color: compose.Black}
	if def != nil {
		s = *def
	}
	if ln == nil {
		if def == nil {
			return nil
		}
		return &s
	}
	if w, ok := EMU(attrOr(ln, "w")); ok && w >= 0 {
		s.Width = w
	}
	set := false
	for _, e := range ln.Children {
		if e.Space != NSA {
			continue
		}
		switch e.Local {
		case "noFill":
			return &chart.Stroke{None: true}
		case "solidFill":
			if c, ok := SchemeColor(e, p.theme); ok {
				s.Color, s.None, set = toCompose(c), false, true
			}
		case "gradFill":
			if stops := e.Child(NSA, "gsLst").Kids(); len(stops) > 0 {
				if c, ok := SchemeColor(stops[0], p.theme); ok {
					s.Color, s.None, set = toCompose(c), false, true
				}
			}
		case "prstDash":
			s.Dash = presetDash(attrOr(e, "val"), s.Width)
		}
	}
	if def == nil && !set {
		return nil
	}
	return &s
}

// presetDash returns the dash pattern of an ST_PresetLineDashVal
// (ECMA-376 Part 1, 20.1.10.48) for a line of width w.
func presetDash(name string, w float64) []float64 {
	w = math.Max(w, 0.75)
	switch name {
	case "dot", "sysDot":
		return []float64{w, w}
	case "dash", "sysDash":
		return []float64{4 * w, 3 * w}
	case "lgDash":
		return []float64{8 * w, 3 * w}
	case "dashDot", "sysDashDot":
		return []float64{4 * w, 3 * w, w, 3 * w}
	case "lgDashDot":
		return []float64{8 * w, 3 * w, w, 3 * w}
	case "lgDashDotDot", "sysDashDotDot":
		return []float64{8 * w, 3 * w, w, 3 * w, w, 3 * w}
	}
	return nil
}

// textProps are character properties read from a text body, each set
// flag telling whether the file gives it.
type textProps struct {
	family           string
	size             float64
	bold, italic     bool
	hasBold, hasItal bool
	color            *compose.Color
	rotation         float64
	hasRotation      bool
}

// textProps reads the default run properties of a c:txPr or c:rich text
// body: its first paragraph's a:defRPr, then its first run's a:rPr, and
// the body's rotation (ECMA-376 Part 1, 21.2.2.216 txPr, 21.1.2.1.1
// bodyPr).
func (p *chartReader) textProps(body *Element) textProps {
	var t textProps
	if body == nil {
		return t
	}
	if bp := body.Child(NSA, "bodyPr"); bp != nil {
		if v, ok := Int(attrOr(bp, "rot")); ok && v != -60000000 {
			t.rotation, t.hasRotation = -float64(v)/60000, true
		}
	}
	for _, para := range body.Kids() {
		if !para.Is(NSA, "p") {
			continue
		}
		p.runProps(&t, para.Child(NSA, "pPr").Child(NSA, "defRPr"))
		for _, r := range para.Kids() {
			if r.Is(NSA, "r") {
				p.runProps(&t, r.Child(NSA, "rPr"))
				break
			}
		}
		break
	}
	return t
}

// runProps lays a run properties element over t.
func (p *chartReader) runProps(t *textProps, rp *Element) {
	if rp == nil {
		return
	}
	if v, ok := Int(attrOr(rp, "sz")); ok && v >= 100 && v <= 400000 {
		t.size = float64(v) / 100
	}
	if v, ok := rp.Attr("b"); ok {
		t.bold, t.hasBold = v == "1" || v == "true", true
	}
	if v, ok := rp.Attr("i"); ok {
		t.italic, t.hasItal = v == "1" || v == "true", true
	}
	if f := rp.Child(NSA, "solidFill"); f != nil {
		if c, ok := SchemeColor(f, p.theme); ok {
			cc := toCompose(c)
			t.color = &cc
		}
	}
	if l := rp.Child(NSA, "latin"); l != nil {
		if tf := attrOr(l, "typeface"); tf != "" {
			t.family = tf
		}
	}
}

// textStyle resolves the character format of a text: LibreOffice's
// automatic text for the chart style (oox's spOtherTexts and
// spChartTitleTexts: the text colour, the theme's minor font, size def
// points, bold for titles), the chart space's text properties (their size
// scaled by rel for titles), then the text's own.
func (p *chartReader) textStyle(own textProps, def float64, bold bool, rel float64) chart.TextStyle {
	st := chart.TextStyle{Size: def, Bold: bold}
	name := "tx1"
	if p.style > 40 {
		name = "lt1"
	}
	if c, ok := p.theme.Color(name); ok {
		st.Color = toCompose(c)
	} else {
		st.Color = compose.Black
	}
	family := p.theme.Minor.Latin
	apply := func(t textProps, scale float64) {
		if t.family != "" {
			family = t.family
		}
		if t.size > 0 {
			st.Size = t.size * scale
		}
		if t.hasBold {
			st.Bold = t.bold
		}
		if t.hasItal {
			st.Italic = t.italic
		}
		if t.color != nil {
			st.Color = *t.color
		}
		if t.hasRotation {
			st.Rotation = t.rotation
		}
	}
	apply(p.global, rel)
	apply(own, 1)
	st.Families = p.families(family)
	return st
}

// families resolves a typeface name, theme references (+mn-lt, +mj-lt)
// included, to a family list.
func (p *chartReader) families(name string) []string {
	switch name {
	case "+mn-lt", "+mn-ea", "+mn-cs", "":
		name = p.theme.Minor.Latin
	case "+mj-lt", "+mj-ea", "+mj-cs":
		name = p.theme.Major.Latin
	}
	if name == "" {
		name = "Calibri"
	}
	return []string{name}
}

// richText returns the text of a c:tx holding c:rich (its paragraphs'
// runs, a paragraph each line) or a string reference's cache.
func richText(tx *Element) string {
	if tx == nil {
		return ""
	}
	if rich := tx.Child(NSChart, "rich"); rich != nil {
		var lines []string
		for _, para := range rich.Kids() {
			if !para.Is(NSA, "p") {
				continue
			}
			var b strings.Builder
			for _, r := range para.Kids() {
				switch {
				case r.Is(NSA, "r"), r.Is(NSA, "fld"):
					b.WriteString(textOf(r.Child(NSA, "t")))
				case r.Is(NSA, "br"):
					b.WriteString("\n")
				}
			}
			lines = append(lines, b.String())
		}
		return strings.Join(lines, "\n")
	}
	if v := tx.Child(NSChart, "v"); v != nil {
		return v.Text
	}
	d := readData(tx)
	var parts []string
	for _, v := range d.Values {
		parts = append(parts, v.Text)
	}
	return strings.Join(parts, " ")
}

// readTitle reads the chart title, or the automatic one LibreOffice shows:
// the series name of a chart of one series, "Chart Title" for a title
// element without text (oox's ChartSpaceConverter).
func (p *chartReader) readTitle(ch *Element) {
	t := ch.Child(NSChart, "title")
	deleted := boolVal(ch, "autoTitleDeleted", !p.mso2007)
	if t == nil && deleted {
		return
	}
	text := richText(t.Child(NSChart, "tx"))
	if text == "" {
		if len(p.c.Groups) > 0 && (len(p.c.Groups[0].Series) == 1 || p.c.Groups[0].Kind == chart.Pie) && len(p.c.Groups[0].Series) > 0 {
			text = p.c.Groups[0].Series[0].Name
		}
		if text == "" && t != nil {
			text = "Chart Title"
		}
	}
	if text == "" {
		return
	}
	own := p.textProps(t.Child(NSChart, "txPr"))
	if rich := t.Child(NSChart, "tx").Child(NSChart, "rich"); rich != nil {
		own = p.textProps(rich)
	}
	p.c.Title = &chart.Title{
		Text:    text,
		Style:   p.textStyle(own, 18, true, 1.2),
		Frame:   p.frame(t.Child(NSChart, "spPr"), chart.Frame{}),
		Overlay: boolVal(t, "overlay", false),
	}
}

// readLegend reads c:legend (ECMA-376 Part 1, 21.2.2.93).
func (p *chartReader) readLegend(lg *Element) {
	l := &chart.Legend{Overlay: boolVal(lg, "overlay", false)}
	switch childVal(lg, "legendPos") {
	case "l":
		l.Position = chart.LegendLeft
	case "t":
		l.Position = chart.LegendTop
	case "b":
		l.Position = chart.LegendBottom
	case "tr":
		l.Position = chart.LegendTopRight
	}
	l.Style = p.textStyle(p.textProps(lg.Child(NSChart, "txPr")), 10, false, 1)
	l.Frame = p.frame(lg.Child(NSChart, "spPr"), chart.Frame{})
	for _, e := range lg.Kids() {
		if e.Is(NSChart, "legendEntry") && boolVal(e, "delete", false) {
			if v, ok := Int(childVal(e, "idx")); ok && v >= 0 {
				l.Hidden = append(l.Hidden, v)
			}
		}
	}
	p.c.Legend = l
}

// readAxes reads the plot area's axes and indexes them by ID.
func (p *chartReader) readAxes(pa *Element) {
	p.axes = map[string]int{}
	for _, e := range pa.Kids() {
		if e.Space != NSChart {
			continue
		}
		var kind chart.AxisKind
		switch e.Local {
		case "catAx":
			kind = chart.CategoryAxis
		case "valAx":
			kind = chart.ValueAxis
		case "dateAx":
			kind = chart.DateAxis
		case "serAx":
			kind = chart.SeriesAxis
		default:
			continue
		}
		id := childVal(e, "axId")
		if _, dup := p.axes[id]; dup {
			continue
		}
		p.axes[id] = len(p.c.Axes)
		p.c.Axes = append(p.c.Axes, p.axis(e, kind))
	}
}

// axis reads one axis element (ECMA-376 Part 1, 21.2.2.25 catAx and its
// siblings).
func (p *chartReader) axis(e *Element, kind chart.AxisKind) *chart.Axis {
	a := &chart.Axis{Kind: kind, Min: math.NaN(), Max: math.NaN(), MajorUnit: math.NaN(), MinorUnit: math.NaN()}
	a.Deleted = boolVal(e, "delete", false)
	sc := e.Child(NSChart, "scaling")
	a.Reverse = childVal(sc, "orientation") == "maxMin"
	a.Min, a.Max = floatVal(sc, "min"), floatVal(sc, "max")
	if b := floatVal(sc, "logBase"); b >= 2 && b <= 1000 {
		a.Log = b
	}
	a.MajorUnit, a.MinorUnit = floatVal(e, "majorUnit"), floatVal(e, "minorUnit")
	switch childVal(e, "axPos") {
	case "l":
		a.Position = chart.AxisLeft
	case "t":
		a.Position = chart.AxisTop
	case "r":
		a.Position = chart.AxisRight
	}
	switch childVal(e, "crosses") {
	case "min":
		a.Crosses = chart.CrossMin
	case "max":
		a.Crosses = chart.CrossMax
	}
	if v := floatVal(e, "crossesAt"); !math.IsNaN(v) {
		a.Crosses, a.CrossAt = chart.CrossValue, v
	}
	if nf := e.Child(NSChart, "numFmt"); nf != nil {
		if code := attrOr(nf, "formatCode"); code != "" && (attrOr(nf, "sourceLinked") != "1" || !strings.EqualFold(code, "General")) {
			a.Format = code
		}
	}
	switch childVal(e, "tickLblPos") {
	case "low":
		a.Labels = chart.LabelLow
	case "high":
		a.Labels = chart.LabelHigh
	case "none":
		a.Labels = chart.LabelNone
	}
	a.MajorTicks = tickMark(childVal(e, "majorTickMark"), chart.TickCross)
	a.MinorTicks = tickMark(childVal(e, "minorTickMark"), chart.TickNone)
	if g := e.Child(NSChart, "majorGridlines"); g != nil {
		a.MajorGrid = p.lineOf(g.Child(NSChart, "spPr"), p.autoLine())
	}
	if g := e.Child(NSChart, "minorGridlines"); g != nil {
		minor := p.autoLine()
		tx, _ := p.theme.Color("tx1")
		minor.Color = toCompose(linearTint(tx, 0.5))
		a.MinorGrid = p.lineOf(g.Child(NSChart, "spPr"), minor)
	}
	a.Line = p.lineOf(e.Child(NSChart, "spPr"), p.autoLine())
	a.Style = p.textStyle(p.textProps(e.Child(NSChart, "txPr")), 10, false, 1)
	if t := e.Child(NSChart, "title"); t != nil {
		own := p.textProps(t.Child(NSChart, "txPr"))
		if rich := t.Child(NSChart, "tx").Child(NSChart, "rich"); rich != nil {
			own = p.textProps(rich)
		}
		text := richText(t.Child(NSChart, "tx"))
		if text == "" {
			text = "Axis Title"
		}
		a.Title = &chart.Title{Text: text, Style: p.textStyle(own, 10, true, 1), Frame: p.frame(t.Child(NSChart, "spPr"), chart.Frame{})}
	}
	a.BetweenCategories = childVal(e, "crossBetween") != "midCat"
	return a
}

// lineOf returns the line of a shape properties element over def, nil
// when it draws none.
func (p *chartReader) lineOf(spPr *Element, def *chart.Stroke) *chart.Stroke {
	s := def
	if ln := spPr.Child(NSA, "ln"); ln != nil {
		s = p.stroke(ln, def)
	}
	if s != nil && s.None {
		return nil
	}
	return s
}

// tickMark reads an ST_TickMark (ECMA-376 Part 1, 21.2.3.48), def when
// absent.
func tickMark(v string, def chart.TickMark) chart.TickMark {
	switch v {
	case "none":
		return chart.TickNone
	case "in":
		return chart.TickIn
	case "out":
		return chart.TickOut
	case "cross":
		return chart.TickCross
	}
	return def
}

// textOf returns e's character data, "" for a nil e.
func textOf(e *Element) string {
	if e == nil {
		return ""
	}
	return e.Text
}
