package icc

import (
	"fmt"
	"math"
	"sync"
)

// Intent is an ICC rendering intent (ICC.1:2010, 6.2), the values of the
// profile header's field, in its two low bits. The two bits above carry the
// [BPC] choice a graphics state makes for colours converted under it
// ([Intent.WithBPC]), so that every cache keyed on an intent keeps the two
// apart. Code that reads the intent itself masks it with 3.
type Intent int

// BPC is the black point compensation a graphics state asks of a colour
// conversion: the /UseBlackPtComp entry of an ExtGState (ISO 32000-2, Table
// 57), whose initial value is /Default.
type BPC int

// The black point compensation choices of ISO 32000-2, Table 57.
const (
	// BPCDefault leaves the choice to litho, which compensates (see
	// [Profile.NewTransform]).
	BPCDefault BPC = iota
	// BPCOn compensates.
	BPCOn
	// BPCOff does not compensate.
	BPCOff
)

// ParseBPC returns the choice a /UseBlackPtComp name selects, and false for a
// name Table 57 does not define.
func ParseBPC(name string) (BPC, bool) {
	switch name {
	case "ON":
		return BPCOn, true
	case "OFF":
		return BPCOff, true
	case "Default":
		return BPCDefault, true
	}
	return BPCDefault, false
}

// String returns the /UseBlackPtComp name of the choice (ISO 32000-2,
// Table 57).
func (b BPC) String() string {
	switch b {
	case BPCOn:
		return "ON"
	case BPCOff:
		return "OFF"
	case BPCDefault:
		return "Default"
	}
	// Only [Intent.BPC] makes another value, from bits WithBPC never sets.
	return "Default"
}

// WithBPC returns the intent with its black point compensation choice set to
// b.
func (i Intent) WithBPC(b BPC) Intent {
	return i&3 | Intent(b&3)<<2
}

// BPC returns the black point compensation choice the intent carries.
func (i Intent) BPC() BPC {
	return BPC(i>>2) & 3
}

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
	// bpcScale and bpcOffset map the profile's black point onto sRGB's in
	// XYZ (see [Profile.NewTransform]); nil leaves XYZ as the profile reads
	// it.
	bpcScale, bpcOffset *[3]float64
	// identity caches what [Transform.Identity] finds, computed on first use.
	identity func() bool
	// neutral caches what [Transform.Neutral] finds, computed on first use.
	neutral func() bool
}

// NewTransform builds the transform from the profile's data colour space to
// sRGB for the rendering intent: through the profile's AToB table for the
// intent, else its AToB0 table, else its matrix and tone curves (ICC.1:2010,
// 8.4 to 8.6). An absolute colorimetric intent is rendered relative.
//
// Under every intent but absolute colorimetric, the transform compensates
// black points (ISO 18619:2015): it scales XYZ so that the profile's black
// point lands on sRGB's black, XYZ 0, and the PCS white stays. ISO 32000-1,
// 8.6.5.8 leaves how a rendering intent is honoured to the conforming
// reader, and the viewers litho is measured against compensate: poppler
// builds every Little CMS transform with cmsFLAGS_BLACKPOINTCOMPENSATION,
// and PDFium reads into a version 4 sRGB profile under the perceptual intent,
// where Little CMS compensates whatever the flags say (ICC.1:2010, 6.2: a
// version 4 perceptual table assumes the perceptual reference medium).
// Without it a profile whose darkest colour is lighter than sRGB black, such
// as a YCC scanner profile whose black reads L* 10.9, draws its black as a
// dark grey where both viewers draw black. Absolute colorimetric keeps the
// medium's own black, as Little CMS does under that intent.
//
// The intent's [BPC] choice overrides litho's: [BPCOff] builds the transform
// without compensation, as a graphics state whose /UseBlackPtComp is /OFF
// demands (ISO 32000-2, Table 57: the conversion shall not use it). [BPCOn]
// and [BPCDefault] compensate; neither does so under absolute colorimetric,
// whose purpose is to keep the medium's black, as Little CMS ignores its
// compensation flag under that intent.
//
// # Corpus evidence
//
//   - pdfjs issue2856: its YCC image blanked by a redaction, samples 0, 128,
//     128, drew (29, 29, 29) uncompensated; poppler and PDFium draw (0, 0, 0),
//     as litho now does.
//   - All corpora: of the render cell's pages, 2 move toward poppler and none
//     away; litho matches Little CMS 2.17 with compensation to within 1 level
//     on every ICC profile behind an image the change moves.
func (p *Profile) NewTransform(intent Intent) (*Transform, error) {
	t, err := p.newTransform(intent)
	if err != nil {
		return nil, err
	}
	if intent&3 != AbsoluteColorimetric && intent.BPC() != BPCOff {
		// sRGB is an ideal display medium: the darkest colorant of its
		// matrix and tone curves is XYZ 0, the destination black point of
		// the Adobe paper's estimate, so only the source side is estimated.
		if bs := p.sourceBlackPoint(intent & 3); bs != ([3]float64{}) {
			t.bpcScale, t.bpcOffset = blackPointScale(bs, [3]float64{})
		}
	}
	return t, nil
}

