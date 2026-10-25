package icc

import (
	"fmt"
	"math"
)

// TransformOptions selects how a [Link] connects two profiles.
type TransformOptions struct {
	// Intent is the rendering intent, which picks each profile's table
	// (ICC.1:2010, 6.2, and the AToB and BToA tags of 9.2).
	Intent Intent
	// BlackPointCompensation maps the source profile's black point onto the
	// destination's, scaling XYZ between them and the PCS white (ISO
	// 18619:2015). It has no effect under the absolute colorimetric intent.
	BlackPointCompensation bool
}

// Link converts colours of one profile's data colour space to another's,
// through the profile connection space, or directly through a DeviceLink
// profile.
//
// Components are normalized to 0 to 1: a device value over its range, and
// Lab data in the version 4 encoding of ICC.1:2010, 6.3.4.2, L* over 0 to
// 100 and a* and b* over -128 to 127. Outputs are clipped to 0 to 1.
//
// Link is safe for concurrent use: it keeps no state between calls.
type Link struct {
	in, out int
	// direct is the table of a DeviceLink profile, which replaces the rest.
	direct *pipeline
	// pcs marks a link from the profile connection space ([NewPCSLink]),
	// whose inputs are XYZ itself and src is nil.
	pcs bool
	src *toPCS
	dst *fromPCS
	// srcFallback and dstFallback name the table each side was read through
	// in place of the intent's own (see [Link.Fallbacks]).
	srcFallback, dstFallback Fallback
	// scale and offset adjust XYZ between the two sides, for the absolute
	// colorimetric intent or black point compensation; nil leaves it as is.
	scale, offset *[3]float64
	// white maps the source's device white onto the destination's (see
	// [NewLink]); nil leaves the tables' reading of it.
	white *whiteFixup
}

// whiteFixup moves a link's conversions near the source's device white so
// that the white itself lands on the destination's device white.
type whiteFixup struct {
	// at is the source's device white, n components, and to the
	// destination's.
	at, to []float64
	// delta is to less the tables' reading of at, per output.
	delta [maxOutputs]float64
	// span is how far from at, per component, the move fades out.
	span float64
}

// The white fix-up's reach. Little CMS 2.16 resamples a transform it
// optimizes into a grid of 33 nodes per input channel, 17 for a four-channel
// input (cmspcs.c, the default precalculation), and replaces the node at the
// source white, so the move fades over one grid cell: 1/32 of the range from
// a grey or RGB white, 1/16 from a CMYK one.
const (
	whiteFixupSpan     = 1.0 / 32
	whiteFixupSpanCMYK = 1.0 / 16
)

// whiteFixupLimit is how far, in a fraction of a component's range, the
// tables may send the source white from the destination's for the fix-up to
// apply. Farther, the profile means its white to be a colour, as a profile
// that maps white to orange does, and the tables are kept. It is the bound
// Little CMS 2.16 keeps, 0xf000 of 0xffff.
const whiteFixupLimit = float64(0xf000) / 0xffff

