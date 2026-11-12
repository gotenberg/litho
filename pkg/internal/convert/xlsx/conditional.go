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
// Part 1, 18.3.1.18 conditionalFormatting, 18.3.1.10 cfRule), then those
// of its Excel 2010 extension (MS-XLSX, 2.6.30 CT_ConditionalFormattings,
// which holds the rules whose operands refer to cells), into
// sh.Conditional: each rule with its kind, priority, operator, constant
// operands and differential style (18.8.14 dxf), its colour scale or data
// bar with the thresholds resolved against the range's numbers. A data
// bar's thresholds come from its Excel 2010 extension when it has one, as
// LibreOffice reads them: "autoMin" and "autoMax" keep zero in the range.
// An operand that refers to one cell, absolutely or from a format of one
// cell, takes that cell's value once the workbook is read
// ([reader.resolveOperands]); any other operand that is not a constant is
// kept as the rule's formula, which the builder does not evaluate, and so
// is a threshold given by a formula.
func (w *sheetReader) conditionalFormats(root *ooxml.Element) {
	ext := w.dataBarExtensions(root)
	n := 0
	add := func(cf *ooxml.Element, ranges []sheet.Range, ns cfNames) {
		if n >= maxCondFormats || len(ranges) == 0 {
			return
		}
		n++
		c := sheet.Conditional{Ranges: ranges}
		var refs []pendingOperand
		for _, e := range cf.Kids() {
			if len(c.Rules) >= maxCondRules {
				break
			}
			if !e.Is(ns.rule, "cfRule") {
				continue
			}
			if ns.rule == nsX14 && attr(e, "type") == "dataBar" && ext.used[attr(e, "id")] {
				continue
			}
			rule, ops := w.cfRule(e, ranges, ext, ns)
			for _, op := range ops {
				op.rule = len(c.Rules)
				refs = append(refs, op)
			}
			c.Rules = append(c.Rules, rule)
		}
		if len(c.Rules) == 0 {
			return
		}
		for _, op := range refs {
			op.sh, op.cond = w.sh, len(w.sh.Conditional)
			if len(w.r.pending) < maxCondFormats*maxCondRules {
				w.r.pending = append(w.r.pending, op)
			}
		}
		w.sh.Conditional = append(w.sh.Conditional, c)
	}
	for _, cf := range root.Kids() {
		if cf.Is(nsMain, "conditionalFormatting") {
			add(cf, sqref(attr(cf, "sqref")), mainNames)
		}
	}
	for _, cf := range ext.formats {
		add(cf, sqref(textOf(cf.Child(nsXM, "sqref"))), x14Names)
	}
}

// sqref parses a list of ranges (ECMA-376 Part 1, 18.18.76 ST_Sqref).
func sqref(s string) []sheet.Range {
	var out []sheet.Range
	for ref := range strings.FieldsSeq(s) {
		if rg, ok := rangeRef(ref); ok {
			out = append(out, rg)
		}
	}
	return out
}

// cfNames are the names of a rule's parts: the namespace of the rule and
// its children, and the formula element.
type cfNames struct {
	rule, formulaNS, formula string
}

var (
	mainNames = cfNames{rule: nsMain, formulaNS: nsMain, formula: "formula"}
	x14Names  = cfNames{rule: nsX14, formulaNS: nsXM, formula: "f"}
)

// maxRuleFormulas is the most formulas a rule holds (ECMA-376 Part 1,
// 18.3.1.10 cfRule: formula occurs at most three times); the reader
// keeps no more.
const maxRuleFormulas = 3

// pendingOperand is an operand of a rule that refers to a cell, read once
// the workbook is: the rule (its sheet, format and index), the operand's
// index among its values, the sheet the reference names ("" for the
// rule's own) and the cell.
type pendingOperand struct {
	sh            *sheet.Sheet
	cond, rule    int
	value         int
	sheetName     string
	row, col      int
	formula       string
	otherFormulas bool
}

