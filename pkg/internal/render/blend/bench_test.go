package blend

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/render/surface"
)

// benchSide is the device side of the benchmarks: a full A4 page at about
// 120 dpi.
const benchSide = 1000

func benchGroup(b *testing.B, attrs Attributes) *Group {
	b.Helper()
	page, err := NewPage(benchSide, benchSide, benchSide*benchSide)
	if err != nil {
		b.Fatal(err)
	}
	page.Canvas().Fill(0.5, 0.6, 0.7, 1)
	g, err := NewGroup(page.Bounds(), page.Backdrop(), attrs, benchSide*benchSide)
	if err != nil {
		b.Fatal(err)
	}
	return g
}

// benchmarkPaint measures one element covering every pixel of a group at
// partial shape and alpha, the per-pixel cost of 11.4.8.
func benchmarkPaint(b *testing.B, mode Mode, attrs Attributes) {
	g := benchGroup(b, attrs)
	cs := [3]float32{0.2, 0.4, 0.9}
	b.ReportAllocs()
	for b.Loop() {
		for y := range benchSide {
			for x := range benchSide {
				g.Paint(x, y, mode, cs, 0.9, 0.6)
			}
		}
	}
}

func BenchmarkPaintNormal(b *testing.B)     { benchmarkPaint(b, Normal, Attributes{Isolated: true}) }
func BenchmarkPaintMultiply(b *testing.B)   { benchmarkPaint(b, Multiply, Attributes{}) }
func BenchmarkPaintSoftLight(b *testing.B)  { benchmarkPaint(b, SoftLight, Attributes{}) }
func BenchmarkPaintLuminosity(b *testing.B) { benchmarkPaint(b, Luminosity, Attributes{}) }
func BenchmarkPaintKnockout(b *testing.B)   { benchmarkPaint(b, Multiply, Attributes{Knockout: true}) }

// BenchmarkInto measures compositing a full-page non-isolated group into
// its parent, backdrop removal included.
func BenchmarkInto(b *testing.B) {
	ctx := context.Background()
	page, err := NewPage(benchSide, benchSide, benchSide*benchSide)
	if err != nil {
		b.Fatal(err)
	}
	g, err := NewGroup(page.Bounds(), page.Backdrop(), Attributes{}, benchSide*benchSide)
	if err != nil {
		b.Fatal(err)
	}
	for y := range benchSide {
		for x := range benchSide {
			g.Paint(x, y, Multiply, [3]float32{0.2, 0.4, 0.9}, 1, 0.6)
		}
	}
	b.ReportAllocs()
	for b.Loop() {
		if err := g.Into(ctx, page, Params{Mode: Screen, Opacity: surface.NewOpacity(0.8)}); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkLuminosityMask measures deriving a full-page luminosity mask.
func BenchmarkLuminosityMask(b *testing.B) {
	ctx := context.Background()
	g := benchGroup(b, Attributes{Isolated: true})
	for y := range benchSide {
		for x := range benchSide {
			v := float32(x) / benchSide
			g.Paint(x, y, Normal, [3]float32{v, v, v}, 1, 1)
		}
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := LuminosityMask(ctx, g, [3]float32{}, nil, benchSide, benchSide, benchSide*benchSide); err != nil {
			b.Fatal(err)
		}
	}
}
