package icc

import (
	"fmt"
	"math"
)

// Intent is an ICC rendering intent (ICC.1:2010, 6.2), the values of the
// profile header's field.
type Intent int

// The rendering intents.
const (
	Perceptual Intent = iota
	RelativeColorimetric
	Saturation
	AbsoluteColorimetric
)

// d50 is the PCS illuminant (ICC.1:2010, 7.2.16).
var d50 = [3]float64{0.9642, 1.0, 0.8249}

// xyzToSRGB takes PCS XYZ, relative to D50, to linear sRGB: the inverse of the
// sRGB colorants adapted to D50 by the Bradford transform, which is how an
// sRGB ICC profile states them.
var xyzToSRGB = [9]float64{
	3.1338561, -1.6168667, -0.4906146,
	-0.9787684, 1.9161415, 0.0334540,
	0.0719453, -0.2289914, 1.4052427,
}

// Transform converts colours of a profile's data colour space to sRGB.
//
// Transform is safe for concurrent use: it keeps no state between calls.
type Transform struct {
	n        int
	lut      *pipeline
	trc      [3]curve
	colorant [9]float64
	grey     curve
	labGrey  bool
}

// NewTransform builds the transform from the profile's data colour space to
// sRGB for the rendering intent: through the profile's AToB table for the
// intent, else its AToB0 table, else its matrix and tone curves (ICC.1:2010,
// 8.4 to 8.6). An absolute colorimetric intent is rendered relative.
func (p *Profile) NewTransform(intent Intent) (*Transform, error) {
	n := p.Components()
	if n == 0 {
		return nil, fmt.Errorf("%w: data colour space %q", ErrUnsupported, p.Space)
	}
	if p.PCS != "XYZ " && p.PCS != "Lab " {
		return nil, fmt.Errorf("%w: connection space %q", ErrUnsupported, p.PCS)
	}
	t := &Transform{n: n}
	tag := [4]string{"A2B0", "A2B1", "A2B2", "A2B1"}[intent&3]
	b, ok := p.tags[tag]
	if !ok {
		b, ok = p.tags["A2B0"]
	}
	if ok {
		lut, err := readPipeline(b, p.PCS)
		if err != nil {
			return nil, err
		}
		if lut.in != n || lut.out != 3 {
			return nil, fmt.Errorf("%w: table of %d inputs and %d outputs for %d components", ErrMalformed, lut.in, lut.out, n)
		}
		t.lut = lut
		return t, nil
	}
	switch n {
	case 1:
		c, err := p.curveTag("kTRC")
		if err != nil {
			return nil, err
		}
		t.grey, t.labGrey = c, p.PCS == "Lab "
		return t, nil
	case 3:
		for i, sig := range []string{"rTRC", "gTRC", "bTRC"} {
			c, err := p.curveTag(sig)
			if err != nil {
				return nil, err
			}
			t.trc[i] = c
		}
		for i, sig := range []string{"rXYZ", "gXYZ", "bXYZ"} {
			xyz, ok := p.xyzTag(sig)
			if !ok {
				return nil, fmt.Errorf("%w: no %s colorant", ErrMalformed, sig)
			}
			t.colorant[i], t.colorant[3+i], t.colorant[6+i] = xyz[0], xyz[1], xyz[2]
		}
		return t, nil
	}
	return nil, fmt.Errorf("%w: a %q profile without an AToB table", ErrMalformed, p.Space)
}

// curveTag reads a tone curve tag.
func (p *Profile) curveTag(sig string) (curve, error) {
	b, ok := p.tags[sig]
	if !ok {
		return nil, fmt.Errorf("%w: no %s curve", ErrMalformed, sig)
	}
	c, _, err := readCurve(b)
	return c, err
}

// Components returns the number of input components the transform takes.
func (t *Transform) Components() int {
	return t.n
}

// Size returns roughly how many bytes the transform holds, for a cache that
// bounds what it keeps.
func (t *Transform) Size() int {
	size := 512
	add := func(cs ...curve) {
		for _, c := range cs {
			if s, ok := c.(sampled); ok {
				size += 8 * len(s)
			}
		}
	}
	add(t.trc[:]...)
	add(t.grey)
	if p := t.lut; p != nil {
		add(p.a...)
		add(p.m...)
		add(p.b...)
		if p.clut != nil {
			size += 8 * len(p.clut.data)
		}
	}
	return size
}

// Convert converts one colour, t.Components() bytes, to sRGB.
func (t *Transform) Convert(in []byte) (uint8, uint8, uint8) {
	var xyz [3]float64
	switch {
	case t.lut != nil:
		var v [maxOutputs]float64
		for i := range t.n {
			v[i] = float64(in[i]) / 255
		}
		t.lut.eval(&v)
		xyz = t.lut.pcs(v[:3])
	case t.grey != nil:
		// The grey tone curve gives Y under an XYZ connection space and L*
		// under a Lab one (ICC.1:2010, F.2).
		y := t.grey.eval(float64(in[0]) / 255)
		if t.labGrey {
			xyz = labToXYZ(y*100, 0, 0)
		} else {
			xyz = [3]float64{y * d50[0], y * d50[1], y * d50[2]}
		}
	default:
		r, g, b := t.trc[0].eval(float64(in[0])/255), t.trc[1].eval(float64(in[1])/255), t.trc[2].eval(float64(in[2])/255)
		m := &t.colorant
		xyz = [3]float64{m[0]*r + m[1]*g + m[2]*b, m[3]*r + m[4]*g + m[5]*b, m[6]*r + m[7]*g + m[8]*b}
	}
	m := &xyzToSRGB
	return encode(m[0]*xyz[0] + m[1]*xyz[1] + m[2]*xyz[2]),
		encode(m[3]*xyz[0] + m[4]*xyz[1] + m[5]*xyz[2]),
		encode(m[6]*xyz[0] + m[7]*xyz[1] + m[8]*xyz[2])
}

// pcs reads the pipeline's normalized outputs in its PCS encoding and returns
// XYZ relative to D50.
func (p *pipeline) pcs(v []float64) [3]float64 {
	switch p.encoding {
	case pcsXYZ:
		s := 65535.0 / 32768
		return [3]float64{v[0] * s, v[1] * s, v[2] * s}
	case pcsLab16:
		return labToXYZ(v[0]*65535/65280*100, v[1]*65535/256-128, v[2]*65535/256-128)
	default:
		return labToXYZ(v[0]*100, v[1]*255-128, v[2]*255-128)
	}
}

// labToXYZ converts CIELAB relative to D50 to XYZ.
func labToXYZ(l, a, b float64) [3]float64 {
	fy := (l + 16) / 116
	fx, fz := fy+a/500, fy-b/200
	f := func(t float64) float64 {
		if t > 6.0/29 {
			return t * t * t
		}
		return 3 * (6.0 / 29) * (6.0 / 29) * (t - 4.0/29)
	}
	return [3]float64{d50[0] * f(fx), d50[1] * f(fy), d50[2] * f(fz)}
}

// encode applies the sRGB transfer function (IEC 61966-2-1) to a linear value
// clipped to 0 to 1, and scales it to a byte.
func encode(v float64) uint8 {
	v = clamp01(v)
	if v <= 0.0031308 {
		v *= 12.92
	} else {
		v = 1.055*math.Pow(v, 1/2.4) - 0.055
	}
	return uint8(math.Round(clamp01(v) * 255))
}
