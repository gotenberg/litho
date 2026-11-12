package slides

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
)

// TestHandoutAreas_Impress locks the handout layouts to the slide
// rectangles the pinned LibreOffice stores in the handout master: each
// case is a count of slides a page, the page, its margins (top, right,
// bottom, left) and the slide in centimetres, then the left edges and the
// top edges of the rectangles and their size.
//
// # Corpus evidence
//
//   - probe, odp: the 48 layouts the pinned LibreOffice stores once a
//     macro sets the handout master's Layout property, for the six counts
//     on eight pages, to 0.01 mm.
func TestHandoutAreas_Impress(t *testing.T) {
	cm := func(v float64) float64 { return v * 72 / 2.54 }
	cases := []struct {
		per     int
		page    [2]float64
		margins [4]float64
		slide   [2]float64
		xs, ys  []float64
		w, h    float64
	}{
		{1, [2]float64{21, 29.7}, [4]float64{0, 0, 0, 0}, [2]float64{28, 15.75}, []float64{1}, []float64{9.506}, 18.999, 10.686},
		{2, [2]float64{21, 29.7}, [4]float64{0, 0, 0, 0}, [2]float64{28, 15.75}, []float64{1}, []float64{3.074, 15.939}, 18.999, 10.686},
		{3, [2]float64{21, 29.7}, [4]float64{0, 0, 0, 0}, [2]float64{28, 15.75}, []float64{1}, []float64{3.742, 12.318, 20.894}, 8.999, 5.061},
		{4, [2]float64{21, 29.7}, [4]float64{0, 0, 0, 0}, [2]float64{28, 15.75}, []float64{1, 11}, []float64{5.886, 18.751}, 8.999, 5.061},
		{6, [2]float64{21, 29.7}, [4]float64{0, 0, 0, 0}, [2]float64{28, 15.75}, []float64{1, 11}, []float64{3.742, 12.318, 20.894}, 8.999, 5.061},
		{9, [2]float64{21, 29.7}, [4]float64{0, 0, 0, 0}, [2]float64{28, 15.75}, []float64{1, 7.666, 14.332}, []float64{4.679, 13.255, 21.831}, 5.665, 3.186},
		{1, [2]float64{29.7, 21}, [4]float64{0, 0, 0, 0}, [2]float64{28, 15.75}, []float64{1}, []float64{2.709}, 27.699, 15.58},
		{2, [2]float64{29.7, 21}, [4]float64{0, 0, 0, 0}, [2]float64{28, 15.75}, []float64{1, 15.35}, []float64{6.745}, 13.349, 7.508},
		{3, [2]float64{29.7, 21}, [4]float64{0, 0, 0, 0}, [2]float64{28, 15.75}, []float64{1, 10.566, 20.132}, []float64{3.616}, 8.565, 4.817},
		{4, [2]float64{29.7, 21}, [4]float64{0, 0, 0, 0}, [2]float64{28, 15.75}, []float64{1, 15.35}, []float64{2.27, 11.22}, 13.349, 7.508},
		{6, [2]float64{29.7, 21}, [4]float64{0, 0, 0, 0}, [2]float64{28, 15.75}, []float64{1, 10.566, 20.132}, []float64{3.616, 12.566}, 8.565, 4.817},
		{9, [2]float64{29.7, 21}, [4]float64{0, 0, 0, 0}, [2]float64{28, 15.75}, []float64{1, 10.566, 20.132}, []float64{2.124, 8.09, 14.056}, 8.565, 4.817},
		{1, [2]float64{21.59, 27.94}, [4]float64{0, 0, 0, 0}, [2]float64{28, 15.75}, []float64{1}, []float64{8.46}, 19.589, 11.018},
		{2, [2]float64{21.59, 27.94}, [4]float64{0, 0, 0, 0}, [2]float64{28, 15.75}, []float64{1}, []float64{2.424, 14.497}, 19.589, 11.018},
		{3, [2]float64{21.59, 27.94}, [4]float64{0, 0, 0, 0}, [2]float64{28, 15.75}, []float64{1}, []float64{3.307, 11.355, 19.403}, 9.294, 5.227},
		{4, [2]float64{21.59, 27.94}, [4]float64{0, 0, 0, 0}, [2]float64{28, 15.75}, []float64{1, 11.295}, []float64{5.319, 17.392}, 9.294, 5.227},
		{6, [2]float64{21.59, 27.94}, [4]float64{0, 0, 0, 0}, [2]float64{28, 15.75}, []float64{1, 11.295}, []float64{3.307, 11.355, 19.403}, 9.294, 5.227},
		{9, [2]float64{21.59, 27.94}, [4]float64{0, 0, 0, 0}, [2]float64{28, 15.75}, []float64{1, 7.863, 14.726}, []float64{4.272, 12.32, 20.368}, 5.862, 3.296},
		{1, [2]float64{21, 29.7}, [4]float64{0, 0, 0, 0}, [2]float64{25.4, 19.05}, []float64{1}, []float64{7.725}, 18.999, 14.249},
		{2, [2]float64{21, 29.7}, [4]float64{0, 0, 0, 0}, [2]float64{25.4, 19.05}, []float64{2.59}, []float64{2.485, 15.35}, 15.819, 11.864},
		{3, [2]float64{21, 29.7}, [4]float64{0, 0, 0, 0}, [2]float64{25.4, 19.05}, []float64{1}, []float64{2.898, 11.474, 20.05}, 8.999, 6.749},
		{4, [2]float64{21, 29.7}, [4]float64{0, 0, 0, 0}, [2]float64{25.4, 19.05}, []float64{1, 11}, []float64{5.042, 17.907}, 8.999, 6.749},
		{6, [2]float64{21, 29.7}, [4]float64{0, 0, 0, 0}, [2]float64{25.4, 19.05}, []float64{1, 11}, []float64{2.898, 11.474, 20.05}, 8.999, 6.749},
		{9, [2]float64{21, 29.7}, [4]float64{0, 0, 0, 0}, [2]float64{25.4, 19.05}, []float64{1, 7.666, 14.332}, []float64{4.148, 12.724, 21.3}, 5.665, 4.248},
		{1, [2]float64{29.7, 21}, [4]float64{0, 0, 0, 0}, [2]float64{25.4, 19.05}, []float64{3.583}, []float64{2.05}, 22.532, 16.899},
		{2, [2]float64{29.7, 21}, [4]float64{0, 0, 0, 0}, [2]float64{25.4, 19.05}, []float64{1, 15.35}, []float64{5.494}, 13.349, 10.011},
		{3, [2]float64{29.7, 21}, [4]float64{0, 0, 0, 0}, [2]float64{25.4, 19.05}, []float64{1, 10.566, 20.132}, []float64{2.813}, 8.565, 6.423},
		{4, [2]float64{29.7, 21}, [4]float64{0, 0, 0, 0}, [2]float64{25.4, 19.05}, []float64{2.375, 16.725}, []float64{2.05, 11}, 10.599, 7.949},
		{6, [2]float64{29.7, 21}, [4]float64{0, 0, 0, 0}, [2]float64{25.4, 19.05}, []float64{1, 10.566, 20.132}, []float64{2.813, 11.763}, 8.565, 6.423},
		{9, [2]float64{29.7, 21}, [4]float64{0, 0, 0, 0}, [2]float64{25.4, 19.05}, []float64{1.972, 11.538, 21.104}, []float64{2.05, 8.016, 13.982}, 6.62, 4.965},
		{1, [2]float64{21, 29.7}, [4]float64{0, 0, 0, 0}, [2]float64{19.05, 25.4}, []float64{1.226}, []float64{2.485}, 18.546, 24.729},
		{2, [2]float64{21, 29.7}, [4]float64{0, 0, 0, 0}, [2]float64{19.05, 25.4}, []float64{6.051}, []float64{2.485, 15.35}, 8.897, 11.864},
		{3, [2]float64{21, 29.7}, [4]float64{0, 0, 0, 0}, [2]float64{19.05, 25.4}, []float64{2.659}, []float64{2.485, 11.061, 19.637}, 5.681, 7.575},
		{4, [2]float64{21, 29.7}, [4]float64{0, 0, 0, 0}, [2]float64{19.05, 25.4}, []float64{1.051, 11.051}, []float64{2.485, 15.35}, 8.897, 11.864},
		{6, [2]float64{21, 29.7}, [4]float64{0, 0, 0, 0}, [2]float64{19.05, 25.4}, []float64{2.659, 12.659}, []float64{2.485, 11.061, 19.637}, 5.681, 7.575},
		{9, [2]float64{21, 29.7}, [4]float64{0, 0, 0, 0}, [2]float64{19.05, 25.4}, []float64{1, 7.666, 14.332}, []float64{2.496, 11.072, 19.648}, 5.665, 7.553},
		{1, [2]float64{21, 29.7}, [4]float64{0.5, 0.7, 0.9, 1.5}, [2]float64{28, 15.75}, []float64{2.5}, []float64{9.925}, 16.799, 9.449},
		{2, [2]float64{21, 29.7}, [4]float64{0.5, 0.7, 0.9, 1.5}, [2]float64{28, 15.75}, []float64{2.5}, []float64{3.792, 16.057}, 16.799, 9.449},
		{3, [2]float64{21, 29.7}, [4]float64{0.5, 0.7, 0.9, 1.5}, [2]float64{28, 15.75}, []float64{2.5}, []float64{4.251, 12.427, 20.603}, 7.899, 4.442},
		{4, [2]float64{21, 29.7}, [4]float64{0.5, 0.7, 0.9, 1.5}, [2]float64{28, 15.75}, []float64{2.5, 11.4}, []float64{6.296, 18.561}, 7.899, 4.442},
		{6, [2]float64{21, 29.7}, [4]float64{0.5, 0.7, 0.9, 1.5}, [2]float64{28, 15.75}, []float64{2.5, 11.4}, []float64{4.251, 12.427, 20.603}, 7.899, 4.442},
		{9, [2]float64{21, 29.7}, [4]float64{0.5, 0.7, 0.9, 1.5}, [2]float64{28, 15.75}, []float64{2.5, 8.433, 14.366}, []float64{5.086, 13.262, 21.438}, 4.932, 2.773},
		{1, [2]float64{29.7, 21}, [4]float64{2, 1.1, 0.3, 0.4}, [2]float64{28, 15.75}, []float64{1.4}, []float64{3.981}, 26.199, 14.736},
		{2, [2]float64{29.7, 21}, [4]float64{2, 1.1, 0.3, 0.4}, [2]float64{28, 15.75}, []float64{1.4, 15}, []float64{7.806}, 12.599, 7.086},
		{3, [2]float64{29.7, 21}, [4]float64{2, 1.1, 0.3, 0.4}, [2]float64{28, 15.75}, []float64{1.4, 10.466, 19.532}, []float64{5.121}, 8.065, 4.536},
		{4, [2]float64{29.7, 21}, [4]float64{2, 1.1, 0.3, 0.4}, [2]float64{28, 15.75}, []float64{1.549, 15.149}, []float64{3.93, 11.85}, 12.301, 6.919},
		{6, [2]float64{29.7, 21}, [4]float64{2, 1.1, 0.3, 0.4}, [2]float64{28, 15.75}, []float64{1.4, 10.466, 19.532}, []float64{5.121, 13.041}, 8.065, 4.536},
		{9, [2]float64{29.7, 21}, [4]float64{2, 1.1, 0.3, 0.4}, [2]float64{28, 15.75}, []float64{1.629, 10.695, 19.761}, []float64{3.93, 9.21, 14.49}, 7.607, 4.279},
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("%vx%v/%v/%vx%v/%d", c.page[0], c.page[1], c.margins, c.slide[0], c.slide[1], c.per), func(t *testing.T) {
			m := [4]float64{css.Top: cm(c.margins[0]), css.Right: cm(c.margins[1]), css.Bottom: cm(c.margins[2]), css.Left: cm(c.margins[3])}
			got, ruled := handoutAreas(cm(c.page[0]), cm(c.page[1]), m, cm(c.slide[0]), cm(c.slide[1]), c.per)
			var want []compose.Rect
			for _, y := range c.ys {
				for _, x := range c.xs {
					want = append(want, compose.Rect{X: cm(x), Y: cm(y), Width: cm(c.w), Height: cm(c.h)})
				}
			}
			if len(got) != len(want) {
				t.Fatalf("%d areas, want %d", len(got), len(want))
			}
			// LibreOffice rounds every length down to 0.01 mm.
			const tol = 0.1
			for i := range want {
				g, w := got[i], want[i]
				if !near(g.X, w.X, tol) || !near(g.Y, w.Y, tol) || !near(g.Width, w.Width, tol) || !near(g.Height, w.Height, tol) {
					t.Errorf("area %d is %+v, want %+v", i, g, w)
				}
			}
			if (c.per == 3) != (len(ruled) == 3) {
				t.Errorf("%d ruled rectangles", len(ruled))
			}
		})
	}
}

