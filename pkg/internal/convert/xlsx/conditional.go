package xlsx

import (
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
)

// The namespaces of the Excel 2010 extensions a worksheet's extLst holds
// its data bar settings in (MS-XLSX, 2.6.30 CT_ConditionalFormattings,
// 2.6.29 CT_DataBar).
const (
	nsX14 = "http://schemas.microsoft.com/office/spreadsheetml/2009/9/main"
	nsXM  = "http://schemas.microsoft.com/office/excel/2006/main"
)

// maxCondFormats caps the conditional formats of a sheet, and
// maxCondRules the rules of one. Anti-DoS guard, not spec-mandated: a
// sheet holds a few dozen formats of a few rules each.
const (
	maxCondFormats = 1024
	maxCondRules   = 64
)

// conditionalFormats reads the sheet's conditional formats (ECMA-376
// Part 1, 18.3.1.18 conditionalFormatting, 18.3.1.10 cfRule) into
// sh.Conditional: each rule with its kind, priority, operator, constant
// operands and differential style (18.8.14 dxf), its colour scale or data
// bar with the thresholds resolved against the range's numbers. A data
// bar's thresholds come from its Excel 2010 extension when it has one, as
// LibreOffice reads them: "autoMin" and "autoMax" keep zero in the range.
// An operand that is not a constant is kept as the rule's formula, which
// the builder does not evaluate; a threshold given by a formula drops its
// rule with a warning.
func (w *sheetReader) conditionalFormats(root *ooxml.Element) {
	ext := w.dataBarExtensions(root)
	n := 0
	for _, cf := range root.Kids() {
		if !cf.Is(nsMain, "conditionalFormatting") {
			continue
		}
		if n >= maxCondFormats {
			break
		}
		n++
		var c sheet.Conditional
		for _, ref := range strings.Fields(attr(cf, "sqref")) {
			if rg, ok := rangeRef(ref); ok {
				c.Ranges = append(c.Ranges, rg)
			}
		}
		if len(c.Ranges) == 0 {
			continue
		}
		for _, e := range cf.Kids() {
			if !e.Is(nsMain, "cfRule") || len(c.Rules) >= maxCondRules {
				continue
			}
			c.Rules = append(c.Rules, w.cfRule(e, c.Ranges, ext))
		}
		if len(c.Rules) > 0 {
			w.sh.Conditional = append(w.sh.Conditional, c)
		}
	}
}

