package layout

import (
	"math"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// This file runs the grid track sizing algorithm (CSS Grid 1, 11.3 to
// 11.8) in one axis: initialising each track's base size and growth limit,
// resolving the intrinsic track sizes from the items' contributions in
// the spec's order (single spans and growing spans, then the items that
// cross flexible tracks), maximising the tracks, expanding the flexible
// ones with the size of an fr, and stretching the auto ones.

// inf is an infinite growth limit.
var inf = math.Inf(1)

// sizing is the constraint a grid is sized under in one axis (CSS Grid 1,
// 11.1): none, its available space being definite or indefinite, or a
// min-content or max-content constraint, for its intrinsic sizes.
type sizing uint8

const (
	sizeAvail sizing = iota
	sizeMin
	sizeMax
)

// gtrack is one grid track being sized.
type gtrack struct {
	min, max css.TrackBreadth
	// fit reports a fit-content() track, fitArg its argument.
	fit    bool
	fitArg float64
	// base and limit are the base size and growth limit, limit infinite
	// until an item gives it a size (CSS Grid 1, 11.4); growable marks
	// an infinitely growable track (11.5.1).
	base, limit float64
	growable    bool
	planned     float64
	// collapsed reports an empty auto-fit track (CSS Grid 1, 7.2.3.2).
	collapsed bool
	// start is the track's offset from the content box's start edge,
	// once placed.
	start float64
}

// flexible reports a flexible track; a collapsed one has none (CSS Grid
// 1, 7.2.3.2).
func (t *gtrack) flexible() bool { return t.max.Kind == css.TrackFlex && !t.collapsed }

func (t *gtrack) intrinsicMin() bool {
	return t.min.Kind == css.TrackAuto || t.min.Kind == css.TrackMinContent || t.min.Kind == css.TrackMaxContent
}

func (t *gtrack) intrinsicMax() bool {
	return t.fit || t.max.Kind == css.TrackAuto || t.max.Kind == css.TrackMinContent || t.max.Kind == css.TrackMaxContent
}

// maxContentMax reports a max-content maximum: max-content, auto, or a
// fit-content() track below its argument.
func (t *gtrack) maxContentMax() bool {
	return t.fit || t.max.Kind == css.TrackMaxContent || t.max.Kind == css.TrackAuto
}

// newTrack returns the track of size s in an axis of definite size ref
// when def; a percentage of an indefinite size counts as auto (CSS Grid
// 1, 7.2.1).
func newTrack(s css.TrackSize, ref float64, def bool) *gtrack {
	t := &gtrack{min: s.Min, max: s.Max, fit: s.FitContent}
	pct := func(b css.TrackBreadth) css.TrackBreadth {
		if b.Kind != css.TrackPercent {
			return b
		}
		if !def {
			return css.TrackBreadth{Kind: css.TrackAuto}
		}
		return css.TrackBreadth{Kind: css.TrackLength, Value: b.Value * ref}
	}
	t.min = pct(t.min)
	if t.fit {
		if fixedBreadth(t.max) && (t.max.Kind != css.TrackPercent || def) {
			t.fitArg = breadthOf(t.max, ref)
		} else {
			// fit-content() of a percentage of an indefinite size is
			// max-content, as Chromium resolves it.
			t.fit = false
			t.max = css.TrackBreadth{Kind: css.TrackMaxContent}
		}
		if t.fit {
			t.max = css.TrackBreadth{Kind: css.TrackAuto}
		}
	} else {
		t.max = pct(t.max)
	}
	if t.min.Kind == css.TrackFlex {
		t.min = css.TrackBreadth{Kind: css.TrackAuto}
	}
	return t
}

// initTrack sets the base size and growth limit of t (CSS Grid 1, 11.4).
func initTrack(t *gtrack) {
	t.base, t.limit, t.growable = 0, inf, false
	if t.min.Kind == css.TrackLength {
		t.base = max(t.min.Value, 0)
	}
	if t.max.Kind == css.TrackLength {
		t.limit = max(t.max.Value, 0)
	}
	if t.limit < t.base {
		t.limit = t.base
	}
}

// contribKind names an item contribution (CSS Grid 1, 11.5).
type contribKind uint8

const (
	contribMinimum contribKind = iota
	contribMinContent
	contribMaxContent
)

// gridSizer sizes the tracks of one axis.
type gridSizer struct {
	tracks []*gtrack
	gap    float64
	mode   sizing
	// contrib returns an item's outer contribution of a kind in this
	// axis.
	contrib func(it *gitem, k contribKind) float64
	ax      int
}

// spanSize returns the sum of the tracks [s, e) of f's sizes, with the
// gaps between them.
func (z *gridSizer) gaps(s, e int) float64 {
	n := 0
	for i := s; i < e; i++ {
		if !z.tracks[i].collapsed {
			n++
		}
	}
	return z.gap * float64(max(n-1, 0))
}

// spansFlex reports an item spanning a flexible track.
func (z *gridSizer) spansFlex(it *gitem) bool {
	for i := it.start[z.ax]; i < it.end[z.ax]; i++ {
		if z.tracks[i].flexible() {
			return true
		}
	}
	return false
}

// spansAutoMin reports an item spanning a track whose minimum is auto.
func (z *gridSizer) spansAutoMin(it *gitem) bool {
	for i := it.start[z.ax]; i < it.end[z.ax]; i++ {
		if z.tracks[i].min.Kind == css.TrackAuto {
			return true
		}
	}
	return false
}

// limited returns the limited min- or max-content contribution of it: its
// contribution capped by the sum of the fixed maximums of the tracks it
// spans when they all have one, floored by its minimum contribution (CSS
// Grid 1, 11.5).
func (z *gridSizer) limited(it *gitem, k contribKind) float64 {
	c := z.contrib(it, k)
	lim := z.gaps(it.start[z.ax], it.end[z.ax])
	for i := it.start[z.ax]; i < it.end[z.ax]; i++ {
		t := z.tracks[i]
		switch {
		case t.fit:
			lim += t.fitArg
		case t.max.Kind == css.TrackLength:
			lim += t.max.Value
		default:
			lim = inf
		}
	}
	return max(min(c, lim), z.contrib(it, contribMinimum))
}

// run sizes the tracks for the items in an axis of available size avail,
// definite when def (CSS Grid 1, 11.3, steps 1 to 5).
func (z *gridSizer) run(items []*gitem, avail float64, def bool, minSize, maxSize float64, stretch bool) {
	for _, t := range z.tracks {
		initTrack(t)
		if t.collapsed {
			t.base, t.limit = 0, 0
		}
	}
	z.intrinsic(items)
	z.maximize(avail, def)
	z.flexible(items, avail, def, minSize, maxSize)
	if stretch {
		z.stretchAuto(avail, def, minSize)
	}
}

// intrinsic resolves the intrinsic track sizes (CSS Grid 1, 11.5).
func (z *gridSizer) intrinsic(items []*gitem) {
	var plain, flex []*gitem
	for _, it := range items {
		if z.spansFlex(it) {
			flex = append(flex, it)
		} else {
			plain = append(plain, it)
		}
	}
	// Steps 2 and 3: the items that cross no flexible track, by span.
	slices.SortStableFunc(plain, func(a, b *gitem) int { return a.span(z.ax) - b.span(z.ax) })
	for i := 0; i < len(plain); {
		j := i
		for j < len(plain) && plain[j].span(z.ax) == plain[i].span(z.ax) {
			j++
		}
		z.spanGroup(plain[i:j], false)
		i = j
	}
	// Step 4: the items that cross flexible tracks, together, onto the
	// flexible tracks alone.
	if len(flex) > 0 {
		z.spanGroup(flex, true)
	}
	// Step 5: an infinite growth limit becomes the base size.
	for _, t := range z.tracks {
		if math.IsInf(t.limit, 1) {
			t.limit = t.base
		}
	}
}

// spanGroup distributes the contributions of one group of items (CSS
// Grid 1, 11.5, step 3), onto flexible tracks alone when flex.
func (z *gridSizer) spanGroup(group []*gitem, flex bool) {
	minFn := func(it *gitem) float64 {
		switch {
		case flex:
			// An item crossing flexible tracks contributes its minimum
			// alone, under any constraint, as Chromium sizes it.
		case z.mode == sizeMin && z.spansAutoMin(it):
			return z.limited(it, contribMinContent)
		case z.mode == sizeMax && z.spansAutoMin(it):
			return z.limited(it, contribMaxContent)
		}
		return z.contrib(it, contribMinimum)
	}
	eligible := func(t *gtrack) bool { return !flex || t.flexible() }
	// a. Intrinsic minimums.
	z.distribute(group, func(t *gtrack) bool { return eligible(t) && t.intrinsicMin() }, minFn, false, contribMinimum, flex)
	// b. Content-based minimums.
	z.distribute(group, func(t *gtrack) bool {
		return eligible(t) && (t.min.Kind == css.TrackMinContent || t.min.Kind == css.TrackMaxContent)
	}, func(it *gitem) float64 { return z.contrib(it, contribMinContent) }, false, contribMinContent, flex)
	// c. Max-content minimums.
	z.distribute(group, func(t *gtrack) bool {
		return eligible(t) && (t.min.Kind == css.TrackMaxContent || t.min.Kind == css.TrackAuto && z.mode == sizeMax)
	}, func(it *gitem) float64 { return z.contrib(it, contribMaxContent) }, false, contribMaxContent, flex)
	// d. A growth limit below the base size rises to it.
	for _, t := range z.tracks {
		if t.limit < t.base {
			t.limit = t.base
		}
	}
	if flex {
		return
	}
	// e. Intrinsic maximums.
	z.distribute(group, func(t *gtrack) bool { return t.intrinsicMax() }, func(it *gitem) float64 { return z.contrib(it, contribMinContent) }, true, contribMinContent, false)
	// f. Max-content maximums.
	z.distribute(group, func(t *gtrack) bool { return t.maxContentMax() }, func(it *gitem) float64 { return z.contrib(it, contribMaxContent) }, true, contribMaxContent, false)
}

// distribute distributes the extra space of each item of group over its
// affected tracks (CSS Grid 1, 11.5.1): base sizes, or growth limits when
// limits, the space being the item's contribution size beyond the tracks'
// affected sizes. kind is the contribution accommodated, which picks the
// tracks the space beyond their limits goes to; flex distributes by flex
// factor onto flexible tracks.
func (z *gridSizer) distribute(group []*gitem, affected func(*gtrack) bool, size func(*gitem) float64, limits bool, kind contribKind, flex bool) {
	for _, t := range z.tracks {
		t.planned = 0
	}
	grew := false
	wasInf := make([]bool, len(z.tracks))
	for i, t := range z.tracks {
		wasInf[i] = math.IsInf(t.limit, 1)
	}
	for _, it := range group {
		var aff []*gtrack
		used := z.gaps(it.start[z.ax], it.end[z.ax])
		for i := it.start[z.ax]; i < it.end[z.ax]; i++ {
			t := z.tracks[i]
			if limits && !math.IsInf(t.limit, 1) {
				used += t.limit
			} else {
				used += t.base
			}
			if affected(t) && !t.collapsed {
				aff = append(aff, t)
			}
		}
		if len(aff) == 0 {
			continue
		}
		space := size(it) - used
		if space <= eps {
			continue
		}
		cur := make([]float64, len(aff))
		lim := make([]float64, len(aff))
		for i, t := range aff {
			switch {
			case !limits:
				cur[i] = t.base
				lim[i] = t.limit
				if t.fit {
					lim[i] = min(lim[i], max(t.fitArg, t.base))
				}
			case math.IsInf(t.limit, 1):
				cur[i] = t.base
				lim[i] = inf
				if t.fit {
					lim[i] = max(t.fitArg, t.base)
				}
			default:
				cur[i] = t.limit
				lim[i] = t.limit
				if t.growable {
					lim[i] = inf
					if t.fit {
						lim[i] = max(t.fitArg, t.limit)
					}
				}
			}
		}
		var weights []float64
		if flex {
			weights = make([]float64, len(aff))
			sum := 0.0
			for i, t := range aff {
				weights[i] = t.max.Value
				sum += t.max.Value
			}
			if sum <= eps {
				weights = nil
			}
		}
		inc := make([]float64, len(aff))
		space = share(cur, lim, inc, space, nil, weights)
		if space > eps {
			// Beyond the limits: onto the tracks whose maximum fits the
			// contribution accommodated, else onto all of them.
			pick := make([]bool, len(aff))
			found := false
			for i, t := range aff {
				switch {
				case limits:
					// A fit-content() growth limit stays at its argument.
					pick[i] = !t.fit
				case kind == contribMaxContent:
					pick[i] = t.max.Kind == css.TrackMaxContent || t.max.Kind == css.TrackAuto && !t.fit ||
						t.fit && cur[i]+inc[i] < t.fitArg-eps
				default:
					pick[i] = t.intrinsicMax()
				}
				found = found || pick[i]
			}
			if !found && !limits {
				for i := range pick {
					pick[i] = true
				}
			}
			free := make([]float64, len(aff))
			for i := range free {
				free[i] = inf
				if !limits && aff[i].fit && kind == contribMaxContent {
					free[i] = max(aff[i].fitArg, cur[i]+inc[i])
				}
			}
			space = share(cur, free, inc, space, pick, weights)
			if space > eps && (found || !limits) {
				share(cur, slices.Repeat([]float64{inf}, len(aff)), inc, space, pick, weights)
			}
		}
		for i, t := range aff {
			t.planned = max(t.planned, inc[i])
		}
		grew = true
	}
	if !grew {
		return
	}
	for i, t := range z.tracks {
		if t.planned <= 0 {
			continue
		}
		switch {
		case !limits:
			t.base += t.planned
			if t.limit < t.base {
				t.limit = t.base
			}
		case math.IsInf(t.limit, 1):
			t.limit = t.base + t.planned
			if kind == contribMinContent && wasInf[i] {
				t.growable = true
			}
		default:
			t.limit += t.planned
		}
		t.planned = 0
	}
	if limits && kind == contribMaxContent {
		for _, t := range z.tracks {
			t.growable = false
		}
	}
}

// share distributes space over the tracks of current sizes cur and
// limits lim into their increases inc, equally (or by weights) among the
// eligible ones (pick, nil for all), freezing each at its limit, and
// returns the space left.
func share(cur, lim, inc []float64, space float64, pick []bool, weights []float64) float64 {
	frozen := make([]bool, len(cur))
	for i := range frozen {
		frozen[i] = pick != nil && !pick[i] || cur[i]+inc[i] >= lim[i]-eps
		if weights != nil && weights[i] <= 0 {
			frozen[i] = true
		}
	}
	for range len(cur) + 1 {
		if space <= eps {
			return 0
		}
		total := 0.0
		for i := range cur {
			if !frozen[i] {
				if weights != nil {
					total += weights[i]
				} else {
					total++
				}
			}
		}
		if total <= 0 {
			return space
		}
		// The largest step before a track reaches its limit.
		step := space / total
		for i := range cur {
			if frozen[i] {
				continue
			}
			w := 1.0
			if weights != nil {
				w = weights[i]
			}
			if room := lim[i] - cur[i] - inc[i]; room < step*w {
				step = room / w
			}
		}
		for i := range cur {
			if frozen[i] {
				continue
			}
			w := 1.0
			if weights != nil {
				w = weights[i]
			}
			inc[i] += step * w
			space -= step * w
			if cur[i]+inc[i] >= lim[i]-eps {
				frozen[i] = true
			}
		}
	}
	return max(space, 0)
}

// sum returns the base sizes of the tracks and the gaps between them.
func (z *gridSizer) sum() float64 {
	s := z.gaps(0, len(z.tracks))
	for _, t := range z.tracks {
		s += t.base
	}
	return s
}

// maximize grows the base sizes to the growth limits within the free
// space (CSS Grid 1, 11.6): a definite available space shares out equally,
// an indefinite one or a max-content constraint is infinite.
func (z *gridSizer) maximize(avail float64, def bool) {
	if z.mode == sizeMin {
		return
	}
	if !def || z.mode == sizeMax {
		for _, t := range z.tracks {
			t.base = max(t.base, t.limit)
		}
		return
	}
	free := avail - z.sum()
	if free <= eps {
		return
	}
	cur := make([]float64, len(z.tracks))
	lim := make([]float64, len(z.tracks))
	inc := make([]float64, len(z.tracks))
	for i, t := range z.tracks {
		cur[i], lim[i] = t.base, t.limit
	}
	share(cur, lim, inc, free, nil, nil)
	for i, t := range z.tracks {
		t.base += inc[i]
	}
}

// findFr returns the size of an fr for the tracks [s, e) filling space
// (CSS Grid 1, 11.7.1): the leftover after the inflexible tracks, shared
// by flex factor, a flexible track whose share is below its base size
// counting as inflexible.
func (z *gridSizer) findFr(s, e int, space float64) float64 {
	inflex := make([]bool, len(z.tracks))
	for range e - s + 1 {
		left := space - z.gaps(s, e)
		flex := 0.0
		for i := s; i < e; i++ {
			t := z.tracks[i]
			if t.flexible() && !inflex[i] {
				flex += t.max.Value
			} else {
				left -= t.base
			}
		}
		if flex < 1 {
			flex = 1
		}
		fr := max(left, 0) / flex
		again := false
		for i := s; i < e; i++ {
			t := z.tracks[i]
			if t.flexible() && !inflex[i] && fr*t.max.Value < t.base-eps {
				inflex[i] = true
				again = true
			}
		}
		if !again {
			return fr
		}
	}
	return 0
}

// flexible expands the flexible tracks (CSS Grid 1, 11.7).
func (z *gridSizer) flexible(items []*gitem, avail float64, def bool, minSize, maxSize float64) {
	hasFlex := false
	for _, t := range z.tracks {
		hasFlex = hasFlex || t.flexible()
	}
	if !hasFlex || z.mode == sizeMin {
		return
	}
	var fr float64
	if def && z.mode == sizeAvail {
		if avail-z.sum() <= 0 && z.findFr(0, len(z.tracks), avail) <= 0 {
			return
		}
		fr = z.findFr(0, len(z.tracks), avail)
	} else {
		for _, t := range z.tracks {
			if t.flexible() {
				if t.max.Value > 1 {
					fr = max(fr, t.base/t.max.Value)
				} else {
					fr = max(fr, t.base)
				}
			}
		}
		for _, it := range items {
			if z.spansFlex(it) {
				fr = max(fr, z.findFr(it.start[z.ax], it.end[z.ax], z.contrib(it, contribMaxContent)))
			}
		}
		// The grid's size at that fr must respect its minimum and maximum
		// sizes: past them, the fr fills the bound instead.
		total := z.gaps(0, len(z.tracks))
		for _, t := range z.tracks {
			if t.flexible() {
				total += max(t.base, fr*t.max.Value)
			} else {
				total += t.base
			}
		}
		switch {
		case total < minSize-eps:
			fr = z.findFr(0, len(z.tracks), minSize)
		case total > maxSize+eps:
			fr = z.findFr(0, len(z.tracks), maxSize)
		}
	}
	for _, t := range z.tracks {
		if t.flexible() && fr*t.max.Value > t.base {
			t.base = fr * t.max.Value
		}
	}
}

// stretchAuto shares the positive free space equally among the tracks
// whose maximum is auto (CSS Grid 1, 11.8), against the minimum size when
// the available space is indefinite.
func (z *gridSizer) stretchAuto(avail float64, def bool, minSize float64) {
	if z.mode != sizeAvail {
		return
	}
	ref := avail
	if !def {
		ref = minSize
	}
	free := ref - z.sum()
	if free <= eps {
		return
	}
	n := 0
	for _, t := range z.tracks {
		if t.max.Kind == css.TrackAuto && !t.fit && !t.collapsed {
			n++
		}
	}
	if n == 0 {
		return
	}
	for _, t := range z.tracks {
		if t.max.Kind == css.TrackAuto && !t.fit && !t.collapsed {
			t.base += free / float64(n)
		}
	}
}