// TestHandoutAreas_Grid checks every count on a portrait and a landscape
// page: one rectangle a slide, each of the slide's aspect ratio, within
// the page less its margins, none overlapping, in reading order row by
// row, three a page down the first column of a portrait page and along
// the first row of a landscape one, each with a ruled rectangle of its
// size to its right or below it, inside the page.
func TestHandoutAreas_Grid(t *testing.T) {
	margins := [4]float64{css.Top: 20, css.Right: 10, css.Bottom: 20, css.Left: 10}
	for _, page := range [][2]float64{{612, 792}, {792, 612}} {
		for _, per := range []int{1, 2, 3, 4, 6, 9} {
			t.Run(fmt.Sprintf("%vx%v/%d", page[0], page[1], per), func(t *testing.T) {
				areas, ruled := handoutAreas(page[0], page[1], margins, 720, 405, per)
				if len(areas) != per {
					t.Fatalf("%d areas, want %d", len(areas), per)
				}
				inside := func(a compose.Rect) bool {
					return a.X >= margins[css.Left] && a.Y >= margins[css.Top] && a.X+a.Width <= page[0]-margins[css.Right] && a.Y+a.Height <= page[1]-margins[css.Bottom]
				}
				for i, a := range areas {
					if !(a.Width > 0) || !near(a.Width/a.Height, 720.0/405, 1e-9) {
						t.Errorf("area %d is %+v", i, a)
					}
					if !inside(a) {
						t.Errorf("area %d %+v leaves the page less its margins", i, a)
					}
					if i == 0 {
						continue
					}
					p := areas[i-1]
					rowBelow := a.Y >= p.Y+p.Height
					sameRow := near(a.Y, p.Y, 1e-9) && a.X >= p.X+p.Width
					if !rowBelow && !sameRow {
						t.Errorf("area %d %+v does not follow %+v row by row", i, a, p)
					}
				}
				if per != 3 {
					if len(ruled) != 0 {
						t.Errorf("%d ruled rectangles", len(ruled))
					}
					return
				}
				if len(ruled) != 3 {
					t.Fatalf("%d ruled rectangles, want 3", len(ruled))
				}
				portrait := page[0] < page[1]
				for i, a := range areas {
					r := ruled[i]
					if portrait && !near(a.X, areas[0].X, 1e-9) || !portrait && !near(a.Y, areas[0].Y, 1e-9) {
						t.Errorf("three a page: %+v", areas)
					}
					beside := portrait && near(r.Y, a.Y, 1e-9) && r.X >= a.X+a.Width
					below := !portrait && near(r.X, a.X, 1e-9) && r.Y >= a.Y+a.Height
					if r.Width != a.Width || r.Height != a.Height || !inside(r) || !beside && !below {
						t.Errorf("ruled rectangle %d %+v of slide %+v", i, r, a)
					}
				}
			})
		}
	}
	if a, r := handoutAreas(10, 10, [4]float64{}, 720, 405, 9); len(a) != 9 || a[0].Width != 0 || len(r) != 0 {
		t.Errorf("a page too small: %+v", a)
	}
	if a, r := handoutAreas(10, 10, [4]float64{}, 720, 405, 3); len(a) != 3 || len(r) != 3 || r[0].Width != 0 {
		t.Errorf("a page too small, three a page: %+v, %+v", a, r)
	}
}