// NewLink builds the transform from src's data colour space to dst's for the
// options. src and dst are each read through their table for the intent
// (ICC.1:2010, 6.2, and the AToB and BToA tags of 9.2), else their perceptual
// table, else their matrix and tone curves (8.4 to 8.6). The absolute
// colorimetric intent reads the relative colorimetric tables and scales XYZ
// by the ratio of the two media white points (6.2). A version 4 destination
// read under the perceptual or saturation intent compensates black points
// whatever the options say, since its tables assume the perceptual reference
// medium. When src is a DeviceLink profile (7.2.5), its table is the whole
// transform and dst must be nil.
//
// Under every intent but absolute colorimetric, a link between two device
// spaces with a white, grey, RGB or CMYK, sends the source's white (1 per
// component, no ink for CMYK) to the destination's exactly. ICC.1:2010, 6.2
// defines the colorimetric intents as relative to the media white, which
// the source's white is and maps onto the destination's medium, and the
// perceptual and saturation intents to the reference medium, whose white
// does the same; yet a profile's tables only approximate their white, so a
// white sent to a CMYK printer profile comes out a fraction of a per cent
// of each ink, a light tint on paper that should stay bare. The link moves
// the result by what the tables miss, the whole of it at the white and
// fading to nothing one grid cell away (whiteFixupSpan), and leaves a white
// that lands farther than whiteFixupLimit as the profile states it.
// Absolute colorimetric keeps the medium's tint, which is its purpose
// (ICC.1:2010, 6.2). A DeviceLink's table is the whole transform its author
// built (7.2.5), and Lab is no device white, so neither moves.
//
// This is what Little CMS does by default under these intents, and the
// tools that write converted colours with it: Ghostscript builds its links
// without cmsFLAGS_NOWHITEONWHITEFIXUP except for PostScript CIE spaces,
// and MuPDF without it at all. poppler (cmsFLAGS_NOOPTIMIZE) and PDFium (a
// format of no named colour space) build transforms Little CMS does not
// fix up, so [Profile.NewTransform], which draws for display, keeps the
// tables' reading of white as they do.
//
// # Corpus evidence
//
//   - All corpora: of the 24 distinct CMYK profiles among the 3,936 profile
//     streams that a link can convert to, 21 send sRGB white to some ink
//     under the relative colorimetric intent and 7 to half a level or more,
//     up to 15.7 levels (verapdf PDF_A-1b 6-2-3-3-t02-pass-a's output
//     intent). litho's stock CMYK printer profile sends it 0.9 levels, so
//     a document converted to CMYK wrote its white as 0.0035 0.0025 0.0026 0 k.
//   - All corpora: none of the 3,936 profile streams reads its own white off
//     sRGB white through [Profile.NewTransform] but three: two litho
//     fixtures that tint it on purpose and pdfjs issue2856's YCC profile, in
//     which full components are no white.
func NewLink(src, dst *Profile, opts TransformOptions) (*Link, error) {
	if src == nil {
		return nil, fmt.Errorf("%w: no source profile", ErrUnsupported)
	}
	if src.Class == "link" {
		if dst != nil {
			return nil, fmt.Errorf("%w: a DeviceLink profile takes no destination", ErrUnsupported)
		}
		return newDeviceLink(src)
	}
	if dst == nil {
		return nil, fmt.Errorf("%w: no destination profile", ErrUnsupported)
	}
	for _, p := range []*Profile{src, dst} {
		if err := checkDeviceClass(p); err != nil {
			return nil, err
		}
	}
	intent := opts.Intent & 3
	s, err := src.toPCS(intent)
	if err != nil {
		return nil, fmt.Errorf("reading the source profile: %w", err)
	}
	d, err := dst.fromPCS(intent)
	if err != nil {
		return nil, fmt.Errorf("reading the destination profile: %w", err)
	}
	l := &Link{
		in: s.n, out: d.n, src: s, dst: d,
		srcFallback: src.fallback("A2B", intent), dstFallback: dst.fallback("B2A", intent),
	}
	switch {
	case intent == AbsoluteColorimetric:
		// Per ICC.1:2010, 6.2, absolute XYZ is relative XYZ scaled by the
		// media white over the PCS illuminant, so from one profile to the
		// other the scale is the ratio of their media whites.
		ws, wd := src.mediaWhite(), dst.mediaWhite()
		l.scale = &[3]float64{ws[0] / wd[0], ws[1] / wd[1], ws[2] / wd[2]}
		l.offset = &[3]float64{}
	case opts.BlackPointCompensation || bpcForced(dst, intent):
		bs, bd := src.sourceBlackPoint(intent), dst.destinationBlackPoint(intent)
		if bs != bd {
			l.scale, l.offset = blackPointScale(bs, bd)
		}
	}
	if intent != AbsoluteColorimetric {
		l.white = l.fixWhite(src.Space, dst.Space)
	}
	return l, nil
}

