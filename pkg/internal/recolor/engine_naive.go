package recolor

// naiveEngine converts between endpoints by the textbook device formulas,
// ignoring their profiles: grey is equal RGB, CMYK is subtractive RGB with
// black (r = (1-c)(1-k)), and RGB to grey takes the Rec. 601 luma. Lab and
// XYZ go through sRGB by the CIE and IEC 61966-2-1 formulas. It stands in for the
// ICC engine until that engine converts to device spaces, and tests pin it to
// state exact values.
type naiveEngine struct{}

// Transform returns the identity between endpoints that mean the same colour,
// and the formulas through sRGB otherwise.
func (naiveEngine) Transform(src, dst Endpoint, _ Options) (Converter, error) {
	if src.same(dst) {
		return identity{}, nil
	}
	return naiveConverter{src: src.Space, dst: dst.Space}, nil
}

// identity copies its input.
type identity struct{}

func (identity) Convert(in, out []float64) {
	copy(out, in)
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