// TestNoteLines checks the lines ruled for notes: seven across the
// rectangle, a seventh of its height apart, the last along its bottom.
func TestNoteLines(t *testing.T) {
	ops := noteLines(compose.Rect{X: 10, Y: 20, Width: 100, Height: 70})
	if len(ops) != 14 {
		t.Fatalf("%d path steps, want 14", len(ops))
	}
	for i := range 7 {
		from, to := ops[2*i], ops[2*i+1]
		y := 30 + 10*float64(i)
		if from.Kind != compose.MoveTo || to.Kind != compose.LineTo || from.P[0] != (compose.Point{X: 10, Y: y}) || to.P[0] != (compose.Point{X: 110, Y: y}) {
			t.Errorf("line %d runs from %+v to %+v, want y %v", i, from.P[0], to.P[0], y)
		}
	}
}

// ruledPaths returns the black stroked paths of fourteen steps among the
// fragments frags and the groups they hold: the lines ruled for notes.
func ruledPaths(frags []compose.Fragment) []*compose.Path {
	var out []*compose.Path
	for _, f := range frags {
		switch v := f.(type) {
		case *compose.Path:
			if v.Stroke != nil && v.Fill == nil && len(v.Ops) == 2*handoutNoteLines && v.Stroke.Color == (compose.Color{A: 1}) {
				out = append(out, v)
			}
		case *compose.Group:
			out = append(out, ruledPaths(v.Content)...)
		}
	}
	return out
}