// cfRule reads one rule against the ranges it applies to, with the
// operands that refer to a cell.
func (w *sheetReader) cfRule(e *ooxml.Element, ranges []sheet.Range, ext *dataBarExt, ns cfNames) (sheet.Rule, []pendingOperand) {
	r := w.r
	rule := sheet.Rule{Priority: max(intAttr(e, "priority", 0), 0), StopIfTrue: boolAttr(e, "stopIfTrue")}
	if id, ok := ooxml.Int(attr(e, "dxfId")); ok {
		rule.Style = r.dxfStyle(id)
	} else if d := e.Child(nsX14, "dxf"); d != nil {
		rule.Style = r.dxfElementStyle(d)
	}
	var formulas []string
	for _, f := range e.Kids() {
		if f.Is(ns.formulaNS, ns.formula) && len(formulas) < maxRuleFormulas {
			formulas = append(formulas, textOf(f))
		}
	}
	var refs []pendingOperand
	hard := false
	operand := func(f string) {
		if v, ok := constant(f); ok {
			rule.Values = append(rule.Values, v)
			return
		}
		rule.Formula = f
		if name, row, col, ok := cellOperand(f, ranges); ok {
			refs = append(refs, pendingOperand{value: len(rule.Values), sheetName: name, row: row, col: col, formula: f})
		} else {
			hard = true
		}
		rule.Values = append(rule.Values, sheet.Value{})
	}
	switch typ := attr(e, "type"); typ {
	case "cellIs":
		rule.Kind = sheet.RuleCellIs
		rule.Operator = attr(e, "operator")
		for _, f := range formulas {
			operand(f)
		}
	case "containsText", "notContainsText", "beginsWith", "endsWith":
		rule.Kind = sheet.RuleText
		rule.Operator = typ
		if typ == "notContainsText" {
			rule.Operator = "notContains"
		}
		if t, ok := e.Attr("text"); ok || len(formulas) < 2 {
			rule.Values = []sheet.Value{{Kind: sheet.Text, Text: t}}
		} else {
			// The extension's rule names its text by its second formula.
			operand(formulas[1])
		}
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
		cs := e.Child(ns.rule, "colorScale")
		rule.Kind = sheet.RuleColorScale
		for _, k := range cs.Kids() {
			if k.Is(ns.rule, "color") {
				c, ok := r.color(k)
				if !ok {
					c = css.Color{A: 1}
				}
				rule.Scale = append(rule.Scale, c)
			}
		}
		vals, ok := w.cfvos(cs, ns.rule, "val", ranges)
		if !ok || len(vals) != len(rule.Scale) {
			rule.Kind, rule.Formula = sheet.RuleExpression, "colorScale"
			break
		}
		rule.Values = vals
	case "dataBar":
		db := e.Child(ns.rule, "dataBar")
		rule.Kind = sheet.RuleDataBar
		if c, ok := r.color(db.Child(ns.rule, "color")); ok {
			rule.Bar = c
		} else if c, ok := r.color(db.Child(nsX14, "fillColor")); ok {
			rule.Bar = c
		}
		src, cfvoNS, valAttr := db, ns.rule, "val"
		if x := ext.bars[extID(e)]; x != nil {
			src, cfvoNS, valAttr = x, nsX14, "value"
		} else if ns.rule == nsX14 {
			valAttr = "value"
		}
		vals, ok := w.cfvos(src, cfvoNS, valAttr, ranges)
		if !ok || len(vals) != 2 {
			rule.Kind, rule.Formula = sheet.RuleExpression, "dataBar"
			break
		}
		rule.Values = vals
		dataBarSettings(&rule, db, ext.bars[extID(e)], func(e *ooxml.Element) (css.Color, bool) { return r.color(e) })
	case "iconSet":
		rule.Kind = sheet.RuleIconSet
		iconSet(&rule, w, e.Child(ns.rule, "iconSet"), ns, ranges)
	default:
		rule.Kind = sheet.RuleExpression
		rule.Formula = typ
		if len(formulas) > 0 {
			rule.Formula = formulas[0]
		}
		return rule, nil
	}
	for i := range refs {
		refs[i].otherFormulas = hard
	}
	return rule, refs
}

// cellOperand reads an operand that refers to one cell: an absolute
// reference ("$B$1", "Sheet2!$A$1"), or a relative one when the format
// covers one cell, which it then names as written (ECMA-376 Part 1,
// 18.17.2.3: a relative reference is relative to the first cell of the
// range).
func cellOperand(f string, ranges []sheet.Range) (name string, row, col int, ok bool) {
	f = strings.TrimSpace(f)
	if i := strings.LastIndexByte(f, '!'); i >= 0 {
		name, f = unquoteSheet(f[:i]), f[i+1:]
	}
	row, col, ok = cellRef(f)
	if !ok {
		return "", 0, 0, false
	}
	absolute := strings.HasPrefix(f, "$") && strings.Contains(f[1:], "$")
	single := len(ranges) == 1 && ranges[0].FirstRow == ranges[0].LastRow && ranges[0].FirstColumn == ranges[0].LastColumn
	return name, row, col, absolute || single
}

