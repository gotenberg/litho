package effect

import (
	"context"
	"encoding/binary"
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// opsOf decodes a path from data: a byte of kind, then the op's points
// as pairs of float32, until data runs out.
func opsOf(data []byte) []typeset.PathOp {
	var ops []typeset.PathOp
	f := func() (float64, bool) {
		if len(data) < 4 {
			return 0, false
		}
		v := math.Float32frombits(binary.LittleEndian.Uint32(data))
		data = data[4:]
		return float64(v), true
	}
	for len(data) > 0 {
		kind := typeset.OpKind(data[0] % 4)
		data = data[1:]
		op := typeset.PathOp{Kind: kind}
		n := map[typeset.OpKind]int{typeset.MoveTo: 1, typeset.LineTo: 1, typeset.CurveTo: 3}[kind]
		for i := range n {
			x, ok1 := f()
			y, ok2 := f()
			if !ok1 || !ok2 {
				return ops
			}
			op.P[i] = typeset.Point{X: x, Y: y}
		}
		ops = append(ops, op)
	}
	return ops
}

func fuzzSeedPath() []byte {
	var b []byte
	pt := func(kind byte, xy ...float32) {
		b = append(b, kind)
		for _, v := range xy {
			b = binary.LittleEndian.AppendUint32(b, math.Float32bits(v))
		}
	}
	pt(0, 2, 3)
	pt(1, 30, 4)
	pt(2, 35, 20, 20, 35, 5, 25)
	pt(3)
	return b
}

// FuzzFill covers a small surface with a path decoded from the input,
// filled by both rules and stroked: no panic, and within a budget of a
// few megabytes whatever the path.
func FuzzFill(f *testing.F) {
	valid := fuzzSeedPath()
	f.Add(valid, uint8(40), uint8(40), 1.5, 1.0)
	f.Add(valid[:len(valid)-7], uint8(3), uint8(200), 0.0, -2.0)
	f.Fuzz(func(t *testing.T, data []byte, w, h uint8, width, scale float64) {
		ctx, _, done := limits.WithBudget(context.Background(), limits.Limits{MaxConvertMemoryBytes: 8 << 20})
		defer done()
		s, err := New(ctx, Coverage, int(w)+1, int(h)+1, Grid{})
		if err != nil {
			t.Fatal(err)
		}
		ops := opsOf(data)
		tr := typeset.Matrix{A: scale, D: scale, E: 1, F: -1}
		for _, p := range []Path{{Ops: ops}, {Ops: ops, Rule: EvenOdd}, {Ops: ops, Width: width}} {
			if err := Fill(ctx, s, []Path{p}, tr); err != nil && ctx.Err() == nil {
				t.Fatalf("Fill: %v", err)
			}
		}
	})
}

// FuzzPlan plans grids for any deviation and extent: a grid either
// refused or within the caps.
func FuzzPlan(f *testing.F) {
	f.Add(2.0, 100.0, 50.0)
	f.Add(math.NaN(), 1e300, -1.0)
	f.Fuzz(func(t *testing.T, sigma, w, h float64) {
		r, ok := Plan(sigma, w, h)
		if !ok {
			return
		}
		if !fits(r.W, r.H) || r.W < 1 || r.H < 1 || !(r.Scale > 0) || r.Scale > MaxScale {
			t.Fatalf("Plan(%v, %v, %v) = %+v", sigma, w, h, r)
		}
	})
}

// FuzzGeometry runs the primitives that take sizes, offsets and
// coordinates from a document on a small surface: no panic, no kernel
// past its cap.
func FuzzGeometry(f *testing.F) {
	f.Add(1.5, -2.25, 0.8, 0.2, -0.1, 1.3, 4.0, 7, 3, 2.0)
	f.Add(math.Inf(1), math.NaN(), 1e300, -1e300, 0.0, 0.0, -5.0, -1, 1<<30, 1e9)
	f.Fuzz(func(t *testing.T, dx, dy, a, b, c, d, sigma float64, width, passes int, r float64) {
		ctx, _, done := limits.WithBudget(context.Background(), limits.Limits{MaxConvertMemoryBytes: 64 << 20})
		defer done()
		s, err := New(ctx, RGBA, 17, 9, Grid{})
		if err != nil {
			t.Fatal(err)
		}
		_ = Flood(ctx, s, Color{10, 20, 30, 200})
		for _, k := range []Kernel{GaussianKernel(sigma), TentKernel(r), BoxKernel(width, passes)} {
			if len(k.W) > MaxKernelRadius+1 {
				t.Fatalf("kernel of %d weights", len(k.W))
			}
		}
		if err := Blur(ctx, s, GaussianKernel(sigma), BoxKernel(width, passes), EdgeWrap); err != nil && ctx.Err() == nil {
			t.Fatalf("Blur: %v", err)
		}
		_ = Offset(ctx, s, dx, dy)
		_ = Dilate(ctx, s, int(r), width)
		_ = AlphaRamp(ctx, s, dx, dy, a, b, c, d)
		dst, err := New(ctx, RGBA, 11, 13, Grid{})
		if err != nil {
			t.Fatal(err)
		}
		_ = Transform(ctx, dst, s, typeset.Matrix{A: a, B: b, C: c, D: d, E: dx, F: dy})
		_ = Tile(ctx, dst, s, width, passes, width+3, passes+2)
		_ = ColorMatrix(ctx, dst, Matrix{a, b, c, d, dx, dy, sigma, r, a, b, c, d, dx, dy, sigma, r, a, b, c, d})
		_ = Arithmetic(ctx, dst, dst, a, b, c, d)
		lut := TransferFunc{Kind: TransferGamma, Amplitude: a, Exponent: b, Offset: c}.Table()
		_ = Transfer(ctx, dst, &lut, &lut, &lut, &lut)
	})
}