// TestConvert_HandoutsRuled checks that a three-a-page handout rules
// lines beside each slide a page holds and no other, and that no other
// count rules any.
func TestConvert_HandoutsRuled(t *testing.T) {
	deck := handoutDeck()
	deck.Slides = deck.Slides[:5]
	out := convert(t, deck, office.Options{Handouts: 3})
	if len(out.Pages) != 2 {
		t.Fatalf("%d pages, want 2", len(out.Pages))
	}
	for i, want := range []int{3, 1} {
		if got := len(ruledPaths(out.Pages[i].Content)); got != want {
			t.Errorf("page %d rules %d rectangles, want %d", i+1, got, want)
		}
	}
	for _, per := range []int{1, 2, 4, 6, 9} {
		for _, pg := range convert(t, handoutDeck(), office.Options{Handouts: per}).Pages {
			if n := len(ruledPaths(pg.Content)); n != 0 {
				t.Errorf("%d a page rules %d rectangles", per, n)
			}
		}
	}
}

// handoutDeck returns a deck of seven 16:9 slides, the fourth hidden, each
// titled "S<n>", the first jumping to the sixth, with a handout master
// whose footer shows the handout page number.
func handoutDeck() *Document {
	var list []*Slide
	for i := 1; i <= 7; i++ {
		s := &Slide{Name: fmt.Sprintf("S%d", i), Hidden: i == 4, Shapes: []*Shape{box(40, 40, 400, 80, para(fmt.Sprintf("S%d", i), arial(40)))}}
		list = append(list, s)
	}
	list[0].Shapes[0].Jump = 6
	number := box(300, 700, 200, 40, Paragraph{Runs: []Run{{Field: FieldSlideNumber, Text: "‹#›", Style: arial(12)}}, Style: arial(12)})
	return &Document{Width: 960, Height: 540, NotesWidth: 540, NotesHeight: 720, Slides: list, HandoutShapes: []*Shape{number}}
}

