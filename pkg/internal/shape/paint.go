package shape

import (
	"math"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/compose"
)

// PaintKind is how an area is painted.
type PaintKind uint8

// The paint kinds (ECMA-376 Part 1, 20.1.8 the fill properties; ODF 1.3
// Part 3, 20.182 draw:fill).
const (
	PaintNone PaintKind = iota
	PaintSolid
	PaintGradient
	PaintPicture
	PaintPattern
)

// Paint is a fill as plain values, for [Fill].
type Paint struct {
	Kind PaintKind
	// Color is a PaintSolid's colour and a PaintPattern's foreground.
	Color compose.Color
	// Background is a PaintPattern's background.
	Background compose.Color
	// Stops are a PaintGradient's stops, by position in [0, 1]; Angle
	// is a linear gradient's direction in degrees, clockwise from the
	// positive x axis (20.1.8.41 lin ang); Radial makes the gradient run
	// from the box's centre, stop 0, to its corners, stop 1, instead.
	Stops  []compose.ColorStop
	Angle  float64
	Radial bool
	// Picture is a PaintPicture's image, stretched over the box unless
	// TileWidth and TileHeight, in points, are both positive, which
	// repeat it from the box's corner at that size.
	Picture               *compose.ImageSource
	TileWidth, TileHeight float64
	// Pattern is a PaintPattern's preset (20.1.10.51 ST_PresetPatternVal:
	// "pct50", "dkUpDiag", ...).
	Pattern string
}

// Fill returns the fragments that paint the interior of the outline ops,
// fitted to box, with p, filled by [FillRule]:
//
//   - a solid colour as a flat fill;
//   - a gradient as a [compose.Shading]: linear across the box at the
//     angle, the axis long enough that the box's corners meet stops 0 and
//     1, or radial from the centre as an ellipse through the corners (a
//     DrawingML path gradient of any shape is drawn so); a box with no
//     area paints its first stop;
//   - a picture stretched over the box, or tiled, clipped to the outline;
//   - a pattern as its background, then its foreground: the line
//     patterns (horizontal, vertical, diagonal, grids, crosses) as lines
//     at the preset's pitch and weight, the others as the foreground at
//     the preset's ink coverage.
//
// An outline that encloses no area, a line, paints nothing: a renderer
// would draw its zero-width fill as a hairline. The gradient's scaled and
// rotWithShape flags (20.1.8.33 gradFill) and a
// picture's source rectangle and tile alignment are not modelled.
func Fill(ops []compose.PathOp, box compose.Rect, p Paint) []compose.Fragment {
	if !hasArea(ops) {
		return nil
	}
	flat := func(c compose.Color) *compose.Path {
		return &compose.Path{Ops: ops, Fill: &compose.Fill{Color: c, Rule: FillRule}}
	}
	switch p.Kind {
	case PaintSolid:
		return []compose.Fragment{flat(p.Color)}
	case PaintGradient:
		stops := normalStops(p.Stops)
		if len(stops) == 0 {
			return nil
		}
		sh := gradientShading(box, stops, p.Angle, p.Radial)
		if sh == nil {
			return []compose.Fragment{flat(stops[0].Color)}
		}
		return []compose.Fragment{&compose.Path{Ops: ops, Fill: &compose.Fill{Rule: FillRule, Shading: sh}}}
	case PaintPicture:
		if p.Picture == nil || box.Width <= 0 || box.Height <= 0 {
			return nil
		}
		return []compose.Fragment{&compose.Group{
			Clip:    &compose.Path{Ops: ops, Fill: &compose.Fill{Rule: FillRule}},
			Content: pictureTiles(box, p),
		}}
	case PaintPattern:
		return patternFill(ops, box, p, flat)
	case PaintNone:
	}
	return nil
}

// hasArea reports whether the outline ops encloses any area: the
// shoelace sum of each subpath over its points, Bézier control points
// included, is not zero.
func hasArea(ops []compose.PathOp) bool {
	var area, sub float64
	var first, last compose.Point
	open := false
	add := func(p compose.Point) {
		sub += last.X*p.Y - p.X*last.Y
		last = p
	}
	closeSub := func() {
		if open {
			add(first)
			area += math.Abs(sub)
		}
		sub, open = 0, false
	}
	for _, op := range ops {
		switch op.Kind {
		case compose.MoveTo:
			closeSub()
			first, last, open = op.P[0], op.P[0], true
		case compose.LineTo:
			add(op.P[0])
		case compose.CurveTo:
			add(op.P[0])
			add(op.P[1])
			add(op.P[2])
		case compose.ClosePath:
			closeSub()
		}
	}
	closeSub()
	return area > 1e-9
}

