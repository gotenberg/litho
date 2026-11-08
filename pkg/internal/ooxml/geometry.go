package ooxml

import (
	"strconv"

	"github.com/gotenberg/litho/pkg/internal/shape"
)

// ParseGeometry reads the geometry of a shape's properties spPr (ECMA-376
// Part 1, 20.1.2.2.35 spPr): a preset geometry (20.1.9.18 prstGeom) with
// the adjustment values of its avLst, or a custom geometry (20.1.9.8
// custGeom). It returns nil when spPr sets neither, which draws the
// shape's box. A preset name the engine does not know is kept, and
// [shape.Known] reports it.
func ParseGeometry(spPr *Element) *shape.Geometry {
	if c := spPr.Child(NSA, "custGeom"); c != nil {
		return &shape.Geometry{Custom: ParseCustomGeometry(c)}
	}
	p := spPr.Child(NSA, "prstGeom")
	if p == nil {
		return nil
	}
	g := &shape.Geometry{Preset: attrOr(p, "prst")}
	for _, gd := range p.Child(NSA, "avLst").Kids() {
		if !gd.Is(NSA, "gd") || len(g.Adjust) >= shape.MaxGuides {
			continue
		}
		// An adjustment value is "val n" (20.1.9.5 avLst); a formula
		// over other guides has no meaning before the preset's own are
		// evaluated, and is ignored.
		op, args, ok := shape.ParseFormula(attrOr(gd, "fmla"), nil)
		if !ok || op != shape.OpVal || args[0].Kind != shape.ArgConst {
			continue
		}
		g.Adjust = append(g.Adjust, shape.Adjust{Name: attrOr(gd, "name"), Value: args[0].Value})
	}
	return g
}

// ParseCustomGeometry reads a custom geometry (ECMA-376 Part 1, 20.1.9.8
// custGeom): the adjustments of avLst (20.1.9.5) and the guides of gdLst
// (20.1.9.12) as formulas (20.1.9.11), the text rectangle rect (20.1.9.22)
// and the paths of pathLst (20.1.9.16), through [shape.DrawingML]. The
// adjust handles (ahLst) and connection sites (cxnLst) are ignored.
func ParseCustomGeometry(el *Element) *shape.Custom {
	var d shape.DrawingML
	for _, list := range []string{"avLst", "gdLst"} {
		for _, gd := range el.Child(NSA, list).Kids() {
			if gd.Is(NSA, "gd") {
				d.Guide(attrOr(gd, "name"), attrOr(gd, "fmla"), list == "avLst")
			}
		}
	}
	if r := el.Child(NSA, "rect"); r != nil {
		d.Rect(attrOr(r, "l"), attrOr(r, "t"), attrOr(r, "r"), attrOr(r, "b"))
	}
	for _, p := range el.Child(NSA, "pathLst").Kids() {
		if !p.Is(NSA, "path") {
			continue
		}
		stroke := true
		if v, ok := p.Attr("stroke"); ok {
			stroke = OnOff(v, true)
		}
		d.Path(pathSize(attrOr(p, "w")), pathSize(attrOr(p, "h")), FillMode(attrOr(p, "fill")), stroke)
		for _, k := range p.Kids() {
			pathCommand(&d, k)
		}
	}
	return d.Custom()
}

// pathSize reads a path's w or h (20.1.10.42 ST_PositiveCoordinate), 0
// when absent or invalid.
func pathSize(s string) float64 {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v <= 0 || v > maxPathSize {
		return 0
	}
	return v
}

// maxPathSize is ST_PositiveCoordinate's upper bound (20.1.10.42).
const maxPathSize = 27273042316900

// FillMode reads a path's fill (ECMA-376 Part 1, 20.1.10.37
// ST_PathFillMode), norm by default.
func FillMode(s string) shape.FillMode {
	switch s {
	case "none":
		return shape.FillNone
	case "lighten":
		return shape.FillLighten
	case "lightenLess":
		return shape.FillLightenLess
	case "darken":
		return shape.FillDarken
	case "darkenLess":
		return shape.FillDarkenLess
	}
	return shape.FillNorm
}

// pathCommand adds the command k of a path (20.1.9.15 path: moveTo, lnTo,
// arcTo, quadBezTo, cubicBezTo, close) to d.
func pathCommand(d *shape.DrawingML, k *Element) {
	pts := func(n int) []string {
		var s []string
		for _, pt := range k.Kids() {
			if pt.Is(NSA, "pt") && len(s) < 2*n {
				s = append(s, attrOr(pt, "x"), attrOr(pt, "y"))
			}
		}
		for len(s) < 2*n {
			s = append(s, "0")
		}
		return s
	}
	if k.Space != NSA {
		return
	}
	switch k.Local {
	case "moveTo":
		d.Command(shape.CmdMove, pts(1)...)
	case "lnTo":
		d.Command(shape.CmdLine, pts(1)...)
	case "quadBezTo":
		d.Command(shape.CmdQuad, pts(2)...)
	case "cubicBezTo":
		d.Command(shape.CmdCubic, pts(3)...)
	case "close":
		d.Command(shape.CmdClose)
	case "arcTo":
		d.Command(shape.CmdArc, attrOr(k, "wR"), attrOr(k, "hR"), attrOr(k, "stAng"), attrOr(k, "swAng"))
	}
}
