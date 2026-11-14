package ooxml

import (
	"context"
	"fmt"
	"maps"
	"math"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/chart"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/detmath"
	"github.com/gotenberg/litho/pkg/internal/intconv"
	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/shape"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// NSChart is the DrawingML chart namespace (ECMA-376 Part 1, 21.2).
const NSChart = "http://schemas.openxmlformats.org/drawingml/2006/chart"

// RelChart is the relationship type of a chart part (ECMA-376 Part 1,
// 14.2.1).
const RelChart = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/chart"

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
// light grey border (in a presentation, no fill and no border for the
// styles 1 to 32), 10-point text in the theme's minor font and an
// 18-point bold title, lines three times the theme's thinnest line.
//
// The data comes from the series' caches (c:strCache, c:numCache, literal
// c:strLit and c:numLit); a series with none keeps its range reference and
// is reported with [warn.ChartNoCache]. External data and the formatting of
// Office 2013's chart style parts are ignored. A 3D chart keeps its view,
// floor and walls; a 3D surface chart is not drawn.
//
// It fails with [ErrNoPart] for a missing part and wraps the parse errors
// of [Package.ParseXML]; a part whose root is not c:chartSpace fails with
// office.ErrMalformed. ctx's error stops it.
func ParseChart(ctx context.Context, pkg *Package, part string) (*chart.Chart, error) {
	root, err := pkg.ParseXML(ctx, part, nil)
	if err != nil {
		return nil, err
	}
	theme := officeTheme
	opts := ChartOptions{MSO2007: isMSO2007(ctx, pkg)}
	if main, err := pkg.MainPart(ctx); err == nil {
		opts.Presentation = strings.HasSuffix(main, "/presentation.xml")
		if name := pkg.RelatedPart(ctx, main, RelTheme); name != "" {
			if troot, err := pkg.ParseXML(ctx, name, nil); err == nil {
				if t := ParseTheme(troot); len(t.Colors) > 0 {
					theme = t
				}
			}
		}
	}
	theme = chartTheme(ctx, pkg, part, theme)
	c, err := ChartFromElement(root, theme, opts)
	if err != nil {
		return nil, fmt.Errorf("reading the chart %s: %w", part, err)
	}
	if rels, err := pkg.Relationships(ctx, part); err == nil {
		for _, r := range rels {
			if strings.HasSuffix(r.Type, "/chartUserShapes") {
				c.Warnings = append(c.Warnings, typeset.Warning{Code: warn.ContentUnsupported, Effect: warn.Omitted, Message: "the shapes drawn over a chart are not drawn"})
			}
		}
	}
	return c, nil
}

// relThemeOverride is the relationship type of a chart's theme override
// part (ECMA-376 Part 1, 14.2.9).
const relThemeOverride = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/themeOverride"

// chartTheme returns theme with the colours and fonts the chart part's
// theme override replaces (ECMA-376 Part 1, 20.1.6.12 themeOverride: a
// colour, font or format scheme the chart takes instead of the
// document's), theme itself without one.
func chartTheme(ctx context.Context, pkg *Package, part string, theme *Theme) *Theme {
	name := pkg.RelatedPart(ctx, part, relThemeOverride)
	if name == "" {
		return theme
	}
	root, err := pkg.ParseXML(ctx, name, nil)
	if err != nil || !root.Is(NSA, "themeOverride") {
		return theme
	}
	o := ParseTheme(&Element{Space: NSA, Local: "theme", Children: []*Element{{Space: NSA, Local: "themeElements", Children: root.Children}}})
	out := &Theme{Colors: map[string]css.Color{}, Major: theme.Major, Minor: theme.Minor}
	maps.Copy(out.Colors, theme.Colors)
	maps.Copy(out.Colors, o.Colors)
	if root.Child(NSA, "fontScheme") != nil {
		out.Major, out.Minor = o.Major, o.Minor
	}
	return out
}

// errNotChart reports a part whose root is not a chart space.
var errNotChart = fmt.Errorf("%w: not a c:chartSpace", office.ErrMalformed)

// relExtendedProperties is the relationship type of the extended
// properties part (ECMA-376 Part 1, 15.2.12.3).
const relExtendedProperties = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/extended-properties"

// isMSO2007 reports a package Office 2007 saved, as its extended
// properties tell (Application "Microsoft ...", AppVersion 12.x): Office
// 2007 wrote the defaults of several chart booleans opposite to the
// specification, and LibreOffice reads such files with Office 2007's
// defaults (oox's XmlFilterBase::isMSO2007Document).
func isMSO2007(ctx context.Context, pkg *Package) bool {
	name := pkg.RelatedPart(ctx, "", relExtendedProperties)
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

// ChartOptions says what [ChartFromElement] knows of the package a chart
// comes from.
type ChartOptions struct {
	// MSO2007 reads a file Office 2007 saved, with its defaults.
	MSO2007 bool
	// Presentation reads a chart of a presentation, whose chart area and
	// two-dimensional plot area are transparent and unframed by default
	// for the chart styles 1 to 32 (LibreOffice's Impress import:
	// PptGraphicHelper's default chart area fill, ObjectFormatter's
	// chart space line).
	Presentation bool
	// ChartStyle and ColorStyle are the cs:chartStyle and cs:colorStyle
	// roots of an Office 2016 chart's style parts, nil for none
	// (MS-ODRAWXML, 2.8): the formats of what the chart part leaves
	// automatic.
	ChartStyle, ColorStyle *Element
}

// ChartFromElement reads a parsed c:chartSpace element as [ParseChart]
// does, with theme for the scheme colours (nil for Office's default) and
// opts for the package's defaults.
func ChartFromElement(root *Element, theme *Theme, opts ChartOptions) (*chart.Chart, error) {
	if !root.Is(NSChart, "chartSpace") {
		return nil, errNotChart
	}
	if theme == nil || len(theme.Colors) == 0 {
		theme = officeTheme
	}
	p := &chartReader{theme: theme, style: defaultChartStyle, c: &chart.Chart{Rules: chart.OfficeRules}, mso2007: opts.MSO2007, impress: opts.Presentation}
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
	// impress reads a presentation's chart (ChartOptions.Presentation).
	impress bool
	// fixedInner marks a plot area the file places without its axis
	// labels.
	fixedInner bool
	// global is the chart space's text properties, which every text
	// inherits.
	global textProps
	// axes maps an axis ID to its index in the chart's axes.
	axes map[string]int
	// crossBetween is each axis's c:crossBetween, "" when absent.
	crossBetween []string
	warned       map[string]bool
	// autoLines are the line series whose line takes the automatic
	// colour.
	autoLines []*chart.Series
}

// warn records a warning once per effect and message.
func (p *chartReader) warn(code warn.Code, effect warn.Effect, msg string) {
	if p.warned == nil {
		p.warned = map[string]bool{}
	}
	k := effect.String() + "\x00" + msg
	if p.warned[k] {
		return
	}
	p.warned[k] = true
	p.c.Warnings = append(p.c.Warnings, typeset.Warning{Code: code, Effect: effect, Message: msg})
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

// percentVal reads a chart group's numeric child c:local whose val the
// schema types as a number or a percentage (ECMA-376 Part 1, 21.2.3.16
// ST_GapAmount, 21.2.3.31 ST_Overlap, 21.2.3.18 ST_HoleSize, 21.2.3.5
// ST_BubbleScale: a union of a percent form, "150%", and an integer
// form): NaN when the element is absent or its val invalid, def, the
// schema's default for val, when the element has no val.
func percentVal(e *Element, local string, def float64) float64 {
	c := e.Child(NSChart, local)
	if c == nil {
		return math.NaN()
	}
	v, ok := c.Attr("val")
	if !ok {
		return def
	}
	return parseFloat(strings.TrimSuffix(strings.TrimSpace(v), "%"))
}

// parseFloat parses a finite number, NaN otherwise.
func parseFloat(s string) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || math.IsInf(v, 0) {
		return math.NaN()
	}
	return v
}

// toTypeset converts a css colour.
func toTypeset(c css.Color) typeset.Color {
	return typeset.Color{R: c.R, G: c.G, B: c.B, A: c.A}
}

// read fills the chart from the chart space root.
func (p *chartReader) read(root *Element) {
	c := p.c
	ch := root.Child(NSChart, "chart")
	c.Area = p.frame(root.Child(NSChart, "spPr"), p.autoFrame(true))
	accents := []string{"accent1", "accent2", "accent3", "accent4", "accent5", "accent6"}
	c.Palette, c.PaletteCycleTint = p.palette(accents), true
	pa := ch.Child(NSChart, "plotArea")
	c.PlotArea = p.frame(pa.Child(NSChart, "spPr"), p.autoFrame(false))
	if l := manualLayout(pa); l != nil && l.Width > 0 && l.Height > 0 {
		c.PlotLayout = l
		p.fixedInner = l.Inner
	}
	p.readAxes(pa)
	known := false
	for _, g := range pa.Kids() {
		if g.Space != NSChart {
			continue
		}
		// A chart type without series draws nothing, not even its axes
		// (oox's PlotAreaConverter makes no axes set of it).
		if grp := p.group(g); grp != nil {
			known = true
			if len(grp.Series) > 0 {
				c.Groups = append(c.Groups, grp)
			}
		}
	}
	if !known {
		p.warn(warn.ContentUnsupported, warn.Omitted, "the chart has no chart type the reader draws")
	}
	p.categoryPositions()
	p.readView3D(ch)
	n := 0
	for _, g := range c.Groups {
		n += len(g.Series)
	}
	for _, s := range p.autoLines {
		b := *s.Border
		b.Color = c.AutoColor(s.Index, n)
		s.Border = &b
	}
	if boolVal(root, "date1904", false) {
		for _, g := range c.Groups {
			for _, s := range g.Series {
				shift1904(&s.Categories)
				shift1904(&s.Values)
				shift1904(&s.Sizes)
			}
		}
	}
	p.readTitle(ch)
	for _, g := range c.Groups {
		for _, s := range g.Series {
			if s.Name == "" {
				s.Name = unnamedSeries(s.Index)
			}
		}
	}
	if lg := ch.Child(NSChart, "legend"); lg != nil {
		p.readLegend(lg)
	}
	if dt := ch.Child(NSChart, "plotArea").Child(NSChart, "dTable"); dt != nil {
		p.readDataTable(dt)
	}
}

// The settings of a view that a chart omits, as Office draws it: the
// schema gives 0 for both angles only to an element present without a
// value (ECMA-376 Part 1, 21.2.2.157 rotX, 21.2.2.158 rotY), 100 for the
// depth (21.2.2.43) and 30 for the perspective (21.2.2.135).
//
// # Corpus evidence
//
//   - poi/xlsx: spreadsheet/45544.xlsx, a 3-D pie whose view3D has no
//     rotX: Excel's pie is 0.258 of its width high, the sine of 15
//     degrees, as a pie with rotX 15 is.
//   - openxmlsdk/pptx: v2FxTestFiles/presentation/Chart_3D_Column_.pptx, a
//     3-D column chart whose view3D sets only the perspective: PowerPoint
//     draws it seen from the right and above, as litho/pptx chart-3d's
//     charts that set rotX 15 and rotY 20.
const (
	defaultRotX        = 15
	defaultRotY        = 20
	defaultDepth       = 100
	defaultPerspective = 30
)

// readView3D reads the view of a chart that draws a group in three
// dimensions (ECMA-376 Part 1, 21.2.2.228 view3D) and its floor and walls,
// and warns of what the projection leaves out. A chart without such a
// group keeps no view: it is drawn flat whatever its view3D says.
func (p *chartReader) readView3D(ch *Element) {
	var g3 *chart.Group
	for _, g := range p.c.Groups {
		if g.ThreeD {
			g3 = g
			break
		}
	}
	if g3 == nil {
		return
	}
	e := ch.Child(NSChart, "view3D")
	v := &chart.View3D{RotX: defaultRotX, RotY: defaultRotY, Depth: defaultDepth, Perspective: defaultPerspective}
	if g3.Kind == chart.Pie {
		v.RotY = 0
	}
	if x := floatVal(e, "rotX"); !math.IsNaN(x) {
		v.RotX = math.Max(-90, math.Min(x, 90))
	}
	if y := floatVal(e, "rotY"); !math.IsNaN(y) {
		v.RotY = math.Max(0, math.Min(y, 360))
	}
	if d := floatVal(e, "depthPercent"); !math.IsNaN(d) {
		v.Depth = math.Max(20, math.Min(d, 2000))
	}
	if d := floatVal(e, "perspective"); !math.IsNaN(d) {
		v.Perspective = math.Max(0, math.Min(d, 240))
	}
	v.RightAngle = boolVal(e, "rAngAx", false)
	p.c.View3D = v
	p.c.Floor = p.frame(ch.Child(NSChart, "floor").Child(NSChart, "spPr"), chart.Frame{})
	p.c.SideWall = p.frame(ch.Child(NSChart, "sideWall").Child(NSChart, "spPr"), chart.Frame{})
	p.c.BackWall = p.frame(ch.Child(NSChart, "backWall").Child(NSChart, "spPr"), chart.Frame{})
	for _, g := range p.c.Groups {
		if !g.ThreeD {
			continue
		}
		switch g.Kind {
		case chart.Bar, chart.Line, chart.Area:
			if !v.RightAngle && v.Perspective > 0 {
				p.warn(warn.ContentUnsupported, warn.Approximated, "a 3D chart's perspective is drawn as a parallel projection")
			}
			if detmath.Cos(v.RotY*math.Pi/180) < 0 {
				p.warn(warn.ContentUnsupported, warn.Approximated, "a 3D chart seen from behind is drawn seen from the front")
			}
		case chart.Pie:
		case chart.Doughnut, chart.Scatter, chart.Bubble, chart.Radar, chart.Unsupported:
			p.warn(warn.ContentUnsupported, warn.Approximated, "a 3D "+g.Kind.String()+" chart is drawn flat")
		}
		p.warn(warn.ContentUnsupported, warn.Approximated, "a 3D chart's faces are shaded flat, without its lighting")
		if e.Child(NSChart, "hPercent") != nil {
			p.warn(warn.ContentUnsupported, warn.Approximated, "a 3D chart's height percentage is not applied")
		}
	}
}

// readDataTable reads c:dTable (ECMA-376 Part 1, 21.2.2.54): its borders,
// keys, text and lines; a fill colours the text's background, as
// LibreOffice draws it.
func (p *chartReader) readDataTable(e *Element) {
	f := p.frame(e.Child(NSChart, "spPr"), chart.Frame{})
	t := &chart.DataTable{
		HBorder: boolVal(e, "showHorzBorder", false),
		VBorder: boolVal(e, "showVertBorder", false),
		Outline: boolVal(e, "showOutline", false),
		Keys:    boolVal(e, "showKeys", false),
		Style:   p.textStyle(p.textProps(e.Child(NSChart, "txPr")), 10, false, 1),
		Fill:    f.Fill,
		Line:    f.Border,
	}
	if t.Fill != nil && t.Fill.None {
		t.Fill = nil
	}
	if t.Line != nil && t.Line.None {
		t.Line = nil
	}
	p.c.DataTable = t
}

// categoryPositions decides whether each category axis puts its
// categories between its ticks, as oox's AxisConverter does: from the
// c:crossBetween of the value axis crossing it (ECMA-376 Part 1, 21.2.2.32),
// else between for bar and line groups and on the ticks for the others; a
// 3D bar group always between, a radar group never.
func (p *chartReader) categoryPositions() {
	done := map[int]bool{}
	for _, g := range p.c.Groups {
		x, y := g.XAxis, g.YAxis
		if x < 0 || x >= len(p.c.Axes) || done[x] {
			continue
		}
		a := p.c.Axes[x]
		if a.Kind != chart.CategoryAxis && a.Kind != chart.DateAxis {
			continue
		}
		done[x] = true
		cb := ""
		if y >= 0 && y < len(p.crossBetween) {
			cb = p.crossBetween[y]
		}
		switch {
		case g.ThreeD && g.Kind == chart.Bar:
			a.BetweenCategories = true
		case g.Kind == chart.Radar:
			a.BetweenCategories = false
		case cb != "":
			a.BetweenCategories = cb == "between"
		default:
			a.BetweenCategories = g.Kind == chart.Bar || g.Kind == chart.Line
		}
	}
}

// palette returns the automatic series colours of the chart's style
// (oox's spFilledSeries2dFills): the six accents for the colourful styles
// (2, 10, 18, 26, 34, 42), shades of the dark colour for the grey ones (1,
// 9, 17, 25, 33, 41), and for the others one accent each series faded from
// dark to light.
func (p *chartReader) palette(accents []string) []typeset.Color {
	col := func(name string) typeset.Color {
		c, _ := p.theme.Color(name)
		return toTypeset(c)
	}
	switch m := (p.style - 1) % 8; {
	case m == 1 || p.style == 42:
		out := make([]typeset.Color, len(accents))
		for i, a := range accents {
			out[i] = col(a)
		}
		return out
	case m == 0:
		dk, _ := p.theme.Color("dk1")
		var out []typeset.Color
		for _, t := range []float64{0.885, 0.55, 0.78, 0.925, 0.70, 0.30} {
			out = append(out, toTypeset(linearTint(dk, t)))
		}
		return out
	default:
		return []typeset.Color{col(accents[(m-2)%6])}
	}
}

// autoFrame returns the automatic frame of the chart area or the plot
// area for the chart's style (oox's spChartSpaceFill and
// spPlotArea2dFills): the chart area takes the background colour (styles
// 1 to 32), the light colour (33 to 40) or the dark colour (41 to 48) with
// a light grey border, oox's default chart area line; the plot area the
// background colour (1 to 32), the dark colour tinted to 20% (33 and 34),
// the style's accent tinted to 20% (35 to 40) or the dark colour tinted to
// 95% (41 to 48), with no border. A presentation's chart area has no
// border, and for styles 1 to 32 neither area has a fill.
func (p *chartReader) autoFrame(area bool) chart.Frame {
	f := chart.Frame{}
	col := func(name string, tint float64) *chart.Paint {
		c, ok := p.theme.Color(name)
		if !ok {
			return nil
		}
		if tint < 1 {
			c = linearTint(c, tint)
		}
		return &chart.Paint{Color: toTypeset(c)}
	}
	switch {
	case p.impress && p.style <= 32:
	case area && p.style >= 41:
		f.Fill = col("dk1", 1)
	case area && p.style >= 33:
		f.Fill = col("lt1", 1)
	case p.style <= 32:
		f.Fill = col("bg1", 1)
	case p.style <= 34:
		f.Fill = col("dk1", 0.2)
	case p.style <= 40:
		f.Fill = col("accent"+strconv.Itoa(p.style-34), 0.2)
	default:
		f.Fill = col("dk1", 0.95)
	}
	if area && !p.impress {
		f.Border = &chart.Stroke{Color: toTypeset(hexColor(0xd9d9d9)), Width: 0.75}
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
	return &chart.Stroke{Color: toTypeset(linearTint(c, 0.75)), Width: themeLineWidth}
}

// linearTint lightens c as oox's colour tint does for the automatic chart
// formats: towards white in linear RGB, keeping the fraction t of the
// colour.
func linearTint(c css.Color, t float64) css.Color {
	f := func(v float64) float64 {
		l := toLinear(v)
		return fromLinear(1 - float64((1-l)*t))
	}
	return css.Color{R: f(c.R), G: f(c.G), B: f(c.B), A: c.A}
}

// toLinear and fromLinear convert an sRGB channel to and from linear
// light (IEC 61966-2-1).
func toLinear(v float64) float64 {
	if v <= 0.04045 {
		return v / 12.92
	}
	return detmath.Pow((v+0.055)/1.055, 2.4)
}

func fromLinear(v float64) float64 {
	if v <= 0.0031308 {
		return v * 12.92
	}
	return float64(1.055*detmath.Pow(v, 1/2.4)) - 0.055
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
// one: a:noFill, a:solidFill, a:gradFill (its middle stop, and the
// gradient blended as Office blends it, [Ramp]), a:pattFill (its
// foreground colour) (ECMA-376 Part 1, 20.1.8).
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
				return &chart.Paint{Color: toTypeset(c)}, true
			}
			return nil, true
		case "gradFill":
			g := parseGradientStops(e, p.theme)
			if g == nil {
				return nil, true
			}
			return &chart.Paint{
				Color: g.Stops[len(g.Stops)/2].Color,
				Shape: &shape.Paint{Kind: shape.PaintGradient, Stops: rampStops(g.Stops), Angle: g.Angle, Radial: g.Radial},
			}, true
		case "pattFill":
			// A pattern without colours draws black on white (20.1.8.47
			// pattFill's fgClr and bgClr defaults, as LibreOffice reads them).
			fg, bg := typeset.Black, typeset.Color{R: 1, G: 1, B: 1, A: 1}
			if c, ok := SchemeColor(e.Child(NSA, "fgClr"), p.theme); ok {
				fg = toTypeset(c)
			}
			if c, ok := SchemeColor(e.Child(NSA, "bgClr"), p.theme); ok {
				bg = toTypeset(c)
			}
			prst := attrOr(e, "prst")
			if prst == "" {
				prst = "pct5"
			}
			return &chart.Paint{Color: fg, Shape: &shape.Paint{Kind: shape.PaintPattern, Pattern: prst, Color: fg, Background: bg}}, true
		case "blipFill", "grpFill":
			return nil, true
		}
	}
	return nil, false
}

