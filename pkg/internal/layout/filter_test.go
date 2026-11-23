package layout

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/effect"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

func filters(fs ...css.Filter) func(*css.Style) {
	return func(s *css.Style) { s.Filter = fs }
}

// paintedKinds returns what the first page of res paints, in order, a
// fragment inside a group of opacity under 1/100 and an invisible run
// named invisible.
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
				if v.Invisible {
					prefix = "invisible "
				}
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

func TestFilter_URLNamingNothingIgnoresTheList(t *testing.T) {
	// Filter Effects 1, 6: a reference to no filter element ignores the
	// whole list, the grayscale with it.
	st := sty(width(20), height(20), bg(blue), filters(css.Filter{Kind: css.FilterGrayscale, Amount: 1}, css.Filter{Kind: css.FilterURL, URL: "#none"}))
	res := run(t, doc(el(st)), page(40, 40))
	if got := strings.Join(paintedKinds(res), ", "); got != "path" {
		t.Errorf("painted %s, want the box unfiltered", got)
	}
}

func TestFilter_URLAppliesTheFilterElement(t *testing.T) {
	st := sty(width(20), height(20), margin(10), bg(blue), filters(css.Filter{Kind: css.FilterURL, URL: "#f"}))
	var asked typeset.Rect
	o := Options{Page: page(40, 40), FilterSource: func(_ context.Context, url string, box typeset.Rect) (*FilterRef, bool) {
		if url != "#f" {
			return nil, false
		}
		asked = box
		return &FilterRef{Region: box, Apply: func(ctx context.Context, s *effect.Surface) (*effect.Surface, error) {
			out, err := s.Clone(ctx)
			if err != nil {
				return nil, err
			}
			// Red where the source is.
			for i := 0; i < len(out.Pix); i += 4 {
				out.Pix[i], out.Pix[i+2] = out.Pix[i+3], 0
			}
			return out, nil
		}}, true
	}}
	res := runWith(t, doc(el(st)), o)
	if !near(asked.Width, px(20)) || !near(asked.X, px(10)) {
		t.Errorf("filter asked for box %+v, want the border box", asked)
	}
	if c := at(render(t, res, 1), 20, 20); code(c) != 'R' {
		t.Errorf("filtered box %v, want red", c)
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

// TestFilter_RootFiltersTheCanvas locks CSS Compositing 1, 9.1 with
// Filter Effects 1, 6: the root's filter applies to the root group, which
// holds the root's background on the canvas, as Chromium shows it on
// screen.
func TestFilter_RootFiltersTheCanvas(t *testing.T) {
	root := el(sty(bg(red), filters(css.Filter{Kind: css.FilterGrayscale, Amount: 1})), el(sty(width(10), height(10), bg(blue))))
	img := render(t, run(t, root, page(30, 30)), 1)
	if c := at(img, 20, 20); c.R != c.G || c.G != c.B || c.R > 90 {
		t.Errorf("the canvas %v, want red made a dark grey", c)
	}
	if c := at(img, 5, 5); c.R != c.B || c.B > 30 {
		t.Errorf("the root's content %v, want blue made a dark grey", c)
	}
}

// TestFilter_HiddenTaggedPaintsNothing checks the tagged content kept
// under a filter's image: a run as invisible text, a path inside a clip
// of no area, a group without its opacity, every fragment keeping its
// structure element; what hangs from none is left out.
func TestFilter_HiddenTaggedPaintsNothing(t *testing.T) {
	elem := &typeset.StructElem{Role: "P"}
	run1 := &typeset.Text{Size: 10, Parent: elem, Stroke: 1, Glyphs: []typeset.Glyph{{GID: 1}}}
	path := &typeset.Path{Ops: rectOps(typeset.Rect{Width: 5, Height: 5}), Fill: &typeset.Fill{Color: typeset.Black}, Parent: elem}
	artifact := &typeset.Path{Ops: rectOps(typeset.Rect{Width: 5, Height: 5}), Fill: &typeset.Fill{Color: typeset.Black}}
	out := hiddenTagged([]typeset.Fragment{artifact, &typeset.Group{Opacity: 0.5, Transparency: &typeset.TransparencyGroup{}, Content: []typeset.Fragment{run1, path}}}, 0)
	if len(out) != 1 {
		t.Fatalf("%d fragments, want the group", len(out))
	}
	g := out[0].(*typeset.Group)
	if g.Opacity != 1 || g.Transparency != nil || len(g.Content) != 2 {
		t.Fatalf("group %+v, want opaque, no transparency group, two fragments", g)
	}
	if txt, ok := g.Content[0].(*typeset.Text); !ok || !txt.Invisible || txt.Stroke != 0 || txt.Parent != elem || run1.Invisible {
		t.Errorf("run %+v, want an invisible copy with its parent", g.Content[0])
	}
	clipped, ok := g.Content[1].(*typeset.Group)
	if !ok || clipped.Clip == nil || len(clipped.Content) != 1 || clipped.Content[0] != path {
		t.Fatalf("path kept as %+v, want it inside a clip", g.Content[1])
	}
	x0, y0, x1, y1 := opsBounds(clipped.Clip.Ops)
	if x1 != x0 || y1 != y0 {
		t.Errorf("clip spans %v,%v to %v,%v, want no area", x0, y0, x1, y1)
	}
}

// TestFilter_HoldsAbsolute locks Filter Effects 1, 3: an
// inline box with a filter is the containing block of its absolutely
// positioned child, placed from the inline's fragment, not from the page.
func TestFilter_HoldsAbsolute(t *testing.T) {
	sp := inlineSty(filters(css.Filter{Kind: css.FilterBlur}))
	root := doc(el(sty(width(20)), txt(sty(), "XX"), el(sp, txt(sp, "X"), square(1, 1, blue, abs(0, auto, auto, 0)))))
	b := boxes(t, root, page(20, 20))
	var found *box
	var walk func(*box)
	walk = func(k *box) {
		for _, c := range k.kids {
			if c.abs {
				found = c
			}
			walk(c)
		}
	}
	walk(b)
	if found == nil {
		t.Fatal("no absolutely positioned box")
	}
	if !near(found.x, px(20)) {
		t.Errorf("x = %v px, want 20 px", found.x/css.Pt)
	}
}

// TestFilter_ReducedGridOnWholePixels locks that a filtered element past
// the surface cap is drawn at a whole number of pixels a CSS pixel: a
// 1,500 pixel square takes 2 pixels a CSS pixel, under the 2.7 the cap
// allows, so its edges on whole CSS pixels stay sharp.
func TestFilter_ReducedGridOnWholePixels(t *testing.T) {
	e := &engine{ctx: context.Background(), warned: map[string]bool{}}
	sq := typeset.Rect{Width: px(1500), Height: px(1500)}
	frags := []typeset.Fragment{&typeset.Path{Ops: rectOps(sq), Fill: &typeset.Fill{Color: typeset.Black}}}
	img, err := e.filterImage([]css.Filter{{Kind: css.FilterInvert, Amount: 1}}, frags, sq, nil, 1, &effect.Page{})
	if err != nil || img == nil {
		t.Fatalf("filterImage: %v, %v", img, err)
	}
	if w := img.Source.Width; w%2 != 0 || w < 3000 || w > 3010 {
		t.Errorf("image %d pixels wide, want 2 a CSS pixel over the region", w)
	}
}

// TestFilter_InlineInColumns locks that a filtered inline in a column
// past the first is filtered whole: the column cut moves the column's
// content to its place on the page, so the filter does not cut what lies
// past the page's height in the column flow as content in flow
// coordinates.
func TestFilter_InlineInColumns(t *testing.T) {
	cols := sty(width(100), height(20), func(s *css.Style) {
		s.Columns.Count = 2
		s.ColumnGap = css.Pts(0)
	})
	span := inlineSty(fg(blue), filters(css.Filter{Kind: css.FilterGrayscale, Amount: 1}))
	res := run(t, doc(el(cols, txt(cols, "aaaa aaaa "), el(span, txt(span, "bbbb bbbb")))), page(120, 30))
	img := render(t, res, 1)
	// Grayscale weighs blue by 0.0722 (Filter Effects 1, 13.1).
	for _, p := range [][2]int{{70, 5}, {70, 15}} {
		if c := at(img, p[0], p[1]); c.R != c.G || c.G != c.B || c.R > 30 {
			t.Errorf("filtered blue at %v: %v, want a grey of 18", p, c)
		}
	}
}

// TestFilter_ImageCoversWhatThePageShows locks that a filtered element
// under a transform draws an image of no more than its page shows, mapped
// back through the transform: a child far larger than the page would
// otherwise push the image past the surface cap, onto a grid of less than
// one pixel a CSS pixel, whose edges fall inside the image's pixels and
// land up to a pixel from the element's own.
func TestFilter_ImageCoversWhatThePageShows(t *testing.T) {
	moved := sty(func(s *css.Style) {
		s.Transform = []css.TransformFunc{{Kind: css.TransformTranslate, X: css.Pts(px(1))}}
	})
	filtered := sty(width(40), height(20), bg(blue), filters(css.Filter{Kind: css.FilterGrayscale, Amount: 1}))
	huge := sty(width(px(20000)), height(px(1500)), bg(red))
	res := run(t, doc(el(moved, el(filtered, el(huge)))), page(60, 1200))
	var img *typeset.Image
	var find func([]typeset.Fragment)
	find = func(fs []typeset.Fragment) {
		for _, f := range fs {
			switch v := f.(type) {
			case *typeset.Image:
				img = v
			case *typeset.Group:
				find(v.Content)
			}
		}
	}
	find(res.Pages[0].Content)
	if img == nil {
		t.Fatal("no filtered image")
	}
	if img.Rect.Width > 100 || img.Rect.Height > 1250 {
		t.Errorf("image of %g by %g points, want no more than the page and the filter's reach", img.Rect.Width, img.Rect.Height)
	}
	perPx := float64(img.Source.Width) / img.Rect.Width * css.Pt
	if perPx < 1 || math.Abs(perPx-math.Round(perPx)) > 1e-6 {
		t.Errorf("%g image pixels a CSS pixel, want a whole number", perPx)
	}
}
