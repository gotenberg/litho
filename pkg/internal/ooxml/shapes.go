package ooxml

import (
	"math"
	"strconv"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/shape"
)

// ShapeStyle is the fill and outline of a DrawingML shape (ECMA-376 Part
// 1, 20.1.2.2.35 spPr): nil Fill (and nil Gradient) or Line for none.
type ShapeStyle struct {
	Fill *css.Color
	// Gradient, when set, fills instead of Fill (20.1.8.33 gradFill).
	Gradient *Gradient
	Line     *css.Color
	// LineWidth is the outline width in points.
	LineWidth float64
	// Dash is the preset dash (20.1.10.48 ST_PresetLineDashVal), "" for a
	// solid line.
	Dash string
	// CustDash is a custom dash (20.1.8.21 custDash), its dash and space
	// lengths in line widths, nil for none; it takes over Dash.
	CustDash []float64
	// Cap and Join are the line's cap and join (20.1.10.31 ST_LineCap,
	// 20.1.8.52 to 20.1.8.54): flat and round unless the file says.
	Cap  compose.LineCap
	Join compose.LineJoin
	// Head and Tail are the arrowheads (20.1.8.38 headEnd, 20.1.8.57
	// tailEnd).
	Head, Tail shape.Arrowhead
}

// Gradient is a gradient fill: its stops, and a linear gradient's angle
// in degrees clockwise or a path gradient drawn as radial.
type Gradient struct {
	Stops  []compose.ColorStop
	Angle  float64
	Radial bool
}

// ParseShapeStyle reads a shape's properties spPr and its style reference
// (wps:style or p:style: the theme colours fillRef and lnRef name, used
// when spPr sets none), colours resolved in theme.
func ParseShapeStyle(spPr, style *Element, theme *Theme) ShapeStyle {
	var s ShapeStyle
	fillSet, lineSet := false, false
	if fill := spPr.Child(NSA, "solidFill"); fill != nil {
		if c, ok := SchemeColor(fill, theme); ok {
			s.Fill = &c
		}
		fillSet = true
	} else if spPr.Child(NSA, "noFill") != nil || spPr.Child(NSA, "blipFill") != nil {
		fillSet = true
	} else if g := spPr.Child(NSA, "gradFill"); g != nil {
		s.Gradient = parseGradient(g, theme)
		if s.Gradient != nil {
			c := css.Color{R: s.Gradient.Stops[0].Color.R, G: s.Gradient.Stops[0].Color.G, B: s.Gradient.Stops[0].Color.B, A: s.Gradient.Stops[0].Color.A}
			s.Fill = &c
		}
		fillSet = true
	}
	s.LineWidth = 0.75
	s.Join = compose.RoundJoin
	if ln := spPr.Child(NSA, "ln"); ln != nil {
		if v, ok := EMU(attrOr(ln, "w")); ok && v >= 0 {
			s.LineWidth = v
		}
		if ln.Child(NSA, "noFill") != nil {
			lineSet = true
		} else if fill := ln.Child(NSA, "solidFill"); fill != nil {
			if c, ok := SchemeColor(fill, theme); ok {
				s.Line = &c
			}
			lineSet = true
		}
		parseLineStyle(&s, ln)
	}
	if style != nil {
		if !fillSet {
			if c, ok := SchemeColor(style.Child(NSA, "fillRef"), theme); ok {
				s.Fill = &c
			}
		}
		if !lineSet {
			if c, ok := SchemeColor(style.Child(NSA, "lnRef"), theme); ok {
				s.Line = &c
			}
		}
	}
	return s
}

