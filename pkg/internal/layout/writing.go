package layout

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// orthoEligible reports a box that may root an orthogonal flow: a block
// container laid out by the flow formatter, a block, an inline block, a
// float, an absolutely positioned box or a table cell, not a table, a
// flex or grid container, a multi-column container or a list item, whose
// markers sit outside.
func orthoEligible(b *box) bool {
	if b.kind != kindBlock || b.flex != nil || b.grid != nil || isMulticol(b) || b.st.Display == css.ListItem {
		return false
	}
	return b.tpart == partNone || b.tpart == partCell
}

// wrapOrtho makes b, a box of a vertical writing mode in a horizontal
// flow, the root of an orthogonal flow (CSS Writing Modes 4, 7.3): its
// content moves into an anonymous block of the horizontal mode, which the
// layout turns a quarter turn into b's content box.
func (e *engine) wrapOrtho(b *box) {
	st := anonStyle(b.st)
	st.WritingMode = css.HorizontalTB
	if e.opts.BrowserRules {
		// The content of an orthogonal flow root is laid out in the
		// root's own block formatting context (CSS Writing Modes 4, 7.3):
		// no margin of it collapses through the anonymous block, whose
		// top-left corner the content turns about.
		st.Display = css.FlowRoot
	}
	in := &box{
		st: st, kind: kindBlock, anon: true, first: true, last: true, parent: b,
		lang: b.lang, elem: b.elem, link: b.link, depth: b.depth + 1,
	}
	e.boxes++
	in.kids = b.kids
	for _, k := range in.kids {
		k.parent = in
	}
	e.normalize(in)
	b.kids = []*box{in}
	b.inlineKids = false
	b.ortho = true
}

// turner reports the anonymous block of an orthogonal flow root
// ([engine.wrapOrtho]): the box whose transform turns the flow's
// horizontal layout onto the frame of the root. It is a device of the
// layout, not a box CSS knows: it holds no positioned descendant.
func (b *box) turner() bool { return b.anon && b.parent != nil && b.parent.ortho }

// frameTurn returns the matrix that maps the coordinates b is laid out in
// onto the engine's own, the frame its pages are cut in: the turns of the
// orthogonal flows around b, innermost first, each about its turner's
// border box where the layout put it, relative offsets aside; false for a
// box laid out in the engine's frame, whose matrix is the identity.
func frameTurn(b *box) (typeset.Matrix, bool) {
	m, turned := typeset.Identity, false
	for a, depth := b.parent, 0; a != nil && depth <= maxTreeDepth; a, depth = a.parent, depth+1 {
		if !a.turner() {
			continue
		}
		if t, _, ok := transformOf(a, typeset.Rect{X: a.x, Y: a.y, Width: a.borderW(), Height: a.borderH()}); ok {
			m = mul(t, m)
			turned = true
		}
	}
	return m, turned
}

// toFrame returns the matrix that maps the coordinates the box from is laid
// out in onto those the box to is laid out in, nil from standing for the
// engine's frame, and false when both are laid out in the same frame.
func toFrame(from, to *box) (typeset.Matrix, bool) {
	if innerTurner(from) == innerTurner(to) {
		return typeset.Identity, false
	}
	fm := typeset.Identity
	if from != nil {
		fm, _ = frameTurn(from)
	}
	tm, _ := frameTurn(to)
	inv, ok := invertMatrix(tm)
	if !ok {
		return typeset.Identity, false
	}
	return mul(inv, fm), true
}

// applyPoint returns the point (x, y) mapped by m.
func applyPoint(m typeset.Matrix, x, y float64) (float64, float64) {
	return float64(m.A*x) + float64(m.C*y) + m.E, float64(m.B*x) + float64(m.D*y) + m.F
}

// innerTurner returns the turner of the orthogonal flow b is laid out in,
// nil for a box of the engine's frame.
func innerTurner(b *box) *box {
	if b == nil {
		return nil
	}
	for a, depth := b.parent, 0; a != nil && depth <= maxTreeDepth; a, depth = a.parent, depth+1 {
		if a.turner() {
			return a
		}
	}
	return nil
}

