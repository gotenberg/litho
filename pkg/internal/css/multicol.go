package css

// This file defines the properties of CSS Multi-column Layout Module
// Level 1 (column-count, column-width, columns, column-rule-*, column-rule,
// column-span, column-fill) and the column-height and column-wrap of
// Level 2, which make the rows of columns wrap. column-gap is shared with
// flexible boxes (flex.go): its normal value is 1em in a multi-column
// container (CSS Multicol 1, 4.1), which the layout resolves.

// Columns is the computed multi-column style of an element (CSS Multicol
// 1, 3, 4, 5 and 6). The zero value is the initial one but for RuleWidth,
// which the cascade computes to medium.
type Columns struct {
	// Count is the column-count, 0 for auto (CSS Multicol 1, 3.2).
	Count int
	// Width is the column-width: Auto or Points (CSS Multicol 1, 3.1).
	Width Dimension
	// RuleWidth is the column-rule-width in points, snapped as a border
	// width; the used width is 0 when RuleStyle is BorderNone or
	// BorderHidden (CSS Multicol 1, 4.4).
	RuleWidth float64
	// RuleStyle and RuleColor are the column-rule-style and
	// column-rule-color (CSS Multicol 1, 4.2 and 4.3).
	RuleStyle BorderStyle
	RuleColor Color
	// ruleCurrent reports a column-rule-color of currentColor, the
	// initial value, which resolves against the element's own color.
	ruleCurrent bool
	// SpanAll reports column-span: all (CSS Multicol 1, 5.1).
	SpanAll bool
	// Fill is the column-fill (CSS Multicol 1, 6.1).
	Fill ColumnFill
	// Height is the column-height: Auto or Points (CSS Multicol 2, 3.3).
	Height Dimension
	// wrap is the column-wrap (CSS Multicol 2, 3.4), read through
	// [Columns.Wrap].
	wrap columnWrap
}

// Wrap reports column-wrap: wrap, which starts a new row of columns below
// a full one instead of an overflow column; auto behaves as wrap when the
// column-height is a length, as nowrap otherwise (CSS Multicol 2, 3.4).
func (c *Columns) Wrap() bool {
	return c.wrap == wrapYes || c.wrap == wrapAuto && c.Height.Kind == Points
}

// ColumnFill is the column-fill property (CSS Multicol 1, 6.1).
type ColumnFill uint8

// The column-fill values.
const (
	// FillBalance balances the content across the columns, the initial
	// value.
	FillBalance ColumnFill = iota
	// FillAuto fills the columns one after the other.
	FillAuto
	// FillBalanceAll balances every fragment of the container.
	FillBalanceAll
)

// The column break values (CSS Fragmentation 3, 3.1 and 3.2), appended to
// the page ones of style.go.
const (
	// Column forces a column break.
	Column Break = RightPage + 1 + iota
	// BreakAvoidColumn avoids a column break.
	BreakAvoidColumn
)

// InsideAvoidColumn avoids column breaks inside the box (CSS Fragmentation
// 3, 3.2).
const InsideAvoidColumn = InsideAvoidPage + 1

// Multicol reports a style that makes a block container a multi-column
// container: a column-count or a column-width other than auto (CSS
// Multicol 1, 2).
func (c *Columns) Multicol() bool {
	return c.Count > 0 || c.Width.Kind == Points
}

// columnFills are the column-fill keywords.
var columnFills = map[string]ColumnFill{"auto": FillAuto, "balance": FillBalance, "balance-all": FillBalanceAll}

// columnAuto is the specified auto keyword of column-count, column-width
// and column-height.
type columnAuto struct{}

// parseColumnCount parses column-count: auto or a positive integer.
func parseColumnCount(v []token, _ *declCtx) (any, bool) {
	t, ok := single(v)
	if !ok {
		return nil, false
	}
	if t.typ == tIdent && lower(t.value) == "auto" {
		return columnAuto{}, true
	}
	n, ok := parseInteger(t)
	if !ok || n < 1 {
		return nil, false
	}
	return n, true
}

// parseColumnLength parses column-width or column-height: auto or a
// non-negative length, no percentage.
func parseColumnLength(v []token, _ *declCtx) (any, bool) {
	t, ok := single(v)
	if !ok {
		return nil, false
	}
	if t.typ == tIdent && lower(t.value) == "auto" {
		return columnAuto{}, true
	}
	l, ok := parseLength(t, nonNeg)
	if !ok {
		return nil, false
	}
	return l, true
}

// columnLength computes a parsed column-width or column-height.
func columnLength(s *styler, v any) (Dimension, bool) {
	switch x := v.(type) {
	case columnAuto:
		return Dimension{Kind: Auto}, true
	case length:
		return x.dim(s.lc, true), true
	}
	return Dimension{}, false
}

// columnWrap is a specified column-wrap value: auto, nowrap or wrap.
type columnWrap uint8

const (
	wrapAuto columnWrap = iota
	wrapNo
	wrapYes
)

