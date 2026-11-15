package officefx

import (
	"context"
	"math"
	"sort"

	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/shape"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// Warp says how a body's text is fitted to a text warp ([WarpText]).
type Warp struct {
	// Lanes are the warp's lanes fitted to the shape's box
	// ([shape.WarpLanes]), in the order the body's lines take them.
	Lanes []shape.WarpLane
	// Fit scales the lines that follow a path so that the widest of them
	// spans its path, as a body converted from WordArt asks (ECMA-376 Part
	// 1, 21.1.2.1.1 bodyPr fromWordArt); false keeps the text's size.
	Fit bool
	// Block is the width the body's lines were laid out in, in the text's
	// own coordinates, from 0: a line that follows a path keeps its place
	// in it, its start, middle or end on the path's.
	Block float64
}

// WarpNotes reports what [WarpText] left out.
type WarpNotes struct {
	// Effects reports effect bitmaps of the text, which a warp does not
	// bend, left out.
	Effects bool
	// Capped reports a text past [MaxWarpGlyphs] glyphs or [MaxWarpOps]
	// path operations, drawn unwarped.
	Capped bool
}

// The bounds of one warped text. Past them the text is drawn unwarped.
const (
	// MaxWarpGlyphs caps the glyphs of one warped body. Anti-DoS guard,
	// not spec-mandated: WordArt is a few words, a text box warped to a
	// shape a few paragraphs.
	MaxWarpGlyphs = 4096
	// MaxWarpOps caps the path operations one warped body writes, each
	// glyph segment cut into at most maxWarpPieces. Anti-DoS guard, not
	// spec-mandated: a glyph has a few dozen segments.
	MaxWarpOps = 1 << 19
	// maxWarpPieces caps the pieces one segment of an outline is cut into
	// before it is mapped into an envelope.
	maxWarpPieces = 16
	// warpStep is the share of an envelope's width one piece of an
	// outline spans at most: past it the segment is cut, so that a line
	// bends with the envelope.
	warpStep = 1.0 / 64
)

// pathOffset is how far a path a line follows lies above the line's
// baseline, in ems of the line's font size as the warp scales it.
//
// # Corpus evidence
//
//   - litho/pptx text-warp-wordart: the glyphs of "WordArt" in Calibri
//     Bold on textArchUp, textArchDown, textCircle and textButton fit
//     Calibri Bold's outlines turned and scaled within 0.02 point, each
//     upright on the path at its advance's middle, their baseline 0.152
//     to 0.153 em of the scaled size below the path on the arcs at 55.4
//     points; "Arch with adjust" in Calibri Bold at 111.5 points on a
//     textArchUp of 708.5 by 173.2 points lies 0.154 em below its path.
const pathOffset = 0.154

// WarpText returns the fragments of a body's laid-out text frags, mapped
// by m into the box the warp's lanes are fitted to, warped as w says. A
// line takes the lane of its rank, the lines past the last lane the last
// one. In an envelope lane the ink box of its lines is stretched between
// the lane's paths (ECMA-376 Part 1, 20.1.9.19): each outline point is
// moved, its segments cut and bent, the glyphs drawn as their outlines
// in their run's colour, artifacts, over the text kept invisible where it
// lay, so it is still copied, searched and tagged. In a lane of one path
// each glyph is turned upright on the path at its advance's middle and
// stays text; the lines after the first follow it at their distance.
// The paths of the text (decorations, outlines, gradient fills) are
// mapped with the glyphs they cover; effect bitmaps are left out.
//
// A text whose transform turns or flips it, or past the bounds, is
// returned mapped by m and unwarped.
//
// # Corpus evidence
//
//   - litho/pptx text-warp-wordart: PowerPoint stretches the ink of
//     "WordArt" between the paths of every two-path preset to the box's
//     edges, each glyph point within 0.4 point of the point the same
//     fractions of the paths' lengths give on textPlain, textChevron,
//     textInflate, the fades, slants and cascades, its segments' ends
//     moved, its lines drawn as cubic Béziers; it turns and scales the
//     glyphs whole on the presets of one path. The text stays in the page,
//     invisible, at its unwarped place.
//   - libreoffice/pptx sd/tdf116350-texteffects: a body not from WordArt
//     follows textArchUp at its 18 point size from the path's start, each
//     glyph text with its own turned matrix; oox/tdf125582_TextOnCircle
//     sets its second line inside the first, at its line distance.
//   - libreoffice/pptx sd/tdf125573_FontWorkScaleX: a body from WordArt
//     of two lines on textArchDown prints both at 47.3 points, the scale
//     at which the longer one spans the path.
func WarpText(ctx context.Context, frags []typeset.Fragment, m typeset.Matrix, w *Warp, outlines *text.Outlines) ([]typeset.Fragment, WarpNotes, error) {
	unwarped := []typeset.Fragment{&typeset.Group{Transform: m, Opacity: 1, Content: frags}}
	if w == nil || len(w.Lanes) == 0 || m.B != 0 || m.C != 0 || m.A <= 0 || m.D <= 0 {
		return unwarped, WarpNotes{}, nil
	}
	wp := &warper{w: w, outlines: outlines}
	if err := wp.collect(ctx, frags, m, 0); err != nil {
		return nil, WarpNotes{}, err
	}
	if wp.glyphs > MaxWarpGlyphs || len(wp.runs) == 0 {
		return unwarped, WarpNotes{Capped: wp.glyphs > MaxWarpGlyphs}, nil
	}
	wp.lines(m)
	if err := wp.prepare(ctx); err != nil {
		return nil, WarpNotes{}, err
	}
	out, err := wp.produce(ctx, frags, m)
	if err != nil {
		return nil, WarpNotes{}, err
	}
	if wp.capped {
		return unwarped, WarpNotes{Capped: true}, nil
	}
	return out, WarpNotes{Effects: wp.effects}, nil
}

// warper holds one body's text being warped.
type warper struct {
	w        *Warp
	outlines *text.Outlines
	runs     []*warpRun
	byText   map[*typeset.Text]*warpRun
	glyphs   int
	ops      int
	lanes    []laneText
	effects  bool
	capped   bool
}

// warpRun is one run of the text with the transform into the box.
type warpRun struct {
	t        *typeset.Text
	m        typeset.Matrix
	baseline float64
	line     int
}

// laneText is what one lane holds: its lines and, for an envelope, the
// ink box they fill it with; for a path, the glyphs' places.
type laneText struct {
	lane  *shape.WarpLane
	box   typeset.Rect
	ink   bool
	cells []glyphCell
}

// glyphCell is the place of one glyph of a lane of one path: its advance
// on its line in box coordinates and the transform that sets it on the
// path.
type glyphCell struct {
	x0, x1, baseline float64
	m                typeset.Matrix
}

// collect gathers the runs of frags, mapped by m, and counts their glyphs.
func (wp *warper) collect(ctx context.Context, frags []typeset.Fragment, m typeset.Matrix, depth int) error {
	if depth > maxWarpDepth {
		return nil
	}
	for _, f := range frags {
		switch v := f.(type) {
		case *typeset.Text:
			if v.Face == nil || len(v.Glyphs) == 0 {
				continue
			}
			wp.glyphs += len(v.Glyphs)
			if wp.glyphs > MaxWarpGlyphs {
				return nil
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			y := mapPoint(m, v.Origin)
			wp.runs = append(wp.runs, &warpRun{t: v, m: m, baseline: y.Y})
		case *typeset.Group:
			if err := wp.collect(ctx, v.Content, mul(m, groupTransform(v)), depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}

// maxWarpDepth caps the groups a warp walks into. Recursion guard, not
// spec-mandated: a laid-out body nests a few groups.
const maxWarpDepth = 32

// lines numbers the runs' lines from the top, runs whose baselines lie
// within a quarter of the larger size apart sharing one, and gives each
// line its lane.
func (wp *warper) lines(m typeset.Matrix) {
	sorted := make([]*warpRun, len(wp.runs))
	copy(sorted, wp.runs)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].baseline < sorted[j].baseline })
	line, last, lastSize := -1, math.Inf(-1), 0.0
	for _, r := range sorted {
		size := r.t.Size * m.D
		if line < 0 || r.baseline-last > max(size, lastSize)/4 {
			line++
			last, lastSize = r.baseline, size
		}
		r.line = line
	}
	wp.lanes = make([]laneText, len(wp.w.Lanes))
	for i := range wp.lanes {
		wp.lanes[i].lane = &wp.w.Lanes[i]
	}
	wp.byText = make(map[*typeset.Text]*warpRun, len(wp.runs))
	for _, r := range wp.runs {
		wp.byText[r.t] = r
	}
}

// laneOf returns the lane of line.
func (wp *warper) laneOf(line int) *laneText {
	return &wp.lanes[min(max(line, 0), len(wp.lanes)-1)]
}

// prepare measures each lane's text: the ink box of an envelope's lines,
// the glyph places of a path's.
func (wp *warper) prepare(ctx context.Context) error {
	for _, r := range wp.runs {
		lt := wp.laneOf(r.line)
		if !lt.lane.Envelope() {
			continue
		}
		ops := wp.outlines.Run(nil, r.t)
		for _, op := range ops {
			for _, p := range op.P[:opPoints(op.Kind)] {
				q := mapPoint(r.m, p)
				lt.addInk(q)
			}
		}
	}
	for i := range wp.lanes {
		if err := ctx.Err(); err != nil {
			return err
		}
		if lt := &wp.lanes[i]; !lt.lane.Envelope() {
			wp.placeOnPath(i)
		}
	}
	return nil
}

// addInk grows the lane's ink box to hold p.
func (lt *laneText) addInk(p typeset.Point) {
	if !lt.ink {
		lt.box, lt.ink = typeset.Rect{X: p.X, Y: p.Y}, true
		return
	}
	x0, y0 := min(lt.box.X, p.X), min(lt.box.Y, p.Y)
	x1, y1 := max(lt.box.X+lt.box.Width, p.X), max(lt.box.Y+lt.box.Height, p.Y)
	lt.box = typeset.Rect{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}
}

// pathLine is one line of a lane of one path being placed.
type pathLine struct {
	x0, x1, size float64
	baseline     float64
	glyphs       []pathGlyph
}

// pathGlyph is one glyph of a line being placed: its run, index and
// advance in box coordinates.
type pathGlyph struct {
	r      *warpRun
	x, adv float64
}

// placeOnPath sets the glyphs of lane i on its path: the line scaled by
// k (the path's length over the widest line's advance when the warp
// fits, 1 otherwise), each glyph upright at its advance's middle, which
// lies at its place along the line times k from where the line starts on
// the path, the line's start, middle or end on the path's as it lies in
// the block; the first line's baseline pathOffset ems below the path, the
// others their distance below it.
func (wp *warper) placeOnPath(i int) {
	lt := &wp.lanes[i]
	curve := &lt.lane.Top
	length := curve.Length()
	var lines []*pathLine
	byLine := map[int]*pathLine{}
	for _, r := range wp.runs {
		if wp.laneOf(r.line) != lt {
			continue
		}
		pl := byLine[r.line]
		if pl == nil {
			pl = &pathLine{x0: math.Inf(1), x1: math.Inf(-1), baseline: r.baseline}
			byLine[r.line] = pl
			lines = append(lines, pl)
		}
		pl.size = max(pl.size, r.t.Size*r.m.D)
		for gi := range r.t.Glyphs {
			x := mapPoint(r.m, typeset.Point{X: r.t.Origin.X + float64(r.t.Glyphs[gi].X*runHScale(r.t)), Y: r.t.Origin.Y})
			adv := float64(wp.outlines.Advance(r.t, gi) * r.m.A)
			pl.glyphs = append(pl.glyphs, pathGlyph{r: r, x: x.X, adv: adv})
			pl.x0, pl.x1 = min(pl.x0, x.X), max(pl.x1, x.X+adv)
		}
	}
	if len(lines) == 0 {
		return
	}
	sort.SliceStable(lines, func(a, b int) bool { return lines[a].baseline < lines[b].baseline })
	widest := 0.0
	for _, pl := range lines {
		widest = max(widest, pl.x1-pl.x0)
	}
	k := 1.0
	if wp.w.Fit && numeric.Positive(widest) {
		k = length / widest
	}
	first := lines[0]
	off := float64(pathOffset*k) * first.size
	left, block := wp.blockSpan()
	for _, pl := range lines {
		width := pl.x1 - pl.x0
		share := 0.0
		if room := block - width; room > 0.01 {
			share = min(max((pl.x0-left)/room, 0), 1)
		}
		start := float64((length - float64(k*width)) * share)
		for _, g := range pl.glyphs {
			mid := g.x + float64(g.adv/2)
			s := start + float64(k*(mid-pl.x0))
			p, dir := curve.At(s / length)
			n := typeset.Point{X: -dir.Y, Y: dir.X}
			// The glyph's point (x, y) of the box goes to p + k·(x − mid)·dir +
			// (k·(y − baseline) + off)·n, baseline the first line's.
			m := typeset.Matrix{A: k * dir.X, B: k * dir.Y, C: k * n.X, D: k * n.Y}
			ox, oy := p.X+float64(n.X*off), p.Y+float64(n.Y*off)
			m.E = ox - float64(m.A*mid) - float64(m.C*first.baseline)
			m.F = oy - float64(m.B*mid) - float64(m.D*first.baseline)
			lt.cells = append(lt.cells, glyphCell{x0: g.x, x1: g.x + g.adv, baseline: pl.baseline, m: m})
		}
	}
}

// blockSpan returns where the block the lines were laid out in starts
// and how wide it is, in box coordinates.
func (wp *warper) blockSpan() (float64, float64) {
	if len(wp.runs) == 0 {
		return 0, 0
	}
	m := wp.runs[0].m
	return m.E, wp.w.Block * m.A
}

// produce returns frags warped, in box coordinates.
func (wp *warper) produce(ctx context.Context, frags []typeset.Fragment, m typeset.Matrix) ([]typeset.Fragment, error) {
	var out []typeset.Fragment
	for _, f := range frags {
		if wp.capped {
			return nil, nil
		}
		switch v := f.(type) {
		case *typeset.Text:
			r := wp.byText[v]
			if r == nil {
				continue
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			out = append(out, wp.text(r)...)
		case *typeset.Path:
			if p := wp.path(v, m); p != nil {
				out = append(out, p)
			}
		case *typeset.Image:
			wp.effects = true
		case *typeset.Group:
			content, err := wp.produce(ctx, v.Content, mul(m, groupTransform(v)))
			if err != nil {
				return nil, err
			}
			if len(content) == 0 {
				continue
			}
			// The group keeps its opacity and blending; its clip, which
			// cuts the text where it lay, is left out with its transform.
			g := *v
			g.Transform, g.Clip, g.Content = typeset.Identity, nil, content
			out = append(out, &g)
		}
	}
	return out, nil
}

// text returns the run r warped.
func (wp *warper) text(r *warpRun) []typeset.Fragment {
	lt := wp.laneOf(r.line)
	if !lt.lane.Envelope() {
		return wp.textOnPath(r, lt)
	}
	var out []typeset.Fragment
	t := r.t
	if !t.Invisible && t.Color.A > 0 && lt.ink {
		ops := wp.mapEnvelope(wp.outlines.Run(nil, t), r.m, lt)
		if len(ops) > 0 {
			p := &typeset.Path{Ops: ops, Fill: &typeset.Fill{Color: t.Color, Rule: typeset.NonZero}}
			if t.Stroke > 0 {
				p.Stroke = &typeset.Stroke{Color: t.Color, Width: float64(t.Stroke * r.m.A), Join: typeset.RoundJoin}
			}
			out = append(out, p)
		}
	}
	hidden := *t
	hidden.Invisible, hidden.Stroke = true, 0
	return append(out, &typeset.Group{Transform: r.m, Opacity: 1, Content: []typeset.Fragment{&hidden}})
}

// textOnPath returns the glyphs of r each set on its lane's path, text
// under its own transform.
func (wp *warper) textOnPath(r *warpRun, lt *laneText) []typeset.Fragment {
	out := make([]typeset.Fragment, 0, len(r.t.Glyphs))
	for gi := range r.t.Glyphs {
		x := mapPoint(r.m, typeset.Point{X: r.t.Origin.X + float64(r.t.Glyphs[gi].X*runHScale(r.t)), Y: r.t.Origin.Y})
		c := lt.cell(x.X+float64(wp.outlines.Advance(r.t, gi)*r.m.A/2), r.baseline)
		if c == nil {
			continue
		}
		one := *r.t
		one.Glyphs = r.t.Glyphs[gi : gi+1 : gi+1]
		out = append(out, &typeset.Group{Transform: mul(c.m, r.m), Opacity: 1, Content: []typeset.Fragment{&one}})
	}
	return out
}

// cell returns the glyph place of the lane at x on the line of baseline
// y: the one whose advance holds x, else the nearest; nil for none.
func (lt *laneText) cell(x, y float64) *glyphCell {
	var best *glyphCell
	bestD := math.Inf(1)
	for i := range lt.cells {
		c := &lt.cells[i]
		dx := 0.0
		switch {
		case x < c.x0:
			dx = c.x0 - x
		case x > c.x1:
			dx = x - c.x1
		}
		d := float64(dx*dx) + float64((y-c.baseline)*(y-c.baseline))
		if d < bestD {
			best, bestD = c, d
		}
	}
	return best
}

// path returns the path p, mapped by m, warped: in an envelope its points
// moved, on a path each subpath set with the glyph it covers. Nil when it
// draws nothing.
func (wp *warper) path(p *typeset.Path, m typeset.Matrix) *typeset.Path {
	if len(p.Ops) == 0 {
		return nil
	}
	_, cy := opsCentre(p.Ops, m)
	lt := wp.laneOf(wp.lineAt(cy))
	q := *p
	if lt.lane.Envelope() {
		if !lt.ink {
			return nil
		}
		q.Ops = wp.mapEnvelope(p.Ops, m, lt)
	} else {
		q.Ops = wp.mapOnPath(p.Ops, m, lt)
	}
	if len(q.Ops) == 0 {
		return nil
	}
	return &q
}

// lineAt returns the line whose baseline lies nearest below or at y, the
// line of a mark that sits over or under its text.
func (wp *warper) lineAt(y float64) int {
	best, bestD := 0, math.Inf(1)
	for _, r := range wp.runs {
		d := r.baseline - y
		if d < 0 {
			// A mark under the baseline belongs to that line, nearer than
			// one far below.
			d = -2 * d
		}
		if d < bestD {
			best, bestD = r.line, d
		}
	}
	return best
}

// mapOnPath returns ops, mapped by m, each subpath set with the glyph
// whose advance holds its middle.
func (wp *warper) mapOnPath(ops []typeset.PathOp, m typeset.Matrix, lt *laneText) []typeset.PathOp {
	out := make([]typeset.PathOp, 0, len(ops))
	for start := 0; start < len(ops); {
		end := start + 1
		for end < len(ops) && ops[end].Kind != typeset.MoveTo {
			end++
		}
		sub := ops[start:end]
		cx, cy := opsCentre(sub, m)
		c := lt.cell(cx, wp.baselineOf(wp.lineAt(cy)))
		if c != nil {
			if wp.ops += len(sub); wp.ops > MaxWarpOps {
				wp.capped = true
				return nil
			}
			full := mul(c.m, m)
			for _, op := range sub {
				o := op
				for j := range opPoints(op.Kind) {
					o.P[j] = mapPoint(full, op.P[j])
				}
				out = append(out, o)
			}
		}
		start = end
	}
	return out
}

// baselineOf returns the baseline of line.
func (wp *warper) baselineOf(line int) float64 {
	for _, r := range wp.runs {
		if r.line == line {
			return r.baseline
		}
	}
	return 0
}

// mapEnvelope returns ops, mapped by m, moved into the lane's envelope:
// each segment cut into pieces of at most warpStep of the ink box's width
// and drawn as cubic Béziers whose ends and control points are moved.
func (wp *warper) mapEnvelope(ops []typeset.PathOp, m typeset.Matrix, lt *laneText) []typeset.PathOp {
	box := lt.box
	toUV := func(p typeset.Point) (float64, float64) {
		u, v := 0.5, 0.5
		if box.Width > 0 {
			u = (p.X - box.X) / box.Width
		}
		if box.Height > 0 {
			v = (p.Y - box.Y) / box.Height
		}
		return u, v
	}
	move := func(p typeset.Point) typeset.Point { return lt.lane.Map(toUV(p)) }
	out := make([]typeset.PathOp, 0, 2*len(ops))
	var cur, start typeset.Point
	curve := func(p0, p1, p2, p3 typeset.Point) {
		u0, _ := toUV(p0)
		u3, _ := toUV(p3)
		lo, hi := min(u0, u3), max(u0, u3)
		for _, p := range [2]typeset.Point{p1, p2} {
			u, _ := toUV(p)
			lo, hi = min(lo, u), max(hi, u)
		}
		n := min(max(int(math.Ceil((hi-lo)/warpStep)), 1), maxWarpPieces)
		for i := range n {
			_, b, c, d := cubicPiece(p0, p1, p2, p3, float64(i)/float64(n), float64(i+1)/float64(n))
			out = append(out, typeset.PathOp{Kind: typeset.CurveTo, P: [3]typeset.Point{move(b), move(c), move(d)}})
		}
	}
	line := func(a, b typeset.Point) {
		curve(a, lerp(a, b, 1.0/3), lerp(a, b, 2.0/3), b)
	}
	for _, op := range ops {
		var p [3]typeset.Point
		for j := range opPoints(op.Kind) {
			p[j] = mapPoint(m, op.P[j])
		}
		switch op.Kind {
		case typeset.MoveTo:
			cur, start = p[0], p[0]
			out = append(out, typeset.PathOp{Kind: typeset.MoveTo, P: [3]typeset.Point{move(cur)}})
		case typeset.LineTo:
			line(cur, p[0])
			cur = p[0]
		case typeset.CurveTo:
			curve(cur, p[0], p[1], p[2])
			cur = p[2]
		case typeset.ClosePath:
			if cur != start {
				line(cur, start)
			}
			out = append(out, typeset.PathOp{Kind: typeset.ClosePath})
			cur = start
		}
		if wp.ops+len(out) > MaxWarpOps {
			wp.capped = true
			return nil
		}
	}
	wp.ops += len(out)
	return out
}

// cubicPiece returns the part of the cubic Bézier p0..p3 from parameter
// t0 to t1, by de Casteljau's construction.
func cubicPiece(p0, p1, p2, p3 typeset.Point, t0, t1 float64) (typeset.Point, typeset.Point, typeset.Point, typeset.Point) {
	// Cut at t1, keep the head; cut the head at t0/t1, keep the tail.
	split := func(a, b, c, d typeset.Point, t float64) (typeset.Point, typeset.Point, typeset.Point, typeset.Point, typeset.Point, typeset.Point, typeset.Point) {
		ab, bc, cd := lerp(a, b, t), lerp(b, c, t), lerp(c, d, t)
		abc, bcd := lerp(ab, bc, t), lerp(bc, cd, t)
		abcd := lerp(abc, bcd, t)
		return a, ab, abc, abcd, bcd, cd, d
	}
	a, b, c, d := p0, p1, p2, p3
	if t1 < 1 {
		a, b, c, d, _, _, _ = split(p0, p1, p2, p3, t1)
	}
	if t0 > 0 && t1 > 0 {
		_, _, _, a, b, c, d = split(a, b, c, d, t0/t1)
	}
	return a, b, c, d
}

// lerp returns the point t of the way from a to b.
func lerp(a, b typeset.Point, t float64) typeset.Point {
	return typeset.Point{X: a.X + float64((b.X-a.X)*t), Y: a.Y + float64((b.Y-a.Y)*t)}
}

// opPoints returns how many points an operation of kind k carries.
func opPoints(k typeset.OpKind) int {
	switch k {
	case typeset.CurveTo:
		return 3
	case typeset.ClosePath:
		return 0
	case typeset.MoveTo, typeset.LineTo:
	}
	return 1
}

// opsCentre returns the middle of the box of the points of ops mapped by
// m.
func opsCentre(ops []typeset.PathOp, m typeset.Matrix) (float64, float64) {
	x0, y0, x1, y1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, op := range ops {
		for _, p := range op.P[:opPoints(op.Kind)] {
			q := mapPoint(m, p)
			x0, y0, x1, y1 = min(x0, q.X), min(y0, q.Y), max(x1, q.X), max(y1, q.Y)
		}
	}
	if x0 > x1 {
		return 0, 0
	}
	return float64((x0 + x1) / 2), float64((y0 + y1) / 2)
}

// groupTransform returns the transform of g, the identity for the zero
// matrix.
func groupTransform(g *typeset.Group) typeset.Matrix {
	if g.Transform == (typeset.Matrix{}) {
		return typeset.Identity
	}
	return g.Transform
}

// mapPoint returns p mapped through m, each product rounded on its own.
func mapPoint(m typeset.Matrix, p typeset.Point) typeset.Point {
	return typeset.Point{X: float64(m.A*p.X) + float64(m.C*p.Y) + m.E, Y: float64(m.B*p.X) + float64(m.D*p.Y) + m.F}
}

// runHScale returns the horizontal scale t draws at, 1 for none.
func runHScale(t *typeset.Text) float64 {
	if t.HScale <= 0 {
		return 1
	}
	return t.HScale
}