// deviceWhite returns the white of a data colour space, normalized: full
// value for grey and RGB, no colorant for CMYK. Other spaces have none.
func deviceWhite(space string) ([]float64, bool) {
	switch space {
	case "GRAY":
		return []float64{1}, true
	case "RGB ":
		return []float64{1, 1, 1}, true
	case "CMYK":
		return []float64{0, 0, 0, 0}, true
	}
	return nil, false
}

// fixWhite returns the fix-up [NewLink] describes for a link from the
// srcSpace data colour space to dstSpace, or nil when the tables already
// send the source white to the destination's, send it too far to move, or
// either space has no white. It runs before l.white is set, so Convert reads
// the tables alone.
func (l *Link) fixWhite(srcSpace, dstSpace string) *whiteFixup {
	at, ok := deviceWhite(srcSpace)
	if !ok || len(at) != l.in {
		return nil
	}
	to, ok := deviceWhite(dstSpace)
	if !ok || len(to) != l.out {
		return nil
	}
	got := make([]float64, l.out)
	l.Convert(at, got)
	f := &whiteFixup{at: at, to: to, span: whiteFixupSpan}
	if l.in == 4 {
		f.span = whiteFixupSpanCMYK
	}
	moved := false
	for i := range to {
		d := to[i] - got[i]
		if math.Abs(d) > whiteFixupLimit {
			return nil
		}
		f.delta[i] = d
		moved = moved || d != 0
	}
	if !moved {
		return nil
	}
	return f
}

// weight returns how much of the fix-up a colour of normalized components in
// takes: 1 at the white, falling to 0 a span away along any component. It is
// the weight the white node of a grid carries when Little CMS interpolates a
// colour in the cell next to it: the least of the components' fractions,
// tetrahedral over RGB and linear over grey, and for CMYK that of cyan,
// magenta and yellow times the black's, as it interpolates linearly between
// two tetrahedral readings along black.
func (f *whiteFixup) weight(in []float64) float64 {
	w := 1.0
	for i, at := range f.at {
		t := 1 - math.Abs(clamp01(in[i])-at)/f.span
		if t <= 0 {
			return 0
		}
		if i == 3 {
			w *= t
		} else {
			w = min(w, t)
		}
	}
	return w
}

// NewPCSLink builds the transform from the profile connection space, CIE XYZ
// relative to D50 (ICC.1:2010, 6.3), to dst's data colour space. It is the
// destination half of a [Link], for a source that reaches the PCS by
// formulas of its own, such as a CalRGB space (ISO 32000-1, 8.6.5.3).
//
// Unlike a [Link] from a profile, [Link.Convert] takes the XYZ values
// themselves, Y = 1 at the PCS white, and does not clip them: a colour
// outside dst's gamut, a CalRGB primary outside the visible gamut among them,
// reaches dst's table, which maps it into the gamut. Black point compensation
// maps the PCS black, XYZ 0, onto dst's black point, as for a source profile
// on an ideal medium; the absolute colorimetric intent scales from the D50
// white.
func NewPCSLink(dst *Profile, opts TransformOptions) (*Link, error) {
	if dst == nil {
		return nil, fmt.Errorf("%w: no destination profile", ErrUnsupported)
	}
	if err := checkDeviceClass(dst); err != nil {
		return nil, err
	}
	intent := opts.Intent & 3
	d, err := dst.fromPCS(intent)
	if err != nil {
		return nil, fmt.Errorf("reading the destination profile: %w", err)
	}
	l := &Link{in: 3, out: d.n, dst: d, pcs: true, dstFallback: dst.fallback("B2A", intent)}
	switch {
	case intent == AbsoluteColorimetric:
		wd := dst.mediaWhite()
		l.scale = &[3]float64{d50[0] / wd[0], d50[1] / wd[1], d50[2] / wd[2]}
		l.offset = &[3]float64{}
	case opts.BlackPointCompensation || bpcForced(dst, intent):
		if bd := dst.destinationBlackPoint(intent); bd != [3]float64{} {
			l.scale, l.offset = blackPointScale([3]float64{}, bd)
		}
	}
	return l, nil
}

