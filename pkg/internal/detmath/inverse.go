package detmath

import "math"

// xatan returns the arctangent of x in [0, 0.66] (math/atan.go, Cephes).
func xatan(x float64) float64 {
	const (
		p0 = -8.750608600031904122785e-01
		p1 = -1.615753718733365076637e+01
		p2 = -7.500855792314704667340e+01
		p3 = -1.228866684490136173410e+02
		p4 = -6.485021904942025371773e+01
		q0 = +2.485846490142306297962e+01
		q1 = +1.650270098316988542046e+02
		q2 = +4.328810604912902668951e+02
		q3 = +4.853903996359136964868e+02
		q4 = +1.945506571482613964425e+02
	)
	z := float64(x * x)
	num := float64(float64(float64(float64(float64(float64(float64(p0*z)+p1)*z)+p2)*z)+p3)*z) + p4
	den := float64(float64(float64(float64(float64(float64(float64(float64(z+q0)*z)+q1)*z)+q2)*z)+q3)*z) + q4
	z = float64(z*num) / den
	return float64(x*z) + x
}

// satan reduces x >= 0 to [0, 0.66] and returns its arctangent.
func satan(x float64) float64 {
	const (
		morebits = 6.123233995736765886130e-17 // Pi/2 = PIO2 + morebits
		tan3pio8 = 2.41421356237309504880      // tan(3*Pi/8)
	)
	if x <= 0.66 {
		return xatan(x)
	}
	if x > tan3pio8 {
		return math.Pi/2 - xatan(1/x) + morebits
	}
	return math.Pi/4 + xatan((x-1)/(x+1)) + 0.5*morebits
}

// Atan returns the arctangent of x in radians, ±0 for ±0.
func Atan(x float64) float64 {
	if x == 0 {
		return x
	}
	if x > 0 {
		return satan(x)
	}
	return -satan(-x)
}

// Asin returns the arcsine of x in radians: ±0 for ±0, NaN when |x| > 1.
func Asin(x float64) float64 {
	if x == 0 {
		return x
	}
	sign := false
	if x < 0 {
		x = -x
		sign = true
	}
	if x > 1 {
		return math.NaN()
	}
	temp := math.Sqrt(1 - float64(x*x))
	if x > 0.7 {
		temp = math.Pi/2 - satan(temp/x)
	} else {
		temp = satan(x / temp)
	}
	if sign {
		temp = -temp
	}
	return temp
}

// Acos returns the arccosine of x in radians: NaN when |x| > 1.
func Acos(x float64) float64 {
	return math.Pi/2 - Asin(x)
}

// Atan2 returns the arctangent of y/x in radians, in the quadrant the
// signs of both give, with math.Atan2's special cases.
func Atan2(y, x float64) float64 {
	switch {
	case math.IsNaN(y) || math.IsNaN(x):
		return math.NaN()
	case y == 0:
		if x >= 0 && !math.Signbit(x) {
			return math.Copysign(0, y)
		}
		return math.Copysign(math.Pi, y)
	case x == 0:
		return math.Copysign(math.Pi/2, y)
	case math.IsInf(x, 0):
		if math.IsInf(x, 1) {
			if math.IsInf(y, 0) {
				return math.Copysign(math.Pi/4, y)
			}
			return math.Copysign(0, y)
		}
		if math.IsInf(y, 0) {
			return math.Copysign(3*math.Pi/4, y)
		}
		return math.Copysign(math.Pi, y)
	case math.IsInf(y, 0):
		return math.Copysign(math.Pi/2, y)
	}
	q := Atan(y / x)
	if x < 0 {
		if q <= 0 {
			return q + math.Pi
		}
		return q - math.Pi
	}
	return q
}

// Hypot returns Sqrt(p*p + q*q), overflowing only when the result does:
// +Inf when either is infinite, NaN when either is NaN.
func Hypot(p, q float64) float64 {
	p, q = math.Abs(p), math.Abs(q)
	switch {
	case math.IsInf(p, 1) || math.IsInf(q, 1):
		return math.Inf(1)
	case math.IsNaN(p) || math.IsNaN(q):
		return math.NaN()
	}
	if p < q {
		p, q = q, p
	}
	if p == 0 {
		return 0
	}
	q /= p
	return float64(p * math.Sqrt(1+float64(q*q)))
}
