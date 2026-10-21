package icc

import "math"

// Black point compensation follows Adobe's algorithm, which ISO 18619:2015
// standardises: "Black-Point Compensation: Theory and Application", Adobe
// Systems, 2006. ISO 18619 is not available to this project, so the clauses
// below name the steps of the Adobe paper, and littleCMS 2.17, which
// implements the same paper, served as the oracle the tests compare against.

// perceptualBlack is the black point of the version 4 perceptual reference
// medium, XYZ relative to D50 (ICC.1:2010, 6.2): the black the perceptual and
// saturation tables of a version 4 profile map to and from.
var perceptualBlack = [3]float64{0.00336, 0.0034731, 0.00287}

// blackRampSteps is how many L* levels, 0 to 100, the destination black point
// estimate sends round the profile: the 256 of the Adobe paper's round trip.
const blackRampSteps = 256

// bpcForced reports whether the connection compensates black points without
// being asked: a version 4 destination's perceptual and saturation tables
// assume the perceptual reference medium (ICC.1:2010, 6.2), so a version 2
// source's tables, which may reach any black, must be scaled onto it.
func bpcForced(dst *Profile, intent Intent) bool {
	return dst.onReferenceMedium(intent)
}

// onReferenceMedium reports whether the profile's colours under the intent
// sit on the version 4 perceptual reference medium (ICC.1:2010, 6.2): a
// version 4 profile with tables or tone curves read under the perceptual or
// saturation intent. A profile with neither, such as [LabProfile], states
// colours of no medium.
func (p *Profile) onReferenceMedium(intent Intent) bool {
	if p.Version < 4 || intent != Perceptual && intent != Saturation {
		return false
	}
	if p.isMatrixShaper() {
		return true
	}
	for _, kind := range []string{"A2B", "B2A"} {
		for i := range 3 {
			if p.hasTable(kind, Intent(i)) {
				return true
			}
		}
	}
	return false
}

// blackPointScale returns the per-component scale and offset that map XYZ
// black bs onto bd and keep the PCS white (Adobe paper, "Applying black
// point compensation": a linear scaling in XYZ).
func blackPointScale(bs, bd [3]float64) (*[3]float64, *[3]float64) {
	var scale, offset [3]float64
	for i := range 3 {
		w := d50[i]
		t := bs[i] - w
		if t == 0 {
			scale[i] = 1
			continue
		}
		scale[i] = (bd[i] - w) / t
		offset[i] = -w * (bd[i] - bs[i]) / t
	}
	return &scale, &offset
}

// isMatrixShaper reports whether the profile has the tone curves, and for
// RGB the colorants, of a matrix and tone curve profile (ICC.1:2010, F.2 and
// F.3).
func (p *Profile) isMatrixShaper() bool {
	has := func(sigs ...string) bool {
		for _, s := range sigs {
			if _, ok := p.tags[s]; !ok {
				return false
			}
		}
		return true
	}
	switch p.Space {
	case "GRAY":
		return has("kTRC")
	case "RGB ":
		return has("rTRC", "gTRC", "bTRC", "rXYZ", "gXYZ", "bXYZ")
	}
	return false
}

// hasTable reports whether the profile has the table of the kind ("A2B" or
// "B2A") for the intent itself, with no fallback to the perceptual one.
func (p *Profile) hasTable(kind string, intent Intent) bool {
	_, ok := p.tags[kind+string("0121"[intent&3])]
	return ok
}

// supports reports whether the profile states the intent in the direction
// of the kind: a table for it, or a matrix and tone curves, which serve
// every intent. A profile that states no such intent gives no black point
// estimate for it, the paper's unsupported case, and the estimate is zero.
func (p *Profile) supports(kind string, intent Intent) bool {
	return p.hasTable(kind, intent) || p.isMatrixShaper()
}

// sourceBlackPoint estimates the black point of the profile used as the
// source of a connection under the intent, XYZ relative to D50 (Adobe paper,
// "Determining the source black point").
func (p *Profile) sourceBlackPoint(intent Intent) [3]float64 {
	if p.onReferenceMedium(intent) {
		if p.isMatrixShaper() {
			return p.darkestColorant(RelativeColorimetric)
		}
		return perceptualBlack
	}
	if intent == RelativeColorimetric && p.Class == "prtr" && p.Space == "CMYK" {
		// An output profile's colorimetric black is its ink limit: the
		// perceptual table's black, read back colorimetrically, discounts it.
		if !p.supports("A2B", Perceptual) {
			return [3]float64{}
		}
		trip, ok := p.roundTripper(Perceptual)
		if !ok {
			return [3]float64{}
		}
		return clippedBlack(trip(0, 0, 0)[0])
	}
	return p.darkestColorant(intent)
}

// darkestColorant converts the darkest device colour of the profile's data
// colour space to the connection space under the intent, and keeps its
// lightness only: the black point as the darkest colorant.
func (p *Profile) darkestColorant(intent Intent) [3]float64 {
	if !p.supports("A2B", intent) {
		return [3]float64{}
	}
	var black [maxOutputs]float64
	switch p.Space {
	case "GRAY", "RGB ":
	case "CMYK", "CMY ":
		for i := range p.Channels() {
			black[i] = 1
		}
	default:
		return [3]float64{}
	}
	s, err := p.toPCS(intent)
	if err != nil {
		return [3]float64{}
	}
	l, _, _ := xyzToLab(s.eval(&black))
	return clippedBlack(l)
}

