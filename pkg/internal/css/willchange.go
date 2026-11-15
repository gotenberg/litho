package css

// WillChange is what the properties will-change names make of a box (CSS
// Will Change 1, 3): a property a value other than its initial one of
// which would make the box a stacking context, a containing block or a
// Backdrop Root makes it one when named. Zero is auto.
type WillChange uint8

// The will-change bits.
const (
	// WillStack makes a stacking context.
	WillStack WillChange = 1 << iota
	// WillHoldAbsolute makes the containing block of absolutely
	// positioned descendants.
	WillHoldAbsolute
	// WillHoldFixed makes the containing block of fixed positioned
	// descendants.
	WillHoldFixed
	// WillBackdropRoot makes a Backdrop Root (Filter Effects 2, 3).
	WillBackdropRoot
)

// willChangeEffects maps each property whose non-initial values make a
// box a stacking context (CSS 2.1, 9.9.1; CSS Transforms 1 and 2; CSS
// Color 4, 15; Filter Effects 1 and 2; CSS Compositing 1, 3.2 and 3.3;
// CSS Masking 1; CSS Contain 2, 3; CSS Position 3, 3.4), a containing
// block (CSS 2.1, 10.1; CSS Transforms 1, 2; Filter Effects 1, 3 and 2,
// 2.1; CSS Contain 2, 3.2 and 3.5) or a Backdrop Root (Filter Effects 2,
// 3) to what naming it in will-change makes. Immutable lookup table.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 holds absolute
//     and fixed children in a box of will-change: transform, translate,
//     rotate, filter, backdrop-filter, perspective or contain, absolute
//     ones alone under will-change: position, none under opacity, top
//     and left, scroll-position or contents; paints a z-index: -1 child
//     over the background of a box of will-change: opacity, under that of
//     a box of will-change: z-index.
var willChangeEffects = map[string]WillChange{
	"transform":       WillStack | WillHoldAbsolute | WillHoldFixed,
	"translate":       WillStack | WillHoldAbsolute | WillHoldFixed,
	"rotate":          WillStack | WillHoldAbsolute | WillHoldFixed,
	"scale":           WillStack | WillHoldAbsolute | WillHoldFixed,
	"perspective":     WillStack | WillHoldAbsolute | WillHoldFixed,
	"contain":         WillStack | WillHoldAbsolute | WillHoldFixed,
	"filter":          WillStack | WillHoldAbsolute | WillHoldFixed | WillBackdropRoot,
	"backdrop-filter": WillStack | WillHoldAbsolute | WillHoldFixed | WillBackdropRoot,
	"position":        WillStack | WillHoldAbsolute,
	"opacity":         WillStack | WillBackdropRoot,
	"mix-blend-mode":  WillStack | WillBackdropRoot,
	"clip-path":       WillStack | WillBackdropRoot,
	"mask":            WillStack | WillBackdropRoot,
	"mask-image":      WillStack | WillBackdropRoot,
	"mask-border":     WillStack | WillBackdropRoot,
	"isolation":       WillStack,
}

// willChangeProp is the will-change property: auto | <animateable-feature>#,
// where <animateable-feature> = scroll-position | contents |
// <custom-ident> (CSS Will Change 1, 3).
func willChangeProp() propDef {
	return propDef{
		name: "will-change", parse: parseWillChange,
		compute: func(_ *styler, st *Style, v any) {
			if w, ok := v.(WillChange); ok {
				st.WillChange = w
			}
		},
		copyFrom: field(func(s *Style) *WillChange { return &s.WillChange }),
	}
}

// parseWillChange parses will-change. A <custom-ident> names a property,
// its legacy aliases included; one litho does not know names nothing and
// keeps the declaration valid, as the clause has it. will-change, none,
// all, auto, scroll-position and contents are no <custom-ident> there.
func parseWillChange(v []token, _ *declCtx) (any, bool) {
	if k, ok := keyword(v); ok && k == "auto" {
		return WillChange(0), true
	}
	var w WillChange
	for _, item := range splitCommas(v) {
		item = significant(item)
		if len(item) != 1 || item[0].typ != tIdent {
			return nil, false
		}
		switch k := lower(item[0].value); k {
		case "scroll-position", "contents":
		case "will-change", "none", "all", "auto":
			return nil, false
		default:
			if !customIdent(k) {
				return nil, false
			}
			w |= willChangeEffects[propertyName(k)]
		}
	}
	return w, true
}
