package layout

import (
	"context"
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// at returns the colour of pixel (x, y) of img.
func at(img image.Image, x, y int) color.NRGBA {
	return color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
}

func TestBorders_Solid(t *testing.T) {
	root := doc(el(sty(width(2), height(2), bg(blue), border(1, css.Solid, red), margin(1))))
	res := run(t, root, page(6, 6))
	assertPixels(t, res, 1, `
		______
		_RRRR_
		_RBBR_
		_RBBR_
		_RRRR_
		______`)
}

// TestBorders_SolidWithoutASide locks that solid sides of one colour meet
// at their corners with no seam when another side has no width: the
// corner pixels on the diagonal two clipped trapezoids would share are
// fully covered.
func TestBorders_SolidWithoutASide(t *testing.T) {
	root := doc(el(sty(width(4), height(4), border(3, css.Solid, red), func(s *css.Style) {
		s.Border[css.Bottom] = css.Border{}
	})))
	img := render(t, run(t, root, page(12, 9)), 1)
	for i := range 3 {
		for _, x := range []int{i, 9 - i} {
			if c := at(img, x, i); c != (color.NRGBA{255, 0, 0, 255}) {
				t.Errorf("corner pixel (%d, %d) is %v, want pure red", x, i, c)
			}
		}
	}
}

// TestBorders_SolidOfUnequalWidths locks that solid sides of one colour
// and unequal widths meet at their corners with no seam, filled as one
// ring as Chromium fills them: every pixel of the four corner squares is
// fully covered.
func TestBorders_SolidOfUnequalWidths(t *testing.T) {
	root := doc(el(sty(width(4), height(4), border(3, css.Solid, red), func(s *css.Style) {
		s.Border[css.Top].Width = px(5)
		s.Border[css.Bottom].Width = px(5)
	})))
	img := render(t, run(t, root, page(12, 16)), 1)
	for y := range 5 {
		for x := range 3 {
			for _, p := range [][2]int{{x, y}, {9 - x, y}, {x, 13 - y}, {9 - x, 13 - y}} {
				if c := at(img, p[0], p[1]); c != (color.NRGBA{255, 0, 0, 255}) {
					t.Errorf("corner pixel (%d, %d) is %v, want pure red", p[0], p[1], c)
				}
			}
		}
	}
}

func TestBorders_Double(t *testing.T) {
	root := doc(el(sty(width(1), height(1), border(3, css.Double, red))))
	res := run(t, root, page(7, 7))
	assertPixels(t, res, 1, `
		RRRRRRR
		R_____R
		R_RRR_R
		R_R_R_R
		R_RRR_R
		R_____R
		RRRRRRR`)
}

func TestBorders_SideColours(t *testing.T) {
	root := doc(el(sty(width(3), height(1), func(s *css.Style) {
		s.Border[css.Top] = css.Border{Width: px(1), Style: css.Solid, Color: red}
		s.Border[css.Bottom] = css.Border{Width: px(2), Style: css.Solid, Color: blue}
	})))
	res := run(t, root, page(3, 4))
	assertPixels(t, res, 1, `
		RRR
		___
		BBB
		BBB`)
}

func TestBorders_TwoTone(t *testing.T) {
	tests := []struct {
		style       css.BorderStyle
		top, bottom color.NRGBA
	}{
		{css.Inset, color.NRGBA{0, 0, 170, 255}, color.NRGBA{0, 0, 255, 255}},
		{css.Outset, color.NRGBA{0, 0, 255, 255}, color.NRGBA{0, 0, 170, 255}},
		{css.Groove, color.NRGBA{0, 0, 170, 255}, color.NRGBA{0, 0, 255, 255}},
		{css.Ridge, color.NRGBA{0, 0, 255, 255}, color.NRGBA{0, 0, 170, 255}},
	}
	for _, tt := range tests {
		root := doc(el(sty(width(8), height(8), border(4, tt.style, blue))))
		img := render(t, run(t, root, page(16, 16)), 1)
		if got := at(img, 8, 0); !similar(got, tt.top) {
			t.Errorf("style %d: top outer %v, want %v", tt.style, got, tt.top)
		}
		if got := at(img, 8, 15); !similar(got, tt.bottom) {
			t.Errorf("style %d: bottom outer %v, want %v", tt.style, got, tt.bottom)
		}
	}
}

// similar reports whether two colours agree within a few levels.
func similar(a, b color.NRGBA) bool {
	d := func(x, y uint8) bool { return int(x)-int(y) < 6 && int(y)-int(x) < 6 }
	return d(a.R, b.R) && d(a.G, b.G) && d(a.B, b.B)
}

func TestBorders_DashedDotted(t *testing.T) {
	for _, bs := range []css.BorderStyle{css.Dashed, css.Dotted} {
		root := doc(el(sty(width(40), height(4), border(2, bs, red))))
		img := render(t, run(t, root, page(44, 8)), 1)
		red, white := 0, 0
		for x := 4; x < 40; x++ {
			// Round dots off the pixel grid anti-alias: count the
			// mostly red pixels.
			switch c := at(img, x, 0); {
			case c.R > 200 && c.G < 100:
				red++
			case c.G == 255:
				white++
			}
		}
		if red == 0 || white == 0 {
			t.Errorf("style %d: top border has %d red and %d white pixels, want both", bs, red, white)
		}
	}
}

func TestBorderRadius_Clamped(t *testing.T) {
	root := doc(el(sty(width(20), height(10), bg(red), func(s *css.Style) {
		for i := range s.BorderRadius {
			s.BorderRadius[i] = css.Radius{H: css.Pts(px(50)), V: css.Pts(px(50))}
		}
	})))
	rb := boxes(t, root, page(20, 10))
	b := kid(t, rb, 0, 0)
	r := radiiOf(b, b.borderW(), b.borderH())
	if !near(r[0][0]/css.Pt, 5) || !near(r[0][1]/css.Pt, 5) {
		t.Errorf("radii %v px, want 5 by 5: clamped to half the height", [2]float64{r[0][0] / css.Pt, r[0][1] / css.Pt})
	}
	img := render(t, run(t, root, page(20, 10)), 1)
	if code(img.At(0, 0)) != '_' || code(img.At(10, 5)) != 'R' || code(img.At(10, 0)) != 'R' {
		t.Errorf("corner %c centre %c top %c, want white, red, red", code(img.At(0, 0)), code(img.At(10, 5)), code(img.At(10, 0)))
	}
}

func TestBackground_Clip(t *testing.T) {
	root := doc(el(sty(width(2), height(2), bg(red), padding(1), border(1, css.Solid, blue), func(s *css.Style) {
		s.BackgroundClip = css.ContentEdge
	})))
	res := run(t, root, page(6, 6))
	assertPixels(t, res, 1, `
		BBBBBB
		B____B
		B_RR_B
		B_RR_B
		B____B
		BBBBBB`)
}

// imageLoader returns an image loader serving one 2 by 1 px image, its
// left pixel red and its right one blue.
func imageLoader() func(context.Context, string) (*Picture, error) {
	img := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	img.Set(0, 0, color.NRGBA{R: 255, A: 255})
	img.Set(1, 0, color.NRGBA{B: 255, A: 255})
	p := &Picture{Source: source(img), Width: px(2), Height: px(1)}
	return func(context.Context, string) (*Picture, error) { return p, nil }
}

func TestBackground_ImageRepeat(t *testing.T) {
	tests := []struct {
		name string
		mods func(*css.Style)
		want string
	}{
		{"repeat", func(*css.Style) {}, "RBRBRB\nRBRBRB"},
		{"no-repeat", func(s *css.Style) { s.BackgroundRepeat = [2]css.Repeat{css.RepeatNoRepeat, css.RepeatNoRepeat} }, "RB____\n______"},
		{"repeat-x centred", func(s *css.Style) {
			s.BackgroundRepeat = [2]css.Repeat{css.RepeatRepeat, css.RepeatNoRepeat}
			s.BackgroundPosition = [2]css.Dimension{{Kind: css.Percent, Value: 0.5}, css.Pts(px(1))}
		}, "______\nRBRBRB"},
		{"size", func(s *css.Style) {
			s.BackgroundRepeat = [2]css.Repeat{css.RepeatNoRepeat, css.RepeatNoRepeat}
			s.BackgroundSize = css.BackgroundSize{Width: css.Pts(px(4)), Height: css.Dimension{Kind: css.Auto}}
		}, "RRBB__\nRRBB__"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := doc(el(sty(width(6), height(2), tt.mods, func(s *css.Style) { s.BackgroundImage = "i.png" })))
			res, err := Layout(context.Background(), root, ahem(t), Options{Page: page(6, 2), Image: imageLoader()})
			if err != nil {
				t.Fatal(err)
			}
			assertPixels(t, res, 1, tt.want)
		})
	}
}

