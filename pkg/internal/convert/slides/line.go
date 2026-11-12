package slides

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/shape"
)

// maxDash caps the entries of a dash array drawn. Anti-DoS guard, not
// spec-mandated: the preset dashes have at most six entries.
const maxDash = 64

// maxLineWidth caps a line's width, in points. Anti-DoS guard, not
// spec-mandated: DrawingML stops at 1584 points (20.1.10.35
// ST_LineWidth).
const maxLineWidth = 1584

// stroke returns what strokes the outline ops with the line l, its
// arrowheads at the ends of an open outline (ECMA-376 Part 1, 20.1.2.2.24
// ln, 20.1.8.37 headEnd, 20.1.8.57 tailEnd), through the shape engine's
// stroke, which sizes and docks the arrowheads as Impress draws them.
func (b *builder) stroke(l *Line, ops []compose.PathOp) []compose.Fragment {
	if l == nil || l.Color.A <= 0 || len(ops) < 2 {
		return nil
	}
	width := l.Width
	if l.Hairline {
		// A hairline's arrowheads take the smallest size the engine draws;
		// the line itself is a PDF zero-width line, the thinnest the
		// device draws.
		width = hairlineArrowWidth
	} else if !positiveFinite(width) {
		return nil
	}
	sl := shape.Line{Width: min(width, maxLineWidth), Color: colorOf(l.Color), Head: arrowheadOf(l.Head), Tail: arrowheadOf(l.Tail)}
	switch l.Cap {
	case CapRound:
		sl.Cap = compose.RoundCap
	case CapSquare:
		sl.Cap = compose.SquareCap
	case CapFlat:
		sl.Cap = compose.ButtCap
	}
	switch l.Join {
	case JoinRound:
		sl.Join = compose.RoundJoin
	case JoinBevel:
		sl.Join = compose.BevelJoin
	case JoinMiter:
		sl.Join = compose.MiterJoin
	}
	if len(l.Dash) > 0 && len(l.Dash) <= maxDash {
		ok, sum := true, 0.0
		for _, d := range l.Dash {
			if math.IsNaN(d) || d < 0 || math.IsInf(d, 0) {
				ok = false
			}
			sum += d
		}
		if ok && sum > 0 {
			sl.Dash = append([]float64(nil), l.Dash...)
		}
	}
	out := shape.Stroke(ops, sl)
	if l.Hairline {
		w := hairlineWidth(l)
		for _, f := range out {
			if p, ok := f.(*compose.Path); ok && p.Stroke != nil {
				p.Stroke.Width = w
			}
		}
	}
	return out
}

// hairlineWidth returns the width a hairline strokes at: a PDF zero-width
// line, the thinnest the device draws, as Impress strokes a hairline
// without arrowheads; 0.1 point, the line width Impress sets at the top of
// every page and leaves in place, for one with an arrowhead at either
// end.
//
// # Corpus evidence
//
//   - libreoffice/odp: sd/tdf66228.odp's outlines and connector and
//     sd/tdf127267DashOnHairline.odp's dashed line, hairlines without
//     arrowheads, stroke at "0 w"; sd/tdf99213-target-missing.odp's three
//     hairlines with arrowheads stroke at the page's "0.1 w".
func hairlineWidth(l *Line) float64 {
	if l.Head.Kind != ArrowNone || l.Tail.Kind != ArrowNone {
		return hairlineArrowedWidth
	}
	return 0
}

// hairlineArrowedWidth is the width of a hairline with an arrowhead, in
// points.
const hairlineArrowedWidth = 0.1

// hairlineArrowWidth is the line width a hairline's arrowheads are sized
// for, in points: any width up to it gives the engine's smallest
// arrowhead, 0.7 millimetre.
const hairlineArrowWidth = 0.1

// arrowheadOf returns the engine's arrowhead for a: its kind, and its
// width and length as the small, medium or large size the model's
// factors name (0.5, 1 and 2).
func arrowheadOf(a Arrow) shape.Arrowhead {
	var h shape.Arrowhead
	switch a.Kind {
	case ArrowTriangle:
		h.Kind = shape.ArrowTriangle
	case ArrowStealth:
		h.Kind = shape.ArrowStealth
	case ArrowDiamond:
		h.Kind = shape.ArrowDiamond
	case ArrowOval:
		h.Kind = shape.ArrowOval
	case ArrowOpen:
		h.Kind = shape.ArrowOpen
	case ArrowNone:
		return h
	}
	h.Width, h.Length = arrowSizeOf(a.Width), arrowSizeOf(a.Length)
	return h
}

// arrowSizeOf returns the engine's size for a model factor: below 1 small,
// above 1 large, else medium.
func arrowSizeOf(f float64) shape.ArrowSize {
	switch {
	case f > 0 && f < 1:
		return shape.ArrowSmall
	case f > 1:
		return shape.ArrowLarge
	}
	return shape.ArrowMedium
}
