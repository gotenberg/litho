package page

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/limits"
)

// TestFormWithHugeBBox draws a form whose /BBox reaches ±9×10^307, which a
// scale past 72 dpi maps to infinity: the clip still holds the page.
func TestFormWithHugeBBox(t *testing.T) {
	t.Parallel()
	const huge = "-89884656743115785407263711865852178399035283762922498299458738401578630390014269380294779316383439085770229476757191232117160663444732091384233773351768758493024955288275641038122745045194664472037934254227566971152291618451611474082904279666061674137398913102072361584369088590459649940625202013092062429184"
	f := fixture{
		page:    "/Resources << /XObject << /Fm 5 0 R >> >>",
		content: "/Fm Do",
		objs: []string{stream("/Type /XObject /Subtype /Form /BBox ["+huge+" "+huge+" "+huge[1:]+" "+huge[1:]+"]",
			"1 0 0 rg 10 10 80 80 re f")},
	}
	doc, pg := open(t, f.bytes(), limits.Limits{})
	for _, dpi := range []float64{72, 300} {
		opts := f.options()
		opts.DPI = dpi
		c, _, err := Render(context.Background(), doc, pg, opts)
		if err != nil {
			t.Fatalf("Render at %v dpi: %v", dpi, err)
		}
		img := c.Image()
		mid := img.Bounds().Dx() / 2
		if got := at(t, img, mid, mid); got != red {
			t.Errorf("at %v dpi the centre is %v, want red", dpi, got)
		}
	}
}