func TestCanvas_FillsPageArea(t *testing.T) {
	// The canvas paints the page box's border box, the page area, and
	// leaves the page margins blank (CSS Page 3, 3.1).
	root := el(sty(bg(lime)), el(sty(height(1), bg(red))))
	pb := page(4, 4)
	pb.Margin = [4]float64{px(1), px(1), px(1), px(1)}
	res := run(t, root, pb)
	assertPixels(t, res, 1, `
		____
		_RR_
		_GG_
		____`)
	// Without a root background the first child's paints the canvas.
	root = el(sty(), el(sty(bg(lime)), el(sty(height(1), bg(red)))))
	assertPixels(t, run(t, root, pb), 1, `
		____
		_RR_
		_GG_
		____`)
}

// TestCanvas_ImagePositionedInRoot locks CSS Backgrounds 3, 2.11.2: the
// canvas image covers the canvas but is positioned in the root's padding
// box, as if painted for the root alone: a root of margin 1px and border
// 1px starts its no-repeat image 2px in.
func TestCanvas_ImagePositionedInRoot(t *testing.T) {
	root := el(sty(margin(1), border(1, css.Solid, black), func(s *css.Style) {
		s.BackgroundImage = "i.png"
		s.BackgroundRepeat = [2]css.Repeat{css.RepeatNoRepeat, css.RepeatNoRepeat}
	}), el(sty(height(3))))
	res, err := Layout(context.Background(), root, ahem(t), Options{Page: page(8, 7), Image: imageLoader()})
	if err != nil {
		t.Fatal(err)
	}
	img := render(t, res, 1)
	if c := at(img, 2, 2); c != (color.NRGBA{255, 0, 0, 255}) {
		t.Errorf("pixel (2, 2) is %v, want the image's red", c)
	}
	if c := at(img, 3, 2); c != (color.NRGBA{0, 0, 255, 255}) {
		t.Errorf("pixel (3, 2) is %v, want the image's blue", c)
	}
}