// newTransform builds the transform [Profile.NewTransform] describes,
// without black point compensation.
func (p *Profile) newTransform(intent Intent) (*Transform, error) {
	n := p.Components()
	if n == 0 {
		return nil, fmt.Errorf("%w: data colour space %q", ErrUnsupported, p.Space)
	}
	if p.PCS != "XYZ " && p.PCS != "Lab " {
		return nil, fmt.Errorf("%w: connection space %q", ErrUnsupported, p.PCS)
	}
	t := &Transform{n: n, srgb: srgb()}
	t.identity = sync.OnceValue(t.readsAsIdentity)
	t.neutral = sync.OnceValue(t.readsAsNeutral)
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

// neutralTolerance is how many levels the three sRGB channels of a grey may
// differ by for [Transform.Neutral] to count the grey as neutral: the
// rounding of the PCS illuminant's s15Fixed16 coordinates (ICC.1:2010, 4.6)
// and of the sRGB matrix already sets a tone curve's greys a level apart.
const neutralTolerance = 1

// Neutral reports whether the transform is a grey profile's that draws every
// sample as a grey: its three sRGB channels within neutralTolerance of each
// other at each of the 256 sample values. A grey profile's tone curve
// characterises an achromatic device (ICC.1:2010, F.2: the PCS value is the
// illuminant scaled by the curve's Y, or L* with a* and b* 0), but an AToB
// table maps a grey sample to any PCS colour (8.3.3, 8.4.2), so a profile
// whose table tints its greys draws them coloured, as Little CMS reads it.
// A neutral transform's colours are read off one channel; any other
// transform's, a grey one included, through all three. The check runs once
// per transform; a transform of more than one component is not neutral.
//
// # Corpus evidence
//
//   - All corpora: of the 348 grey ICC profile streams (23 distinct), none
//     draws a sample more than a level off the neutral axis under any
//     intent, so every grey ICC colour they hold keeps its one-channel
//     reading.
//   - litho greytint_2b.pdf: its profile's table tints the middle greys,
//     which poppler 26.09.0, PDFium and MuPDF draw brown and litho drew grey.
func (t *Transform) Neutral() bool {
	return t.neutral()
}

// readsAsNeutral computes what [Transform.Neutral] reports.
func (t *Transform) readsAsNeutral() bool {
	if t.n != 1 {
		return false
	}
	for v := range 256 {
		r, g, b := t.Convert([]byte{byte(v)})
		if int(max(r, g, b))-int(min(r, g, b)) > neutralTolerance {
			return false
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
	xyz := t.xyz(in)
	if sc, off := t.bpcScale, t.bpcOffset; sc != nil {
		xyz = [3]float64{sc[0]*xyz[0] + off[0], sc[1]*xyz[1] + off[1], sc[2]*xyz[2] + off[2]}
	}
	m, s := &xyzToSRGB, t.srgb
	return s.encode(m[0]*xyz[0] + m[1]*xyz[1] + m[2]*xyz[2]),
		s.encode(m[3]*xyz[0] + m[4]*xyz[1] + m[5]*xyz[2]),
		s.encode(m[6]*xyz[0] + m[7]*xyz[1] + m[8]*xyz[2])
}

// Lab converts one colour, t.Components() bytes, to CIELAB relative to D50,
// the profile connection space reading before any clipping to sRGB, so two
// colours sRGB shows alike, out of its gamut, still differ.
func (t *Transform) Lab(in []byte) (l, a, b float64) {
	return xyzToLab(t.xyz(in))
}

// xyz converts one colour, t.Components() bytes, to XYZ relative to D50.
func (t *Transform) xyz(in []byte) [3]float64 {
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
	return xyz
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
