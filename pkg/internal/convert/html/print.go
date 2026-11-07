package html

import (
	"fmt"
	"math"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/layout"
)

// maxShrink is the most a document wider than its page area is shrunk to
// fit it: 1.5, Blink's printingMaximumShrinkFactor
// (third_party/blink/renderer/core/frame/settings.json5). Content wider
// still is laid out at 1.5 times the area and clipped.
const maxShrink = 1.5

// shrinkSlack is the overflow, in points of layout, below which a document
// counts as fitting its page area: a sixty-fourth of a CSS pixel, Blink's
// layout unit, so rounding never lays a document out twice.
const shrinkSlack = 0.75 / 64

// printSetup is how the page boxes the @page rules give, in points of the
// page, become the boxes the layout engine cuts the flow with.
//
// Chromium lays the content of a page out in its page area multiplied by
// the pagination scale factor and paints it divided by that factor
// (blink::PrintContext::BeginPrintMode,
// blink::CalculateInitialContainingBlockSizeForPagination,
// blink::TargetScaleForPage): the factor starts at 1 / scale and grows by
// the shrink-to-fit ratio of a document wider than its area
// (blink::LocalFrameView::ForceLayoutForPagination). litho multiplies the
// whole page box by the factor, margins included, and draws the laid out
// page divided by it, so the margins come out as the page box has them.
type printSetup struct {
	// factor is the layout's points per point of the page.
	factor float64
	// measure lays the document out as the screen measurement of a single
	// page: one continuous column [screenWidth] wide, no margins, no page
	// box, the viewport [screenWidth] by [screenHeight] (see
	// [measureSinglePage]).
	measure bool
}

// newPrintSetup returns the setup of a first layout under o.
func newPrintSetup(o Options) printSetup {
	return printSetup{factor: 1 / o.Scale}
}

// source returns the page box b of the @page rules as the setup lays it
// out, still in points of the page: the screen column when measuring.
func (s printSetup) source(b layout.PageBox) layout.PageBox {
	if s.measure {
		return layout.PageBox{Width: screenWidth, Height: maxPageSide}
	}
	return b
}

// layoutBox returns the box the layout engine lays the page b out on: the
// source box multiplied by the factor.
func (s printSetup) layoutBox(b layout.PageBox) layout.PageBox {
	b = s.source(b)
	b.Width *= s.factor
	b.Height *= s.factor
	b.Bleed *= s.factor
	for i := range b.Margin {
		b.Margin[i] *= s.factor
	}
	return b
}

// fitFactor returns the setup with its factor lowered, with a warning, so
// that the page boxes boxes, in points of the page, stay within the PDF
// page limit once multiplied.
func (s printSetup) fitFactor(boxes ...layout.PageBox) (printSetup, []compose.Warning) {
	side := 0.0
	for _, b := range boxes {
		b = s.source(b)
		side = max(side, b.Width, b.Height)
	}
	if side <= 0 || side*s.factor <= maxPageSide {
		return s, nil
	}
	limit := maxPageSide / side
	w := compose.Warning{Code: CodePageBox, Message: fmt.Sprintf(
		"the page of %v points a side cannot be laid out %.3g times larger, past the PDF page limit; it is laid out %.3g times larger", side, s.factor, limit)}
	s.factor = limit
	return s, []compose.Warning{w}
}

// refit returns the setup of a second layout when the first, res, laid
// the document out wider than its page area, false when it fits or is
// measured: the factor grown by the overflow ratio, at most [maxShrink], as
// Chromium shrinks such a document
// (blink::CalculateOverflowShrinkForPrinting).
func (p *prepared) refit(res *layout.Result) (printSetup, bool) {
	s := p.setup
	lw, _ := areaOf(s.layoutBox(p.first))
	if !(lw > 0) || !(res.ContentWidth-lw > shrinkSlack) {
		return s, false
	}
	if s.measure || p.o.NoShrinkToFit {
		return s, false
	}
	s.factor *= min(res.ContentWidth/lw, maxShrink)
	return s, true
}

// placement is where a laid out page lands on its sheet: scaled by s,
// moved by ox and oy, on a sheet of w by h points.
type placement struct {
	s, ox, oy, w, h float64
}