// stroke reads an a:ln element over the automatic line def (ECMA-376
// Part 1, 20.1.2.2.24).
func (p *chartReader) stroke(ln *Element, def *chart.Stroke) *chart.Stroke {
	s := chart.Stroke{Width: themeLineWidth, Color: typeset.Black}
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
				s.Color, s.None, set = toTypeset(c), false, true
			}
		case "gradFill":
			if stops := e.Child(NSA, "gsLst").Kids(); len(stops) > 0 {
				if c, ok := SchemeColor(stops[0], p.theme); ok {
					s.Color, s.None, set = toTypeset(c), false, true
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
	color            *typeset.Color
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
			cc := toTypeset(c)
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
		st.Color = toTypeset(c)
	} else {
		st.Color = typeset.Black
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
		if len(p.c.Groups) == 1 && (len(p.c.Groups[0].Series) == 1 || p.c.Groups[0].Kind == chart.Pie) && len(p.c.Groups[0].Series) > 0 {
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
		Layout:  manualLayout(t),
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
	l.Layout = manualLayout(lg)
	for _, e := range lg.Kids() {
		if e.Is(NSChart, "legendEntry") && boolVal(e, "delete", false) {
			if v, ok := Int(childVal(e, "idx")); ok && v >= 0 {
				l.Hidden = append(l.Hidden, v)
			}
		}
	}
	p.c.Legend = l
}

// timeUnit maps an ST_TimeUnit value (ECMA-376 Part 1, 21.2.3.49) to its
// unit, TimeAuto for none or an unknown one.
func timeUnit(v string) chart.TimeUnit {
	switch v {
	case "days":
		return chart.TimeDays
	case "months":
		return chart.TimeMonths
	case "years":
		return chart.TimeYears
	}
	return chart.TimeAuto
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
		p.crossBetween = append(p.crossBetween, childVal(e, "crossBetween"))
	}
}

// axis reads one axis element (ECMA-376 Part 1, 21.2.2.25 catAx and its
// siblings). A numFmt linked to its source (21.2.2.121, sourceLinked)
// labels the axis in the source data's format, whatever its formatCode
// says.
//
// # Corpus evidence
//
//   - litho/xlsx: charts_axes_legends (log axis, formatCode "#,##0") and
//     real_dashboard (formatCode '#,##0,"k"'), both sourceLinked over
//     General cells, print 1000 and 1400000 in Excel; real_budget
//     (formatCode "#,##0" over General caches and cells of
//     "#,##0;[Red]-#,##0") prints 6,000; real_financial_statement over
//     cells of the built-in format 37 prints 16,000.
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
		if attrOr(nf, "sourceLinked") == "1" {
			a.SourceLinked = true
		} else if code := attrOr(nf, "formatCode"); code != "" {
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
	a.MajorTicks = tickMarkOf(e, "majorTickMark", chart.TickCross)
	a.MinorTicks = tickMarkOf(e, "minorTickMark", chart.TickNone)
	if g := e.Child(NSChart, "majorGridlines"); g != nil {
		a.MajorGrid = p.lineOf(g.Child(NSChart, "spPr"), p.autoLine())
	}
	if g := e.Child(NSChart, "minorGridlines"); g != nil {
		minor := p.autoLine()
		tx, _ := p.theme.Color("tx1")
		minor.Color = toTypeset(linearTint(tx, 0.5))
		a.MinorGrid = p.lineOf(g.Child(NSChart, "spPr"), minor)
	}
	a.Line = p.lineOf(e.Child(NSChart, "spPr"), p.autoLine())
	tp := p.textProps(e.Child(NSChart, "txPr"))
	a.Style = p.textStyle(tp, 10, false, 1)
	switch kind {
	case chart.DateAxis:
		// A date axis keeps chart2's defaults: no line breaks.
		a.NoBreak = true
		a.BaseTimeUnit = timeUnit(childVal(e, "baseTimeUnit"))
		a.MajorTimeUnit = timeUnit(childVal(e, "majorTimeUnit"))
	case chart.CategoryAxis:
		// oox's AxisConverter: labels turned by the file never overlap and
		// break only when upright or turned a quarter beside a plot area
		// the file does not place; an explicit zero turn lets them
		// overlap.
		r := math.Mod(tp.rotation+360, 360)
		a.Overlap = tp.hasRotation && r == 0
		a.NoBreak = tp.hasRotation && r != 0 && (p.fixedInner || r != 90 && r != 270)
	case chart.ValueAxis, chart.SeriesAxis:
	}
	if t := e.Child(NSChart, "title"); t != nil {
		own := p.textProps(t.Child(NSChart, "txPr"))
		if rich := t.Child(NSChart, "tx").Child(NSChart, "rich"); rich != nil {
			own = p.textProps(rich)
		}
		text := richText(t.Child(NSChart, "tx"))
		if text == "" {
			text = "Axis Title"
		}
		// A body rotation of zero keeps the title of a vertical axis
		// unturned, where a title that states none turns (ECMA-376 Part 1,
		// 21.1.2.1.1 bodyPr rot).
		upright := own.hasRotation && own.rotation == 0
		a.Title = &chart.Title{Text: text, Style: p.textStyle(own, 10, true, 1), Frame: p.frame(t.Child(NSChart, "spPr"), chart.Frame{}), Upright: upright}
	}
	p.displayUnits(e.Child(NSChart, "dispUnits"), a)
	a.BetweenCategories = childVal(e, "crossBetween") != "midCat"
	return a
}

// builtInUnits are the divisors of the built-in display units (ECMA-376
// Part 1, 21.2.3.6 ST_BuiltInUnit).
var builtInUnits = map[string]float64{
	"hundreds": 1e2, "thousands": 1e3, "tenThousands": 1e4, "hundredThousands": 1e5,
	"millions": 1e6, "tenMillions": 1e7, "hundredMillions": 1e8, "billions": 1e9, "trillions": 1e12,
}

// displayUnits reads a value axis's dispUnits (ECMA-376 Part 1, 21.2.2.47):
// a built-in unit (21.2.2.17) or a custom one (21.2.2.38) divides the
// values its labels show, and a dispUnitsLbl (21.2.2.46) states the unit
// as the axis's title when it has none: its own text, else "Millions" for
// the millions unit. The label of the other built-in units is the
// application's and is not drawn.
//
// # Corpus evidence
//
//   - litho/pptx: chart-labels-axes chart7 (millions, an empty
//     dispUnitsLbl, values 1,200,000 to 5,600,000) prints 0 to 6 and
//     "Millions" turned beside the axis in PowerPoint.
func (p *chartReader) displayUnits(d *Element, a *chart.Axis) {
	if d == nil {
		return
	}
	unit := floatVal(d, "custUnit")
	name := childVal(d, "builtInUnit")
	if v, ok := builtInUnits[name]; ok {
		unit = v
	}
	if !numeric.PositiveFinite(unit) {
		return
	}
	a.DisplayUnit = unit
	lbl := d.Child(NSChart, "dispUnitsLbl")
	if lbl == nil || a.Title != nil {
		return
	}
	text := richText(lbl.Child(NSChart, "tx"))
	if text == "" && name == "millions" {
		text = "Millions"
	}
	if text == "" {
		return
	}
	own := p.textProps(lbl.Child(NSChart, "txPr"))
	if rich := lbl.Child(NSChart, "tx").Child(NSChart, "rich"); rich != nil {
		own = p.textProps(rich)
	}
	a.Title = &chart.Title{Text: text, Style: p.textStyle(own, 10, false, 1), Frame: p.frame(lbl.Child(NSChart, "spPr"), chart.Frame{})}
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

// tickMarkOf reads the tick mark child c:local of an axis: def when the
// element is absent, cross when it has no val, the schema's default for
// CT_TickMark's val (ECMA-376 Part 1, 21.2.2.101 majorTickMark, 21.2.2.110
// minorTickMark).
func tickMarkOf(e *Element, local string, def chart.TickMark) chart.TickMark {
	c := e.Child(NSChart, local)
	if c == nil {
		return def
	}
	return tickMark(attrOr(c, "val"), chart.TickCross)
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

// unnamedSeries returns the name Office gives a series whose file names it
// not (no c:tx, ECMA-376 Part 1, 21.2.2.215): "Series" and its index plus
// one, whatever its values' range.
//
// # Corpus evidence
//
//   - libreoffice/xlsx, openxmlsdk/xlsx, poi/xlsx: in the 17 workbooks
//     whose legend lists a series without c:tx, Excel prints "Series1",
//     "Series2" and on; none prints a name after the range, such as
//     "Column B" for chart-area-style-border's series over $B$1:$B$4.
func unnamedSeries(index int) string {
	return "Series" + strconv.Itoa(index+1)
}

// rangeName returns the name a spreadsheet gives a series without one,
// after its values' range: "Column B" for a range down one column, "Row 3"
// for a range across one row, "" for another reference (LibreOffice
// Calc's ScChart2DataSequence::generateLabel).
func rangeName(ref string) string {
	ref = strings.ReplaceAll(ref, "$", "")
	from, to, isRange := strings.Cut(ref, ":")
	if i := strings.LastIndexByte(from, '!'); i >= 0 {
		from = from[i+1:]
	}
	c1, r1, ok := splitCell(from)
	if !ok {
		return ""
	}
	c2, r2 := c1, r1
	if isRange {
		if c2, r2, ok = splitCell(to); !ok {
			return ""
		}
	}
	if r1 == r2 && c1 != c2 {
		return "Row " + strconv.Itoa(r1)
	}
	return "Column " + c1
}

// splitCell splits an A1 cell reference into its column letters and its
// row number.
func splitCell(s string) (string, int, bool) {
	i := 0
	for i < len(s) && s[i] >= 'A' && s[i] <= 'Z' {
		i++
	}
	if i == 0 || i > 3 {
		return "", 0, false
	}
	// A row index is an xsd:unsignedInt (ECMA-376 Part 1, 18.3.1.73), and
	// one past 2^31 - 1 names no row a sheet holds: it reads as no cell on
	// every platform.
	row, err := intconv.Atoi32(s[i:])
	if err != nil || row < 1 {
		return "", 0, false
	}
	return s[:i], row, true
}

// date1904Shift is the days a chart of a 1904 workbook adds to its date
// serials, as LibreOffice shows them: 1461, a day short of the 1462 days
// from the 1900 system's day 0 to 1904-01-01 that a cell's value takes
// (ECMA-376 Part 1, 21.2.2.38 date1904; 18.17.4.1).
//
// # Corpus evidence
//
//   - libreoffice/pptx: chart2/1904NullDate.pptx, serial 37257 under m/d/yyyy prints
//     1/1/2006.
const date1904Shift = 1461

// shift1904 moves the numbers of d into the 1900 date system when its
// format shows a date or a time: a chart of a 1904 workbook counts its
// dates from 1904-01-01 (ECMA-376 Part 1, 21.2.2.38 date1904).
func shift1904(d *chart.Data) {
	if !isDateCode(d.Format) {
		return
	}
	for i, v := range d.Values {
		if v.Kind == chart.Number {
			d.Values[i].Number += date1904Shift
		}
	}
}

// isDateCode reports a number format code that shows a date or a time: a
// y, m, d, h or s outside quotes, brackets and escapes (ECMA-376 Part 1,
// 18.8.31 numFmt). A time shifted by whole days shows alike.
func isDateCode(code string) bool {
	for i := 0; i < len(code); i++ {
		switch c := code[i]; c {
		case '"', '[':
			// A quoted text or a bracketed section runs to its closing
			// character, or to the end of the code.
			end := byte('"')
			if c == '[' {
				end = ']'
			}
			if j := strings.IndexByte(code[i+1:], end); j >= 0 {
				i += j + 1
			} else {
				i = len(code)
			}
		case '\\', '_', '*':
			i++
		case 'y', 'Y', 'm', 'M', 'd', 'D', 'h', 'H', 's', 'S':
			return !strings.EqualFold(code, "General")
		}
	}
	return false
}

// manualLayout reads the c:layout of e (ECMA-376 Part 1, 21.2.2.88 and
// 21.2.2.95 manualLayout), nil when automatic. Only a corner in the edge
// mode places a part: oox's LayoutConverter leaves the factor mode, an
// offset from the automatic place and the default, automatic. A size in
// the edge mode is the far edge, in the factor mode the size.
func manualLayout(e *Element) *chart.ManualLayout {
	ml := e.Child(NSChart, "layout").Child(NSChart, "manualLayout")
	if ml == nil || childVal(ml, "xMode") != "edge" || childVal(ml, "yMode") != "edge" {
		return nil
	}
	clamp := func(v float64) float64 {
		if math.IsNaN(v) {
			return 0
		}
		return math.Max(0, math.Min(1, v))
	}
	l := &chart.ManualLayout{X: clamp(floatVal(ml, "x")), Y: clamp(floatVal(ml, "y")), Inner: childVal(ml, "layoutTarget") == "inner"}
	size := func(pos float64, local, mode string) float64 {
		v := clamp(floatVal(ml, local))
		if childVal(ml, mode) == "edge" {
			v -= pos
		}
		return math.Max(0, math.Min(v, 1-pos))
	}
	l.Width, l.Height = size(l.X, "w", "wMode"), size(l.Y, "h", "hMode")
	return l
}
