package raster

import (
	"errors"
	"image"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
)

// row returns a 1-pixel-high mask of the given coverages.
func row(v ...uint8) *image.Alpha {
	m := image.NewAlpha(image.Rect(0, 0, len(v), 1))
	copy(m.Pix, v)
	return m
}

// col returns a 1-pixel-wide mask of the given coverages.
func col(v ...uint8) *image.Alpha {
	m := image.NewAlpha(image.Rect(0, 0, 1, len(v)))
	copy(m.Pix, v)
	return m
}

// band returns a 3-pixel-high mask whose rows are the given coverages.
func band(v ...uint8) *image.Alpha {
	m := image.NewAlpha(image.Rect(0, 0, len(v), 3))
	for y := range 3 {
		copy(m.Pix[y*m.Stride:], v)
	}
	return m
}

// middle returns the middle row of a 3-pixel-high mask.
func middle(m *image.Alpha) *image.Alpha {
	w := m.Bounds().Dx()
	return row(m.Pix[m.Stride : m.Stride+w]...)
}

func rect(x0, y0, x1, y1 float64) []compose.PathOp {
	p := func(x, y float64) [3]compose.Point { return [3]compose.Point{{X: x, Y: y}} }
	return []compose.PathOp{
		{Kind: compose.MoveTo, P: p(x0, y0)},
		{Kind: compose.LineTo, P: p(x1, y0)},
		{Kind: compose.LineTo, P: p(x1, y1)},
		{Kind: compose.LineTo, P: p(x0, y1)},
		{Kind: compose.ClosePath},
	}
}

func TestNewMask_Bounds(t *testing.T) {
	for _, tc := range []struct {
		w, h int
		ok   bool
	}{
		{1, 1, true}, {2048, 2048, true}, {0, 4, false}, {4, -1, false}, {MaxPixels, 2, false}, {1 << 30, 1 << 30, false},
	} {
		m, err := NewMask(tc.w, tc.h)
		if tc.ok != (err == nil) {
			t.Errorf("NewMask(%d, %d) error %v, want ok %v", tc.w, tc.h, err, tc.ok)
		}
		if err != nil && !errors.Is(err, ErrTooLarge) {
			t.Errorf("NewMask(%d, %d) error %v, want ErrTooLarge", tc.w, tc.h, err)
		}
		if err == nil && (m.Bounds().Dx() != tc.w || m.Bounds().Dy() != tc.h) {
			t.Errorf("NewMask(%d, %d) bounds %v", tc.w, tc.h, m.Bounds())
		}
	}
}

func TestDraw_Coverage(t *testing.T) {
	reverse := func(ops []compose.PathOp) []compose.PathOp {
		return []compose.PathOp{ops[0], ops[3], ops[2], ops[1], ops[4]}
	}
	for _, tc := range []struct {
		name  string
		paths []Path
		tr    compose.Matrix
		want  []uint8
	}{
		{"square", []Path{{Ops: rect(1, 1, 3, 3)}}, compose.Identity, []uint8{
			0, 0, 0, 0,
			0, 255, 255, 0,
			0, 255, 255, 0,
			0, 0, 0, 0,
		}},
		{"scaled", []Path{{Ops: rect(0.5, 0.5, 1.5, 1.5)}}, compose.Matrix{A: 2, D: 2}, []uint8{
			0, 0, 0, 0,
			0, 255, 255, 0,
			0, 255, 255, 0,
			0, 0, 0, 0,
		}},
		{"half pixel", []Path{{Ops: rect(0, 0, 0.5, 4)}}, compose.Identity, []uint8{
			128, 0, 0, 0,
			128, 0, 0, 0,
			128, 0, 0, 0,
			128, 0, 0, 0,
		}},
		{"opposite subpaths cancel", []Path{{Ops: append(rect(0, 0, 4, 4), reverse(rect(1, 1, 3, 3))...)}}, compose.Identity, []uint8{
			255, 255, 255, 255,
			255, 0, 0, 255,
			255, 0, 0, 255,
			255, 255, 255, 255,
		}},
		{"same subpaths union", []Path{{Ops: append(rect(0, 0, 4, 4), rect(1, 1, 3, 3)...)}}, compose.Identity, slices.Repeat([]uint8{255}, 16)},
		{"paths union", []Path{{Ops: rect(0, 0, 2, 4)}, {Ops: reverse(rect(1, 0, 4, 4))}}, compose.Identity, slices.Repeat([]uint8{255}, 16)},
		{"stroke", []Path{{Ops: []compose.PathOp{{Kind: compose.MoveTo, P: [3]compose.Point{{X: -2, Y: 2}}}, {Kind: compose.LineTo, P: [3]compose.Point{{X: 6, Y: 2}}}}, Width: 2}}, compose.Identity, []uint8{
			0, 0, 0, 0,
			255, 255, 255, 255,
			255, 255, 255, 255,
			0, 0, 0, 0,
		}},
		{"non-finite skipped", []Path{{Ops: rect(0, 0, 1e30, 4)}}, compose.Identity, make([]uint8, 16)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := image.NewAlpha(image.Rect(0, 0, 4, 4))
			Draw(m, tc.paths, tc.tr)
			if !slices.Equal(m.Pix, tc.want) {
				t.Errorf("coverage %v, want %v", m.Pix, tc.want)
			}
		})
	}
}