// normalStops returns the stops sorted by position, clamped to [0, 1],
// with a stop at each end, as a shading needs; nil for none.
func normalStops(in []compose.ColorStop) []compose.ColorStop {
	if len(in) == 0 {
		return nil
	}
	s := make([]compose.ColorStop, 0, len(in)+2)
	for _, st := range in {
		if !finite(st.Offset) {
			continue
		}
		st.Offset = math.Max(0, math.Min(1, st.Offset))
		s = append(s, st)
	}
	if len(s) == 0 {
		return nil
	}
	slices.SortStableFunc(s, func(a, b compose.ColorStop) int {
		switch {
		case a.Offset < b.Offset:
			return -1
		case a.Offset > b.Offset:
			return 1
		}
		return 0
	})
	if s[0].Offset > 0 {
		s = append([]compose.ColorStop{{Offset: 0, Color: s[0].Color}}, s...)
	}
	if last := s[len(s)-1]; last.Offset < 1 || len(s) == 1 {
		s = append(s, compose.ColorStop{Offset: 1, Color: last.Color})
	}
	return s
}

// gradientShading returns the shading of a gradient over box, nil when
// the box has no area.
func gradientShading(box compose.Rect, stops []compose.ColorStop, angle float64, radial bool) *compose.Shading {
	if box.Width <= 0 || box.Height <= 0 || !finite(angle) {
		return nil
	}
	cx, cy := box.X+box.Width/2, box.Y+box.Height/2
	if radial {
		// The unit circle scaled to the ellipse through the corners.
		return &compose.Shading{
			Kind: compose.Radial, From: compose.Point{}, To: compose.Point{}, ToRadius: 1, Stops: stops,
			Transform: compose.Matrix{A: box.Width / math.Sqrt2, D: box.Height / math.Sqrt2, E: cx, F: cy},
		}
	}
	a := angle * math.Pi / 180
	dx, dy := math.Cos(a), math.Sin(a)
	half := (box.Width*math.Abs(dx) + box.Height*math.Abs(dy)) / 2
	return &compose.Shading{
		Kind:  compose.Axial,
		From:  compose.Point{X: cx - dx*half, Y: cy - dy*half},
		To:    compose.Point{X: cx + dx*half, Y: cy + dy*half},
		Stops: stops,
	}
}

// maxPictureTiles caps the copies of a tiled picture. Anti-DoS guard, not
// spec-mandated: past it the picture is stretched, as a tile too small to
// see is no texture anyway.
const maxPictureTiles = 4096

// pictureTiles places a picture fill's image over box.
func pictureTiles(box compose.Rect, p Paint) []compose.Fragment {
	tw, th := p.TileWidth, p.TileHeight
	if tw > 0 && th > 0 && finite(tw) && finite(th) && math.Ceil(box.Width/tw)*math.Ceil(box.Height/th) <= maxPictureTiles {
		var out []compose.Fragment
		for y := box.Y; y < box.Y+box.Height; y += th {
			for x := box.X; x < box.X+box.Width; x += tw {
				out = append(out, &compose.Image{Rect: compose.Rect{X: x, Y: y, Width: tw, Height: th}, Source: p.Picture})
			}
		}
		return out
	}
	return []compose.Fragment{&compose.Image{Rect: box, Source: p.Picture}}
}

// patternPixel is the size of one pixel of a preset pattern's 8 by 8
// tile: Office draws the tile at 96 pixels to the inch.
const patternPixel = 0.75

// linePattern is a preset pattern drawn as lines: each direction's lines
// every pitch pixels, weight pixels wide, dashed when dash is set.
type linePattern struct {
	horz, vert, down, up bool
	pitch, weight        float64
	dash                 bool
}

// linePatterns are the line patterns of 20.1.10.51, by name, as their 8 by
// 8 tiles draw them.
var linePatterns = map[string]linePattern{ //nolint:gochecknoglobals // a read-only spec table, ECMA-376 Part 1, 20.1.10.51.
	"horz": {horz: true, pitch: 8, weight: 1}, "vert": {vert: true, pitch: 8, weight: 1},
	"ltHorz": {horz: true, pitch: 4, weight: 1}, "ltVert": {vert: true, pitch: 4, weight: 1},
	"dkHorz": {horz: true, pitch: 4, weight: 2}, "dkVert": {vert: true, pitch: 4, weight: 2},
	"narHorz": {horz: true, pitch: 2, weight: 1}, "narVert": {vert: true, pitch: 2, weight: 1},
	"dashHorz": {horz: true, pitch: 4, weight: 1, dash: true}, "dashVert": {vert: true, pitch: 4, weight: 1, dash: true},
	"dnDiag": {down: true, pitch: 4, weight: 1}, "upDiag": {up: true, pitch: 4, weight: 1},
	"ltDnDiag": {down: true, pitch: 4, weight: 1}, "ltUpDiag": {up: true, pitch: 4, weight: 1},
	"dkDnDiag": {down: true, pitch: 4, weight: 2}, "dkUpDiag": {up: true, pitch: 4, weight: 2},
	"wdDnDiag": {down: true, pitch: 8, weight: 3}, "wdUpDiag": {up: true, pitch: 8, weight: 3},
	"dashDnDiag": {down: true, pitch: 4, weight: 1, dash: true}, "dashUpDiag": {up: true, pitch: 4, weight: 1, dash: true},
	"cross": {horz: true, vert: true, pitch: 8, weight: 1}, "lgGrid": {horz: true, vert: true, pitch: 8, weight: 1},
	"smGrid": {horz: true, vert: true, pitch: 4, weight: 1}, "diagCross": {down: true, up: true, pitch: 8, weight: 1},
}