// placement returns where the laid out page lp lands. The page box it
// was laid out on, divided by the factor, is the source page. With
// [Options.PreferCSSPageSize] the sheet is the source page. Otherwise the sheet is the paper, turned to the source page's
// orientation, and the source page is scaled down to fit it, never up,
// and centred on it, as Chromium prints without preferCSSPageSize
// (blink::TargetPageBorderBoxLogicalRect,
// blink::PageBoxDefaultSizeWithSourceOrientation). A sheet side is at
// most the PDF page limit.
func (p *prepared) placement(lp *compose.Page) placement {
	f := p.setup.factor
	sw, sh := lp.Width/f, lp.Height/f
	tw, th := sw, sh
	if !p.o.PreferCSSPageSize {
		tw, th = p.o.PageWidth, p.o.PageHeight
		if !nearly(sw, sh) && (tw > th) != (sw > sh) {
			tw, th = th, tw
		}
	}
	fit := min(1, tw/sw, th/sh)
	if fit > 1-1e-9 {
		fit = 1
	}
	pl := placement{s: fit / f, ox: (tw - sw*fit) / 2, oy: (th - sh*fit) / 2, w: min(tw, maxPageSide), h: min(th, maxPageSide)}
	if math.Abs(pl.ox) < 1e-6 {
		pl.ox = 0
	}
	if math.Abs(pl.oy) < 1e-6 {
		pl.oy = 0
	}
	if nearly(pl.s, 1) {
		pl.s = 1
	}
	return pl
}

// nearly reports a and b equal to a billionth.
func nearly(a, b float64) bool {
	return math.Abs(a-b) <= 1e-9*max(math.Abs(a), math.Abs(b), 1)
}

// identity reports a placement that changes nothing.
func (pl placement) identity() bool {
	return pl.s == 1 && pl.ox == 0 && pl.oy == 0
}

// page returns the laid out page lp placed on its sheet: its content in a
// group under the placement's transform, clipped to the source page, its
// links moved with it.
func (pl placement) page(lp *compose.Page) *compose.Page {
	out := &compose.Page{Width: pl.w, Height: pl.h, Content: lp.Content, Links: lp.Links, TrimBox: lp.TrimBox, BleedBox: lp.BleedBox}
	if pl.identity() {
		return out
	}
	for _, b := range []**compose.Rect{&out.TrimBox, &out.BleedBox} {
		if *b != nil {
			r := pl.rect(**b)
			*b = &r
		}
	}
	src := compose.Rect{Width: lp.Width, Height: lp.Height}
	out.Content = []compose.Fragment{&compose.Group{
		Transform: compose.Matrix{A: pl.s, D: pl.s, E: pl.ox, F: pl.oy},
		Clip:      &compose.Path{Ops: rectPath(src), Fill: &compose.Fill{}},
		Opacity:   1,
		Content:   lp.Content,
	}}
	out.Links = make([]compose.Link, len(lp.Links))
	for i, l := range lp.Links {
		l.Rect = pl.rect(l.Rect)
		out.Links[i] = l
	}
	return out
}

// rect returns r placed.
func (pl placement) rect(r compose.Rect) compose.Rect {
	return compose.Rect{X: r.X*pl.s + pl.ox, Y: r.Y*pl.s + pl.oy, Width: r.Width * pl.s, Height: r.Height * pl.s}
}

// point returns the position x, y placed.
func (pl placement) point(x, y float64) (float64, float64) {
	return x*pl.s + pl.ox, y*pl.s + pl.oy
}

// rectPath returns the path of the rectangle r.
func rectPath(r compose.Rect) []compose.PathOp {
	return []compose.PathOp{
		{Kind: compose.MoveTo, P: [3]compose.Point{{X: r.X, Y: r.Y}}},
		{Kind: compose.LineTo, P: [3]compose.Point{{X: r.X + r.Width, Y: r.Y}}},
		{Kind: compose.LineTo, P: [3]compose.Point{{X: r.X + r.Width, Y: r.Y + r.Height}}},
		{Kind: compose.LineTo, P: [3]compose.Point{{X: r.X, Y: r.Y + r.Height}}},
		{Kind: compose.ClosePath},
	}
}
