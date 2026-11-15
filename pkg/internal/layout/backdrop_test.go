package layout

import (
	"math"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

func backdrop(fs ...css.Filter) func(*css.Style) {
	return func(s *css.Style) { s.BackdropFilter = fs }
}

var invert = css.Filter{Kind: css.FilterInvert, Amount: 1}

// backdropWarnings returns the warnings of res about backdrop filters.
func backdropWarnings(res *Result) []string {
	var out []string
	for _, w := range res.Warnings {
		if strings.Contains(w.Message, "backdrop") {
			out = append(out, w.Message)
		}
	}
	return out
}

// TestBackdropFilter_FiltersWhatLiesBehind locks Filter Effects 2, 2.1:
// the backdrop, filtered, clipped to the border box, drawn under the
// element's background; what lies outside the box stays as it was. The
// backdrop is an artifact.
func TestBackdropFilter_FiltersWhatLiesBehind(t *testing.T) {
	glass := square(20, 20, css.Color{B: 1, A: 0.5}, abs(10, auto, auto, 10), backdrop(invert))
	res := run(t, doc(square(30, 15, red), glass), page(60, 60))
	if got := strings.Join(paintedKinds(res), ", "); got != "path, image, path" {
		t.Errorf("painted %s, want the red box, the backdrop, the glass's background", got)
	}
	if w := backdropWarnings(res); len(w) > 0 {
		t.Errorf("warnings %q, want none", w)
	}
	var walk func([]typeset.Fragment)
	walk = func(fs []typeset.Fragment) {
		for _, f := range fs {
			switch v := f.(type) {
			case *typeset.Image:
				if v.Parent != nil {
					t.Errorf("backdrop image hangs from %s, want an artifact", v.Parent.Role)
				}
			case *typeset.Group:
				walk(v.Content)
			}
		}
	}
	walk(res.Pages[0].Content)
	img := render(t, res, 1)
	// Red inverted is cyan; half blue over it.
	if c := at(img, 15, 12); c.R > 2 || c.G < 125 || c.G > 130 || c.B < 253 {
		t.Errorf("inside the box over red: %v, want half blue over cyan", c)
	}
	// White inverted is black; half blue over it.
	if c := at(img, 25, 25); c.R > 2 || c.G > 2 || c.B < 125 || c.B > 130 {
		t.Errorf("inside the box below the red: %v, want half blue over black", c)
	}
	if c := at(img, 5, 12); code(c) != 'R' {
		t.Errorf("left of the box: %v, want red unfiltered", c)
	}
}

// TestBackdropFilter_BackdropRoot locks Filter Effects 2, 3: the backdrop
// starts at the nearest Backdrop Root around the element, an opacity, a
// filter, a clip-path or a blend mode, and holds what that root painted
// before it; a transform or a z-index makes none.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156's screenshot inverts the red under an
//     element of backdrop-filter: invert(1) inside a container that is
//     translated or has a z-index, and leaves it red inside one of
//     opacity 0.999, filter: blur(0), clip-path: inset(0) or
//     mix-blend-mode: multiply.
func TestBackdropFilter_BackdropRoot(t *testing.T) {
	for _, tc := range []struct {
		name string
		mod  func(*css.Style)
		want byte
	}{
		{"opacity", func(s *css.Style) { s.Opacity = 0.999 }, 'R'},
		{"filter", filters(css.Filter{Kind: css.FilterBlur}), 'R'},
		{"blend", blendMode(css.BlendMultiply), 'R'},
		{"transform", func(s *css.Style) {
			s.Transform = []css.TransformFunc{{Kind: css.TransformTranslate, X: css.Pts(px(1))}}
		}, 'c'},
		{"z-index", zIndex(1), 'c'},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inner := square(10, 10, css.Color{}, abs(5, auto, auto, 5), backdrop(invert))
			box := el(sty(width(20), height(20), abs(0, auto, auto, 0), tc.mod), inner)
			img := render(t, run(t, doc(square(40, 40, red), box), page(40, 40)), 1)
			got := code(at(img, 10, 10))
			if c := at(img, 10, 10); c.R < 2 && c.G > 253 && c.B > 253 {
				got = 'c'
			}
			if got != tc.want {
				t.Errorf("inside the element: %v, want %c", at(img, 10, 10), tc.want)
			}
		})
	}
}

