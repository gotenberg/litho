package layout

import (
	"context"
	"runtime"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/limits"
)

// heapNoise is what the live heap moves by between two readings with
// nothing allocated in between: a measurement compares above it.
const heapNoise = 4 << 10

// liveHeap returns the bytes the live objects hold, after a collection.
func liveHeap() int64 {
	runtime.GC()
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return int64(m.HeapAlloc)
}

// costShapes are documents of many small boxes of one kind each.
func costShapes() map[string]func() *Node {
	const n = 10_000
	red := css.Color{R: 1, A: 1}
	repeat := func(st *css.Style, kid func() *Node) *Node {
		root := el(sty())
		for range n {
			var kids []*Node
			if kid != nil {
				kids = append(kids, kid())
			}
			root.Children = append(root.Children, el(st, kids...))
		}
		return root
	}
	return map[string]func() *Node{
		"empty blocks": func() *Node { return repeat(sty(height(2)), nil) },
		"paragraphs of a word": func() *Node {
			st := sty()
			return repeat(st, func() *Node { return txt(st, "alpha") })
		},
		"bordered paragraphs on a background": func() *Node {
			st := sty(border(1, css.Solid, red), bg(css.Color{B: 1, A: 1}))
			return repeat(st, func() *Node { return txt(st, "alpha") })
		},
		"bordered table cells": func() *Node {
			var rows []*Node
			for range n / 4 {
				var cells []*Node
				for range 4 {
					cs := sty(display(css.TableCell), border(1, css.Solid, red))
					cells = append(cells, part(css.TableCell, []func(*css.Style){border(1, css.Solid, red)}, txt(cs, "r c")))
				}
				rows = append(rows, tr(nil, cells...))
			}
			return el(sty(), table(nil, rows...))
		},
	}
}

// TestBoxCostCoversABox holds [boxCost] to what a box holds once block
// flow has laid it out: what the engine holds then, its boxes, their line
// boxes and inline state, their shaped text and glyphs, stays within what
// it charged.
func TestBoxCostCoversABox(t *testing.T) {
	fonts := ahem(t)
	for name, mk := range costShapes() {
		root := mk()
		ShareStyles(context.Background(), root, nil)
		ctx, b, done := limits.WithBudget(context.Background(), limits.Limits{})
		pb := page(400, 600)
		before := liveHeap()
		e, rb, err := layoutFlow(ctx, root, fonts, Options{Page: pb}, pb, nil, nil, css.HorizontalTB)
		if err != nil {
			t.Fatal(err)
		}
		if held, charged := liveHeap()-before, b.Held(); held > charged+heapNoise {
			t.Errorf("%s: %d boxes hold %d bytes after block flow, %d a box, past the %d charged", name, e.boxes, held, held/int64(e.boxes), charged)
		}
		runtime.KeepAlive(rb)
		e.release()
		done()
	}
}

// TestDisplayCostCoversTheList holds [ditemCost] and the fragment costs
// to what painting adds: the display list of each document holds no more
// than the engine charged for it.
func TestDisplayCostCoversTheList(t *testing.T) {
	fonts := ahem(t)
	for name, mk := range costShapes() {
		root := mk()
		ShareStyles(context.Background(), root, nil)
		ctx, _, done := limits.WithBudget(context.Background(), limits.Limits{})
		pb := page(400, 600)
		e, rb, err := layoutFlow(ctx, root, fonts, Options{Page: pb}, pb, nil, nil, css.HorizontalTB)
		if err != nil {
			t.Fatal(err)
		}
		before := liveHeap()
		disp, _, _ := e.paint(rb)
		e.settleDrawn(disp)
		if held := liveHeap() - before; held > e.drawn+heapNoise {
			t.Errorf("%s: the display list holds %d bytes, past the %d charged", name, held, e.drawn)
		}
		if e.drawn != displayCost(disp) {
			t.Errorf("%s: %d charged for a display list of %d", name, e.drawn, displayCost(disp))
		}
		runtime.KeepAlive(disp)
		e.release()
		done()
	}
}

func TestSettleDrawn_GivesBackWhatTheListDoesNotHold(t *testing.T) {
	ctx, b, done := limits.WithBudget(context.Background(), limits.Limits{})
	defer done()
	e := newEngine(ctx, ahem(t), Options{}, 100, 100)
	e.chargeDrawn(10_000)
	d := &ditem{group: true}
	e.settleDrawn(d)
	if e.drawn != ditemCost || e.held != ditemCost || b.Held() != ditemCost {
		t.Errorf("after settling an empty list, drawn %d, held %d, budget %d, want %d", e.drawn, e.held, b.Held(), ditemCost)
	}
	e.release()
	if b.Held() != 0 {
		t.Errorf("the budget holds %d bytes once the engine is released", b.Held())
	}
}
