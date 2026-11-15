package css

// colorScheme is a specified color-scheme (CSS Color Adjustment 1, 2.1):
// whether the list names light and dark, and only. normal is the zero
// value.
type colorScheme struct {
	light, dark, only bool
}

// colorSchemeProp is the color-scheme property: normal | [ light | dark |
// <custom-ident> ]+ && only?, inherited, its used scheme held as
// [Style.DarkScheme].
func colorSchemeProp() propDef {
	return propDef{
		name: "color-scheme", inherited: true, parse: parseColorScheme,
		compute: func(s *styler, st *Style, v any) {
			if c, ok := v.(colorScheme); ok {
				st.DarkScheme = c.usedDark(s.prefersDark)
			}
		},
		copyFrom: field(func(s *Style) *bool { return &s.DarkScheme }),
	}
}

// parseColorScheme parses color-scheme. An ident other than light, dark,
// only and normal names a scheme litho does not know, which is ignored,
// as the clause asks; only appears at most once, first or last.
func parseColorScheme(v []token, _ *declCtx) (any, bool) {
	if k, ok := keyword(v); ok && k == "normal" {
		return colorScheme{}, true
	}
	var c colorScheme
	names := 0
	for i, t := range v {
		if t.typ != tIdent {
			return nil, false
		}
		switch k := lower(t.value); k {
		case "only":
			if c.only || i != 0 && i != len(v)-1 {
				return nil, false
			}
			c.only = true
			continue
		case "light":
			c.light = true
		case "dark":
			c.dark = true
		case "normal":
			return nil, false
		default:
			if !customIdent(k) {
				return nil, false
			}
		}
		names++
	}
	return c, names > 0
}

// usedDark returns whether the used colour scheme is dark (CSS Color
// Adjustment 1, 2.1): the preferred scheme when the list supports it,
// else the first of light and dark it names, else light, the scheme of a
// page that supports none.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 prints dark
//     under color-scheme: dark, only dark and foo dark, and light under
//     light dark and dark light: its print prefers light, also with the
//     route's emulatedMediaFeatures prefers-color-scheme: dark, under which
//     @media (prefers-color-scheme: dark) does not match either.
func (c colorScheme) usedDark(prefersDark bool) bool {
	if prefersDark {
		return c.dark
	}
	return c.dark && !c.light
}

// prefersDark reports a medium whose prefers-color-scheme is dark.
func (m Media) prefersDark() bool {
	v, _ := discreteFeature("prefers-color-scheme", m)
	return v == "dark"
}

// CanvasColor returns the Canvas system colour (CSS Color 4, 6.2) under a
// light used colour scheme, or under a dark one when dark is set: the
// colour of the canvas surface a root of that scheme paints under its
// content (CSS Color Adjustment 1, 2.2).
func CanvasColor(dark bool) Color {
	c := systemColors["canvas"]
	if dark {
		c = darkSystemColors["canvas"]
	}
	return Color{R: float64(c[0]) / 255, G: float64(c[1]) / 255, B: float64(c[2]) / 255, A: float64(c[3]) / 255}
}
