package layout

import (
	"fmt"
	"math"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/detmath"
	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// maxGradientStops bounds the stops the shading of one gradient holds once
// a repeating gradient is unrolled over its box and the curved parts are
// split: past it a repeating gradient is painted with its average colour,
// with a warning. It is the typeset model's own bound. Anti-DoS guard,
// not spec-mandated: a page-wide stripe pattern of 10 point periods holds
// some 250 stops.
const maxGradientStops = 1024

// gradientSplits is the number of linear pieces a curved segment of a
// gradient is split into: a segment with a transition hint (CSS Images 4,
// 3.5.3), or between stops of different alpha, whose premultiplied
// interpolation is not linear in straight alpha (CSS Images 3, 3.4.3).
// Eight pieces keep the curve within a few levels of 8-bit colour.
const gradientSplits = 8

// interpolatedStep is the length in points of the linear pieces a
// segment of a gradient interpolated outside sRGB is split into (CSS
// Images 4, 3.5.2), between interpolatedMin and interpolatedMax pieces: a
// hue sweep or an Oklab ramp bends further from a line in sRGB than a
// hint's curve, and clipping to sRGB bends it sharply, so the pieces
// follow the length rather than a fixed count.
const (
	interpolatedStep = 0.5
	interpolatedMin  = 16
	interpolatedMax  = 256
)

// gstop is a colour stop or hint after the fixup of CSS Images 3, 3.4.3,
// its position in points along the gradient line or ray.
type gstop struct {
	pos  float64
	c    typeset.Color
	hint bool
	// idx is the stop's index in the gradient's stops.
	idx int
}

// gpiece is one piece of a gradient's colour function: constant before the
// first stop and after the last, a ramp between two stops otherwise.
type gpiece struct {
	a, b   float64
	ca, cb typeset.Color
	// hint is the transition hint's position as a fraction of [a, b], -1
	// for none.
	hint float64
	// g, when set, interpolates the colours between its stops ia and ib
	// outside sRGB ([css.Gradient.Interpolated]).
	g      *css.Gradient
	ia, ib int
}

// at returns the piece's colour at s, interpolated in premultiplied sRGB
// (CSS Images 3, 3.4.3) with the hint's curve (CSS Images 4, 3.5.3).
func (p gpiece) at(s float64) typeset.Color {
	if p.ca == p.cb && p.hint < 0 {
		return p.ca
	}
	if p.b <= p.a || math.IsInf(p.a, 0) || math.IsInf(p.b, 0) {
		if s < p.b {
			return p.ca
		}
		return p.cb
	}
	t := min(max((s-p.a)/(p.b-p.a), 0), 1)
	if p.hint >= 0 {
		switch {
		case p.hint <= 0:
			t = 1
		case p.hint >= 1:
			t = 0
		default:
			t = detmath.Pow(t, detmath.Log(0.5)/detmath.Log(p.hint))
		}
	}
	if p.g != nil {
		c, _ := colorOf(p.g.Mix(p.ia, p.ib, t))
		return c
	}
	return mix(p.ca, p.cb, t)
}

// curved reports whether the piece is not linear in straight alpha.
func (p gpiece) curved() bool {
	return p.hint >= 0 || p.ca.A != p.cb.A || p.g != nil
}

// mix interpolates a and b at t in premultiplied space and returns the
// straight colour.
func mix(a, b typeset.Color, t float64) typeset.Color {
	al := a.A + float64((b.A-a.A)*t)
	if al <= 0 {
		return typeset.Color{}
	}
	ch := func(x, y float64) float64 {
		return min(max((float64(x*a.A*(1-t))+float64(y*b.A*t))/al, 0), 1)
	}
	return typeset.Color{R: ch(a.R, b.R), G: ch(a.G, b.G), B: ch(a.B, b.B), A: min(al, 1)}
}

// fixStops returns the stops of g with their positions resolved against
// length, the gradient line's or the ray's (CSS Images 3, 3.4.3, Color
// Stop "Fixup", with hints after CSS Images 4, 3.5.3): a first stop
// without a position at 0, a last one at length, a position before an
// earlier one moved up to it, and runs without positions spread evenly.
func fixStops(g *css.Gradient, length float64) []gstop {
	n := len(g.Stops)
	out := make([]gstop, n)
	set := make([]bool, n)
	for i, s := range g.Stops {
		c, _ := colorOf(s.Color)
		if c.A == 0 {
			// A transparent stop keeps no colour of its own: its
			// premultiplied neighbours decide (CSS Images 3, 3.4.3).
			c = typeset.Color{}
		}
		out[i] = gstop{c: c, hint: s.Hint, idx: i}
		if s.Position.IsLength() {
			out[i].pos, set[i] = resolve(s.Position, length), true
		}
	}
	if !set[0] {
		out[0].pos, set[0] = 0, true
	}
	if !set[n-1] {
		out[n-1].pos, set[n-1] = length, true
	}
	high := math.Inf(-1)
	for i := range out {
		if set[i] {
			out[i].pos = max(out[i].pos, high)
			high = out[i].pos
		}
	}
	for i := 0; i < n; {
		if set[i] {
			i++
			continue
		}
		j := i
		for j < n && !set[j] {
			j++
		}
		// set[i-1] and set[j] hold: the ends are set above.
		from, to := out[i-1].pos, out[j].pos
		for k := i; k < j; k++ {
			out[k].pos = from + (to-from)*float64(k-i+1)/float64(j-i+1)
		}
		i = j
	}
	return out
}

// pieces returns the ramps between consecutive stops, a hint between two
// stops bending the ramp it sits in, interpolated by g when it
// interpolates outside sRGB, nil for none.
func pieces(stops []gstop, g *css.Gradient) []gpiece {
	var out []gpiece
	prev := -1
	hint := math.NaN()
	for i, s := range stops {
		if s.hint {
			hint = s.pos
			continue
		}
		if prev >= 0 {
			a, b := stops[prev], s
			p := gpiece{a: a.pos, b: b.pos, ca: a.c, cb: b.c, hint: -1}
			if g != nil && g.Interpolated() {
				p.g, p.ia, p.ib = g, a.idx, b.idx
			}
			if !math.IsNaN(hint) && b.pos > a.pos {
				p.hint = min(max((hint-a.pos)/(b.pos-a.pos), 0), 1)
			}
			out = append(out, p)
		}
		prev, hint = i, math.NaN()
	}
	return out
}

// colorStops returns the stops of g as a shading over [lo, hi] along its
// line or ray, positions in points resolved against length, and lo and hi.
// A repeating gradient is unrolled over [from, to], the part of the line
// or ray its box shows; a plain one spans its stops and the shading
// extends past them. ray clamps the span at 0, where a radial gradient
// starts; turn spans [from, to] whatever the stops, the full turn of a
// conic gradient. ok is false when the gradient paints a single colour,
// solid, which is then returned.
func (e *engine) colorStops(g *css.Gradient, length, from, to float64, ray, turn bool, y float64) (stops []typeset.ColorStop, lo, hi float64, solid typeset.Color, ok bool) {
	gs := fixStops(g, length)
	ps := pieces(gs, g)
	var first, last typeset.Color
	for _, s := range gs {
		if !s.hint {
			last = s.c
		}
	}
	first = gs[0].c
	if len(ps) == 0 {
		// One stop paints its colour everywhere (CSS Images 4, 3.5.1).
		return nil, 0, 0, first, false
	}
	p0, pn := ps[0].a, ps[len(ps)-1].b
	var span []gpiece
	if g.Repeating {
		period := pn - p0
		if math.IsNaN(period) || period <= eps {
			return nil, 0, 0, averageEquidistant(gs), false
		}
		if ray {
			from = max(from, 0)
		}
		kmin, kmax := math.Floor((from-p0)/period), math.Ceil((to-p0)/period)
		if n := (kmax - kmin + 1) * float64(len(ps)); math.IsNaN(n) || n > maxGradientStops/2 {
			e.warnOnce(warn.LimitReached, warn.Approximated, fmt.Sprintf("a repeating gradient of more than %d stops was painted with its average colour", maxGradientStops), y)
			return nil, 0, 0, average(ps), false
		}
		for k := kmin; k <= kmax; k++ {
			for _, p := range ps {
				p.a += float64(k * period)
				p.b += float64(k * period)
				span = append(span, p)
			}
		}
		lo, hi = from, to
	} else {
		span = append(span, gpiece{a: math.Inf(-1), b: p0, ca: first, cb: first, hint: -1})
		span = append(span, ps...)
		span = append(span, gpiece{a: pn, b: math.Inf(1), ca: last, cb: last, hint: -1})
		// The span reaches a point past each end stop, so a sharp edge at
		// an end stop falls inside it: the shading extends with the
		// colour its end offsets carry.
		lo, hi = p0-1, pn+1
		if turn {
			lo, hi = from, to
		}
		if ray {
			if pn <= 0 {
				return nil, 0, 0, last, false
			}
			lo = max(lo, 0)
		}
	}
	if !numeric.Positive(hi - lo) {
		return nil, 0, 0, last, false
	}
	splits := gradientSplits
	for range 2 {
		stops = stops[:0]
		for _, p := range span {
			a, b := max(p.a, lo), min(p.b, hi)
			if b < a || (b == a && p.b > p.a) {
				continue
			}
			n := 1
			if p.curved() && b > a {
				n = splits
				if p.g != nil && splits > 1 {
					n = int(min(max(math.Ceil((b-a)/interpolatedStep), interpolatedMin), interpolatedMax)) // fits int: at most interpolatedMax
				}
			}
			for i := 0; i <= n; i++ {
				s := a + (b-a)*float64(i)/float64(n)
				c := p.at(s)
				if i == 0 && p.b <= p.a {
					c = p.ca
				}
				stops = appendStop(stops, typeset.ColorStop{Offset: (s - lo) / (hi - lo), Color: c})
			}
		}
		if len(stops) <= maxGradientStops {
			break
		}
		splits = 1
	}
	if len(stops) > maxGradientStops || len(stops) < 2 {
		e.warnOnce(warn.LimitReached, warn.Approximated, fmt.Sprintf("a gradient of more than %d stops was painted with its average colour", maxGradientStops), y)
		return nil, 0, 0, average(ps), false
	}
	stops[0].Offset, stops[len(stops)-1].Offset = 0, 1
	visible := false
	for i := range stops {
		stops[i].Offset = min(max(stops[i].Offset, 0), 1)
		if i > 0 {
			stops[i].Offset = max(stops[i].Offset, stops[i-1].Offset)
		}
		visible = visible || stops[i].Color.A > 0
	}
	if !visible {
		return nil, 0, 0, typeset.Color{}, false
	}
	return stops, lo, hi, typeset.Color{}, true
}

// appendStop appends s unless it repeats the last stop.
func appendStop(stops []typeset.ColorStop, s typeset.ColorStop) []typeset.ColorStop {
	if n := len(stops); n > 0 && stops[n-1] == s {
		return stops
	}
	return append(stops, s)
}

// average returns the average colour of the ramps ps over their span, in
// premultiplied space: what CSS Images 3, 3.4.1 paints a repeating
// gradient with when the device cannot resolve its period.
func average(ps []gpiece) typeset.Color {
	var r, g, b, a, total float64
	const samples = 16
	for _, p := range ps {
		w := p.b - p.a
		if !numeric.Positive(w) {
			continue
		}
		for i := range samples {
			c := p.at(p.a + float64(w*(float64(i)+0.5)/samples))
			k := float64(w / samples)
			r += float64(c.R * c.A * k)
			g += float64(c.G * c.A * k)
			b += float64(c.B * c.A * k)
			a += float64(c.A * k)
			total += k
		}
	}
	if !numeric.Positive(a) || !numeric.Positive(total) {
		return typeset.Color{}
	}
	return typeset.Color{R: min(r/a, 1), G: min(g/a, 1), B: min(b/a, 1), A: min(a/total, 1)}
}

// averageEquidistant returns the average colour of the stops gs spread
// evenly over a unit span: CSS Images 3, 3.4.1 paints a repeating gradient
// whose stops share one position with it.
func averageEquidistant(gs []gstop) typeset.Color {
	var even []gstop
	for _, s := range gs {
		if !s.hint {
			even = append(even, s)
		}
	}
	if len(even) == 1 {
		return even[0].c
	}
	for i := range even {
		even[i].pos = float64(i)
	}
	return average(pieces(even, nil))
}

// gradientFill returns the fill of the gradient g over a w by h box at the
// origin: a shading, or a flat colour when the gradient paints one. ok is
// false when it paints nothing.
func (e *engine) gradientFill(g *css.Gradient, w, h, y float64) (*typeset.Fill, bool) {
	if !numeric.Positive(w) || !numeric.Positive(h) || len(g.Stops) == 0 {
		return nil, false
	}
	switch g.Kind {
	case css.RadialGradient:
		return e.radialFill(g, w, h, y)
	case css.ConicGradient:
		return e.conicFill(g, w, h, y)
	case css.LinearGradient:
	}
	// CSS Images 3, 3.1.1: the angle points from the start to the end of
	// the gradient line, 0deg up and 90deg right; "to <corner>" makes the
	// line perpendicular to the diagonal between the two other corners. A
	// Diagonal one runs along the diagonal itself, from corner to corner:
	// along it the line's length is the diagonal's.
	theta := g.Angle * math.Pi / 180
	switch {
	case g.Corner != [2]int8{} && g.Diagonal:
		theta = detmath.Atan2(float64(g.Corner[0])*w, -float64(g.Corner[1])*h)
	case g.Corner != [2]int8{}:
		theta = detmath.Atan2(float64(g.Corner[0])*h, -float64(g.Corner[1])*w)
	}
	dx, dy := detmath.Sin(theta), -detmath.Cos(theta)
	length := math.Abs(w*dx) + math.Abs(h*dy)
	stops, lo, hi, solid, ok := e.colorStops(g, length, 0, length, false, false, y)
	if !ok {
		return flatFill(solid)
	}
	sx, sy := float64(w/2)-float64(dx*length/2), float64(h/2)-float64(dy*length/2)
	return &typeset.Fill{Shading: &typeset.Shading{
		Kind:  typeset.Axial,
		From:  typeset.Point{X: sx + float64(dx*lo), Y: sy + float64(dy*lo)},
		To:    typeset.Point{X: sx + float64(dx*hi), Y: sy + float64(dy*hi)},
		Stops: stops,
	}}, true
}

// flatFill returns the fill of a single colour, false for a transparent
// one.
func flatFill(c typeset.Color) (*typeset.Fill, bool) {
	if c.A <= 0 {
		return nil, false
	}
	return &typeset.Fill{Color: c}, true
}

// minRadius is the radius a degenerate ending shape takes: CSS Images 3,
// 3.2.2 renders a zero-sized one as a very small one.
const minRadius = 0.01

// radialFill returns the fill of the radial gradient g over a w by h box
// at the origin (CSS Images 3, 3.2): circles, or ellipses as circles
// scaled vertically about the centre.
func (e *engine) radialFill(g *css.Gradient, w, h, y float64) (*typeset.Fill, bool) {
	cx, cy := resolve(g.Center[0], w), resolve(g.Center[1], h)
	l, r, t, b := math.Abs(cx), math.Abs(w-cx), math.Abs(cy), math.Abs(h-cy)
	corners := [4][2]float64{{l, t}, {r, t}, {l, b}, {r, b}}
	corner := func(far bool) (float64, float64) {
		best := corners[0]
		for _, c := range corners[1:] {
			d, bd := detmath.Hypot(c[0], c[1]), detmath.Hypot(best[0], best[1])
			if (far && d > bd) || (!far && d < bd) {
				best = c
			}
		}
		return best[0], best[1]
	}
	var rx, ry float64
	switch g.Size {
	case css.ClosestSide, css.FarthestSide:
		pick := math.Min
		if g.Size == css.FarthestSide {
			pick = math.Max
		}
		rx, ry = pick(l, r), pick(t, b)
		if g.Circle {
			rx = pick(rx, ry)
			ry = rx
		}
	case css.ClosestCorner, css.FarthestCorner:
		far := g.Size == css.FarthestCorner
		dx, dy := corner(far)
		if g.Circle {
			rx = detmath.Hypot(dx, dy)
			ry = rx
			break
		}
		// The ellipse keeps the aspect ratio of the closest-side or
		// farthest-side one and passes through the corner.
		pick := math.Min
		if far {
			pick = math.Max
		}
		sx, sy := pick(l, r), pick(t, b)
		if sx > 0 && sy > 0 {
			k := sx / sy
			rx = math.Sqrt(float64(dx*dx) + float64(k*k*dy*dy))
			ry = rx / k
		}
	case css.RadialExplicit:
		rx, ry = resolve(g.Radius[0], w), resolve(g.Radius[1], h)
		if g.Circle {
			ry = rx
		}
	}
	if !numeric.PositiveFinite(rx) || !numeric.PositiveFinite(ry) {
		rx, ry = minRadius, minRadius
	}
	k := ry / rx
	// The farthest corner in the circles' own space, where y is divided
	// by k, bounds the radii the box shows.
	reach := 0.0
	for _, c := range corners {
		reach = max(reach, detmath.Hypot(c[0], c[1]/k))
	}
	stops, lo, hi, solid, ok := e.colorStops(g, rx, 0, reach, true, false, y)
	if !ok {
		return flatFill(solid)
	}
	c := typeset.Point{X: cx, Y: cy}
	return &typeset.Fill{Shading: &typeset.Shading{
		Kind: typeset.Radial, From: c, To: c, FromRadius: lo, ToRadius: hi,
		Stops:     stops,
		Transform: typeset.Matrix{A: 1, D: k, F: cy * (1 - k)},
	}}, true
}

// conicFill returns the fill of the conic gradient g over a w by h box at
// the origin (CSS Images 4, 3.3): its stops over the full turn about its
// centre, starting at its from angle, 0deg up and growing clockwise.
// Positions are fractions of the turn, resolved here against 360.
func (e *engine) conicFill(g *css.Gradient, w, h, y float64) (*typeset.Fill, bool) {
	stops, _, _, solid, ok := e.colorStops(g, 360, 0, 360, false, true, y)
	if !ok {
		return flatFill(solid)
	}
	cx, cy := resolve(g.Center[0], w), resolve(g.Center[1], h)
	a := g.Angle * math.Pi / 180
	c := typeset.Point{X: cx, Y: cy}
	return &typeset.Fill{Shading: &typeset.Shading{
		Kind: typeset.Conic, From: c, To: typeset.Point{X: cx + detmath.Sin(a), Y: cy - detmath.Cos(a)},
		Stops: stops,
	}}, true
}

// gradientRect returns the path painting the gradient g over r, sized as
// r, nil when it paints nothing.
func (e *engine) gradientRect(g *css.Gradient, r typeset.Rect) typeset.Fragment {
	fill, ok := e.gradientFill(g, r.Width, r.Height, r.Y)
	if !ok {
		return nil
	}
	if fill.Shading != nil {
		sh := *fill.Shading
		m := sh.Transform
		if m == (typeset.Matrix{}) {
			m = typeset.Identity
		}
		m.E += r.X
		m.F += r.Y
		sh.Transform = m
		fill = &typeset.Fill{Shading: &sh}
	}
	return &typeset.Path{Ops: rectOps(r), Fill: fill}
}

// gradientTiles returns the paths painting the gradient of the
// background layer l over the positioning area within the painting area clip: the gradient
// is an image without intrinsic size or ratio, so its size is the area's
// (CSS Backgrounds 3, 3.9), placed and repeated as an image is.
func (e *engine) gradientTiles(l *css.BackgroundLayer, area, clip typeset.Rect, y float64) []*typeset.Path {
	if !numeric.Positive(area.Width) || !numeric.Positive(area.Height) {
		return nil
	}
	sized := *l
	if bs := &sized.Size; bs.Kind == css.SizeExplicit {
		// CSS Backgrounds 3, 3.9: an auto dimension of an image with
		// neither intrinsic size nor ratio is 100%.
		if !bs.Width.IsLength() {
			bs.Width = css.Dimension{Kind: css.Percent, Value: 1}
		}
		if !bs.Height.IsLength() {
			bs.Height = css.Dimension{Kind: css.Percent, Value: 1}
		}
	}
	tiles, over := tileRects(&sized, &Picture{Width: area.Width, Height: area.Height}, area, clip)
	if len(tiles) == 0 {
		return nil
	}
	if over {
		// Tiles too small to count, a fraction of a pixel, blend into the
		// gradient's average colour, which fills the painting area: a
		// gradient of one colour stays that colour, as browsers draw it.
		c := gradientAverage(l.Gradient, tiles[0].Width, tiles[0].Height)
		if c.A <= 0 {
			return nil
		}
		return []*typeset.Path{{Ops: rectOps(clip), Fill: &typeset.Fill{Color: c}}}
	}
	fill, ok := e.gradientFill(l.Gradient, tiles[0].Width, tiles[0].Height, y)
	if !ok {
		return nil
	}
	out := make([]*typeset.Path, 0, len(tiles))
	for _, t := range tiles {
		var f *typeset.Fill
		if fill.Shading != nil {
			sh := *fill.Shading
			m := sh.Transform
			if m == (typeset.Matrix{}) {
				m = typeset.Identity
			}
			m.E += t.X
			m.F += t.Y
			sh.Transform = m
			f = &typeset.Fill{Shading: &sh}
		} else {
			f = &typeset.Fill{Color: fill.Color}
		}
		out = append(out, &typeset.Path{Ops: rectOps(t), Fill: f})
	}
	return out
}

// gradientAverage returns the average colour of the gradient g over a w by
// h box, its stops placed on a line as long as the box's larger side.
func gradientAverage(g *css.Gradient, w, h float64) typeset.Color {
	if len(g.Stops) == 0 {
		return typeset.Color{}
	}
	gs := fixStops(g, max(w, h))
	if len(gs) == 0 {
		return typeset.Color{}
	}
	ps := pieces(gs, g)
	if len(ps) == 0 {
		return gs[0].c
	}
	return average(ps)
}
