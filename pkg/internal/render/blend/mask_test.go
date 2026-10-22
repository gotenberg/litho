package blend

import (
	"context"
	"errors"
	"image"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics/function"
	"github.com/gotenberg/litho/pkg/internal/render/surface"
)

// exponential compiles a type 2 function from c0 to c1 with exponent n
// (ISO 32000-1, 7.10.3).
func exponential(t *testing.T, c0, c1 ast.PdfArray, n float64) *function.Function {
	t.Helper()
	fn, err := function.New(context.Background(), nil, ast.PdfDict{
		"FunctionType": ast.PdfInt(2),
		"Domain":       ast.PdfArray{ast.PdfInt(0), ast.PdfInt(1)},
		"C0":           c0,
		"C1":           c1,
		"N":            ast.PdfReal(n),
	})
	if err != nil {
		t.Fatal(err)
	}
	return fn
}

// gradient paints an isolated group over x in [1, 12) of a 13x2 device: a
// grey ramp from 0 to 1 in steps of 0.1, at the given alpha.
func gradient(t *testing.T, backdrop Backdrop, attrs Attributes, alpha float32) *Group {
	t.Helper()
	g, err := NewGroup(image.Rect(1, 0, 12, 2), backdrop, attrs, testBudget)
	if err != nil {
		t.Fatal(err)
	}
	for y := range 2 {
		for x := 1; x < 12; x++ {
			v := float32(x-1) / 10
			g.Paint(x, y, Normal, Components{v, v, v}, 1, alpha)
		}
	}
	return g
}

// TestLuminosityMaskOfGradient checks 11.5.3 on a grey ramp: over a black
// backdrop an opaque ramp's mask is the ramp itself; at half alpha over
// white it is (1 + v) / 2; outside the group it is the backdrop's
// luminosity; and an inverting /TR flips the values (11.6.5.2).
func TestLuminosityMaskOfGradient(t *testing.T) {
	ctx := context.Background()
	black, white := Components{}, Components{1, 1, 1}

	m, err := LuminosityMask(ctx, gradient(t, Backdrop{}, Attributes{Isolated: true}, 1), black, nil, testBudget)
	if err != nil {
		t.Fatal(err)
	}
	for x := 1; x < 12; x++ {
		if got, want := m.At(x, 1), float32(x-1)/10; !near32(got, want) {
			t.Errorf("opaque ramp over black at x=%d: %g, want %g", x, got, want)
		}
	}
	if got := m.At(12, 0); got != 0 {
		t.Errorf("outside over black: %g, want 0", got)
	}

	// The same ramp, non-isolated over the white backdrop the mask defines,
	// at half alpha: the group colour is the ramp, so C = (1 + v) / 2.
	m, err = LuminosityMask(ctx, gradient(t, Opaque(white), Attributes{}, 0.5), white, nil, testBudget)
	if err != nil {
		t.Fatal(err)
	}
	for x := 1; x < 12; x++ {
		if got, want := m.At(x, 0), (1+float32(x-1)/10)/2; !near32(got, want) {
			t.Errorf("half-alpha ramp over white at x=%d: %g, want %g", x, got, want)
		}
	}
	if got := m.At(0, 0); !near32(got, 1) {
		t.Errorf("outside over white: %g, want 1", got)
	}

	invert := exponential(t, ast.PdfArray{ast.PdfInt(1)}, ast.PdfArray{ast.PdfInt(0)}, 1)
	m, err = LuminosityMask(ctx, gradient(t, Backdrop{}, Attributes{Isolated: true}, 1), black, invert, testBudget)
	if err != nil {
		t.Fatal(err)
	}
	for x := 1; x < 12; x++ {
		if got, want := m.At(x, 0), 1-float32(x-1)/10; !near32(got, want) {
			t.Errorf("inverted ramp at x=%d: %g, want %g", x, got, want)
		}
	}
	if got := m.At(0, 1); !near32(got, 1) {
		t.Errorf("inverted outside: %g, want TR(0) = 1", got)
	}
}

// TestAlphaMask checks 11.5.2: the mask is the group alpha through /TR, its
// colour ignored, and TR(0) outside the group.
func TestAlphaMask(t *testing.T) {
	ctx := context.Background()
	square := exponential(t, ast.PdfArray{ast.PdfInt(0)}, ast.PdfArray{ast.PdfInt(1)}, 2)
	m, err := AlphaMask(ctx, gradient(t, Backdrop{}, Attributes{Isolated: true}, 0.6), square, testBudget)
	if err != nil {
		t.Fatal(err)
	}
	for x := 1; x < 12; x++ {
		if got := m.At(x, 0); !near32(got, 0.36) {
			t.Errorf("x=%d: %g, want 0.6² = 0.36", x, got)
		}
	}
	if got := m.At(0, 0); got != 0 {
		t.Errorf("outside: %g, want 0", got)
	}
}

// TestCoarseMask checks a mask read at 4 device pixels a value, its two
// values over device pixels 4 to 11: exact at the centre of a square, linear
// between two centres, the nearest value along its edges, and the outside
// value past them.
func TestCoarseMask(t *testing.T) {
	values := &surface.Mask{Width: 2, Height: 1, A: []float32{0, 1}}
	m := NewMask(values, image.Point{X: 1}, 0.25).Coarse(4)
	for x, want := range map[int]float32{3: 0.25, 4: 0, 5: 0, 6: 0.125, 7: 0.375, 8: 0.625, 9: 0.875, 10: 1, 11: 1, 12: 0.25} {
		if got := m.At(x, 0); !near32(got, want) {
			t.Errorf("At(%d, 0) = %g, want %g", x, got, want)
		}
	}
	if got := m.At(6, 4); got != 0.25 {
		t.Errorf("below the values: %g, want the outside value 0.25", got)
	}
	if got := m.Pixels(); got != 2 {
		t.Errorf("Pixels = %d, want 2", got)
	}
	if NewMask(values, image.Point{}, 0).Coarse(1).At(1, 0) != 1 {
		t.Error("Coarse(1) changed the mask")
	}
}

// TestConstantMask checks a mask of one value everywhere.
func TestConstantMask(t *testing.T) {
	m := ConstantMask(0)
	for _, p := range []image.Point{{}, {-5, 3}, {1 << 20, 1 << 20}} {
		if got := m.At(p.X, p.Y); got != 0 {
			t.Errorf("At(%v) = %g, want 0", p, got)
		}
	}
	if ConstantMask(2).At(0, 0) != 1 {
		t.Error("ConstantMask(2) is not clamped to 1")
	}
}

func TestTransferArity(t *testing.T) {
	rgb := exponential(t, ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(0)},
		ast.PdfArray{ast.PdfInt(1), ast.PdfInt(1), ast.PdfInt(1)}, 1)
	g := gradient(t, Backdrop{}, Attributes{Isolated: true}, 1)
	if _, err := AlphaMask(context.Background(), g, rgb, testBudget); !errors.Is(err, ErrTransfer) {
		t.Errorf("err = %v, want ErrTransfer", err)
	}
}
