package layout

import (
	"fmt"
	"math"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
)

// maxGradientStops bounds the stops the shading of one gradient holds once
// a repeating gradient is unrolled over its box and the curved parts are
// split: past it a repeating gradient is painted with its average colour,
// with a warning. It is the composition model's own bound. Anti-DoS guard,
// not spec-mandated: a page-wide stripe pattern of 10 point periods holds
// some 250 stops.
const maxGradientStops = 1024

// gradientSplits is the number of linear pieces a curved segment of a
// gradient is split into: a segment with a transition hint (CSS Images 4,
// 3.5.3), or between stops of different alpha, whose premultiplied
// interpolation is not linear in straight alpha (CSS Images 3, 3.4.3).
// Eight pieces keep the curve within a few levels of 8-bit colour.
const gradientSplits = 8

// codeGradient is the warning code of a gradient painted approximately.
const codeGradient = "gradient-approximated"

// gstop is a colour stop or hint after the fixup of CSS Images 3, 3.4.3,
// its position in points along the gradient line or ray.
type gstop struct {
	pos  float64
	c    compose.Color
	hint bool
}

// gpiece is one piece of a gradient's colour function: constant before the
// first stop and after the last, a ramp between two stops otherwise.
type gpiece struct {
	a, b   float64
	ca, cb compose.Color
	// hint is the transition hint's position as a fraction of [a, b], -1
	// for none.
	hint float64
}

// at returns the piece's colour at s, interpolated in premultiplied sRGB
// (CSS Images 3, 3.4.3) with the hint's curve (CSS Images 4, 3.5.3).
func (p gpiece) at(s float64) compose.Color {
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
			t = math.Pow(t, math.Log(0.5)/math.Log(p.hint))
		}
	}
	return mix(p.ca, p.cb, t)
}

// curved reports whether the piece is not linear in straight alpha.
func (p gpiece) curved() bool {
	return p.hint >= 0 || p.ca.A != p.cb.A
}

