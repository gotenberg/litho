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

// propertyName returns the property a lowercased declared name stands
// for: the property a legacy name alias names, else the name itself.
func propertyName(name string) string {
	if p, ok := legacyAliases[name]; ok {
		return p
	}
	return name
}
