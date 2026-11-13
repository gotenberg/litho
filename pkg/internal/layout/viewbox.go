package layout

import "github.com/gotenberg/litho/pkg/internal/css"

// viewBox is the part of a picture an object-view-box selects: its left
// and top offsets in the picture, and the picture's whole natural size,
// in points.
type viewBox struct {
	x, y, w, h float64
}

// viewBoxPicture returns pic with the view box vb, an inset of its
// natural size, as its natural size (CSS Images 5, 4.1); pic itself for
// none, for a picture without a natural size, and for a view box that
// encloses nothing.
func viewBoxPicture(pic *Picture, vb *css.Shape) *Picture {
	if vb == nil || pic.NoNaturalWidth || pic.NoNaturalHeight {
		return pic
	}
	w, h := pic.Width, pic.Height
	l, t := resolve(vb.Inset[css.Left], w), resolve(vb.Inset[css.Top], h)
	vw, vh := w-l-resolve(vb.Inset[css.Right], w), h-t-resolve(vb.Inset[css.Bottom], h)
	if !(vw > 0) || !(vh > 0) {
		return pic
	}
	out := *pic
	out.Width, out.Height = vw, vh
	out.view = &viewBox{x: l, y: t, w: w, h: h}
	return &out
}
