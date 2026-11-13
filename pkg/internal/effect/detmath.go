package effect

import "math"

// The functions of this file compute the few transcendental values the
// engine needs (kernel weights, gamma tables, hue rotations) with results
// that do not depend on the machine. IEEE 754 makes addition,
// subtraction, multiplication, division and square root correctly
// rounded, so they agree on every architecture; Go may fuse a product
// with the addition that follows it into one rounding (The Go
// Programming Language Specification, "Arithmetic operators"), which
// arm64 and amd64 with FMA do and other targets do not. Every product
// here is wrapped in an explicit float64 conversion, which the
// specification says rounds and so blocks the fusion. The math package's
// Exp and Log are not used: amd64 and arm64 run them in assembly of their
// own. math.Floor, math.Round, math.Ldexp, math.Frexp, math.Mod and
// math.Sqrt are exact or correctly rounded everywhere.

const (
	ln2Hi  = 6.93147180369123816490e-01 // the high bits of ln 2, exact in 33 bits
	ln2Lo  = 1.90821492927058770002e-10 // ln 2 - ln2Hi
	log2e  = 1.44269504088896338700e+00
	sqrt12 = 0.70710678118654752440
)

// detExp returns e^x, 0 below -708 and +Inf above 709.
func detExp(x float64) float64 {
	switch {
	case math.IsNaN(x):
		return x
	case x < -708:
		return 0
	case x > 709:
		return math.Inf(1)
	}
	k := math.Floor(float64(x*log2e) + 0.5)
	r := float64(x-float64(k*ln2Hi)) - float64(k*ln2Lo)
	// Taylor series of e^r on |r| <= ln 2 / 2, Horner from the 14th term:
	// the first left out is under 2^-60.
	p := 1.0 / 87178291200
	for _, c := range [...]float64{
		1.0 / 6227020800, 1.0 / 479001600, 1.0 / 39916800, 1.0 / 3628800,
		1.0 / 362880, 1.0 / 40320, 1.0 / 5040, 1.0 / 720, 1.0 / 120,
		1.0 / 24, 1.0 / 6, 1.0 / 2, 1, 1,
	} {
		p = float64(p*r) + c
	}
	return math.Ldexp(p, int(k)) // fits int: |k| <= 1023, x bounded above
}

// detLog returns the natural logarithm of x > 0; -Inf for 0, NaN below.
func detLog(x float64) float64 {
	switch {
	case math.IsNaN(x) || x < 0:
		return math.NaN()
	case x == 0:
		return math.Inf(-1)
	case math.IsInf(x, 1):
		return x
	}
	m, e := math.Frexp(x)
	if m < sqrt12 {
		m *= 2
		e--
	}
	// log m = 2 atanh(s), s = (m-1)/(m+1), |s| <= 0.172: the series to
	// s^23 leaves less than 2^-60.
	s := (m - 1) / (m + 1)
	s2 := float64(s * s)
	p := 1.0 / 23
	for _, c := range [...]float64{
		1.0 / 21, 1.0 / 19, 1.0 / 17, 1.0 / 15, 1.0 / 13, 1.0 / 11,
		1.0 / 9, 1.0 / 7, 1.0 / 5, 1.0 / 3, 1,
	} {
		p = float64(p*s2) + c
	}
	fe := float64(e)
	return float64(fe*ln2Hi) + (float64(fe*ln2Lo) + float64(2*float64(s*p)))
}

// detPow returns x^y for x >= 0, the cases of zero and infinity as
// math.Pow has them for a non-negative base.
func detPow(x, y float64) float64 {
	switch {
	case y == 0:
		return 1
	case math.IsNaN(x) || math.IsNaN(y) || x < 0:
		return math.NaN()
	case x == 0:
		if y > 0 {
			return 0
		}
		return math.Inf(1)
	}
	return detExp(float64(y * detLog(x)))
}

// Sincos returns the sine and cosine of deg degrees, the same bits on
// every machine, which math.Sincos does not promise: a multiple of 90
// degrees exactly, any other angle within 4e-15 of math.Sincos. It
// returns NaN for both when deg is not finite.
func Sincos(deg float64) (float64, float64) {
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
