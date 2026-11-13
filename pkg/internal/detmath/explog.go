package detmath

import "math"

// expCoef holds the Taylor coefficients of math/exp_amd64.s: 1/2, 1,
// then 1/3! to 1/8!.
var expCoef = [...]float64{
	0.5,
	1.0,
	1.6666666666666666667e-1,
	4.1666666666666666667e-2,
	8.3333333333333333333e-3,
	1.3888888888888888889e-3,
	1.9841269841269841270e-4,
	2.4801587301587301587e-5,
}

// Exp returns e^x: +Inf above 709.78, 0 for -Inf and below the smallest
// subnormal, NaN for NaN.
//
// The algorithm is the one math/exp_amd64.s runs on a CPU without AVX:
// x = k ln 2 + r, e^(r/16) by its Taylor series to the eighth power, then
// squared four times as (e^t - 1)(e^t + 1).
func Exp(x float64) float64 {
	const (
		log2e    = 1.4426950408889634073599246810018920
		ln2U     = 0.69314718055966295651160180568695068359375
		ln2L     = 0.28235290563031577122588448175013436025525412068e-12
		overflow = 7.09782712893384e+02
	)
	switch {
	case math.IsNaN(x) || math.IsInf(x, 1):
		return x
	case math.IsInf(x, -1):
		return 0
	case x > overflow:
		return math.Inf(1)
	}
	k := roundInt32(float64(log2e * x))
	fk := float64(k)
	x -= float64(fk * ln2U)
	x -= float64(fk * ln2L)
	x = float64(x * 0.0625)
	p := expCoef[7]
	for _, c := range [...]float64{expCoef[6], expCoef[5], expCoef[4], expCoef[3], expCoef[2], expCoef[0], expCoef[1]} {
		p = float64(p*x) + c
	}
	x = float64(p * x)
	for range 4 {
		x = float64(x * (2 + x))
	}
	return scale(x+1, k)
}

// roundInt32 converts t to an int32 as the CVTSD2SL instruction does under
// the default rounding mode: to the nearest, ties to even, and math.MinInt32
// when t is NaN or out of range.
func roundInt32(t float64) int32 {
	if math.IsNaN(t) || t >= 2147483647.5 || t < -2147483648.5 {
		return math.MinInt32
	}
	return int32(math.RoundToEven(t)) // fits int32: checked above
}

// scale returns x * 2^k as math/exp_amd64.s does: through the subnormal
// range in two products, 0 below it, +Inf above the largest exponent.
func scale(x float64, k int32) float64 {
	b := int64(k) + 0x3FF
	if b <= 0 {
		if b < -52 {
			return 0
		}
		x *= math.Float64frombits(uint64(b+0x3FE) << 52) //nolint:gosec // b+0x3FE is in [0x3FE-52, 0x3FE]
		return x * math.Float64frombits(1<<52)
	}
	if b >= 0x7FF {
		return math.Inf(1)
	}
	return x * math.Float64frombits(uint64(b)<<52) //nolint:gosec // b is in [1, 0x7FE]
}

// Log returns the natural logarithm of x: -Inf for ±0, NaN below 0 and
// for NaN, +Inf for +Inf.
//
// The algorithm is FreeBSD's, as math/log.go and math/log_amd64.s run it.
func Log(x float64) float64 {
	const (
		ln2Hi = 6.93147180369123816490e-01
		ln2Lo = 1.90821492927058770002e-10
		l1    = 6.666666666666735130e-01
		l2    = 3.999999999940941908e-01
		l3    = 2.857142874366239149e-01
		l4    = 2.222219843214978396e-01
		l5    = 1.818357216161805012e-01
		l6    = 1.531383769920937332e-01
		l7    = 1.479819860511658591e-01
	)
	switch {
	case math.IsNaN(x) || math.IsInf(x, 1):
		return x
	case x < 0:
		return math.NaN()
	case x == 0:
		return math.Inf(-1)
	}
	f1, ki := math.Frexp(x)
	if f1 < math.Sqrt2/2 {
		f1 *= 2
		ki--
	}
	f := f1 - 1
	k := float64(ki)
	s := f / (2 + f)
	s2 := float64(s * s)
	s4 := float64(s2 * s2)
	t1 := float64(s2 * float64(l1+float64(s4*float64(l3+float64(s4*float64(l5+float64(s4*l7)))))))
	t2 := float64(s4 * float64(l2+float64(s4*float64(l4+float64(s4*l6)))))
	r := t1 + t2
	hfsq := float64(float64(0.5*f) * f)
	return float64(k*ln2Hi) - float64(float64(hfsq-float64(float64(s*float64(hfsq+r))+float64(k*ln2Lo)))-f)
}