// mix interpolates a and b at t in premultiplied space and returns the
// straight colour.
func mix(a, b compose.Color, t float64) compose.Color {
	al := a.A + (b.A-a.A)*t
	if al <= 0 {
		return compose.Color{}
	}
	ch := func(x, y float64) float64 {
		return min(max((x*a.A*(1-t)+y*b.A*t)/al, 0), 1)
	}
	return compose.Color{R: ch(a.R, b.R), G: ch(a.G, b.G), B: ch(a.B, b.B), A: min(al, 1)}
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
			c = compose.Color{}
		}
		out[i] = gstop{c: c, hint: s.Hint}
		if s.Position.Kind == css.Points || s.Position.Kind == css.Percent {
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
// stops bending the ramp it sits in.
func pieces(stops []gstop) []gpiece {
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
// starts. ok is false when the gradient paints a single colour, solid,
// which is then returned.
func (e *engine) colorStops(g *css.Gradient, length, from, to float64, ray bool, y float64) (stops []compose.ColorStop, lo, hi float64, solid compose.Color, ok bool) {
	gs := fixStops(g, length)
	ps := pieces(gs)
	var first, last compose.Color
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
		if !(period > eps) {
			return nil, 0, 0, averageEquidistant(gs), false
		}
		if ray {
			from = max(from, 0)
		}
		kmin, kmax := math.Floor((from-p0)/period), math.Ceil((to-p0)/period)
		if !((kmax-kmin+1)*float64(len(ps)) <= maxGradientStops/2) {
			e.warnOnce(codeGradient, fmt.Sprintf("a repeating gradient of more than %d stops was painted with its average colour", maxGradientStops), y)
			return nil, 0, 0, average(ps), false
		}
		for k := kmin; k <= kmax; k++ {
			for _, p := range ps {
				p.a += k * period
				p.b += k * period
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
		if ray {
			if pn <= 0 {
				return nil, 0, 0, last, false
			}
			lo = max(lo, 0)
		}
	}
	if !(hi > lo) {
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
			}
			for i := 0; i <= n; i++ {
				s := a + (b-a)*float64(i)/float64(n)
				c := p.at(s)
				if i == 0 && p.b <= p.a {
					c = p.ca
				}
				stops = appendStop(stops, compose.ColorStop{Offset: (s - lo) / (hi - lo), Color: c})
			}
		}
		if len(stops) <= maxGradientStops {
			break
		}
		splits = 1
	}
	if len(stops) > maxGradientStops || len(stops) < 2 {
		e.warnOnce(codeGradient, fmt.Sprintf("a gradient of more than %d stops was painted with its average colour", maxGradientStops), y)
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
		return nil, 0, 0, compose.Color{}, false
	}
	return stops, lo, hi, compose.Color{}, true
}

// appendStop appends s unless it repeats the last stop.
func appendStop(stops []compose.ColorStop, s compose.ColorStop) []compose.ColorStop {
	if n := len(stops); n > 0 && stops[n-1] == s {
		return stops
	}
	return append(stops, s)
}

// average returns the average colour of the ramps ps over their span, in
// premultiplied space: what CSS Images 3, 3.4.1 paints a repeating
// gradient with when the device cannot resolve its period.
func average(ps []gpiece) compose.Color {
	var r, g, b, a, total float64
	const samples = 16
	for _, p := range ps {
		w := p.b - p.a
		if !(w > 0) {
			continue
		}
		for i := range samples {
			c := p.at(p.a + w*(float64(i)+0.5)/samples)
			k := w / samples
			r += c.R * c.A * k
			g += c.G * c.A * k
			b += c.B * c.A * k
			a += c.A * k
			total += k
		}
	}
	if !(a > 0) || !(total > 0) {
		return compose.Color{}
	}
	return compose.Color{R: min(r/a, 1), G: min(g/a, 1), B: min(b/a, 1), A: min(a/total, 1)}
}

// averageEquidistant returns the average colour of the stops gs spread
// evenly over a unit span: CSS Images 3, 3.4.1 paints a repeating gradient
// whose stops share one position with it.
func averageEquidistant(gs []gstop) compose.Color {
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
	return average(pieces(even))
}

// gradientFill returns the fill of the gradient g over a w by h box at the
// origin: a shading, or a flat colour when the gradient paints one. ok is
// false when it paints nothing.
func (e *engine) gradientFill(g *css.Gradient, w, h, y float64) (*compose.Fill, bool) {
	if !(w > 0) || !(h > 0) || len(g.Stops) == 0 {
		return nil, false
	}
	if g.Kind == css.RadialGradient {
		return e.radialFill(g, w, h, y)
	}
	// CSS Images 3, 3.1.1: the angle points from the start to the end of
	// the gradient line, 0deg up and 90deg right; "to <corner>" makes the
	// line perpendicular to the diagonal between the two other corners.
	theta := g.Angle * math.Pi / 180
	if g.Corner != [2]int8{} {
		theta = math.Atan2(float64(g.Corner[0])*h, -float64(g.Corner[1])*w)
	}
	dx, dy := math.Sin(theta), -math.Cos(theta)
	length := math.Abs(w*dx) + math.Abs(h*dy)
	stops, lo, hi, solid, ok := e.colorStops(g, length, 0, length, false, y)
	if !ok {
		return flatFill(solid)
	}
	sx, sy := w/2-dx*length/2, h/2-dy*length/2
	return &compose.Fill{Shading: &compose.Shading{
		Kind:  compose.Axial,
		From:  compose.Point{X: sx + dx*lo, Y: sy + dy*lo},
		To:    compose.Point{X: sx + dx*hi, Y: sy + dy*hi},
		Stops: stops,
	}}, true
}

// flatFill returns the fill of a single colour, false for a transparent
// one.
func flatFill(c compose.Color) (*compose.Fill, bool) {
	if c.A <= 0 {
		return nil, false
	}
	return &compose.Fill{Color: c}, true
}

// minRadius is the radius a degenerate ending shape takes: CSS Images 3,
// 3.2.2 renders a zero-sized one as a very small one.
const minRadius = 0.01

// radialFill returns the fill of the radial gradient g over a w by h box
// at the origin (CSS Images 3, 3.2): circles, or ellipses as circles
// scaled vertically about the centre.
func (e *engine) radialFill(g *css.Gradient, w, h, y float64) (*compose.Fill, bool) {
	cx, cy := resolve(g.Center[0], w), resolve(g.Center[1], h)
	l, r, t, b := math.Abs(cx), math.Abs(w-cx), math.Abs(cy), math.Abs(h-cy)
	corners := [4][2]float64{{l, t}, {r, t}, {l, b}, {r, b}}
	corner := func(far bool) (float64, float64) {
		best := corners[0]
		for _, c := range corners[1:] {
			d, bd := math.Hypot(c[0], c[1]), math.Hypot(best[0], best[1])
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
			rx = math.Hypot(dx, dy)
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
			rx = math.Sqrt(dx*dx + k*k*dy*dy)
			ry = rx / k
		}
	case css.RadialExplicit:
		rx, ry = resolve(g.Radius[0], w), resolve(g.Radius[1], h)
		if g.Circle {
			ry = rx
		}
	}
	if !(rx > 0) || !(ry > 0) || math.IsInf(rx, 0) || math.IsInf(ry, 0) {
		rx, ry = minRadius, minRadius
	}
	k := ry / rx
	// The farthest corner in the circles' own space, where y is divided
	// by k, bounds the radii the box shows.
	reach := 0.0
	for _, c := range corners {
		reach = max(reach, math.Hypot(c[0], c[1]/k))
	}
	stops, lo, hi, solid, ok := e.colorStops(g, rx, 0, reach, true, y)
	if !ok {
		return flatFill(solid)
	}
	c := compose.Point{X: cx, Y: cy}
	return &compose.Fill{Shading: &compose.Shading{
		Kind: compose.Radial, From: c, To: c, FromRadius: lo, ToRadius: hi,
		Stops:     stops,
		Transform: compose.Matrix{A: 1, D: k, F: cy * (1 - k)},
	}}, true
}

// gradientTiles returns the paths painting the background gradient of st
// over the positioning area within the painting area clip: the gradient
// is an image without intrinsic size or ratio, so its size is the area's
// (CSS Backgrounds 3, 3.9), placed and repeated as an image is.
func (e *engine) gradientTiles(st *css.Style, area, clip compose.Rect, y float64) []*compose.Path {
	if !(area.Width > 0) || !(area.Height > 0) {
		return nil
	}
	sized := *st
	if bs := &sized.BackgroundSize; bs.Kind == css.SizeExplicit {
		// CSS Backgrounds 3, 3.9: an auto dimension of an image with
		// neither intrinsic size nor ratio is 100%.
		if bs.Width.Kind != css.Points && bs.Width.Kind != css.Percent {
			bs.Width = css.Dimension{Kind: css.Percent, Value: 1}
		}
		if bs.Height.Kind != css.Points && bs.Height.Kind != css.Percent {
			bs.Height = css.Dimension{Kind: css.Percent, Value: 1}
		}
	}
	tiles := e.tiles(&sized, &Picture{Width: area.Width, Height: area.Height}, area, clip, y)
	if len(tiles) == 0 {
		return nil
	}
	fill, ok := e.gradientFill(st.BackgroundGradient, tiles[0].Width, tiles[0].Height, y)
	if !ok {
		return nil
	}
	out := make([]*compose.Path, 0, len(tiles))
	for _, t := range tiles {
		var f *compose.Fill
		if fill.Shading != nil {
			sh := *fill.Shading
			m := sh.Transform
			if m == (compose.Matrix{}) {
				m = compose.Identity
			}
			m.E += t.X
			m.F += t.Y
			sh.Transform = m
			f = &compose.Fill{Shading: &sh}
		} else {
			f = &compose.Fill{Color: fill.Color}
		}
		out = append(out, &compose.Path{Ops: rectOps(t), Fill: f})
	}
	return out
}
