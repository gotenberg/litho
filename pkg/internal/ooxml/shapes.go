package ooxml

import (
	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/shape"
)

// PresetPath returns the outline of the preset geometry name (ECMA-376
// Part 1, 20.1.10.56 ST_ShapeType) fitted to r, through the shape
// package's engine, and whether the engine draws that preset rather than
// its bounding box.
func PresetPath(name string, r compose.Rect) ([]compose.PathOp, bool) {
	g := &shape.Geometry{Preset: name}
	paths := shape.Paths(g, r)
	return paths[0].Ops, shape.Known(g)
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
