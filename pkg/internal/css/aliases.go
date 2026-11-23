package css

// legacyAliases maps each legacy name alias to the property it names. A
// legacy name alias is converted into the property it names at parse time
// (CSS Cascade 5, 3.1): it parses, cascades and warns as that property,
// and a later declaration of either name wins over an earlier one.
//
// The table is the Compatibility Standard's, 3.4.1, with the aliases CSS
// Basic User Interface 4 (-webkit-appearance), CSS Gaps 1 (grid-gap,
// grid-row-gap, grid-column-gap) and CSS Text 4 (word-wrap) define. It
// leaves out three of the aliases the standards define, whose names
// Chromium reads as properties of their own:
//
//   - font-stretch, which CSS Fonts 4, 2.3.1 makes an alias of font-width:
//     Chromium parses no font-width, and litho keeps font-stretch as the
//     property.
//   - -webkit-mask-box-image and its five longhands, which the
//     Compatibility Standard makes aliases of mask-border: Chromium parses
//     no mask-border, and its -webkit-mask-box-image slices with fill when
//     the value leaves fill out, where mask-border-slice does not.
//
// Two aliases keep the standard's reading where Chromium reads one form
// otherwise: -webkit-border-radius with two values sets each corner's
// horizontal and vertical radii in Chromium, and -webkit-background-size
// with one value sets both sizes in Chromium. litho reads both as
// border-radius and background-size read them.
var legacyAliases = map[string]string{
	"-webkit-align-content":              "align-content",
	"-webkit-align-items":                "align-items",
	"-webkit-align-self":                 "align-self",
	"-webkit-animation":                  "animation",
	"-webkit-animation-delay":            "animation-delay",
	"-webkit-animation-direction":        "animation-direction",
	"-webkit-animation-duration":         "animation-duration",
	"-webkit-animation-fill-mode":        "animation-fill-mode",
	"-webkit-animation-iteration-count":  "animation-iteration-count",
	"-webkit-animation-name":             "animation-name",
	"-webkit-animation-play-state":       "animation-play-state",
	"-webkit-animation-timing-function":  "animation-timing-function",
	"-webkit-appearance":                 "appearance",
	"-webkit-backface-visibility":        "backface-visibility",
	"-webkit-background-clip":            "background-clip",
	"-webkit-background-origin":          "background-origin",
	"-webkit-background-size":            "background-size",
	"-webkit-border-bottom-left-radius":  "border-bottom-left-radius",
	"-webkit-border-bottom-right-radius": "border-bottom-right-radius",
	"-webkit-border-radius":              "border-radius",
	"-webkit-border-top-left-radius":     "border-top-left-radius",
	"-webkit-border-top-right-radius":    "border-top-right-radius",
	"-webkit-box-shadow":                 "box-shadow",
	"-webkit-box-sizing":                 "box-sizing",
	"-webkit-filter":                     "filter",
	"-webkit-flex":                       "flex",
	"-webkit-flex-basis":                 "flex-basis",
	"-webkit-flex-direction":             "flex-direction",
	"-webkit-flex-flow":                  "flex-flow",
	"-webkit-flex-grow":                  "flex-grow",
	"-webkit-flex-shrink":                "flex-shrink",
	"-webkit-flex-wrap":                  "flex-wrap",
	"-webkit-justify-content":            "justify-content",
	"-webkit-mask":                       "mask",
	"-webkit-mask-clip":                  "mask-clip",
	"-webkit-mask-composite":             "mask-composite",
	"-webkit-mask-image":                 "mask-image",
	"-webkit-mask-origin":                "mask-origin",
	"-webkit-mask-position":              "mask-position",
	"-webkit-mask-repeat":                "mask-repeat",
	"-webkit-mask-size":                  "mask-size",
	"-webkit-order":                      "order",
	"-webkit-perspective":                "perspective",
	"-webkit-perspective-origin":         "perspective-origin",
	"-webkit-text-size-adjust":           "text-size-adjust",
	"-webkit-transform":                  "transform",
	"-webkit-transform-origin":           "transform-origin",
	"-webkit-transform-style":            "transform-style",
	"-webkit-transition":                 "transition",
	"-webkit-transition-delay":           "transition-delay",
	"-webkit-transition-duration":        "transition-duration",
	"-webkit-transition-property":        "transition-property",
	"-webkit-transition-timing-function": "transition-timing-function",
	"grid-column-gap":                    "column-gap",
	"grid-gap":                           "gap",
	"grid-row-gap":                       "row-gap",
	"word-wrap":                          "overflow-wrap",
}