// TestBackdropFilter_RootHoldsTheCanvas locks Filter Effects 2, 3 at the
// root element, with the white surface CSS Compositing 1, 8.4 lets a user
// agent put under the root group: a page that paints no background
// inverts to black, one whose root paints red to cyan.
func TestBackdropFilter_RootHoldsTheCanvas(t *testing.T) {
	glass := square(20, 20, css.Color{}, backdrop(invert))
	if c := at(render(t, run(t, doc(glass), page(40, 40)), 1), 10, 10); code(c) != 'K' {
		t.Errorf("over a page with no background: %v, want black", c)
	}
	root := el(sty(bg(red)), el(sty(), square(20, 20, css.Color{}, backdrop(invert))))
	if c := at(render(t, run(t, root, page(40, 40)), 1), 10, 10); c.R > 2 || c.G < 253 || c.B < 253 {
		t.Errorf("over a red canvas: %v, want cyan", c)
	}
}

// TestBackdropFilter_BlurMirrorsTheEdge locks the edge of Filter Effects
// 2, 2.1: a blur reads past the border box by mirroring it. The box's
// left 10px lie over black; at its left edge a blur(10px) mirrored reads
// black over 20px around it, where duplicating the edge would read
// black to the left for ever and darken it twice as much.
func TestBackdropFilter_BlurMirrorsTheEdge(t *testing.T) {
	glass := square(100, 40, css.Color{}, abs(10, auto, auto, 40), backdrop(css.Filter{Kind: css.FilterBlur, Amount: px(10)}))
	img := render(t, run(t, doc(square(50, 60, black), glass), page(160, 60)), 1)
	if c := at(img, 40, 30); c.R < 70 || c.R > 90 {
		t.Errorf("at the box's left edge: %v, want the mirror's grey of about 80", c)
	}
	if c := at(img, 140, 30); c.R < 254 {
		t.Errorf("at the box's right edge: %v, want white", c)
	}
}

// TestBackdropFilter_ClippedToTheRadii locks the clip of Filter Effects 2,
// 2.1: the border box with its border radii.
func TestBackdropFilter_ClippedToTheRadii(t *testing.T) {
	round := func(s *css.Style) {
		for i := range s.BorderRadius {
			s.BorderRadius[i] = css.Radius{H: css.Pts(px(10)), V: css.Pts(px(10))}
		}
	}
	glass := square(20, 20, css.Color{}, abs(0, auto, auto, 0), round, backdrop(invert))
	img := render(t, run(t, doc(square(30, 30, red), glass), page(30, 30)), 1)
	if c := at(img, 0, 0); code(c) != 'R' {
		t.Errorf("in the rounded corner: %v, want red", c)
	}
	if c := at(img, 10, 10); c.R > 2 || c.G < 253 {
		t.Errorf("in the middle: %v, want cyan", c)
	}
}

// TestBackdropFilter_NestedReadsItsRoot locks the Backdrop Root Image of
// an element inside another of backdrop-filter: what the outer element
// painted, its background, and not the outer element's filtered
// backdrop.
//
// # Corpus evidence
//
//   - probe, html: in Chromium 156's screenshot, a box of
//     backdrop-filter: blur(8px) inside one of backdrop-filter:
//     invert(1) and 10% black adds another 10% black over the outer
//     box's sharp inverted stripes.
func TestBackdropFilter_NestedReadsItsRoot(t *testing.T) {
	inner := square(10, 10, css.Color{}, abs(5, auto, auto, 5), backdrop(invert))
	outer := el(sty(width(20), height(20), abs(0, auto, auto, 0), bg(css.Color{B: 1, A: 0.5}), backdrop(invert)), inner)
	img := render(t, run(t, doc(square(30, 30, red), outer), page(30, 30)), 1)
	// The outer box: cyan under half blue.
	if c := at(img, 2, 2); c.R > 2 || c.G < 125 || c.G > 130 || c.B < 253 {
		t.Errorf("the outer box: %v, want half blue over cyan", c)
	}
	// The inner box: the outer box's half blue inverted, half yellow,
	// over the outer box's rendering, (127, 191, 127). Had the backdrop
	// held the outer box's cyan, it would invert to an opaque orange.
	if c := at(img, 10, 10); c.R < 125 || c.R > 130 || c.G < 189 || c.G > 194 || c.B < 125 || c.B > 130 {
		t.Errorf("the inner box: %v, want half yellow over the outer box", c)
	}
}