func TestDraw_StrokeClosedRing(t *testing.T) {
	m := image.NewAlpha(image.Rect(0, 0, 8, 8))
	Draw(m, []Path{{Ops: rect(2, 2, 6, 6), Width: 2}}, compose.Identity)
	for _, tc := range []struct {
		x, y int
		want uint8
	}{{4, 4, 0}, {2, 4, 255}, {1, 4, 255}, {4, 1, 255}, {0, 4, 0}, {0, 0, 0}} {
		if got := m.AlphaAt(tc.x, tc.y).A; got != tc.want {
			t.Errorf("pixel (%d, %d) = %d, want %d", tc.x, tc.y, got, tc.want)
		}
	}
}

func TestTent_Weights(t *testing.T) {
	for _, tc := range []struct {
		r    float64
		want Kernel
	}{
		{0, Kernel{1}},
		{-1, Kernel{1}},
		{1, Kernel{0.25, 0.5, 0.25}},
		{2, Kernel{1.0 / 9, 2.0 / 9, 3.0 / 9, 2.0 / 9, 1.0 / 9}},
		{1.5, Kernel{0.5 / 6.5, 1.5 / 6.5, 2.5 / 6.5, 1.5 / 6.5, 0.5 / 6.5}},
	} {
		got := Tent(tc.r)
		if len(got) != len(tc.want) {
			t.Fatalf("Tent(%v) = %v, want %v", tc.r, got, tc.want)
		}
		for i := range got {
			if d := got[i] - tc.want[i]; d > 1e-12 || d < -1e-12 {
				t.Errorf("Tent(%v) = %v, want %v", tc.r, got, tc.want)
				break
			}
		}
	}
	if n := len(Tent(1e9)); n != 2*MaxKernelRadius+1 {
		t.Errorf("Tent(1e9) has %d weights, want the cap's %d", n, 2*MaxKernelRadius+1)
	}
}

func TestGaussian_Weights(t *testing.T) {
	if k := Gaussian(0); !slices.Equal(k, Kernel{1}) {
		t.Errorf("Gaussian(0) = %v, want the identity", k)
	}
	k := Gaussian(1)
	if len(k) != 7 {
		t.Fatalf("Gaussian(1) has %d weights, want 7", len(k))
	}
	sum := 0.0
	for i, w := range k {
		sum += w
		if w != k[len(k)-1-i] {
			t.Errorf("Gaussian(1) = %v is not symmetric", k)
		}
	}
	if d := sum - 1; d > 1e-12 || d < -1e-12 {
		t.Errorf("Gaussian(1) sums to %v", sum)
	}
	if !(k[3] > k[2] && k[2] > k[1] && k[1] > k[0]) {
		t.Errorf("Gaussian(1) = %v does not fall from its middle", k)
	}
}

