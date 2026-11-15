package ooxml

import (
	"math"
	"strconv"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/intconv"
	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/shape"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// ShapeStyle is the fill and outline of a DrawingML shape (ECMA-376 Part
// 1, 20.1.2.2.35 spPr): nil Fill (and nil Gradient) or Line for none.
type ShapeStyle struct {
	Fill *css.Color
	// Gradient, when set, fills instead of Fill (20.1.8.33 gradFill).
	Gradient *Gradient
	// Pattern is a pattern fill's preset (20.1.8.47 pattFill prst), ""
	// for none; Fill is then its foreground, PatternBg its background.
	Pattern   string
	PatternBg *css.Color
	// Blip is a picture fill's picture (20.1.8.14 blipFill), nil for none:
	// the caller resolves its relationship into Picture.
	Blip *BlipFill
	Line *css.Color
	// LineGradient, when set, paints the line instead of Line (20.1.2.2.24
	// ln with a gradFill).
	LineGradient *Gradient
	// LineWidth is the outline width in points.
	LineWidth float64
	// Dash is the preset dash (20.1.10.48 ST_PresetLineDashVal), "" for a
	// solid line.
	Dash string
	// CustDash is a custom dash (20.1.8.21 custDash), its dash and space
	// lengths in line widths, nil for none; it takes over Dash.
	CustDash []float64
	// Cap and Join are the line's cap and join (20.1.10.31 ST_LineCap,
	// 20.1.8.52 to 20.1.8.54): flat and round unless the shape or its
	// theme line says.
	Cap  typeset.LineCap
	Join typeset.LineJoin
	// Head and Tail are the arrowheads (20.1.8.38 headEnd, 20.1.8.57
	// tailEnd).
	Head, Tail shape.Arrowhead
}

// Gradient is a gradient fill: its stops, and a linear gradient's angle
// in degrees clockwise, or a path gradient drawn as radial, or along
// rectangles for a path rect.
type Gradient struct {
	Stops  []typeset.ColorStop
	Angle  float64
	Radial bool
	Rect   bool
}

// BlipFill is a picture fill (20.1.8.14 blipFill): its picture's
// relationship ID, and whether it tiles (20.1.8.58 tile) at Scale times
// its natural size, or stretches (20.1.8.56 stretch).
type BlipFill struct {
	Embed string
	Tile  bool
	// ScaleX and ScaleY are the tile's scale (tile sx, sy), 1 by default.
	ScaleX, ScaleY float64
	// Picture is the picture the caller resolved Embed to, nil until it
	// does.
	Picture *typeset.ImageSource
	// Width and Height are the picture's natural size in points.
	Width, Height float64
}

// ParseShapeStyle reads a shape's properties spPr and its style reference
// (wps:style or p:style): the theme colour fillRef names when spPr sets no
// fill, and each line property spPr's a:ln leaves unset from the theme
// line lnRef names, colours resolved in theme.
func ParseShapeStyle(spPr, style *Element, theme *Theme) ShapeStyle {
	var s ShapeStyle
	fillSet := false
	if fill := spPr.Child(NSA, "solidFill"); fill != nil {
		if c, ok := SchemeColor(fill, theme); ok {
			s.Fill = &c
		}
		fillSet = true
	} else if spPr.Child(NSA, "noFill") != nil {
		fillSet = true
	} else if bf := spPr.Child(NSA, "blipFill"); bf != nil {
		s.Blip = parseBlipFill(bf)
		fillSet = true
	} else if pf := spPr.Child(NSA, "pattFill"); pf != nil {
		s.Pattern = attrOr(pf, "prst")
		if s.Pattern == "" {
			// 20.1.8.47 pattFill gives prst no default: a missing one draws
			// pct5, the first value of 20.1.10.51 ST_PresetPatternVal.
			s.Pattern = "pct5"
		}
		fg := css.Color{A: 1}
		if c, ok := SchemeColor(pf.Child(NSA, "fgClr"), theme); ok {
			fg = c
		}
		bg := css.Color{R: 1, G: 1, B: 1, A: 1}
		if c, ok := SchemeColor(pf.Child(NSA, "bgClr"), theme); ok {
			bg = c
		}
		s.Fill, s.PatternBg = &fg, &bg
		fillSet = true
	} else if g := spPr.Child(NSA, "gradFill"); g != nil {
		s.Gradient = parseGradient(g, theme)
		if s.Gradient != nil {
			c := css.Color{R: s.Gradient.Stops[0].Color.R, G: s.Gradient.Stops[0].Color.G, B: s.Gradient.Stops[0].Color.B, A: s.Gradient.Stops[0].Color.A}
			s.Fill = &c
		}
		fillSet = true
	}
	ln := spPr.Child(NSA, "ln")
	themeLn, ph := themeLine(style, theme)
	lns := []*Element{ln, themeLn}
	s.LineWidth = 0.75
	for _, l := range lns {
		if v, ok := EMU(attrOr(l, "w")); ok && v >= 0 {
			s.LineWidth = v
			break
		}
	}
	switch {
	case lineFill(ln) != nil:
		switch fill := lineFill(ln); fill.Local {
		case "solidFill":
			if c, ok := SchemeColor(fill, theme); ok {
				s.Line = &c
			}
		case "gradFill":
			if s.LineGradient = parseGradient(fill, theme); s.LineGradient != nil {
				first := s.LineGradient.Stops[0].Color
				s.Line = &css.Color{R: first.R, G: first.G, B: first.B, A: first.A}
			}
		case "pattFill":
			if c, ok := SchemeColor(fill.Child(NSA, "fgClr"), theme); ok {
				s.Line = &c
			}
		}
	case themeLn != nil:
		// The theme's line paints its own solid fill, phClr standing for
		// the reference's colour (20.1.4.2.19 lnRef), and the reference's
		// colour for any other fill or none.
		switch fill := lineFill(themeLn); {
		case fill == nil || fill.Local == "gradFill" || fill.Local == "pattFill":
			s.Line = ph
		case fill.Local == "solidFill":
			if c, ok := DrawingColor(fill, themeResolver(theme, ph)); ok {
				s.Line = &c
			}
		}
	case style != nil:
		if c, ok := SchemeColor(style.Child(NSA, "lnRef"), theme); ok {
			s.Line = &c
		}
	}
	parseLineStyle(&s, lns)
	if style != nil && !fillSet {
		if c, ok := SchemeColor(style.Child(NSA, "fillRef"), theme); ok {
			s.Fill = &c
		}
	}
	return s
}

// themeLine returns the theme line style a shape's style reference names
// (ECMA-376 Part 1, 20.1.4.2.19 lnRef, idx 1 or more into 20.1.4.1.21
// lnStyleLst) and the reference's colour, which phClr stands for in it;
// nil when it names none.
//
// # Corpus evidence
//
//   - libreoffice/docx: ooxmlexport/dml-shape-title.docx, Shape.docx,
//     fdo70942.docx and five more print the outline of lnRef idx 2 at the
//     theme's 25,400 EMU, 2 points, where the shape's spPr gives no width;
//     layout/tdf138782.docx at 12,700 EMU with the theme's miter join.
//   - libreoffice/docx: ooxmlexport/dml-gradientfill-theme.docx and
//     fdo77117.docx paint lnRef idx 1 of accent1 4F81BD as 74,126,187,
//     the theme line's phClr under its shade 95% and satMod 105%.
//   - litho/docx shapes-presets: 74 outlines of idx 2 at 1 point with miter
//     joins, the theme's 12,700 EMU and a:miter.
//   - libreoffice/xlsx: sc/groupShape.xlsx, tdf142881.xlsx,
//     tdf135828_Shape_Rect.xlsx and two more, litho/xlsx
//     groups_connectors and shapes_textboxes, poi/xlsx 50299.xlsx: Excel
//     prints the theme's width and join likewise.
func themeLine(style *Element, theme *Theme) (*Element, *css.Color) {
	ref := style.Child(NSA, "lnRef")
	idx, ok := Int(attrOr(ref, "idx"))
	if !ok || theme == nil || idx < 1 || idx > len(theme.LineStyles) {
		return nil, nil
	}
	if c, ok := SchemeColor(ref, theme); ok {
		return theme.LineStyles[idx-1], &c
	}
	return theme.LineStyles[idx-1], nil
}

// lineFill returns the fill of a line (20.1.2.2.24 ln: noFill, solidFill,
// gradFill or pattFill), nil when it sets none.
func lineFill(ln *Element) *Element {
	for _, k := range ln.Kids() {
		if k.Space == NSA && (k.Local == "noFill" || k.Local == "solidFill" || k.Local == "gradFill" || k.Local == "pattFill") {
			return k
		}
	}
	return nil
}

// parseLineStyle reads a line's dash, cap, join and arrowheads
// (20.1.2.2.24 ln), each from the first line of lns that sets it: the
// shape's own, then the theme's its style names. A line that sets no join
// is round.
func parseLineStyle(s *ShapeStyle, lns []*Element) {
	s.Join = typeset.RoundJoin
	for _, ln := range lns {
		if d := ln.Child(NSA, "prstDash"); d != nil {
			if v := attrOr(d, "val"); v != "solid" {
				s.Dash = v
			}
			break
		}
		if d := ln.Child(NSA, "custDash"); d != nil {
			for _, ds := range d.Children {
				if !ds.Is(NSA, "ds") || len(s.CustDash) >= maxCustDashes {
					continue
				}
				// 20.1.10.44 ST_PositivePercentage: thousandths of a percent
				// of the line width, an int, which a value past saturates.
				dv, _ := intconv.Atoi32(attrOr(ds, "d"))
				sv, _ := intconv.Atoi32(attrOr(ds, "sp"))
				s.CustDash = append(s.CustDash, max(float64(dv), 0)/100000, max(float64(sv), 0)/100000)
			}
			break
		}
	}
	for _, ln := range lns {
		if v, ok := ln.Attr("cap"); ok {
			switch v {
			case "rnd":
				s.Cap = typeset.RoundCap
			case "sq":
				s.Cap = typeset.SquareCap
			}
			break
		}
	}
	for _, ln := range lns {
		switch {
		case ln.Child(NSA, "round") != nil:
		case ln.Child(NSA, "bevel") != nil:
			s.Join = typeset.BevelJoin
		case ln.Child(NSA, "miter") != nil:
			s.Join = typeset.MiterJoin
		default:
			continue
		}
		break
	}
	for _, ln := range lns {
		if e := ln.Child(NSA, "headEnd"); e != nil {
			s.Head = arrowhead(e)
			break
		}
	}
	for _, ln := range lns {
		if e := ln.Child(NSA, "tailEnd"); e != nil {
			s.Tail = arrowhead(e)
			break
		}
	}
}

// arrowhead reads a headEnd or tailEnd (20.1.8.38, 20.1.10.33
// ST_LineEndType, 20.1.10.32 and 20.1.10.34 the sizes).
func arrowhead(e *Element) shape.Arrowhead {
	var a shape.Arrowhead
	switch attrOr(e, "type") {
	case "triangle":
		a.Kind = shape.ArrowTriangle
	case "stealth":
		a.Kind = shape.ArrowStealth
	case "diamond":
		a.Kind = shape.ArrowDiamond
	case "oval":
		a.Kind = shape.ArrowOval
	case "arrow":
		a.Kind = shape.ArrowOpen
	default:
		return a
	}
	size := func(v string) shape.ArrowSize {
		switch v {
		case "sm":
			return shape.ArrowSmall
		case "lg":
			return shape.ArrowLarge
		}
		return shape.ArrowMedium
	}
	a.Width, a.Length = size(attrOr(e, "w")), size(attrOr(e, "len"))
	return a
}

// maxGradientStops caps a gradient's stops. Anti-DoS guard, not
// spec-mandated: Office's gradient editor offers ten.
const maxGradientStops = 256

// parseGradient reads a gradient fill (20.1.8.33 gradFill): its stops
// (20.1.8.36 gs, pos in 1/1000 of a percent), blended as Office blends
// them ([Ramp]), a linear one's angle
// (20.1.8.41 lin ang, 60,000ths of a degree), or a path one (20.1.8.46
// path) drawn as radial. nil without a stop.
func parseGradient(g *Element, theme *Theme) *Gradient {
	out := parseGradientStops(g, theme)
	if out != nil {
		out.Stops = rampStops(out.Stops)
	}
	return out
}

// parseGradientStops reads a gradient fill as [parseGradient] does, its
// stops as the document gives them.
func parseGradientStops(g *Element, theme *Theme) *Gradient {
	out := &Gradient{}
	for _, gs := range g.Child(NSA, "gsLst").Kids() {
		if !gs.Is(NSA, "gs") || len(out.Stops) >= maxGradientStops {
			continue
		}
		c, ok := SchemeColor(gs, theme)
		if !ok {
			continue
		}
		pos, err := strconv.ParseFloat(attrOr(gs, "pos"), 64)
		if err != nil || math.IsNaN(pos) {
			pos = 0
		}
		out.Stops = append(out.Stops, typeset.ColorStop{Offset: pos / 100000, Color: typeset.Color{R: c.R, G: c.G, B: c.B, A: c.A}})
	}
	if len(out.Stops) == 0 {
		return nil
	}
	if lin := g.Child(NSA, "lin"); lin != nil {
		if v, err := strconv.ParseFloat(attrOr(lin, "ang"), 64); err == nil && !math.IsNaN(v) && !math.IsInf(v, 0) {
			out.Angle = v / 60000
		}
	} else if p := g.Child(NSA, "path"); p != nil {
		out.Radial = attrOr(p, "path") != "rect"
		out.Rect = !out.Radial
	}
	return out
}

// parseBlipFill reads a shape's picture fill (20.1.8.14 blipFill): its
// blip's embed, and its tile (20.1.8.58: sx, sy in thousandths of a
// percent, 100% by default) or its stretch.
func parseBlipFill(bf *Element) *BlipFill {
	b := &BlipFill{ScaleX: 1, ScaleY: 1}
	b.Embed, _ = bf.Child(NSA, "blip").AttrNS(NSR, "embed")
	if t := bf.Child(NSA, "tile"); t != nil {
		b.Tile = true
		if v, ok := Int(attrOr(t, "sx")); ok && v > 0 {
			b.ScaleX = float64(v) / 100000
		}
		if v, ok := Int(attrOr(t, "sy")); ok && v > 0 {
			b.ScaleY = float64(v) / 100000
		}
	}
	return b
}

// Paint returns the fragments that draw the geometry g fitted to r with
// style s: each path the geometry fills painted with the fill, shaded
// for a lighten or darken path (ECMA-376 Part 1, 20.1.10.37), then each
// path it strokes drawn with the line, its dash, cap, join and
// arrowheads (shape.Fill, shape.Stroke). A nil g is the rectangle r.
func (s ShapeStyle) Paint(g *shape.Geometry, r typeset.Rect) []typeset.Fragment {
	if s.Fill == nil && s.Gradient == nil && s.Line == nil && s.Blip == nil {
		return nil
	}
	paths := shape.Paths(g, r)
	var out, strokes []typeset.Fragment
	for _, p := range paths {
		if p.Fill {
			out = append(out, shape.Fill(p.Ops, r, s.paint(p.Mode))...)
		}
		if p.Stroke && s.Line != nil && s.LineWidth > 0 {
			c := typeset.Color{R: s.Line.R, G: s.Line.G, B: s.Line.B, A: s.Line.A}
			dash, lc := s.dash()
			l := shape.Line{
				Width: s.LineWidth, Color: c, Cap: lc, Join: s.Join,
				Dash: dash, Head: s.Head, Tail: s.Tail,
			}
			if g := s.LineGradient; g != nil {
				if frags := shape.GradientStroke(p.Ops, l, shape.Paint{Kind: shape.PaintGradient, Stops: g.Stops, Angle: g.Angle, Radial: g.Radial}, r); frags != nil {
					strokes = append(strokes, frags...)
					continue
				}
			}
			strokes = append(strokes, shape.Stroke(p.Ops, l)...)
		}
	}
	return append(out, strokes...)
}

// PaintOps returns the fragments that fill the outline ops, fitted to r,
// with the style's fill, then stroke it with the style's line, its dash,
// cap and join.
func (s ShapeStyle) PaintOps(ops []typeset.PathOp, r typeset.Rect) []typeset.Fragment {
	out := shape.Fill(ops, r, s.paint(shape.FillNorm))
	if s.Line != nil && s.LineWidth > 0 {
		c := typeset.Color{R: s.Line.R, G: s.Line.G, B: s.Line.B, A: s.Line.A}
		dash, lc := s.dash()
		out = append(out, shape.Stroke(ops, shape.Line{Width: s.LineWidth, Color: c, Cap: lc, Join: s.Join, Dash: dash})...)
	}
	return out
}

// maxCustDashes bounds the dash and space lengths of a custom dash read:
// 64, 32 pairs.
// Anti-DoS guard, not spec-mandated: Office's dialogs make at most a few,
// and each pair adds to every stroke's dash array.
const maxCustDashes = 64

// dash returns the dash array in points of the line and the cap it is
// stroked with: the custom dash in line widths when there is one, else the
// preset's, as [OfficeDash] draws it under the line's cap. A dash array
// whose lengths are all zero draws solid.
func (s ShapeStyle) dash() ([]float64, typeset.LineCap) {
	var out []float64
	if len(s.CustDash) == 0 {
		out = shape.PresetDash(s.Dash, s.LineWidth)
	} else {
		w := s.LineWidth
		if w <= 0 {
			w = 1
		}
		out = make([]float64, len(s.CustDash))
		sum := 0.0
		for i, v := range s.CustDash {
			out[i] = float64(v * w)
			sum += out[i]
		}
		if !numeric.Positive(sum) {
			return nil, s.Cap
		}
	}
	return OfficeDash(out, s.LineWidth, s.Cap)
}

// OfficeDash returns the dash array dash of a line width wide as Word,
// Excel and PowerPoint stroke it under the cap c, and the cap they stroke
// it with: a round cap
// shortens each dash by one width and lengthens each gap by as much, so
// that the caps end where the dash and the gap meet; a square cap strokes
// the lengths as they are with butt caps. A solid line keeps its cap.
//
// # Corpus evidence
//
//   - litho/docx shapes-fills-lines: Word strokes sysDash and
//     sysDashDotDot at 2.5 points under round caps as 5 5 and 5 5 0 5 0 5,
//     and sysDot and lgDashDot at 3.5 points under square caps as 3.5 3.5
//     and 28 10.5 3.5 10.5 with butt caps; its solid square-capped line
//     keeps the square cap.
//   - poi/xlsx: spreadsheet/56514.xlsx, Excel strokes a 10-point round
//     dot as 0 20.
//   - litho/pptx line-dashes: PowerPoint strokes dot, sysDot, dash,
//     dashDot, lgDash, lgDashDot, lgDashDotDot and sysDash at 4 points
//     under round caps as 0 16, 0 8, 12 16, 12 16 0 16, 28 16, 28 16 0 16,
//     28 16 0 16 0 16 and 8 8, and its flat-capped dashes at their
//     lengths. No PowerPoint drawing shows a dash under a square cap.
func OfficeDash(dash []float64, width float64, c typeset.LineCap) ([]float64, typeset.LineCap) {
	if len(dash) == 0 {
		return nil, c
	}
	switch c {
	case typeset.RoundCap:
		w := max(width, 0)
		for i := range dash {
			if i%2 == 0 {
				dash[i] = max(dash[i]-w, 0)
			} else {
				dash[i] += w
			}
		}
	case typeset.SquareCap:
		c = typeset.ButtCap
	case typeset.ButtCap:
	}
	return dash, c
}

// paint returns the fill of a path of fill mode m.
func (s ShapeStyle) paint(m shape.FillMode) shape.Paint {
	if s.Blip != nil {
		if s.Blip.Picture == nil {
			return shape.Paint{}
		}
		p := shape.Paint{Kind: shape.PaintPicture, Picture: s.Blip.Picture}
		if s.Blip.Tile {
			p.TileWidth, p.TileHeight = s.Blip.Width*s.Blip.ScaleX, s.Blip.Height*s.Blip.ScaleY
		}
		return p
	}
	if s.Gradient != nil {
		stops := make([]typeset.ColorStop, len(s.Gradient.Stops))
		for i, st := range s.Gradient.Stops {
			stops[i] = typeset.ColorStop{Offset: st.Offset, Color: shape.Shade(st.Color, m)}
		}
		return shape.Paint{Kind: shape.PaintGradient, Stops: stops, Angle: s.Gradient.Angle, Radial: s.Gradient.Radial, Rect: s.Gradient.Rect}
	}
	if s.Pattern != "" && s.Fill != nil && s.PatternBg != nil {
		fg, bg := s.Fill, s.PatternBg
		return shape.Paint{
			Kind: shape.PaintPattern, Pattern: s.Pattern,
			Color:      shape.Shade(typeset.Color{R: fg.R, G: fg.G, B: fg.B, A: fg.A}, m),
			Background: shape.Shade(typeset.Color{R: bg.R, G: bg.G, B: bg.B, A: bg.A}, m),
		}
	}
	if s.Fill == nil {
		return shape.Paint{}
	}
	c := typeset.Color{R: s.Fill.R, G: s.Fill.G, B: s.Fill.B, A: s.Fill.A}
	return shape.Paint{Kind: shape.PaintSolid, Color: shape.Shade(c, m)}
}