// cfRule reads one rule against the ranges it applies to.
func (w *sheetReader) cfRule(e *ooxml.Element, ranges []sheet.Range, ext map[string]*ooxml.Element) sheet.Rule {
	r := w.r
	rule := sheet.Rule{Priority: max(intAttr(e, "priority", 0), 0), StopIfTrue: boolAttr(e, "stopIfTrue")}
	if id, ok := ooxml.Int(attr(e, "dxfId")); ok {
		rule.Style = r.dxfStyle(id)
	}
	var formulas []string
	for _, f := range e.Kids() {
		if f.Is(nsMain, "formula") && len(formulas) < 3 {
			formulas = append(formulas, textOf(f))
		}
	}
	switch typ := attr(e, "type"); typ {
	case "cellIs":
		rule.Kind = sheet.RuleCellIs
		rule.Operator = attr(e, "operator")
		for _, f := range formulas {
			v, ok := constant(f)
			if !ok {
				rule.Formula = f
				break
			}
			rule.Values = append(rule.Values, v)
		}
	case "containsText", "notContainsText", "beginsWith", "endsWith":
		rule.Kind = sheet.RuleText
		rule.Operator = typ
		if typ == "notContainsText" {
			rule.Operator = "notContains"
		}
		rule.Values = []sheet.Value{{Kind: sheet.Text, Text: attr(e, "text")}}
	case "containsBlanks":
		rule.Kind = sheet.RuleBlanks
	case "notContainsBlanks":
		rule.Kind = sheet.RuleNoBlanks
	case "duplicateValues":
		rule.Kind = sheet.RuleDuplicates
	case "uniqueValues":
		rule.Kind = sheet.RuleUniques
	case "top10":
		rule.Kind = sheet.RuleTop
		if boolAttr(e, "bottom") {
			rule.Kind = sheet.RuleBottom
		}
		rank := float64(intAttr(e, "rank", 10))
		if boolAttr(e, "percent") {
			// The top percent holds for the share of the range's values
			// that many: the rank is that share of their count, truncated
			// (ScConditionEntry::IsTopNPercent).
			rank = float64(int(float64(len(sheet.RangeNumbers(w.sh, ranges))) * rank / 100))
		}
		rule.Values = []sheet.Value{{Kind: sheet.Number, Number: rank}}
	case "aboveAverage":
		rule.Kind = sheet.RuleAboveAverage
		if v, ok := e.Attr("aboveAverage"); ok && !ooxml.OnOff(v, true) {
			rule.Kind = sheet.RuleBelowAverage
		}
		if _, ok := e.Attr("stdDev"); ok {
			rule.Kind, rule.Formula = sheet.RuleExpression, "stdDev"
		}
	case "colorScale":
		cs := e.Child(nsMain, "colorScale")
		rule.Kind = sheet.RuleColorScale
		for _, k := range cs.Kids() {
			if k.Is(nsMain, "color") {
				c, ok := r.color(k)
				if !ok {
					c = css.Color{A: 1}
				}
				rule.Scale = append(rule.Scale, c)
			}
		}
		vals, ok := w.cfvos(cs, nsMain, "val", ranges)
		if !ok || len(vals) != len(rule.Scale) {
			rule.Kind, rule.Formula = sheet.RuleExpression, "colorScale"
			break
		}
		rule.Values = vals
	case "dataBar":
		db := e.Child(nsMain, "dataBar")
		rule.Kind = sheet.RuleDataBar
		if c, ok := r.color(db.Child(nsMain, "color")); ok {
			rule.Bar = c
		}
		src, ns, valAttr := db, nsMain, "val"
		if x := ext[extID(e)]; x != nil {
			src, ns, valAttr = x, nsX14, "value"
		}
		vals, ok := w.cfvos(src, ns, valAttr, ranges)
		if !ok || len(vals) != 2 {
			rule.Kind, rule.Formula = sheet.RuleExpression, "dataBar"
			break
		}
		rule.Values = vals
	case "iconSet":
		rule.Kind = sheet.RuleIconSet
	default:
		rule.Kind = sheet.RuleExpression
		rule.Formula = typ
		if len(formulas) > 0 {
			rule.Formula = formulas[0]
		}
	}
	return rule
}

// cfvos resolves the thresholds of a colour scale or a data bar (18.3.1.11
// cfvo; MS-XLSX, 2.6.17 CT_Cfvo): each to a number over the range's
// numbers; false when one is a formula or the range holds no number for a
// threshold that needs one.
func (w *sheetReader) cfvos(e *ooxml.Element, ns, valAttr string, ranges []sheet.Range) ([]sheet.Value, bool) {
	var nums []float64
	read := false
	var out []sheet.Value
	for _, k := range e.Kids() {
		if !k.Is(ns, "cfvo") {
			continue
		}
		typ := attr(k, "type")
		raw := attr(k, valAttr)
		if raw == "" {
			raw = textOf(k.Child(nsXM, "f"))
		}
		v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
		switch typ {
		case "num", "percent", "percentile":
			if err != nil {
				return nil, false
			}
		case "formula":
			// A constant formula is its number.
			if err != nil {
				return nil, false
			}
			typ = "num"
		}
		if !read && typ != "num" {
			nums, read = sheet.RangeNumbers(w.sh, ranges), true
		}
		t, ok := sheet.Threshold(typ, v, nums)
		if !ok {
			return nil, false
		}
		out = append(out, sheet.Value{Kind: sheet.Number, Number: t})
	}
	return out, true
}

