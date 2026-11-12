package html

import (
	"context"
	"fmt"
	"math"
	"reflect"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// pageSel returns the selector of the unnamed page at a 0-based index:
// the first page, and a right or a left page, the first page being a right
// page in a left-to-right document and a left page in a right-to-left one
// (CSS Page 3, 4.3.2).
func pageSel(index int, rtl bool) css.PageSelector {
	right := (index%2 == 0) != rtl
	return css.PageSelector{First: index == 0, Right: right, Left: !right}
}

// pageBoxes returns the page box of the first page and, when the pages
// differ, the function that gives the box of every page for
// layout.Options.PageAt. Unnamed pages differ only by being the first and
// by their side, so three boxes cover a document. ctx's error stops it.
func pageBoxes(ctx context.Context, r *css.Resolver, o Options, rtl bool) (layout.PageBox, func(int) layout.PageBox, []typeset.Warning, error) {
	var warns []typeset.Warning
	seen := map[string]bool{}
	var boxes [3]layout.PageBox
	for index := range boxes {
		b, ws, err := pageBox(ctx, r, o, pageSel(index, rtl))
		if err != nil {
			return layout.PageBox{}, nil, nil, err
		}
		for _, w := range ws {
			if !seen[w.Message] {
				seen[w.Message] = true
				warns = append(warns, w)
			}
		}
		boxes[index] = b
	}
	first, second, third := boxes[0], boxes[1], boxes[2]
	// Each box holds its own copy of the page background: compare values.
	if reflect.DeepEqual(first, second) && reflect.DeepEqual(second, third) {
		return first, nil, warns, nil
	}
	return first, func(index int) layout.PageBox {
		switch {
		case index <= 0:
			return first
		case index%2 == 1:
			return second
		}
		return third
	}, warns, nil
}

// pageBox returns the box of the page sel names: the paper and margins of
// o, overridden by the document's @page rules that match it (CSS Page 3,
// 4.2). A size descriptor replaces the paper, an orientation alone turns
// it (CSS Page 3, 5.1), and a margin replaces that side's, a percentage
// resolved against the page's width for the left and right margins and its
// height for the top and bottom ones (CSS Page 3, the page-margin
// properties), and the page's background properties paint it.
//
// A size past the PDF page limit is clamped to it, and @page margins that
// leave no page area give way to o's, with a warning. ctx's error stops
// it.
func pageBox(ctx context.Context, r *css.Resolver, o Options, sel css.PageSelector) (layout.PageBox, []typeset.Warning, error) {
	ps, err := r.Page(ctx, sel)
	if err != nil {
		return layout.PageBox{}, nil, err
	}
	var warns []typeset.Warning
	w, h := o.PageWidth, o.PageHeight
	switch {
	case ps.Width > 0 && ps.Height > 0:
		w, h = ps.Width, ps.Height
		if w > maxPageSide || h > maxPageSide || math.IsInf(w, 0) || math.IsInf(h, 0) {
			warns = append(warns, typeset.Warning{Code: CodePageBox, Message: fmt.Sprintf(
				"the @page size of %v by %v points was clamped to %v points a side, the PDF limit", w, h, maxPageSide)})
			w, h = min(w, maxPageSide), min(h, maxPageSide)
		}
	case ps.Orientation == css.Landscape && w < h, ps.Orientation == css.Portrait && w > h:
		w, h = h, w
	}
	box := layout.PageBox{Width: w, Height: h, Background: ps.Background, Marks: ps.Marks}
	if r.HasFootnoteArea() || r.HasNoteArea() {
		in, err := r.PageIn(ctx, sel, nil)
		if err != nil {
			return layout.PageBox{}, nil, err
		}
		box.Footnote, box.NoteArea = in.Footnote, in.NoteArea
	}
	// The bleed grows the sheet past the page box (CSS Page 3, 7.2), up
	// to the PDF page limit.
	if b := ps.UsedBleed(); numeric.PositiveFinite(b) {
		box.Bleed = min(b, max((maxPageSide-max(w, h))/2, 0))
	}
	opt := [4]float64{css.Top: o.MarginTop, css.Right: o.MarginRight, css.Bottom: o.MarginBottom, css.Left: o.MarginLeft}
	fromRules := false
	for side := css.Top; side <= css.Left; side++ {
		ref := w
		if side == css.Top || side == css.Bottom {
			ref = h
		}
		m := ps.Margin[side]
		switch m.Kind { //nolint:exhaustive // a page margin is Auto, Points, Percent or Calc
		case css.Points, css.Percent, css.Calc:
			box.Margin[side], fromRules = resolveDim(m, ref), true
		default:
			box.Margin[side] = opt[side]
		}
		if !isMargin(box.Margin[side]) {
			box.Margin[side] = opt[side]
		}
	}
	// The width and height of the page context size the page area, and
	// the page box follows with its margins (CSS Page 3, 3.2).
	if a := ps.AreaWidth; a.Kind == css.Points && a.Value > 0 {
		box.Width = min(box.Margin[css.Left]+a.Value+box.Margin[css.Right], maxPageSide)
	}
	if a := ps.AreaHeight; a.Kind == css.Points && a.Value > 0 {
		box.Height = min(box.Margin[css.Top]+a.Value+box.Margin[css.Bottom], maxPageSide)
	}
	if fits(box) {
		return box, warns, nil
	}
	if fromRules {
		warns = append(warns, typeset.Warning{Code: CodePageBox, Message: fmt.Sprintf(
			"the @page margins leave no room on a page of %v by %v points; the conversion's margins apply", w, h)})
	}
	box.Margin = opt
	if !fits(box) {
		// The options' margins were checked against the paper; an @page
		// size can be smaller than they allow.
		warns = append(warns, typeset.Warning{Code: CodePageBox, Message: fmt.Sprintf(
			"the margins leave no room on the @page size of %v by %v points; the page has none", w, h)})
		box.Margin = [4]float64{}
	}
	return box, warns, nil
}

// areaOf returns the size of the page area of b, its size less its
// margins.
func areaOf(b layout.PageBox) (float64, float64) {
	return b.Width - b.Margin[css.Left] - b.Margin[css.Right], b.Height - b.Margin[css.Top] - b.Margin[css.Bottom]
}

// isMargin reports whether v is a margin in points: finite and zero or
// more. NaN is not.
func isMargin(v float64) bool { return v >= 0 && v < math.Inf(1) }

// fits reports a page box whose margins leave a positive page area.
func fits(b layout.PageBox) bool {
	return b.Margin[css.Left]+b.Margin[css.Right] < b.Width && b.Margin[css.Top]+b.Margin[css.Bottom] < b.Height
}
