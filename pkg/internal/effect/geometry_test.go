package effect

import (
	"context"
	"math"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typeset"
)

func TestMorphology_Values(t *testing.T) {
	run := func(f func(context.Context, *Surface, int, int) error, rx, ry int, in []uint8, w int) []uint8 {
		s := newSurface(t, Coverage, w, len(in)/w)
		copy(s.Pix, in)
		if err := f(context.Background(), s, rx, ry); err != nil {
			t.Fatal(err)
		}
		return s.Pix
	}
	for _, tc := range []struct {
		name string
		got  []uint8
		want []uint8
	}{
		{"dilate row", run(Dilate, 1, 0, []uint8{0, 0, 200, 0, 0, 0, 90}, 7), []uint8{0, 200, 200, 200, 0, 90, 90}},
		{"erode row", run(Erode, 1, 0, []uint8{9, 200, 200, 200, 100, 255, 255}, 7), []uint8{0, 9, 200, 100, 100, 100, 0}},
		{"dilate column", run(Dilate, 0, 1, []uint8{0, 50, 0, 0}, 1), []uint8{50, 50, 50, 0}},
		{"dilate square", run(Dilate, 1, 1, []uint8{
			0, 0, 0, 0,
			0, 0, 0, 0,
			0, 0, 7, 0,
			0, 0, 0, 0,
		}, 4), []uint8{
			0, 0, 0, 0,
			0, 7, 7, 7,
			0, 7, 7, 7,
			0, 7, 7, 7,
		}},
		{"zero radius", run(Erode, 0, 0, []uint8{1, 2, 3}, 3), []uint8{1, 2, 3}},
	} {
		if !slices.Equal(tc.got, tc.want) {
			t.Errorf("%s = %v, want %v", tc.name, tc.got, tc.want)
		}
	}
}

func TestOffset_Values(t *testing.T) {
	run := func(dx, dy float64, in []uint8, w int) []uint8 {
		s := newSurface(t, Coverage, w, len(in)/w)
		copy(s.Pix, in)
		if err := Offset(context.Background(), s, dx, dy); err != nil {
			t.Fatal(err)
		}
		return s.Pix
	}
	for _, tc := range []struct {
		name string
		got  []uint8
		want []uint8
	}{
		{"whole right", run(2, 0, []uint8{10, 20, 30, 40}, 4), []uint8{0, 0, 10, 20}},
		{"whole left", run(-1, 0, []uint8{10, 20, 30, 40}, 4), []uint8{20, 30, 40, 0}},
		{"half right", run(0.5, 0, []uint8{200, 100, 0, 0}, 4), []uint8{100, 150, 50, 0}},
		{"quarter left", run(-0.25, 0, []uint8{0, 200, 0, 0}, 4), []uint8{50, 150, 0, 0}},
		{"down", run(0, 1, []uint8{1, 2, 3, 4}, 2), []uint8{0, 0, 1, 2}},
		{"not finite", run(math.Inf(1), 0, []uint8{1, 2}, 2), []uint8{1, 2}},
	} {
		if !slices.Equal(tc.got, tc.want) {
			t.Errorf("%s = %v, want %v", tc.name, tc.got, tc.want)
		}
	}
}

func TestTransform_Values(t *testing.T) {
	src := newSurface(t, Coverage, 4, 2)
	copy(src.Pix, []uint8{10, 20, 30, 40, 50, 60, 70, 80})
	for _, tc := range []struct {
		name string
		m    typeset.Matrix
		w, h int
		want []uint8
	}{
		{"identity", typeset.Identity, 4, 2, []uint8{10, 20, 30, 40, 50, 60, 70, 80}},
		{"flip vertically", typeset.Matrix{A: 1, D: -1, F: 2}, 4, 2, []uint8{50, 60, 70, 80, 10, 20, 30, 40}},
		{"flip horizontally", typeset.Matrix{A: -1, D: 1, E: 4}, 4, 2, []uint8{40, 30, 20, 10, 80, 70, 60, 50}},
		{"translate", typeset.Matrix{A: 1, D: 1, E: 1}, 4, 2, []uint8{0, 10, 20, 30, 0, 50, 60, 70}},
		{"halve", typeset.Matrix{A: 0.5, D: 0.5}, 2, 1, []uint8{35, 55}},
		{"singular", typeset.Matrix{A: 1}, 4, 2, make([]uint8, 8)},
		{"not finite", typeset.Matrix{A: math.NaN(), D: 1}, 4, 2, make([]uint8, 8)},
	} {
		dst := newSurface(t, Coverage, tc.w, tc.h)
		if err := Transform(context.Background(), dst, src, tc.m); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(dst.Pix, tc.want) {
			t.Errorf("%s = %v, want %v", tc.name, dst.Pix, tc.want)
		}
	}
}

// TestTransform_Rotation rotates a blurred disc about its centre: a disc
// is its own rotation, so the result keeps it within the bilinear
// resampling's error on a smooth image, two levels.
func TestTransform_Rotation(t *testing.T) {
	src := filled(t, 64, 64, typeset.Identity, Path{Ops: ellipse(32, 32, 20, 20)})
	if err := Blur(context.Background(), src, GaussianKernel(3), GaussianKernel(3), EdgeNone); err != nil {
		t.Fatal(err)
	}
	dst := newSurface(t, Coverage, 64, 64)
	s, c := math.Sincos(0.7)
	m := typeset.Matrix{A: c, B: s, C: -s, D: c}
	m.E, m.F = 32-(m.A*32+m.C*32), 32-(m.B*32+m.D*32)
	if err := Transform(context.Background(), dst, src, m); err != nil {
		t.Fatal(err)
	}
	worst := 0
	for i := range src.Pix {
		d := int(dst.Pix[i]) - int(src.Pix[i])
		worst = max(worst, d, -d)
	}
	if worst > 2 {
		t.Errorf("rotated disc differs from the disc by %d levels, want at most 2", worst)
	}
}

func TestTile_Values(t *testing.T) {
	src := newSurface(t, Coverage, 4, 1)
	copy(src.Pix, []uint8{1, 2, 3, 4})
	dst := newSurface(t, Coverage, 6, 1)
	if err := Tile(context.Background(), dst, src, 1, 0, 3, 1); err != nil {
		t.Fatal(err)
	}
	if want := []uint8{3, 2, 3, 2, 3, 2}; !slices.Equal(dst.Pix, want) {
		t.Errorf("tile = %v, want %v", dst.Pix, want)
	}
}

func TestAlphaRamp_Values(t *testing.T) {
	s := newSurface(t, Coverage, 5, 1)
	copy(s.Pix, []uint8{200, 200, 200, 200, 200})
	// From full at x = 1 to none at x = 4: centres 0.5 to 4.5.
	if err := AlphaRamp(context.Background(), s, 1, 0, 4, 0, 1, 0); err != nil {
		t.Fatal(err)
	}
	if want := []uint8{200, 167, 100, 33, 0}; !slices.Equal(s.Pix, want) {
		t.Errorf("ramp = %v, want %v", s.Pix, want)
	}
	c := newSurface(t, RGBA, 1, 2)
	copy(c.Pix, []uint8{100, 50, 0, 200, 100, 50, 0, 200})
	if err := AlphaRamp(context.Background(), c, 0, 0, 0, 2, 0.5, 0.5); err != nil {
		t.Fatal(err)
	}
	if want := []uint8{50, 25, 0, 100, 50, 25, 0, 100}; !slices.Equal(c.Pix, want) {
		t.Errorf("flat ramp on colour = %v, want %v", c.Pix, want)
	}
}
