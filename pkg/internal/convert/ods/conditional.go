package ods

import (
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/odf"
)

// nsCalcExt is LibreOffice's Calc extension namespace, which holds the
// conditional formats Calc writes (calcext:conditional-formats).
const nsCalcExt = "urn:org:documentfoundation:names:experimental:calc:xmlns:calcext:1.0"

// maxCondFormats caps the conditional formats of a table, and
// maxCondRules the rules of one. Anti-DoS guard, not spec-mandated: a
// sheet holds a few dozen formats of a few rules each.
const (
	maxCondFormats = 1024
	maxCondRules   = 64
)

// conditionalFormats reads the calcext:conditional-formats element el of
// the table sh into sh.Conditional: each calcext:conditional-format with
// its target ranges and, in document order, which is Calc's evaluation
// order, its conditions (calcext:condition, the style a named cell style
// sets over the cell's), colour scales (calcext:color-scale, the
// thresholds resolved over the range's numbers), data bars
// (calcext:data-bar) and icon sets. A condition whose operand is not a
// constant or a cell of this table, or that tests a formula or a date, is
// kept with its formula, which the builder does not evaluate.
func (r *reader) conditionalFormats(sh *sheet.Sheet, el *odf.Element) {
	for _, cf := range el.Children {
		if !cf.Is(nsCalcExt, "conditional-format") || len(sh.Conditional) >= maxCondFormats {
			continue
		}
		c := sheet.Conditional{Ranges: parseRanges(cf.A(nsCalcExt, "target-range-address"))}
		if len(c.Ranges) == 0 {
			continue
		}
		for _, e := range cf.Children {
			if len(c.Rules) >= maxCondRules {
				break
			}
			if rule, ok := r.condRule(sh, e, c.Ranges); ok {
				c.Rules = append(c.Rules, rule)
			}
		}
		if len(c.Rules) > 0 {
			sh.Conditional = append(sh.Conditional, c)
		}
	}
}

// condRule reads one entry of a conditional format.
func (r *reader) condRule(sh *sheet.Sheet, e *odf.Element, ranges []sheet.Range) (sheet.Rule, bool) {
	switch {
	case e.Is(nsCalcExt, "condition"):
		rel := &relative{single: len(ranges) == 1 && ranges[0].FirstRow == ranges[0].LastRow && ranges[0].FirstColumn == ranges[0].LastColumn}
		if base, ok := cellRef(e.A(nsCalcExt, "base-cell-address")); ok && rel.single && !base.whole {
			rel.dr, rel.dc = ranges[0].FirstRow-base.row, ranges[0].FirstColumn-base.col
		}
		rule := r.condition(sh, e.A(nsCalcExt, "value"), ranges, rel)
		rule.Style = r.condStyle(e.A(nsCalcExt, "apply-style-name"))
		return rule, true
	case e.Is(nsCalcExt, "date-is"):
		return sheet.Rule{Kind: sheet.RuleExpression, Formula: "date-is " + e.A(nsCalcExt, "date"), Style: r.condStyle(e.A(nsCalcExt, "style"))}, true
	case e.Is(nsCalcExt, "color-scale"):
		rule := sheet.Rule{Kind: sheet.RuleColorScale}
		var entries []*odf.Element
		for _, k := range e.Children {
			if k.Is(nsCalcExt, "color-scale-entry") {
				entries = append(entries, k)
				c, ok := color(k.A(nsCalcExt, "color"))
				if !ok {
					c = css.Color{A: 1}
				}
				rule.Scale = append(rule.Scale, c)
			}
		}
		vals, ok := thresholds(sh, entries, ranges)
		if !ok || len(vals) != len(rule.Scale) {
			return sheet.Rule{Kind: sheet.RuleExpression, Formula: "color-scale"}, true
		}
		rule.Values = vals
		return rule, true
	case e.Is(nsCalcExt, "data-bar"):
		rule := sheet.Rule{Kind: sheet.RuleDataBar}
		if c, ok := color(e.A(nsCalcExt, "positive-color")); ok {
			rule.Bar = c
		}
		var entries []*odf.Element
		for _, k := range e.Children {
			if k.Is(nsCalcExt, "formatting-entry") {
				entries = append(entries, k)
			}
		}
		vals, ok := thresholds(sh, entries, ranges)
		if !ok || len(vals) != 2 {
			return sheet.Rule{Kind: sheet.RuleExpression, Formula: "data-bar"}, true
		}
		rule.Values = vals
		return rule, true
	case e.Is(nsCalcExt, "icon-set"):
		return sheet.Rule{Kind: sheet.RuleIconSet}, true
	}
	return sheet.Rule{}, false
}

// thresholdKinds maps the calcext:type of a colour scale or data bar entry
// (LibreOffice's ScXMLColorScaleFormatEntryContext) to the kinds of
// sheet.Threshold.
var thresholdKinds = map[string]string{
	"minimum": "min", "maximum": "max", "auto-minimum": "autoMin", "auto-maximum": "autoMax",
	"percent": "percent", "percentile": "percentile", "number": "num", "formula": "num",
}

