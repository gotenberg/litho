package recolor

import "math"

// d50 is the PCS illuminant (ICC.1:2010, 7.2.16).
var d50 = [3]float64{0.9642, 1.0, 0.8249}

// bradford is the Bradford cone response matrix, the chromatic adaptation
// ICC.1:2010, Annex E recommends.
var bradford = [9]float64{
	0.8951, 0.2664, -0.1614,
	-0.7502, 1.7135, 0.0367,
	0.0389, -0.0685, 1.0296,
}

var bradfordInv = [9]float64{
	0.9869929, -0.1470543, 0.1599627,
	0.4323053, 0.5183603, 0.0492912,
	-0.0085287, 0.0400428, 0.9684867,
}

func mul3(m [9]float64, v [3]float64) [3]float64 {
	return [3]float64{
		m[0]*v[0] + m[1]*v[1] + m[2]*v[2],
		m[3]*v[0] + m[4]*v[1] + m[5]*v[2],
		m[6]*v[0] + m[7]*v[1] + m[8]*v[2],
	}
}

// adaptToD50 moves XYZ relative to the white point white to XYZ relative to
// D50, by the Bradford transform.
func adaptToD50(xyz, white [3]float64) [3]float64 {
	if white == d50 {
		return xyz
	}
	src := mul3(bradford, white)
	dst := mul3(bradford, d50)
	cone := mul3(bradford, xyz)
	for i := range cone {
		if src[i] != 0 {
			cone[i] *= dst[i] / src[i]
		}
	}
	return mul3(bradfordInv, cone)
}

// labToXYZ is the CIE 1976 L*a*b* to XYZ conversion relative to white, as
// ISO 32000-1, 8.6.5.4 states it for a Lab colour space.
func labToXYZ(l, a, b float64, white [3]float64) [3]float64 {
	g := func(x float64) float64 {
		if x >= 6.0/29 {
			return x * x * x
		}
		return 108.0 / 841 * (x - 4.0/29)
	}
	m := (l + 16) / 116
	return [3]float64{white[0] * g(m+a/500), white[1] * g(m), white[2] * g(m-b/200)}
}

// xyzToLab is the inverse of [labToXYZ].
func xyzToLab(xyz, white [3]float64) (l, a, b float64) {
	f := func(t float64) float64 {
		if t > 216.0/24389 {
			return math.Cbrt(t)
		}
		return t*841/108 + 4.0/29
	}
	fx, fy, fz := f(xyz[0]/white[0]), f(xyz[1]/white[1]), f(xyz[2]/white[2])
	return 116*fy - 16, 500 * (fx - fy), 200 * (fy - fz)
}

// encodeLab writes Lab relative to D50 as an ICC Lab profile's data space
// holds it (ICC.1:2010, 6.3.4.2): L*/100, (a*+128)/255 and (b*+128)/255,
// clipped to the encoding's range.
func encodeLab(l, a, b float64, out []float64) {
	out[0] = clamp01(l / 100)
	out[1] = clamp01((a + 128) / 255)
	out[2] = clamp01((b + 128) / 255)
}

func decodeLab(in []float64) (l, a, b float64) {
	return in[0] * 100, in[1]*255 - 128, in[2]*255 - 128
}

// xyzScale is the largest value the ICC XYZ encoding holds, 1 + 32767/32768
// (ICC.1:2010, 6.3.4.3).
const xyzScale = 1 + 32767.0/32768

// encodeXYZ writes XYZ relative to D50 as an [Endpoint] of [SpaceXYZ] holds
// it, unclipped.
func encodeXYZ(xyz [3]float64, out []float64) {
	for i, v := range xyz {
		out[i] = v / xyzScale
	}
}

func decodeXYZ(in []float64) [3]float64 {
	return [3]float64{in[0] * xyzScale, in[1] * xyzScale, in[2] * xyzScale}
}

// xyzD50ToSRGB takes XYZ relative to D50 to linear sRGB: the inverse of the
// sRGB colorants adapted to D50 by the Bradford transform.
var xyzD50ToSRGB = [9]float64{
	3.1338561, -1.6168667, -0.4906146,
	-0.9787684, 1.9161415, 0.0334540,
	0.0719453, -0.2289914, 1.4052427,
}

// srgbToXYZD50 is the sRGB colorants adapted to D50 (IEC 61966-2-1 with the
// Bradford transform), the matrix an sRGB ICC profile states.
var srgbToXYZD50 = [9]float64{
	0.4360747, 0.3850649, 0.1430804,
	0.2225045, 0.7168786, 0.0606169,
	0.0139322, 0.0971045, 0.7141733,
}

// srgbEncode applies the sRGB transfer function (IEC 61966-2-1).
func srgbEncode(v float64) float64 {
	v = clamp01(v)
	if v <= 0.0031308 {
		return 12.92 * v
	}
	return 1.055*math.Pow(v, 1/2.4) - 0.055
}

// srgbDecode is the inverse of [srgbEncode].
func srgbDecode(v float64) float64 {
	v = clamp01(v)
	if v <= 0.04045 {
		return v / 12.92
	}
	return math.Pow((v+0.055)/1.055, 2.4)
}

func clamp01(v float64) float64 {
	return clamp(v, 0, 1)
}

func clamp(v, lo, hi float64) float64 {
	switch {
	case math.IsNaN(v) || v < lo:
		return lo
	case v > hi:
		return hi
	}
	return v
}