// resolveOperands gives the rules' operands that refer to a cell that
// cell's value, once every sheet is read: its number, its text, the empty
// text for an empty cell a text rule names and 0 for one a comparison
// names. A rule whose operands all resolve loses its formula; one whose
// reference names no sheet of the book or an error keeps it.
func (r *reader) resolveOperands(doc *sheet.Document) {
	type ruleKey struct {
		sh         *sheet.Sheet
		cond, rule int
	}
	failed := map[ruleKey]bool{}
	for _, op := range r.pending {
		if op.cond >= len(op.sh.Conditional) || op.rule >= len(op.sh.Conditional[op.cond].Rules) {
			continue
		}
		rule := &op.sh.Conditional[op.cond].Rules[op.rule]
		key := ruleKey{op.sh, op.cond, op.rule}
		v, ok := r.cellValue(doc, op.sh, op.sheetName, op.row, op.col, rule.Kind == sheet.RuleText)
		if !ok || op.value >= len(rule.Values) || op.otherFormulas {
			failed[key] = true
			continue
		}
		rule.Values[op.value] = v
	}
	for _, op := range r.pending {
		key := ruleKey{op.sh, op.cond, op.rule}
		if op.cond < len(op.sh.Conditional) && op.rule < len(op.sh.Conditional[op.cond].Rules) && !failed[key] {
			op.sh.Conditional[op.cond].Rules[op.rule].Formula = ""
		}
	}
	r.pending = nil
}

