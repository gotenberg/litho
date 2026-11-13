package layout

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

func filters(fs ...css.Filter) func(*css.Style) {
	return func(s *css.Style) { s.Filter = fs }
}

// paintedKinds returns what the first page of res paints, in order, a
// fragment inside a group of opacity under 1/100 named invisible.
func paintedKinds(res *Result) []string {
	var kinds []string
	var walk func([]typeset.Fragment, float64)
	walk = func(fs []typeset.Fragment, opacity float64) {
		for _, f := range fs {
			prefix := ""
			if opacity < 0.01 {
				prefix = "invisible "
			}
			switch v := f.(type) {
			case *typeset.Image:
				kinds = append(kinds, prefix+"image")
			case *typeset.Text:
				kinds = append(kinds, prefix+"text")
			case *typeset.Path:
				kinds = append(kinds, prefix+"path")
			case *typeset.Group:
				walk(v.Content, opacity*v.Opacity)
			}
		}
	}
	walk(res.Pages[0].Content, 1)
	return kinds
}

func TestFilter_ElementBecomesOneImage(t *testing.T) {
	st := sty(width(40), height(20), margin(10), bg(red), filters(css.Filter{Kind: css.FilterGrayscale, Amount: 1}))
	res := run(t, doc(el(st, txt(sty(), "a"))), page(60, 40))
	if got := strings.Join(paintedKinds(res), ", "); got != "image, invisible text" {
		t.Errorf("painted %s, want the filtered image, then the text invisible", got)
	}
	img := render(t, res, 1)
	// Grayscale weighs red by 0.2126 (Filter Effects 1, 13.1).
	if c := at(img, 40, 20); c.R != c.G || c.G != c.B || c.R < 50 || c.R > 58 {
		t.Errorf("filtered red: %v, want a grey of 54", c)
	}
}

func TestFilter_BlurSpreadsPastTheBox(t *testing.T) {
	st := sty(width(20), height(20), margin(20), bg(blue), filters(css.Filter{Kind: css.FilterBlur, Amount: px(4)}))
	img := render(t, run(t, doc(el(st)), page(60, 60)), 1)
	if c := at(img, 17, 30); c.R > 250 || c.R < 120 {
		t.Errorf("3px out of the box: %v, want a partly blue white", c)
	}
	if c := at(img, 30, 30); c.R > 5 || c.B < 250 {
		t.Errorf("in the box: %v, want blue", c)
	}
}

func TestFilter_DropShadowUnderTheContent(t *testing.T) {
	st := sty(width(20), height(20), margin(10), bg(blue),
		filters(css.Filter{Kind: css.FilterDropShadow, Shadow: css.Shadow{X: px(10), Y: px(10), Color: red}}))
	img := render(t, run(t, doc(el(st)), page(50, 50)), 1)
	for _, c := range []struct {
		x, y int
		want byte
	}{{15, 15, 'B'}, {25, 25, 'B'}, {35, 35, 'R'}, {35, 15, '_'}, {15, 35, '_'}} {
		if got := code(at(img, c.x, c.y)); got != c.want {
			t.Errorf("(%d, %d): %c, want %c", c.x, c.y, got, c.want)
		}
	}
}

func TestFilter_AppliesBeforeOpacity(t *testing.T) {
	// Filter Effects 1, 6: the filter, then clipping, masking and
	// opacity. Inverting the white box then halving its opacity gives
	// grey; the other order would invert the half-white over the page.
	st := sty(width(20), height(20), margin(10), bg(css.Color{R: 1, G: 1, B: 1, A: 1}),
		filters(css.Filter{Kind: css.FilterInvert, Amount: 1}), func(s *css.Style) { s.Opacity = 0.5 })
	img := render(t, run(t, doc(el(st)), page(40, 40)), 1)
	if c := at(img, 20, 20); c.R < 120 || c.R > 135 {
		t.Errorf("inverted then half opaque: %v, want a mid grey", c)
	}
}

func TestFilter_URLDrawsUnfilteredWithAWarning(t *testing.T) {
	st := sty(width(20), height(20), bg(blue), filters(css.Filter{Kind: css.FilterURL, URL: "#none"}))
	res := run(t, doc(el(st)), page(40, 40))
	if got := strings.Join(paintedKinds(res), ", "); got != "path" {
		t.Errorf("painted %s, want the box unfiltered", got)
	}
	n := 0
	for _, w := range res.Warnings {
		if w.Code == warn.CSSUnsupported && strings.Contains(w.Message, "unfiltered") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("warnings %+v, want one for the unfiltered element", res.Warnings)
	}
}

func TestFilter_SameBytesOnEveryArchitecture(t *testing.T) {
	st := sty(width(40), height(30), margin(20), bg(blue), border(2, css.Solid, lime),
		filters(
			css.Filter{Kind: css.FilterBlur, Amount: px(1.5)},
			css.Filter{Kind: css.FilterHueRotate, Amount: 33},
			css.Filter{Kind: css.FilterDropShadow, Shadow: css.Shadow{X: px(3), Y: px(-2), Blur: px(2), Color: css.Color{R: 0.5, A: 0.7}}},
			css.Filter{Kind: css.FilterContrast, Amount: 1.4},
			css.Filter{Kind: css.FilterOpacity, Amount: 0.8},
		))
	got := shadowImages(t, run(t, doc(el(st, txt(sty(), "ab"))), page(100, 100)))
	want := []string{
		"240x210 94b938b647fd4b0d",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("filter images:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestBackdropFilter_PrintsTheElementWithOneWarning(t *testing.T) {
	glass := sty(width(20), height(20), bg(css.Color{R: 1, G: 1, B: 1, A: 0.5}), func(s *css.Style) {
		s.BackdropFilter = []css.Filter{{Kind: css.FilterBlur, Amount: px(10)}}
	})
	res := run(t, doc(el(sty(bg(blue)), el(glass), el(glass))), page(40, 60))
	if got := strings.Join(paintedKinds(res), ", "); got != "path, path, path" {
		t.Errorf("painted %s, want the backdrop and the two boxes as paths", got)
	}
	n := 0
	for _, w := range res.Warnings {
		if strings.Contains(w.Message, "backdrop-filter") {
			n++
			if w.Effect != warn.Approximated {
				t.Errorf("warning effect %v, want approximated", w.Effect)
			}
		}
	}
	if n != 1 {
		t.Errorf("%d backdrop-filter warnings in %+v, want one a document", n, res.Warnings)
	}
}