// checkDeviceClass refuses a profile a [Link] cannot connect through the PCS:
// a DeviceLink, abstract or named colour profile, which has no device side
// to convert from or to, and a class ICC.1:2010, 7.2.5, Table 18 does not
// list, which makes the header malformed.
func checkDeviceClass(p *Profile) error {
	switch p.Class {
	case "scnr", "mntr", "prtr", "spac":
		return nil
	case "link", "abst", "nmcl":
		return fmt.Errorf("%w: a %q class profile as an end of a connection", ErrUnsupported, p.Class)
	}
	return fmt.Errorf("%w: device class %q", ErrMalformed, p.Class)
}

// newDeviceLink builds the transform of a DeviceLink profile: its AToB0
// table maps the data colour space the header names onto the one it names
// in the PCS field (ICC.1:2010, 7.2.7 and 8.8).
func newDeviceLink(p *Profile) (*Link, error) {
	in, out := spaceChannels(p.Space), spaceChannels(p.PCS)
	if in == 0 || out == 0 {
		return nil, fmt.Errorf("%w: DeviceLink from %q to %q", ErrUnsupported, p.Space, p.PCS)
	}
	b, ok := p.tags["A2B0"]
	if !ok {
		return nil, fmt.Errorf("%w: DeviceLink without an AToB0 table", ErrMalformed)
	}
	lut, err := readPipeline(b, "")
	if err != nil {
		return nil, err
	}
	if lut.in != in || lut.out != out {
		return nil, fmt.Errorf("%w: DeviceLink table of %d inputs and %d outputs for %d and %d channels", ErrMalformed, lut.in, lut.out, in, out)
	}
	return &Link{in: in, out: out, direct: lut}, nil
}

// Fallback names the table a profile was read through when it holds none for
// the rendering intent a [Link] was built for.
type Fallback uint8

const (
	// NoFallback is a profile read as the intent asks: through the intent's
	// own table, or through a matrix and tone curves, which serve every
	// intent alike (ICC.1:2010, 8.4 to 8.6).
	NoFallback Fallback = iota
	// PerceptualFallback is a profile read through its perceptual table, the
	// one ICC.1:2010, 6.2 has a profile without the intent's table fall back
	// to.
	PerceptualFallback
	// MatrixFallback is a profile holding tables for other intents than the
	// one asked, and no perceptual table, read through its matrix and tone
	// curves.
	MatrixFallback
)

// String returns "perceptual", "matrix", or "" for [NoFallback].
func (f Fallback) String() string {
	switch f {
	case PerceptualFallback:
		return "perceptual"
	case MatrixFallback:
		return "matrix"
	case NoFallback:
	}
	return ""
}

// Fallbacks reports the table each side of the link was read through in place
// of the one for its rendering intent. The source is [NoFallback] for a link
// from the PCS or a DeviceLink profile, whose one table serves every intent
// (ICC.1:2010, 8.8). The absolute colorimetric intent reads the relative
// colorimetric tables by definition (6.2), which is no fallback.
func (l *Link) Fallbacks() (src, dst Fallback) {
	return l.srcFallback, l.dstFallback
}

// fallback returns the table [Profile.intentTag] reads for the intent instead
// of the intent's own, for the kind "A2B" or "B2A".
func (p *Profile) fallback(kind string, intent Intent) Fallback {
	if _, ok := p.tags[kind+string("0121"[intent&3])]; ok {
		return NoFallback
	}
	if _, ok := p.tags[kind+"0"]; ok {
		return PerceptualFallback
	}
	for _, n := range []string{"1", "2"} {
		if _, ok := p.tags[kind+n]; ok {
			return MatrixFallback
		}
	}
	return NoFallback
}

// InputChannels returns the number of components [Link.Convert] takes.
func (l *Link) InputChannels() int {
	return l.in
}

