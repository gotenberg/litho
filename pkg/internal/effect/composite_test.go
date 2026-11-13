package effect

import (
	"cmp"
	"context"
	"math"
	"slices"
	"testing"
)

// pixels returns n premultiplied RGBA pixels spread over the colour
// cube, alpha included, from a fixed sequence.
func pixels(n int) []uint8 {
	out := make([]uint8, 0, 4*n)
	x := uint32(12345)
	next := func() uint8 {
		x = x*1103515245 + 12345
		return uint8(x >> 23)
	}
	for range n {
		a := next()
		if a < 30 {
			a = []uint8{0, 255}[a%2]
		}
		out = append(out, premultiply(next(), a), premultiply(next(), a), premultiply(next(), a), a)
	}
	return out
}

func surfaceOf(t *testing.T, f Format, pix []uint8) *Surface {
	t.Helper()
	s := newSurface(t, f, len(pix)/int(f), 1)
	copy(s.Pix, pix)
	return s
}

// TestComposite_Formula checks every operator against the Porter-Duff
// formula co = ca Fa + cb Fb in floating point over 2,000 pixel pairs,
// within half a level.
func TestComposite_Formula(t *testing.T) {
	const n = 2000
	a, b := pixels(n), pixels(2 * n)[4*n:]
	for _, op := range []Operator{Over, In, Out, Atop, Xor} {
		dst := surfaceOf(t, RGBA, b)
		if err := Composite(context.Background(), dst, surfaceOf(t, RGBA, a), op); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < len(a); i += 4 {
			aa, ab := float64(a[i+3])/255, float64(b[i+3])/255
			fa, fb := map[Operator][2]float64{
				Over: {1, 1 - aa}, In: {ab, 0}, Out: {1 - ab, 0}, Atop: {ab, 1 - aa}, Xor: {1 - ab, 1 - aa},
			}[op][0], map[Operator][2]float64{
				Over: {1, 1 - aa}, In: {ab, 0}, Out: {1 - ab, 0}, Atop: {ab, 1 - aa}, Xor: {1 - ab, 1 - aa},
			}[op][1]
			for c := range 4 {
				want := float64(a[i+c])*fa + float64(b[i+c])*fb
				if d := math.Abs(float64(dst.Pix[i+c]) - want); d > 0.5 {
					t.Fatalf("op %d pixel %d channel %d = %d, want %.2f", op, i/4, c, dst.Pix[i+c], want)
				}
			}
		}
	}
	cov := surfaceOf(t, Coverage, []uint8{255, 128, 0})
	if err := Composite(context.Background(), cov, surfaceOf(t, Coverage, []uint8{128, 128, 128}), In); err != nil {
		t.Fatal(err)
	}
	if want := []uint8{128, 64, 0}; !slices.Equal(cov.Pix, want) {
		t.Errorf("coverage in = %v, want %v", cov.Pix, want)
	}
}

func TestArithmetic_Formula(t *testing.T) {
	a, b := pixels(500), pixels(1000)[2000:]
	k1, k2, k3, k4 := 0.5, 0.25, 0.75, -0.1
	dst := surfaceOf(t, RGBA, b)
	if err := Arithmetic(context.Background(), dst, surfaceOf(t, RGBA, a), k1, k2, k3, k4); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < len(a); i += 4 {
		var want [4]float64
		for c := range 4 {
			x, y := float64(a[i+c])/255, float64(b[i+c])/255
			want[c] = min(max(k1*x*y+k2*x+k3*y+k4, 0), 1) * 255
		}
		for c := range 3 {
			want[c] = min(want[c], math.Round(want[3]))
		}
		for c := range 4 {
			// Half a level, and the rounding of the coefficients to 2^-16.
			if d := math.Abs(float64(dst.Pix[i+c]) - want[c]); d > 0.51 {
				t.Fatalf("pixel %d channel %d = %d, want %.2f", i/4, c, dst.Pix[i+c], want[c])
			}
		}
	}
}

// TestBlend_Formula checks every blend mode against Compositing 1, 5, in
// floating point over 2,000 pixel pairs, within one level.
func TestBlend_Formula(t *testing.T) {
	const n = 2000
	src, bd := pixels(n), pixels(2 * n)[4*n:]
	for mode := Normal; mode <= Luminosity; mode++ {
		dst := surfaceOf(t, RGBA, bd)
		if err := Blend(context.Background(), dst, surfaceOf(t, RGBA, src), mode); err != nil {
			t.Fatal(err)
		}
		worst := 0.0
		for i := 0; i < len(src); i += 4 {
			want := blendFloat(mode, src[i:i+4], bd[i:i+4])
			for c := range 4 {
				worst = max(worst, math.Abs(float64(dst.Pix[i+c])-want[c]))
			}
		}
		if worst > 1 {
			t.Errorf("mode %d off the formula by %.2f levels, want at most 1", mode, worst)
		}
	}
}

