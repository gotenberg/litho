package layout

import (
	"context"
	"math"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// named sets the page property.
func named(name string) func(*css.Style) { return func(s *css.Style) { s.Page = name } }

// brk sets break-before.
func brk(v css.Break) func(*css.Style) { return func(s *css.Style) { s.BreakBefore = v } }

// namedRun lays root out with PageFor sizing a page named "wide" 40 px
// wide and every other page 20 px, all 30 px tall, and returns the result
// and the pages PageFor and MarginContent saw.
func namedRun(t *testing.T, root *Node) (*Result, map[int]PageInfo) {
	t.Helper()
	seen := map[int]PageInfo{}
	opts := Options{
		Page: page(20, 30),
		PageFor: func(info PageInfo) PageBox {
			if info.Name == "wide" {
				return page(40, 30)
			}
			return page(20, 30)
		},
		MarginContent: func(info PageInfo, _ int) []typeset.Fragment {
			seen[info.Index] = info
			return nil
		},
	}
	res, err := Layout(context.Background(), root, ahem(t), opts)
	if err != nil {
		t.Fatalf("Layout: %v", err)
	}
	if err := document(res).Validate(t.Context()); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	return res, seen
}

func TestNamedPages_NameChangeForcesBreak(t *testing.T) {
	tests := []struct {
		name  string
		root  *Node
		names []string
		blank []bool
	}{
		{"same name, one page", doc(el(sty(height(5))), el(sty(height(5)))), []string{""}, nil},
		{"named sibling breaks", doc(el(sty(height(5))), el(sty(height(5), named("a")))), []string{"", "a"}, nil},
		{"auto resolves against the parent", doc(el(sty(named("a")), el(sty(height(5))), el(sty(height(5), named("a"))))), []string{"a"}, nil},
		{"first child propagates its start value", doc(el(sty(height(5), named("a"))), el(sty(), el(sty(height(5), named("b"))))), []string{"a", "b"}, nil},
		{"back to unnamed breaks again", doc(el(sty(height(5), named("a"))), el(sty(height(5)))), []string{"a", ""}, nil},
		{"root start value names the first page", doc(el(sty(height(5), named("wide")))), []string{"wide"}, nil},
		{"right break leaves a blank page", doc(el(sty(height(5))), el(sty(height(5), brk(css.RightPage)))), []string{"", "", ""}, []bool{false, true, false}},
		{"blank page takes the next name", doc(el(sty(height(5))), el(sty(height(5), named("a"), brk(css.RightPage)))), []string{"", "a", "a"}, []bool{false, true, false}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, seen := namedRun(t, tt.root)
			if len(res.Pages) != len(tt.names) {
				t.Fatalf("%d pages, want %d", len(res.Pages), len(tt.names))
			}
			for i, want := range tt.names {
				if seen[i].Name != want {
					t.Errorf("page %d named %q, want %q", i+1, seen[i].Name, want)
				}
				if tt.blank != nil && seen[i].Blank != tt.blank[i] {
					t.Errorf("page %d blank %v, want %v", i+1, seen[i].Blank, tt.blank[i])
				}
			}
		})
	}
}

func TestNamedPages_PageForSizesEachPage(t *testing.T) {
	root := doc(el(sty(height(5))), el(sty(height(5), named("wide"))), el(sty(height(5))))
	res, _ := namedRun(t, root)
	want := []float64{20, 40, 20}
	if len(res.Pages) != len(want) {
		t.Fatalf("%d pages, want %d", len(res.Pages), len(want))
	}
	for i, w := range want {
		if got := res.Pages[i].Width; !near(got, px(w)) {
			t.Errorf("page %d width %v, want %v", i+1, got, px(w))
		}
	}
}

