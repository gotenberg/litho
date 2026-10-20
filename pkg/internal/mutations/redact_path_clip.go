package mutations

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/heuristics"
)

// A painted path reaching both inside and outside a region is rewritten rather
// than refused. Two things happen to it, and each closes a gap the other
// leaves:
//
//   - The run is wrapped in q ... Q with, per region, an even-odd clip made of
//     a rectangle around everything the path paints and the region inside it
//     (ISO 32000-1, 8.5.4). The path paints only outside the regions, whatever
//     its line width, caps, joins and dashes do, which is what makes the clip
//     the one approach correct for strokes: a stroke's ink lies up to half the
//     line width (more at a miter) past the geometry, so clipping the geometry
//     alone would leave ink inside the region.
//   - The geometry inside each region is removed from the operands. A clip
//     hides what it covers but keeps it in the file, and path coordinates are
//     content in their own right: a vector signature, handwriting or a chart is
//     legible from the numbers alone. Each run of the path inside a region is
//     replaced by one straight chord between where it entered and where it
//     left. The region is convex and every point that still paints is outside
//     it, so the closed loop the old run and the chord form winds zero times
//     around any such point: the fill under either rule (8.5.3.3) is unchanged
//     outside the region.
//
// A stroke is cut at the region shrunk by the stroke's reach rather than at the
// region itself, so the removed geometry and the new chord paint only inside
// the region, where the clip discards them, and the ink outside is the ink the
// original drew. The reach counts the miter of every join the path makes, the
// chords' included. The geometry left between the shrunk region and the region
// is what draws the ink visible just outside it, so it shows no more than the
// page already shows.

// strokeReachSlack widens a stroke's reach by one page unit. Not spec-mandated:
// it absorbs what the line width does not state, the thinnest-line rule (ISO
// 32000-1, 8.4.3.2 paints width 0 one device pixel wide) and stroke adjustment
// (10.7.5), which moves a stroke by up to a pixel. One unit is one pixel at 72
// dpi. Underestimating it costs only ink at the region's edge, which the clip
// still keeps out of the region.
const strokeReachSlack = 1.0

// fillCutInset is how far inside a region, in page units, a fill-only path is
// cut. The chord is then strictly inside the region, so no new edge of the fill
// lies on the clip's edge, where a renderer anti-aliasing both would composite
// them differently from the original. Half a unit keeps the geometry left
// inside the region to a sliver no shape can be read from.
const fillCutInset = 0.5

// clipOuterMargin pads the rectangle each clip pairs with its region, in page
// units, beyond what the path can paint. Not spec-mandated: it only has to
// keep the clip's outer edge clear of every mark, which the path's reach
// already bounds.
const clipOuterMargin = 10.0

// maxBezierBisections bounds the bisection that locates where a curve crosses
// a region edge. Each step halves the parameter interval, so 64 steps reach
// the resolution of a float64. A bound on work, not spec-mandated.
const maxBezierBisections = 64

// errPathCutNeedsUndashedStroke reports a dashed stroke whose geometry would
// have to be cut. The chord replacing a removed run is shorter or longer than
// the run, which would shift the dash phase of everything after it.
var errPathCutNeedsUndashedStroke = errors.New("a dashed stroke cannot be cut without moving its dashes")

// strokeParams is the part of the graphics state that decides how far a
// stroke's ink reaches past its path (ISO 32000-1, 8.4.3, Table 52).
type strokeParams struct {
	lineWidth  float64
	lineJoin   int
	miterLimit float64
	dashed     bool
	// known is false once an operator set a parameter to something that could
	// not be read, so the reach of a later stroke cannot be bounded.
	known bool
}

// pathPaintState is what [pagePaintStates] reads at one paint operator.
type pathPaintState struct {
	stroke strokeParams
	// inText reports a path painted inside BT ... ET, where ISO 32000-1, 8.2,
	// Figure 9 allows no q, so the rewrite has nowhere to put its clip.
	inText bool
}

// pagePaintStates walks the page's own operators and returns the state in
// force at each path-painting operator, keyed by operator index.
//
// It reads only the page's stream. A form XObject runs between an implied q
// and Q (ISO 32000-1, 8.10.1), so nothing it sets reaches the page's later
// operators, and paths a form paints are refused before this is consulted.
func pagePaintStates(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	page *document.Page,
	ops []content.Operation,
) (map[int]pathPaintState, error) {
	// Table 52 defaults: LW 1.0, LJ 0 (miter), ML 10.0, D [] 0.
	stack := []strokeParams{{lineWidth: 1, miterLimit: 10, known: true}}
	inText := false
	states := make(map[int]pathPaintState)

	for i, op := range ops {
		if i%100 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, fmt.Errorf("reading the page's stroke state: %w", err)
			}
		}
		cur := &stack[len(stack)-1]
		switch op.Operator {
		case "q":
			stack = append(stack, *cur)
		case "Q":
			// Per ISO 32000-1, 8.4.2 a Q with no matching q restores nothing.
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
		case "BT":
			inText = true
		case "ET":
			inText = false
		case "w":
			v, ok := singleNumber(op.Operands)
			cur.lineWidth = v
			cur.known = cur.known && ok
		case "j":
			v, ok := singleNumber(op.Operands)
			cur.lineJoin = int(v)
			cur.known = cur.known && ok
		case "M":
			v, ok := singleNumber(op.Operands)
			cur.miterLimit = v
			cur.known = cur.known && ok
		case "d":
			if len(op.Operands) != 2 {
				cur.known = false
				break
			}
			dashed, ok := dashArrayDashes(op.Operands[0])
			cur.dashed = dashed
			cur.known = cur.known && ok
		case "gs":
			applyExtGState(ctx, alloc, doc, page, op.Operands, cur)
		case "S", "s", "f", "F", "f*", "B", "B*", "b", "b*":
			states[i] = pathPaintState{stroke: *cur, inText: inText}
		}
	}
	return states, nil
}

