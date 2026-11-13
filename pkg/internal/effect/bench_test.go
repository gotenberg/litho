package effect

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typeset"
)

const benchSide = 1000

// benchSurface returns a 1,000 by 1,000 surface of format f holding a
// large antialiased disc, colour from a fixed sequence for RGBA.
func benchSurface(b *testing.B, f Format) *Surface {
	b.Helper()
	cov := filled(b, benchSide, benchSide, typeset.Identity, Path{Ops: ellipse(500, 500, 420, 350)})
	if f == Coverage {
		return cov
	}
	s, err := Tint(context.Background(), cov, Color{200, 120, 40, 230})
	if err != nil {
		b.Fatal(err)
	}
	return s
}

func benchRun(b *testing.B, f Format, op func(ctx context.Context, s *Surface) error) {
	ctx := context.Background()
	s := benchSurface(b, f)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if err := op(ctx, s); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFill(b *testing.B) {
	s := newSurface(b, Coverage, benchSide, benchSide)
	paths := []Path{{Ops: ellipse(500, 500, 420, 350)}, {Ops: star(500, 500, 480), Rule: EvenOdd}}
	b.ReportAllocs()
	for b.Loop() {
		if err := Fill(context.Background(), s, paths, typeset.Identity); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFillStroke(b *testing.B) {
	s := newSurface(b, Coverage, benchSide, benchSide)
	paths := []Path{{Ops: ellipse(500, 500, 420, 350), Width: 12}}
	b.ReportAllocs()
	for b.Loop() {
		if err := Fill(context.Background(), s, paths, typeset.Identity); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBlurGaussian5Coverage(b *testing.B) {
	k := GaussianKernel(5)
	benchRun(b, Coverage, func(ctx context.Context, s *Surface) error { return Blur(ctx, s, k, k, EdgeNone) })
}

func BenchmarkBlurGaussian5RGBA(b *testing.B) {
	k := GaussianKernel(5)
	benchRun(b, RGBA, func(ctx context.Context, s *Surface) error { return Blur(ctx, s, k, k, EdgeNone) })
}

func BenchmarkBlurGaussian20Coverage(b *testing.B) {
	k := GaussianKernel(20)
	benchRun(b, Coverage, func(ctx context.Context, s *Surface) error { return Blur(ctx, s, k, k, EdgeNone) })
}

func BenchmarkBlurBox3Coverage(b *testing.B) {
	k := BoxKernel(29, 3)
	benchRun(b, Coverage, func(ctx context.Context, s *Surface) error { return Blur(ctx, s, k, k, EdgeNone) })
}

func BenchmarkDilate(b *testing.B) {
	benchRun(b, Coverage, func(ctx context.Context, s *Surface) error { return Dilate(ctx, s, 8, 8) })
}

func BenchmarkErodeRGBA(b *testing.B) {
	benchRun(b, RGBA, func(ctx context.Context, s *Surface) error { return Erode(ctx, s, 8, 8) })
}

func BenchmarkOffset(b *testing.B) {
	benchRun(b, RGBA, func(ctx context.Context, s *Surface) error { return Offset(ctx, s, 0.37, -0.61) })
}

func BenchmarkTransform(b *testing.B) {
	dst := newSurface(b, RGBA, benchSide, benchSide)
	m := typeset.Matrix{A: 0.8, B: 0.6, C: -0.6, D: 0.8, E: 300, F: -200}
	benchRun(b, RGBA, func(ctx context.Context, s *Surface) error { return Transform(ctx, dst, s, m) })
}

func BenchmarkTile(b *testing.B) {
	dst := newSurface(b, RGBA, benchSide, benchSide)
	benchRun(b, RGBA, func(ctx context.Context, s *Surface) error { return Tile(ctx, dst, s, 100, 100, 230, 170) })
}

func BenchmarkColorMatrix(b *testing.B) {
	m := HueRotateMatrix(40)
	benchRun(b, RGBA, func(ctx context.Context, s *Surface) error { return ColorMatrix(ctx, s, m) })
}

func BenchmarkTransfer(b *testing.B) {
	lut := TransferFunc{Kind: TransferGamma, Amplitude: 1, Exponent: 2.2}.Table()
	benchRun(b, RGBA, func(ctx context.Context, s *Surface) error { return Transfer(ctx, s, &lut, &lut, &lut, &IdentityLUT) })
}

func BenchmarkFlood(b *testing.B) {
	benchRun(b, RGBA, func(ctx context.Context, s *Surface) error { return Flood(ctx, s, Color{1, 2, 3, 4}) })
}

func BenchmarkTint(b *testing.B) {
	benchRun(b, Coverage, func(ctx context.Context, s *Surface) error {
		out, err := Tint(ctx, s, Color{10, 20, 30, 200})
		if err == nil {
			out.Release()
		}
		return err
	})
}

func BenchmarkAlphaRamp(b *testing.B) {
	benchRun(b, RGBA, func(ctx context.Context, s *Surface) error { return AlphaRamp(ctx, s, 0, 100, 0, 900, 0.5, 0) })
}

func BenchmarkCompositeOver(b *testing.B) {
	src := benchSurface(b, RGBA)
	benchRun(b, RGBA, func(ctx context.Context, s *Surface) error { return Composite(ctx, s, src, Over) })
}

func BenchmarkArithmetic(b *testing.B) {
	src := benchSurface(b, RGBA)
	benchRun(b, RGBA, func(ctx context.Context, s *Surface) error { return Arithmetic(ctx, s, src, 0.5, 0.5, 0.5, 0) })
}

func BenchmarkBlendMultiply(b *testing.B) {
	src := benchSurface(b, RGBA)
	benchRun(b, RGBA, func(ctx context.Context, s *Surface) error { return Blend(ctx, s, src, Multiply) })
}

func BenchmarkBlendHue(b *testing.B) {
	src := benchSurface(b, RGBA)
	benchRun(b, RGBA, func(ctx context.Context, s *Surface) error { return Blend(ctx, s, src, Hue) })
}
