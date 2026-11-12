package layout

import (
	"fmt"
	"math"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/numeric"
)

// checkFrame reports a page frame whose insets [Layout] cannot draw: not
// finite, negative, or leaving no box on the page pb.
func checkFrame(pb PageBox) error {
	f := pb.Frame
	if f == nil {
		return nil
	}
	for _, v := range f.Inset {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			return fmt.Errorf("%w: page frame inset %v", ErrInvalidOptions, v)
		}
	}
	if pb.Width-f.Inset[css.Left]-f.Inset[css.Right] <= 0 || pb.Height-f.Inset[css.Top]-f.Inset[css.Bottom] <= 0 {
		return fmt.Errorf("%w: page frame insets %v leave no box on a page of %v by %v", ErrInvalidOptions, f.Inset, pb.Width, pb.Height)
	}
	return nil
}

// paintFrame paints the page frame of pb on pg, in page box coordinates:
// the border box at its insets with the borders and corner radii of its
// style, drawn as a block's borders are (CSS Backgrounds 3, 4 and 5), as
// Word and Writer draw page borders (ECMA-376 Part 1, 17.6.10 pgBorders;
// ODF 1.3 Part 3, 20.183 fo:border of a page layout). It paints over the
// page background and the canvas and under the margin content and the
// page area.
func (e *engine) paintFrame(pg *compose.Page, pb PageBox) {
	f := pb.Frame
	if f == nil || f.Style == nil {
		return
	}
	r := compose.Rect{
		X: f.Inset[css.Left], Y: f.Inset[css.Top],
		Width:  pb.Width - f.Inset[css.Left] - f.Inset[css.Right],
		Height: pb.Height - f.Inset[css.Top] - f.Inset[css.Bottom],
	}
	if !numeric.Positive(r.Width) || !numeric.Positive(r.Height) {
		return
	}
	fb := &box{st: f.Style, kind: kindBlock}
	_, bd, _ := edgesOf(fb, r.Width)
	p := &painter{e: e, top: &ditem{group: true, opacity: 1}, mono: -1, alpha: 1}
	p.frames = []*frame{{container: p.top, root: fb}}
	p.border(fb, f.Style, r, radiiOf(fb, r.Width, r.Height), bd, [4]bool{true, true, true, true})
	pg.Content = append(pg.Content, e.flatten(p.top, 0, true)...)
}