// orthoFormatter lays out the content of the root of an orthogonal flow:
// the anonymous block holding it is laid out horizontally, as wide as the
// root's content box is tall, then turned a quarter turn into the root's
// content box, whose height is that width (CSS Writing Modes 4, 7.3). The
// text reads top to bottom, lines stacking right to left, but in
// sideways-lr, where it reads bottom to top, lines stacking left to
// right. The content is monolithic: it does not break across pages.
//
// The inline size is the root's height when definite, else its table
// row's, else the content's fit-content width in the space
// [engine.orthoAvailable] gives (CSS Writing Modes 4, 7.3.1).
type orthoFormatter struct{}

func (orthoFormatter) layout(e *engine, b *box, cb cblock, _ *bfc, cursor float64, _ *marginState) float64 {
	in := b.kids[0]
	l := e.orthoInline(b, cb)
	hc := e.layoutOrtho(b, in, cb, cursor, l)
	w := cb.w
	if w <= 0 || e.contentBlockSize(b) {
		w = hc
		b.w = hc
	}
	in.st.TransformOrigin = [2]css.Dimension{css.Pts(0), css.Pts(0)}
	mode := b.st.WritingMode
	if mode == css.HorizontalTB {
		// A horizontal root inside a vertical flow turns its content back
		// the way the flow turned: a vertical-lr flow is its own
		// reflection; a flow turned one way is turned back the other.
		switch e.frameOf(b) { //nolint:exhaustive // the flows that turn other ways than clockwise alone
		case css.VerticalLR:
			mode = css.VerticalLR
		case css.SidewaysLR:
			mode = css.VerticalRL
		default:
			mode = css.SidewaysLR
		}
	}
	switch {
	case mode == css.VerticalLR && e.opts.BrowserRules:
		// Lines stacking left to right with text reading top to bottom
		// are the horizontal layout reflected across its diagonal: its
		// top on the left, its left on the top (see [transposed]).
		in.st.Transform = []css.TransformFunc{{Kind: css.TransformMatrix, V: [6]float64{0, 1, 1, 0, 0, 0}}}
	case mode == css.SidewaysLR:
		in.st.Transform = []css.TransformFunc{
			{Kind: css.TransformTranslate, X: css.Pts(0), Y: css.Pts(l)},
			{Kind: css.TransformRotate, V: [6]float64{-math.Pi / 2}},
		}
	default:
		in.st.Transform = []css.TransformFunc{
			{Kind: css.TransformTranslate, X: css.Pts(w), Y: css.Pts(0)},
			{Kind: css.TransformRotate, V: [6]float64{math.Pi / 2}},
		}
	}
	return cursor + l
}

// contentBlockSize reports an orthogonal flow root b whose width, its
// block size, is its content's under the browser rules: an in-flow block
// of width auto, whose block size is its content's height turned (CSS 2.1,
// 10.6.3, read in the root's writing mode; CSS Writing Modes 4, 7.3),
// where the Office converters fill the containing block.
func (e *engine) contentBlockSize(b *box) bool {
	return e.opts.BrowserRules && b.st.Width.Kind == css.Auto && b.float == css.FloatNone && !b.abs && !b.inline &&
		b.tpart == partNone && !b.flexItem && !b.gridItem
}

// intrinsic returns the width the content takes turned: its height laid
// out at the inline size.
func (orthoFormatter) intrinsic(e *engine, b *box) (float64, float64) {
	in := b.kids[0]
	l := e.orthoInline(b, cblock{})
	hc := e.layoutOrtho(b, in, cblock{}, 0, l)
	return hc, hc
}

// layoutOrtho lays out in, the anonymous block of the orthogonal flow
// root b, l wide at cursor in the root's content box cb, off the page
// cuts, and returns its height.
func (e *engine) layoutOrtho(b, in *box, cb cblock, cursor, l float64) float64 {
	in.st.Width = css.Pts(l)
	in.st.Transform = nil
	e.noPaginate++
	defer func() { e.noPaginate-- }()
	kcb := cblock{x: cb.x, w: l}
	if cb.w == 0 && cb.x == 0 {
		kcb.x = b.contentX()
	}
	e.layoutBlock(in, kcb, &bfc{}, cursor, &marginState{kept: true}, breakNone)
	return in.borderH()
}