func TestBlur_Values(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   *image.Alpha
		k    Kernel
		want []uint8
	}{
		{"impulse", row(0, 0, 255, 0, 0), Tent(1), []uint8{0, 64, 128, 64, 0}},
		{"edge repeats", row(255, 0, 0), Tent(1), []uint8{191, 64, 0}},
		{"step ramps over 2r", row(0, 0, 0, 0, 255, 255, 255, 255), Tent(2), []uint8{0, 0, 28, 85, 170, 227, 255, 255}},
		{"vertical", col(0, 255, 0), Tent(1), []uint8{64, 128, 64}},
		{"identity", row(1, 2, 3), Kernel{1}, []uint8{1, 2, 3}},
		{"even kernel copies", row(1, 2, 3), Kernel{0.5, 0.5}, []uint8{1, 2, 3}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Blur(tc.in, tc.k)
			if !slices.Equal(got.Pix, tc.want) {
				t.Errorf("Blur = %v, want %v", got.Pix, tc.want)
			}
		})
	}
}

func TestMorphology_Values(t *testing.T) {
	for _, tc := range []struct {
		name string
		got  *image.Alpha
		want []uint8
	}{
		{"dilate", Dilate(row(0, 0, 255, 0, 0, 0), 1), []uint8{0, 255, 255, 255, 0, 0}},
		{"dilate keeps the largest", Dilate(row(10, 0, 0, 200, 0), 2), []uint8{10, 200, 200, 200, 200}},
		{"erode from the edges", middle(Erode(band(255, 255, 255, 255, 255), 1)), []uint8{0, 255, 255, 255, 0}},
		{"erode keeps the smallest", middle(Erode(band(0, 90, 255, 255, 255, 255, 255, 255), 1)), []uint8{0, 0, 90, 255, 255, 255, 255, 0}},
		{"erode a thin row away", Erode(row(255, 255, 255), 1), []uint8{0, 0, 0}},
		{"erode vertical", Erode(col(255, 255, 255), 1), []uint8{0, 0, 0}},
		{"zero radius copies", Dilate(row(1, 2), 0), []uint8{1, 2}},
	} {
		if !slices.Equal(tc.got.Pix, tc.want) {
			t.Errorf("%s = %v, want %v", tc.name, tc.got.Pix, tc.want)
		}
	}
}

func TestThresholdMultiply_Values(t *testing.T) {
	m := row(0, 1, 128, 255)
	Threshold(m)
	if want := []uint8{0, 255, 255, 255}; !slices.Equal(m.Pix, want) {
		t.Errorf("Threshold = %v, want %v", m.Pix, want)
	}
	a := row(255, 128, 64, 0)
	Multiply(a, row(128, 128, 255, 255))
	if want := []uint8{128, 64, 64, 0}; !slices.Equal(a.Pix, want) {
		t.Errorf("Multiply = %v, want %v", a.Pix, want)
	}
}

func TestTint_Values(t *testing.T) {
	img := Tint(row(255, 128, 0), compose.Color{R: 1, G: 0.5, B: 0, A: 0.5})
	want := []uint8{255, 128, 0, 128, 255, 128, 0, 64, 255, 128, 0, 0}
	if !slices.Equal(img.Pix, want) {
		t.Errorf("Tint = %v, want %v", img.Pix, want)
	}
}

func TestPaintMask_Values(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 3, 1))
	Paint(img, row(255, 255, 0), compose.Color{R: 1, A: 1})
	Paint(img, row(0, 128, 255), compose.Color{B: 1, A: 1})
	want := []uint8{255, 0, 0, 255, 127, 0, 128, 255, 0, 0, 255, 255}
	if !slices.Equal(img.Pix, want) {
		t.Errorf("Paint = %v, want %v", img.Pix, want)
	}
	Mask(img, row(255, 128, 0))
	if got := []uint8{img.Pix[3], img.Pix[7], img.Pix[11]}; !slices.Equal(got, []uint8{255, 128, 0}) {
		t.Errorf("Mask alphas %v, want [255 128 0]", got)
	}
}
