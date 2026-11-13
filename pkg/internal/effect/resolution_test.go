package effect

import (
	"context"
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// blurError draws a w by h point rectangle blurred by a Gaussian of
// sigma points at k pixels a point, offset by off pixels, enlarges it
// bilinearly onto a grid of 12 pixels a point and returns the largest
// and the root mean square difference, in levels, with the rectangle's
// exact blur there: the product of two differences of the normal
// distribution.
func blurError(t *testing.T, sigma, k, off, w, h float64) (float64, float64) {
	t.Helper()
	m := 3*sigma + 2
	x0, y0 := m+off/k, m+0.7*off/k
	cw, ch := int(math.Ceil((w+2*m+1)*k)), int(math.Ceil((h+2*m+1)*k))
	s := filled(t, cw, ch, typeset.Matrix{A: k, D: k}, Path{Ops: rect(x0, y0, x0+w, y0+h)})
	defer s.Release()
	if err := Blur(context.Background(), s, GaussianKernel(sigma*k), GaussianKernel(sigma*k), EdgeNone); err != nil {
		t.Fatal(err)
	}
	phi := func(x float64) float64 { return 0.5 * (1 + math.Erf(x/math.Sqrt2)) }
	at := func(x, y int) float64 {
		return float64(s.Pix[min(max(y, 0), ch-1)*cw+min(max(x, 0), cw-1)])
	}
	const fine = 12.0
	worst, sum, n := 0.0, 0.0, 0
	for fy := 0.5 / fine; fy < h+2*m; fy += 1 / fine {
		for fx := 0.5 / fine; fx < w+2*m; fx += 1 / fine {
			px, py := fx*k-0.5, fy*k-0.5
			ix, iy := int(math.Floor(px)), int(math.Floor(py))
			ax, ay := px-float64(ix), py-float64(iy)
			v := (1-ay)*((1-ax)*at(ix, iy)+ax*at(ix+1, iy)) + ay*((1-ax)*at(ix, iy+1)+ax*at(ix+1, iy+1))
			ref := 255 * (phi((fx-x0)/sigma) - phi((fx-x0-w)/sigma)) * (phi((fy-y0)/sigma) - phi((fy-y0-h)/sigma))
			d := math.Abs(v - ref)
			worst = max(worst, d)
			sum += d * d
			n++
		}
	}
	return worst, math.Sqrt(sum / float64(n))
}

// TestPlan_RuleError draws blurred rectangles at the scale Plan chooses
// and checks them against their exact blur: within 1.5 levels at worst
// and 0.3 root mean square (the byte's rounding, the kernel cut at three
// deviations and the bilinear enlargement together), for every
// deviation the rule does not cap.
func TestPlan_RuleError(t *testing.T) {
	for _, tc := range []struct{ sigma, w, h float64 }{
		{1.2, 30, 20}, {2, 30, 20}, {6, 30, 20}, {16, 30, 20}, {3, 6, 6},
	} {
		r, ok := Plan(tc.sigma, tc.w, tc.h)
		if !ok || r.Reduced {
			t.Fatalf("Plan(%v) = %+v, %v", tc.sigma, r, ok)
		}
		for _, off := range []float64{0, 0.29} {
			worst, rms := blurError(t, tc.sigma, r.Scale, off, tc.w, tc.h)
			if worst > 1.5 || rms > 0.3 {
				t.Errorf("sigma %v pt at %.3f px/pt, offset %v: error %.2f levels, rms %.2f, want at most 1.5 and 0.3", tc.sigma, r.Scale, off, worst, rms)
			}
		}
	}
}

// TestPlan_ErrorFallsWithResolution checks the measurement the rule
// rests on: under 5 pixels a deviation the error grows past what the
// rule allows.
func TestPlan_ErrorFallsWithResolution(t *testing.T) {
	const sigma = 2.0
	prev := math.Inf(1)
	for _, perSigma := range []float64{1, 2, 3, 5} {
		worst, _ := blurError(t, sigma, perSigma/sigma, 0.13, 30, 20)
		if worst >= prev {
			t.Errorf("at %v pixels a deviation the error is %.2f, not under the coarser grid's %.2f", perSigma, worst, prev)
		}
		prev = worst
		if perSigma < 5 && worst <= 1.5 {
			t.Errorf("at %v pixels a deviation the error is %.2f, within the rule's bound", perSigma, worst)
		}
	}
}

func TestPlan_Bounds(t *testing.T) {
	for _, tc := range []struct {
		name         string
		sigma, w, h  float64
		scale        float64
		pw, ph       int
		reduced, bad bool
	}{
		{"rule", 2, 100, 50, 2.5, 250, 125, false, false},
		{"cap", 0.5, 10, 10, MaxScale, 42, 42, false, false},
		{"no blur", 0, 10, 10, MaxScale, 42, 42, false, false},
		{"floor", 50, 100, 100, MinScale, 100, 100, false, false},
		{"reduced", 0, 2000, 2000, 2.048, 4096, 4096, true, false},
		{"long", 0, 20000, 1, 3.2768, 65536, 4, true, false},
		{"empty", 1, 0, 10, 0, 0, 0, false, true},
		{"not finite", 1, math.Inf(1), 10, 0, 0, 0, false, true},
	} {
		r, ok := Plan(tc.sigma, tc.w, tc.h)
		if ok == tc.bad {
			t.Errorf("%s: ok = %v", tc.name, ok)
			continue
		}
		if !ok {
			continue
		}
		if math.Abs(r.Scale-tc.scale) > 0.001 || r.W != tc.pw || r.H != tc.ph || r.Reduced != tc.reduced {
			t.Errorf("%s: Plan = %+v, want scale %v, %d by %d, reduced %v", tc.name, r, tc.scale, tc.pw, tc.ph, tc.reduced)
		}
		if !fits(r.W, r.H) {
			t.Errorf("%s: %d by %d does not fit", tc.name, r.W, r.H)
		}
	}
	var p Page
	big := Resolution{W: 4096, H: 4096}
	for i := range 4 {
		if !p.Take(big) {
			t.Fatalf("take %d refused", i)
		}
	}
	if p.Take(Resolution{W: 1, H: 1}) {
		t.Error("take past MaxPagePixels accepted")
	}
}

// TestPlan_LastBitsTakeTheSamePixels checks that sides differing in their
// last bits, as two machines compute one box, take the same pixels: 99.6
// points at 300 pixels an inch is 415 pixels, which a bit more must not
// round up to 416.
func TestPlan_LastBitsTakeTheSamePixels(t *testing.T) {
	for _, w := range []float64{99.6, math.Nextafter(99.6, 100), math.Nextafter(math.Nextafter(99.6, 100), 100), math.Nextafter(99.6, 99)} {
		if r, ok := Plan(1, w, 70.8); !ok || r.W != 415 {
			t.Errorf("Plan of a side of %v points = %d pixels, %v, want 415", w, r.W, ok)
		}
	}
}