// orthoInline returns the inline size of the orthogonal flow root b laid
// out in cb (see [orthoFormatter]).
func (e *engine) orthoInline(b *box, cb cblock) float64 {
	if cb.hDefinite {
		return max(cb.h, 0)
	}
	if h, ok := specHeight(b, b.st.Height, cblock{}); ok {
		return h
	}
	if b.tpart == partCell && b.parent != nil {
		if h, ok := specHeight(b.parent, b.parent.st.Height, cblock{}); ok {
			return max(h-vEdges(b), 0)
		}
	}
	lo, hi := formatterFor(b.kids[0]).intrinsic(e, b.kids[0])
	return min(max(lo, e.orthoAvailable(b)-vEdges(b)-b.m[css.Top]-b.m[css.Bottom]), hi)
}

// orthoAvailable returns the space an orthogonal flow root b whose
// containing block has no definite height fits its content in (CSS
// Writing Modes 4, 7.3.1): the smaller of the containing block's inner
// max-height, floored by its min-height, and the initial containing
// block's height, the page area's. The nearest scrollport is not counted.
// The root's margin box fits in it, as fit-content sizes a box (CSS
// Sizing 3, 5.2).
func (e *engine) orthoAvailable(b *box) float64 {
	avail := e.areaH(0)
	if p := b.parent; p != nil && !b.abs && b.float == css.FloatNone {
		if mx, ok := specHeight(p, p.st.MaxHeight, cblock{}); ok {
			if mn, ok := specMinHeight(p, p.st.MinHeight, cblock{}); ok {
				mx = max(mx, mn)
			}
			avail = min(avail, mx)
		}
	}
	return avail
}

// turnKey names the copy of a style turned into an orthogonal flow of a
// writing mode.
type turnKey struct {
	st    *css.Style
	frame css.WritingMode
}

// turnedStyle returns st as a box inside an orthogonal flow of the
// writing mode frame holds it, under [Options.BrowserRules]: the flow is
// laid out horizontally and turned onto the page, so the physical sides
// and axes of st are turned the other way. The physical properties keep
// their physical meaning (CSS Writing Modes 4, 6): margin-left stays on
// the left of the page whatever the writing mode. The flow-relative
// properties were mapped to physical ones by the cascade, and text-align,
// float and clear, whose left and right mean line-left and line-right
// (6.4), need no turn. The Office converters write the sides of turned
// text as the text's own, and keep st. A copy is made once a style and
// charged as a computed style.
func (e *engine) turnedStyle(st *css.Style, frame css.WritingMode) *css.Style {
	if !e.opts.BrowserRules || st == nil {
		return st
	}
	if t, ok := e.turned[turnKey{st, frame}]; ok {
		return t
	}
	if e.turned == nil {
		e.turned = map[turnKey]*css.Style{}
	}
	t := *st
	from := turnSides(frame)
	for i := css.Top; i <= css.Left; i++ {
		t.Margin[i] = st.Margin[from[i]]
		t.Padding[i] = st.Padding[from[i]]
		t.Border[i] = st.Border[from[i]]
		// A corner of the flow lies between two of its sides, the corner
		// of the page between the sides they come from; its radii swap
		// axes.
		r := st.BorderRadius[cornerFrom(from, css.Corner(i))]
		t.BorderRadius[i] = css.Radius{H: r.V, V: r.H}
	}
	phys := [4]css.Dimension{st.Top, st.Right, st.Bottom, st.Left}
	t.Top, t.Right, t.Bottom, t.Left = phys[from[css.Top]], phys[from[css.Right]], phys[from[css.Bottom]], phys[from[css.Left]]
	t.Width, t.Height = st.Height, st.Width
	t.MinWidth, t.MinHeight = st.MinHeight, st.MinWidth
	t.MaxWidth, t.MaxHeight = st.MaxHeight, st.MaxWidth
	t.MinWidthAuto, t.MinHeightAuto = st.MinHeightAuto, st.MinWidthAuto
	if c := st.Clip.Edges; st.Clip.Set && transposed(frame) {
		// clip: rect() offsets its edges from the top and left of the box
		// (CSS 2.1, 11.1.2), which a reflection across the diagonal
		// swaps: its top and left offsets name the flow's left and top,
		// its bottom and right ones the flow's right and bottom.
		t.Clip.Edges = [4]css.Dimension{c[css.Left], c[css.Bottom], c[css.Right], c[css.Top]}
	}
	if len(st.BoxShadow) > 0 {
		t.BoxShadow = make([]css.Shadow, len(st.BoxShadow))
		for i, sh := range st.BoxShadow {
			sh.X, sh.Y = turnOffset(frame, sh.X, sh.Y)
			t.BoxShadow[i] = sh
		}
	}
	e.turned[turnKey{st, frame}] = &t
	e.turned[turnKey{&t, frame}] = &t
	return &t
}

