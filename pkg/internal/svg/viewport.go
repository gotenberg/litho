package svg

import (
	"strings"

	"golang.org/x/net/html"

	"github.com/gotenberg/litho/pkg/internal/compose"
)

// viewBox is a viewBox attribute (SVG 1.1, 7.7).
type viewBox struct {
	x, y, w, h float64
	ok         bool
}

// parseViewBox parses a viewBox: four numbers, the width and height
// positive. A zero or negative size is an error that disables the
// attribute (SVG 1.1, 7.7) for litho, as for browsers, which then draw
// the element as if it had none.
func parseViewBox(s string) viewBox {
	v, ok := parseNumberList(s, 4)
	if !ok || len(v) != 4 || !positive(v[2]) || !positive(v[3]) {
		return viewBox{}
	}
	return viewBox{x: v[0], y: v[1], w: v[2], h: v[3], ok: true}
}

// aspect is a preserveAspectRatio value (SVG 1.1, 7.8): the alignment of
// each axis, 0 for min, 1 for mid and 2 for max, or none, and meet or
// slice.
type aspect struct {
	none   bool
	ax, ay int
	slice  bool
}

// defaultAspect is xMidYMid meet, the initial value.
var defaultAspect = aspect{ax: 1, ay: 1}

// parseAspect parses preserveAspectRatio: an optional defer, which only
// matters for image elements referencing SVG and is ignored, an alignment
// and an optional meet or slice. An invalid value gives the initial one.
func parseAspect(s string) aspect {
	f := strings.Fields(s)
	if len(f) > 0 && f[0] == "defer" {
		f = f[1:]
	}
	if len(f) == 0 || len(f) > 2 {
		return defaultAspect
	}
	a := aspect{}
	switch {
	case f[0] == "none":
		a.none = true
	case len(f[0]) == 8 && f[0][0] == 'x' && f[0][4] == 'Y':
		x, okx := alignOf(f[0][1:4])
		y, oky := alignOf(f[0][5:8])
		if !okx || !oky {
			return defaultAspect
		}
		a.ax, a.ay = x, y
	default:
		return defaultAspect
	}
	if len(f) == 2 {
		switch f[1] {
		case "meet":
		case "slice":
			a.slice = true
		default:
			return defaultAspect
		}
	}
	return a
}

// alignOf maps Min, Mid and Max to 0, 1 and 2.
func alignOf(s string) (int, bool) {
	switch s {
	case "Min":
		return 0, true
	case "Mid":
		return 1, true
	case "Max":
		return 2, true
	}
	return 0, false
}

// viewBoxTransform returns the transform that maps the viewBox vb onto a
// viewport of w by h at the origin as par asks (SVG 1.1, 7.8): scaled
// apart with none, else uniformly by the smaller scale for meet or the
// larger for slice, and aligned. It reports false for an empty viewport.
func viewBoxTransform(vb viewBox, par aspect, w, h float64) (compose.Matrix, bool) {
	if !positive(w) || !positive(h) || !vb.ok {
		return identity, false
	}
	sx, sy := w/vb.w, h/vb.h
	if par.none {
		return mul(scale(sx, sy), translate(-vb.x, -vb.y)), true
	}
	s := min(sx, sy)
	if par.slice {
		s = max(sx, sy)
	}
	tx := (w - vb.w*s) * float64(par.ax) / 2
	ty := (h - vb.h*s) * float64(par.ay) / 2
	return mul(translate(tx, ty), mul(scale(s, s), translate(-vb.x, -vb.y))), true
}

// fitRect returns where content of size cw by ch lands in the rectangle
// (x, y, w, h) under par, for an image element (SVG 1.1, 7.8 and 14.7).
func fitRect(cw, ch float64, par aspect, x, y, w, h float64) compose.Rect {
	if par.none || !positive(cw) || !positive(ch) {
		return compose.Rect{X: x, Y: y, Width: w, Height: h}
	}
	s := min(w/cw, h/ch)
	if par.slice {
		s = max(w/cw, h/ch)
	}
	rw, rh := cw*s, ch*s
	return compose.Rect{
		X: x + (w-rw)*float64(par.ax)/2, Y: y + (h-rh)*float64(par.ay)/2,
		Width: rw, Height: rh,
	}
}

// rootGeometry reads the natural size, the viewBox and the
// preserveAspectRatio of the root svg element (SVG 2, 8.2; CSS Images 3,
// 4.1): an absolute width or height is natural, a percentage or a missing
// one is not; the ratio comes from the viewBox, or from both dimensions.
// The em unit resolves against the 16 pixel default font size. A negative
// width or height is an error that disables rendering (SVG 1.1, 7.9),
// which disabled reports.
func rootGeometry(root *html.Node) (s Size, vb viewBox, par aspect, disabled bool) {
	lc := lengthCtx{fontSize: 16}
	for _, k := range [2]string{"width", "height"} {
		if l, ok := parseLength(attrOf(root, k)); ok && l.v < 0 {
			disabled = true
		}
	}
	if l, ok := parseLength(attrOf(root, "width")); ok && l.u != unitPercent && l.v >= 0 {
		s.Width, s.HasWidth = l.resolve(lc, axisX)*ptPerPx, true
	}
	if l, ok := parseLength(attrOf(root, "height")); ok && l.u != unitPercent && l.v >= 0 {
		s.Height, s.HasHeight = l.resolve(lc, axisY)*ptPerPx, true
	}
	vb = parseViewBox(attrOf(root, "viewBox"))
	switch {
	case vb.ok:
		s.Ratio = vb.w / vb.h
	case s.HasWidth && s.HasHeight && s.Width > 0 && s.Height > 0:
		s.Ratio = s.Width / s.Height
	}
	return s, vb, parseAspect(attrOf(root, "preserveAspectRatio")), disabled
}
