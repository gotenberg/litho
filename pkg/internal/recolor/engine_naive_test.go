package recolor

import "math"

// naiveEngine converts between endpoints by the textbook device formulas,
// ignoring their profiles: grey is equal RGB, CMYK is subtractive RGB with
// black (r = (1-c)(1-k)), and RGB to grey takes the Rec. 601 luma. Lab and
// XYZ go through sRGB by the CIE and IEC 61966-2-1 formulas. Tests pin it to
// state exact values the ICC engine's tables would only approximate.
type naiveEngine struct{}

// Transform returns the identity between endpoints that mean the same colour,
// and the formulas through sRGB otherwise.
func (naiveEngine) Transform(src, dst Endpoint, _ Options) (Converter, error) {
	if src.same(dst) {
		return identity{}, nil
	}
	return naiveConverter{src: src.Space, dst: dst.Space}, nil
}

type naiveConverter struct {
	src, dst string
}

func (c naiveConverter) Convert(in, out []float64) {
	var rgb [3]float64
	switch c.src {
	case SpaceGray:
		rgb = [3]float64{in[0], in[0], in[0]}
	case SpaceRGB:
		rgb = [3]float64{in[0], in[1], in[2]}
	case SpaceCMYK:
		k := 1 - in[3]
		rgb = [3]float64{(1 - in[0]) * k, (1 - in[1]) * k, (1 - in[2]) * k}
	case SpaceLab:
		l, a, b := decodeLab(in)
		lin := mul3(xyzD50ToSRGB, labToXYZ(l, a, b, d50))
		rgb = [3]float64{srgbEncode(lin[0]), srgbEncode(lin[1]), srgbEncode(lin[2])}
	case SpaceXYZ:
		lin := mul3(xyzD50ToSRGB, decodeXYZ(in))
		rgb = [3]float64{srgbEncode(lin[0]), srgbEncode(lin[1]), srgbEncode(lin[2])}
	}
	for i := range rgb {
		rgb[i] = clamp01(rgb[i])
	}
	switch c.dst {
	case SpaceGray:
		out[0] = 0.299*rgb[0] + 0.587*rgb[1] + 0.114*rgb[2]
	case SpaceRGB:
		copy(out, rgb[:])
	case SpaceCMYK:
		k := 1 - max(rgb[0], rgb[1], rgb[2])
		out[3] = k
		for i := range 3 {
			if k >= 1 {
				out[i] = 0
			} else {
				out[i] = (1 - rgb[i] - k) / (1 - k)
			}
		}
	case SpaceLab:
		lin := [3]float64{srgbDecode(rgb[0]), srgbDecode(rgb[1]), srgbDecode(rgb[2])}
		l, a, b := xyzToLab(mul3(srgbToXYZD50, lin), d50)
		encodeLab(l, a, b, out)
	case SpaceXYZ:
		lin := [3]float64{srgbDecode(rgb[0]), srgbDecode(rgb[1]), srgbDecode(rgb[2])}
		encodeXYZ(mul3(srgbToXYZD50, lin), out)
	}
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

func decodeLab(in []float64) (l, a, b float64) {
	return in[0] * 100, in[1]*255 - 128, in[2]*255 - 128
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