// TestConvert_Handouts checks the handout pages of every count, portrait
// and turned to landscape: a page for each count of shown slides, the last
// holding the rest, on the notes size; the slides in show order on them;
// the outline entry and the jump of each slide leading to the page that
// holds it and naming its Sect; a Sect per slide in order; the handout
// master's page number counting the handout pages.
func TestConvert_Handouts(t *testing.T) {
	for _, per := range []int{1, 2, 3, 4, 6, 9} {
		for _, landscape := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/landscape=%t", per, landscape), func(t *testing.T) {
				out := convert(t, handoutDeck(), office.Options{Handouts: per, Landscape: landscape})
				shown := []string{"S1", "S2", "S3", "S5", "S6", "S7"}
				if want := (len(shown) + per - 1) / per; len(out.Pages) != want {
					t.Fatalf("%d pages, want %d", len(out.Pages), want)
				}
				w, h := 540.0, 720.0
				if landscape {
					w, h = h, w
				}
				for i, pg := range out.Pages {
					if !near(pg.Width, w, 0.01) || !near(pg.Height, h, 0.01) {
						t.Errorf("page %d is %v by %v, want %v by %v", i+1, pg.Width, pg.Height, w, h)
					}
					if n := find(t, pg, fmt.Sprint(i+1)); n.size == 0 {
						t.Errorf("page %d has no number", i+1)
					}
				}
				var prev run
				for i, name := range shown {
					pg := out.Pages[i/per]
					r := find(t, pg, name)
					if i%per > 0 && !(r.y > prev.y+0.5 || near(r.y, prev.y, 0.5) && r.x > prev.x) {
						t.Errorf("%s at %.1f, %.1f does not follow %.1f, %.1f", name, r.x, r.y, prev.x, prev.y)
					}
					prev = r
				}
				if len(out.Structure.Kids) != len(shown) {
					t.Fatalf("%d Sects, want %d", len(out.Structure.Kids), len(shown))
				}
				if len(out.Outline) != len(shown) {
					t.Fatalf("%d outline entries, want %d", len(out.Outline), len(shown))
				}
				for i, o := range out.Outline {
					if o.Title != shown[i] || o.Dest.Page != i/per+1 || o.Dest.Elem != out.Structure.Kids[i] {
						t.Errorf("outline %d: %q to page %d, %p; want %q to %d, %p", i, o.Title, o.Dest.Page, o.Dest.Elem, shown[i], i/per+1, out.Structure.Kids[i])
					}
				}
				links := out.Pages[0].Links
				if len(links) != 1 || links[0].Dest == nil {
					t.Fatalf("links of page 1: %+v", links)
				}
				if d := links[0].Dest; d.Page != 4/per+1 || d.Elem != out.Structure.Kids[4] {
					t.Errorf("jump to S6 leads to page %d, %p; want %d, %p", d.Page, d.Elem, 4/per+1, out.Structure.Kids[4])
				}
			})
		}
	}
}