// TestBackdropFilter_BlendsWithItsBackdrop locks Filter Effects 2, 2.1:
// the element is drawn into the buffer that holds its filtered backdrop,
// which composites with the page by source-over, so its blend mode
// blends it with the filtered backdrop.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156's screenshot draws a box of
//     backdrop-filter: blur(6px), mix-blend-mode: difference and a
//     background of 30% blue over stripes as the blurred stripes, then
//     the blue in difference with them, no stripe left sharp.
func TestBackdropFilter_BlendsWithItsBackdrop(t *testing.T) {
	glass := square(20, 20, blue, abs(0, auto, auto, 0), blendMode(css.BlendDifference), backdrop(css.Filter{Kind: css.FilterOpacity, Amount: 1}))
	img := render(t, run(t, doc(square(30, 30, red), glass), page(30, 30)), 1)
	// Blue in difference with the red backdrop is magenta; with the page,
	// the same here, so a second, sharper check below.
	if c := at(img, 10, 10); c.R < 253 || c.G > 2 || c.B < 253 {
		t.Errorf("blue in difference with red: %v, want magenta", c)
	}
	glass = square(20, 20, blue, abs(0, auto, auto, 0), blendMode(css.BlendDifference), backdrop(invert))
	img = render(t, run(t, doc(square(30, 30, red), glass), page(30, 30)), 1)
	// Blue in difference with cyan, the inverted red, is green; with the
	// red page it would be magenta.
	if c := at(img, 10, 10); c.R > 2 || c.G < 253 || c.B > 2 {
		t.Errorf("blue in difference with the inverted red: %v, want green", c)
	}
}

// TestBackdropFilter_EachPageItsSlice locks an element split across pages:
// each fragment filters its own backdrop (Filter Effects 2, 2.1, with CSS
// Fragmentation 3, 5.4), an empty element included.
func TestBackdropFilter_EachPageItsSlice(t *testing.T) {
	glass := el(sty(width(20), height(50), margin(0, 0, 0, 0), backdrop(invert)))
	box := el(sty(bg(red)), square(30, 10, css.Color{}), glass)
	res := run(t, doc(box), page(30, 40))
	if len(res.Pages) != 2 {
		t.Fatalf("%d pages, want 2", len(res.Pages))
	}
	for _, pt := range [][2]int{{1, 20}, {2, 10}} {
		if c := at(render(t, res, pt[0]), 10, pt[1]); c.R > 2 || c.G < 253 || c.B < 253 {
			t.Errorf("page %d: %v, want the red inverted to cyan", pt[0], c)
		}
	}
}

// TestBackdropFilter_InlineWarns locks what is left out: an inline box's
// backdrop, printed unfiltered with one warning.
func TestBackdropFilter_InlineWarns(t *testing.T) {
	span := el(sty(display(css.Inline), backdrop(invert)), txt(sty(), "a"))
	res := run(t, doc(el(sty(bg(red)), span)), page(40, 40))
	w := backdropWarnings(res)
	if len(w) != 1 || !strings.Contains(w[0], "inline") {
		t.Errorf("warnings %q, want one about the inline box", w)
	}
}

// TestBackdropFilter_PageCap locks [maxPageBackdrops]: past it an element
// prints its backdrop unfiltered, with one warning.
func TestBackdropFilter_PageCap(t *testing.T) {
	var kids []*Node
	for range maxPageBackdrops + 2 {
		kids = append(kids, square(2, 1, css.Color{}, backdrop(invert)))
	}
	res := run(t, doc(el(sty(bg(red)), kids...)), page(10, 60))
	images := 0
	for _, k := range paintedKinds(res) {
		if k == "image" {
			images++
		}
	}
	if images != maxPageBackdrops {
		t.Errorf("%d backdrop images, want %d", images, maxPageBackdrops)
	}
	w := backdropWarnings(res)
	if len(w) != 1 || res.Warnings[0].Code != warn.LimitReached {
		t.Errorf("warnings %q, want one limit", w)
	}
}