// thresholds resolves the entries of a colour scale or a data bar to
// numbers over the range's numbers; false when one is a formula other
// than a number or a cell of this table.
func thresholds(sh *sheet.Sheet, entries []*odf.Element, ranges []sheet.Range) ([]sheet.Value, bool) {
	var nums []float64
	read := false
	var out []sheet.Value
	for _, k := range entries {
		kind, ok := thresholdKinds[k.A(nsCalcExt, "type")]
		if !ok {
			return nil, false
		}
		raw := strings.TrimPrefix(strings.TrimSpace(k.A(nsCalcExt, "value")), "=")
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			if c, ok := operand(sh, raw, nil); ok && c.Kind == sheet.Number {
				v, err = c.Number, nil
			}
		}
		if err != nil && (kind == "num" || kind == "percent" || kind == "percentile") {
			return nil, false
		}
		if !read && kind != "num" {
			nums, read = sheet.RangeNumbers(sh, ranges), true
		}
		t, ok := sheet.Threshold(kind, v, nums)
		if !ok {
			return nil, false
		}
		out = append(out, sheet.Value{Kind: sheet.Number, Number: t})
	}
	return out, true
}

// conditionOps are the comparison prefixes of a condition's value, the
// longer first, with the operators of sheet.Rule they stand for.
var conditionOps = []struct{ prefix, op string }{
	{"<=", "lessThanOrEqual"}, {">=", "greaterThanOrEqual"}, {"!=", "notEqual"},
	{"<", "lessThan"}, {">", "greaterThan"}, {"=", "equal"},
}

// conditionFuncs are the functions of a condition's value without
// operands, LibreOffice's ScXMLConditionContext names.
var conditionFuncs = map[string]sheet.RuleKind{
	"duplicate": sheet.RuleDuplicates, "unique": sheet.RuleUniques,
	"above-average": sheet.RuleAboveAverage, "below-average": sheet.RuleBelowAverage,
	"above-equal-average": sheet.RuleAboveAverage, "below-equal-average": sheet.RuleBelowAverage,
}

// condition reads the calcext:value of a calcext:condition (LibreOffice's
// syntax: a comparison "<10", "between(1,2)", a text test
// "contains-text("x")", a rank "top-elements(5)", "duplicate", an average
// test, "formula-is(...)"). The equal-average tests read as the strict
// ones, which the model has no operator for.
func (r *reader) condition(sh *sheet.Sheet, v string, ranges []sheet.Range, rel *relative) sheet.Rule {
	v = strings.TrimSpace(v)
	name, args, call := strings.Cut(v, "(")
	if call {
		args = strings.TrimSuffix(args, ")")
	}
	if kind, ok := conditionFuncs[name]; ok {
		return sheet.Rule{Kind: kind}
	}
	var rule sheet.Rule
	operands := func(list ...string) {
		for _, a := range list {
			c, ok := operand(sh, a, rel)
			if !ok {
				rule.Formula = a
			}
			rule.Values = append(rule.Values, c)
		}
	}
	switch name {
	case "between", "not-between":
		rule.Kind, rule.Operator = sheet.RuleCellIs, "between"
		if name == "not-between" {
			rule.Operator = "notBetween"
		}
		parts := splitArgs(args)
		if len(parts) != 2 {
			return sheet.Rule{Kind: sheet.RuleExpression, Formula: v}
		}
		operands(parts...)
		return rule
	case "contains-text", "not-contains-text", "begins-with", "ends-with":
		rule.Kind = sheet.RuleText
		rule.Operator = map[string]string{"contains-text": "containsText", "not-contains-text": "notContains", "begins-with": "beginsWith", "ends-with": "endsWith"}[name]
		operands(args)
		if len(rule.Values) == 1 && rule.Values[0].Kind == sheet.Number {
			rule.Values[0] = sheet.Value{Kind: sheet.Text, Text: strconv.FormatFloat(rule.Values[0].Number, 'g', -1, 64)}
		}
		return rule
	case "top-elements", "bottom-elements", "top-percent", "bottom-percent":
		rule.Kind = sheet.RuleTop
		if strings.HasPrefix(name, "bottom") {
			rule.Kind = sheet.RuleBottom
		}
		n, err := strconv.ParseFloat(strings.TrimSpace(args), 64)
		if err != nil {
			return sheet.Rule{Kind: sheet.RuleExpression, Formula: v}
		}
		if strings.HasSuffix(name, "percent") {
			// The share of the range's values, truncated
			// (ScConditionEntry::IsTopNPercent).
			n = float64(int(float64(len(sheet.RangeNumbers(sh, ranges))) * n / 100))
		}
		rule.Values = []sheet.Value{{Kind: sheet.Number, Number: n}}
		return rule
	}
	if call {
		return sheet.Rule{Kind: sheet.RuleExpression, Formula: v}
	}
	for _, o := range conditionOps {
		if rest, ok := strings.CutPrefix(v, o.prefix); ok {
			rule.Kind, rule.Operator = sheet.RuleCellIs, o.op
			operands(rest)
			return rule
		}
	}
	return sheet.Rule{Kind: sheet.RuleExpression, Formula: v}
}

