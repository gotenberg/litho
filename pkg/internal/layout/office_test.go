package layout

import (
	"context"
	"image"
	"image/color"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
)

// TestOffice_SectionBreaks starts Office sections on odd and even pages:
// break-before and break-after of right and left between named pages, a
// blank page where the side calls for one.
func TestOffice_SectionBreaks(t *testing.T) {
	after := func(v css.Break) func(*css.Style) { return func(s *css.Style) { s.BreakAfter = v } }
	tests := []struct {
		name  string
		root  *Node
		names []string
		blank []bool
	}{
		{"odd section start after an odd page", doc(el(sty(height(5), named("a"))), el(sty(height(5), named("b"), brk(css.RightPage)))), []string{"a", "b", "b"}, []bool{false, true, false}},
		{"even section start after an odd page", doc(el(sty(height(5), named("a"))), el(sty(height(5), named("b"), brk(css.LeftPage)))), []string{"a", "b"}, []bool{false, false}},
		{"break after right", doc(el(sty(height(5), named("a"), after(css.RightPage))), el(sty(height(5), named("b")))), []string{"a", "b", "b"}, []bool{false, true, false}},
		{"break after left", doc(el(sty(height(5), named("a"), after(css.LeftPage))), el(sty(height(5), named("b")))), []string{"a", "b"}, []bool{false, false}},
		{"even section start after an even page", doc(el(sty(height(40), named("a"))), el(sty(height(5), named("b"), brk(css.LeftPage)))), []string{"a", "a", "b", "b"}, []bool{false, false, true, false}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, seen := namedRun(t, tt.root)
			if len(res.Pages) != len(tt.names) {
				t.Fatalf("%d pages, want %d", len(res.Pages), len(tt.names))
			}
			for i, want := range tt.names {
				if seen[i].Name != want || seen[i].Blank != tt.blank[i] {
					t.Errorf("page %d named %q blank %v, want %q %v", i+1, seen[i].Name, seen[i].Blank, want, tt.blank[i])
				}
			}
		})
	}
}

// TestOffice_DoubleStrike draws a double line-through, Word's dstrike, as
// two lines over the glyphs.
func TestOffice_DoubleStrike(t *testing.T) {
	u := sty(func(s *css.Style) {
		s.TextDecoration = css.LineThrough
		s.TextDecorationStyle = css.DecorationDouble
		s.TextDecorationColor = lime
		s.TextDecorationThickness = css.Pts(px(1))
	})
	res := run(t, doc(el(u, txt(sty(fg(css.Color{R: 1, G: 1, B: 1, A: 1})), "a"))), page(10, 14))
	var rows []int
	for r, row := range grid(render(t, res, 1)) {
		if strings.Contains(row, "G") {
			rows = append(rows, r)
		}
	}
	if len(rows) != 2 || rows[1]-rows[0] != 2 || rows[1] >= 8 {
		t.Errorf("lime rows %v, want two a pixel apart above the baseline at 8", rows)
	}
}

// TestOffice_CroppedPicture crops a raster picture through a vector
// picture that clips it, as the converters draw srcRect and fo:clip: it
// paints the kept part alone and stays a Figure with its Alt, the image
// hanging from it.
func TestOffice_CroppedPicture(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 4, 2))
	for x := range 4 {
		c := color.NRGBA{R: 255, A: 255}
		if x >= 2 {
			c = color.NRGBA{B: 255, A: 255}
		}
		img.Set(x, 0, c)
		img.Set(x, 1, c)
	}
	src := source(img)
	crop := &Picture{Width: px(2), Height: px(2), Vector: func(_ context.Context, r compose.Rect) []compose.Fragment {
		// The picture's right half fills r: the whole is twice as wide.
		whole := compose.Rect{X: r.X - r.Width, Y: r.Y, Width: 2 * r.Width, Height: r.Height}
		return []compose.Fragment{&compose.Group{Opacity: 1, Transform: compose.Identity, Clip: &compose.Path{Ops: rectOps(r), Fill: &compose.Fill{}}, Content: []compose.Fragment{
			&compose.Image{Rect: whole, Source: src},
		}}}
	}}
	n := &Node{Kind: Replaced, Style: sty(width(2), height(2)), Picture: crop, Role: "Figure", Alt: "a cropped picture"}
	res := run(t, doc(el(sty(margin(0, 0, 0, 2)), n)), page(6, 2))
	assertPixels(t, res, 1, `
		__BB__
		__BB__`)
	var fig *compose.StructElem
	var find func(*compose.StructElem)
	find = func(s *compose.StructElem) {
		if s == nil || fig != nil {
			return
		}
		if s.Role == "Figure" {
			fig = s
			return
		}
		for _, k := range s.Kids {
			find(k)
		}
	}
	find(res.Structure)
	if fig == nil || fig.Alt != "a cropped picture" {
		t.Fatalf("figure %+v, want one with its Alt", fig)
	}
	var parents []*compose.StructElem
	var walk func([]compose.Fragment)
	walk = func(fs []compose.Fragment) {
		for _, f := range fs {
			switch v := f.(type) {
			case *compose.Image:
				parents = append(parents, v.Parent)
			case *compose.Group:
				walk(v.Content)
			}
		}
	}
	walk(res.Pages[0].Content)
	if len(parents) != 1 || parents[0] != fig {
		t.Errorf("image parents %v, want the figure", parents)
	}
}