// applyExtGState folds the stroke parameters a gs operator's dictionary sets
// into p (ISO 32000-1, 8.4.5, Table 58). A dictionary that does not resolve
// leaves the parameters unknown: it may have set any of them.
func applyExtGState(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	page *document.Page,
	operands []ast.PdfObject,
	p *strokeParams,
) {
	if len(operands) != 1 {
		p.known = false
		return
	}
	name, ok := operands[0].(ast.PdfName)
	if !ok {
		p.known = false
		return
	}
	dict, ok := extGStateDict(ctx, alloc, doc, page, name)
	if !ok {
		p.known = false
		return
	}
	if v, present := dict["LW"]; present {
		f, ok := resolvedNumber(ctx, alloc, doc, v)
		p.lineWidth = f
		p.known = p.known && ok
	}
	if v, present := dict["LJ"]; present {
		f, ok := resolvedNumber(ctx, alloc, doc, v)
		p.lineJoin = int(f)
		p.known = p.known && ok
	}
	if v, present := dict["ML"]; present {
		f, ok := resolvedNumber(ctx, alloc, doc, v)
		p.miterLimit = f
		p.known = p.known && ok
	}
	if v, present := dict["D"]; present {
		obj, err := resolveAllocFirst(ctx, alloc, doc, v)
		arr, isArr := obj.(ast.PdfArray)
		if err != nil || !isArr || len(arr) != 2 {
			p.known = false
			return
		}
		dashed, ok := dashArrayDashes(arr[0])
		p.dashed = dashed
		p.known = p.known && ok
	}
}

// extGStateDict resolves the page's /ExtGState entry for name.
func extGStateDict(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	page *document.Page,
	name ast.PdfName,
) (ast.PdfDict, bool) {
	obj, err := resolveAllocFirst(ctx, alloc, doc, page.Resources["ExtGState"])
	if err != nil {
		doc.Logger.Debug("redaction stroke state: /ExtGState did not resolve", slog.Any("err", err))
		return nil, false
	}
	states, ok := obj.(ast.PdfDict)
	if !ok {
		return nil, false
	}
	obj, err = resolveAllocFirst(ctx, alloc, doc, states[name])
	if err != nil {
		doc.Logger.Debug("redaction stroke state: a graphics state did not resolve",
			slog.String("name", string(name)), slog.Any("err", err))
		return nil, false
	}
	dict, ok := obj.(ast.PdfDict)
	return dict, ok
}

// resolvedNumber resolves obj, from alloc first, and reads it as a number.
func resolvedNumber(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, obj ast.PdfObject) (float64, bool) {
	resolved, err := resolveAllocFirst(ctx, alloc, doc, obj)
	if err != nil {
		return 0, false
	}
	return document.Number(resolved)
}

// singleNumber reads the one numeric operand of a w, j or M operator.
func singleNumber(operands []ast.PdfObject) (float64, bool) {
	if len(operands) != 1 {
		return 0, false
	}
	return document.Number(operands[0])
}

// dashArrayDashes reports whether a dash array dashes the line. An empty array
// draws it solid (ISO 32000-1, 8.4.3.6); any element at all is read as a dash,
// since an array of zeros is an error renderers resolve differently.
func dashArrayDashes(obj ast.PdfObject) (dashed, ok bool) {
	arr, isArr := obj.(ast.PdfArray)
	if !isArr {
		return false, false
	}
	return len(arr) > 0, true
}

// strokeReach returns how far, in page units, a stroke's ink can land from the
// path it strokes, given the join factor of that path (see [joinFactor]).
//
// Half the line width on either side (ISO 32000-1, 8.4.3.2), times the
// farthest a cap or join carries past that. The line is stroked in user space,
// and the CTM stretches user-space distances by at most its largest singular
// value.
func strokeReach(p strokeParams, ctm [6]float64, factor float64) float64 {
	return math.Abs(p.lineWidth)/2*factor*maxStretch(ctm) + strokeReachSlack
}

// miterCeiling returns the largest join factor p allows whatever the path: a
// projecting square cap's corner is √2 half-widths from its end point (ISO
// 32000-1, 8.4.3.3), and a miter join's tip at most the miter limit
// half-widths from its vertex, past which it is bevelled (8.4.3.5). A join
// style other than round or bevel is read as miter.
func miterCeiling(p strokeParams) float64 {
	if p.lineJoin == 1 || p.lineJoin == 2 || p.miterLimit <= math.Sqrt2 {
		return math.Sqrt2
	}
	return p.miterLimit
}

// miterLimitTolerance widens the miter limit a join is compared against, so a
// join a renderer's rounding still mitres is never taken for bevelled. Not
// spec-mandated.
const miterLimitTolerance = 1.01