// turnSides returns, for each side of an orthogonal flow of the writing
// mode frame, the side of the page it lies on: the flow turned a quarter
// turn clockwise puts its top on the right of the page, its left on the
// top; turned counter-clockwise, in sideways-lr, its top on the left and
// its left on the bottom; reflected across its diagonal, in vertical-lr
// ([transposed]), its top on the left and its left on the top.
func turnSides(frame css.WritingMode) [4]css.Side {
	switch frame { //nolint:exhaustive // the flows that turn other ways than clockwise alone
	case css.SidewaysLR:
		return [4]css.Side{css.Left, css.Top, css.Right, css.Bottom}
	case css.VerticalLR:
		return [4]css.Side{css.Left, css.Bottom, css.Right, css.Top}
	}
	return [4]css.Side{css.Right, css.Bottom, css.Left, css.Top}
}

// transposed reports a flow of the writing mode frame laid out under the
// browser rules as a horizontal layout reflected across its diagonal onto
// the page: vertical-lr, whose lines stack left to right while its text
// reads top to bottom, which no turn of a horizontal layout gives. Its
// glyphs are reflected back ([typeset.Text.Mirror]) and its pictures
// stand upright.
func transposed(frame css.WritingMode) bool { return frame == css.VerticalLR }

// cornerFrom returns the corner of the page the corner c of a flow whose
// sides lie on the page sides from lies on.
func cornerFrom(from [4]css.Side, c css.Corner) css.Corner {
	// The corners in order follow the sides: corner c lies between side c
	// (top, right, bottom, left) and the side before it.
	a, b := from[c], from[(c+3)%4]
	for k := css.TopLeft; k <= css.BottomLeft; k++ {
		if s, before := css.Side(k), css.Side((k+3)%4); s == a && before == b || s == b && before == a {
			return k
		}
	}
	return c
}

// turnOffset returns the page offset (x, y) in the coordinates of an
// orthogonal flow of the writing mode frame.
func turnOffset(frame css.WritingMode, x, y float64) (float64, float64) {
	switch frame { //nolint:exhaustive // the flows that turn other ways than clockwise alone
	case css.SidewaysLR:
		return -y, x
	case css.VerticalLR:
		return y, x
	}
	return y, -x
}

// turnedPicture returns pic as a box of a turned flow sizes it: its
// natural width is the height it stands on the page.
func turnedPicture(pic *Picture) *Picture {
	t := *pic
	t.Width, t.Height = pic.Height, pic.Width
	t.NoNaturalWidth, t.NoNaturalHeight = pic.NoNaturalHeight, pic.NoNaturalWidth
	return &t
}

// uprightPicture returns the fragment that paints the picture of the
// replaced box b of a turned flow, whose content box is box, standing
// upright on the page (CSS Writing Modes 4, 7: a writing mode turns the
// text, not a replaced element's content), and where it lands: the
// picture is placed by object-fit and object-position in the content box
// as the page shows it, then turned the other way about its centre.
func (e *engine) uprightPicture(b *box, box typeset.Rect) (typeset.Fragment, typeset.Rect) {
	pic := turnedPicture(b.picture)
	cx, cy := box.X+float64(box.Width/2), box.Y+float64(box.Height/2)
	page := typeset.Rect{X: cx - float64(box.Height/2), Y: cy - float64(box.Width/2), Width: box.Height, Height: box.Width}
	r := objectRect(b.st, objectPicture(b.st, pic, page.Width, page.Height), page)
	f := e.pictureFrag(pic, r, b.elem)
	if f == nil {
		return nil, box
	}
	// The flow turns a quarter turn clockwise onto the page, or
	// counter-clockwise in sideways-lr: the picture turns the other way.
	g := &typeset.Group{Transform: uprightTurn(b.turn, cx, cy), Opacity: 1, Content: []typeset.Fragment{f}}
	return g, turnedRect(b.turn, cx, cy, r)
}