// cellValue returns the value of the cell at row and col of the sheet
// named name, own when the name is empty.
func (r *reader) cellValue(doc *sheet.Document, own *sheet.Sheet, name string, row, col int, text bool) (sheet.Value, bool) {
	sh := own
	if name != "" {
		sh = nil
		for _, s := range doc.Sheets {
			if strings.EqualFold(s.Name, name) {
				sh = s
				break
			}
		}
		if sh == nil {
			return sheet.Value{}, false
		}
	}
	for _, rw := range sh.Rows {
		if rw.Index != row {
			continue
		}
		for _, c := range rw.Cells {
			if c.Column != col {
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
	if text {
		return sheet.Value{Kind: sheet.Text}, true
	}
	return sheet.Value{Kind: sheet.Number}, true
}

// cfvos resolves the thresholds of a colour scale or a data bar (18.3.1.11
// cfvo; MS-XLSX, 2.6.17 CT_Cfvo): each to a number over the range's
// numbers, a value naming an absolute cell of the sheet being that cell's
// number; false when one is another formula or the range holds no number
// for a threshold that needs one.
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
		if err != nil && strings.TrimSpace(raw) != "" {
			// A threshold naming a cell of this sheet takes its number.
			if name, row, col, ok := cellOperand(raw, nil); ok && (name == "" || strings.EqualFold(name, w.sh.Name)) {
				if c, ok := w.r.cellValue(nil, w.sh, "", row, col, false); ok && c.Kind == sheet.Number {
					v, err = c.Number, nil
				}
			}
		}
		switch typ {
		case "num", "percent", "percentile":
			if err != nil {
				return nil, false
			}
		case "formula":
			// A constant formula, or one naming a cell, is its number;
			// another is left for the builder to compute.
			if err != nil {
				out = append(out, sheet.Value{Kind: sheet.Text, Text: "=" + strings.TrimSpace(raw)})
				continue
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

// dataBarExt is the Excel 2010 extension of a worksheet's conditional
// formats: its formats, and the data bars a cfRule names by id, which
// extend that rule rather than stand alone.
type dataBarExt struct {
	formats []*ooxml.Element
	bars    map[string]*ooxml.Element
	used    map[string]bool
}

// dataBarExtensions returns the Excel 2010 conditional formats of the
// worksheet's extLst (MS-XLSX, 2.6.30), with the data bars the
// worksheet's own rules name.
func (w *sheetReader) dataBarExtensions(root *ooxml.Element) *dataBarExt {
	out := &dataBarExt{bars: map[string]*ooxml.Element{}, used: map[string]bool{}}
	for _, x := range root.Child(nsMain, "extLst").Kids() {
		for _, cfs := range x.Kids() {
			if !cfs.Is(nsX14, "conditionalFormattings") {
				continue
			}
			for _, cf := range cfs.Kids() {
				if len(out.formats) >= maxCondFormats {
					break
				}
				if !cf.Is(nsX14, "conditionalFormatting") {
					continue
				}
				out.formats = append(out.formats, cf)
				for _, rule := range cf.Kids() {
					if db := rule.Child(nsX14, "dataBar"); rule.Is(nsX14, "cfRule") && db != nil && len(out.bars) < maxCondFormats {
						out.bars[attr(rule, "id")] = db
					}
				}
			}
		}
	}
	for _, cf := range root.Kids() {
		if !cf.Is(nsMain, "conditionalFormatting") {
			continue
		}
		for _, e := range cf.Kids() {
			if id := extID(e); id != "" && out.bars[id] != nil {
				out.used[id] = true
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
	st := r.dxfElementStyle(s.dxfs[id])
	s.dxfStyles[id] = st
	return st
}

// dxfElementStyle returns the differential format e as the fields of a
// style it sets, as dxfStyle says.
func (r *reader) dxfElementStyle(e *ooxml.Element) *sheet.Style {
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
	return st
}

// dataBarSettings reads a data bar's axis, lengths, fill and negative
// colour as LibreOffice imports them (DataBarRule::importAttribs,
// ExtCfDataBarRule): a bar without the Excel 2010 extension has no axis,
// its lengths the dataBar element's minLength and maxLength, 10 and 90
// percent by default; the extension (MS-XLSX, 2.6.29 x14:dataBar) gives
// the axis, automatic by default, the gradient and the negative colour,
// and the lengths stay the dataBar element's.
//
// # Corpus evidence
//
//   - libreoffice/xlsx: sc/databar.xlsx's middle axis, whose extension says
//     maxLength 100, paints its longest bar across 90% of the half cell.
func dataBarSettings(rule *sheet.Rule, db, ext *ooxml.Element, color func(*ooxml.Element) (css.Color, bool)) {
	length := func(name string, def int) float64 {
		return float64(max(0, min(intAttr(db, name, def), 100))) / 100
	}
	rule.BarMinLength, rule.BarMaxLength = length("minLength", 10), length("maxLength", 90)
	if rule.BarMinLength == 0 && rule.BarMaxLength == 0 {
		rule.BarMaxLength = 0.001
	}
	if ext == nil {
		rule.BarAxis = sheet.BarAxisNone
		return
	}
	switch attr(ext, "axisPosition") {
	case "middle":
		rule.BarAxis = sheet.BarAxisMiddle
	case "none":
		rule.BarAxis = sheet.BarAxisNone
	default:
		rule.BarAxis = sheet.BarAxisAuto
	}
	rule.BarSolid = !boolOrTrue(ext, "gradient")
	if c, ok := color(ext.Child(nsX14, "negativeFillColor")); ok {
		rule.BarNegative = c
	}
}

// iconSet reads the icon set of a rule (ECMA-376 Part 1, 18.3.1.49
// iconSet; MS-XLSX, 2.6.32 CT_IconSet): its set, 3TrafficLights1 by
// default, its thresholds, reverse and showValue, and per threshold the
// gte flag and the custom icon x14:cfIcon names. A rule whose thresholds
// do not resolve keeps no set and is reported.
func iconSet(rule *sheet.Rule, w *sheetReader, is *ooxml.Element, ns cfNames, ranges []sheet.Range) {
	if is == nil {
		return
	}
	name := attr(is, "iconSet")
	if name == "" {
		name = "3TrafficLights1"
	}
	valAttr := "val"
	if ns.rule == nsX14 {
		valAttr = "value"
	}
	vals, ok := w.cfvos(is, ns.rule, valAttr, ranges)
	if !ok {
		return
	}
	rule.Icons, rule.Values = name, vals
	rule.IconsReversed = boolAttr(is, "reverse")
	if v, ok := is.Attr("showValue"); ok {
		rule.IconsOnly = !ooxml.OnOff(v, true)
	}
	for _, k := range is.Kids() {
		switch {
		case k.Is(ns.rule, "cfvo"):
			above := false
			if v, ok := k.Attr("gte"); ok {
				above = !ooxml.OnOff(v, true)
			}
			rule.IconsAbove = append(rule.IconsAbove, above)
		case k.Is(nsX14, "cfIcon"):
			if len(rule.IconsCustom) < len(vals) {
				rule.IconsCustom = append(rule.IconsCustom, attr(k, "iconSet")+":"+attr(k, "iconId"))
			}
		}
	}
	if !boolAttr(is, "custom") {
		rule.IconsCustom = nil
	}
}