// TestTurned checks which handout pages turn: one upright for a page on
// its side or the reverse, never a square one.
func TestTurned(t *testing.T) {
	cases := []struct {
		w, h, pw, ph float64
		want         bool
	}{
		{540, 720, 720, 540, true},
		{720, 540, 595, 842, true},
		{540, 720, 595, 842, false},
		{720, 540, 842, 595, false},
		{540, 720, 600, 600, false},
		{600, 600, 842, 595, false},
	}
	for _, c := range cases {
		if got := turned(c.w, c.h, c.pw, c.ph); got != c.want {
			t.Errorf("turned(%v, %v, %v, %v) = %t", c.w, c.h, c.pw, c.ph, got)
		}
	}
}

// TestTurnBox checks where a handout master's shapes go on a turned page.
// The four boxes Impress gives a master, each a share of the page less
// its margins in a corner of that area, land where the same shares put
// them on the turned page; a shape that is no rectangle keeps its size,
// its centre keeping its place in proportion.
//
// # Corpus evidence
//
//   - probe, odp: the pinned LibreOffice's own handout master on an A4
//     portrait page without margins: boxes 9.113 by 1.484 cm at x 0 and
//     11.886 cm, y 0 and 28.215 cm. The boxes on the turned page, and
//     those of a page with margins, follow the shares its source states
//     (SdPage::CreateDefaultPresObj) and are not measured.
func TestTurnBox(t *testing.T) {
	cm := func(v float64) float64 { return v * 72 / 2.54 }
	// impress returns the four boxes of Impress's rule on a page w by h cm
	// with the margins m: 43.4% of the width and 5% of the height of the
	// page less its margins, in the corners of that area.
	impress := func(w, h float64, m [4]float64) []compose.Rect {
		cw, ch := w-m[css.Left]-m[css.Right], h-m[css.Top]-m[css.Bottom]
		bw, bh := cw*0.434, ch*0.05
		var out []compose.Rect
		for _, y := range []float64{m[css.Top], m[css.Top] + ch - bh} {
			for _, x := range []float64{m[css.Left], m[css.Left] + cw - bw} {
				out = append(out, compose.Rect{X: cm(x), Y: cm(y), Width: cm(bw), Height: cm(bh)})
			}
		}
		return out
	}
	measured := []compose.Rect{
		{X: 0, Y: 0, Width: cm(9.113), Height: cm(1.484)},
		{X: cm(11.886), Y: 0, Width: cm(9.113), Height: cm(1.484)},
		{X: 0, Y: cm(28.215), Width: cm(9.113), Height: cm(1.484)},
		{X: cm(11.886), Y: cm(28.215), Width: cm(9.113), Height: cm(1.484)},
	}
	same := func(a, b compose.Rect, tol float64) bool {
		return near(a.X, b.X, tol) && near(a.Y, b.Y, tol) && near(a.Width, b.Width, tol) && near(a.Height, b.Height, tol)
	}
	for i, want := range impress(29.7, 21, [4]float64{}) {
		if got := turnBox(measured[i], true, cm(21), cm(29.7), [4]float64{}); !same(got, want, 0.1) {
			t.Errorf("measured box %d turns to %+v, want %+v", i, got, want)
		}
	}
	margins := [4]float64{css.Top: 0.5, css.Right: 0.7, css.Bottom: 0.9, css.Left: 1.5}
	pt := [4]float64{css.Top: cm(0.5), css.Right: cm(0.7), css.Bottom: cm(0.9), css.Left: cm(1.5)}
	before, after := impress(21, 29.7, margins), impress(29.7, 21, margins)
	for i := range before {
		if got := turnBox(before[i], true, cm(21), cm(29.7), pt); !same(got, after[i], 1e-6) {
			t.Errorf("box %d with margins turns to %+v, want %+v", i, got, after[i])
		}
		// Turned back, a box is where it was.
		if got := turnBox(after[i], true, cm(29.7), cm(21), pt); !same(got, before[i], 1e-6) {
			t.Errorf("box %d turns back to %+v, want %+v", i, got, before[i])
		}
	}
	// A picture 60 by 30 in the top right corner of a 540 by 720 page keeps
	// its size; its centre, at 510 of 540 and 15 of 720, goes to 680 of 720
	// and 11.25 of 540.
	if got := turnBox(compose.Rect{X: 480, Y: 0, Width: 60, Height: 30}, false, 540, 720, [4]float64{}); !same(got, compose.Rect{X: 650, Y: -3.75, Width: 60, Height: 30}, 1e-9) {
		t.Errorf("a picture turns to %+v", got)
	}
	// Margins that leave no area keep the box.
	r := compose.Rect{X: 10, Y: 20, Width: 30, Height: 40}
	if got := turnBox(r, true, 540, 720, [4]float64{css.Left: 300, css.Right: 300}); got != r {
		t.Errorf("a page without area moves the box to %+v", got)
	}
}