// TestBackground_FixedRepeatsOnEveryPage locks CSS Backgrounds 3, 3.5:
// in paged media a fixed image is positioned in the page area and
// repeated on every page: a box two pages tall shows it 1px down on both
// pages.
func TestBackground_FixedRepeatsOnEveryPage(t *testing.T) {
	fixed := func(s *css.Style) {
		s.BackgroundImage = "i.png"
		s.BackgroundAttachment = css.AttachFixed
		s.BackgroundRepeat = [2]css.Repeat{css.RepeatNoRepeat, css.RepeatNoRepeat}
		s.BackgroundPosition = [2]css.Dimension{css.Pts(0), css.Pts(px(1))}
	}
	root := doc(el(sty(height(8), fixed)))
	res, err := Layout(context.Background(), root, ahem(t), Options{Page: page(6, 4), Image: imageLoader()})
	if err != nil {
		t.Fatal(err)
	}
	for n := 1; n <= 2; n++ {
		if c := at(render(t, res, n), 0, 1); c != (color.NRGBA{255, 0, 0, 255}) {
			t.Errorf("page %d pixel (0, 1) is %v, want the image's red", n, c)
		}
	}
}

func TestOpacity_Group(t *testing.T) {
	root := doc(el(sty(width(2), height(2), bg(red), func(s *css.Style) { s.Opacity = 0.5 })))
	img := render(t, run(t, root, page(2, 2)), 1)
	if got := at(img, 1, 1); !similar(got, color.NRGBA{255, 128, 128, 255}) && !similar(got, color.NRGBA{255, 127, 127, 255}) {
		t.Errorf("pixel %v, want half-transparent red", got)
	}
}

