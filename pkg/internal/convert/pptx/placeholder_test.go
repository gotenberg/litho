package pptx

import (
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
)

func TestPlaceholders_Inheritance(t *testing.T) {
	t.Parallel()
	masterBox := compose.Rect{X: 36, Y: 21.6249, Width: 648, Height: 90}
	for _, tc := range []struct {
		name   string
		deck   testDeck
		shape  string
		want   compose.Rect
		absent bool
	}{
		{
			name:  "title from the master through the layout",
			deck:  testDeck{slides: []testSlide{{tree: title("T")}}},
			shape: "Title 1", want: masterBox,
		},
		{
			name: "layout's own position wins over the master's",
			deck: testDeck{
				layoutTree: phShape("Title 1", "title", -1, 914400, 914400, 1828800, 457200, para("x")),
				slides:     []testSlide{{tree: title("T")}},
			},
			shape: "Title 1", want: compose.Rect{X: 72, Y: 72, Width: 144, Height: 36},
		},
		{
			name: "slide's own position wins",
			deck: testDeck{slides: []testSlide{{tree: phShape("Own", "title", -1, 0, 0, 12700, 25400, para("T"))}}},
			shape: "Own", want: compose.Rect{Width: 1, Height: 2},
		},
		{
			name: "matched by idx before type",
			deck: testDeck{
				layoutTree: phShape("A", "body", 7, 127000, 0, 12700, 12700, para("x")) + phShape("B", "body", 8, 254000, 0, 12700, 12700, para("x")),
				slides:     []testSlide{{tree: phShape("S", "body", 8, 0, 0, 0, 0, para("text"))}},
			},
			shape: "S", want: compose.Rect{X: 20, Width: 1, Height: 1},
		},
		{
			name: "an obj placeholder takes the master's body",
			deck: testDeck{
				layoutTree: phShape("L", "obj", 5, 0, 0, 0, 0, para("x")),
				slides:     []testSlide{{tree: phShape("S", "", 5, 0, 0, 0, 0, para("text"))}},
			},
			shape: "S", want: compose.Rect{X: 36, Y: 126, Width: 648, Height: 356.3750},
		},
		{
			name:  "an empty placeholder prints nothing",
			deck:  testDeck{slides: []testSlide{{tree: phShape("Empty", "title", -1, 0, 0, 0, 0, `<a:bodyPr/><a:p><a:endParaRPr/></a:p>`)}}},
			shape: "Empty", absent: true,
		},
		{
			name: "a placeholder no layer places prints nothing",
			deck: testDeck{
				masterTree: phShape("Title Placeholder 1", "title", -1, 0, 0, 0, 0, para("x")),
				slides:     []testSlide{{tree: title("T")}},
			},
			shape: "Title 1", absent: true,
		},
		{
			name:  "a footer prints when the layout has one",
			deck:  testDeck{slides: []testSlide{{tree: phShape("F", "ftr", 11, 0, 0, 0, 0, para("FOOT"))}}},
			shape: "F", want: compose.Rect{X: 246, Y: 500.5, Width: 228, Height: 28.75},
		},
		{
			name:  "a slide number prints only when the layout has one",
			deck:  testDeck{slides: []testSlide{{tree: phShape("N", "sldNum", 12, 0, 0, 0, 0, para("3"))}}},
			shape: "N", absent: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := readDeck(t, tc.deck)
			sh := shapeNamed(doc.Slides[0].Shapes, tc.shape)
			if tc.absent {
				if sh != nil {
					t.Fatalf("shape %q printed: %+v", tc.shape, sh)
				}
				return
			}
			if sh == nil {
				t.Fatalf("no shape %q among %q", tc.shape, shapeNames(doc.Slides[0].Shapes))
			}
			if !near(sh.Box, tc.want) {
				t.Errorf("box %+v, want %+v", sh.Box, tc.want)
			}
		})
	}
}

// near reports whether two boxes agree to a thousandth of a point.
func near(a, b compose.Rect) bool {
	d := func(x, y float64) bool { return x-y < 1e-3 && y-x < 1e-3 }
	return d(a.X, b.X) && d(a.Y, b.Y) && d(a.Width, b.Width) && d(a.Height, b.Height)
}

func TestPlaceholders_MasterShapesShowThrough(t *testing.T) {
	t.Parallel()
	logo := textBox("Logo", 0, 0, 12700, 12700, para("M"))
	band := textBox("Band", 0, 0, 12700, 12700, para("L"))
	for _, tc := range []struct {
		name                string
		slideAttrs, layAttr string
		want                []string
	}{
		{"master, layout, slide", "", "", []string{"Logo", "Band", "Title 1"}},
		{"slide hides both", `showMasterSp="0"`, "", []string{"Title 1"}},
		{"layout hides the master's", "", `showMasterSp="0"`, []string{"Band", "Title 1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := readDeck(t, testDeck{
				masterTree:  defaultMasterTree + logo,
				layoutTree:  defaultLayoutTree + band,
				layoutAttrs: tc.layAttr,
				slides:      []testSlide{{tree: title("T"), attrs: tc.slideAttrs}},
			})
			if got := shapeNames(doc.Slides[0].Shapes); !slices.Equal(got, tc.want) {
				t.Errorf("shapes %q, want %q", got, tc.want)
			}
		})
	}
}