// joinFactor returns the farthest, in half line widths, any cap or join of the
// path carries ink past its vertex: √2 for caps, and for each miter join the
// ratio of its miter length to the line width, 1/sin(φ/2) for an angle φ
// between the segments (ISO 32000-1, 8.4.3.5), unless that exceeds the miter
// limit and the join is bevelled. A join with a direction it cannot read, a
// zero-length segment's, takes [miterCeiling].
func joinFactor(subs []subpath, paintCloses bool, p strokeParams) float64 {
	ceiling := miterCeiling(p)
	if ceiling == math.Sqrt2 {
		return ceiling
	}
	factor := math.Sqrt2
	for si, sp := range subs {
		segs := sp.segs
		closed := sp.closed || (paintCloses && si == len(subs)-1)
		if n := len(segs); closed && n > 0 && segs[n-1].p[3] != sp.start {
			segs = append(slices.Clip(segs), pathSeg{p: [4]bezPt{segs[n-1].p[3], {}, {}, sp.start}})
		}
		n := len(segs)
		pairs := n - 1
		if closed && n > 1 {
			pairs = n
		}
		for k := 0; k < pairs; k++ {
			in, okIn := segDirection(segs[k], false)
			out, okOut := segDirection(segs[(k+1)%n], true)
			if !okIn || !okOut {
				return ceiling
			}
			sinHalf := math.Sqrt(math.Max((1+in.x*out.x+in.y*out.y)/2, 0))
			if sinHalf == 0 {
				continue // A reversal exceeds every miter limit and is bevelled.
			}
			ratio := 1 / sinHalf
			if ratio > p.miterLimit*miterLimitTolerance {
				continue
			}
			factor = math.Max(factor, ratio)
		}
	}
	return factor
}

// segDirection returns the unit tangent of seg at its start or at its end.
func segDirection(seg pathSeg, atStart bool) (bezPt, bool) {
	var a, b bezPt
	switch {
	case !seg.cubic:
		a, b = seg.p[0], seg.p[3]
	case atStart:
		a = seg.p[0]
		b = seg.p[1]
		if b == a {
			b = seg.p[2]
		}
		if b == a {
			b = seg.p[3]
		}
	default:
		b = seg.p[3]
		a = seg.p[2]
		if a == b {
			a = seg.p[1]
		}
		if a == b {
			a = seg.p[0]
		}
	}
	dx, dy := b.x-a.x, b.y-a.y
	l := math.Hypot(dx, dy)
	if l == 0 || math.IsNaN(l) || math.IsInf(l, 0) {
		return bezPt{}, false
	}
	return bezPt{dx / l, dy / l}, true
}

// maxStretch returns the largest singular value of the CTM's linear part, the
// most it lengthens any user-space distance.
func maxStretch(ctm [6]float64) float64 {
	a, b, c, d := ctm[0], ctm[1], ctm[2], ctm[3]
	sum := a*a + b*b + c*c + d*d
	det := a*d - b*c
	disc := math.Max(sum*sum-4*det*det, 0)
	return math.Sqrt((sum + math.Sqrt(disc)) / 2)
}

// bezPt is a point of a path, in whatever space the caller is working in.
type bezPt struct{ x, y float64 }

// pathSeg is one segment of a subpath: a line from p[0] to p[3], or a cubic
// Bézier curve with control points p[1] and p[2].
type pathSeg struct {
	cubic bool
	p     [4]bezPt
}

// subpath is one subpath of a path run as its operators built it.
type subpath struct {
	start  bezPt
	segs   []pathSeg
	closed bool
	// ops lists the operators that built it, so an untouched subpath can be
	// copied byte for byte.
	ops []int
	// explicitStart reports a subpath begun by m or re. One begun by a segment
	// operator after h starts at the closed subpath's start (ISO 32000-1,
	// 8.5.2.1), which a copy placed after rewritten geometry has to state.
	explicitStart bool
}