// TestBackdropFilter_LengthsOnThePage locks the order of Filter Effects 2,
// 2.1: the filter applies to the backdrop on the page, before the inverse
// of the element's transforms, so its lengths are the page's.
func TestBackdropFilter_LengthsOnThePage(t *testing.T) {
	fs := []css.Filter{
		{Kind: css.FilterBlur, Amount: 8},
		{Kind: css.FilterDropShadow, Shadow: css.Shadow{X: 4, Y: 2, Blur: 6}},
		{Kind: css.FilterGrayscale, Amount: 1},
	}
	got, ok := pageFilters(fs, typeset.Matrix{A: 2, D: 2, E: 30})
	if !ok {
		t.Fatal("refused a scale of 2")
	}
	if got[0].Amount != 4 || got[1].Shadow != (css.Shadow{X: 2, Y: 1, Blur: 3}) || got[2] != fs[2] {
		t.Errorf("under a scale of 2: %+v, want the lengths halved", got)
	}
	turn := typeset.Matrix{A: 0, B: 1, C: -1, D: 0}
	got, _ = pageFilters(fs, turn)
	if math.Abs(got[0].Amount-8) > 1e-12 || math.Abs(got[1].Shadow.X-2) > 1e-12 || math.Abs(got[1].Shadow.Y+4) > 1e-12 {
		t.Errorf("under a quarter turn: %+v, want the blur kept and the offset turned back", got)
	}
	if _, ok := pageFilters(fs, typeset.Matrix{A: 1}); ok {
		t.Error("accepted a singular matrix")
	}
}

// TestBackdropFilter_SameBytesOnEveryArchitecture pins the backdrop's
// image: the drawing, the filters and the encoding give the same bytes
// on every machine.
func TestBackdropFilter_SameBytesOnEveryArchitecture(t *testing.T) {
	glass := square(40, 30, css.Color{R: 1, G: 1, B: 1, A: 0.3}, abs(5, auto, auto, 10), backdrop(
		css.Filter{Kind: css.FilterBlur, Amount: px(3)},
		css.Filter{Kind: css.FilterHueRotate, Amount: 40},
		css.Filter{Kind: css.FilterSaturate, Amount: 1.5},
	))
	stripes := []*Node{square(60, 10, red), square(60, 10, blue), square(60, 10, lime), square(60, 10, black)}
	got := shadowImages(t, run(t, doc(append(stripes, glass)...), page(60, 40)))
	want := []string{"40x30 527655c7ee8844e0"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("backdrop images:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// TestBackdropFilter_HoldsFixedAndAbsolute locks Filter Effects 2, 2.1: a
// backdrop filter makes its element the containing block of its absolute
// and fixed descendants, which the page would otherwise place.
func TestBackdropFilter_HoldsFixedAndAbsolute(t *testing.T) {
	fixed := func(s *css.Style) { s.Position = css.Fixed; s.Top = css.Pts(0) }
	for _, mod := range []func(*css.Style){fixed, abs(0, auto, auto, 0)} {
		kid := square(5, 5, blue, mod)
		root := doc(square(10, 10, red), el(sty(width(20), height(20), margin(0, 0, 0, 10), backdrop(invert)), kid))
		b := boxes(t, root, page(40, 40))
		var found *box
		var walk func(*box)
		walk = func(k *box) {
			for _, c := range k.kids {
				if c.st.Position == css.Fixed || c.abs {
					found = c
				}
				walk(c)
			}
		}
		walk(b)
		if found == nil {
			t.Fatal("no positioned box")
		}
		if !near(found.y, px(10)) || !near(found.x, px(10)) {
			t.Errorf("%v box at (%v, %v) px, want (10, 10), the element's corner", found.st.Position, found.x/css.Pt, found.y/css.Pt)
		}
	}
}