// TestConvert_HandoutsTurned checks that a handout page turned by the
// options lays its slides out again: six a page in two rows of three on a
// portrait page turned to landscape, and in three rows of two on a
// landscape page turned to portrait, the master's page number inside the
// turned page.
func TestConvert_HandoutsTurned(t *testing.T) {
	shown := []string{"S1", "S2", "S3", "S5", "S6", "S7"}
	check := func(t *testing.T, out *compose.Document, w, h float64, cols int) {
		t.Helper()
		if len(out.Pages) != 1 {
			t.Fatalf("%d pages, want 1", len(out.Pages))
		}
		pg := out.Pages[0]
		if !near(pg.Width, w, 0.01) || !near(pg.Height, h, 0.01) {
			t.Fatalf("page %v by %v, want %v by %v", pg.Width, pg.Height, w, h)
		}
		var at []run
		for _, name := range shown {
			at = append(at, find(t, pg, name))
		}
		for i := range at {
			col, row := i%cols, i/cols
			first, above := at[row*cols], at[col]
			if !near(at[i].y, first.y, 0.5) || !near(at[i].x, above.x, 0.5) {
				t.Errorf("%s at %.1f, %.1f is not in column %d of row %d", shown[i], at[i].x, at[i].y, col, row)
			}
			if col > 0 && !(at[i].x > at[i-1].x) || row > 0 && !(at[i].y > at[i-cols].y) {
				t.Errorf("%s at %.1f, %.1f does not follow in its row and column", shown[i], at[i].x, at[i].y)
			}
		}
		if n := find(t, pg, "1"); !(n.x > w/2) || !(n.y > h/2) || n.y > h {
			t.Errorf("the page number is at %.1f, %.1f on a page %v by %v", n.x, n.y, w, h)
		}
	}
	t.Run("to landscape", func(t *testing.T) {
		check(t, convert(t, handoutDeck(), office.Options{Handouts: 6, Landscape: true}), 720, 540, 3)
	})
	t.Run("to portrait", func(t *testing.T) {
		deck := handoutDeck()
		deck.HandoutWidth, deck.HandoutHeight = 720, 540
		deck.HandoutShapes[0].Box = compose.Rect{X: 480, Y: 500, Width: 200, Height: 30}
		check(t, convert(t, deck, office.Options{Handouts: 6, Portrait: true}), 540, 720, 2)
	})
	t.Run("as it is", func(t *testing.T) {
		check(t, convert(t, handoutDeck(), office.Options{Handouts: 6}), 540, 720, 2)
	})
}