// parsePathRun reads the construction operators ops[first:paint] into
// subpaths, in user space, and lists the state operators among them (see
// [hoistableInPath]). It reports any other operator, and any operand that is
// not the numbers the operator takes: the rewrite has to account for every
// byte it replaces.
func parsePathRun(ops []content.Operation, first, paint int) ([]subpath, []int, error) {
	var (
		subs    []subpath
		hoisted []int
		cur     = -1
		pen     bezPt
		hasPen  bool
	)
	begin := func(at bezPt, op int, explicit bool) {
		subs = append(subs, subpath{start: at, ops: []int{op}, explicitStart: explicit})
		cur = len(subs) - 1
	}
	segment := func(i int, seg pathSeg) error {
		if cur < 0 {
			if !hasPen {
				return fmt.Errorf("operation %d (%s) has no current point", i, ops[i].Operator)
			}
			begin(pen, i, false)
		}
		subs[cur].segs = append(subs[cur].segs, seg)
		if last := subs[cur].ops; last[len(last)-1] != i {
			subs[cur].ops = append(subs[cur].ops, i)
		}
		pen = seg.p[3]
		return nil
	}

	for i := first; i < paint; i++ {
		op := ops[i]
		switch op.Operator {
		case "m":
			v, ok := numericOperands(op, 2)
			if !ok {
				return nil, nil, fmt.Errorf("operation %d (m) has unreadable operands", i)
			}
			pen, hasPen = bezPt{v[0], v[1]}, true
			begin(pen, i, true)
		case "l":
			v, ok := numericOperands(op, 2)
			if !ok || (cur < 0 && !hasPen) {
				return nil, nil, fmt.Errorf("operation %d (l) has unreadable operands or no current point", i)
			}
			if err := segment(i, pathSeg{p: [4]bezPt{pen, {}, {}, {v[0], v[1]}}}); err != nil {
				return nil, nil, err
			}
		case "c", "v", "y":
			want := 6
			if op.Operator != "c" {
				want = 4
			}
			v, ok := numericOperands(op, want)
			if !ok || (cur < 0 && !hasPen) {
				return nil, nil, fmt.Errorf("operation %d (%s) has unreadable operands or no current point", i, op.Operator)
			}
			var seg pathSeg
			seg.cubic = true
			// Per ISO 32000-1, Table 59: v takes its first control point from
			// the current point, y its second from the end point.
			switch op.Operator {
			case "c":
				seg.p = [4]bezPt{pen, {v[0], v[1]}, {v[2], v[3]}, {v[4], v[5]}}
			case "v":
				seg.p = [4]bezPt{pen, pen, {v[0], v[1]}, {v[2], v[3]}}
			case "y":
				seg.p = [4]bezPt{pen, {v[0], v[1]}, {v[2], v[3]}, {v[2], v[3]}}
			}
			if err := segment(i, seg); err != nil {
				return nil, nil, err
			}
		case "re":
			v, ok := numericOperands(op, 4)
			if !ok {
				return nil, nil, fmt.Errorf("operation %d (re) has unreadable operands", i)
			}
			// Per ISO 32000-1, 8.5.2.1, x y w h re is x y m, x+w y l, x+w y+h l,
			// x y+h l, h.
			x, y, w, h := v[0], v[1], v[2], v[3]
			corners := [4]bezPt{{x, y}, {x + w, y}, {x + w, y + h}, {x, y + h}}
			begin(corners[0], i, true)
			for k := 1; k < 4; k++ {
				subs[cur].segs = append(subs[cur].segs, pathSeg{p: [4]bezPt{corners[k-1], {}, {}, corners[k]}})
			}
			subs[cur].closed = true
			pen, hasPen = corners[0], true
			cur = -1
		case "h":
			if cur < 0 {
				// Closing a closed subpath does nothing (ISO 32000-1, Table 59),
				// but its bytes still belong to the run.
				if len(subs) == 0 {
					return nil, nil, fmt.Errorf("operation %d (h) closes no subpath", i)
				}
				subs[len(subs)-1].ops = append(subs[len(subs)-1].ops, i)
				continue
			}
			subs[cur].closed = true
			subs[cur].ops = append(subs[cur].ops, i)
			pen, hasPen = subs[cur].start, true
			cur = -1
		default:
			if !hoistableInPath[op.Operator] {
				return nil, nil, fmt.Errorf("operation %d (%s) is not path construction", i, op.Operator)
			}
			hoisted = append(hoisted, i)
		}
	}
	return subs, hoisted, nil
}

// hoistableInPath names the operators a producer writes inside a path object
// whose effect a rewrite keeps by moving them ahead of it. ISO 32000-1, 8.2,
// Figure 9 allows only construction operators there, but these general
// graphics state and colour operators are what producers interleave, and a
// reader applies them in order: the paint operator uses the state they set
// either way, and content after the path still sees it. q, Q and cm are not
// among them: moving a q changes what its Q restores, and a cm changes how the
// points after it map onto the page.
var hoistableInPath = map[string]bool{
	"w": true, "J": true, "j": true, "M": true, "d": true, "ri": true, "i": true, "gs": true,
	"CS": true, "cs": true, "SC": true, "SCN": true, "sc": true, "scn": true,
	"G": true, "g": true, "RG": true, "rg": true, "K": true, "k": true,
}

// numericOperands returns op's operands as numbers when it has exactly want of
// them and every one is a finite number.
func numericOperands(op content.Operation, want int) ([]float64, bool) {
	if len(op.Operands) != want {
		return nil, false
	}
	out := make([]float64, want)
	for i, o := range op.Operands {
		v, ok := document.Number(o)
		if !ok || math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, false
		}
		out[i] = v
	}
	return out, true
}

// cutRect is a region, in page space, at which a path is cut.
type cutRect struct{ x0, y0, x1, y1 float64 }

// piece is the part of one segment between two parameters, and which cut it
// lies strictly inside, or -1 for none.
type piece struct {
	seg    int
	t0, t1 float64
	zone   int
}

// apply maps a user-space point into page space through ctm.
func apply(ctm [6]float64, p bezPt) bezPt {
	return bezPt{p.x*ctm[0] + p.y*ctm[2] + ctm[4], p.x*ctm[1] + p.y*ctm[3] + ctm[5]}
}