func TestNamedPages_NotForFloatsOrInline(t *testing.T) {
	// A float and an inline block do not create class A break points: their
	// page value forces nothing.
	root := doc(el(sty(height(5))),
		el(sty(height(5), width(5), float(css.FloatLeft), named("a"))),
		el(sty(), el(sty(display(css.InlineBlock), height(5), width(5), named("b")))))
	res, _ := namedRun(t, root)
	if len(res.Pages) != 1 {
		t.Errorf("%d pages, want 1", len(res.Pages))
	}
}

func TestLayout_Extents(t *testing.T) {
	b := el(sty(width(10), height(6), func(s *css.Style) {
		s.Padding = [4]css.Dimension{css.Pts(px(1)), css.Pts(px(2)), css.Pts(px(3)), css.Pts(px(4))}
	}))
	b.ID = "b"
	in := txt(inlineSty(), "x")
	span := el(inlineSty(), in)
	span.ID = "span"
	res := run(t, doc(el(sty(height(7))), b, el(sty(), span)), page(40, 40))
	r, ok := res.Extents["b"]
	if !ok {
		t.Fatal("no extent for the block")
	}
	want := typeset.Rect{X: px(4), Y: px(8), Width: px(10), Height: px(6)}
	if !near(r.X, want.X) || !near(r.Y, want.Y) || !near(r.Width, want.Width) || !near(r.Height, want.Height) {
		t.Errorf("extent %+v, want %+v", r, want)
	}
	if _, ok := res.Extents["span"]; ok {
		t.Error("an inline box has an extent")
	}
}

func TestNamedPages_OverflowCountsAgainstEachPage(t *testing.T) {
	// A block as wide as the wide page it lands on overflows nothing: the
	// result's width is the first page's. One twice as wide as the wide
	// page overflows it twice over, on the first page's scale.
	w0, _ := areaOf(page(20, 30))
	for _, tt := range []struct {
		name string
		root *Node
		want float64
	}{
		{"fills its page", doc(el(sty(height(5))), el(sty(height(5), named("wide")))), w0},
		{"overflows its page", doc(el(sty(height(5))), el(sty(height(5), width(80), named("wide")))), 2 * w0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			res, _ := namedRun(t, tt.root)
			if len(res.Pages) != 2 {
				t.Fatalf("%d pages, want 2", len(res.Pages))
			}
			if d := res.ContentWidth - tt.want; d > 1e-6 || d < -1e-6 {
				t.Errorf("content width %v, want %v", res.ContentWidth, tt.want)
			}
		})
	}
}

func TestNamedPages_WidthFollowsTheNamedPage(t *testing.T) {
	// The box whose name starts the wide page is laid out at that page's
	// width, its child too, once: the first page's width stays the flow's
	// and nothing overflows it.
	w0, _ := areaOf(page(20, 30))
	w1, _ := areaOf(page(40, 30))
	kid := el(sty(height(5)))
	kid.ID = "kid"
	wide := el(sty(named("wide")), kid)
	wide.ID = "wide"
	res, _ := namedRun(t, doc(el(sty(height(5))), wide))
	if len(res.Pages) != 2 {
		t.Fatalf("%d pages, want 2", len(res.Pages))
	}
	for _, id := range []string{"wide", "kid"} {
		if r, ok := res.Extents[id]; !ok || math.Abs(r.Width-w1) > 1e-6 {
			t.Errorf("%s: extent %+v, want %v wide", id, r, w1)
		}
	}
	if math.Abs(res.ContentWidth-w0) > 1e-6 {
		t.Errorf("content width %v, want %v", res.ContentWidth, w0)
	}
}