// clippedBlack is the neutral of lightness l, clipped to 0 to 50: a black
// point keeps no hue, and none is lighter than mid grey (Adobe paper).
func clippedBlack(l float64) [3]float64 {
	return labToXYZ(min(max(l, 0), 50), 0, 0)
}

// roundTripper returns a function that sends CIELAB through the profile's
// BToA side under the intent and back through its relative colorimetric
// AToB side, reporting false when the profile lacks either side.
func (p *Profile) roundTripper(intent Intent) (func(l, a, b float64) [3]float64, bool) {
	d, err := p.fromPCS(intent)
	if err != nil {
		return nil, false
	}
	s, err := p.toPCS(RelativeColorimetric)
	if err != nil {
		return nil, false
	}
	var scale, offset *[3]float64
	if p.onReferenceMedium(intent) {
		// CIELAB reaches black at L* 0, below the perceptual reference
		// medium's black, so it is scaled onto it as any source would be.
		scale, offset = blackPointScale([3]float64{}, perceptualBlack)
	}
	return func(l, a, b float64) [3]float64 {
		xyz := labToXYZ(l, a, b)
		if scale != nil {
			for i := range xyz {
				xyz[i] = scale[i]*xyz[i] + offset[i]
			}
		}
		var v [maxOutputs]float64
		d.eval(xyz, &v)
		for i := range d.n {
			v[i] = clamp01(v[i])
		}
		l2, a2, b2 := xyzToLab(s.eval(&v))
		return [3]float64{l2, a2, b2}
	}, true
}

// destinationBlackPoint estimates the black point of the profile used as the
// destination of a connection under the intent, XYZ relative to D50 (Adobe
// paper, "Determining the destination black point"): it sends a ramp of L*
// round the profile, and where the round trip bends off the straight line
// it fits a quadratic to the shadows and takes its root.
func (p *Profile) destinationBlackPoint(intent Intent) [3]float64 {
	if p.onReferenceMedium(intent) {
		if p.isMatrixShaper() {
			return p.darkestColorant(RelativeColorimetric)
		}
		return perceptualBlack
	}
	switch p.Space {
	case "GRAY", "RGB ", "CMYK":
	default:
		return p.sourceBlackPoint(intent)
	}
	if !p.hasTable("B2A", intent) {
		return p.sourceBlackPoint(intent)
	}
	var initial [3]float64
	if intent == RelativeColorimetric {
		initial[0], initial[1], initial[2] = xyzToLab(p.sourceBlackPoint(intent))
	}
	trip, ok := p.roundTripper(intent)
	if !ok {
		return [3]float64{}
	}
	var in, out [blackRampSteps]float64
	for i := range blackRampSteps {
		l := float64(i) * 100 / (blackRampSteps - 1)
		in[i], out[i] = l, trip(l, min(max(initial[1], -50), 50), min(max(initial[2], -50), 50))[0]
	}
	for i := blackRampSteps - 2; i > 0; i-- {
		out[i] = min(out[i], out[i+1])
	}
	lo, hi := out[0], out[blackRampSteps-1]
	if !(lo < hi) {
		return [3]float64{}
	}
	if intent == RelativeColorimetric {
		// A round trip straight through the mid tones means the initial
		// estimate already is the black point.
		straight := true
		for i := range blackRampSteps {
			if in[i] > lo+0.2*(hi-lo) && math.Abs(in[i]-out[i]) >= 4 {
				straight = false
				break
			}
		}
		if straight {
			return labToXYZ(initial[0], initial[1], initial[2])
		}
	}
	// The shadow section the quadratic is fitted to, as fractions of the
	// round trip's range: the Adobe paper's 0.1 to 0.5 for relative
	// colorimetric, 0.03 to 0.25 for the other intents.
	from, to := 0.03, 0.25
	if intent == RelativeColorimetric {
		from, to = 0.1, 0.5
	}
	var xs, ys []float64
	for i := range blackRampSteps {
		y := (out[i] - lo) / (hi - lo)
		if y >= from && y < to {
			xs, ys = append(xs, in[i]), append(ys, y)
		}
	}
	if len(xs) < 3 {
		return [3]float64{}
	}
	return labToXYZ(quadraticRoot(xs, ys), initial[1], initial[2])
}

// quadraticRoot fits y = a x^2 + b x + c to the points by least squares and
// returns the root where the fit leaves zero, clipped to 0 to 50: the L* of
// the destination black point.
func quadraticRoot(xs, ys []float64) float64 {
	var sx, sx2, sx3, sx4, sy, sxy, sx2y float64
	for i, x := range xs {
		y, x2 := ys[i], x*x
		sx, sx2, sx3, sx4 = sx+x, sx2+x2, sx3+x2*x, sx4+x2*x2
		sy, sxy, sx2y = sy+y, sxy+x*y, sx2y+x2*y
	}
	n := float64(len(xs))
	inv, ok := invert3([9]float64{n, sx, sx2, sx, sx2, sx3, sx2, sx3, sx4})
	if !ok {
		return 0
	}
	c := inv[0]*sy + inv[1]*sxy + inv[2]*sx2y
	b := inv[3]*sy + inv[4]*sxy + inv[5]*sx2y
	a := inv[6]*sy + inv[7]*sxy + inv[8]*sx2y
	if math.Abs(a) < 1e-10 {
		if math.Abs(b) < 1e-10 {
			return 0
		}
		return min(max(-c/b, 0), 50)
	}
	d := b*b - 4*a*c
	if d <= 0 {
		return 0
	}
	return min(max((-b+math.Sqrt(d))/(2*a), 0), 50)
}