// cutPieces splits every segment where it crosses a cut's edge and labels each
// piece with the cut it lies inside.
//
// Between consecutive crossings a segment cannot change sides of any edge, so
// the piece's midpoint decides the whole piece. Extra split points are
// harmless: adjacent pieces with the same label merge again.
func cutPieces(segs []pathSeg, ctm [6]float64, cuts []cutRect) []piece {
	var out []piece
	for si, seg := range segs {
		var page [4]bezPt
		for k := range page {
			page[k] = apply(ctm, seg.p[k])
		}
		ts := []float64{0, 1}
		for _, c := range cuts {
			for _, edge := range []struct {
				vertical bool
				at       float64
			}{{true, c.x0}, {true, c.x1}, {false, c.y0}, {false, c.y1}} {
				var q [4]float64
				for k := range q {
					if edge.vertical {
						q[k] = page[k].x - edge.at
					} else {
						q[k] = page[k].y - edge.at
					}
				}
				ts = append(ts, segmentRoots(q, seg.cubic)...)
			}
		}
		slices.Sort(ts)
		ts = slices.Compact(ts)
		for k := 0; k+1 < len(ts); k++ {
			t0, t1 := ts[k], ts[k+1]
			if t1 <= t0 {
				continue
			}
			mid := apply(ctm, evalSeg(seg, (t0+t1)/2))
			zone := -1
			for ci, c := range cuts {
				if mid.x > c.x0 && mid.x < c.x1 && mid.y > c.y0 && mid.y < c.y1 {
					zone = ci
					break
				}
			}
			if n := len(out); n > 0 && out[n-1].seg == si && out[n-1].zone == zone {
				out[n-1].t1 = t1
				continue
			}
			out = append(out, piece{seg: si, t0: t0, t1: t1, zone: zone})
		}
	}
	return out
}

// segmentRoots returns the parameters in (0, 1) where a segment's coordinate
// along one axis, less an edge's position, is zero or turns: q holds that
// difference at the four control points (a line uses q[0] and q[3]).
func segmentRoots(q [4]float64, cubic bool) []float64 {
	if !cubic {
		if (q[0] < 0) != (q[3] < 0) && q[0] != q[3] {
			if t := q[0] / (q[0] - q[3]); t > 0 && t < 1 {
				return []float64{t}
			}
		}
		return nil
	}
	// Power-basis coefficients of the cubic Bernstein polynomial.
	a := -q[0] + 3*q[1] - 3*q[2] + q[3]
	b := 3*q[0] - 6*q[1] + 3*q[2]
	c := 3*q[1] - 3*q[0]
	d := q[0]
	f := func(t float64) float64 { return ((a*t+b)*t+c)*t + d }

	// The turning points split [0, 1] into intervals where f is monotone, so
	// each holds at most one root, which bisection finds.
	bounds := []float64{0, 1}
	for _, t := range quadraticRoots(3*a, 2*b, c) {
		if t > 0 && t < 1 {
			bounds = append(bounds, t)
		}
	}
	slices.Sort(bounds)
	out := append([]float64(nil), bounds[1:len(bounds)-1]...)
	for k := 0; k+1 < len(bounds); k++ {
		lo, hi := bounds[k], bounds[k+1]
		flo, fhi := f(lo), f(hi)
		if flo == 0 || fhi == 0 || (flo < 0) == (fhi < 0) {
			continue
		}
		for range maxBezierBisections {
			mid := (lo + hi) / 2
			if mid <= lo || mid >= hi {
				break
			}
			if fm := f(mid); (fm < 0) == (flo < 0) {
				lo, flo = mid, fm
			} else {
				hi = mid
			}
		}
		out = append(out, (lo+hi)/2)
	}
	return out
}

// quadraticRoots returns the real roots of a t² + b t + c.
func quadraticRoots(a, b, c float64) []float64 {
	if a == 0 {
		if b == 0 {
			return nil
		}
		return []float64{-c / b}
	}
	disc := b*b - 4*a*c
	if disc < 0 {
		return nil
	}
	s := math.Sqrt(disc)
	return []float64{(-b - s) / (2 * a), (-b + s) / (2 * a)}
}

// lerp interpolates from a to b at t.
func lerp(a, b bezPt, t float64) bezPt {
	return bezPt{a.x + (b.x-a.x)*t, a.y + (b.y-a.y)*t}
}

// blossom evaluates a cubic's polar form at (u, v, w): de Casteljau with a
// different parameter at each level. blossom(t, t, t) is the point at t, and
// the control points of the curve over [t0, t1] are its blossoms at (t0, t0,
// t0), (t0, t0, t1), (t0, t1, t1) and (t1, t1, t1).
func blossom(p [4]bezPt, u, v, w float64) bezPt {
	a, b, c := lerp(p[0], p[1], u), lerp(p[1], p[2], u), lerp(p[2], p[3], u)
	d, e := lerp(a, b, v), lerp(b, c, v)
	return lerp(d, e, w)
}

// evalSeg returns the segment's point at t, exactly its end points at 0 and 1.
func evalSeg(seg pathSeg, t float64) bezPt {
	switch t {
	case 0:
		return seg.p[0]
	case 1:
		return seg.p[3]
	}
	if !seg.cubic {
		return lerp(seg.p[0], seg.p[3], t)
	}
	return blossom(seg.p, t, t, t)
}

// subSeg returns the part of seg over [t0, t1], ending exactly where evalSeg
// says, so a cut point shared with a chord is one value.
func subSeg(seg pathSeg, t0, t1 float64) pathSeg {
	if t0 == 0 && t1 == 1 {
		return seg
	}
	out := pathSeg{cubic: seg.cubic}
	out.p[0], out.p[3] = evalSeg(seg, t0), evalSeg(seg, t1)
	if seg.cubic {
		out.p[1] = blossom(seg.p, t0, t0, t1)
		out.p[2] = blossom(seg.p, t0, t1, t1)
	}
	return out
}

// pathEmitter writes path construction operators.
type pathEmitter struct{ b strings.Builder }

