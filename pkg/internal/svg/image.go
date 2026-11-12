package svg

import (
	"fmt"
	"strings"

	"golang.org/x/net/html"

	"github.com/gotenberg/litho/pkg/internal/compose"
)

// image draws an image element styled st (SVG 1.1, 5.7): the raster
// picture its href names, through [Options.Image], fitted into x, y,
// width and height by preserveAspectRatio. A missing width or height is
// the picture's own (SVG 2, 5.4.6, auto), the other one keeping its
// ratio. A sliced picture is clipped to the rectangle.
func (r *renderer) image(n *html.Node, st *style, s sink) {
	if !st.visible {
		return
	}
	ref := strings.TrimSpace(href(n))
	if ref == "" {
		return
	}
	pic := r.raster(ref)
	if pic == nil {
		return
	}
	x, y := r.length(n, "x", st, axisX, 0), r.length(n, "y", st, axisY, 0)
	wl, wok := parseLength(attrOf(n, "width"))
	hl, hok := parseLength(attrOf(n, "height"))
	w, h := pic.Width, pic.Height
	switch {
	case wok && hok:
		w, h = wl.resolve(r.lc(st), axisX), hl.resolve(r.lc(st), axisY)
	case wok:
		w = wl.resolve(r.lc(st), axisX)
		h = w * pic.Height / pic.Width
	case hok:
		h = hl.resolve(r.lc(st), axisY)
		w = h * pic.Width / pic.Height
	}
	if !positive(w) || !positive(h) {
		return
	}
	par := parseAspect(attrOf(n, "preserveAspectRatio"))
	rect := fitRect(pic.Width, pic.Height, par, x, y, w, h)
	inner, done := r.open(s, n, st, nil)
	defer done()
	if inner.alpha < 1 {
		// An image carries no alpha of its own: a translucent one needs a
		// group, which the nesting cap has run out of.
		r.d.warn("svg-limit", "a translucent image nested too deep is drawn opaque")
	}
	img := &compose.Image{Rect: rect, Source: pic.Source}
	if par.slice && !par.none {
		c := &container{}
		r.emit(sink{c: c, pending: identity, alpha: 1}, img)
		g := &compose.Group{Clip: &compose.Path{Ops: rectOps(x, y, w, h), Fill: &compose.Fill{}}, Opacity: 1, Content: c.out}
		r.emit(inner, g)
		return
	}
	r.emit(inner, img)
}

// raster loads the picture ref names, once per reference.
func (r *renderer) raster(ref string) *Raster {
	d := r.d
	d.mu.Lock()
	pic, done := d.images[ref]
	d.mu.Unlock()
	if done {
		return pic
	}
	if d.opts.Image == nil {
		d.warn("svg-image", "image elements are not drawn: no image loader")
	} else {
		p, err := d.opts.Image(r.ctx, ref)
		switch {
		case err != nil:
			d.warn("svg-image", fmt.Sprintf("an image element was left out: %v", err))
		case p == nil || p.Source == nil || !positive(p.Width) || !positive(p.Height):
			d.warn("svg-image", "an image element was left out: the picture has no size")
		default:
			pic = p
		}
	}
	d.mu.Lock()
	d.images[ref] = pic
	d.mu.Unlock()
	return pic
}
