package icc

import (
	"fmt"
	"math"
	"sync"
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

// ParseIntent returns the rendering intent a PDF name selects (ISO 32000-1,
// 8.6.5.8, Table 70). Per 8.6.5.8 an unrecognised name selects relative
// colorimetric, which is also the graphics state's initial intent.
func ParseIntent(name string) Intent {
	switch name {
	case "Perceptual":
		return Perceptual
	case "Saturation":
		return Saturation
	case "AbsoluteColorimetric":
		return AbsoluteColorimetric
	}
	return RelativeColorimetric
}

// String returns the PDF name of the rendering intent (ISO 32000-1, Table 70).
func (i Intent) String() string {
	return [4]string{"Perceptual", "RelativeColorimetric", "Saturation", "AbsoluteColorimetric"}[i&3]
}

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
	n int
	// input tabulates the first stage for each component: the tone curve, or
	// the table's input curve, at each of the 256 values a byte takes. It
	// gives what evaluating the curve would, without evaluating it per pixel.
	input    [][256]float64
	lut      *pipeline
	colorant [9]float64
	labGrey  bool
	srgb     *srgbTable
	// identity caches what [Transform.Identity] finds, computed on first use.
	identity func() bool
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
	t := &Transform{n: n, srgb: srgb()}
	t.identity = sync.OnceValue(t.readsAsIdentity)
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
		t.input = make([][256]float64, n)
		for i := range n {
			var c curve
			if i < len(lut.a) {
				c = lut.a[i]
			}
			t.input[i] = tabulate(c)
		}
		return t, nil
	}
	switch n {
	case 1:
		c, err := p.curveTag("kTRC")
		if err != nil {
			return nil, err
		}
		t.input, t.labGrey = [][256]float64{tabulate(c)}, p.PCS == "Lab "
		return t, nil
	case 3:
		t.input = make([][256]float64, 3)
		for i, sig := range []string{"rTRC", "gTRC", "bTRC"} {
			c, err := p.curveTag(sig)
			if err != nil {
				return nil, err
			}
			t.input[i] = tabulate(c)
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

// tabulate evaluates c at each byte value, scaled to 0 to 1; a nil c is the
// identity.
func tabulate(c curve) [256]float64 {
	var table [256]float64
	for j := range table {
		x := float64(j) / 255
		if c != nil {
			x = c.eval(x)
		}
		table[j] = x
	}
	return table
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

// identityGridStep spaces the colours [Transform.Identity] samples off the
// grey and single-channel ramps: every 17th level, so 0 and 255 included,
// 16 levels per channel.
const identityGridStep = 17

// identityTolerance is how many levels a colour may come back off itself
// for [Transform.Identity] to count it as given back. The colorants of an
// ICC profile are s15Fixed16 numbers (ICC.1:2010, 4.6), so even the sRGB
// profile's matrix is rounded, and it brings a few saturated colours back a
// level off, the rounding two colour engines differ by.
const identityTolerance = 1

// Identity reports whether the transform gives every colour back as it came,
// to within identityTolerance, so that its colours already are sRGB values,
// as they are under an sRGB profile. Only a grey or an RGB profile of tone
// curves and colorants qualifies: its result is a smooth function of each
// channel. Such a transform is taken for the identity when every grey, every
// level of each channel alone and every colour of a 16-level grid come back
// as they came. The check runs once per transform.
func (t *Transform) Identity() bool {
	return t.identity()
}

// readsAsIdentity computes what [Transform.Identity] reports.
func (t *Transform) readsAsIdentity() bool {
	if t.lut != nil {
		return false
	}
	near := func(a, b uint8) bool {
		return int(a)-int(b) <= identityTolerance && int(b)-int(a) <= identityTolerance
	}
	same := func(in []byte, want [3]byte) bool {
		r, g, b := t.Convert(in)
		return near(r, want[0]) && near(g, want[1]) && near(b, want[2])
	}
	for v := range 256 {
		c := byte(v)
		if t.n == 1 {
			if !same([]byte{c}, [3]byte{c, c, c}) {
				return false
			}
			continue
		}
		for _, px := range [][3]byte{{c, c, c}, {c, 0, 0}, {0, c, 0}, {0, 0, c}} {
			if !same(px[:], px) {
				return false
			}
		}
	}
	if t.n == 1 {
		return true
	}
	for r := 0; r < 256; r += identityGridStep {
		for g := 0; g < 256; g += identityGridStep {
			for b := 0; b < 256; b += identityGridStep {
				px := [3]byte{byte(r), byte(g), byte(b)}
				if !same(px[:], px) {
					return false
				}
			}
		}
	}
	return true
}

// Size returns roughly how many bytes the transform holds, for a cache that
// bounds what it keeps.
func (t *Transform) Size() int {
	size := 512 + 8*256*len(t.input)
	add := func(cs ...curve) {
		for _, c := range cs {
			if s, ok := c.(sampled); ok {
				size += 8 * len(s)
			}
		}
	}
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
			v[i] = t.input[i][in[i]]
		}
		t.lut.eval(&v)
		xyz = t.lut.pcs(v[:3])
	case t.n == 1:
		// The grey tone curve gives Y under an XYZ connection space and L*
		// under a Lab one (ICC.1:2010, F.2).
		y := t.input[0][in[0]]
		if t.labGrey {
			xyz = labToXYZ(y*100, 0, 0)
		} else {
			xyz = [3]float64{y * d50[0], y * d50[1], y * d50[2]}
		}
	default:
		r, g, b := t.input[0][in[0]], t.input[1][in[1]], t.input[2][in[2]]
		m := &t.colorant
		xyz = [3]float64{m[0]*r + m[1]*g + m[2]*b, m[3]*r + m[4]*g + m[5]*b, m[6]*r + m[7]*g + m[8]*b}
	}
	m, s := &xyzToSRGB, t.srgb
	return s.encode(m[0]*xyz[0] + m[1]*xyz[1] + m[2]*xyz[2]),
		s.encode(m[3]*xyz[0] + m[4]*xyz[1] + m[5]*xyz[2]),
		s.encode(m[6]*xyz[0] + m[7]*xyz[1] + m[8]*xyz[2])
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

// srgbSteps is how many equal steps srgbTable.start divides the linear range
// into. The transfer function is steepest at 0, where 1/4096 spans 0.8 of a
// level, so encoding a value from its step's level takes at most one step up.
const srgbSteps = 4096

// srgbTable gives what encode gives without a power function per call.
type srgbTable struct {
	// start is the level of each step's lowest value.
	start [srgbSteps + 1]uint8
	// rise[k] is the least value encode takes above level k.
	rise [255]float64
}

// srgb builds the table once. Each rise is found by bisecting encode itself
// over the ordered float64 values from 0 to 1, so the table reads exactly as
// encode does wherever encode does not decrease.
var srgb = sync.OnceValue(func() *srgbTable {
	s := &srgbTable{}
	for i := range s.start {
		s.start[i] = encode(float64(i) / srgbSteps)
	}
	for k := range s.rise {
		// encode(0) is 0 and encode(1) is 255, so the least value above
		// level k lies in (lo, hi].
		lo, hi := math.Float64bits(0), math.Float64bits(1)
		for hi-lo > 1 {
			mid := lo + (hi-lo)/2
			if int(encode(math.Float64frombits(mid))) > k {
				hi = mid
			} else {
				lo = mid
			}
		}
		s.rise[k] = math.Float64frombits(hi)
	}
	return s
})

// encode is encode through the table. A NaN, which clamp01 lets through,
// reads as 0.
func (s *srgbTable) encode(v float64) uint8 {
	if !(v > 0) {
		return 0
	}
	if v >= 1 {
		return 255
	}
	// v*srgbSteps is exact: srgbSteps is a power of two.
	k := s.start[int(v*srgbSteps)]
	for k < 255 && v >= s.rise[k] {
		k++
	}
	return k
}
