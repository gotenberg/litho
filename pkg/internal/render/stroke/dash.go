package stroke

import (
	"fmt"
	"math"

	"github.com/gotenberg/litho/pkg/internal/render/geom"
)

// maxDashes caps the dashes one path is cut into. Anti-DoS guard, not
// spec-mandated: a pattern of tiny lengths along a long path would otherwise
// produce an unbounded count. A million dashes is a dash every millimetre
// along a kilometre of line, far denser than any page shows.
const maxDashes = 1 << 20

// dashPattern is a validated dash array (ISO 32000-1, 8.4.3.6) with an even
// number of elements, the first of each pair "on".
type dashPattern struct {
	lengths []float64
	phase   float64
}

// newDashPattern validates a dash array and phase and reports false when the
// stroke is solid. Per 8.4.3.6 an empty array is solid, and an odd-length
// array repeats, so its elements alternate between on and off from one
// repetition to the next.
//
// 8.4.3.6 requires elements that are nonnegative and not all zero. An array
// that breaks this strokes solid: an all-zero array has no off part to
// leave, and pdf.js, whose canvas setLineDash ignores an array holding a
// negative or non-finite length, draws such a stroke solid. A non-finite
// phase counts as zero.
func newDashPattern(arr []float64, phase float64) (dashPattern, bool) {
	if len(arr) == 0 {
		return dashPattern{}, false
	}
	total := 0.0
	for _, v := range arr {
		if !finite(v) || v < 0 {
			return dashPattern{}, false
		}
		total += v
	}
	if total == 0 || !finite(total) {
		return dashPattern{}, false
	}
	lengths := append([]float64(nil), arr...)
	if len(lengths)%2 == 1 {
		lengths = append(lengths, arr...)
		total *= 2
	}
	if !finite(phase) {
		phase = 0
	}
	phase = math.Mod(phase, total)
	if phase < 0 {
		phase += total
	}
	return dashPattern{lengths: lengths, phase: phase}, true
}

// start returns the element the pattern is in at its phase and the length
// left in it. A phase on the boundary between two elements starts the later
// one, except a zero phase, which starts the first element even when it has
// zero length, so a pattern beginning with a zero-length dash paints a dot at
// the start.
func (dp dashPattern) start() (idx int, rem float64) {
	ph := dp.phase
	// The phase is below the total, so the walk ends within one pass; the
	// bound only documents that.
	for range 2 * len(dp.lengths) {
		if ph <= 0 || ph < dp.lengths[idx] {
			break
		}
		ph -= dp.lengths[idx]
		idx = (idx + 1) % len(dp.lengths)
	}
	return idx, dp.lengths[idx] - max(ph, 0)
}

// apply cuts each piece into its dashes. Each piece restarts the pattern: the
// pieces are subpaths, and 8.4.3.6 applies the phase at the start of each.
// A dash spanning the start of a closed subpath stays two capped dashes.
func (dp dashPattern) apply(in []piece) ([]piece, error) {
	var out []piece
	toggles := 0
	for _, pc := range in {
		if len(pc.pts) == 1 {
			// A degenerate subpath has no length to lay the pattern along:
			// it paints (with round caps, 8.5.3.2) when the pattern starts
			// on.
			if idx, _ := dp.start(); idx%2 == 0 {
				out = append(out, pc)
			}
			continue
		}
		var err error
		out, err = dp.cut(pc, out, &toggles)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (dp dashPattern) cut(pc piece, out []piece, toggles *int) ([]piece, error) {
	idx, rem := dp.start()
	on := idx%2 == 0
	zero := on && dp.lengths[idx] == 0
	var cur piece
	if on {
		cur.add(pc.pts[0], false)
	}
	finish := func(d geom.Point) {
		cur.tangent = d
		dash := cur.dedupe()
		// A single point is a dash only when its element has zero length,
		// and then paints its two caps alone (8.4.3.6 caps both ends of
		// every dash); otherwise it is a dash that begins where the subpath
		// ends, and paints nothing.
		if len(dash.pts) > 1 || zero {
			out = append(out, dash)
		}
	}
	segs := len(pc.pts) - 1
	if pc.closed {
		segs++
	}
	var d geom.Point
	for i := range segs {
		a, b := pc.pts[i], pc.pts[(i+1)%len(pc.pts)]
		l := math.Hypot(b.X-a.X, b.Y-a.Y)
		d = geom.Point{X: (b.X - a.X) / l, Y: (b.Y - a.Y) / l}
		t := 0.0
		for rem <= l-t {
			*toggles++
			if *toggles > 2*maxDashes {
				return nil, fmt.Errorf("more than %d dashes: %w", maxDashes, ErrTooComplex)
			}
			t += rem
			p := b
			if t < l {
				p = geom.Point{X: a.X + d.X*t, Y: a.Y + d.Y*t}
			}
			if on {
				cur.add(p, false)
				finish(d)
			} else {
				cur = piece{}
				cur.add(p, false)
			}
			idx = (idx + 1) % len(dp.lengths)
			rem = dp.lengths[idx]
			on = !on
			zero = on && rem == 0
		}
		rem -= l - t
		if on {
			cur.add(b, pc.smooth[(i+1)%len(pc.pts)])
		}
	}
	if on {
		finish(d)
	}
	return out, nil
}
