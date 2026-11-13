package css

// TextBoxTrim is text-box-trim (CSS Inline 3, 5.1): which sides of a
// block container's first and last lines are trimmed to text-box-edge.
type TextBoxTrim uint8

// The text-box-trim values.
const (
	TrimNone TextBoxTrim = iota
	TrimStart
	TrimEnd
	TrimBoth
)

// TrimEdge is one metric of text-box-edge (CSS Inline 3, 5.2).
type TrimEdge uint8

// The text-box-edge metrics: the text-over or text-under baseline, the
// cap height, the x-height, the alphabetic baseline, and the ideographic
// em box's edge.
const (
	EdgeText TrimEdge = iota
	EdgeCap
	EdgeEx
	EdgeAlphabetic
	EdgeIdeographic
)

// TextBoxEdge is text-box-edge: the over and under metrics a trimmed line
// is cut at. The zero value is auto, which takes text for both, the line
// fit edge being leading (CSS Inline 3, 5.2).
type TextBoxEdge struct {
	Over, Under TrimEdge
}

// overEdges and underEdges are the keywords of each half of
// text-box-edge. ideographic-ink is taken as ideographic: the converter
// measures no ink.
var (
	overEdges = map[string]TrimEdge{
		"text": EdgeText, "cap": EdgeCap, "ex": EdgeEx, "ideographic": EdgeIdeographic, "ideographic-ink": EdgeIdeographic,
	}
	underEdges = map[string]TrimEdge{
		"text": EdgeText, "alphabetic": EdgeAlphabetic, "ideographic": EdgeIdeographic, "ideographic-ink": EdgeIdeographic,
	}
	trimValues = map[string]TextBoxTrim{"none": TrimNone, "trim-start": TrimStart, "trim-end": TrimEnd, "trim-both": TrimBoth}
)

// parseTextBoxEdge parses text-box-edge: auto, or an over metric and an
// optional under metric; one value names both where it can, the under
// side taking text otherwise (CSS Inline 3, 5.2).
func parseTextBoxEdge(v []token, _ *declCtx) (any, bool) {
	if len(v) == 1 && v[0].isIdent("auto") {
		return TextBoxEdge{}, true
	}
	if len(v) == 0 || len(v) > 2 || v[0].typ != tIdent {
		return nil, false
	}
	over, ok := overEdges[lower(v[0].value)]
	if !ok {
		return nil, false
	}
	under, ok := underEdges[lower(v[0].value)]
	if !ok {
		under = EdgeText
	}
	if len(v) == 2 {
		if v[1].typ != tIdent {
			return nil, false
		}
		if under, ok = underEdges[lower(v[1].value)]; !ok {
			return nil, false
		}
	}
	return TextBoxEdge{Over: over, Under: under}, true
}

// textBoxProps returns text-box-trim, not inherited, and text-box-edge,
// inherited (CSS Inline 3, 5.1 and 5.2).
func textBoxProps() map[propID]propDef {
	return map[propID]propDef{
		pTextBoxTrim: enumProp("text-box-trim", false, func(s *Style) *TextBoxTrim { return &s.TextBoxTrim }, trimValues),
		pTextBoxEdge: {name: "text-box-edge", inherited: true, parse: parseTextBoxEdge, compute: func(_ *styler, st *Style, v any) {
			if e, ok := v.(TextBoxEdge); ok {
				st.TextBoxEdge = e
			}
		}, copyFrom: field(func(s *Style) *TextBoxEdge { return &s.TextBoxEdge })},
	}
}

// textBoxShorthand is text-box (CSS Inline 3, 5.3): normal, which is none
// and auto, or text-box-trim and text-box-edge in either order, each
// optional; an edge alone trims both sides.
func textBoxShorthand() shorthandDef {
	return shorthandDef{
		longhands: []propID{pTextBoxTrim, pTextBoxEdge},
		expand: func(v []token, c *declCtx) ([]entry, bool) {
			if len(v) == 1 && v[0].isIdent("normal") {
				return []entry{{pTextBoxTrim, TrimNone}, {pTextBoxEdge, TextBoxEdge{}}}, true
			}
			trim, edge := TrimBoth, TextBoxEdge{}
			var trimSet bool
			var rest []token
			for i, t := range v {
				if k, ok := trimValues[lower(t.value)]; ok && t.typ == tIdent && !trimSet && (i == 0 || i == len(v)-1) {
					trim, trimSet = k, true
					continue
				}
				rest = append(rest, t)
			}
			if len(rest) > 0 {
				e, ok := parseTextBoxEdge(rest, c)
				if !ok {
					return nil, false
				}
				edge = e.(TextBoxEdge)
			} else if !trimSet {
				return nil, false
			}
			return []entry{{pTextBoxTrim, trim}, {pTextBoxEdge, edge}}, true
		},
	}
}