// blendFloat returns the premultiplied result of blending s onto b by
// Compositing 1, 5, in floating point, in levels.
func blendFloat(mode BlendMode, s, b []uint8) [4]float64 {
	as, ab := float64(s[3])/255, float64(b[3])/255
	if as == 0 {
		return [4]float64{float64(b[0]), float64(b[1]), float64(b[2]), float64(b[3])}
	}
	var cs, cb [3]float64
	for c := range 3 {
		cs[c] = min(float64(s[c])/255/as, 1)
		if ab > 0 {
			cb[c] = min(float64(b[c])/255/ab, 1)
		}
	}
	lum := func(c [3]float64) float64 { return 0.3*c[0] + 0.59*c[1] + 0.11*c[2] }
	clip := func(c [3]float64) [3]float64 {
		l := lum(c)
		n, x := min(c[0], c[1], c[2]), max(c[0], c[1], c[2])
		for i := range c {
			if n < 0 {
				c[i] = l + (c[i]-l)*l/(l-n)
			}
		}
		for i := range c {
			if x > 1 {
				c[i] = l + (c[i]-l)*(1-l)/(x-l)
			}
		}
		return c
	}
	setLum := func(c [3]float64, l float64) [3]float64 {
		d := l - lum(c)
		return clip([3]float64{c[0] + d, c[1] + d, c[2] + d})
	}
	sat := func(c [3]float64) float64 { return max(c[0], c[1], c[2]) - min(c[0], c[1], c[2]) }
	setSat := func(c [3]float64, s float64) [3]float64 {
		idx := []int{0, 1, 2}
		slices.SortStableFunc(idx, func(i, j int) int { return cmp.Compare(c[i], c[j]) })
		var out [3]float64
		lo, mid, hi := idx[0], idx[1], idx[2]
		if c[hi] > c[lo] {
			out[mid] = (c[mid] - c[lo]) * s / (c[hi] - c[lo])
			out[hi] = s
		}
		return out
	}
	var sep func(m BlendMode, b, s float64) float64
	sep = func(m BlendMode, b, s float64) float64 {
		switch m {
		case Multiply:
			return b * s
		case Screen:
			return b + s - b*s
		case Overlay:
			return sep(HardLight, s, b)
		case Darken:
			return min(b, s)
		case Lighten:
			return max(b, s)
		case ColorDodge:
			if b == 0 {
				return 0
			}
			if s >= 1 {
				return 1
			}
			return min(1, b/(1-s))
		case ColorBurn:
			if b >= 1 {
				return 1
			}
			if s == 0 {
				return 0
			}
			return 1 - min(1, (1-b)/s)
		case HardLight:
			if s <= 0.5 {
				return b * 2 * s
			}
			return sep(Screen, b, 2*s-1)
		case SoftLight:
			if s <= 0.5 {
				return b - (1-2*s)*b*(1-b)
			}
			d := math.Sqrt(b)
			if b <= 0.25 {
				d = ((16*b-12)*b + 4) * b
			}
			return b + (2*s-1)*(d-b)
		case Difference:
			return math.Abs(b - s)
		case Exclusion:
			return b + s - 2*b*s
		case Normal, Hue, Saturation, ColorMode, Luminosity:
		}
		return s
	}
	var mix [3]float64
	switch mode {
	case Hue:
		mix = setLum(setSat(cs, sat(cb)), lum(cb))
	case Saturation:
		mix = setLum(setSat(cb, sat(cs)), lum(cb))
	case ColorMode:
		mix = setLum(cs, lum(cb))
	case Luminosity:
		mix = setLum(cb, lum(cs))
	default:
		for c := range 3 {
			mix[c] = sep(mode, cb[c], cs[c])
		}
	}
	var out [4]float64
	for c := range 3 {
		out[c] = 255 * min(max(as*cs[c]*(1-ab)+ab*cb[c]*(1-as)+as*ab*mix[c], 0), 1)
	}
	out[3] = 255 * (as + ab - as*ab)
	return out
}

func TestMerge_Order(t *testing.T) {
	dst := surfaceOf(t, Coverage, []uint8{0})
	if err := Merge(context.Background(), dst, surfaceOf(t, Coverage, []uint8{128}), surfaceOf(t, Coverage, []uint8{128})); err != nil {
		t.Fatal(err)
	}
	if dst.Pix[0] != 192 {
		t.Errorf("merge = %d, want 192", dst.Pix[0])
	}
}
