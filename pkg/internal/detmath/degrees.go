package detmath

import "math"

// SincosDegrees returns the sine and cosine of deg degrees: a multiple of
// 90 degrees exactly, any other angle within 4e-15 of math.Sincos of its
// radians. It returns NaN for both when deg is not finite.
func SincosDegrees(deg float64) (float64, float64) {
	if math.IsNaN(deg) || math.IsInf(deg, 0) {
		return math.NaN(), math.NaN()
	}
	d := math.Mod(deg, 360)
	if d < 0 {
		d += 360
	}
	// The octant's centre is a multiple of 90 degrees, exact; the rest
	// is within 45 degrees of it.
	q := math.Floor(d/90 + 0.5)
	r := float64((d - float64(q*90)) * (math.Pi / 180))
	r2 := float64(r * r)
	// Taylor series on |r| <= pi/4 to the 21st power: under 2^-60 left.
	sp := 1.0 / 51090942171709440000
	for _, c := range [...]float64{
		-1.0 / 121645100408832000, 1.0 / 355687428096000, -1.0 / 1307674368000,
		1.0 / 6227020800, -1.0 / 39916800, 1.0 / 362880, -1.0 / 5040, 1.0 / 120,
		-1.0 / 6, 1,
	} {
		sp = float64(sp*r2) + c
	}
	cp := 1.0 / 2432902008176640000
	for _, c := range [...]float64{
		-1.0 / 6402373705728000, 1.0 / 20922789888000, -1.0 / 87178291200,
		1.0 / 479001600, -1.0 / 3628800, 1.0 / 40320, -1.0 / 720, 1.0 / 24,
		-1.0 / 2, 1,
	} {
		cp = float64(cp*r2) + c
	}
	s, c := float64(sp*r), cp
	switch int(q) % 4 { // fits int: q is 0 to 4
	case 1:
		return c, -s
	case 2:
		return -s, -c
	case 3:
		return -c, s
	}
	return s, c
}
