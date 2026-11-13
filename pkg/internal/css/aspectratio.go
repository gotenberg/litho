package css

import "github.com/gotenberg/litho/pkg/internal/numeric"

// AspectRatio is the aspect-ratio property (CSS Sizing 4, 4.1): a box's
// preferred aspect ratio, width over height. The zero value is auto: a
// replaced element keeps its natural ratio, any other box has none.
//
// A degenerate ratio, one whose width or height is 0, computes to auto,
// as the property then behaves (CSS Sizing 4, 4.1); the computed value
// keeps the ratio alone, not the pair of numbers it was written as.
type AspectRatio struct {
	// v is 0 for auto, the ratio for <ratio>, and minus the ratio for
	// auto && <ratio>: one field keeps the property as large as a
	// pointer to a rarely set value would.
	v float64
}

// Ratio returns the specified ratio, width over height, 0 for auto.
func (a AspectRatio) Ratio() float64 {
	if a.v < 0 {
		return -a.v
	}
	return a.v
}

// Auto reports auto && <ratio>: a replaced element with a natural ratio
// keeps it, and the ratio sizes the content box whatever box-sizing
// says (CSS Sizing 4, 4.1). It is false for auto alone.
func (a AspectRatio) Auto() bool { return a.v < 0 }

// Ratio returns the aspect-ratio of width over height, auto && <ratio>
// when auto is set.
func Ratio(width, height float64, auto bool) AspectRatio {
	if !numeric.Positive(width) || !numeric.Positive(height) || !numeric.Finite(width/height) {
		return AspectRatio{}
	}
	if auto {
		return AspectRatio{v: -width / height}
	}
	return AspectRatio{v: width / height}
}

// aspectRatioProps returns the aspect-ratio property, not inherited.
func aspectRatioProps() map[propID]propDef {
	return map[propID]propDef{
		pAspectRatio: {name: "aspect-ratio", parse: parseAspectRatio, compute: func(_ *styler, st *Style, v any) {
			if a, ok := v.(AspectRatio); ok {
				st.AspectRatio = a
			}
		}, copyFrom: field(func(s *Style) *AspectRatio { return &s.AspectRatio })},
	}
}

// parseAspectRatio parses aspect-ratio (CSS Sizing 4, 4.1): auto || <ratio>,
// a <ratio> being a non-negative number, optionally followed by a slash
// and a second one (CSS Values 4, 5.7), math functions included.
func parseAspectRatio(v []token, _ *declCtx) (any, bool) {
	auto := false
	if len(v) > 0 && v[0].typ == tIdent && eqFold(v[0].value, "auto") {
		auto, v = true, v[1:]
	} else if n := len(v); n > 0 && v[n-1].typ == tIdent && eqFold(v[n-1].value, "auto") {
		auto, v = true, v[:n-1]
	}
	if len(v) == 0 {
		return AspectRatio{}, auto
	}
	num := func(t token) (float64, bool) {
		n, ok := parseNumber(t)
		return n, ok && n >= 0
	}
	w, ok := num(v[0])
	if !ok {
		return nil, false
	}
	h := 1.0
	switch {
	case len(v) == 1:
	case len(v) == 3 && v[1].isDelim('/'):
		if h, ok = num(v[2]); !ok {
			return nil, false
		}
	default:
		return nil, false
	}
	return Ratio(w, h, auto), true
}
