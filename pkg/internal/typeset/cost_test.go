package typeset

import (
	"runtime"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/limits"
)

// liveHeap returns the bytes the live objects hold, after a collection.
func liveHeap() int64 {
	runtime.GC()
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return int64(m.HeapAlloc)
}

// rectPath returns the path of the rectangle r, filled with fill, built as
// the layout builds a background or a tile: five operators in a slice of
// their own.
func rectPath(r Rect, fill *Fill) *Path {
	return &Path{Ops: []PathOp{
		{Kind: MoveTo, P: [3]Point{{X: r.X, Y: r.Y}}},
		{Kind: LineTo, P: [3]Point{{X: r.X + r.Width, Y: r.Y}}},
		{Kind: LineTo, P: [3]Point{{X: r.X + r.Width, Y: r.Y + r.Height}}},
		{Kind: LineTo, P: [3]Point{{X: r.X, Y: r.Y + r.Height}}},
		{Kind: ClosePath},
	}, Fill: fill}
}

// TestContentCostCoversTheContent holds the costs [ContentCost] adds up to
// what pages of many small fragments of each kind hold on the heap, their
// glyphs charged at [limits.GlyphCost] aside: no page of these outgrows
// its charge.
func TestContentCostCoversTheContent(t *testing.T) {
	const n = 20_000
	face := &Face{Key: "f"}
	red := Color{R: 1, A: 1}
	shapes := map[string]func(i int) Fragment{
		"filled rectangles": func(i int) Fragment {
			return rectPath(Rect{X: float64(i), Width: 10, Height: 10}, &Fill{Color: red})
		},
		"border rings": func(i int) Fragment {
			p := rectPath(Rect{X: float64(i), Width: 10, Height: 10}, &Fill{Color: red, Rule: EvenOdd})
			inner := rectPath(Rect{X: float64(i) + 1, Y: 1, Width: 8, Height: 8}, nil)
			p.Ops = append(p.Ops, inner.Ops...)
			return p
		},
		"stroked dashed lines": func(i int) Fragment {
			return &Path{Ops: []PathOp{{Kind: MoveTo, P: [3]Point{{X: float64(i)}}}, {Kind: LineTo, P: [3]Point{{X: float64(i) + 10}}}},
				Stroke: &Stroke{Color: red, Width: 1, Dash: []float64{2, 2}}}
		},
		"gradient tiles": func(i int) Fragment {
			sh := &Shading{To: Point{X: 10}, Transform: Matrix{A: 1, D: 1, E: float64(i)}, Stops: []ColorStop{{Color: red}, {Offset: 1, Color: Color{B: 1, A: 1}}}}
			return rectPath(Rect{X: float64(i), Width: 10, Height: 10}, &Fill{Shading: sh})
		},
		"runs of a word": func(i int) Fragment {
			g := make([]Glyph, 5)
			for k := range g {
				g[k] = Glyph{GID: uint16(k), X: float64(k)}
			}
			g[0].Text = "alpha"
			return &Text{Face: face, Size: 10, Color: red, Origin: Point{X: float64(i)}, Glyphs: g}
		},
		"images": func(i int) Fragment {
			return &Image{Rect: Rect{X: float64(i), Width: 10, Height: 10}}
		},
		"clipped groups": func(i int) Fragment {
			return &Group{Opacity: 0.5, Clip: rectPath(Rect{X: float64(i), Width: 10, Height: 10}, &Fill{}), Content: []Fragment{rectPath(Rect{Width: 1, Height: 1}, &Fill{Color: red})},
				Transparency: &TransparencyGroup{Isolated: true}}
		},
	}
	for name, mk := range shapes {
		before := liveHeap()
		content := make([]Fragment, 0, n)
		for i := range n {
			content = append(content, mk(i))
		}
		held := liveHeap() - before
		pg := &Page{Content: content}
		cost := ContentCost(content) + CountGlyphs(pg)*limits.GlyphCost
		if held > cost {
			t.Errorf("%s: %d fragments hold %d bytes, %d a fragment, past the %d charged", name, n, held, held/n, cost)
		}
		runtime.KeepAlive(content)
	}
}

func TestContentCost_CountsNestedContentOnce(t *testing.T) {
	leaf := rectPath(Rect{Width: 1, Height: 1}, &Fill{Color: Color{A: 1}})
	one := ContentCost([]Fragment{leaf})
	if want := int64(fragmentSlotCost + pathCost + 5*pathOpCost); one != want {
		t.Errorf("ContentCost of a rectangle = %d, want %d", one, want)
	}
	mask := &SoftMask{Content: []Fragment{leaf}}
	g := &Group{Content: []Fragment{leaf, leaf}, Transparency: &TransparencyGroup{Mask: mask}}
	got := ContentCost([]Fragment{g})
	if want := fragmentSlotCost + groupCost + transparencyCost + 3*one; got != want {
		t.Errorf("ContentCost of a masked group of two = %d, want %d", got, want)
	}
	if ContentCost([]Fragment{(*Path)(nil), (*Text)(nil), (*Group)(nil), (*Image)(nil)}) != 4*fragmentSlotCost {
		t.Error("nil fragments cost more than their slots")
	}
}
