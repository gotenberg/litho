package blend

// Kind is the family of a blending colour space (ISO 32000-1, 11.3.4): the
// number of its components and whether they are additive or subtractive.
type Kind uint8

// The blending colour space families. The renderer's elements arrive in sRGB
// and its output leaves in sRGB, so RGB is the space a group blends in when
// nothing names another.
const (
	RGB Kind = iota
	Gray
	CMYK
)

// Channels returns how many components a colour of the family carries.
func (k Kind) Channels() int {
	switch k {
	case Gray:
		return 1
	case CMYK:
		return 4
	case RGB:
	}
	return 3
}

// Components holds a colour's components in a blending colour space, as PDF gives
// them: Gray uses [0], RGB [0] to [2], CMYK [0] to [3], each in [0, 1], a
// CMYK component an amount of ink.
type Components [4]float32

// Converter connects a blending colour space with sRGB, the space the
// renderer's rasters and its output are in. Colours are components as
// [Components] holds them.
type Converter interface {
	// ToRGB returns the sRGB of c.
	ToRGB(c Components) [3]float32
	// FromRGB returns the colour of the space nearest the sRGB colour rgb.
	FromRGB(rgb [3]float32) Components
}

// UncompensatedConverter is a [Converter] that also takes sRGB colours into
// its space without black point compensation (ISO 18619:2015), as an element
// painted under /UseBlackPtComp /OFF asks (ISO 32000-2, Table 57).
// A space whose converter is not one takes every element's colours through
// [Converter.FromRGB].
type UncompensatedConverter interface {
	Converter
	// FromRGBUncompensated is [Converter.FromRGB] without black point
	// compensation.
	FromRGBUncompensated(rgb [3]float32) Components
}

// Space is a blending colour space other than sRGB: the grey or CMYK space a
// transparency group's /CS names (ISO 32000-1, 11.6.6, Table 147), in which
// its elements composite (11.3.4). A nil *Space is sRGB.
//
// A Space is immutable; it is safe for concurrent use when its [Converter]
// is.
type Space struct {
	kind Kind
	key  string
	conv Converter
}

// NewSpace returns the blending colour space of family kind, converted to
// and from sRGB through conv, and told apart from other spaces of its family
// by key: two spaces of one family and key are the same space, between which
// a colour passes unconverted. A nil conv converts by the device formulas of
// ISO 32000-1, 10.3. An RGB space returns nil, sRGB itself: the renderer
// blends every RGB group in sRGB.
func NewSpace(kind Kind, key string, conv Converter) *Space {
	if kind == RGB {
		return nil
	}
	return &Space{kind: kind, key: key, conv: conv}
}

// Kind returns the family of s, RGB for nil.
func (s *Space) Kind() Kind {
	if s == nil {
		return RGB
	}
	return s.kind
}

// Same reports whether s and o are one space.
func (s *Space) Same(o *Space) bool {
	if s == nil || o == nil {
		return s == o
	}
	return s.kind == o.kind && s.key == o.key
}

// Key returns the key [NewSpace] told s apart by, "" for sRGB.
func (s *Space) Key() string {
	if s == nil {
		return ""
	}
	return s.key
}

// ToRGB returns the sRGB of the components c of s.
func (s *Space) ToRGB(c Components) [3]float32 { return s.toRGB(s.store(c)) }

// FromRGB returns the components of s nearest the sRGB colour rgb.
func (s *Space) FromRGB(rgb [3]float32) Components { return s.load(s.fromRGB(rgb, false)) }

// FromRGBUncompensated is [Space.FromRGB] for a colour painted without black
// point compensation ([UncompensatedConverter]).
func (s *Space) FromRGBUncompensated(rgb [3]float32) Components {
	return s.load(s.fromRGB(rgb, true))
}

// channels returns how many colour channels a group in s holds.
func (s *Space) channels() int { return s.Kind().Channels() }

// store returns the components of c as a group in s holds them: additive.
// ISO 32000-1, 11.3.5 applies the blend functions of a subtractive space to
// the complements of its components, and 11.4 composites any space by
// weighted sums, which complementing leaves unchanged; holding a CMYK colour
// as its complement lets one arithmetic serve every family, and makes 0 the
// transparent premultiplied value in each.
func (s *Space) store(c Components) Components {
	if s.Kind() == CMYK {
		return Components{1 - c[0], 1 - c[1], 1 - c[2], 1 - c[3]}
	}
	return c
}

// load undoes [Space.store].
func (s *Space) load(c Components) Components {
	return s.store(c)
}

