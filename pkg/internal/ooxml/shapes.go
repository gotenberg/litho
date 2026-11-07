package ooxml

import (
	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
)

// kappa places the control points of a cubic Bézier quarter circle.
const kappa = 0.5522847498

// PresetPath returns the outline of the preset geometry name (ECMA-376
// Part 1, 20.1.10.56 ST_ShapeType) fitted to r: rect, roundRect (with the
// default adjustment, a sixth of the shorter side), ellipse, line and the
// connectors drawn as lines. It reports false for any other geometry,
// which a converter draws as its bounding box.
func PresetPath(name string, r compose.Rect) ([]compose.PathOp, bool) {
	x0, y0, x1, y1 := r.X, r.Y, r.X+r.Width, r.Y+r.Height
	pt := func(x, y float64) [3]compose.Point { return [3]compose.Point{{X: x, Y: y}} }
	switch name {
	case "rect", "flowChartProcess", "frame":
		return RectPath(r).Ops, true
	case "line", "straightConnector1", "bentConnector2", "bentConnector3", "curvedConnector3":
		return []compose.PathOp{{Kind: compose.MoveTo, P: pt(x0, y0)}, {Kind: compose.LineTo, P: pt(x1, y1)}}, true
	case "ellipse", "flowChartConnector":
		cx, cy, rx, ry := (x0+x1)/2, (y0+y1)/2, r.Width/2, r.Height/2
		kx, ky := rx*kappa, ry*kappa
		return []compose.PathOp{
			{Kind: compose.MoveTo, P: pt(cx+rx, cy)},
			{Kind: compose.CurveTo, P: [3]compose.Point{{X: cx + rx, Y: cy + ky}, {X: cx + kx, Y: cy + ry}, {X: cx, Y: cy + ry}}},
			{Kind: compose.CurveTo, P: [3]compose.Point{{X: cx - kx, Y: cy + ry}, {X: cx - rx, Y: cy + ky}, {X: cx - rx, Y: cy}}},
			{Kind: compose.CurveTo, P: [3]compose.Point{{X: cx - rx, Y: cy - ky}, {X: cx - kx, Y: cy - ry}, {X: cx, Y: cy - ry}}},
			{Kind: compose.CurveTo, P: [3]compose.Point{{X: cx + kx, Y: cy - ry}, {X: cx + rx, Y: cy - ky}, {X: cx + rx, Y: cy}}},
			{Kind: compose.ClosePath},
		}, true
	case "roundRect", "flowChartAlternateProcess":
		rad := min(r.Width, r.Height) / 6
		k := rad * (1 - kappa)
		return []compose.PathOp{
			{Kind: compose.MoveTo, P: pt(x0+rad, y0)},
			{Kind: compose.LineTo, P: pt(x1-rad, y0)},
			{Kind: compose.CurveTo, P: [3]compose.Point{{X: x1 - k, Y: y0}, {X: x1, Y: y0 + k}, {X: x1, Y: y0 + rad}}},
			{Kind: compose.LineTo, P: pt(x1, y1-rad)},
			{Kind: compose.CurveTo, P: [3]compose.Point{{X: x1, Y: y1 - k}, {X: x1 - k, Y: y1}, {X: x1 - rad, Y: y1}}},
			{Kind: compose.LineTo, P: pt(x0+rad, y1)},
			{Kind: compose.CurveTo, P: [3]compose.Point{{X: x0 + k, Y: y1}, {X: x0, Y: y1 - k}, {X: x0, Y: y1 - rad}}},
			{Kind: compose.LineTo, P: pt(x0, y0+rad)},
			{Kind: compose.CurveTo, P: [3]compose.Point{{X: x0, Y: y0 + k}, {X: x0 + k, Y: y0}, {X: x0 + rad, Y: y0}}},
			{Kind: compose.ClosePath},
		}, true
	}
	return RectPath(r).Ops, false
}

// ShapeStyle is the fill and outline of a DrawingML shape (ECMA-376 Part
// 1, 20.1.2.2.35 spPr): nil Fill or Line for none.
type ShapeStyle struct {
	Fill *css.Color
	Line *css.Color
	// LineWidth is the outline width in points.
	LineWidth float64
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
		// A gradient fills with its first stop, an approximation.
		if gs := g.Child(NSA, "gsLst"); gs != nil && len(gs.Children) > 0 {
			if c, ok := SchemeColor(gs.Children[0], theme); ok {
				s.Fill = &c
			}
		}
		fillSet = true
	}
	s.LineWidth = 0.75
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

// Paint returns the fragments that paint the outline ops with style s.
func (s ShapeStyle) Paint(ops []compose.PathOp) []compose.Fragment {
	if s.Fill == nil && s.Line == nil {
		return nil
	}
	p := &compose.Path{Ops: ops}
	if s.Fill != nil {
		p.Fill = &compose.Fill{Color: compose.Color{R: s.Fill.R, G: s.Fill.G, B: s.Fill.B, A: s.Fill.A}}
	}
	if s.Line != nil && s.LineWidth > 0 {
		p.Stroke = &compose.Stroke{Color: compose.Color{R: s.Line.R, G: s.Line.G, B: s.Line.B, A: s.Line.A}, Width: s.LineWidth}
	}
	return []compose.Fragment{p}
}