// splitArgs splits a function's arguments at the commas and semicolons
// outside quotes and brackets.
func splitArgs(s string) []string {
	var out []string
	depth, start, quoted := 0, 0, false
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '"':
			quoted = !quoted
		case quoted:
		case c == '(' || c == '[':
			depth++
		case c == ')' || c == ']':
			depth--
		case (c == ',' || c == ';') && depth <= 0:
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

// relative says how the relative parts of a condition's cell references
// move: by the offset from the condition's base cell to the one cell the
// format covers; a format of several cells has no constant for them.
type relative struct {
	single bool
	dr, dc int
}

// operand reads a constant operand of OpenFormula (ODF 1.3, Part 2): a
// number, a string in double quotes, TRUE or FALSE, or a reference to one
// cell of this table, "[.B1]" or "[$Sheet1.$B$1]", read as that cell's
// value (an empty cell as 0): an absolute reference names its cell, a
// relative one, under rel, the cell as far from the format's cell as it
// is from the base cell (ODF 1.3, Part 3, 19.587 calcext:base-cell-address
// as LibreOffice writes it); false otherwise.
func operand(sh *sheet.Sheet, s string, rel *relative) (sheet.Value, bool) {
	s = strings.TrimSpace(s)
	if v, err := strconv.ParseFloat(s, 64); err == nil {
		return sheet.Value{Kind: sheet.Number, Number: v}, true
	}
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' && !strings.Contains(strings.ReplaceAll(s[1:len(s)-1], `""`, ""), `"`) {
		return sheet.Value{Kind: sheet.Text, Text: strings.ReplaceAll(s[1:len(s)-1], `""`, `"`)}, true
	}
	switch strings.ToUpper(s) {
	case "TRUE", "TRUE()":
		return sheet.Value{Kind: sheet.Number, Number: 1}, true
	case "FALSE", "FALSE()":
		return sheet.Value{Kind: sheet.Number, Number: 0}, true
	}
	if !strings.HasPrefix(s, "[") || !strings.HasSuffix(s, "]") || strings.Contains(s, ":") {
		return sheet.Value{}, false
	}
	addr := s[1 : len(s)-1]
	table, _, _ := strings.Cut(strings.TrimPrefix(addr, "$"), ".")
	if table = strings.Trim(table, "'"); table != "" && table != sh.Name {
		return sheet.Value{}, false
	}
	at, ok := cellRef(addr)
	if !ok || at.whole {
		return sheet.Value{}, false
	}
	if rel != nil {
		local := addr
		if i := strings.LastIndexByte(local, '.'); i >= 0 {
			local = local[i+1:]
		}
		colAbs := strings.HasPrefix(local, "$")
		rowAbs := strings.Contains(strings.TrimPrefix(local, "$"), "$")
		if (!colAbs || !rowAbs) && !rel.single {
			return sheet.Value{}, false
		}
		if !colAbs {
			at.col += rel.dc
		}
		if !rowAbs {
			at.row += rel.dr
		}
		if at.row < 0 || at.col < 0 {
			return sheet.Value{}, false
		}
	}
	for _, rw := range sh.Rows {
		if rw.Index != at.row {
			continue
		}
		for _, c := range rw.Cells {
			if c.Column != at.col {
				continue
			}
			switch c.Value.Kind {
			case sheet.Number, sheet.Bool:
				return sheet.Value{Kind: sheet.Number, Number: c.Value.Number}, true
			case sheet.Text:
				return sheet.Value{Kind: sheet.Text, Text: c.Value.Text}, true
			case sheet.Error:
				return sheet.Value{}, false
			case sheet.Empty:
			}
		}
	}
	return sheet.Value{Kind: sheet.Number}, true
}

// condStyle returns the cell style name a condition applies as the
// fields it sets over the cell's: the properties of the style and its
// ancestors up to, not including, Default, which every cell already
// carries (its font colour, weight, slant, underline and strike, its
// background, borders and data style); nil for no style.
func (r *reader) condStyle(name string) *sheet.Style {
	if name == "" || name == defaultStyleName {
		return nil
	}
	if st, ok := r.condSty[name]; ok {
		return st
	}
	var chain []*odf.Style
	for _, s := range r.styles.Chain(odf.ScopeContent, odf.FamilyCell, name) {
		if s.Name != defaultStyleName || s.Auto {
			chain = append(chain, s)
		}
	}
	if len(chain) == 0 {
		r.condSty[name] = nil
		return nil
	}
	var cp odf.Props
	var text []odf.Props
	format := ""
	for _, s := range chain {
		cp = cp.Over(s.Props(odf.CellProps))
		if p := s.Props(odf.TextProps); !p.Empty() {
			text = append(text, p)
		}
		if ds, ok := s.El.Lookup(odf.NSStyle, "data-style-name"); ok {
			format = r.data.Code(odf.ScopeContent, ds)
		}
	}
	st := &sheet.Style{Format: format}
	st.Font = r.foldFont(sheet.Font{}, text)
	if c, ok := color(cp.Get("fo:background-color")); ok {
		st.Fill.Color = c
	}
	st.Border, _ = borders(cp)
	r.condSty[name] = st
	return st
}
