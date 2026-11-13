package css

import (
	"math"
	"slices"
)

// VariationSetting is one entry of font-variation-settings (CSS Fonts 4,
// 8.2): a variation axis tag and its value.
type VariationSetting struct {
	// Tag is the four-character axis tag, each character from U+20 to
	// U+7E.
	Tag string
	// Value is the axis value, finite and within ±maxVariationValue.
	Value float64
}

// maxVariationValue caps the magnitude of a font-variation-settings value.
// Overflow guard, not spec-mandated: CSS Fonts 4, 8.2 allows any number,
// the font clamps it to its axis, and an 'fvar' axis holds 16.16 values,
// below 32768.
const maxVariationValue = 1 << 20

// OpticalSizing is font-optical-sizing (CSS Fonts 4, 8.1).
type OpticalSizing uint8

// The font-optical-sizing values.
const (
	OpticalSizingAuto OpticalSizing = iota
	OpticalSizingNone
)

// parseVariationSettings parses font-variation-settings (CSS Fonts 4,
// 8.2), the property and the @font-face descriptor alike: normal, or a
// comma list of a four-character string tag followed by a number. A tag
// named twice keeps its last value, in the place of its last appearance.
func parseVariationSettings(v []token, _ *declCtx) (any, bool) {
	if k, ok := keyword(v); ok && k == "normal" {
		return []VariationSetting(nil), true
	}
	parts := splitCommas(v)
	if len(parts) > maxFeatureSettings {
		return nil, false
	}
	out := make([]VariationSetting, 0, len(parts))
	for _, part := range parts {
		p := significant(part)
		if len(p) != 2 || p[0].typ != tString || !validFeatureTag(p[0].value) {
			return nil, false
		}
		n, ok := parseNumber(p[1])
		if !ok || math.IsNaN(n) {
			return nil, false
		}
		s := VariationSetting{Tag: p[0].value, Value: min(max(n, -maxVariationValue), maxVariationValue)}
		for i, e := range out {
			if e.Tag == s.Tag {
				out = append(out[:i], out[i+1:]...)
				break
			}
		}
		out = append(out, s)
	}
	return out, len(out) > 0
}

// fontVariationProps returns the longhands of CSS Fonts 4, 8: both
// inherited, font-optical-sizing and font-variation-settings.
func fontVariationProps() map[propID]propDef {
	return map[propID]propDef{
		pFontVariationSettings: {
			name: "font-variation-settings", inherited: true, parse: parseVariationSettings,
			compute: func(_ *styler, st *Style, v any) {
				if l, ok := v.([]VariationSetting); ok {
					st.FontVariationSettings = l
				}
			},
			copyFrom: field(func(s *Style) *[]VariationSetting { return &s.FontVariationSettings }),
		},
		pFontOpticalSizing: enumProp("font-optical-sizing", true, func(s *Style) *OpticalSizing { return &s.FontOpticalSizing }, map[string]OpticalSizing{
			"auto": OpticalSizingAuto, "none": OpticalSizingNone,
		}),
	}
}

// variationInterp interpolates font-variation-settings (CSS Fonts 4, 8.2):
// two lists that set the same axes, in any order, interpolate value by
// value, in the order of the first; two that do not, normal included,
// flip discretely.
func variationInterp(dst, a, b *Style, p float64) interpResult {
	la, lb := a.FontVariationSettings, b.FontVariationSettings
	if len(la) == 0 || len(la) != len(lb) {
		return discrete
	}
	out := make([]VariationSetting, len(la))
	for i, va := range la {
		j := slices.IndexFunc(lb, func(v VariationSetting) bool { return v.Tag == va.Tag })
		if j < 0 {
			return discrete
		}
		out[i] = VariationSetting{Tag: va.Tag, Value: min(max(lerp(va.Value, lb[j].Value, p), -maxVariationValue), maxVariationValue)}
	}
	dst.FontVariationSettings = out
	return interpolated
}