// TestOpacity_CompositesAsWhole locks CSS Color 4, 4.1: an element of
// opacity 0.5 composites its content as one group, so where its two
// children overlap the later one shows at half alpha alone, not over the
// earlier one at half alpha.
func TestOpacity_CompositesAsWhole(t *testing.T) {
	half := func(s *css.Style) { s.Opacity = 0.5 }
	root := doc(el(sty(width(4), height(2), rel(0, 0), half),
		square(3, 2, red, abs(0, math.NaN(), math.NaN(), 0)),
		square(3, 2, blue, abs(0, math.NaN(), math.NaN(), 1))))
	img := render(t, run(t, root, page(4, 2)), 1)
	for _, c := range []struct {
		x    int
		want color.NRGBA
	}{{0, color.NRGBA{255, 128, 128, 255}}, {2, color.NRGBA{128, 128, 255, 255}}, {3, color.NRGBA{128, 128, 255, 255}}} {
		if got := at(img, c.x, 1); !similar(got, c.want) {
			t.Errorf("pixel %d: %v, want %v", c.x, got, c.want)
		}
	}
}

// TestOpacity_OneObjectNoGroup locks that a translucent element painting
// one object writes no transparency group: its alpha alone composites the
// same, and the page keeps its bytes.
func TestOpacity_OneObjectNoGroup(t *testing.T) {
	half := func(s *css.Style) { s.Opacity = 0.5 }
	for _, c := range []struct {
		name  string
		root  *Node
		group bool
	}{
		{"one background", doc(el(sty(width(2), height(2), bg(red), half))), false},
		{"two children", doc(el(sty(width(2), half), square(2, 1, red), square(2, 1, blue))), true},
	} {
		t.Run(c.name, func(t *testing.T) {
			res := run(t, c.root, page(2, 2))
			if got := hasTransparency(res.Pages[0].Content); got != c.group {
				t.Errorf("transparency group %v, want %v", got, c.group)
			}
		})
	}
}

// hasTransparency reports whether frags hold a transparency group.
func hasTransparency(frags []typeset.Fragment) bool {
	for _, f := range frags {
		if g, ok := f.(*typeset.Group); ok && (g.Transparency != nil || hasTransparency(g.Content)) {
			return true
		}
	}
	return false
}

func TestOverflowHidden_Clips(t *testing.T) {
	root := doc(el(sty(width(2), height(2), func(s *css.Style) { s.Overflow = css.OverflowHidden }), el(sty(width(4), height(4), bg(red)))))
	res := run(t, root, page(4, 4))
	assertPixels(t, res, 1, `
		RR__
		RR__
		____
		____`)
}

func TestVisibilityHidden_LaidOutNotPainted(t *testing.T) {
	hidden := func(s *css.Style) { s.Visibility = css.Hidden }
	root := doc(el(sty(height(2), bg(red), hidden)), el(sty(height(2), bg(blue))))
	res := run(t, root, page(2, 4))
	assertPixels(t, res, 1, `
		__
		__
		BB
		BB`)
}