// Log10 returns the decimal logarithm of x, with Log's special cases.
func Log10(x float64) float64 {
	return float64(Log(x) * (1 / math.Ln10))
}

// Pow returns x^y with math.Pow's special cases.
func Pow(x, y float64) float64 {
	switch {
	case y == 0 || x == 1:
		return 1
	case y == 1:
		return x
	case math.IsNaN(x) || math.IsNaN(y):
		return math.NaN()
	case x == 0:
		switch {
		case y < 0:
			if math.Signbit(x) && isOddInt(y) {
				return math.Inf(-1)
			}
			return math.Inf(1)
		case y > 0:
			if math.Signbit(x) && isOddInt(y) {
				return x
			}
			return 0
		}
	case math.IsInf(y, 0):
		switch {
		case x == -1:
			return 1
		case (math.Abs(x) < 1) == math.IsInf(y, 1):
			return 0
		default:
			return math.Inf(1)
		}
	case math.IsInf(x, 0):
		if math.IsInf(x, -1) {
			return Pow(1/x, -y)
		}
		switch {
		case y < 0:
			return 0
		case y > 0:
			return math.Inf(1)
		}
	case y == 0.5:
		return math.Sqrt(x)
	case y == -0.5:
		return 1 / math.Sqrt(x)
	}
	yi, yf := math.Modf(math.Abs(y))
	if yf != 0 && x < 0 {
		return math.NaN()
	}
	if yi >= 1<<63 {
		switch {
		case x == -1:
			return 1
		case (math.Abs(x) < 1) == (y > 0):
			return 0
		default:
			return math.Inf(1)
		}
	}
	a1 := 1.0
	ae := 0
	if yf != 0 {
		if yf > 0.5 {
			yf--
			yi++
		}
		a1 = Exp(yf * Log(x))
	}
	// The integer part by repeated squaring, the exponent kept apart so
	// that a1 does not overflow before the end.
	x1, xe := math.Frexp(x)
	for i := int64(yi); i != 0; i >>= 1 {
		if xe < -1<<12 || 1<<12 < xe {
			ae += xe
			break
		}
		if i&1 == 1 {
			a1 *= x1
			ae += xe
		}
		x1 = float64(x1 * x1)
		xe <<= 1
		if x1 < .5 {
			x1 += x1
			xe--
		}
	}
	if y < 0 {
		a1 = 1 / a1
		ae = -ae
	}
	return math.Ldexp(a1, ae)
}

// isOddInt reports whether x is an odd integer.
func isOddInt(x float64) bool {
	if math.Abs(x) >= (1 << 53) {
		return false
	}
	xi, xf := math.Modf(x)
	return xf == 0 && int64(xi)&1 == 1
}

// Cbrt returns the cube root of x, ±0, ±Inf and NaN unchanged.
//
// The algorithm is FreeBSD's, as math/cbrt.go runs it.
func Cbrt(x float64) float64 {
	const (
		b1             = 715094163
		b2             = 696219795
		c              = 5.42857142857142815906e-01
		d              = -7.05306122448979611050e-01
		e              = 1.41428571428571436819e+00
		f              = 1.60714285714285720630e+00
		g              = 3.57142857142857150787e-01
		smallestNormal = 2.22507385850720138309e-308
	)
	if x == 0 || math.IsNaN(x) || math.IsInf(x, 0) {
		return x
	}
	sign := false
	if x < 0 {
		x = -x
		sign = true
	}
	t := math.Float64frombits(math.Float64bits(x)/3 + b1<<32)
	if x < smallestNormal {
		t = float64(1 << 54)
		t *= x
		t = math.Float64frombits(math.Float64bits(t)/3 + b2<<32)
	}
	r := t * t / x
	s := c + float64(r*t)
	t *= g + f/(s+e+d/s)
	t = math.Float64frombits(math.Float64bits(t)&(0xFFFFFFFFC<<28) + 1<<30)
	s = t * t
	r = x / s
	w := t + t
	r = (r - t) / (w + r)
	t += float64(t * r)
	if sign {
		t = -t
	}
	return t
}