// chromiumAliases maps the prefixed names Chromium reads as another
// property, and no standard lists, to that property. They resolve as the
// standard's aliases do (CSS Cascade 5, 3.1): each parses, cascades and
// warns as the property it names, and the later of two names wins. The
// table holds the names Chromium declares as aliases of a standard
// property and lays out as it: the multi-column properties, the margins,
// paddings and borders named before, after, start and end (the block-start,
// block-end, inline-start and inline-end sides), the logical sizes, text
// emphasis, shapes, clip-path, opacity, font-feature-settings,
// hyphenate-character, writing-mode, text-orientation and user-select.
// The prefixed names Chromium reads with values or effects of their own
// (-webkit-box-*, -webkit-text-combine, -webkit-ruby-position,
// -webkit-border-image, the origin components) are not in it.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 prints each name of the table as the
//     property it names (columns, rules and gaps; margins, paddings and
//     borders on the logical sides; logical widths and heights with their
//     bounds; emphasis marks; vertical writing and upright orientation;
//     opacity, clip-path and shape-outside; the hyphenation character).
//     Every name parses (CSS.supports true); getComputedStyle lists
//     -webkit-writing-mode and -webkit-text-orientation beside the
//     standard names and none of the others.
var chromiumAliases = map[string]string{
	"-webkit-column-count":           "column-count",
	"-webkit-column-gap":             "column-gap",
	"-webkit-column-rule":            "column-rule",
	"-webkit-column-rule-color":      "column-rule-color",
	"-webkit-column-rule-style":      "column-rule-style",
	"-webkit-column-rule-width":      "column-rule-width",
	"-webkit-column-span":            "column-span",
	"-webkit-column-width":           "column-width",
	"-webkit-columns":                "columns",
	"-webkit-margin-before":          "margin-block-start",
	"-webkit-margin-after":           "margin-block-end",
	"-webkit-margin-start":           "margin-inline-start",
	"-webkit-margin-end":             "margin-inline-end",
	"-webkit-padding-before":         "padding-block-start",
	"-webkit-padding-after":          "padding-block-end",
	"-webkit-padding-start":          "padding-inline-start",
	"-webkit-padding-end":            "padding-inline-end",
	"-webkit-border-before":          "border-block-start",
	"-webkit-border-before-color":    "border-block-start-color",
	"-webkit-border-before-style":    "border-block-start-style",
	"-webkit-border-before-width":    "border-block-start-width",
	"-webkit-border-after":           "border-block-end",
	"-webkit-border-after-color":     "border-block-end-color",
	"-webkit-border-after-style":     "border-block-end-style",
	"-webkit-border-after-width":     "border-block-end-width",
	"-webkit-border-start":           "border-inline-start",
	"-webkit-border-start-color":     "border-inline-start-color",
	"-webkit-border-start-style":     "border-inline-start-style",
	"-webkit-border-start-width":     "border-inline-start-width",
	"-webkit-border-end":             "border-inline-end",
	"-webkit-border-end-color":       "border-inline-end-color",
	"-webkit-border-end-style":       "border-inline-end-style",
	"-webkit-border-end-width":       "border-inline-end-width",
	"-webkit-logical-width":          "inline-size",
	"-webkit-logical-height":         "block-size",
	"-webkit-min-logical-width":      "min-inline-size",
	"-webkit-min-logical-height":     "min-block-size",
	"-webkit-max-logical-width":      "max-inline-size",
	"-webkit-max-logical-height":     "max-block-size",
	"-webkit-text-emphasis":          "text-emphasis",
	"-webkit-text-emphasis-color":    "text-emphasis-color",
	"-webkit-text-emphasis-position": "text-emphasis-position",
	"-webkit-text-emphasis-style":    "text-emphasis-style",
	"-webkit-shape-outside":          "shape-outside",
	"-webkit-shape-margin":           "shape-margin",
	"-webkit-shape-image-threshold":  "shape-image-threshold",
	"-webkit-clip-path":              "clip-path",
	"-webkit-opacity":                "opacity",
	"-webkit-font-feature-settings":  "font-feature-settings",
	"-webkit-hyphenate-character":    "hyphenate-character",
	"-webkit-writing-mode":           "writing-mode",
	"-webkit-text-orientation":       "text-orientation",
	"-webkit-user-select":            "user-select",
}

// propertyName returns the property a lowercased declared name stands
// for: the property a legacy name alias or a Chromium alias names, else
// the name itself.
func propertyName(name string) string {
	if p, ok := legacyAliases[name]; ok {
		return p
	}
	if p, ok := chromiumAliases[name]; ok {
		return p
	}
	return name
}
