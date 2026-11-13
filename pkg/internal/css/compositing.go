package css

// BlendMode is how an element or a background layer composites with what
// lies beneath it (CSS Compositing 1, 5.3): the sixteen modes of
// <blend-mode>, which ISO 32000-1, 11.3.5 names alike, and plus-lighter
// (CSS Compositing 2, 9.1.1), which mix-blend-mode alone takes.
type BlendMode uint8

// The blend modes, in the order of ISO 32000-1, Tables 136 and 137.
const (
	BlendNormal BlendMode = iota
	BlendMultiply
	BlendScreen
	BlendOverlay
	BlendDarken
	BlendLighten
	BlendColorDodge
	BlendColorBurn
	BlendHardLight
	BlendSoftLight
	BlendDifference
	BlendExclusion
	BlendHue
	BlendSaturation
	BlendColor
	BlendLuminosity
	// BlendPlusLighter adds the colours, clamped: no PDF blend mode does.
	BlendPlusLighter
)

// blendModes are the <blend-mode> keywords (CSS Compositing 1, 5.3).
// plus-darker (CSS Compositing 2, 9.1.2) is not one: Chromium does not
// parse it, and an element that names it paints as normal.
var blendModes = map[string]BlendMode{
	"normal": BlendNormal, "multiply": BlendMultiply, "screen": BlendScreen,
	"overlay": BlendOverlay, "darken": BlendDarken, "lighten": BlendLighten,
	"color-dodge": BlendColorDodge, "color-burn": BlendColorBurn,
	"hard-light": BlendHardLight, "soft-light": BlendSoftLight,
	"difference": BlendDifference, "exclusion": BlendExclusion, "hue": BlendHue,
	"saturation": BlendSaturation, "color": BlendColor, "luminosity": BlendLuminosity,
}

// parseMixBlendMode parses mix-blend-mode: a <blend-mode> or plus-lighter
// (CSS Compositing 2, 3.2).
func parseMixBlendMode(v []token, _ *declCtx) (any, bool) {
	k, ok := keyword(v)
	if !ok {
		return nil, false
	}
	if k == "plus-lighter" {
		return BlendPlusLighter, true
	}
	m, ok := blendModes[k]
	return m, ok
}

// parseBgBlendMode parses background-blend-mode, a list of <blend-mode>
// one per background layer (CSS Compositing 1, 3.4), and keeps the first:
// [Style] holds one background layer.
func parseBgBlendMode(v []token, _ *declCtx) (any, bool) {
	var first BlendMode
	layers := splitCommas(v)
	for i, l := range layers {
		k, ok := keyword(l)
		if !ok {
			return nil, false
		}
		m, ok := blendModes[k]
		if !ok {
			return nil, false
		}
		if i == 0 {
			first = m
		}
	}
	return first, len(layers) > 0
}

// compositingProps are the properties of CSS Compositing 1, 3.
func compositingProps() map[propID]propDef {
	blend := func(name string, parse parseFn, f func(*Style) *BlendMode) propDef {
		return propDef{name: name, parse: parse, compute: func(_ *styler, st *Style, v any) {
			if m, ok := v.(BlendMode); ok {
				*f(st) = m
			}
		}, copyFrom: field(f)}
	}
	return map[propID]propDef{
		pMixBlendMode: blend("mix-blend-mode", parseMixBlendMode, func(s *Style) *BlendMode { return &s.MixBlendMode }),
		pBgBlendMode:  blend("background-blend-mode", parseBgBlendMode, func(s *Style) *BlendMode { return &s.BackgroundBlendMode }),
		pIsolation: enumProp("isolation", false, func(s *Style) *Isolation { return &s.Isolation }, map[string]Isolation{
			"auto": IsolationAuto, "isolate": IsolationIsolate,
		}),
	}
}

// Isolation is whether an element must form an isolated group (CSS
// Compositing 1, 3.3).
type Isolation uint8

// The isolation values.
const (
	IsolationAuto Isolation = iota
	IsolationIsolate
)