// frameOf returns the writing mode of the turned flow the box b is laid
// out in under the browser rules, HorizontalTB for a box on the page as
// it is.
func (e *engine) frameOf(b *box) css.WritingMode {
	if !e.opts.BrowserRules {
		return css.HorizontalTB
	}
	for a, depth := b.parent, 0; a != nil && depth <= maxTreeDepth; a, depth = a.parent, depth+1 {
		if a.ortho {
			return a.st.WritingMode
		}
	}
	return e.turn
}

// uprightTurn returns the transform that turns content drawn upright
// about the point (cx, cy) into a flow of the writing mode wm, which the
// page turns a quarter turn clockwise, or counter-clockwise in
// sideways-lr: the other way.
func uprightTurn(wm css.WritingMode, cx, cy float64) typeset.Matrix {
	if transposed(wm) {
		return typeset.Matrix{B: 1, C: 1, E: cx - cy, F: cy - cx}
	}
	if wm == css.SidewaysLR {
		return typeset.Matrix{B: 1, C: -1, E: cx + cy, F: cy - cx}
	}
	return typeset.Matrix{B: -1, C: 1, E: cx - cy, F: cy + cx}
}

// uprightRect returns the rectangle r of a flow of the writing mode wm in
// the coordinates [uprightTurn] turns from, about (cx, cy).
func uprightRect(wm css.WritingMode, cx, cy float64, r typeset.Rect) typeset.Rect {
	if transposed(wm) {
		return typeset.Rect{X: cx - cy + r.Y, Y: cy - cx + r.X, Width: r.Height, Height: r.Width}
	}
	if wm == css.SidewaysLR {
		return typeset.Rect{X: cx - cy + r.Y, Y: cy + cx - r.X - r.Width, Width: r.Height, Height: r.Width}
	}
	return typeset.Rect{X: cx + cy - r.Y - r.Height, Y: r.X - cx + cy, Width: r.Height, Height: r.Width}
}

// uprightLayer returns the fragment that paints the image pic of the
// background layer l over the slice sl of a box in a flow of the writing
// mode wm, standing upright on the page: the background's positioning
// area, size, position and repetition are physical (CSS Backgrounds 3,
// 3.6 to 3.9), so its tiles are placed in the area as the page shows it
// and turned the other way about the area's centre.
func (e *engine) uprightLayer(l *css.BackgroundLayer, pic *Picture, sl bgSlice, wm css.WritingMode, y float64) typeset.Fragment {
	cx, cy := sl.area.X+float64(sl.area.Width/2), sl.area.Y+float64(sl.area.Height/2)
	area, clip := uprightRect(wm, cx, cy, sl.area), uprightRect(wm, cx, cy, sl.clip)
	pic = naturalPicture(pic, area.Width, area.Height)
	tiles := e.tiles(l, pic, area, clip, y)
	var frags []typeset.Fragment
	for i, t := range tiles {
		f := e.pictureFrag(pic, t, nil)
		if f == nil {
			break
		}
		if i == 0 && pic.Vector != nil {
			tiles = e.vectorTiles(tiles, fragNodes(f), y)
		}
		frags = append(frags, f)
	}
	if len(frags) == 0 {
		return nil
	}
	return &typeset.Group{Transform: uprightTurn(wm, cx, cy), Opacity: 1, Content: frags}
}

// turnedRect returns the rectangle r drawn upright about (cx, cy) as
// [uprightTurn] places it in a flow of the writing mode wm: the inverse
// of [uprightRect].
func turnedRect(wm css.WritingMode, cx, cy float64, r typeset.Rect) typeset.Rect {
	if transposed(wm) {
		return uprightRect(wm, cx, cy, r)
	}
	if wm == css.SidewaysLR {
		return typeset.Rect{X: cx + cy - r.Y - r.Height, Y: r.X - cx + cy, Width: r.Height, Height: r.Width}
	}
	return typeset.Rect{X: r.Y - cy + cx, Y: cy + cx - r.X - r.Width, Width: r.Height, Height: r.Width}
}

