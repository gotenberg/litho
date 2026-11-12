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

// TestHandoutAreas_Impress locks the six-a-page handout of an A4 portrait
// page without margins for a 28 by 15.75 cm slide to the thumbnails the
// pinned LibreOffice writes in the handout master it creates.
//
// # Corpus evidence
//
//   - probe, odp: thumbnails 8.999 by 5.061 cm at x 1 and 11 cm, y 3.742,
//     12.318 and 20.894 cm.
func TestHandoutAreas_Impress(t *testing.T) {
	cm := func(v float64) float64 { return v * 72 / 2.54 }
	got := handoutAreas(cm(21), cm(29.7), [4]float64{}, cm(28), cm(15.75), 6)
	var want []compose.Rect
	for _, y := range []float64{3.742, 12.318, 20.894} {
		for _, x := range []float64{1, 11} {
			want = append(want, compose.Rect{X: cm(x), Y: cm(y), Width: cm(8.999), Height: cm(5.061)})
		}
	}
	if len(got) != len(want) {
		t.Fatalf("%d areas, want %d", len(got), len(want))
	}
	for i := range want {
		g, w := got[i], want[i]
		if !near(g.X, w.X, 0.05) || !near(g.Y, w.Y, 0.05) || !near(g.Width, w.Width, 0.05) || !near(g.Height, w.Height, 0.05) {
			t.Errorf("area %d is %+v, want %+v", i, g, w)
		}
	}
}

// TestHandoutAreas_Grid checks every count on a portrait and a landscape
// page: one rectangle a slide, each of the slide's aspect ratio, within
// the page less its margins, none overlapping, in reading order row by
// row, three a page down the first column of a portrait page and along
// the first row of a landscape one.
func TestHandoutAreas_Grid(t *testing.T) {
	margins := [4]float64{css.Top: 20, css.Right: 10, css.Bottom: 20, css.Left: 10}
	for _, page := range [][2]float64{{612, 792}, {792, 612}} {
		for _, per := range []int{1, 2, 3, 4, 6, 9} {
			t.Run(fmt.Sprintf("%vx%v/%d", page[0], page[1], per), func(t *testing.T) {
				areas := handoutAreas(page[0], page[1], margins, 720, 405, per)
				if len(areas) != per {
					t.Fatalf("%d areas, want %d", len(areas), per)
				}
				for i, a := range areas {
					if !(a.Width > 0) || !near(a.Width/a.Height, 720.0/405, 1e-9) {
						t.Errorf("area %d is %+v", i, a)
					}
					if a.X < margins[css.Left] || a.Y < margins[css.Top] || a.X+a.Width > page[0]-margins[css.Right] || a.Y+a.Height > page[1]-margins[css.Bottom] {
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
				if per == 3 {
					for _, a := range areas[1:] {
						if page[0] < page[1] && !near(a.X, areas[0].X, 1e-9) || page[0] > page[1] && !near(a.Y, areas[0].Y, 1e-9) {
							t.Errorf("three a page: %+v", areas)
						}
					}
				}
			})
		}
	}
	if a := handoutAreas(10, 10, [4]float64{}, 720, 405, 9); len(a) != 9 || a[0].Width != 0 {
		t.Errorf("a page too small: %+v", a)
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
