package formula

import (
	mt "github.com/gotenberg/litho/pkg/internal/typography/mathtable"
)

// constant returns the layout constant c of the first available font f at
// size points (MathML Core, 5.1): the MATH table's value scaled to the
// size, or the fallback 5.1 gives when the font has no MATH table. A nil
// face, a font that loads nowhere, takes the fallbacks of a font of no
// metrics. The three percentages come back as fractions, the scale-downs
// of 0 read as their fallbacks.
func (f *face) constant(c mt.Constant, size float64) float64 {
	if f != nil {
		if v, ok := f.math.Constant(c); ok {
			switch c { //nolint:exhaustive // only the percentages read apart
			case mt.ScriptPercentScaleDown, mt.ScriptScriptPercentScaleDown:
				if v != 0 {
					return float64(v) / 100
				}
			case mt.RadicalDegreeBottomRaisePercent:
				return float64(v) / 100
			default:
				return f.units(float64(v), size)
			}
		}
	}
	return f.fallback(c, size)
}

// units scales v font units of f to size points.
func (f *face) units(v, size float64) float64 {
	if f == nil || f.upem <= 0 {
		return 0
	}
	return float64(v*size) / f.upem
}

// fallback returns the fallback value MathML Core, 5.1 gives the constant
// c of a font without a MATH table, at size points.
func (f *face) fallback(c mt.Constant, size float64) float64 {
	var rule, xHeight, subOffset, superOffset float64
	if f != nil {
		rule = f.units(f.ruleThickness, size)
		xHeight = f.units(f.xHeight, size)
		subOffset = f.units(f.subOffset, size)
		superOffset = f.units(f.superOffset, size)
	}
	switch c { //nolint:exhaustive // the rest fall back to the default fallback constant, 0
	case mt.ScriptPercentScaleDown:
		return 0.71
	case mt.ScriptScriptPercentScaleDown:
		return 0.5041
	case mt.AxisHeight:
		return xHeight / 2
	case mt.AccentBaseHeight:
		return xHeight
	case mt.SubscriptShiftDown:
		return subOffset
	case mt.SubscriptTopMax, mt.SuperscriptBottomMaxWithSubscript:
		return float64(4*xHeight) / 5
	case mt.SuperscriptShiftUp:
		return superOffset
	case mt.SuperscriptBottomMin:
		return xHeight / 4
	case mt.SubSuperscriptGapMin:
		return 4 * rule
	case mt.SpaceAfterScript:
		return size / 24
	case mt.StackGapMin, mt.OverbarVerticalGap, mt.UnderbarVerticalGap, mt.FractionNumDisplayStyleGapMin, mt.FractionDenomDisplayStyleGapMin:
		return 3 * rule
	case mt.StackDisplayStyleGapMin:
		return 7 * rule
	case mt.FractionNumeratorGapMin, mt.FractionRuleThickness, mt.FractionDenominatorGapMin,
		mt.OverbarExtraAscender, mt.UnderbarExtraDescender, mt.RadicalRuleThickness, mt.RadicalExtraAscender:
		return rule
	case mt.RadicalVerticalGap:
		return float64(5*rule) / 4
	case mt.RadicalDisplayStyleVerticalGap:
		return rule + float64(xHeight/4)
	case mt.RadicalKernBeforeDegree:
		return float64(5*size) / 18
	case mt.RadicalKernAfterDegree:
		return float64(-10*size) / 18
	case mt.RadicalDegreeBottomRaisePercent:
		return 0.6
	}
	return 0
}
