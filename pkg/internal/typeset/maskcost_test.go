package typeset

import (
	"context"
	"runtime"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/models"
)

// TestMaskCostCoversATranslucentFill holds [maskCost] to what the writer
// holds for each fill of a shading with a translucent stop until the file
// is written: what a page of them holds once written stays within what
// the budget accounts for it.
func TestMaskCostCoversATranslucentFill(t *testing.T) {
	ctx, mem, cancel := limits.WithBudget(context.Background(), models.Limits{})
	defer cancel()
	const n = 16_000
	w := newDocWriter(ctx, Options{Spool: -1}, Version17, false)
	content := make([]Fragment, 0, n)
	for i := range n {
		x, y := float64(i%50*10), float64(i/50*10)
		sh := &Shading{To: Point{X: 5}, Transform: Matrix{A: 1, D: 1, E: x, F: y}, Stops: []ColorStop{{Color: Color{R: 1, A: 0.5}}, {Offset: 1, Color: Color{B: 1, A: 1}}}}
		content = append(content, rectPath(Rect{X: x, Y: y, Width: 10, Height: 10}, &Fill{Shading: sh}))
	}
	pg := &Page{Width: 600, Height: 3300, Content: content}
	before := liveHeap()
	if err := w.page(ctx, pg, nil); err != nil {
		t.Fatal(err)
	}
	if err := w.spill(ctx); err != nil {
		t.Fatal(err)
	}
	held := liveHeap() - before
	if charged := mem.Held(); held > charged {
		t.Errorf("the writer holds %d bytes for %d translucent fills, %d a fill, past the %d charged", held, n, held/n, charged)
	}
	runtime.KeepAlive(w)
	runtime.KeepAlive(pg)
}

// TestPatternCostCoversAPattern holds [patternCost] to what the writer
// holds for each fill of a tiling pattern until the file is written:
// what a page of them holds once written stays within what the budget
// accounts for it.
func TestPatternCostCoversAPattern(t *testing.T) {
	for name, translucent := range map[string]bool{"opaque cells": false, "translucent cells": true} {
		ctx, mem, cancel := limits.WithBudget(context.Background(), models.Limits{})
		const n = 2_000
		w := newDocWriter(ctx, Options{Spool: -1}, Version17, false)
		a := 1.0
		if translucent {
			a = 0.5
		}
		content := make([]Fragment, 0, n)
		for i := range n {
			x, y := float64(i%20*30), float64(i/20*30)
			sh := &Shading{To: Point{X: 5}, Stops: []ColorStop{{Color: Color{R: 1, A: a}}, {Offset: 1, Color: Color{B: 1, A: 1}}}}
			cell := rectPath(Rect{Width: 10, Height: 10}, &Fill{Shading: sh})
			content = append(content, rectPath(Rect{X: x, Y: y, Width: 28, Height: 28}, &Fill{Tiling: &Tiling{
				Content: []Fragment{cell}, Width: 10, Height: 10, XStep: 10, YStep: 10, Transform: Matrix{A: 1, D: 1, E: x, F: y},
			}}))
		}
		pg := &Page{Width: 600, Height: 3000, Content: content}
		before := liveHeap()
		if err := w.page(ctx, pg, nil); err != nil {
			t.Fatal(err)
		}
		if err := w.spill(ctx); err != nil {
			t.Fatal(err)
		}
		held := liveHeap() - before
		if charged := mem.Held(); held > charged {
			t.Errorf("%s: the writer holds %d bytes for %d pattern fills, %d a fill, past the %d charged", name, held, n, held/n, charged)
		}
		runtime.KeepAlive(w)
		runtime.KeepAlive(pg)
		cancel()
	}
}
