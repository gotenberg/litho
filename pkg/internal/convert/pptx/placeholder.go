package pptx

import (
	"github.com/gotenberg/litho/pkg/internal/compose"
)

// defaultPlaceholderBox returns the box of a placeholder that neither the
// slide, its layout nor its master places: the area Impress gives the
// presentation object of its kind on a slide of size page.
func defaultPlaceholderBox(p placeholder, page compose.Rect) compose.Rect {
	w, h := page.Width, page.Height
	switch p.class() {
	case "title":
		return compose.Rect{X: w * 0.05, Y: h * 0.04, Width: w * 0.9, Height: h * 0.17}
	case "dt", "ftr", "sldNum", "hdr":
		return compose.Rect{X: w * 0.05, Y: h * 0.91, Width: w * 0.9, Height: h * 0.07}
	}
	return compose.Rect{X: w * 0.05, Y: h * 0.25, Width: w * 0.9, Height: h * 0.66}
}