func (e *pathEmitter) nums(op string, pts ...bezPt) {
	for _, p := range pts {
		e.b.WriteString(writer.FormatReal(p.x))
		e.b.WriteByte(' ')
		e.b.WriteString(writer.FormatReal(p.y))
		e.b.WriteByte(' ')
	}
	e.b.WriteString(op)
	e.b.WriteByte(' ')
}

func (e *pathEmitter) seg(s pathSeg) {
	if s.cubic {
		e.nums("c", s.p[1], s.p[2], s.p[3])
		return
	}
	e.nums("l", s.p[3])
}

// clipPathRun rewrites a painted path the regions select but do not contain:
// it removes the geometry inside each region and paints what is left under a
// clip that excludes the regions. See the comment at the top of this file.
//
// zones are the regions the path's ink meets and inked the box that ink
// covers. stroke is the stroke state for a stroking paint operator and nil for
// a fill alone, and factor the original path's [joinFactor]. mayCut is false
// for a dashed stroke, whose geometry cannot change length. The result is the
// replacement for the run's bytes and whether it still paints: when nothing
// outside the regions remains, only the state operators the run held are
// kept. A run it cannot rewrite exactly is reported, for the caller to refuse.
//
// A stroke is cut at the regions shrunk by its reach, and the chords the cut
// adds make joins of their own. A sharper one than the original's reaches
// farther, so the cut is redone at the reach the rewritten path needs until
// it no longer grows, and at the miter limit's reach, which bounds every join,
// past [maxJoinRefits] attempts.
func clipPathRun(
	ctx context.Context,
	data []byte,
	ops []content.Operation,
	ranges []content.ByteRange,
	box graphics.PathBox,
	zones []heuristics.Zone,
	inked heuristics.Zone,
	stroke *strokeParams,
	factor float64,
	mayCut bool,
) (string, bool, error) {
	ctm := box.CTM
	det := ctm[0]*ctm[3] - ctm[1]*ctm[2]
	if det == 0 || math.IsNaN(det) || math.IsInf(det, 0) {
		return "", false, errors.New("the path's CTM is not invertible")
	}
	paint := ops[box.EndOp].Operator
	paintCloses := pathPaintCloses(paint)

	subs, hoisted, err := parsePathRun(ops, box.StartOp, box.EndOp)
	if err != nil {
		return "", false, err
	}

	for attempt := 0; ; attempt++ {
		reach := 0.0
		inset := fillCutInset
		if stroke != nil {
			if attempt == maxJoinRefits {
				factor = miterCeiling(*stroke)
			}
			reach = strokeReach(*stroke, ctm, factor)
			inset = reach
		}
		frame := cutFrame{ctm: ctm, det: det, cuts: insetCuts(zones, inset)}
		body, cut, err := cutSubpaths(data, ranges, subs, paint, frame)
		if err != nil {
			return "", false, err
		}
		if cut && !mayCut {
			return "", false, errPathCutNeedsUndashedStroke
		}
		if stroke != nil && cut && attempt < maxJoinRefits {
			rewritten, err := content.ParseStreamData(ctx, []byte(body))
			if err != nil {
				return "", false, fmt.Errorf("reading the rewritten path back: %w", err)
			}
			rsubs, _, err := parsePathRun(rewritten, 0, len(rewritten))
			if err != nil {
				return "", false, fmt.Errorf("reading the rewritten path back: %w", err)
			}
			if need := joinFactor(rsubs, paintCloses, *stroke); need > factor {
				factor = need
				continue
			}
		}
		// The state operators the path held come first and outside the q, so
		// the paint and everything after the path still see them.
		kept := opBytes(data, ranges, hoisted)
		if strings.TrimSpace(body) == "" {
			if kept == "" {
				return "", false, nil
			}
			return " " + kept, false, nil
		}
		var b strings.Builder
		b.WriteString(" ")
		b.WriteString(kept)
		b.WriteString("q ")
		b.WriteString(regionExclusionClips(frame, zones, inked, reach))
		b.WriteString(body)
		b.WriteString(paint)
		b.WriteString(" Q ")
		return b.String(), true, nil
	}
}

// pathPaintCloses reports whether a path-painting operator closes the last
// subpath before painting: s, b and b* (ISO 32000-1, Table 60).
func pathPaintCloses(operator string) bool {
	return operator == "s" || operator == "b" || operator == "b*"
}

// maxJoinRefits bounds how often [clipPathRun] redoes a cut for the joins its
// chords add before it falls back to the miter limit's reach, which needs no
// check. A bound on work, not spec-mandated: each attempt only widens the
// reach, and the fallback is always correct.
const maxJoinRefits = 4

// insetCuts shrinks each zone by inset on every side. A zone narrower than
// twice the inset has no point whose geometry's ink could not reach outside
// it, so nothing is cut there and the clip alone keeps it clear.
func insetCuts(zones []heuristics.Zone, inset float64) []cutRect {
	cuts := make([]cutRect, 0, len(zones))
	for _, z := range zones {
		x0, y0, x1, y1 := heuristics.ZoneBounds(z)
		c := cutRect{x0 + inset, y0 + inset, x1 - inset, y1 - inset}
		if c.x0 < c.x1 && c.y0 < c.y1 {
			cuts = append(cuts, c)
		}
	}
	return cuts
}

