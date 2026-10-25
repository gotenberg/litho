package recolor

// kOnlyBlack converts a DeviceGray level to the black colorant alone, the
// conversion ISO 32000-1, 10.3.3 defines between DeviceGray and DeviceCMYK:
// c = m = y = 0, k = 1 - gray. It writes the four components of a CMYK
// target, DeviceCMYK or an ICCBased space of four components.
type kOnlyBlack struct{}

func (kOnlyBlack) Convert(in, out []float64) {
	out[0], out[1], out[2] = 0, 0, 0
	out[3] = 1 - clamp01(in[0])
}

// kOnlyRGBBlack converts DeviceRGB to the black colorant alone where the
// colour is black, 0 0 0, and through next, the space's profile, otherwise.
type kOnlyRGBBlack struct {
	next Converter
}

func (c kOnlyRGBBlack) Convert(in, out []float64) {
	if isRGBBlack(in) {
		out[0], out[1], out[2], out[3] = 0, 0, 0, 1
		return
	}
	c.next.Convert(in, out)
}

// isRGBBlack reports an RGB colour whose three components are 0, or below
// as a viewer clips them (ISO 32000-1, 8.6.4.3).
func isRGBBlack(in []float64) bool {
	return len(in) == 3 && in[0] <= 0 && in[1] <= 0 && in[2] <= 0
}

// solidPlan returns the plan a solid colour of the space p plans is
// converted with: the colour a colour operator sets in a content stream
// (ISO 32000-1, 8.6.8, Table 74), and the device colour an annotation
// dictionary or a default appearance string states (Table 164, 12.7.3.3).
// Under [Options.KOnlyBlack] and a CMYK target, DeviceGray converts to the
// black colorant alone (10.3.3), and DeviceRGB black, 0 0 0, to 100% K, so
// text and line art print on one plate, as print practice asks of black
// text: an ink spread over four plates misregisters at a glyph's edges and
// adds to the total ink. Every other plan, and every plan without the
// option, comes back as it is.
//
// Images, shadings, Indexed palettes and spot alternates convert through
// their profile whatever the option: their tones are continuous, where the
// deeper black and the smoother shadows of a four-colour black serve, and
// they register as one raster.
//
// A space a default space stands in for (8.6.5.6) is CIE-based, calibrated
// by its profile, and so is an ICCBased space, of whatever family: both
// convert through their profile. An RGB grey, r = g = b above 0, does too:
// 10.3.4 leaves the black of an RGB colour to the device's black generation
// and undercolour removal, so no K-only reading of it is the spec's.
func (s *session) solidPlan(p *spacePlan) *spacePlan {
	if !s.opts.KOnlyBlack || !s.t.cmyk() || p == nil || p.kind != planConvert {
		return p
	}
	switch {
	case p.deviceGray:
		if s.kOnlyGray == nil {
			s.kOnlyGray = &spacePlan{kind: planConvert, n: 1, conv: kOnlyBlack{}, initial: p.initial, kOnly: true}
		}
		return s.kOnlyGray
	case p.deviceRGB:
		q, ok := s.kOnlyRGB[variant{bpc: s.bpc, intent: s.intent}]
		if !ok {
			c := *p
			c.conv, c.kOnly = kOnlyRGBBlack{next: p.conv}, true
			q = &c
			s.kOnlyRGB[variant{bpc: s.bpc, intent: s.intent}] = q
		}
		return q
	}
	return p
}

// kOnlyWritten reports whether p converts comps to the black colorant
// alone under [Options.KOnlyBlack], which [Report.KOnlyBlack] counts.
func (p *spacePlan) kOnlyWritten(comps []float64) bool {
	if p == nil || !p.kOnly {
		return false
	}
	if p.n == 1 {
		return true
	}
	return isRGBBlack(comps)
}