// OutputChannels returns the number of components [Link.Convert] gives.
func (l *Link) OutputChannels() int {
	return l.out
}

// Convert converts one colour: in holds [Link.InputChannels] components and
// out receives [Link.OutputChannels]. A link from [NewPCSLink] takes XYZ.
func (l *Link) Convert(in, out []float64) {
	var v [maxOutputs]float64
	if l.pcs {
		xyz := [3]float64{in[0], in[1], in[2]}
		if s, o := l.scale, l.offset; s != nil {
			for i := range xyz {
				xyz[i] = s[i]*xyz[i] + o[i]
			}
		}
		l.dst.eval(xyz, &v)
		for i := range l.out {
			out[i] = clamp01(v[i])
		}
		return
	}
	for i := range l.in {
		v[i] = clamp01(in[i])
	}
	if l.direct != nil {
		l.direct.run(&v)
	} else {
		xyz := l.src.eval(&v)
		if s, o := l.scale, l.offset; s != nil {
			for i := range xyz {
				xyz[i] = s[i]*xyz[i] + o[i]
			}
		}
		l.dst.eval(xyz, &v)
	}
	if f := l.white; f != nil {
		switch w := f.weight(in); {
		case w == 1:
			copy(v[:l.out], f.to)
		case w > 0:
			for i := range l.out {
				v[i] += w * f.delta[i]
			}
		}
	}
	for i := range l.out {
		out[i] = clamp01(v[i])
	}
}

// run evaluates the whole table on normalized inputs in v, input curves
// included, and leaves its normalized outputs there.
func (p *pipeline) run(v *[maxOutputs]float64) {
	if p.btoa {
		for i, c := range p.b {
			v[i] = c.eval(v[i])
		}
		if m := p.matrix; m != nil {
			x, y, z := v[0], v[1], v[2]
			for i := range 3 {
				v[i] = m[3*i]*x + m[3*i+1]*y + m[3*i+2]*z + m[9+i]
			}
		}
		for i, c := range p.m {
			v[i] = c.eval(v[i])
		}
		if p.clut != nil {
			p.clut.eval(v)
		}
		for i, c := range p.a {
			v[i] = c.eval(v[i])
		}
		return
	}
	if m := p.inMatrix; m != nil {
		x, y, z := v[0], v[1], v[2]
		for i := range 3 {
			v[i] = m[3*i]*x + m[3*i+1]*y + m[3*i+2]*z
		}
	}
	for i, c := range p.a {
		v[i] = c.eval(v[i])
	}
	p.eval(v)
}

// toPCS takes device values of a profile to XYZ relative to D50.
type toPCS struct {
	n int
	// lut is the AToB table; labData marks a lut16Type over Lab data, whose
	// input takes the legacy encoding.
	lut     *pipeline
	labData bool
	// curves and colorant are a matrix and tone curve profile's, or a grey
	// profile's one curve, with labGrey for a grey profile over Lab.
	curves   []curve
	colorant [9]float64
	labGrey  bool
	// lab marks Lab data with no table, read as the PCS itself.
	lab bool
}

// toPCS builds the profile's device to PCS side for the intent.
func (p *Profile) toPCS(intent Intent) (*toPCS, error) {
	n := p.Channels()
	if n == 0 || n > maxChannels {
		return nil, fmt.Errorf("%w: data colour space %q", ErrUnsupported, p.Space)
	}
	if p.PCS != "XYZ " && p.PCS != "Lab " {
		return nil, fmt.Errorf("%w: connection space %q", ErrUnsupported, p.PCS)
	}
	s := &toPCS{n: n}
	if b, ok := p.intentTag("A2B", intent); ok {
		lut, err := readPipeline(b, p.PCS)
		if err != nil {
			return nil, err
		}
		if lut.in != n || lut.out != 3 {
			return nil, fmt.Errorf("%w: table of %d inputs and %d outputs for %d components", ErrMalformed, lut.in, lut.out, n)
		}
		s.lut, s.labData = lut, p.Space == "Lab " && string(b[:4]) == "mft2"
		if lut.clut != nil && p.Space == "Lab " {
			lut.clut.trilinear = true
		}
		return s, nil
	}
	switch {
	case p.Space == "Lab " && p.PCS == "Lab ":
		s.lab = true
		return s, nil
	case n == 1:
		c, err := p.curveTag("kTRC")
		if err != nil {
			return nil, err
		}
		s.curves, s.labGrey = []curve{c}, p.PCS == "Lab "
		return s, nil
	case p.Space == "RGB ":
		cs, m, err := p.matrixShaper()
		if err != nil {
			return nil, err
		}
		s.curves, s.colorant = cs, m
		return s, nil
	}
	return nil, fmt.Errorf("%w: a %q profile without an AToB table", ErrMalformed, p.Space)
}

