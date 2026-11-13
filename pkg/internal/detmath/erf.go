package detmath

import "math"

// The coefficients of Erf are FreeBSD's, as math/erf.go holds them.
const (
	erx  = 8.45062911510467529297e-01
	efx  = 1.28379167095512586316e-01
	efx8 = 1.02703333676410069053e+00
	pp0  = 1.28379167095512558561e-01
	pp1  = -3.25042107247001499370e-01
	pp2  = -2.84817495755985104766e-02
	pp3  = -5.77027029648944159157e-03
	pp4  = -2.37630166566501626084e-05
	qq1  = 3.97917223959155352819e-01
	qq2  = 6.50222499887672944485e-02
	qq3  = 5.08130628187576562776e-03
	qq4  = 1.32494738004321644526e-04
	qq5  = -3.96022827877536812320e-06
	pa0  = -2.36211856075265944077e-03
	pa1  = 4.14856118683748331666e-01
	pa2  = -3.72207876035701323847e-01
	pa3  = 3.18346619901161753674e-01
	pa4  = -1.10894694282396677476e-01
	pa5  = 3.54783043256182359371e-02
	pa6  = -2.16637559486879084300e-03
	qa1  = 1.06420880400844228286e-01
	qa2  = 5.40397917702171048937e-01
	qa3  = 7.18286544141962662868e-02
	qa4  = 1.26171219808761642112e-01
	qa5  = 1.36370839120290507362e-02
	qa6  = 1.19844998467991074170e-02
	ra0  = -9.86494403484714822705e-03
	ra1  = -6.93858572707181764372e-01
	ra2  = -1.05586262253232909814e+01
	ra3  = -6.23753324503260060396e+01
	ra4  = -1.62396669462573470355e+02
	ra5  = -1.84605092906711035994e+02
	ra6  = -8.12874355063065934246e+01
	ra7  = -9.81432934416914548592e+00
	sa1  = 1.96512716674392571292e+01
	sa2  = 1.37657754143519042600e+02
	sa3  = 4.34565877475229228821e+02
	sa4  = 6.45387271733267880336e+02
	sa5  = 4.29008140027567833386e+02
	sa6  = 1.08635005541779435134e+02
	sa7  = 6.57024977031928170135e+00
	sa8  = -6.04244152148580987438e-02
	rb0  = -9.86494292470009928597e-03
	rb1  = -7.99283237680523006574e-01
	rb2  = -1.77579549177547519889e+01
	rb3  = -1.60636384855821916062e+02
	rb4  = -6.37566443368389627722e+02
	rb5  = -1.02509513161107724954e+03
	rb6  = -4.83519191608651397019e+02
	sb1  = 3.03380607434824582924e+01
	sb2  = 3.25792512996573918826e+02
	sb3  = 1.53672958608443695994e+03
	sb4  = 3.19985821950859553908e+03
	sb5  = 2.55305040643316442583e+03
	sb6  = 4.74528541206955367215e+02
	sb7  = -2.24409524465858183362e+01
)

// ma returns a*b + c, the product rounded before the sum.
func ma(a, b, c float64) float64 {
	return float64(a*b) + c
}

// Erf returns the error function of x: ±1 for ±Inf, NaN for NaN.
func Erf(x float64) float64 {
	const (
		veryTiny = 2.848094538889218e-306
		small    = 1.0 / (1 << 28)
	)
	switch {
	case math.IsNaN(x):
		return math.NaN()
	case math.IsInf(x, 1):
		return 1
	case math.IsInf(x, -1):
		return -1
	}
	sign := false
	if x < 0 {
		x = -x
		sign = true
	}
	if x < 0.84375 {
		var temp float64
		switch {
		case x < veryTiny:
			temp = 0.125 * (float64(8.0*x) + float64(efx8*x))
		case x < small:
			temp = x + float64(efx*x)
		default:
			z := float64(x * x)
			r := ma(z, ma(z, ma(z, ma(z, pp4, pp3), pp2), pp1), pp0)
			s := ma(z, ma(z, ma(z, ma(z, ma(z, qq5, qq4), qq3), qq2), qq1), 1)
			temp = ma(x, r/s, x)
		}
		if sign {
			return -temp
		}
		return temp
	}
	if x < 1.25 {
		s := x - 1
		p := ma(s, ma(s, ma(s, ma(s, ma(s, ma(s, pa6, pa5), pa4), pa3), pa2), pa1), pa0)
		q := ma(s, ma(s, ma(s, ma(s, ma(s, ma(s, qa6, qa5), qa4), qa3), qa2), qa1), 1)
		if sign {
			return -erx - p/q
		}
		return erx + p/q
	}
	if x >= 6 {
		if sign {
			return -1
		}
		return 1
	}
	s := 1 / (x * x)
	var r, q float64
	if x < 1/0.35 {
		r = ma(s, ma(s, ma(s, ma(s, ma(s, ma(s, ma(s, ra7, ra6), ra5), ra4), ra3), ra2), ra1), ra0)
		q = ma(s, ma(s, ma(s, ma(s, ma(s, ma(s, ma(s, ma(s, sa8, sa7), sa6), sa5), sa4), sa3), sa2), sa1), 1)
	} else {
		r = ma(s, ma(s, ma(s, ma(s, ma(s, ma(s, rb6, rb5), rb4), rb3), rb2), rb1), rb0)
		q = ma(s, ma(s, ma(s, ma(s, ma(s, ma(s, ma(s, sb7, sb6), sb5), sb4), sb3), sb2), sb1), 1)
	}
	z := math.Float64frombits(math.Float64bits(x) & 0xffffffff00000000)
	e := Exp(float64(-z*z)-0.5625) * Exp(float64((z-x)*(z+x))+r/q)
	if sign {
		return e/x - 1
	}
	return 1 - e/x
}
