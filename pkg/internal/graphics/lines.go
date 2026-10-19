package graphics

import (
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/models"
)

type pathPoint struct{ x, y float64 }

// ExtractLines processes content stream [content.Operation] values and returns
// all stroked line segments in absolute page coordinates. It tracks the CTM so
// that lines inside transformed Form XObjects are correctly mapped.
func ExtractLines(ops []content.Operation) []models.Line {
	var (
		ctm      = newCTMTracker()
		segments []models.Line // accumulated path segments before paint
		curPt    pathPoint
		subStart pathPoint
		hasCurPt bool
		result   []models.Line
	)

	addSeg := func(x1, y1, x2, y2 float64) {
		px1, py1 := ctm.transform(x1, y1)
		px2, py2 := ctm.transform(x2, y2)
		segments = append(segments, models.Line{X1: px1, Y1: py1, X2: px2, Y2: py2})
	}

	closePath := func() {
		if hasCurPt {
			addSeg(curPt.x, curPt.y, subStart.x, subStart.y)
			curPt = subStart
		}
	}

	emitStroked := func() {
		result = append(result, segments...)
		segments = segments[:0]
		hasCurPt = false
	}

	discardPath := func() {
		segments = segments[:0]
		hasCurPt = false
	}

	for _, op := range ops {
		switch op.Operator {
		case "q", "Q", "cm":
			ctm.apply(op)
		case "m": // moveto
			if len(op.Operands) == 2 {
				x, y := operandNumber(op.Operands[0]), operandNumber(op.Operands[1])
				curPt = pathPoint{x, y}
				subStart = curPt
				hasCurPt = true
			}
		case "l": // lineto
			if len(op.Operands) == 2 && hasCurPt {
				x, y := operandNumber(op.Operands[0]), operandNumber(op.Operands[1])
				addSeg(curPt.x, curPt.y, x, y)
				curPt = pathPoint{x, y}
			}
		case "re": // rectangle → 4 line segments
			if len(op.Operands) == 4 {
				x, y := operandNumber(op.Operands[0]), operandNumber(op.Operands[1])
				w, h := operandNumber(op.Operands[2]), operandNumber(op.Operands[3])
				addSeg(x, y, x+w, y)
				addSeg(x+w, y, x+w, y+h)
				addSeg(x+w, y+h, x, y+h)
				addSeg(x, y+h, x, y)
				curPt = pathPoint{x, y}
				subStart = curPt
				hasCurPt = true
			}
		case "h": // close subpath
			closePath()
		case "S": // stroke
			emitStroked()
		case "s": // close and stroke
			closePath()
			emitStroked()
		case "B", "B*": // fill and stroke
			emitStroked()
		case "b", "b*": // close, fill, and stroke
			closePath()
			emitStroked()
		case "f", "F", "f*": // fill only, no visible stroked lines
			discardPath()
		case "n": // end path without painting
			discardPath()
		}
	}

	return result
}

// ExtractFilledBoxes returns the bounding box, in absolute page coordinates as
// [llx lly urx ury], of every path the content fills, in content order. A path
// a fill-and-stroke operator paints counts as filled. Curve control points
// widen the box, which keeps it a bound on what the fill paints: a Bézier curve
// lies inside the convex hull of its control points (ISO 32000-1, 8.5.2.2).
// Like [ExtractLines], it does not descend into form XObjects, and it does not
// read the fill colour.
func ExtractFilledBoxes(ops []content.Operation) [][4]float64 {
	var (
		ctm    = newCTMTracker()
		box    [4]float64
		inPath bool
		result [][4]float64
	)
	add := func(x, y float64) {
		px, py := ctm.transform(x, y)
		if !inPath {
			box, inPath = [4]float64{px, py, px, py}, true
			return
		}
		box = [4]float64{min(box[0], px), min(box[1], py), max(box[2], px), max(box[3], py)}
	}
	for _, op := range ops {
		switch op.Operator {
		case "q", "Q", "cm":
			ctm.apply(op)
		case "m", "l", "c", "v", "y":
			for i := 0; i+1 < len(op.Operands); i += 2 {
				add(operandNumber(op.Operands[i]), operandNumber(op.Operands[i+1]))
			}
		case "re":
			if len(op.Operands) == 4 {
				x, y := operandNumber(op.Operands[0]), operandNumber(op.Operands[1])
				w, h := operandNumber(op.Operands[2]), operandNumber(op.Operands[3])
				add(x, y)
				add(x+w, y)
				add(x+w, y+h)
				add(x, y+h)
			}
		case "f", "F", "f*", "B", "B*", "b", "b*":
			if inPath {
				result = append(result, box)
			}
			inPath = false
		case "S", "s", "n":
			inPath = false
		}
	}
	return result
}

// ctmTracker follows the current transformation matrix through q, Q and cm
// (ISO 32000-1, 8.4.4).
type ctmTracker struct {
	m     [6]float64
	stack [][6]float64
}

func newCTMTracker() *ctmTracker {
	return &ctmTracker{m: [6]float64{1, 0, 0, 1, 0, 0}}
}

func (t *ctmTracker) transform(x, y float64) (float64, float64) {
	return x*t.m[0] + y*t.m[2] + t.m[4], x*t.m[1] + y*t.m[3] + t.m[5]
}

// apply updates the matrix for a q, Q or cm operator and ignores any other.
func (t *ctmTracker) apply(op content.Operation) {
	switch op.Operator {
	case "q":
		t.stack = append(t.stack, t.m)
	case "Q":
		if len(t.stack) > 0 {
			t.m = t.stack[len(t.stack)-1]
			t.stack = t.stack[:len(t.stack)-1]
		}
	case "cm":
		if len(op.Operands) == 6 {
			a, b := operandNumber(op.Operands[0]), operandNumber(op.Operands[1])
			c, d := operandNumber(op.Operands[2]), operandNumber(op.Operands[3])
			e, f := operandNumber(op.Operands[4]), operandNumber(op.Operands[5])
			old := t.m
			t.m = [6]float64{
				a*old[0] + b*old[2],
				a*old[1] + b*old[3],
				c*old[0] + d*old[2],
				c*old[1] + d*old[3],
				e*old[0] + f*old[2] + old[4],
				e*old[1] + f*old[3] + old[5],
			}
		}
	}
}

// operandNumber reads a numeric operand, or 0 for any other object.
func operandNumber(obj ast.PdfObject) float64 {
	switch v := obj.(type) {
	case ast.PdfReal:
		return float64(v)
	case ast.PdfInt:
		return float64(v)
	}
	return 0
}
