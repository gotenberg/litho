package detmath

import (
	"math"
	"math/bits"
)

// The coefficients and the reduction are those of the Go math package
// (sin.go, sincos.go, tan.go, trig_reduce.go), from the Cephes library.

var sinCoef = [...]float64{
	1.58962301576546568060e-10,
	-2.50507477628578072866e-8,
	2.75573136213857245213e-6,
	-1.98412698295895385996e-4,
	8.33333333332211858878e-3,
	-1.66666666666666307295e-1,
}

var cosCoef = [...]float64{
	-1.13585365213876817300e-11,
	2.08757008419747316778e-9,
	-2.75573141792967388112e-7,
	2.48015872888517045348e-5,
	-1.38888888888730564116e-3,
	4.16666666666665929218e-2,
}

var tanP = [...]float64{
	-1.30936939181383777646e4,
	1.15351664838587416140e6,
	-1.79565251976484877988e7,
}

var tanQ = [...]float64{
	1.00000000000000000000e0,
	1.36812963470692954678e4,
	-1.32089234440210967447e6,
	2.50083801823357915839e7,
	-5.38695755929454629881e7,
}

// pi4A, pi4B and pi4C split Pi/4 into three parts for the reduction.
const (
	pi4A = 7.85398125648498535156e-1
	pi4B = 3.77489470793079817668e-8
	pi4C = 2.69515142907905952645e-15
)

// reduceThreshold is the argument from which the reduction by Payne and
// Hanek replaces the three-part one, which loses precision past it.
const reduceThreshold = 1 << 29

// reduce returns the octant j of x >= 0 and the remainder z in it.
func reduce(x float64) (uint64, float64) {
	if x >= reduceThreshold {
		return trigReduce(x)
	}
	j := uint64(x * (4 / math.Pi)) // the octant's integer part: x < 2^29
	y := float64(j)
	if j&1 == 1 {
		j++
		y++
	}
	return j, float64(float64(x-float64(y*pi4A))-float64(y*pi4B)) - float64(y*pi4C)
}

// sinPoly returns the sine of z in [-Pi/4, Pi/4], zz its square.
func sinPoly(z, zz float64) float64 {
	p := float64(float64(float64(float64(float64(float64(sinCoef[0]*zz)+sinCoef[1])*zz)+sinCoef[2])*zz) + sinCoef[3])
	p = float64(float64(float64(p*zz)+sinCoef[4])*zz) + sinCoef[5]
	return z + float64(float64(z*zz)*p)
}

// cosPoly returns the cosine of z in [-Pi/4, Pi/4], zz its square.
func cosPoly(zz float64) float64 {
	p := float64(float64(float64(float64(float64(float64(cosCoef[0]*zz)+cosCoef[1])*zz)+cosCoef[2])*zz) + cosCoef[3])
	p = float64(float64(float64(p*zz)+cosCoef[4])*zz) + cosCoef[5]
	return float64(1.0-float64(0.5*zz)) + float64(float64(zz*zz)*p)
}

// Sin returns the sine of x radians: NaN for an infinite or NaN x, ±0 for
// ±0.
func Sin(x float64) float64 {
	switch {
	case x == 0 || math.IsNaN(x):
		return x
	case math.IsInf(x, 0):
		return math.NaN()
	}
	sign := false
	if x < 0 {
		x = -x
		sign = true
	}
	j, z := reduce(x)
	j &= 7
	if j > 3 {
		sign = !sign
		j -= 4
	}
	zz := float64(z * z)
	var y float64
	if j == 1 || j == 2 {
		y = cosPoly(zz)
	} else {
		y = sinPoly(z, zz)
	}
	if sign {
		y = -y
	}
	return y
}

// Cos returns the cosine of x radians: NaN for an infinite or NaN x.
func Cos(x float64) float64 {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return math.NaN()
	}
	sign := false
	x = math.Abs(x)
	j, z := reduce(x)
	j &= 7
	if j > 3 {
		j -= 4
		sign = !sign
	}
	if j > 1 {
		sign = !sign
	}
	zz := float64(z * z)
	var y float64
	if j == 1 || j == 2 {
		y = sinPoly(z, zz)
	} else {
		y = cosPoly(zz)
	}
	if sign {
		y = -y
	}
	return y
}