// TestMarginLinks_JoinPagesAndRoot locks Options.MarginLinks: the links of
// each page join its links, and each Link element the root of the
// structure tree; and Options.Targets: an internal link resolves against
// another layout's anchors when this one has no such ID.
func TestMarginLinks_JoinPagesAndRoot(t *testing.T) {
	root := el(sty(), el(sty(), txt(sty(), "a")), el(sty(brk(css.Page)), txt(sty(), "b")))
	var made []*typeset.StructElem
	opts := Options{
		Page:          page(100, 100),
		MarginContent: func(PageInfo, int) []typeset.Fragment { return nil },
		MarginLinks: func(info PageInfo) []typeset.Link {
			e := &typeset.StructElem{Role: "Link"}
			made = append(made, e)
			return []typeset.Link{{Rect: typeset.Rect{X: 1, Y: 1, Width: 5, Height: 5}, URI: "https://example.com/", Parent: e, Contents: string(rune('A' + info.Index))}}
		},
	}
	res, err := Layout(context.Background(), root, ahem(t), opts)
	if err != nil {
		t.Fatalf("Layout: %v", err)
	}
	if err := document(res).Validate(t.Context()); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(res.Pages) != 2 || len(made) != 2 {
		t.Fatalf("%d pages, %d MarginLinks calls, want 2 and 2", len(res.Pages), len(made))
	}
	for i, pg := range res.Pages {
		if len(pg.Links) != 1 || pg.Links[0].Parent != made[i] || pg.Links[0].Contents != string(rune('A'+i)) {
			t.Errorf("page %d: links %+v", i+1, pg.Links)
		}
		if !slices.Contains(res.Structure.Kids, made[i]) {
			t.Errorf("page %d: the Link element is not a kid of the root", i+1)
		}
	}

	link := el(inlineSty(), txt(inlineSty(), "go"))
	link.Href = "#far"
	sub, err := Layout(context.Background(), el(sty(), el(sty(), link)), ahem(t), Options{
		Page:    page(100, 100),
		Targets: map[string]typeset.Dest{"far": {Page: 7, Y: 3}},
	})
	if err != nil {
		t.Fatalf("Layout: %v", err)
	}
	if ls := sub.Pages[0].Links; len(ls) != 1 || ls[0].Dest == nil || ls[0].Dest.Page != 7 {
		t.Errorf("links %+v, want one to page 7", ls)
	}
}

// TestNamedPages_WhiteSpaceBeforeFirstBlock locks the start page value
// past the white space a source leaves between its blocks: the anonymous
// block that holds it has no line box (CSS 2.1, 9.4.2), so the first page
// takes the name of the first block (CSS Page 3, 4.1).
func TestNamedPages_WhiteSpaceBeforeFirstBlock(t *testing.T) {
	root := el(sty(),
		txt(sty(), "\n  "),
		el(inlineSty()),
		el(sty(named("wide")), txt(sty(), "a")),
		txt(sty(), "\n"),
		el(sty(named("wide")), txt(sty(), "b")))
	res, seen := namedRun(t, root)
	if len(res.Pages) != 1 {
		t.Fatalf("%d pages, want 1: white space between two blocks of one name forces no break", len(res.Pages))
	}
	if seen[0].Name != "wide" || !near(res.Pages[0].Width, px(40)) {
		t.Errorf("first page %+v, %v wide, want the wide page", seen[0], res.Pages[0].Width)
	}
}

// TestNamedPages_NotOutOfFlexOrGrid locks the start page value of a flex
// or a grid container: its own, whatever the blocks inside its items
// name, since its items create no class A break points (CSS Page 3, 4.1):
// no break comes before the container, as in the web platform test
// css/css-page/page-name-flex-004-print.
func TestNamedPages_NotOutOfFlexOrGrid(t *testing.T) {
	column := func(s *css.Style) { s.FlexDirection = css.FlexColumn }
	for _, d := range []css.Display{css.Flex, css.Grid} {
		inner := el(sty(), txt(sty(), "\n"), el(sty(height(5), named("a"))))
		root := doc(el(sty(height(5))), el(sty(display(d), column), el(sty(), inner)), el(sty(height(5))))
		res, seen := namedRun(t, root)
		if len(res.Pages) != 1 || seen[0].Name != "" {
			t.Errorf("display %v: %d pages, the first %+v, want one unnamed page", d, len(res.Pages), seen[0])
		}
	}
}
