package layout

import (
	"errors"
	"fmt"
	"math"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/effect"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// maxPageBackdrops is how many backdrop filters one page draws: 32.
// Anti-DoS guard, not spec-mandated: each draws again what the page
// painted behind its element, so a page of thousands would cost
// thousands of renders; designs filter a few panels. Past it an element
// is printed with its backdrop unfiltered, with a warning.
const maxPageBackdrops = 32

// backdropFx is the backdrop filter of an element, held by the group of
// its rendering (Filter Effects 2, 2.1).
type backdropFx struct {
	// filter is the backdrop-filter list.
	filter []css.Filter
	// box is the element's border box and edge its outline with the
	// border radii, in the coordinates of the group's content.
	box  typeset.Rect
	edge *typeset.Path
	// zoom is how much the groups around the element enlarge it on the
	// page, as [groupFx] zoom.
	zoom float64
	// blend is the element's blend mode: its rendering blends with its
	// filtered backdrop, not with the page.
	blend typeset.BlendMode
}

// backdropCost is what a [backdropFx] holds, its edge aside: 96 bytes.
const backdropCost = 96

// backdropLevel is one group the display list is being distributed or
// flattened through, from the outermost: the content it holds so far
// is what it painted before the group being drawn.
type backdropLevel struct {
	// it is the group, nil for a level that only moves its content.
	it *ditem
	// pages holds a distributed group's content by page, flat a
	// flattened one's; flow reports the display list's root, whose
	// content is in flow coordinates.
	pages map[int][]typeset.Fragment
	flat  *[]typeset.Fragment
	flow  bool
	// move is the transform of a level without a group.
	move typeset.Matrix
}

// transform returns the matrix that maps the level's content to its
// parent's.
func (l *backdropLevel) transform() typeset.Matrix {
	if l.it == nil {
		return l.move
	}
	return groupTransform(l.it)
}

// content returns what the level holds on page p.
func (l *backdropLevel) content(p int) []typeset.Fragment {
	if l.flat != nil {
		return *l.flat
	}
	return l.pages[p]
}

// backdropRoot reports whether b forms a Backdrop Root (Filter Effects 2,
// 3): a filter, an opacity below 1, a mask, a clip-path, a backdrop
// filter or a blend mode. Transforms, z-index and fixed or sticky
// positions do not. The root element forms one too: it is the bottom of
// every walk ([engine.backdropImage]).
func (b *box) backdropRoot() bool {
	st := b.st
	return len(st.Filter) > 0 || st.Opacity < 1 || len(st.Mask.Images) > 0 || !st.ClipPath.None() ||
		len(st.BackdropFilter) > 0 || st.MixBlendMode != css.BlendNormal
}

// backdropOf returns the backdrop filter of b to draw, nil for none. An
// inline box is left out with a warning: litho draws a non-atomic
// inline's effects as one group of its fragments ([effectInline]), and
// has no border box to clip one backdrop to. The root element has no
// Backdrop Root around it, so there is nothing behind it to filter
// (Filter Effects 2, 3).
func (p *painter) backdropOf(b *box) *backdropFx {
	if b.parent == nil || p.capture != nil || p.into != nil {
		return nil
	}
	if b.kind == kindInline {
		p.e.warnOnce(warn.CSSUnsupported, warn.Approximated, "backdrop-filter on an inline box is printed without filtering the backdrop", b.y+p.dy)
		return nil
	}
	r := p.borderRect(b)
	return &backdropFx{
		filter: b.st.BackdropFilter,
		box:    r,
		edge:   &typeset.Path{Ops: roundedRect(r, radiiOf(b, r.Width, r.Height)), Fill: &typeset.Fill{}},
		zoom:   1,
	}
}

// pushBackdropLevel opens a level of the walk the backdrop filters of a
// document read ([engine.backdropped]); a document without one keeps no
// levels.
func (e *engine) pushBackdropLevel(l backdropLevel) {
	if e.backdrops {
		e.backdropLevels = append(e.backdropLevels, l)
	}
}

// popBackdropLevel closes the innermost level.
func (e *engine) popBackdropLevel() {
	if e.backdrops {
		e.backdropLevels = e.backdropLevels[:len(e.backdropLevels)-1]
	}
}

// isBackdropMark reports the empty group an element's backdrop group
// starts with ([painter.unit]): it spans the element's box, so the group
// reaches every page the box does, painted or not, and draws nothing.
func isBackdropMark(f typeset.Fragment) bool {
	g, ok := f.(*typeset.Group)
	return ok && g.Opacity == 0 && len(g.Content) == 0 && g.Clip == nil && g.Transparency == nil
}

// backdropped returns frags, the content of the group it on one page,
// under the element's filtered backdrop (Filter Effects 2, 2.1): the
// Backdrop Root Image, filtered by the element's backdrop-filter, clipped
// to its border box with its radii, drawn before the element's
// background, border and content. The group's own transform, effects and
// clip, and those around it, apply to both. The image is an artifact. A filter the element
// cannot draw leaves the backdrop unfiltered, with a warning.
//
// Chromium prints the element without filtering its backdrop, where its
// screen applies the filter: litho follows the clause, which the screen
// agrees with.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 prints an element of backdrop-filter:
//     blur(10px) and a translucent background over a gradient as the
//     gradient, then the element's background and text, with no bitmap
//     and no blur; its screenshot blurs the gradient inside the box.
func (e *engine) backdropped(it *ditem, frags []typeset.Fragment, page int) []typeset.Fragment {
	kept := frags[:0:0]
	for _, f := range frags {
		if !isBackdropMark(f) {
			kept = append(kept, f)
		}
	}
	bd := it.effects().backdrop
	if bd.blend != typeset.BlendNormal && len(kept) > 0 {
		// The element's rendering blends with its filtered backdrop as a
		// stacking context does with its backdrop (CSS Compositing 1, 3.2).
		g := &typeset.Group{Transform: typeset.Identity, Opacity: 1, Blend: bd.blend, Content: kept}
		if !oneObject(kept) {
			g.Transparency = &typeset.TransparencyGroup{Isolated: true}
		}
		kept = []typeset.Fragment{g}
	}
	if e.backdropCount == nil {
		e.backdropCount = map[int]int{}
	}
	if e.backdropCount[page] >= maxPageBackdrops {
		e.warnOnce(warn.LimitReached, warn.Approximated, fmt.Sprintf("a page of more than %d backdrop filters prints the others without filtering the backdrop", maxPageBackdrops), -1)
		return kept
	}
	e.backdropCount[page]++
	img, err := e.backdropImage(bd, page)
	switch {
	case err == nil:
	case errors.Is(err, errNoFilter):
		return kept
	case limits.Stops(err):
		e.fail(fmt.Errorf("drawing a backdrop filter: %w", err))
		return kept
	case errors.Is(err, errFilterUnsupported):
		e.warnOnce(warn.CSSUnsupported, warn.Approximated, fmt.Sprintf("an element's backdrop is printed unfiltered: %v", err), -1)
		return kept
	default:
		e.warnOnce(warn.ContentUnsupported, warn.Approximated, fmt.Sprintf("an element's backdrop is printed unfiltered: %v", err), -1)
		return kept
	}
	if img == nil {
		return kept
	}
	g := &typeset.Group{Transform: typeset.Identity, Clip: bd.edge, Opacity: 1, Content: []typeset.Fragment{img}}
	return append([]typeset.Fragment{g}, kept...)
}

// backdropImage draws the filtered backdrop of bd on the given page as
// an image over its border box, nil when nothing shows.
//
// The Backdrop Root Image is what the nearest enclosing Backdrop Root
// painted before the element (Filter Effects 2, 3): the levels of the
// walk from the innermost group marked a root ([groupFx] root), or from
// the display list's root, the root element, whose canvas and the white
// surface under it, which CSS Compositing 1, 8.4 lets a user agent put
// in the root group and the paper is, are painted first. The filtered
// backdrops of the elements around it are not part of it: each is drawn
// into its element's group after the content the element paints.
// Content that lies outside the box is left out of the drawing. On a
// page of the flow, the box is cut to the slice of the flow the page
// shows: an element split across pages filters each fragment's own
// backdrop, its edge at the cut (CSS Fragmentation 3, 5.4), and the part
// of a fixed or transformed element past the page is not drawn.
//
// The image is drawn in the element's own coordinates, so the clip and
// the mirror edge of a blur lie on its border box, as Filter Effects 2,
// 2.1 has them after the inverse of the transforms between the element
// and its Backdrop Root. The filter's lengths are taken in the page's
// space, where 2.1 applies the filter before that inverse: a blur(4px)
// on an element scaled by 1.5 spreads 4 pixels on the page. A blur reads
// past the box by mirroring it.
//
// Chromium's screen draws a backdrop filter in the element's own space,
// lengths scaled with the element: litho follows the clause.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156's screenshot of backdrop-filter:
//     blur(10px) on a box whose left 10px lie over black, white
//     beyond, reads 78 at the box's left edge, the mirror's 81 and not
//     the 40 duplicating the edge gives; a box overlapping a 2px black
//     line by its top edge reads 215 there, the mirror's 214.
//   - probe, html: an opacity below 1, a filter, a clip-path, a blend
//     mode or a backdrop filter on an ancestor stops Chromium's
//     screenshot from inverting what lies behind the ancestor under
//     backdrop-filter: invert(1); a transform, z-index or overflow:
//     hidden does not.
//   - probe, html: under backdrop-filter: invert(1) over a page that
//     paints no background, Chromium's screenshot draws black.
//   - probe, html: a box of backdrop-filter: blur(8px) inside one of
//     backdrop-filter: invert(1) and a background of 10% black draws,
//     in Chromium's screenshot, the outer box's inverted stripes under
//     another 10% black: its backdrop is the outer box's background,
//     not the outer box's filtered backdrop.
//   - probe, html: Chromium's screenshot attenuates stripes of 20px
//     under blur(4px) on an element scaled by 1.5 to 0.24 of their
//     contrast, a deviation of 6px on the screen, and those under
//     blur(8px) on one scaled by 0.5 to 0.51, of 4px.
func (e *engine) backdropImage(bd *backdropFx, page int) (*typeset.Image, error) {
	box := bd.box
	levels := e.backdropLevels
	if len(levels) == 0 {
		return nil, nil
	}
	top := len(levels) - 1
	root, ground := 0, true
	for i := top; i >= 0; i-- {
		if it := levels[i].it; it != nil && it.fx != nil && it.fx.root {
			root, ground = i, false
			break
		}
	}
	// local maps the element's coordinates to its Backdrop Root's; page
	// maps them to the page's, for the filter's lengths.
	local, onPage := typeset.Identity, typeset.Identity
	for i := range levels {
		m := levels[i].transform()
		onPage = mul(onPage, m)
		if i > root {
			local = mul(local, m)
		}
	}
	inv, ok := invertMatrix(local)
	if !ok {
		return nil, nil
	}
	if fromPage, ok := invertMatrix(onPage); ok && levels[0].flow && page >= 0 {
		on := boundsOf(onPage, box)
		shown := boundsOf(fromPage, typeset.Rect{X: on.X, Y: e.pageTop(page), Width: on.Width, Height: e.sliceH(page)})
		x0, y0 := max(box.X, shown.X), max(box.Y, shown.Y)
		x1, y1 := min(box.X+box.Width, shown.X+shown.Width), min(box.Y+box.Height, shown.Y+shown.Height)
		box = typeset.Rect{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}
	}
	if !(box.Width > 0) || !(box.Height > 0) {
		return nil, nil
	}
	fs, ok := pageFilters(bd.filter, onPage)
	if !ok {
		return nil, nil
	}
	refs, err := e.filterRefs(fs, box)
	if err != nil {
		return nil, err
	}

	// The tree the Backdrop Root painted, innermost level first, each
	// level's content culled to the box as that level sees it.
	ms := typeset.NewMeasurer(e.ctx)
	var inner []typeset.Fragment
	toLevel := typeset.Identity
	for i := top; i >= root; i-- {
		l := &levels[i]
		near := padRect(boundsOf(toLevel, box), 1)
		content := e.cullBackdrop(ms, l.content(page), near, 0)
		content = append(content, inner...)
		if i == root {
			if ground {
				lead := []typeset.Fragment{&typeset.Path{Ops: rectOps(padRect(near, 1)), Fill: &typeset.Fill{Color: typeset.Color{R: 1, G: 1, B: 1, A: 1}}}}
				if l.flow && e.backdropGround != nil && page >= 0 {
					lead = append(lead, e.backdropGround(page)...)
				}
				content = append(lead, content...)
			}
			inner = content
			break
		}
		if len(content) > 0 {
			var clip *typeset.Path
			if l.it != nil {
				clip = l.it.effects().clip
			}
			inner = []typeset.Fragment{&typeset.Group{Transform: l.transform(), Clip: clip, Opacity: 1, Content: content}}
		} else {
			inner = nil
		}
		toLevel = mul(l.transform(), toLevel)
	}
	if err := e.ctx.Err(); err != nil {
		return nil, err
	}
	if len(inner) == 0 {
		return nil, nil
	}

	_, sigma := filterReach(fs)
	x0, y0 := float64(math.Floor(box.X/css.Pt)*css.Pt), float64(math.Floor(box.Y/css.Pt)*css.Pt)
	x1, y1 := float64(math.Ceil((box.X+box.Width)/css.Pt)*css.Pt), float64(math.Ceil((box.Y+box.Height)/css.Pt)*css.Pt)
	zoom := max(bd.zoom, 1.0/64)
	res, ok := effect.Plan(float64(sigma*zoom), float64((x1-x0)*zoom), float64((y1-y0)*zoom))
	if !ok {
		return nil, nil
	}
	res.Scale = float64(res.Scale * zoom)
	if res.Reduced {
		e.warnOnce(warn.LimitReached, warn.Approximated, fmt.Sprintf("a backdrop filter past %d pixels is drawn at a lower resolution", effect.MaxPixels), -1)
	}
	res = cssPixelGrid(res, x1-x0, y1-y0)
	if e.filterPixels == nil {
		e.filterPixels = map[int]*effect.Page{}
	}
	pixels := e.filterPixels[page]
	if pixels == nil {
		pixels = &effect.Page{}
		e.filterPixels[page] = pixels
	}
	if !pixels.Take(res) {
		return nil, fmt.Errorf("%w: past %d pixels of filters", errFilterUnsupported, effect.MaxPagePixels)
	}
	grid := effect.Grid{X: x0, Y: y0, Scale: res.Scale}
	ss, err := effect.Paint(e.ctx, []effect.Content{{Fragments: inner, Transform: inv, Grid: grid, W: res.W, H: res.H}})
	if err != nil {
		return nil, err
	}
	s := ss[0]
	defer func() { s.Release() }()
	for i, f := range fs {
		var next *effect.Surface
		var err error
		if f.Kind == css.FilterURL {
			next, err = refs[i].Apply(e.ctx, s)
		} else {
			next, err = applyFilter(e.ctx, s, f, effect.EdgeMirror)
		}
		if err != nil {
			return nil, err
		}
		if next != s {
			s.Release()
			s = next
		}
	}
	return effect.Encode(e.ctx, s)
}

// pageFilters returns fs with its lengths, given on the page, taken into
// the coordinates m maps to the page: a blur's deviation and a drop
// shadow's divided by m's scale, the square root of its determinant, and
// a drop shadow's offset mapped back through m. false for a singular m.
func pageFilters(fs []css.Filter, m typeset.Matrix) ([]css.Filter, bool) {
	lin := typeset.Matrix{A: m.A, B: m.B, C: m.C, D: m.D}
	if lin == (typeset.Matrix{A: 1, D: 1}) {
		return fs, true
	}
	inv, ok := invertMatrix(lin)
	if !ok {
		return nil, false
	}
	k := math.Sqrt(math.Abs(float64(m.A*m.D) - float64(m.B*m.C)))
	out := make([]css.Filter, len(fs))
	for i, f := range fs {
		switch f.Kind { //nolint:exhaustive // the colour functions have no length
		case css.FilterBlur:
			f.Amount = clamp(f.Amount) / k
		case css.FilterDropShadow:
			x, y := clamp(f.Shadow.X), clamp(f.Shadow.Y)
			f.Shadow.X = float64(inv.A*x) + float64(inv.C*y)
			f.Shadow.Y = float64(inv.B*x) + float64(inv.D*y)
			f.Shadow.Blur = clamp(f.Shadow.Blur) / k
		}
		out[i] = f
	}
	return out, true
}

// invertMatrix returns the inverse of m, false for a singular one.
func invertMatrix(m typeset.Matrix) (typeset.Matrix, bool) {
	det := float64(m.A*m.D) - float64(m.B*m.C)
	if !(math.Abs(det) > 1e-12) || math.IsInf(det, 0) {
		return typeset.Identity, false
	}
	return typeset.Matrix{
		A: m.D / det, B: -m.B / det, C: -m.C / det, D: m.A / det,
		E: (float64(m.C*m.F) - float64(m.D*m.E)) / det,
		F: (float64(m.B*m.E) - float64(m.A*m.F)) / det,
	}, true
}

// boundsOf returns the bounding box of r mapped by m.
func boundsOf(m typeset.Matrix, r typeset.Rect) typeset.Rect {
	x0, y0 := math.Inf(1), math.Inf(1)
	x1, y1 := math.Inf(-1), math.Inf(-1)
	for _, c := range [4][2]float64{{r.X, r.Y}, {r.X + r.Width, r.Y}, {r.X, r.Y + r.Height}, {r.X + r.Width, r.Y + r.Height}} {
		x := float64(m.A*c[0]) + float64(m.C*c[1]) + m.E
		y := float64(m.B*c[0]) + float64(m.D*c[1]) + m.F
		x0, y0, x1, y1 = min(x0, x), min(y0, y), max(x1, x), max(y1, y)
	}
	return typeset.Rect{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}
}

// padRect returns r grown by d on every side.
func padRect(r typeset.Rect, d float64) typeset.Rect {
	return typeset.Rect{X: r.X - d, Y: r.Y - d, Width: r.Width + 2*d, Height: r.Height + 2*d}
}

// meets reports whether a and b overlap.
func meets(a, b typeset.Rect) bool {
	return a.X < b.X+b.Width && b.X < a.X+a.Width && a.Y < b.Y+b.Height && b.Y < a.Y+a.Height
}

// cullBackdrop returns the fragments of frags that may paint inside r,
// groups rebuilt around the part of their content that does: what lies
// outside the box adds nothing to its backdrop, and a page's content is
// drawn again for each box only where the box is. It checks the
// context every 256 fragments, returning what it kept when it ends.
func (e *engine) cullBackdrop(ms *typeset.Measurer, frags []typeset.Fragment, r typeset.Rect, depth int) []typeset.Fragment {
	var out []typeset.Fragment
	for i, f := range frags {
		if i%256 == 255 && e.ctx.Err() != nil {
			return out
		}
		if g, ok := f.(*typeset.Group); ok && depth < maxGroupDepth {
			m := g.Transform
			if m == (typeset.Matrix{}) {
				m = typeset.Identity
			}
			if inv, ok := invertMatrix(m); ok {
				kids := e.cullBackdrop(ms, g.Content, boundsOf(inv, r), depth+1)
				switch {
				case len(kids) == 0:
				case len(kids) == len(g.Content):
					out = append(out, g)
				default:
					c := *g
					c.Content = kids
					out = append(out, &c)
				}
				continue
			}
		}
		if b, ok := ms.Bounds([]typeset.Fragment{f}); ok && meets(padRect(b, 1), r) {
			out = append(out, f)
		}
	}
	return out
}

// groundOf returns the function that gives what the root element paints
// under its content on page k, in flow coordinates: the canvas, which
// its background paints (CSS 2.1, 14.2), each page's drawn once.
func (e *engine) groundOf(canvas, rb *box, fitH float64) func(int) []typeset.Fragment {
	drawn := map[int][]typeset.Fragment{}
	return func(k int) []typeset.Fragment {
		if g, ok := drawn[k]; ok {
			return g
		}
		pb := e.assembledBox(k, fitH)
		pg := &typeset.Page{}
		e.paintCanvas(pg, canvas, rb, pb, k)
		var g []typeset.Fragment
		if len(pg.Content) > 0 {
			flow := typeset.Matrix{A: 1, D: 1, E: -pb.Margin[css.Left], F: e.pageTop(k) - e.noteShift(k) - pb.Margin[css.Top]}
			g = []typeset.Fragment{&typeset.Group{Transform: flow, Opacity: 1, Content: pg.Content}}
		}
		drawn[k] = g
		return g
	}
}
