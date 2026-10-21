package recolor

import (
	"bytes"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/icc"
)

// Converter converts one colour of a source space to the target's
// components. in holds the source components, out receives the target's,
// each in [0, 1] unless the source space defines another range (Lab, an
// ICCBased /Range). A Converter is safe for concurrent use.
type Converter interface {
	Convert(in, out []float64)
}

// ICC data colour space signatures (ICC.1:2010, 7.2.6, Table 19) an
// [Endpoint] names.
const (
	SpaceGray = "GRAY"
	SpaceRGB  = "RGB "
	SpaceCMYK = "CMYK"
	SpaceLab  = "Lab "
	SpaceXYZ  = "XYZ "
)

// Endpoint is what the device values at one end of a conversion mean: an ICC
// data colour space and the profile that characterises it. An XYZ endpoint
// with no profile is CIE XYZ relative to D50, the profile connection space
// (ICC.1:2010, 6.3), its components encoded as ICC.1:2010, 6.3.4.3 encodes
// XYZ: each value over 1 + 32767/32768, so [0, 1] holds [0, 1.99997]. Its
// components are not clipped: a CalRGB space may name primaries outside the
// visible gamut, and an engine carries such a colour to the destination,
// which clips it.
type Endpoint struct {
	Space   string
	Profile []byte
}

// same reports two endpoints whose values mean the same colour, so a
// conversion between them is the identity.
func (e Endpoint) same(o Endpoint) bool {
	return e.Space == o.Space && bytes.Equal(e.Profile, o.Profile)
}

// components returns the number of components of the endpoint's space.
func (e Endpoint) components() int {
	switch e.Space {
	case SpaceGray:
		return 1
	case SpaceCMYK:
		return 4
	}
	return 3
}

// Engine builds the device-to-device transforms recolouring converts
// through. The ICC engine provides it (ICC.1:2010, through the profile
// connection space); a test provides its own to pin exact values.
type Engine interface {
	Transform(src, dst Endpoint, opts Options) (Converter, error)
}

// Target is the colour space recolouring converts to.
type Target struct {
	// Family is graphics.FamilyDeviceGray, FamilyDeviceRGB, FamilyDeviceCMYK
	// or FamilyICCBased.
	Family string
	// Profile characterises the target's values. An ICCBased target needs
	// it. For a device family it is the profile the family means, such as an
	// output intent's (ISO 32000-1, 14.11.5); nil means litho's built-in
	// profile of that family.
	Profile []byte
	// ColorSpace is the colour space object an ICCBased target is written
	// as, an [/ICCBased stream] array or a reference to one, which the caller
	// allocates once. Device families write their name.
	ColorSpace ast.PdfObject
}

// Options are what a conversion depends on beyond its source and target.
type Options struct {
	// Intent is the rendering intent of the conversion (ISO 32000-1,
	// 8.6.5.8; ICC.1:2010, 6.2). Its zero value is perceptual; the initial
	// intent of the graphics state, and the one the renderer reads a page
	// with, is relative colorimetric (8.6.5.8), so a conversion meant to draw
	// as the source draws passes [icc.RelativeColorimetric].
	Intent icc.Intent
	// BlackPointCompensation scales the source black point to the
	// destination's.
	BlackPointCompensation bool
	// ConvertSpots converts Separation and DeviceN colour to process colour
	// through its tint transform. By default spot colour keeps its colorants,
	// and only its alternate space converts.
	ConvertSpots bool
	// DeviceGray, DeviceRGB and DeviceCMYK are the profiles the device spaces
	// of the source mean: the document's output intent where the renderer
	// reads the device space through it (ISO 32000-1, 14.11.5), so the
	// converted page draws as the source drew. nil means the target's
	// profile for the target's own family, whose values then stay as they
	// are, and litho's built-in profile for the others. A profile the engine
	// builds no transform from gives way to the built-in one.
	DeviceGray, DeviceRGB, DeviceCMYK []byte
	// Engine converts between endpoints. nil selects the package's engine.
	Engine Engine
}

// defaultEngine is the engine [Options] select when they name none.
//
// TODO: [c1 join] replace the naive stand-in with the ICC engine's
// icc.NewTransform once its output direction lands.
var defaultEngine Engine = naiveEngine{}

func (o Options) engine() Engine {
	if o.Engine != nil {
		return o.Engine
	}
	return defaultEngine
}

// targetInfo is a validated [Target]: its endpoint, component count and the
// object that names it in a content stream's resources.
type targetInfo struct {
	Target
	end Endpoint
	n   int
}

func (t Target) info() (targetInfo, error) {
	ti := targetInfo{Target: t}
	switch t.Family {
	case graphics.FamilyDeviceGray:
		ti.end = Endpoint{Space: SpaceGray, Profile: defaultProfile(SpaceGray, t.Profile)}
	case graphics.FamilyDeviceRGB:
		ti.end = Endpoint{Space: SpaceRGB, Profile: defaultProfile(SpaceRGB, t.Profile)}
	case graphics.FamilyDeviceCMYK:
		ti.end = Endpoint{Space: SpaceCMYK, Profile: defaultProfile(SpaceCMYK, t.Profile)}
	case graphics.FamilyICCBased:
		if t.Profile == nil || t.ColorSpace == nil {
			return ti, fmt.Errorf("%w: an ICCBased target needs its profile and colour space", ErrUnsupportedTarget)
		}
		p, err := icc.Parse(t.Profile)
		if err != nil {
			return ti, fmt.Errorf("%w: %w", ErrUnsupportedTarget, err)
		}
		switch p.Space {
		case SpaceGray, SpaceRGB, SpaceCMYK:
		default:
			// ISO 32000-1, 8.6.5.5, Table 66: /N is 1, 3 or 4.
			return ti, fmt.Errorf("%w: profile data colour space %q", ErrUnsupportedTarget, p.Space)
		}
		ti.end = Endpoint{Space: p.Space, Profile: t.Profile}
	default:
		return ti, fmt.Errorf("%w: family %q", ErrUnsupportedTarget, t.Family)
	}
	ti.n = ti.end.components()
	return ti, nil
}

// device reports a target written with the device colour operators.
func (t targetInfo) device() bool {
	return t.Family != graphics.FamilyICCBased
}

// spaceObject returns the colour space object the target is written as in a
// definition: a family name, or the caller's ICCBased object.
func (t targetInfo) spaceObject() ast.PdfObject {
	if t.device() {
		return ast.PdfName(t.Family)
	}
	return t.ColorSpace
}

// defaultProfile returns profile, or litho's built-in profile of space when
// profile is nil.
func defaultProfile(space string, profile []byte) []byte {
	if profile != nil {
		return profile
	}
	switch space {
	case SpaceGray:
		return builtinGray
	case SpaceRGB:
		return builtinRGB
	}
	return builtinCMYK
}

// The built-in profiles a device space means when nothing names another:
// sGray, sRGB, and the CGATS TR 001 printer profile, which reads as the
// built-in CMYK reading of DeviceCMYK does ([icc.CMYKPrinterProfile]).
var (
	builtinGray = icc.SGrayProfile()
	builtinRGB  = icc.SRGBProfile()
	builtinCMYK = icc.CMYKPrinterProfile()
)
