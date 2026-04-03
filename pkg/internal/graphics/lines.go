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
		ctmStack [][6]float64
		ctm      = [6]float64{1, 0, 0, 1, 0, 0}
		segments []models.Line // accumulated path segments before paint
		curPt    pathPoint
		subStart pathPoint
		hasCurPt bool
		result   []models.Line
	)

	transform := func(x, y float64) (float64, float64) {
		return x*ctm[0] + y*ctm[2] + ctm[4], x*ctm[1] + y*ctm[3] + ctm[5]
	}

	addSeg := func(x1, y1, x2, y2 float64) {
		px1, py1 := transform(x1, y1)
		px2, py2 := transform(x2, y2)
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

	gf := func(obj ast.PdfObject) float64 {
		switch v := obj.(type) {
		case ast.PdfReal:
			return float64(v)
		case ast.PdfInt:
			return float64(v)
		}
		return 0
	}

	for _, op := range ops {
		switch op.Operator {
		case "q":
			ctmStack = append(ctmStack, ctm)
		case "Q":
			if len(ctmStack) > 0 {
				ctm = ctmStack[len(ctmStack)-1]
				ctmStack = ctmStack[:len(ctmStack)-1]
			}
		case "cm":
			if len(op.Operands) == 6 {
				a, b := gf(op.Operands[0]), gf(op.Operands[1])
				c, d := gf(op.Operands[2]), gf(op.Operands[3])
				e, f := gf(op.Operands[4]), gf(op.Operands[5])
				old := ctm
				ctm = [6]float64{
					a*old[0] + b*old[2],
					a*old[1] + b*old[3],
					c*old[0] + d*old[2],
					c*old[1] + d*old[3],
					e*old[0] + f*old[2] + old[4],
					e*old[1] + f*old[3] + old[5],
				}
			}
		case "m": // moveto
			if len(op.Operands) == 2 {
				x, y := gf(op.Operands[0]), gf(op.Operands[1])
				curPt = pathPoint{x, y}
				subStart = curPt
				hasCurPt = true
			}
		case "l": // lineto
			if len(op.Operands) == 2 && hasCurPt {
				x, y := gf(op.Operands[0]), gf(op.Operands[1])
				addSeg(curPt.x, curPt.y, x, y)
				curPt = pathPoint{x, y}
			}
		case "re": // rectangle → 4 line segments
			if len(op.Operands) == 4 {
				x, y := gf(op.Operands[0]), gf(op.Operands[1])
				w, h := gf(op.Operands[2]), gf(op.Operands[3])
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
		case "f", "F", "f*": // fill only — no visible stroked lines
			discardPath()
		case "n": // end path without painting
			discardPath()
		}
	}

	return result
}