// Sincos returns Sin(x) and Cos(x), the bits the two functions return.
func Sincos(x float64) (sin, cos float64) {
	switch {
	case x == 0:
		return x, 1
	case math.IsNaN(x) || math.IsInf(x, 0):
		return math.NaN(), math.NaN()
	}
	sinSign, cosSign := false, false
	if x < 0 {
		x = -x
		sinSign = true
	}
	j, z := reduce(x)
	j &= 7
	if j > 3 {
		j -= 4
		sinSign, cosSign = !sinSign, !cosSign
	}
	if j > 1 {
		cosSign = !cosSign
	}
	zz := float64(z * z)
	cos = cosPoly(zz)
	sin = sinPoly(z, zz)
	if j == 1 || j == 2 {
		sin, cos = cos, sin
	}
	if cosSign {
		cos = -cos
	}
	if sinSign {
		sin = -sin
	}
	return sin, cos
}

// Tan returns the tangent of x radians: NaN for an infinite or NaN x, ±0
// for ±0.
func Tan(x float64) float64 {
	switch {
	case x == 0 || math.IsNaN(x):
		return x
	case math.IsInf(x, 0):
		return math.NaN()
	}
	sign := false
	if x < 0 {
		x = -x
		sign = true
	}
	j, z := reduce(x)
	zz := float64(z * z)
	y := z
	if zz > 1e-14 {
		num := float64(zz * float64(float64(float64(float64(tanP[0]*zz)+tanP[1])*zz)+tanP[2]))
		den := float64(float64(float64(float64(float64(float64(zz+tanQ[1])*zz)+tanQ[2])*zz)+tanQ[3])*zz) + tanQ[4]
		y = z + float64(z*(num/den))
	}
	if j&2 == 2 {
		y = -1 / y
	}
	if sign {
		y = -y
	}
	return y
}

// mPi4 is the binary digits of 4/Pi as a 1280-bit integer, for the
// reduction of large arguments (math/trig_reduce.go).
var mPi4 = [...]uint64{
	0x0000000000000001,
	0x45f306dc9c882a53,
	0xf84eafa3ea69bb81,
	0xb6c52b3278872083,
	0xfca2c757bd778ac3,
	0x6e48dc74849ba5c0,
	0x0c925dd413a32439,
	0xfc3bd63962534e7d,
	0xd1046bea5d768909,
	0xd338e04d68befc82,
	0x7323ac7306a673e9,
	0x3908bf177bf25076,
	0x3ff12fffbc0b301f,
	0xde5e2316b414da3e,
	0xda6cfd9e4f96136e,
	0x9e8c7ecd3cbfd45a,
	0xea4f758fd7cbe2f6,
	0x7a0e73ef14a525d4,
	0xd7f6bf623f1aba10,
	0xac06608df8f6d757,
}

// trigReduce reduces x >= reduceThreshold by Payne and Hanek's method in
// integers: the octant j and the remainder z in [-Pi/4, Pi/4].
func trigReduce(x float64) (j uint64, z float64) {
	const (
		shift = 52
		mask  = 0x7FF
		bias  = 1023
	)
	ix := math.Float64bits(x)
	exp := int(ix>>shift&mask) - bias - shift // fits int: an 11-bit exponent
	ix &^= mask << shift
	ix |= 1 << shift
	digit, bitshift := uint(exp+61)/64, uint(exp+61)%64 // exp+61 > 0: x >= 2^29
	z0 := (mPi4[digit] << bitshift) | (mPi4[digit+1] >> (64 - bitshift))
	z1 := (mPi4[digit+1] << bitshift) | (mPi4[digit+2] >> (64 - bitshift))
	z2 := (mPi4[digit+2] << bitshift) | (mPi4[digit+3] >> (64 - bitshift))
	z2hi, _ := bits.Mul64(z2, ix)
	z1hi, z1lo := bits.Mul64(z1, ix)
	z0lo := z0 * ix
	lo, c := bits.Add64(z1lo, z2hi, 0)
	hi, _ := bits.Add64(z0lo, z1hi, c)
	j = hi >> 61
	hi = hi<<3 | lo>>61
	lz := uint(bits.LeadingZeros64(hi))
	e := uint64(bias - (lz + 1))
	hi = (hi << (lz + 1)) | (lo >> (64 - (lz + 1)))
	hi >>= 64 - shift
	hi |= e << shift
	z = math.Float64frombits(hi)
	if j&1 == 1 {
		j++
		j &= 7
		z--
	}
	return j, float64(z * (math.Pi / 4))
}