// multicolProps returns the longhands of multi-column layout.
func multicolProps() map[propID]propDef {
	return map[propID]propDef{
		pColumnCount: {
			name: "column-count", parse: parseColumnCount,
			compute: func(_ *styler, st *Style, v any) {
				switch n := v.(type) {
				case columnAuto:
					st.Columns.Count = 0
				case int:
					st.Columns.Count = n
				}
			},
			copyFrom: field(func(s *Style) *int { return &s.Columns.Count }),
		},
		pColumnWidth: {
			name: "column-width", parse: parseColumnLength,
			compute: func(s *styler, st *Style, v any) {
				if d, ok := columnLength(s, v); ok {
					st.Columns.Width = d
				}
			},
			copyFrom: field(func(s *Style) *Dimension { return &s.Columns.Width }),
		},
		pColumnRuleStyle: enumProp("column-rule-style", false, func(s *Style) *BorderStyle { return &s.Columns.RuleStyle }, borderStyles),
		pColumnRuleWidth: {
			name: "column-rule-width", parse: parseBorderWidth,
			compute: func(s *styler, st *Style, v any) {
				if w, ok := computeLineWidth(s, v); ok {
					st.Columns.RuleWidth = w
				}
			},
			copyFrom: field(func(s *Style) *float64 { return &s.Columns.RuleWidth }),
			initial:  length{pt: 3 * Pt, hasLen: true},
		},
		pColumnRuleColor: {
			name: "column-rule-color",
			parse: func(v []token, _ *declCtx) (any, bool) {
				t, ok := single(v)
				if !ok {
					return nil, false
				}
				c, ok := parseColor(t)
				return c, ok
			},
			compute: func(_ *styler, st *Style, v any) {
				if c, ok := v.(colorValue); ok {
					st.Columns.RuleColor, st.Columns.ruleCurrent = c.c, c.current
					if c.current {
						st.Columns.RuleColor = st.Color
					}
				}
			},
			copyFrom: func(dst, src *Style) {
				dst.Columns.RuleColor, dst.Columns.ruleCurrent = src.Columns.RuleColor, src.Columns.ruleCurrent
				if dst.Columns.ruleCurrent {
					dst.Columns.RuleColor = dst.Color
				}
			},
			initial: colorValue{current: true},
		},
		pColumnSpan: {
			name: "column-span",
			parse: func(v []token, _ *declCtx) (any, bool) {
				k, ok := keyword(v)
				if !ok || k != "none" && k != "all" {
					return nil, false
				}
				return k == "all", true
			},
			compute: func(_ *styler, st *Style, v any) {
				if all, ok := v.(bool); ok {
					st.Columns.SpanAll = all
				}
			},
			copyFrom: field(func(s *Style) *bool { return &s.Columns.SpanAll }),
		},
		pColumnFill: enumProp("column-fill", false, func(s *Style) *ColumnFill { return &s.Columns.Fill }, columnFills),
		pColumnHeight: {
			name: "column-height", parse: parseColumnLength,
			compute: func(s *styler, st *Style, v any) {
				if d, ok := columnLength(s, v); ok {
					st.Columns.Height = d
				}
			},
			copyFrom: field(func(s *Style) *Dimension { return &s.Columns.Height }),
		},
		pColumnWrap: {
			name: "column-wrap",
			parse: func(v []token, _ *declCtx) (any, bool) {
				k, ok := keyword(v)
				if !ok {
					return nil, false
				}
				w, ok := map[string]columnWrap{"auto": wrapAuto, "nowrap": wrapNo, "wrap": wrapYes}[k]
				return w, ok
			},
			compute: func(_ *styler, st *Style, v any) {
				if w, ok := v.(columnWrap); ok {
					st.Columns.wrap = w
				}
			},
			copyFrom: field(func(s *Style) *columnWrap { return &s.Columns.wrap }),
		},
	}
}

// expandColumns expands the columns shorthand (CSS Multicol 1, 3.3):
// <'column-width'> || <'column-count'>, an auto keyword standing for
// either, the longhands left out reset to auto.
func expandColumns(v []token, c *declCtx) ([]entry, bool) {
	if len(v) < 1 || len(v) > 2 {
		return nil, false
	}
	var width, count any = columnAuto{}, columnAuto{}
	var hasW, hasC bool
	autos := 0
	for _, t := range v {
		one := []token{t}
		if t.typ == tIdent && lower(t.value) == "auto" {
			autos++
			continue
		}
		if !hasC {
			if n, ok := parseColumnCount(one, c); ok {
				count, hasC = n, true
				continue
			}
		}
		if !hasW {
			if l, ok := parseColumnLength(one, c); ok {
				width, hasW = l, true
				continue
			}
		}
		return nil, false
	}
	return []entry{{pColumnWidth, width}, {pColumnCount, count}}, true
}

// expandColumnRule expands the column-rule shorthand (CSS Multicol 1,
// 4.5): <'column-rule-width'> || <'column-rule-style'> ||
// <'column-rule-color'>.
func expandColumnRule(v []token, c *declCtx) ([]entry, bool) {
	if len(v) < 1 || len(v) > 3 {
		return nil, false
	}
	width, style, colour := any(wideInitial), any(wideInitial), any(wideInitial)
	var w, s, col bool
	for _, t := range v {
		one := []token{t}
		if !s {
			if pv, ok := props[pColumnRuleStyle].parse(one, c); ok {
				style, s = pv, true
				continue
			}
		}
		if !w {
			if pv, ok := parseBorderWidth(one, c); ok {
				width, w = pv, true
				continue
			}
		}
		if !col {
			if pv, ok := parseColor(t); ok {
				colour, col = pv, true
				continue
			}
		}
		return nil, false
	}
	return []entry{{pColumnRuleWidth, width}, {pColumnRuleStyle, style}, {pColumnRuleColor, colour}}, true
}

// multicolShorthands returns the shorthands of multi-column layout.
func multicolShorthands() map[string]shorthandDef {
	return map[string]shorthandDef{
		"columns":     {longhands: []propID{pColumnWidth, pColumnCount}, expand: expandColumns},
		"column-rule": {longhands: []propID{pColumnRuleWidth, pColumnRuleStyle, pColumnRuleColor}, expand: expandColumnRule},
	}
}
