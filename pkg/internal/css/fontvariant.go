package css

import (
	"strings"

	"github.com/gotenberg/litho/pkg/internal/warn"
)

// fontVariantGroups numbers the components of the font-variant shorthand
// (CSS Fonts 4, 6.11) by keyword: each component appears at most once
// (the || combinator). A group of capsGroup is a value of
// font-variant-caps; every other group belongs to a font-variant-*
// longhand litho does not have (ligatures, alternates, numeric,
// east-asian, position, emoji).
var fontVariantGroups = map[string]int{
	"common-ligatures": 1, "no-common-ligatures": 1,
	"discretionary-ligatures": 2, "no-discretionary-ligatures": 2,
	"historical-ligatures": 3, "no-historical-ligatures": 3,
	"contextual": 4, "no-contextual": 4,
	"small-caps": capsGroup, "all-small-caps": capsGroup, "petite-caps": capsGroup,
	"all-petite-caps": capsGroup, "unicase": capsGroup, "titling-caps": capsGroup,
	"historical-forms": 6,
	"lining-nums":      7, "oldstyle-nums": 7,
	"proportional-nums": 8, "tabular-nums": 8,
	"diagonal-fractions": 9, "stacked-fractions": 9,
	"ordinal":      10,
	"slashed-zero": 11,
	"jis78":        12, "jis83": 12, "jis90": 12, "jis04": 12, "simplified": 12, "traditional": 12,
	"full-width": 13, "proportional-width": 13,
	"ruby": 14,
	"sub":  15, "super": 15,
	"text": 16, "emoji": 16, "unicode": 16,
}

// capsGroup is the group of the font-variant-caps keywords.
const capsGroup = 5

// fontVariantFunctions numbers the functional components of font-variant
// (the font-variant-alternates values of CSS Fonts 4, 6.8) and says
// whether each takes a list of names or one.
var fontVariantFunctions = map[string]struct {
	group int
	list  bool
}{
	"stylistic": {17, false}, "styleset": {18, true}, "character-variant": {19, true},
	"swash": {20, false}, "ornaments": {21, false}, "annotation": {22, false},
}

// expandFontVariant expands the font-variant shorthand (CSS Fonts 4,
// 6.11): normal, none, or any of the values of its subproperties, each
// at most once. litho has font-variant-caps alone of the subproperties,
// with the values its longhand parses; the value sets it and resets it
// when left out. Any other component is valid and warned of as not
// supported, and none, which sets font-variant-ligatures, too.
func expandFontVariant(v []token, c *declCtx) ([]entry, bool) {
	if k, ok := keyword(v); ok && (k == "normal" || k == "none") {
		if k == "none" {
			c.warn(warn.CSSUnsupported, warn.Approximated, "font-variant: none sets font-variant-ligatures, which is not supported; it was ignored")
		}
		return []entry{{pFontVariant, wideInitial}}, true
	}
	var caps any = wideInitial
	seen := map[int]bool{}
	var ignored []string
	for _, t := range v {
		var g int
		switch t.typ { //nolint:exhaustive // any other token is invalid here
		case tIdent:
			var ok bool
			if g, ok = fontVariantGroups[lower(t.value)]; !ok {
				return nil, false
			}
		case tFunction:
			f, ok := fontVariantFunctions[lower(t.value)]
			if !ok || !featureValueNames(t.children, f.list) {
				return nil, false
			}
			g = f.group
		default:
			return nil, false
		}
		if seen[g] {
			return nil, false
		}
		seen[g] = true
		if g == capsGroup {
			if pv, ok := props[pFontVariant].parse([]token{t}, c); ok {
				caps = pv
				continue
			}
		}
		name := lower(t.value)
		if t.typ == tFunction {
			name += "()"
		}
		ignored = append(ignored, name)
	}
	if len(ignored) > 0 {
		c.warn(warn.CSSUnsupported, warn.Approximated, "font-variant: %s is not supported and was ignored", strings.Join(ignored, " "))
	}
	return []entry{{pFontVariant, caps}}, true
}

// featureValueNames reports whether the arguments of a font-variant
// function are <font-feature-value-name>s, identifiers (CSS Fonts 4,
// 6.8): one, or a comma-separated list when list is set.
func featureValueNames(args []token, list bool) bool {
	parts := splitCommas(trimWS(args))
	if len(parts) == 0 || (!list && len(parts) > 1) {
		return false
	}
	for _, p := range parts {
		p = significant(p)
		if len(p) != 1 || p[0].typ != tIdent {
			return false
		}
	}
	return true
}