// intentTag returns the profile's table of the kind ("A2B" or "B2A") for the
// intent, else its perceptual table (ICC.1:2010, 6.2, and the AToB and BToA tags of 9.2). The absolute
// colorimetric intent reads the relative colorimetric table.
func (p *Profile) intentTag(kind string, intent Intent) ([]byte, bool) {
	sig := kind + string("0121"[intent&3])
	if b, ok := p.tags[sig]; ok {
		return b, true
	}
	b, ok := p.tags[kind+"0"]
	return b, ok
}

// matrixShaper reads the tone curves and colorants of a matrix and tone
// curve RGB profile (ICC.1:2010, F.3).
func (p *Profile) matrixShaper() ([]curve, [9]float64, error) {
	var m [9]float64
	cs := make([]curve, 3)
	for i, sig := range []string{"rTRC", "gTRC", "bTRC"} {
		c, err := p.curveTag(sig)
		if err != nil {
			return nil, m, err
		}
		cs[i] = c
	}
	for i, sig := range [3]string{"rXYZ", "gXYZ", "bXYZ"} {
		xyz, ok := p.xyzTag(sig)
		if !ok {
			return nil, m, fmt.Errorf("%w: no %s colorant", ErrMalformed, sig)
		}
		m[i], m[3+i], m[6+i] = xyz[0], xyz[1], xyz[2]
	}
	return cs, m, nil
}

// eval converts the normalized device values in v to XYZ relative to D50.
func (s *toPCS) eval(v *[maxOutputs]float64) [3]float64 {
	switch {
	case s.lut != nil:
		if s.labData {
			v4ToLegacy(v)
		}
		s.lut.run(v)
		return s.lut.pcs(v[:3])
	case s.lab:
		return labToXYZ(labFromV4(v[:3]))
	case s.n == 1:
		// The grey tone curve gives Y under an XYZ connection space and L*
		// under a Lab one (ICC.1:2010, F.2).
		y := eval(s.curves[0], v[0])
		if s.labGrey {
			return labToXYZ(y*100, 0, 0)
		}
		return [3]float64{y * d50[0], y * d50[1], y * d50[2]}
	}
	r, g, b := eval(s.curves[0], v[0]), eval(s.curves[1], v[1]), eval(s.curves[2], v[2])
	m := &s.colorant
	return [3]float64{m[0]*r + m[1]*g + m[2]*b, m[3]*r + m[4]*g + m[5]*b, m[6]*r + m[7]*g + m[8]*b}
}

// eval evaluates c at x, a nil c being the identity.
func eval(c curve, x float64) float64 {
	if c == nil {
		return clamp01(x)
	}
	return c.eval(x)
}

// fromPCS takes XYZ relative to D50 to device values of a profile.
type fromPCS struct {
	n int
	// lut is the BToA table; labData marks a lut16Type over Lab data, whose
	// output takes the legacy encoding.
	lut     *pipeline
	labData bool
	// inverse and matrix are a matrix and tone curve profile's inverted tone
	// curves and colorants, or a grey profile's one inverted curve, with
	// labGrey for a grey profile over Lab.
	inverse []curve
	matrix  [9]float64
	labGrey bool
	lab     bool
}