// cutSubpaths writes the construction operators of subs with the geometry
// inside the frame's cuts removed, and reports whether anything was. An
// untouched subpath is copied byte for byte.
func cutSubpaths(data []byte, ranges []content.ByteRange, subs []subpath, paint string, f cutFrame) (string, bool, error) {
	fills := paint != "S" && paint != "s"
	strokes := pathPaintStrokes(paint)
	paintCloses := pathPaintCloses(paint)

	var body pathEmitter
	cut := false
	for si, sp := range subs {
		closed := sp.closed || (paintCloses && si == len(subs)-1)
		out, changed, err := rewriteSubpath(sp, closed, fills, strokes, f)
		if err != nil {
			return "", false, err
		}
		if !changed {
			if !sp.explicitStart {
				body.nums("m", sp.start)
			}
			body.b.WriteString(opBytes(data, ranges, sp.ops))
			continue
		}
		cut = true
		body.b.WriteString(out)
	}
	return body.b.String(), cut, nil
}

// regionExclusionClips returns one even-odd clip per zone, each a rectangle
// around everything the path paints with the zone inside it, in user space.
//
// One clip per zone, intersected by successive W* n, rather than one path
// holding every zone: under either winding rule a point inside two
// overlapping zones would count as inside the clip again.
func regionExclusionClips(f cutFrame, zones []heuristics.Zone, inked heuristics.Zone, reach float64) string {
	rect := func(e *pathEmitter, x0, y0, x1, y1 float64) {
		e.nums("m", f.toUser(bezPt{x0, y0}))
		e.nums("l", f.toUser(bezPt{x1, y0}))
		e.nums("l", f.toUser(bezPt{x1, y1}))
		e.nums("l", f.toUser(bezPt{x0, y1}))
		e.b.WriteString("h ")
	}
	var e pathEmitter
	margin := 2*reach + clipOuterMargin
	ix0, iy0, ix1, iy1 := heuristics.ZoneBounds(inked)
	for _, z := range zones {
		x0, y0, x1, y1 := heuristics.ZoneBounds(z)
		rect(&e,
			math.Min(ix0, x0)-margin, math.Min(iy0, y0)-margin,
			math.Max(ix1, x1)+margin, math.Max(iy1, y1)+margin)
		rect(&e, x0, y0, x1, y1)
		e.b.WriteString("W* n ")
	}
	return e.b.String()
}

// cutFrame is what cutting one path needs: the cuts in page space and the CTM
// that carries points to and from the path's user space.
type cutFrame struct {
	ctm  [6]float64
	det  float64
	cuts []cutRect
}

// toUser maps a page-space point into user space.
func (f cutFrame) toUser(p bezPt) bezPt {
	dx, dy := p.x-f.ctm[4], p.y-f.ctm[5]
	return bezPt{(dx*f.ctm[3] - dy*f.ctm[2]) / f.det, (dy*f.ctm[0] - dx*f.ctm[1]) / f.det}
}

// inside reports the cut a user-space point lies strictly inside, or -1.
func (f cutFrame) inside(p bezPt) int {
	q := apply(f.ctm, p)
	for i, c := range f.cuts {
		if q.x > c.x0 && q.x < c.x1 && q.y > c.y0 && q.y < c.y1 {
			return i
		}
	}
	return -1
}

// transitionPoint returns, in user space, the centre of the overlap of two
// cuts: the point a chord passes through going from one to the other. It
// depends on the cuts alone, so it tells nothing of where the path crossed.
func (f cutFrame) transitionPoint(from, to int) (bezPt, error) {
	a, b := f.cuts[from], f.cuts[to]
	x0, y0 := math.Max(a.x0, b.x0), math.Max(a.y0, b.y0)
	x1, y1 := math.Min(a.x1, b.x1), math.Min(a.y1, b.y1)
	if x0 > x1 || y0 > y1 {
		return bezPt{}, fmt.Errorf("the path passes between regions %d and %d, which do not meet", from, to)
	}
	return f.toUser(bezPt{(x0 + x1) / 2, (y0 + y1) / 2}), nil
}

// rewriteSubpath returns sp with the geometry inside the cuts removed, and
// whether anything changed. An unchanged subpath is copied by the caller.
//
// A closed subpath, and any subpath a fill alone paints (which the fill
// closes, ISO 32000-1, 8.5.3.1), is a loop: a run inside a cut is replaced by a
// chord wherever it falls, the start included. An open stroked one loses the
// parts before its first and after its last surviving piece, whose ink lies
// inside the region. An open subpath both filled and stroked is closed for the
// fill and open for the stroke, so moving its start would move the fill's
// closing edge: it is rewritten only where its ends and that edge stay outside
// every cut.
func rewriteSubpath(sp subpath, closed, fills, strokes bool, f cutFrame) (string, bool, error) {
	if len(f.cuts) == 0 {
		return "", false, nil
	}
	if len(sp.segs) == 0 {
		// A lone point paints at most a round cap's dot around itself
		// (ISO 32000-1, 8.5.3.2), inside the region when the point is inside a
		// cut.
		return "", f.inside(sp.start) >= 0, nil
	}

	segs := sp.segs
	closingSeg := -1
	loop := closed || !strokes
	if last := segs[len(segs)-1].p[3]; (loop || fills) && last != sp.start {
		closingSeg = len(segs)
		segs = append(slices.Clip(segs), pathSeg{p: [4]bezPt{last, {}, {}, sp.start}})
	}

	pieces := cutPieces(segs, f.ctm, f.cuts)
	if !slices.ContainsFunc(pieces, func(pc piece) bool { return pc.zone >= 0 }) {
		return "", false, nil
	}

	if loop {
		return emitLoop(segs, pieces, closingSeg, fills, f)
	}
	if !fills {
		return emitOpen(segs, pieces, f)
	}
	// The closing edge is the fill's alone, and the ends anchor it.
	errAnchor := errors.New("a filled and stroked open subpath starts, ends or closes inside the region")
	if slices.ContainsFunc(pieces, func(pc piece) bool { return pc.seg == closingSeg && pc.zone >= 0 }) {
		return "", false, errAnchor
	}
	open := slices.DeleteFunc(slices.Clone(pieces), func(pc piece) bool { return pc.seg == closingSeg })
	if len(open) == 0 || open[0].zone >= 0 || open[len(open)-1].zone >= 0 {
		return "", false, errAnchor
	}
	return emitOpen(segs, open, f)
}

