package layout

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
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
		MarginContent: func(info PageInfo, _ int) []compose.Fragment {
			seen[info.Index] = info
			return nil
		},
	}
	res, err := Layout(context.Background(), root, ahem(t), opts)
	if err != nil {
		t.Fatalf("Layout: %v", err)
	}
	if err := document(res).Validate(); err != nil {
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
	want := compose.Rect{X: px(4), Y: px(8), Width: px(10), Height: px(6)}
	if !near(r.X, want.X) || !near(r.Y, want.Y) || !near(r.Width, want.Width) || !near(r.Height, want.Height) {
		t.Errorf("extent %+v, want %+v", r, want)
	}
	if _, ok := res.Extents["span"]; ok {
		t.Error("an inline box has an extent")
	}
}
