package css

// ClipBox is the visual box of overflow-clip-margin (CSS Overflow 4,
// 3.2): the edge the overflow clip starts from. The zero value is the
// padding box, the initial value.
type ClipBox uint8

// The visual boxes.
const (
	ClipPaddingBox ClipBox = iota
	ClipBorderBox
	ClipContentBox
)

// ClipMargin is the overflow-clip-margin property (CSS Overflow 4, 3.2):
// how far past its Box edge, in points, a box that clips its overflow
// clips it.
type ClipMargin struct {
	Box    ClipBox
	Margin float64
}

// clipMarginSpec is the specified value of overflow-clip-margin.
type clipMarginSpec struct {
	box ClipBox
	l   length
}

// clipBoxes maps the <visual-box> keywords to their box.
var clipBoxes = map[string]ClipBox{"padding-box": ClipPaddingBox, "border-box": ClipBorderBox, "content-box": ClipContentBox}

// parseClipMargin parses overflow-clip-margin: <visual-box> || <length
// [0,∞]>, each at most once.
func parseClipMargin(v []token, _ *declCtx) (any, bool) {
	if len(v) < 1 || len(v) > 2 {
		return nil, false
	}
	var out clipMarginSpec
	seenBox, seenLen := false, false
	for _, t := range v {
		if t.typ == tIdent {
			b, ok := clipBoxes[lower(t.value)]
			if !ok || seenBox {
				return nil, false
			}
			out.box, seenBox = b, true
			continue
		}
		l, ok := parseLength(t, nonNeg)
		if !ok || seenLen {
			return nil, false
		}
		out.l, seenLen = l, true
	}
	return out, true
}

// overflowClipProps defines overflow-clip-margin, which is not inherited.
func overflowClipProps() map[propID]propDef {
	return map[propID]propDef{
		pOverflowClipMargin: {
			name:  "overflow-clip-margin",
			parse: parseClipMargin,
			compute: func(s *styler, st *Style, v any) {
				if c, ok := v.(clipMarginSpec); ok {
					st.OverflowClipMargin = ClipMargin{Box: c.box, Margin: max(c.l.dim(s.lc, true).Value, 0)}
				}
			},
			copyFrom: field(func(s *Style) *ClipMargin { return &s.OverflowClipMargin }),
		},
	}
}