// patternCoverage is the share of a preset pattern's tile its foreground
// inks, for the patterns drawn as a blend (20.1.10.51).
var patternCoverage = map[string]float64{ //nolint:gochecknoglobals // a read-only spec table, ECMA-376 Part 1, 20.1.10.51.
	"pct5": 0.05, "pct10": 0.10, "pct20": 0.20, "pct25": 0.25, "pct30": 0.30, "pct40": 0.40,
	"pct50": 0.50, "pct60": 0.60, "pct70": 0.70, "pct75": 0.75, "pct80": 0.80, "pct90": 0.90,
	"smCheck": 0.5, "lgCheck": 0.5, "dotGrid": 0.125, "smConfetti": 0.25, "lgConfetti": 0.375,
	"horzBrick": 0.25, "diagBrick": 0.25, "solidDmnd": 0.5, "openDmnd": 0.25, "dotDmnd": 0.125,
	"plaid": 0.5, "sphere": 0.5, "weave": 0.375, "divot": 0.125, "shingle": 0.25, "wave": 0.25,
	"trellis": 0.5, "zigZag": 0.25,
}

// maxPatternLines caps the lines of a line pattern. Anti-DoS guard, not
// spec-mandated: past it the pattern is drawn as a blend.
const maxPatternLines = 4096

// patternFill paints a preset pattern.
func patternFill(ops []compose.PathOp, box compose.Rect, p Paint, flat func(compose.Color) *compose.Path) []compose.Fragment {
	out := []compose.Fragment{flat(p.Background)}
	if lp, ok := linePatterns[p.Pattern]; ok {
		if lines := patternLines(box, lp); lines != nil {
			stroke := &compose.Stroke{Color: p.Color, Width: lp.weight * patternPixel}
			if lp.dash {
				stroke.Dash = []float64{4 * patternPixel, 4 * patternPixel}
			}
			return append(out, &compose.Group{
				Clip:    &compose.Path{Ops: ops, Fill: &compose.Fill{Rule: FillRule}},
				Content: []compose.Fragment{&compose.Path{Ops: lines, Stroke: stroke}},
			})
		}
	}
	cov, ok := patternCoverage[p.Pattern]
	if !ok {
		if lp, isLine := linePatterns[p.Pattern]; isLine {
			cov = math.Min(1, lp.weight/lp.pitch*(btoi(lp.horz)+btoi(lp.vert)+btoi(lp.down)+btoi(lp.up)))
		} else {
			cov = 0.5
		}
	}
	return append(out, &compose.Group{Opacity: cov, Content: []compose.Fragment{flat(p.Color)}})
}

// btoi is 1 for true.
func btoi(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// patternLines returns the lines of a line pattern over box, nil past
// maxPatternLines.
func patternLines(box compose.Rect, lp linePattern) []compose.PathOp {
	step := lp.pitch * patternPixel
	if box.Width <= 0 || box.Height <= 0 || (box.Width+box.Height)/step*4 > maxPatternLines {
		return nil
	}
	pt := func(x, y float64) [3]compose.Point { return [3]compose.Point{{X: x, Y: y}} }
	var ops []compose.PathOp
	line := func(x0, y0, x1, y1 float64) {
		ops = append(ops, compose.PathOp{Kind: compose.MoveTo, P: pt(x0, y0)}, compose.PathOp{Kind: compose.LineTo, P: pt(x1, y1)})
	}
	x0, y0, x1, y1 := box.X, box.Y, box.X+box.Width, box.Y+box.Height
	if lp.horz {
		for y := y0 + step/2; y < y1; y += step {
			line(x0, y, x1, y)
		}
	}
	if lp.vert {
		for x := x0 + step/2; x < x1; x += step {
			line(x, y0, x, y1)
		}
	}
	// Diagonals at 45 degrees, a pitch apart along each axis.
	if lp.down {
		for d := -box.Height; d < box.Width; d += step {
			line(x0+d, y0, x0+d+box.Height, y1)
		}
	}
	if lp.up {
		for d := 0.0; d < box.Width+box.Height; d += step {
			line(x0+d, y0, x0+d-box.Height, y1)
		}
	}
	return ops
}

// Shade returns the colour c as a path's fill mode m paints it: darker or
// lighter by the shares LibreOffice's custom shape engine uses (darken
// 40%, darkenLess 20% toward black; lighten 40%, lightenLess 20% toward
// white); c itself for the other modes.
func Shade(c compose.Color, m FillMode) compose.Color {
	toward := func(t, f float64) compose.Color {
		return compose.Color{R: c.R + (t-c.R)*f, G: c.G + (t-c.G)*f, B: c.B + (t-c.B)*f, A: c.A}
	}
	switch m {
	case FillDarken:
		return toward(0, 0.4)
	case FillDarkenLess:
		return toward(0, 0.2)
	case FillLighten:
		return toward(1, 0.4)
	case FillLightenLess:
		return toward(1, 0.2)
	case FillNorm, FillNone:
	}
	return c
}
