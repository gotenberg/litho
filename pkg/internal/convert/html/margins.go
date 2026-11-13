package html

import (
	"context"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// measureSide is the side of the page a measuring layout runs on: 14,400
// points, the PDF page limit, wider than any page-margin box.
const measureSide = maxPageSide

// maxSubLayoutPages caps the pages of the layout of a page's margin boxes,
// headers or footers: 16. Anti-DoS guard, not spec-mandated: only the
// first page is drawn, the rest is content overflowing the page, and the
// cap stops a box of runaway content from laying out page after page.
const maxSubLayoutPages = 16

// The page counters a page-margin box reads (CSS Page 3, 5; CSS Lists 3,
// 4.4.1, page-based counters).
const (
	pageCounter  = "page"
	pagesCounter = "pages"
)

// marginAxis is one dimension of a page-margin box: the used start and
// end margins, the used size of its content box, and its border and
// padding along the dimension.
type marginAxis struct {
	m1, size, m2, bp float64
}

// outer returns the outer size of the axis, margins included.
func (a marginAxis) outer() float64 { return a.m1 + a.bp + a.size + a.m2 }

// marginBox is a generated page-margin box being laid out.
type marginBox struct {
	which css.MarginBox
	st    *css.Style
	kids  []*layout.Node
	// cb is the containing block (CSS Page 3, 5.3.1), x and y the used
	// axes.
	cb   typeset.Rect
	x, y marginAxis
	// minW and maxW are the outer min- and max-content widths, contentH
	// the content's height at the used width.
	minW, maxW, contentH float64
}

// The page sides a page-margin box belongs to.
const (
	sideCorner = iota
	sideTop
	sideRight
	sideBottom
	sideLeft
)

// marginSide returns the side of b and its position along that side: 0,
// 1 or 2 for the start, middle and end boxes (A, B and C of CSS Page 3,
// 5.3.2), left to right and top to bottom.
func marginSide(b css.MarginBox) (int, int) {
	switch b { //nolint:exhaustive // the corners take the default
	case css.MarginTopLeft, css.MarginTopCenter, css.MarginTopRight:
		return sideTop, int(b - css.MarginTopLeft)
	case css.MarginBottomLeft, css.MarginBottomCenter, css.MarginBottomRight:
		return sideBottom, int(css.MarginBottomLeft - b)
	case css.MarginLeftTop, css.MarginLeftMiddle, css.MarginLeftBottom:
		return sideLeft, int(css.MarginLeftTop - b)
	case css.MarginRightTop, css.MarginRightMiddle, css.MarginRightBottom:
		return sideRight, int(b - css.MarginRightTop)
	}
	return sideCorner, 0
}

// containingBlock returns the containing block of the page-margin box b on
// the page box pb (CSS Page 3, 5.3.1): the intersection of two margins for
// a corner, the margin less the corners for the others.
func containingBlock(b css.MarginBox, pb layout.PageBox) typeset.Rect {
	w, h := pb.Width, pb.Height
	mt, mr, mb, ml := pb.Margin[css.Top], pb.Margin[css.Right], pb.Margin[css.Bottom], pb.Margin[css.Left]
	aw, ah := w-ml-mr, h-mt-mb
	switch b { //nolint:exhaustive // the side boxes follow their side
	case css.MarginTopLeftCorner:
		return typeset.Rect{Width: ml, Height: mt}
	case css.MarginTopRightCorner:
		return typeset.Rect{X: w - mr, Width: mr, Height: mt}
	case css.MarginBottomRightCorner:
		return typeset.Rect{X: w - mr, Y: h - mb, Width: mr, Height: mb}
	case css.MarginBottomLeftCorner:
		return typeset.Rect{Y: h - mb, Width: ml, Height: mb}
	}
	switch side, _ := marginSide(b); side {
	case sideTop:
		return typeset.Rect{X: ml, Width: aw, Height: mt}
	case sideBottom:
		return typeset.Rect{X: ml, Y: h - mb, Width: aw, Height: mb}
	case sideLeft:
		return typeset.Rect{Y: mt, Width: ml, Height: ah}
	}
	return typeset.Rect{X: w - mr, Y: mt, Width: mr, Height: ah}
}

// usedBorder returns the used width of a border: zero when its style
// draws none (CSS Backgrounds 3, 4.3).
func usedBorder(b css.Border) float64 {
	if b.Style == css.BorderNone || b.Style == css.BorderHidden {
		return 0
	}
	return b.Width
}

// edges returns the border and padding of st along one axis, start then
// end, percentages of ref.
func edges(st *css.Style, start, end css.Side, ref float64) float64 {
	return usedBorder(st.Border[start]) + usedBorder(st.Border[end]) + max(resolveDim(st.Padding[start], ref), 0) + max(resolveDim(st.Padding[end], ref), 0)
}

// resolveDim returns a length, a percentage of ref or a calculation of
// both, 0 otherwise.
func resolveDim(d css.Dimension, ref float64) float64 {
	switch d.Kind { //nolint:exhaustive // the other kinds have no length here
	case css.Points:
		return d.Value
	case css.Percent:
		return d.Value * ref
	case css.Calc:
		return d.Resolve(ref)
	}
	return 0
}

// contentSize returns the content size a definite size property asks for,
// the border and padding bp taken out under box-sizing: border-box, and
// false for auto.
func contentSize(st *css.Style, d css.Dimension, ref, bp float64) (float64, bool) {
	if !d.IsLength() {
		return 0, false
	}
	v := resolveDim(d, ref)
	if st.BoxSizing == css.BorderBox {
		v -= bp
	}
	return max(v, 0), true
}

// fixedAxis returns the used margins and size of a page-margin box in its
// fixed dimension, the one the page margin it sits in gives, of length l
// (CSS Page 3, 5.3.3). outerStart reports that the start margin is the
// one away from the page area, the one ignored when over-constrained.
func fixedAxis(st *css.Style, d css.Dimension, m1, m2 css.Dimension, bp, l, ref float64, outerStart bool) marginAxis {
	a := marginAxis{bp: bp}
	a1, a2 := m1.Kind == css.Auto, m2.Kind == css.Auto
	a.m1, a.m2 = resolveDim(m1, ref), resolveDim(m2, ref)
	size, definite := contentSize(st, d, ref, bp)
	sum := bp + a.m1 + a.m2
	if definite {
		sum += size
	}
	if sum > l {
		// Auto margins count as zero when the rest overflows.
		a1, a2 = false, false
	}
	if definite && !a1 && !a2 {
		// Over-constrained: the outer margin gives way.
		if outerStart {
			a1 = true
		} else {
			a2 = true
		}
	}
	switch {
	case !definite:
		a.size = max(l-bp-a.m1-a.m2, 0)
	case a1 && a2:
		a.size = size
		a.m1 = (l - bp - size) / 2
		a.m2 = a.m1
	case a1:
		a.size = size
		a.m1 = l - bp - size - a.m2
	case a2:
		a.size = size
		a.m2 = l - bp - size - a.m1
	default:
		a.size = size
	}
	return a
}

// sizing is one box of a side in the variable dimension: its outer min-
// and max-content sizes, and whether its size is auto.
type sizing struct {
	gen, auto        bool
	minO, maxO, used float64
}

// distribute shares the available length avail between two boxes whose
// sizes are auto, in proportion to their content (CSS Page 3, 5.3.2.2),
// and returns their used outer sizes.
func distribute(avail float64, x, y sizing) (float64, float64) {
	share := func(base1, base2, f1, f2 float64) (float64, float64) {
		flex := avail - base1 - base2
		if f1+f2 == 0 {
			f1, f2 = 1, 1
		}
		return base1 + flex*f1/(f1+f2), base2 + flex*f2/(f1+f2)
	}
	switch {
	case x.maxO+y.maxO < avail:
		return share(x.maxO, y.maxO, x.maxO, y.maxO)
	case x.minO+y.minO < avail:
		return share(x.minO, y.minO, x.maxO-x.minO, y.maxO-y.minO)
	}
	return share(x.minO, y.minO, x.minO, y.minO)
}

// variableSizes returns the used outer sizes of the start, middle and end
// boxes of a side, of available length avail (CSS Page 3, 5.3.2.2): the
// middle box stays centred, the space goes to the content.
func variableSizes(avail float64, s [3]sizing) [3]float64 {
	a, b, c := s[0], s[1], s[2]
	out := [3]float64{a.used, b.used, c.used}
	if !b.gen {
		switch {
		case a.auto && c.auto:
			out[0], out[2] = distribute(avail, a, c)
		case a.auto:
			out[0] = avail - c.used
		case c.auto:
			out[2] = avail - a.used
		}
		return out
	}
	if b.auto {
		ac := sizing{auto: true, minO: 2 * max(a.minO, c.minO), maxO: 2 * max(a.maxO, c.maxO)}
		out[1], _ = distribute(avail, b, ac)
	}
	if a.auto {
		out[0] = (avail - out[1]) / 2
	}
	if c.auto {
		out[2] = (avail - out[1]) / 2
	}
	return out
}

// marginContent returns the page-margin boxes of the page info names, one
// of pages, laid out on the page (CSS Page 3, 5), as artifacts: the
// layout.Options.MarginContent of the document.
func (p *prepared) marginContent(ctx context.Context, info layout.PageInfo, pages int) []typeset.Fragment {
	e := p.pages.entry(info)
	if len(e.style.MarginBoxes) == 0 {
		return nil
	}
	pb := p.setup.layoutBox(e.box)
	var boxes []*marginBox
	for which := range css.NumMarginBoxes {
		st := e.style.MarginBoxes[which]
		if st == nil {
			continue
		}
		mb := &marginBox{which: which, st: st, cb: containingBlock(which, pb)}
		mb.kids = p.marginNodes(ctx, st, info.Index, pages, pb)
		p.fixedAxes(mb)
		boxes = append(boxes, mb)
	}
	if err := p.measureVariable(ctx, boxes); err != nil {
		p.warnMargins(err)
		return nil
	}
	resolveSides(boxes)
	if err := p.measureHeights(ctx, boxes); err != nil {
		p.warnMargins(err)
		return nil
	}
	for _, mb := range boxes {
		p.warnOverflow(mb)
	}
	frags, links, err := p.paintMargins(ctx, boxes, pb)
	if err != nil {
		p.warnMargins(err)
		return nil
	}
	p.pages.links = links
	return frags
}

// overflowSlack is how far a page-margin box's content may pass its box
// before it counts as overflowing: a hundredth of a point, below what
// rounding the measured heights moves.
const overflowSlack = 0.01

// warnOverflow records a page-margin box whose content is taller than the
// box. The box's height is the page margin's less its own margins,
// borders and paddings, or its share of a side, and the page area keeps
// its size (CSS Page 3, 5.3.3): the content overflows the box, over the
// page area's content or off the page, as overflow: visible draws it.
func (p *prepared) warnOverflow(mb *marginBox) {
	if mb.contentH <= mb.y.size+overflowSlack {
		return
	}
	round := func(v float64) float64 { return math.Round(v*100) / 100 }
	p.warnOnce(warn.Approximated, fmt.Sprintf(
		"the content of the @%s page-margin box is %v points tall and the box %v points: it overflows the box",
		mb.which, round(mb.contentH), round(mb.y.size)))
}

// warnMargins records that the page-margin boxes could not be laid out.
func (p *prepared) warnMargins(err error) {
	p.warnOnce(warn.Omitted, fmt.Sprintf("the page-margin boxes were left out: %v", err))
}

// warnOnce records a page decoration warning the first time it is met.
func (p *prepared) warnOnce(effect warn.Effect, msg string) {
	if !p.pages.seen[msg] {
		p.pages.seen[msg] = true
		p.pages.warnings = append(p.pages.warnings, typeset.Warning{Code: warn.PageSetupUnsupported, Effect: effect, Message: msg})
	}
}

// fixedAxes resolves the dimensions of mb its page margin fixes: both for
// a corner, the height of a top or bottom box, the width of a left or
// right one (CSS Page 3, 5.3.3).
func (p *prepared) fixedAxes(mb *marginBox) {
	st, cb := mb.st, mb.cb
	side, _ := marginSide(mb.which)
	bpx := edges(st, css.Left, css.Right, cb.Width)
	bpy := edges(st, css.Top, css.Bottom, cb.Width)
	top := mb.which == css.MarginTopLeftCorner || mb.which == css.MarginTopRightCorner || side == sideTop
	left := mb.which == css.MarginTopLeftCorner || mb.which == css.MarginBottomLeftCorner || side == sideLeft
	if side == sideCorner || side == sideTop || side == sideBottom {
		mb.y = fixedAxis(st, st.Height, st.Margin[css.Top], st.Margin[css.Bottom], bpy, cb.Height, cb.Width, top)
	}
	if side == sideCorner || side == sideLeft || side == sideRight {
		mb.x = fixedAxis(st, st.Width, st.Margin[css.Left], st.Margin[css.Right], bpx, cb.Width, cb.Width, left)
	}
	if side == sideTop || side == sideBottom {
		mb.x = marginAxis{m1: resolveDim(st.Margin[css.Left], cb.Width), m2: resolveDim(st.Margin[css.Right], cb.Width), bp: bpx}
	}
	if side == sideLeft || side == sideRight {
		mb.y = marginAxis{m1: resolveDim(st.Margin[css.Top], cb.Width), m2: resolveDim(st.Margin[css.Bottom], cb.Width), bp: bpy}
	}
}

// resolveSides resolves the variable dimension of the boxes of each side
// and places them along it (CSS Page 3, 5.3.2.2 and 5.3.2.4).
func resolveSides(boxes []*marginBox) {
	var sides [5][3]*marginBox
	for _, mb := range boxes {
		if side, pos := marginSide(mb.which); side != sideCorner {
			sides[side][pos] = mb
		}
	}
	for side := sideTop; side <= sideLeft; side++ {
		horizontal := side == sideTop || side == sideBottom
		var s [3]sizing
		var avail float64
		// clamped holds the sizes min- and max-width or -height fixed, by
		// position (CSS Page 3, 5.3.2.3).
		var clamped [3]*float64
		var used [3]float64
		for range 3 {
			s = [3]sizing{}
			for i, mb := range sides[side] {
				if mb == nil {
					continue
				}
				ax, d, ref := &mb.y, mb.st.Height, mb.cb.Height
				if horizontal {
					ax, d, ref = &mb.x, mb.st.Width, mb.cb.Width
				}
				avail = ref
				s[i].gen = true
				if size, ok := contentSize(mb.st, d, ref, ax.bp); ok || clamped[i] != nil {
					if clamped[i] != nil {
						size = *clamped[i]
					}
					ax.size = size
					s[i].used = ax.outer()
					s[i].minO, s[i].maxO = s[i].used, s[i].used
					continue
				}
				s[i].auto = true
				s[i].minO, s[i].maxO = mb.minW, mb.maxW
			}
			used = variableSizes(avail, s)
			again := false
			for i, mb := range sides[side] {
				if mb == nil || !s[i].auto {
					continue
				}
				ax, lo, hi, ref := &mb.y, mb.st.MinHeight, mb.st.MaxHeight, mb.cb.Height
				if horizontal {
					ax, lo, hi, ref = &mb.x, mb.st.MinWidth, mb.st.MaxWidth, mb.cb.Width
				}
				size := max(used[i]-ax.m1-ax.m2-ax.bp, 0)
				if mx, ok := contentSize(mb.st, hi, ref, ax.bp); ok && size > mx {
					clamped[i], again = &mx, true
				} else if mn, ok := contentSize(mb.st, lo, ref, ax.bp); ok && size < mn {
					clamped[i], again = &mn, true
				}
			}
			if !again {
				break
			}
		}
		for i, mb := range sides[side] {
			if mb == nil {
				continue
			}
			ax := &mb.y
			if horizontal {
				ax = &mb.x
			}
			if s[i].auto {
				ax.size = max(used[i]-ax.m1-ax.m2-ax.bp, 0)
			} else if clamped[i] != nil {
				ax.size = *clamped[i]
			}
			// A is flush with the start, B centred, C flush with the end;
			// the start margin absorbs the offset.
			switch i {
			case 1:
				ax.m1 += (avail - ax.outer()) / 2
			case 2:
				ax.m1 += avail - ax.outer()
			}
		}
	}
}

// probeID returns the anchor of the measuring box of mb.
func probeID(mb *marginBox, kind string) string {
	return fmt.Sprintf("litho-margin-%d-%s", mb.which, kind)
}

// measureVariable measures what the variable dimension of each side box
// needs: the outer min- and max-content widths of a top or bottom box
// whose width is auto, the content height of a left or right box at its
// width.
func (p *prepared) measureVariable(ctx context.Context, boxes []*marginBox) error {
	root := measureRoot()
	for _, mb := range boxes {
		switch side, _ := marginSide(mb.which); side {
		case sideTop, sideBottom:
			if _, ok := contentSize(mb.st, mb.st.Width, mb.cb.Width, mb.x.bp); ok {
				continue
			}
			// The max-content width is the shrink-to-fit width on a wide
			// page, the min-content width the one in a container of no
			// width (CSS 2.1, 10.3.7 and 10.3.5).
			root.Children = append(root.Children, probeNode(mb, probeID(mb, "max"), -1))
			zero := absStyle(css.Initial(), 0, 0)
			zero.Width = css.Pts(0)
			fl := probeStyle(mb.st)
			fl.Position, fl.Float = css.Static, css.FloatLeft
			root.Children = append(root.Children, &layout.Node{Kind: layout.Element, Style: zero, Children: []*layout.Node{
				{Kind: layout.Element, Style: fl, ID: probeID(mb, "min"), Children: mb.kids},
			}})
		case sideLeft, sideRight:
			root.Children = append(root.Children, probeNode(mb, probeID(mb, "h"), mb.x.size))
		}
	}
	if len(root.Children) == 0 {
		return nil
	}
	ext, err := p.measure(ctx, root)
	if err != nil {
		return err
	}
	for _, mb := range boxes {
		side, _ := marginSide(mb.which)
		if r, ok := ext[probeID(mb, "max")]; ok {
			mb.maxW = r.Width + mb.x.m1 + mb.x.m2 + mb.x.bp
		}
		if r, ok := ext[probeID(mb, "min")]; ok {
			mb.minW = r.Width + mb.x.m1 + mb.x.m2 + mb.x.bp
		}
		if r, ok := ext[probeID(mb, "h")]; ok && (side == sideLeft || side == sideRight) {
			mb.contentH = r.Height
			mb.minW = r.Height + mb.y.m1 + mb.y.m2 + mb.y.bp
			mb.maxW = mb.minW
		}
	}
	return nil
}

// measureHeights measures the content height of every box at its used
// width, for its vertical alignment.
func (p *prepared) measureHeights(ctx context.Context, boxes []*marginBox) error {
	root := measureRoot()
	for _, mb := range boxes {
		root.Children = append(root.Children, probeNode(mb, probeID(mb, "h"), mb.x.size))
	}
	ext, err := p.measure(ctx, root)
	if err != nil {
		return err
	}
	for _, mb := range boxes {
		if r, ok := ext[probeID(mb, "h")]; ok {
			mb.contentH = r.Height
		}
	}
	return nil
}

// measure lays root out on a measuring page and returns the extents of its
// boxes.
func (p *prepared) measure(ctx context.Context, root *layout.Node) (map[string]typeset.Rect, error) {
	res, err := layout.Layout(ctx, wrapRoot(root), p.fonts, layout.Options{
		Page: layout.PageBox{Width: measureSide, Height: measureSide},
		Lang: p.layout.Lang, Fallback: p.layout.Fallback,
		Image: p.layout.Image, MaxPages: maxSubLayoutPages,
	})
	if err != nil {
		return nil, err
	}
	return res.Extents, nil
}

// paintMargins lays the boxes out at their used positions on a page of
// the size of pb and returns what they paint, as artifacts, and the links
// of what they paint, an internal one resolved against the document's
// anchors.
//
// A link keeps its annotation on every page it is drawn on. Its content
// is an artifact, and an artifact holds no structure element (ISO 32000-1,
// 14.8.2.2.2); the annotation is tagged in a Link element of its own,
// which holds its object reference alone, as ISO 14289-1, 7.18.1 and
// 7.18.5 ask of every link annotation of a tagged document.
func (p *prepared) paintMargins(ctx context.Context, boxes []*marginBox, pb layout.PageBox) ([]typeset.Fragment, []typeset.Link, error) {
	root := measureRoot()
	for _, mb := range boxes {
		st := absStyle(mb.st, mb.cb.X+mb.x.m1, mb.cb.Y+mb.y.m1)
		st.Width, st.Height = css.Pts(mb.x.size), css.Pts(mb.y.size)
		st.BoxSizing = css.ContentBox
		inner := probeStyle(mb.st)
		inner.Position = css.Static
		// vertical-align places the content in the box as in a table cell
		// (CSS Page 3, 5, the page-margin box defaults).
		switch free := mb.y.size - mb.contentH; mb.st.VerticalAlign.Kind { //nolint:exhaustive // the other values align to the top
		case css.Middle:
			inner.Margin[css.Top] = css.Pts(free / 2)
		case css.AlignBottom:
			inner.Margin[css.Top] = css.Pts(free)
		}
		root.Children = append(root.Children, &layout.Node{Kind: layout.Element, Style: st, Children: []*layout.Node{
			{Kind: layout.Element, Style: inner, Children: mb.kids},
		}})
	}
	var targets map[string]typeset.Dest
	if p.b != nil {
		targets = p.b.gcpm.placed
	}
	res, err := layout.Layout(ctx, wrapRoot(root), p.fonts, layout.Options{
		Page: layout.PageBox{Width: pb.Width, Height: pb.Height},
		Lang: p.layout.Lang, Fallback: p.layout.Fallback,
		Image: p.layout.Image, MaxPages: maxSubLayoutPages,
		Targets: targets,
	})
	if err != nil {
		return nil, nil, err
	}
	if len(res.Pages) == 0 {
		return nil, nil, nil
	}
	links := res.Pages[0].Links
	for i, l := range links {
		if l.Parent != nil {
			links[i].Parent = &typeset.StructElem{Role: "Link", Lang: l.Parent.Lang, Alt: l.Parent.Alt}
		}
	}
	return artifacts(res.Pages[0].Content), links, nil
}

// artifacts returns frags with no structure element: page-margin content
// is a pagination artifact (ISO 32000-1, 14.8.2.2.2).
func artifacts(frags []typeset.Fragment) []typeset.Fragment {
	for _, f := range frags {
		switch v := f.(type) {
		case *typeset.Text:
			v.Parent = nil
		case *typeset.Image:
			v.Parent = nil
		case *typeset.Path:
			v.Parent = nil
		case *typeset.Group:
			artifacts(v.Content)
		}
	}
	return frags
}

// wrapRoot returns a root element holding body, so that no box of body
// propagates its background to the canvas (CSS Backgrounds 3, 2.11.2).
func wrapRoot(body *layout.Node) *layout.Node {
	return &layout.Node{Kind: layout.Element, Style: blockStyle(css.Initial()), Children: []*layout.Node{body}}
}

// measureRoot returns the body of a sub-layout: a plain block whose
// children are positioned on the page.
func measureRoot() *layout.Node {
	return &layout.Node{Kind: layout.Element, Style: blockStyle(css.Initial())}
}

// probeNode returns a box that measures the content of mb: absolutely
// positioned at the top left, width wide, or shrink-to-fit for a negative
// width, with mb's text styles and no edges.
func probeNode(mb *marginBox, id string, width float64) *layout.Node {
	st := probeStyle(mb.st)
	if width >= 0 {
		st.Width = css.Pts(width)
	}
	return &layout.Node{Kind: layout.Element, Style: st, ID: id, Children: mb.kids}
}

// probeStyle returns the style of a block holding the content of a box
// of style st: st's inherited properties, no edges, positioned at the top
// left of the page.
func probeStyle(st *css.Style) *css.Style {
	return absStyle(anonInline(st), 0, 0)
}

// absStyle returns a copy of st absolutely positioned with its border box
// at x, y on the page, without margins.
func absStyle(st *css.Style, x, y float64) *css.Style {
	s := blockStyle(st)
	s.Position = css.Absolute
	s.Left, s.Top = css.Pts(x), css.Pts(y)
	s.Right, s.Bottom = css.Dimension{Kind: css.Auto}, css.Dimension{Kind: css.Auto}
	s.Margin = [4]css.Dimension{css.Pts(0), css.Pts(0), css.Pts(0), css.Pts(0)}
	s.MinWidth, s.MinHeight = css.Pts(0), css.Pts(0)
	s.MaxWidth, s.MaxHeight = css.Dimension{Kind: css.None}, css.Dimension{Kind: css.None}
	return s
}

// blockStyle returns a copy of st as a block in flow.
func blockStyle(st *css.Style) *css.Style {
	s := *st
	s.Display, s.Float, s.Position, s.OuterInline = css.Block, css.FloatNone, css.Static, false
	s.Content = css.Content{}
	s.Page = ""
	s.BreakBefore, s.BreakAfter = css.BreakAuto, css.BreakAuto
	return &s
}

// marginNodes returns the content of a page-margin box of style st on
// the page of index, one of pages, whose page box is pb (CSS Page 3, 5.2;
// CSS Generated Content 3, 1.1): its strings, its counters as
// [prepared.counterText] writes them, attr() at its fallback since a
// page-margin box has no element, its images, its leaders, and the named
// strings and running elements the page shows (CSS GCPM 3, 1), a running
// element's page and pages counters at the page's values.
func (p *prepared) marginNodes(ctx context.Context, st *css.Style, index, pages int, pb layout.PageBox) []*layout.Node {
	ts := anonInline(st)
	var out []*layout.Node
	var sb strings.Builder
	flush := func() {
		if sb.Len() > 0 {
			out = append(out, &layout.Node{Kind: layout.Text, Style: ts, Text: strings.ToValidUTF8(sb.String(), "�")})
			sb.Reset()
		}
	}
	text := p.counterText(st, index, pages)
	for _, it := range st.Content.Items {
		if sb.Len() > maxGeneratedBytes {
			break
		}
		switch it.Kind {
		case css.ItemString:
			sb.WriteString(it.Text)
		case css.ItemAttr:
			sb.WriteString(it.Fallback)
		case css.ItemCounter, css.ItemCounters:
			sb.WriteString(text(it))
		case css.ItemImage:
			if p.b != nil {
				if pic := p.b.contentImage(ctx, it.Text); pic != nil {
					flush()
					out = append(out, &layout.Node{Kind: layout.Replaced, Style: anonInline(ts), Picture: pic})
				}
			}
		case css.ItemLeader:
			if n := leaderNode(ts, it.Text); n != nil {
				flush()
				out = append(out, n)
			}
		case css.ItemNamedString, css.ItemElement:
			if p.b == nil {
				continue
			}
			g := &p.b.gcpm
			top := pb.Bleed + pb.Margin[css.Top]
			if it.Kind == css.ItemNamedString {
				if a, ok := g.pick(g.strings, it.Text, it.Pick, index, top); ok {
					sb.WriteString(a.value)
				}
				continue
			}
			if a, ok := g.pick(g.running, it.Text, it.Pick, index, top); ok && a.node != nil {
				flush()
				n := a.node
				if a.paged {
					n = g.atPage(n, text, 0)
				}
				out = append(out, runningBox(n, st))
			}
		case css.ItemOpenQuote, css.ItemCloseQuote, css.ItemNoOpenQuote, css.ItemNoCloseQuote, css.ItemContent:
		}
	}
	flush()
	return out
}

// counterText returns what a counter() or counters() item writes in the
// page-margin box styled box on the page of index, one of pages (CSS Page
// 3, 6.1): pages is the page count; a counter the box's own counter
// properties touch takes the value they give; then the counters of the
// page context, the page counter among them; then the document's
// counters as an element at the start of the page sees them; 0 for any
// other.
func (p *prepared) counterText(box *css.Style, index, pages int) func(css.ContentItem) string {
	pc := p.pageCounters(index)
	var g *gcpmState
	var cs *css.CounterStyles
	if p.b != nil {
		g = &p.b.gcpm
		cs = p.b.counterStyles
	}
	values := func(name string) []int {
		if name == pagesCounter {
			return []int{pages}
		}
		if v, ok := pc[name]; ok {
			return []int{v}
		}
		if g != nil {
			if i := slices.Index(g.track, name); i >= 0 {
				return g.countersAt(i, index)
			}
		}
		return nil
	}
	// The box's counters are in scope for the box alone, over those of
	// the page context and the document.
	var local map[string]int
	if len(box.CounterReset)+len(box.CounterIncrement)+len(box.CounterSet) > 0 {
		local = map[string]int{}
		base := func(name string) int {
			if v, ok := local[name]; ok {
				return v
			}
			if vs := values(name); len(vs) > 0 {
				return vs[len(vs)-1]
			}
			return 0
		}
		runCounterOps(local, base, box, false)
	}
	return func(it css.ContentItem) string {
		vals := values(it.Text)
		if v, ok := local[it.Text]; ok && it.Text != pagesCounter {
			vals = []int{v}
		}
		if len(vals) == 0 {
			vals = []int{0}
		}
		if it.Kind == css.ItemCounter {
			return counterText(cs, it.Style, it.StyleName, vals[len(vals)-1])
		}
		var sb strings.Builder
		for i, v := range vals {
			if i > 0 {
				sb.WriteString(it.Separator)
			}
			sb.WriteString(counterText(cs, it.Style, it.StyleName, v))
		}
		return sb.String()
	}
}

// runCounterOps runs the counter properties of st on the counters cur, in
// the order CSS Lists 3, 4.5 gives, reset, increment, set, base giving the
// value of a counter cur does not hold. implicitPage adds 1 to the page
// counter when no property names it. The pages counter takes none of them
// (CSS Page 3, 6.1).
func runCounterOps(cur map[string]int, base func(string) int, st *css.Style, implicitPage bool) {
	touched := false
	for _, ops := range [...][]css.CounterOp{st.CounterReset, st.CounterIncrement, st.CounterSet} {
		for _, op := range ops {
			touched = touched || op.Name == pageCounter
		}
	}
	for _, op := range st.CounterReset {
		if op.Name != pagesCounter {
			cur[op.Name] = clampCounter(op.Value)
		}
	}
	for _, op := range st.CounterIncrement {
		if op.Name != pagesCounter {
			cur[op.Name] = addCounter(base(op.Name), op.Value)
		}
	}
	if implicitPage && !touched {
		cur[pageCounter] = addCounter(base(pageCounter), 1)
	}
	for _, op := range st.CounterSet {
		if op.Name != pagesCounter {
			cur[op.Name] = clampCounter(op.Value)
		}
	}
}

// addCounter returns a + b within the values [clampCounter] keeps.
func addCounter(a, b int) int {
	s := min(max(int64(a)+int64(b), -maxListValue), maxListValue)
	return int(s) // fits int: within ±maxListValue, 2^30
}

// pageCounters returns the counters of the page context on the page of
// index (CSS Page 3, 6.1), computed in page order from the page styles:
// the page counter starts at 0 before the first page and each page adds 1
// to it, unless the page context's counter-reset, counter-increment or
// counter-set names it, as WeasyPrint decides the draft leaves open; a
// counter of the page context lives on from page to page, the pages being
// siblings (CSS 2.1, 12.4.1). nil when the page was not laid out.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 numbers four pages 2, 4, 6, 8 under
//     @page { counter-increment: page 2 }, 10 to 13 under @page :first {
//     counter-set: page 10 }, 5 on each under @page { counter-reset: page 5
//     }, and 5, 1, 2, 3 under @page :first { counter-reset: page 5 }, where
//     litho numbers 5, 6, 7, 8.
func (p *prepared) pageCounters(index int) map[string]int {
	s := p.pages
	for k := len(s.counters); k <= index && k < len(p.infos); k++ {
		cur := map[string]int{pageCounter: 0}
		if k > 0 {
			cur = maps.Clone(s.counters[k-1])
		}
		ps := s.entry(p.infos[k]).style
		st := css.Style{CounterReset: ps.CounterReset, CounterIncrement: ps.CounterIncrement, CounterSet: ps.CounterSet}
		runCounterOps(cur, func(name string) int { return cur[name] }, &st, true)
		s.counters = append(s.counters, cur)
	}
	if index < len(s.counters) {
		return s.counters[index]
	}
	return nil
}

// runningBox returns the running element n as the page-margin box styled
// box shows it: a block sized to fit its content and placed as the box's
// text-align places a line, as WeasyPrint lays running elements out (CSS
// GCPM 3, 2.2.1 leaves their layout in the box open).
func runningBox(n *layout.Node, box *css.Style) *layout.Node {
	if n.Style == nil || n.Style.Display != css.Block || n.Style.Width.Kind != css.Auto {
		return n
	}
	c := *n
	st := *n.Style
	st.Width = css.Dimension{Kind: css.FitContent}
	auto := css.Dimension{Kind: css.Auto}
	rtl := box.Direction == css.RTL
	switch box.TextAlign {
	case css.AlignCenter:
		st.Margin[css.Left], st.Margin[css.Right] = auto, auto
	case css.AlignRight:
		st.Margin[css.Left] = auto
	case css.AlignEnd:
		if rtl {
			st.Margin[css.Right] = auto
		} else {
			st.Margin[css.Left] = auto
		}
	case css.AlignStart, css.AlignJustify, css.AlignJustifyAll, css.AlignAuto:
		if rtl {
			st.Margin[css.Left] = auto
		}
	case css.AlignLeft:
	}
	c.Style = &st
	return &c
}
