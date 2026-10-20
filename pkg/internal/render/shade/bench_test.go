package shade

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/render/geom"
	"github.com/gotenberg/litho/pkg/internal/render/surface"
)

// benchSide is the canvas side of the benchmarks: the fixtures scaled ten
// times, a page region at about 720 dpi or a full A4 page at 120 dpi.
const benchSide = 1000

// benchmarkPaint measures painting a fixture's shading over a benchSide
// canvas. Types 1 to 3 cost one sample per pixel; meshes cost the area of
// their triangles plus one resolve per pixel.
func benchmarkPaint(b *testing.B, f fixture) {
	ctx, doc := openFixture(b, f.bytes())
	sh, err := New(ctx, doc, ast.PdfIndirectRef{ObjectNum: 5}, deviceComponents)
	if err != nil {
		b.Fatal(err)
	}
	canvas, err := surface.NewCanvas(benchSide, benchSide, benchSide*benchSide)
	if err != nil {
		b.Fatal(err)
	}
	toDevice := geom.Matrix{benchSide / pageSide, 0, 0, -benchSide / pageSide, 0, benchSide}
	target := Target{Canvas: canvas, Convert: deviceRGB{}}
	b.ReportAllocs()
	for b.Loop() {
		if err := sh.Paint(ctx, target, toDevice); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPaintFunctionBased(b *testing.B) { benchmarkPaint(b, fixtureFunctionBased) }
func BenchmarkPaintAxial(b *testing.B)         { benchmarkPaint(b, fixtureAxial) }
func BenchmarkPaintRadial(b *testing.B)        { benchmarkPaint(b, fixtureRadial) }
func BenchmarkPaintGouraud(b *testing.B)       { benchmarkPaint(b, fixtureGouraud) }
func BenchmarkPaintCoons(b *testing.B)         { benchmarkPaint(b, fixtureCoons) }
func BenchmarkPaintTensor(b *testing.B)        { benchmarkPaint(b, fixtureTensor) }