// toRGB returns the sRGB of the stored colour c.
func (s *Space) toRGB(c Components) [3]float32 {
	switch s.Kind() {
	case RGB:
		return [3]float32{c[0], c[1], c[2]}
	case Gray:
		if s.conv != nil {
			return s.conv.ToRGB(c)
		}
		// ISO 32000-1, 10.3: DeviceGray to DeviceRGB repeats the grey.
		return [3]float32{c[0], c[0], c[0]}
	case CMYK:
	}
	if s.conv != nil {
		return s.conv.ToRGB(s.load(c))
	}
	// ISO 32000-1, 10.3: red = 1 - min(1, cyan + black), and so on; held
	// complemented, the ink sum's complement is c + k - 1.
	return [3]float32{clamp01(c[0] + c[3] - 1), clamp01(c[1] + c[3] - 1), clamp01(c[2] + c[3] - 1)}
}

// fromRGB returns the stored colour of s nearest the sRGB colour rgb, without
// black point compensation when uncompensated is set and s's converter can
// leave it out.
func (s *Space) fromRGB(rgb [3]float32, uncompensated bool) Components {
	switch s.Kind() {
	case RGB:
		return Components{rgb[0], rgb[1], rgb[2]}
	case Gray:
		if s.conv != nil {
			return s.convFromRGB(rgb, uncompensated)
		}
		// ISO 32000-1, 10.3: DeviceRGB to DeviceGray weighs the
		// components as the NTSC luminance does.
		return Components{clamp01(lum(rgb))}
	case CMYK:
	}
	if s.conv != nil {
		return s.store(s.convFromRGB(rgb, uncompensated))
	}
	// ISO 32000-1, 10.3: cyan = 1 - red and so on, black the smallest of
	// the three, removed from them in full.
	c, m, y := 1-rgb[0], 1-rgb[1], 1-rgb[2]
	k := min(c, m, y)
	if k >= 1 {
		return s.store(Components{0, 0, 0, 1})
	}
	return s.store(Components{clamp01(c - k), clamp01(m - k), clamp01(y - k), clamp01(k)})
}

// convFromRGB converts rgb through s's converter, by its uncompensated
// conversion when uncompensated is set and it has one.
func (s *Space) convFromRGB(rgb [3]float32, uncompensated bool) Components {
	if uc, ok := s.conv.(UncompensatedConverter); ok && uncompensated {
		return uc.FromRGBUncompensated(rgb)
	}
	return s.conv.FromRGB(rgb)
}

// convert returns the stored colour c of space from as space to stores it,
// through sRGB when they differ, into to without black point compensation
// when uncompensated is set ([Space.fromRGB]).
func convert(c Components, from, to *Space, uncompensated bool) Components {
	if from.Same(to) {
		return c
	}
	return to.fromRGB(from.toRGB(c), uncompensated)
}

// ProfiledConverter is a [Converter] of a grey space that reads its colours
// through an ICC profile rather than as DeviceGray's levels (ISO 32000-1,
// 8.6.5.5), whose components then weigh light as the profile's tone curve
// does, not as a device's grey.
type ProfiledConverter interface {
	Converter
	// Profiled reports whether the space is read through a profile.
	Profiled() bool
}

// luminosity returns the luminosity of the stored colour c, the value a
// luminosity soft mask takes from it (ISO 32000-1, 11.5.3): a grey is its own
// luminosity, an RGB colour weighs its components by the Lum function of
// 11.3.5, and a CMYK colour, or a grey read through a profile
// ([ProfiledConverter]), is weighed as it looks, through sRGB.
//
// A grey profile's component is no device grey: under a tone curve of gamma
// 1.0 the component of sRGB grey 0.5 is 0.21, its luminance, and a mask of
// that value would hide more than the same mask drawn in an RGB group. Read
// through sRGB, the two groups mask alike.
//
// # Corpus evidence
//
//   - litho greygroup_2b.pdf: black through a luminosity mask of sRGB grey
//     0.5 drawn in a grey group of a linear tone curve draws 126 to 127 on
//     poppler 26.09.0, PDFium and MuPDF 1.26.10, and 126 here; the component
//     itself would draw 199.
func (s *Space) luminosity(c Components) float32 {
	switch s.Kind() {
	case Gray:
		if p, ok := s.conv.(ProfiledConverter); ok && p.Profiled() {
			return lum(s.toRGB(c))
		}
		return c[0]
	case CMYK:
		return lum(s.toRGB(c))
	case RGB:
	}
	return lum([3]float32{c[0], c[1], c[2]})
}

// White returns the white of s: every component 1 in an additive space, no
// ink in CMYK.
func (s *Space) White() Components {
	return s.load(Components{1, 1, 1, 1})
}