// parseLineStyle reads a line's dash, cap, join and arrowheads (20.1.2.2.24
// ln).
func parseLineStyle(s *ShapeStyle, ln *Element) {
	if d := ln.Child(NSA, "prstDash"); d != nil {
		if v := attrOr(d, "val"); v != "solid" {
			s.Dash = v
		}
	} else if d := ln.Child(NSA, "custDash"); d != nil {
		for _, ds := range d.Children {
			if !ds.Is(NSA, "ds") || len(s.CustDash) >= maxCustDashes {
				continue
			}
			// 20.1.10.44 ST_PositivePercentage: thousandths of a percent
			// of the line width.
			dv, _ := strconv.Atoi(attrOr(ds, "d"))
			sv, _ := strconv.Atoi(attrOr(ds, "sp"))
			s.CustDash = append(s.CustDash, max(float64(dv), 0)/100000, max(float64(sv), 0)/100000)
		}
	}
	switch attrOr(ln, "cap") {
	case "rnd":
		s.Cap = compose.RoundCap
	case "sq":
		s.Cap = compose.SquareCap
	}
	switch {
	case ln.Child(NSA, "bevel") != nil:
		s.Join = compose.BevelJoin
	case ln.Child(NSA, "miter") != nil:
		s.Join = compose.MiterJoin
	}
	s.Head = arrowhead(ln.Child(NSA, "headEnd"))
	s.Tail = arrowhead(ln.Child(NSA, "tailEnd"))
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
// (20.1.8.36 gs, pos in 1/1000 of a percent), a linear one's angle
// (20.1.8.41 lin ang, 60,000ths of a degree), or a path one (20.1.8.46
// path) drawn as radial. nil without a stop.
func parseGradient(g *Element, theme *Theme) *Gradient {
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
		out.Stops = append(out.Stops, compose.ColorStop{Offset: pos / 100000, Color: compose.Color{R: c.R, G: c.G, B: c.B, A: c.A}})
	}
	if len(out.Stops) == 0 {
		return nil
	}
	if lin := g.Child(NSA, "lin"); lin != nil {
		if v, err := strconv.ParseFloat(attrOr(lin, "ang"), 64); err == nil && !math.IsNaN(v) && !math.IsInf(v, 0) {
			out.Angle = v / 60000
		}
	} else if g.Child(NSA, "path") != nil {
		out.Radial = true
	}
	return out
}

// Paint returns the fragments that draw the geometry g fitted to r with
// style s: each path the geometry fills painted with the fill, shaded
// for a lighten or darken path (ECMA-376 Part 1, 20.1.10.37), then each
// path it strokes drawn with the line, its dash, cap, join and
// arrowheads (shape.Fill, shape.Stroke). A nil g is the rectangle r.
func (s ShapeStyle) Paint(g *shape.Geometry, r compose.Rect) []compose.Fragment {
	if s.Fill == nil && s.Gradient == nil && s.Line == nil {
		return nil
	}
	paths := shape.Paths(g, r)
	var out, strokes []compose.Fragment
	for _, p := range paths {
		if p.Fill {
			out = append(out, shape.Fill(p.Ops, r, s.paint(p.Mode))...)
		}
		if p.Stroke && s.Line != nil && s.LineWidth > 0 {
			c := compose.Color{R: s.Line.R, G: s.Line.G, B: s.Line.B, A: s.Line.A}
			strokes = append(strokes, shape.Stroke(p.Ops, shape.Line{
				Width: s.LineWidth, Color: c, Cap: s.Cap, Join: s.Join,
				Dash: s.dash(), Head: s.Head, Tail: s.Tail,
			})...)
		}
	}
	return append(out, strokes...)
}

// maxCustDashes bounds the dash and space lengths of a custom dash read:
// 64, 32 pairs.
// Anti-DoS guard, not spec-mandated: Office's dialogs make at most a few,
// and each pair adds to every stroke's dash array.
const maxCustDashes = 64

// dash returns the dash array in points of the line: the custom dash in
// line widths when there is one, else the preset's. A dash array whose
// lengths are all zero draws solid.
func (s ShapeStyle) dash() []float64 {
	if len(s.CustDash) == 0 {
		return shape.PresetDash(s.Dash, s.LineWidth, s.Cap)
	}
	w := s.LineWidth
	if w <= 0 {
		w = 1
	}
	out := make([]float64, len(s.CustDash))
	sum := 0.0
	for i, v := range s.CustDash {
		out[i] = v * w
		sum += out[i]
	}
	if !(sum > 0) {
		return nil
	}
	return out
}

// paint returns the fill of a path of fill mode m.
func (s ShapeStyle) paint(m shape.FillMode) shape.Paint {
	if s.Gradient != nil {
		stops := make([]compose.ColorStop, len(s.Gradient.Stops))
		for i, st := range s.Gradient.Stops {
			stops[i] = compose.ColorStop{Offset: st.Offset, Color: shape.Shade(st.Color, m)}
		}
		return shape.Paint{Kind: shape.PaintGradient, Stops: stops, Angle: s.Gradient.Angle, Radial: s.Gradient.Radial}
	}
	if s.Fill == nil {
		return shape.Paint{}
	}
	c := compose.Color{R: s.Fill.R, G: s.Fill.G, B: s.Fill.B, A: s.Fill.A}
	return shape.Paint{Kind: shape.PaintSolid, Color: shape.Shade(c, m)}
}