// dataBarExtensions returns the Excel 2010 data bars of the worksheet's
// extLst by the id their cfRule's extension names (MS-XLSX, 2.6.30).
func (w *sheetReader) dataBarExtensions(root *ooxml.Element) map[string]*ooxml.Element {
	out := map[string]*ooxml.Element{}
	for _, x := range root.Child(nsMain, "extLst").Kids() {
		for _, cfs := range x.Kids() {
			if !cfs.Is(nsX14, "conditionalFormattings") {
				continue
			}
			for _, cf := range cfs.Kids() {
				for _, rule := range cf.Kids() {
					if db := rule.Child(nsX14, "dataBar"); rule.Is(nsX14, "cfRule") && db != nil && len(out) < maxCondFormats {
						out[attr(rule, "id")] = db
					}
				}
			}
		}
	}
	return out
}

// extID returns the id of the Excel 2010 extension a cfRule names, "" for
// none.
func extID(e *ooxml.Element) string {
	for _, x := range e.Child(nsMain, "extLst").Kids() {
		if id := x.Child(nsX14, "id"); id != nil {
			return strings.TrimSpace(textOf(id))
		}
	}
	return ""
}

// constant reads a formula that is a constant: a number, a quoted string
// (its doubled quotes read as one) or a boolean; false otherwise.
func constant(f string) (sheet.Value, bool) {
	f = strings.TrimSpace(f)
	if v, err := strconv.ParseFloat(f, 64); err == nil {
		return sheet.Value{Kind: sheet.Number, Number: v}, true
	}
	if len(f) >= 2 && f[0] == '"' && f[len(f)-1] == '"' && !strings.Contains(strings.ReplaceAll(f[1:len(f)-1], `""`, ""), `"`) {
		return sheet.Value{Kind: sheet.Text, Text: strings.ReplaceAll(f[1:len(f)-1], `""`, `"`)}, true
	}
	switch strings.ToUpper(f) {
	case "TRUE":
		return sheet.Value{Kind: sheet.Number, Number: 1}, true
	case "FALSE":
		return sheet.Value{Kind: sheet.Number, Number: 0}, true
	}
	return sheet.Value{}, false
}

// dxfStyle returns the differential format id (ECMA-376 Part 1, 18.8.14
// dxf) as the fields of a style it sets: the font's colour, weight, slant,
// underline and strike, the fill, the border sides and the number
// format; nil for an id past the table.
func (r *reader) dxfStyle(id int) *sheet.Style {
	s := r.styles
	if st, ok := s.dxfStyles[id]; ok {
		return st
	}
	if id < 0 || id >= len(s.dxfs) {
		return nil
	}
	e := s.dxfs[id]
	d := r.readDxf(e)
	st := &sheet.Style{}
	if d.bold != nil {
		st.Font.Bold = *d.bold
	}
	if d.italic != nil {
		st.Font.Italic = *d.italic
	}
	if d.color != nil {
		st.Font.Color = *d.color
	}
	if d.fill != nil {
		st.Fill.Color = *d.fill
	}
	if f := e.Child(nsMain, "font"); f != nil {
		if u := f.Child(nsMain, "u"); u != nil {
			v, has := u.Attr("val")
			st.Font.Underline = underline(v, has)
		}
		if k := f.Child(nsMain, "strike"); k != nil {
			st.Font.Strike = ooxml.Toggle(k)
		}
	}
	for side, at := range map[css.Side]int{css.Left: dxfLeft, css.Right: dxfRight, css.Top: dxfTop, css.Bottom: dxfBottom} {
		if b := d.border[at]; b != nil {
			st.Border[side] = *b
		}
	}
	if nf := e.Child(nsMain, "numFmt"); nf != nil {
		code := attr(nf, "formatCode")
		if code == "" {
			if fid, ok := ooxml.Int(attr(nf, "numFmtId")); ok {
				code = r.format(fid)
			}
		}
		if len(code) > maxFormatCode {
			code = code[:maxFormatCode]
		}
		st.Format = code
	}
	s.dxfStyles[id] = st
	return st
}
