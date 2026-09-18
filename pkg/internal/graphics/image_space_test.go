package graphics

import (
	"math"
	"testing"
)

const imageSpaceTolerance = 1e-9

func TestImageSpace_AxisAlignedPlacement(t *testing.T) {
	t.Parallel()

	// 100 0 0 50 10 20 cm: the unit square lands on (10,20)-(110,70).
	box := ImageBox{Name: "Im1", MinX: 10, MinY: 20, MaxX: 110, MaxY: 70,
		CTM: [6]float64{100, 0, 0, 50, 10, 20}}

	cases := []struct {
		name string
		x, y float64
		u, v float64
	}{
		{"bottom-left corner", 10, 20, 0, 0},
		{"top-right corner", 110, 70, 1, 1},
		{"centre", 60, 45, 0.5, 0.5},
		{"outside, left of the image", 0, 45, -0.1, 0.5},
	}
	for _, tc := range cases {
		u, v, ok := box.ImageSpace(tc.x, tc.y)
		if !ok {
			t.Errorf("%s: ImageSpace reported the CTM uninvertible", tc.name)
			continue
		}
		if math.Abs(u-tc.u) > imageSpaceTolerance || math.Abs(v-tc.v) > imageSpaceTolerance {
			t.Errorf("%s: got (%g,%g), want (%g,%g)", tc.name, u, v, tc.u, tc.v)
		}
	}
}

func TestImageSpace_RotatedPlacementTheBoxCannotDescribe(t *testing.T) {
	t.Parallel()

	// 0 40 -30 0 100 200 cm rotates the image a quarter turn:
	//   x' = -30v + 100,  y' = 40u + 200
	// so the unit corners land at (100,200) (100,240) (70,200) (70,240).
	// The axis-aligned box is x 70..100, y 200..240 for every one of them,
	// which is exactly why blanking pixels needs the matrix and not the box.
	box := ImageBox{Name: "Im1", MinX: 70, MinY: 200, MaxX: 100, MaxY: 240,
		CTM: [6]float64{0, 40, -30, 0, 100, 200}}

	cases := []struct {
		name string
		x, y float64
		u, v float64
	}{
		{"image origin", 100, 200, 0, 0},
		{"u edge", 100, 240, 1, 0},
		{"v edge", 70, 200, 0, 1},
		{"far corner", 70, 240, 1, 1},
	}
	for _, tc := range cases {
		u, v, ok := box.ImageSpace(tc.x, tc.y)
		if !ok {
			t.Errorf("%s: ImageSpace reported the CTM uninvertible", tc.name)
			continue
		}
		if math.Abs(u-tc.u) > imageSpaceTolerance || math.Abs(v-tc.v) > imageSpaceTolerance {
			t.Errorf("%s: got (%g,%g), want (%g,%g)", tc.name, u, v, tc.u, tc.v)
		}
	}
}

func TestImageSpace_RoundTripsThroughTheCTM(t *testing.T) {
	t.Parallel()

	// A skewed placement, so neither axis is independent of the other.
	ctm := [6]float64{120, 30, -20, 60, 15, 25}
	box := ImageBox{Name: "Im1", CTM: ctm}

	for _, corner := range [][2]float64{{0, 0}, {1, 0}, {0, 1}, {1, 1}, {0.25, 0.75}} {
		u0, v0 := corner[0], corner[1]
		// Forward: the unit square through the CTM, per ISO 32000-1, 8.9.5.2.
		x := ctm[0]*u0 + ctm[2]*v0 + ctm[4]
		y := ctm[1]*u0 + ctm[3]*v0 + ctm[5]

		u, v, ok := box.ImageSpace(x, y)
		if !ok {
			t.Fatalf("(%g,%g): ImageSpace reported the CTM uninvertible", u0, v0)
		}
		if math.Abs(u-u0) > imageSpaceTolerance || math.Abs(v-v0) > imageSpaceTolerance {
			t.Errorf("(%g,%g) round-tripped to (%g,%g)", u0, v0, u, v)
		}
	}
}

func TestImageSpace_DegeneratePlacementIsReported(t *testing.T) {
	t.Parallel()

	// A placement that collapses the unit square onto a line or a point has no
	// inverse: there is no single pixel a page point came from. A caller that
	// must account for what it covers has to refuse, not guess.
	cases := []struct {
		name string
		ctm  [6]float64
	}{
		{"zero matrix", [6]float64{0, 0, 0, 0, 10, 20}},
		{"zero height", [6]float64{100, 0, 0, 0, 10, 20}},
		{"zero width", [6]float64{0, 0, 0, 50, 10, 20}},
		{"collinear axes", [6]float64{10, 20, 20, 40, 0, 0}},
	}
	for _, tc := range cases {
		box := ImageBox{Name: "Im1", CTM: tc.ctm}
		if _, _, ok := box.ImageSpace(50, 50); ok {
			t.Errorf("%s: ImageSpace accepted an uninvertible CTM", tc.name)
		}
	}
}