// TestConvert_HandoutsTurnedPaper checks that a paper on its side turns an
// upright handout page before it is scaled to fit: the slides in two rows
// of three, the turned page centred on the paper.
func TestConvert_HandoutsTurnedPaper(t *testing.T) {
	out := convert(t, handoutDeck(), office.Options{Handouts: 6, PageWidth: 842, PageHeight: 595})
	pg := out.Pages[0]
	if pg.Width != 842 || pg.Height != 595 || len(pg.Content) != 1 {
		t.Fatalf("page %v by %v, %d fragments", pg.Width, pg.Height, len(pg.Content))
	}
	g, ok := pg.Content[0].(*compose.Group)
	if !ok || g.Transform != (compose.Matrix{A: 1, D: 1, E: 61, F: 27.5}) {
		t.Errorf("handout page placed by %+v", pg.Content[0])
	}
	if a, b := find(t, pg, "S1"), find(t, pg, "S3"); !near(a.y, b.y, 0.5) || !(b.x > a.x) {
		t.Errorf("S1 at %.1f, %.1f and S3 at %.1f, %.1f are not in one row", a.x, a.y, b.x, b.y)
	}
}

// TestConvert_HandoutsPaper checks that a paper scales the handout page
// down to fit and centres it, as it does a slide.
func TestConvert_HandoutsPaper(t *testing.T) {
	out := convert(t, handoutDeck(), office.Options{Handouts: 6, PageWidth: 1080, PageHeight: 1080})
	pg := out.Pages[0]
	if pg.Width != 1080 || pg.Height != 1080 || len(pg.Content) != 1 {
		t.Fatalf("page %v by %v, %d fragments", pg.Width, pg.Height, len(pg.Content))
	}
	g, ok := pg.Content[0].(*compose.Group)
	if !ok || g.Transform != (compose.Matrix{A: 1, D: 1, E: 270, F: 180}) {
		t.Errorf("handout page placed by %+v", pg.Content[0])
	}
}

// TestConvert_HandoutsHiddenSlides checks that hidden slides print on the
// handout pages when the options ask for them.
func TestConvert_HandoutsHiddenSlides(t *testing.T) {
	out := convert(t, handoutDeck(), office.Options{Handouts: 2, HiddenSlides: true})
	if len(out.Pages) != 4 {
		t.Fatalf("%d pages, want 4", len(out.Pages))
	}
	find(t, out.Pages[1], "S4")
}

// TestConvert_HandoutsInvalid checks that a count no handout layout has,
// or handouts beside notes pages, fail with office.ErrInvalidOptions.
func TestConvert_HandoutsInvalid(t *testing.T) {
	for _, opts := range []office.Options{{Handouts: 5}, {Handouts: -1}, {Handouts: 12}, {Handouts: 2, NotesPages: true}} {
		if _, err := Convert(context.Background(), handoutDeck(), pinnedFonts(t), opts); !errors.Is(err, office.ErrInvalidOptions) {
			t.Errorf("%+v: %v, want office.ErrInvalidOptions", opts, err)
		}
	}
}

// TestConvert_HandoutsStream checks that handout pages go to the sink as
// they are laid out, the document holding none.
func TestConvert_HandoutsStream(t *testing.T) {
	sink := &countSink{}
	out := convert(t, handoutDeck(), office.Options{Handouts: 4, Sink: sink})
	if sink.expect != 2 || sink.pages != 2 || len(out.Pages) != 0 {
		t.Errorf("sink expected %d, took %d, document holds %d", sink.expect, sink.pages, len(out.Pages))
	}
}

// countSink counts the pages handed to it.
type countSink struct{ expect, pages int }

func (s *countSink) Expect(n int) { s.expect = n }

func (s *countSink) Page(context.Context, *compose.Page) error {
	s.pages++
	return nil
}