// fromPCS builds the profile's PCS to device side for the intent.
func (p *Profile) fromPCS(intent Intent) (*fromPCS, error) {
	n := p.Channels()
	if n == 0 || n > maxOutputs {
		return nil, fmt.Errorf("%w: data colour space %q", ErrUnsupported, p.Space)
	}
	if p.PCS != "XYZ " && p.PCS != "Lab " {
		return nil, fmt.Errorf("%w: connection space %q", ErrUnsupported, p.PCS)
	}
	d := &fromPCS{n: n}
	if b, ok := p.intentTag("B2A", intent); ok {
		lut, err := readBPipeline(b, p.PCS)
		if err != nil {
			return nil, err
		}
		if lut.in != 3 || lut.out != n {
			return nil, fmt.Errorf("%w: table of %d inputs and %d outputs for %d components", ErrMalformed, lut.in, lut.out, n)
		}
		d.lut, d.labData = lut, p.Space == "Lab " && string(b[:4]) == "mft2"
		if lut.clut != nil && lut.encoding != pcsXYZ {
			lut.clut.trilinear = true
		}
		return d, nil
	}
	switch {
	case p.Space == "Lab " && p.PCS == "Lab ":
		d.lab = true
		return d, nil
	case n == 1:
		c, err := p.curveTag("kTRC")
		if err != nil {
			return nil, err
		}
		d.inverse, d.labGrey = []curve{invertCurve(c)}, p.PCS == "Lab "
		return d, nil
	case p.Space == "RGB ":
		cs, m, err := p.matrixShaper()
		if err != nil {
			return nil, err
		}
		inv, ok := invert3(m)
		if !ok {
			return nil, fmt.Errorf("%w: singular colorant matrix", ErrMalformed)
		}
		d.matrix = inv
		for _, c := range cs {
			d.inverse = append(d.inverse, invertCurve(c))
		}
		return d, nil
	}
	return nil, fmt.Errorf("%w: a %q profile without a BToA table", ErrMalformed, p.Space)
}

// eval converts XYZ relative to D50 to normalized device values in v.
func (d *fromPCS) eval(xyz [3]float64, v *[maxOutputs]float64) {
	switch {
	case d.lut != nil:
		d.lut.fromXYZ(xyz, v)
		d.lut.run(v)
		if d.labData {
			legacyToV4(v)
		}
	case d.lab:
		l, a, b := xyzToLab(xyz)
		labToEncoding(l, a, b, pcsLabV4, v)
	case d.n == 1:
		y := xyz[1] / d50[1]
		if d.labGrey {
			l, _, _ := xyzToLab(xyz)
			y = l / 100
		}
		v[0] = eval(d.inverse[0], y)
	default:
		m := &d.matrix
		for i := range 3 {
			v[i] = eval(d.inverse[i], m[3*i]*xyz[0]+m[3*i+1]*xyz[1]+m[3*i+2]*xyz[2])
		}
	}
}

// mediaWhite returns the profile's media white point (the mediaWhitePointTag, ICC.1:2010, 9.2),
// the PCS illuminant when the profile records none. A version 2 display
// profile's white is the PCS illuminant too: ICC.1:2001-04 has it
// record the display's unadapted white, which relative colorimetry has
// already mapped onto the illuminant.
func (p *Profile) mediaWhite() [3]float64 {
	if p.Version < 4 && p.Class == "mntr" {
		return d50
	}
	if w, ok := p.xyzTag("wtpt"); ok && w[0] > 0 && w[1] > 0 && w[2] > 0 {
		return w
	}
	return d50
}

// LabProfile returns a profile of the Lab data colour space over the Lab
// connection space with no table: a [Link] from it reads CIELAB relative to
// D50, in the version 4 encoding, and one to it gives CIELAB.
func LabProfile() *Profile {
	return &Profile{Class: "spac", Space: "Lab ", PCS: "Lab ", Version: 4, tags: map[string][]byte{}}
}
