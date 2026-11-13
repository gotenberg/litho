package css

import (
	"math"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/warn"
)

// FilterKind is one function of a filter list (Filter Effects 1, 13.1), or
// a reference to an SVG filter element.
type FilterKind uint8

// The filter functions.
const (
	// FilterURL references a filter element by [Filter.URL].
	FilterURL FilterKind = iota
	FilterBlur
	FilterBrightness
	FilterContrast
	FilterDropShadow
	FilterGrayscale
	FilterHueRotate
	FilterInvert
	FilterOpacity
	FilterSaturate
	FilterSepia
)

// Filter is one entry of a filter or backdrop-filter list (Filter Effects
// 1, 6; Filter Effects 2, 3).
type Filter struct {
	Kind FilterKind
	// Amount is the function's argument: the blur radius in points for
	// blur, the angle in degrees for hue-rotate, else a number, a
	// percentage divided by 100. grayscale, invert, opacity and sepia hold
	// it within [0, 1] (Filter Effects 1, 13.1 clamps them there); none is
	// negative.
	Amount float64
	// Shadow is drop-shadow's offsets, blur radius and colour, with no
	// spread and not inset.
	Shadow Shadow
	// URL is the resolved reference of FilterURL.
	URL string
}

// maxFilters caps the functions of one filter list: 32. Anti-DoS guard,
// not spec-mandated: each function is a pass over the element's bitmap,
// and real designs chain a handful; later ones are dropped with a warning.
const maxFilters = 32

// filterSpec is one specified filter function.
type filterSpec struct {
	kind   FilterKind
	amount float64
	blur   length
	shadow shadowSpec
	url    string
}

// filterFns are the filter functions of Filter Effects 1, 13.1 by name.
var filterFns = map[string]FilterKind{
	"blur": FilterBlur, "brightness": FilterBrightness, "contrast": FilterContrast,
	"drop-shadow": FilterDropShadow, "grayscale": FilterGrayscale, "hue-rotate": FilterHueRotate,
	"invert": FilterInvert, "opacity": FilterOpacity, "saturate": FilterSaturate, "sepia": FilterSepia,
}

// parseFilter parses filter or backdrop-filter (Filter Effects 1, 6;
// Filter Effects 2, 3): none, or a space-separated list of filter
// functions and url() references.
func parseFilter(v []token, c *declCtx) (any, bool) {
	if k, ok := keyword(v); ok && k == "none" {
		return []filterSpec(nil), true
	}
	if len(v) == 0 {
		return nil, false
	}
	out := make([]filterSpec, 0, min(len(v), maxFilters))
	for _, t := range v {
		var f filterSpec
		if u, ok := parseURL(t, c.baseURL); ok {
			f = filterSpec{kind: FilterURL, url: u}
		} else if f, ok = parseFilterFn(t); !ok {
			return nil, false
		}
		out = append(out, f)
	}
	if len(out) > maxFilters {
		c.warn(warn.CSSUnsupported, warn.Approximated, "filter: only the first %d functions are applied", maxFilters)
		out = out[:maxFilters]
	}
	return out, true
}

// parseFilterFn parses one <filter-function> (Filter Effects 1, 13.1):
// each takes an optional argument, none negative.
func parseFilterFn(t token) (filterSpec, bool) {
	if t.typ != tFunction {
		return filterSpec{}, false
	}
	kind, ok := filterFns[lower(t.value)]
	if !ok {
		return filterSpec{}, false
	}
	args := significant(t.children)
	f := filterSpec{kind: kind, amount: 1}
	switch kind { //nolint:exhaustive // FilterURL is no function
	case FilterBlur:
		if len(args) == 0 {
			f.blur = length{hasLen: true}
			return f, true
		}
		l, ok := parseLength(args[0], nonNeg)
		if !ok || len(args) != 1 || l.hasPct {
			return f, false
		}
		f.blur = l
		return f, true
	case FilterHueRotate:
		f.amount = 0
		if len(args) == 0 {
			return f, true
		}
		if len(args) != 1 || args[0].typ == tNumber && args[0].num != 0 {
			// A bare number is an angle only when it is zero.
			return f, false
		}
		a, ok := parseAngle(args[0])
		f.amount = a
		return f, ok
	case FilterDropShadow:
		s, ok := parseShadow(args)
		if !ok || s.inset || s.n > 3 {
			return f, false
		}
		f.shadow = s
		return f, true
	}
	if len(args) == 0 {
		return f, true
	}
	if len(args) != 1 {
		return f, false
	}
	switch args[0].typ { //nolint:exhaustive // the other token types are invalid here
	case tPercentage:
		f.amount = args[0].num / 100
	default:
		n, ok := parseNumber(args[0])
		if !ok {
			return f, false
		}
		f.amount = n
	}
	if !(f.amount >= 0) || math.IsInf(f.amount, 0) {
		return f, false
	}
	return f, true
}

// computeFilters computes a filter list: lengths in points, a
// drop-shadow's currentColor resolved, the amounts of grayscale, invert,
// opacity and sepia clamped to 1 (Filter Effects 1, 13.1).
func computeFilters(s *styler, st *Style, specs []filterSpec) []Filter {
	if len(specs) == 0 {
		return nil
	}
	out := make([]Filter, len(specs))
	for i, sp := range specs {
		f := Filter{Kind: sp.kind, Amount: sp.amount, URL: sp.url}
		switch sp.kind { //nolint:exhaustive // the others keep their amount
		case FilterBlur:
			f.Amount = math.Max(sp.blur.points(s.lc), 0)
		case FilterGrayscale, FilterInvert, FilterOpacity, FilterSepia:
			f.Amount = math.Min(f.Amount, 1)
		case FilterDropShadow:
			var d [3]float64
			for k := range sp.shadow.n {
				d[k] = sp.shadow.lens[k].points(s.lc)
			}
			f.Shadow = Shadow{X: d[0], Y: d[1], Blur: math.Max(d[2], 0), Color: sp.shadow.color.resolve(st.Color)}
			f.Shadow.current = sp.shadow.color.current
		}
		out[i] = f
	}
	return out
}

// copyFilters copies a filter list, resolving again a currentColor
// drop-shadow against dst's color.
func copyFilters(dst *[]Filter, src []Filter, color Color) {
	*dst = src
	copied := false
	for i, f := range src {
		if f.Kind != FilterDropShadow || !f.Shadow.current || f.Shadow.Color == color {
			continue
		}
		if !copied {
			*dst = slices.Clone(src)
			copied = true
		}
		(*dst)[i].Shadow.Color = color
	}
}

// filterProps are filter (Filter Effects 1, 6) and backdrop-filter
// (Filter Effects 2, 3).
func filterProps() map[propID]propDef {
	def := func(name string, f func(*Style) *[]Filter) propDef {
		return propDef{name: name, parse: parseFilter, compute: func(s *styler, st *Style, v any) {
			if specs, ok := v.([]filterSpec); ok {
				*f(st) = computeFilters(s, st, specs)
			}
		}, copyFrom: func(dst, src *Style) { copyFilters(f(dst), *f(src), dst.Color) }}
	}
	return map[propID]propDef{
		pFilter:         def("filter", func(s *Style) *[]Filter { return &s.Filter }),
		pBackdropFilter: def("backdrop-filter", func(s *Style) *[]Filter { return &s.BackdropFilter }),
	}
}