// centralBaseline reports whether the inline box b aligns its content on
// the central baseline, the dominant baseline of a vertical line whose
// text stands upright (CSS Inline 3, 4.2.1, dominant-baseline: auto, in
// vertical typographic mode with text-orientation mixed or upright), and
// returns how far that baseline lies above the alphabetic one of its
// first available font, which a turned flow lays its lines out on
// ([engine.centralRise]).
func (e *engine) centralBaseline(b *box) (float64, bool) {
	if !e.centralLine(b) {
		return 0, false
	}
	return e.centralRise(e.faceMetrics(b.st, b.lang)), true
}

// centralLine reports whether the inline box b lays its line out on the
// central baseline: its text stands upright in a vertical line (CSS
// Inline 3, 4.2.1, dominant-baseline: auto).
func (e *engine) centralLine(b *box) bool {
	if b == nil {
		return false
	}
	st := b.st
	if b.anon && b.parent != nil && b.parent.ortho {
		// The anonymous block an orthogonal flow root lays its content
		// out in is horizontal in the turned flow: its lines are the
		// root's.
		st = b.parent.st
	}
	return e.orientation(st) != text.OrientNone
}

// centralRise returns how far the central baseline of a face of metrics
// m lies above its alphabetic baseline. CSS Inline 3, 4.1 puts the
// central baseline halfway between the ideographic-under and
// ideographic-over baselines, the middle of the em box; under the
// browser rules it is the middle of the face's ascent and descent, as
// Blink takes it for a vertical line (FontMetrics::IntAscentInternal,
// kCentral), the part above it the larger half of a content area of odd
// pixels ([engine.centralOver]).
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 sets the
//     sideways baseline of a vertical-rl line of Liberation Sans, DejaVu
//     Sans or Liberation Serif, 10 to 30 px, that also draws Noto Sans CJK
//     JP, the ascent less the larger half of the content area below the
//     line's central baseline, on all 63 lines.
func (e *engine) centralRise(m text.Metrics) float64 {
	if !e.opts.BrowserRules {
		return float64((m.EmAscent - m.EmDescent) / 2)
	}
	return m.Ascent - e.centralOver(m.Ascent+m.Descent)
}

// centralOver returns the part of a content area h tall that lies over
// the central baseline of a vertical line: half of it, and with pixel
// font metrics the larger half of a content area of odd pixels, as Blink
// splits a font's height on the central baseline (Height() - Height() /
// 2).
func (e *engine) centralOver(h float64) float64 {
	if !e.opts.PixelFontMetrics {
		return h / 2
	}
	px := math.Round(h / css.Pt)
	return float64((px - math.Floor(px/2)) * css.Pt)
}

// uprightShift returns how far the upright glyphs of a run laid out in a
// vertical line move across it, down in the turned flow, under the
// browser rules: the text layout centres them on the middle of the em
// box (CSS Inline 3, 4.1), Blink on the middle of their face's ascent
// and descent, in halves of a pixel (FontMetrics::FloatAscent,
// kCentral). ascent and descent are the face's, rounded as the line
// rounds them; emAscent and emDescent its em box split.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 centres an
//     upright glyph of Noto Sans CJK JP, 10 to 30 px, (a - d) / 2 above
//     the alphabetic baseline of its line, a and d the face's rounded
//     ascent and descent, alone or as the fallback face of Liberation
//     Sans, DejaVu Sans and Liberation Serif, on all 84 lines.
func (e *engine) uprightShift(ascent, descent, emAscent, emDescent float64) float64 {
	if !e.opts.BrowserRules {
		return 0
	}
	return float64((descent-ascent)/2) - float64((emDescent-emAscent)/2)
}

// mirrorRun reflects the glyph run t, laid out in a transposed flow
// ([transposed]) on a line turned over across its middle
// ([inlineState.overRight]), across its baseline: each glyph is reflected
// about it, and drawn reflected, so that the page's reflection draws it as
// the vertical-lr line sets it, its top on the line's over side, the right
// (CSS Writing Modes 4, 4.2). The glyph slice is copied: the run is shared
// with the line.
func mirrorRun(t *typeset.Text) {
	glyphs := make([]typeset.Glyph, len(t.Glyphs))
	for i, g := range t.Glyphs {
		g.Y = -g.Y
		glyphs[i] = g
	}
	t.Glyphs, t.Mirror = glyphs, true
}
