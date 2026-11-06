package html

import (
	"fmt"
	"math"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
)

// pageBox returns the page box every page is laid out on: the paper and
// margins of o, overridden by the document's @page rules for an unnamed
// page (CSS Page 3, 4.2). A size descriptor replaces the paper, an
// orientation alone turns it (CSS Page 3, 5.1), and a margin replaces that
// side's, a percentage resolved against the page's width for the left and
// right margins and its height for the top and bottom ones (CSS Page 3,
// the page-margin properties). :first, :left and :right rules do not
// apply: the engine lays every page out on one box.
//
// A size past the PDF page limit is clamped to it, and @page margins that
// leave no page area give way to o's, with a warning.
func pageBox(r *css.Resolver, o Options) (layout.PageBox, []compose.Warning) {
	ps := r.Page(css.PageSelector{})
	var warns []compose.Warning
	w, h := o.PageWidth, o.PageHeight
	switch {
	case ps.Width > 0 && ps.Height > 0:
		w, h = ps.Width, ps.Height
		if w > maxPageSide || h > maxPageSide || math.IsInf(w, 0) || math.IsInf(h, 0) {
			warns = append(warns, compose.Warning{Code: CodePageBox, Message: fmt.Sprintf(
				"the @page size of %v by %v points was clamped to %v points a side, the PDF limit", w, h, maxPageSide)})
			w, h = min(w, maxPageSide), min(h, maxPageSide)
		}
	case ps.Orientation == css.Landscape && w < h, ps.Orientation == css.Portrait && w > h:
		w, h = h, w
	}
	box := layout.PageBox{Width: w, Height: h}
	opt := [4]float64{css.Top: o.MarginTop, css.Right: o.MarginRight, css.Bottom: o.MarginBottom, css.Left: o.MarginLeft}
	fromRules := false
	for side := css.Top; side <= css.Left; side++ {
		ref := w
		if side == css.Top || side == css.Bottom {
			ref = h
		}
		m := ps.Margin[side]
		switch m.Kind { //nolint:exhaustive // a page margin is Auto, Points or Percent
		case css.Points:
			box.Margin[side], fromRules = m.Value, true
		case css.Percent:
			box.Margin[side], fromRules = m.Value*ref, true
		default:
			box.Margin[side] = opt[side]
		}
		if !(box.Margin[side] >= 0 && box.Margin[side] < math.Inf(1)) {
			box.Margin[side] = opt[side]
		}
	}
	if fits(box) {
		return box, warns
	}
	if fromRules {
		warns = append(warns, compose.Warning{Code: CodePageBox, Message: fmt.Sprintf(
			"the @page margins leave no room on a page of %v by %v points; the conversion's margins apply", w, h)})
	}
	box.Margin = opt
	if !fits(box) {
		// The options' margins were checked against the paper; an @page
		// size can be smaller than they allow.
		warns = append(warns, compose.Warning{Code: CodePageBox, Message: fmt.Sprintf(
			"the margins leave no room on the @page size of %v by %v points; the page has none", w, h)})
		box.Margin = [4]float64{}
	}
	return box, warns
}

// fits reports a page box whose margins leave a positive page area.
func fits(b layout.PageBox) bool {
	return b.Margin[css.Left]+b.Margin[css.Right] < b.Width && b.Margin[css.Top]+b.Margin[css.Bottom] < b.Height
}