// emitOpen writes an open subpath from its first surviving piece to its last,
// with a chord across every run between them that lies inside a cut.
func emitOpen(segs []pathSeg, pieces []piece, f cutFrame) (string, bool, error) {
	first, last := -1, -1
	for i, pc := range pieces {
		if pc.zone < 0 {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	if first < 0 {
		// Everything it strokes lies inside the regions.
		return "", true, nil
	}
	var e pathEmitter
	e.nums("m", evalSeg(segs[pieces[first].seg], pieces[first].t0))
	if err := emitPieces(&e, segs, pieces[first:last+1], f); err != nil {
		return "", false, err
	}
	return e.b.String(), true, nil
}

// emitLoop writes a closed subpath from a point outside every cut, so a run
// that wraps past the original start is written like any other, and closes
// it with h.
func emitLoop(segs []pathSeg, pieces []piece, closingSeg int, fills bool, f cutFrame) (string, bool, error) {
	n := len(pieces)
	start := 0
	if pieces[0].zone >= 0 {
		start = slices.IndexFunc(pieces, func(pc piece) bool { return pc.zone < 0 })
	}
	if start < 0 {
		// Nothing survives. Inside one convex region the loop winds around no
		// point outside it, so it paints nothing there. Across several, the
		// regions can ring an area outside all of them, which a fill still
		// covers: the loop becomes the polygon through the points where it
		// passes from one region to the next.
		multi := slices.ContainsFunc(pieces, func(pc piece) bool { return pc.zone != pieces[0].zone })
		if !fills || !multi {
			return "", true, nil
		}
		for i := range pieces {
			if pieces[i].zone != pieces[(i+n-1)%n].zone {
				start = i
				break
			}
		}
	}

	rotated := make([]piece, 0, n)
	for i := range n {
		rotated = append(rotated, pieces[(start+i)%n])
	}
	var e pathEmitter
	if rotated[0].zone >= 0 {
		p, err := f.transitionPoint(rotated[n-1].zone, rotated[0].zone)
		if err != nil {
			return "", false, err
		}
		e.nums("m", p)
	} else {
		e.nums("m", evalSeg(segs[rotated[0].seg], rotated[0].t0))
	}

	// h draws the last edge back to the start: the chord of a final run, or
	// the closing line the original drew the same way.
	body := rotated
	switch tail := rotated[n-1]; {
	case tail.zone >= 0:
		k := n - 1
		for k > 0 && rotated[k-1].zone == tail.zone {
			k--
		}
		body = rotated[:k]
	case tail.seg == closingSeg && tail.t1 == 1:
		body = rotated[:n-1]
	}
	if err := emitPieces(&e, segs, body, f); err != nil {
		return "", false, err
	}
	e.b.WriteString("h ")
	return e.b.String(), true, nil
}

// emitPieces writes pieces in order after the current point: a surviving
// piece as itself, a run inside one cut as a chord to where the run ends.
//
// Where a run passes straight from one cut into another, the point between
// them lies inside the region and is replaced by [cutFrame.transitionPoint].
// Each chord then still lies inside one convex cut.
func emitPieces(e *pathEmitter, segs []pathSeg, pieces []piece, f cutFrame) error {
	for i := 0; i < len(pieces); i++ {
		pc := pieces[i]
		if pc.zone < 0 {
			e.seg(subSeg(segs[pc.seg], pc.t0, pc.t1))
			continue
		}
		j := i
		for j+1 < len(pieces) && pieces[j+1].zone == pc.zone {
			j++
		}
		end := evalSeg(segs[pieces[j].seg], pieces[j].t1)
		if j+1 < len(pieces) && pieces[j+1].zone >= 0 {
			p, err := f.transitionPoint(pc.zone, pieces[j+1].zone)
			if err != nil {
				return err
			}
			end = p
		}
		e.nums("l", end)
		i = j
	}
	return nil
}

// opBytes returns the bytes of the listed operators, each followed by a space,
// or "" for none.
func opBytes(data []byte, ranges []content.ByteRange, list []int) string {
	var b strings.Builder
	for _, i := range list {
		b.Write(data[ranges[i].Start:ranges[i].End])
		b.WriteByte(' ')
	}
	return b.String()
}

// pathConstruction reports whether an operator constructs a path (ISO
// 32000-1, Table 59). A producer writing anything else inside a path object
// (8.2, Figure 9 allows nothing else there) still has it do what it does, so
// a path removed whole keeps it in place.
func pathConstruction(operator string) bool {
	switch operator {
	case "m", "l", "c", "v", "y", "h", "re":
		return true
	}
	return false
}
