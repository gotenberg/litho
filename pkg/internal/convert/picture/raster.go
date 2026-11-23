package picture

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/imagefile"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// raster places the picture p on a page of its own, tagged as a Figure.
//
// Its size is its pixels at its file's resolution, 96 pixels an inch where
// the file states none or one imagefile.Resolution refuses. Its EXIF or
// TIFF orientation turns it by the transform it is drawn under, its
// samples untouched: the picture is drawn into its stored size, then
// mirrored or turned into the displayed one (EXIF 2.32, 4.6.4, Table 4,
// Orientation).
func (c *converter) raster(ctx context.Context, p *imagefile.Picture) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.pages++
	xdpi, ydpi := p.XDPI, p.YDPI
	if xdpi <= 0 || ydpi <= 0 {
		xdpi, ydpi = imagefile.DefaultDPI, imagefile.DefaultDPI
	}
	w := float64(p.Width) * pointsPerInch / xdpi
	h := float64(p.Height) * pointsPerInch / ydpi
	dw, dh := w, h
	if p.Orientation >= 5 {
		dw, dh = h, w
	}
	pw, ph, r := c.pageFor(dw, dh)
	// A picture its file does not describe is described by its title, as
	// an SVG file's picture, an HTML image and an Office drawing are: the
	// file's own words for it, which a Figure needs (ISO 14289-1, 7.3).
	alt := p.Description
	if alt == "" {
		alt = p.Title
	}
	fig := &typeset.StructElem{Role: "Figure", Alt: alt}
	var frag typeset.Fragment
	if p.Orientation <= 1 {
		frag = &typeset.Image{Rect: r, Source: p.Source, Parent: fig}
	} else {
		s := r.Width / dw
		o := orientation(p.Orientation, w, h)
		m := typeset.Matrix{
			A: float64(s * o.A), B: float64(s * o.B), C: float64(s * o.C), D: float64(s * o.D),
			E: float64(s*o.E) + r.X, F: float64(s*o.F) + r.Y,
		}
		frag = &typeset.Group{Transform: m, Opacity: 1, Content: []typeset.Fragment{
			&typeset.Image{Rect: typeset.Rect{Width: w, Height: h}, Source: p.Source, Parent: fig},
		}}
	}
	return c.addPage(ctx, &typeset.Page{Width: pw, Height: ph, Content: []typeset.Fragment{frag}, Fitted: c.fitted()}, fig)
}

// orientation returns the transform that takes a picture drawn at its
// stored size, w by h, with its top-left corner at the origin, to its
// displayed one, its top-left corner at the origin, for the EXIF
// orientation o (EXIF 2.32, 4.6.4, Table 4): 2 mirrors it left to right,
// 3 turns it a half, 4 mirrors it top to bottom, 5 transposes it, 6 turns
// it a quarter clockwise, 7 transverses it, 8 turns it a quarter
// counter-clockwise. The coordinates run down the page, as the model's
// do.
func orientation(o int, w, h float64) typeset.Matrix {
	switch o {
	case 2:
		return typeset.Matrix{A: -1, D: 1, E: w}
	case 3:
		return typeset.Matrix{A: -1, D: -1, E: w, F: h}
	case 4:
		return typeset.Matrix{A: 1, D: -1, F: h}
	case 5:
		return typeset.Matrix{B: 1, C: 1}
	case 6:
		return typeset.Matrix{B: 1, C: -1, E: h}
	case 7:
		return typeset.Matrix{B: -1, C: -1, E: h, F: w}
	case 8:
		return typeset.Matrix{B: -1, C: 1, F: w}
	}
	return typeset.Identity
}
