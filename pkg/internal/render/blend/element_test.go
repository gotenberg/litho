package blend

import (
	"context"
	"errors"
	"image"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/render/surface"
)

// TestDirectMatchesPaint checks that Normal elements painted source over
// onto an isolated group's direct plane leave the group, once composited,
// as Paint would: 11.4.8 with B(Cb, Cs) = Cs, and the shape taken from the
// alpha.
func TestDirectMatchesPaint(t *testing.T) {
	ctx := context.Background()
	elements := []element{
		{Normal, [3]float32{0.3, 0.8, 0.5}, image.Rect(1, 0, 4, 3), 0.6, 1},
		{Normal, [3]float32{0.9, 0.1, 0.2}, image.Rect(0, 1, 3, 4), 1, 0.5},
	}
	for _, mode := range []Mode{Normal, Multiply, Difference} {
		painted, direct := newPage(t), newPage(t)
		for _, page := range []*Group{painted, direct} {
			g, err := NewGroup(image.Rect(0, 0, 4, 4), page.Backdrop(), Attributes{Isolated: true}, testBudget)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range elements {
				if page == painted {
					e.paint(g)
					continue
				}
				plane := g.Direct()
				for y := e.rect.Min.Y; y < e.rect.Max.Y; y++ {
					for x := e.rect.Min.X; x < e.rect.Max.X; x++ {
						plane.BlendOver(x, y, e.color[0], e.color[1], e.color[2], e.coverage*e.opacity)
					}
				}
			}
			if err := g.Into(ctx, page, Params{Mode: mode, Opacity: surface.NewOpacity(0.8)}); err != nil {
				t.Fatal(err)
			}
		}
		sameCanvas(t, direct.Canvas(), painted.Canvas())
	}
}

// TestDirectRefusesBlendingGroups checks that a group whose elements
// composite with anything but the stack before them has no direct plane.
func TestDirectRefusesBlendingGroups(t *testing.T) {
	page := newPage(t)
	for _, attrs := range []Attributes{{}, {Knockout: true}, {Isolated: true, Knockout: true}} {
		g, err := NewGroup(image.Rect(0, 0, 4, 4), page.Backdrop(), attrs, testBudget)
		if err != nil {
			t.Fatal(err)
		}
		if g.Direct() != nil {
			t.Errorf("%+v: a direct plane", attrs)
		}
	}
}

// TestPaintCanvasMatchesPaint checks that an element raster composites as
// its pixels painted one by one, its alpha both shape and alpha.
func TestPaintCanvasMatchesPaint(t *testing.T) {
	ctx := context.Background()
	src, err := surface.NewCanvas(3, 2, testBudget)
	if err != nil {
		t.Fatal(err)
	}
	cs := [3]float32{0.2, 0.7, 0.4}
	alphas := []float32{1, 0.5, 0, 0.25, 0.75, 1}
	for i, a := range alphas {
		src.BlendOver(i%3, i/3, cs[0], cs[1], cs[2], a)
	}
	mask, err := surface.NewMask(4, 4, testBudget)
	if err != nil {
		t.Fatal(err)
	}
	for i := range mask.A {
		mask.A[i] = 0.6
	}
	params := Params{Mode: Overlay, Opacity: surface.NewOpacity(0.9), SoftMask: NewMask(mask, image.Point{}, 0)}

	want, got := newPage(t), newPage(t)
	for i, a := range alphas {
		fs, as := params.Source(1+i%3, 2+i/3, a, a)
		want.Paint(1+i%3, 2+i/3, Overlay, cs, fs, as)
	}
	if err := got.PaintCanvas(ctx, src, image.Pt(1, 2), params); err != nil {
		t.Fatal(err)
	}
	sameCanvas(t, got.Canvas(), want.Canvas())

	cov := newPage(t)
	if err := cov.PaintCoverage(ctx, image.Rect(1, 2, 4, 4), alphas, cs, params); err != nil {
		t.Fatal(err)
	}
	sameCanvas(t, cov.Canvas(), want.Canvas())
}

// TestPaintCoverageRefusesShortCoverage checks the coverage length guard.
func TestPaintCoverageRefusesShortCoverage(t *testing.T) {
	err := newPage(t).PaintCoverage(context.Background(), image.Rect(0, 0, 2, 2), []float32{1}, [3]float32{}, Params{})
	if !errors.Is(err, ErrCoverage) {
		t.Errorf("err = %v, want ErrCoverage", err)
	}
}
