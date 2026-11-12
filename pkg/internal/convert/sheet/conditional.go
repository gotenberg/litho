package sheet

import (
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
)

// The caps of conditional formatting. Anti-DoS guard, not spec-mandated:
// a sheet holds a few dozen conditional formats over a few ranges each,
// and the statistics of a format (its range's values) read at most
// maxCondCells cells; past it, the rest of the range is left out of the
// minimum, maximum, average, rank and duplicate counts.
const (
	maxConditionals = 1024
	maxCondRanges   = 4096
	maxCondCells    = 1 << 20
	maxCondStyles   = 1 << 14
)

// dataBarInset is the room Calc leaves between a data bar and its cell's
// edges, in points.
//
// # Corpus evidence
//
//   - libreoffice/xlsx: sc/databar.xlsx, rows 12.65 points high, prints bars 12.25 to
//     12.5 points high and starts the bar of B6 0.35 points right of the
//     column's edge (ScOutputData::DrawDataBars, getDataBarRect, 2 pixels
//     at the print scale).
const dataBarInset = 0.3

// dataBarAxis is the zero axis Calc draws across a cell of a data bar whose
// range holds negative and positive values: a fine dashed black line.
//
// # Corpus evidence
//
//   - libreoffice/xlsx: sc/databar.xlsx, B3:B6 holding -2 to 3, prints a black line
//     a quarter point wide dotted every quarter point at 2/5 of the cells'
//     width.
var dataBarAxis = compose.Stroke{Color: compose.Black, Width: 0.25, Dash: []float64{0.25, 0.25}}

// negativeBar is the colour of the bar of a negative value, Calc's default
// negative colour (ScDataBarFormatData, mxNegativeColor COL_LIGHTRED, the
// red sc/databar.xlsx prints).
var negativeBar = css.Color{R: 1, A: 1}

// positiveBar is the colour of a data bar whose rule names none.
var positiveBar = css.Color{R: 0x63 / 255.0, G: 0x8E / 255.0, B: 0xC6 / 255.0, A: 1}

// unprioritized is the sort key the rules without a priority start from:
// past any priority a sheet's rules give, so that they follow them in
// file order.
const unprioritized = maxConditionals * 1024

// condRule is one rule of a sheet with the format it belongs to and its
// place in the evaluation order.
type condRule struct {
	f    *condFormat
	rule *Rule
	key  int
}

// condFormat is a conditional format of a sheet: its ranges, clamped to
// the sheet, their bounding box, and the statistics of its values, read on
// first use.
type condFormat struct {
	// index is the format's index in the sheet's Conditional.
	index  int
	ranges []Range
	box    Range
	stats  *condStats
}

// condStats are the values of a conditional format's range: the numbers,
// sorted, their mean, and how many cells hold each value.
type condStats struct {
	nums  []float64
	mean  float64
	count map[string]int
	// formula computes a threshold given as a formula, nil for none.
	formula func(string) (float64, bool)
}

// condResult is what the rules give a cell: a differential style, a
// colour scale's fill, a data bar.
type condResult struct {
	style    *Style
	scale    css.Color
	hasScale bool
	bar      *Rule
	barF     *condFormat
	icon     *Rule
	iconF    *condFormat
}

// condStyleKey caches the styles the rules make.
type condStyleKey struct {
	base, diff *Style
	scale      css.Color
	hasScale   bool
}

// conditions is the conditional formatting of one sheet.
type conditions struct {
	rules  []condRule
	styles map[condStyleKey]*Style
	// doc and sheet place the sheet in its book, eval computes the
	// rules' formulas, made on first use.
	doc   *Document
	sheet int
	eval  *formulaEval
}

// newConditions indexes the conditional formats of sh, the rules sorted
// in the order Calc evaluates them: by priority where the file gives one,
// else in file order.
func newConditions(doc *Document, sh *Sheet) *conditions {
	if len(sh.Conditional) == 0 {
		return nil
	}
	cs := &conditions{styles: map[condStyleKey]*Style{}, doc: doc, sheet: -1}
	for i, s := range doc.Sheets {
		if s == sh {
			cs.sheet = i
		}
	}
	nRanges := 0
	for i := range sh.Conditional {
		if i >= maxConditionals {
			break
		}
		c := &sh.Conditional[i]
		f := &condFormat{index: i, box: Range{FirstRow: maxRows, FirstColumn: maxColumns, LastRow: -1, LastColumn: -1}}
		for _, rg := range c.Ranges {
			if nRanges >= maxCondRanges {
				break
			}
			rg = clampRange(rg)
			if rg.LastRow < rg.FirstRow || rg.LastColumn < rg.FirstColumn {
				continue
			}
			nRanges++
			f.ranges = append(f.ranges, rg)
			f.box.FirstRow, f.box.LastRow = min(f.box.FirstRow, rg.FirstRow), max(f.box.LastRow, rg.LastRow)
			f.box.FirstColumn, f.box.LastColumn = min(f.box.FirstColumn, rg.FirstColumn), max(f.box.LastColumn, rg.LastColumn)
		}
		if len(f.ranges) == 0 {
			continue
		}
		for j := range c.Rules {
			r := &c.Rules[j]
			if !evaluable(r) {
				continue
			}
			key := len(cs.rules) + unprioritized
			if r.Priority > 0 {
				key = r.Priority
			}
			cs.rules = append(cs.rules, condRule{f: f, rule: r, key: key})
		}
	}
	sort.SliceStable(cs.rules, func(i, j int) bool { return cs.rules[i].key < cs.rules[j].key })
	if len(cs.rules) == 0 {
		return nil
	}
	return cs
}

// evaluable reports a rule the builder evaluates: its operands constants,
// its kind one of those whose operands are the cell's value.
func evaluable(r *Rule) bool {
	switch r.Kind {
	case RuleExpression:
		return r.Formula != ""
	case RuleIconSet:
		set := iconSets[r.Icons]
		return r.Formula == "" && len(set) > 0 && len(r.Values) == len(set)
	case RuleCellIs:
		return r.Formula == "" && len(r.Values) > 0
	case RuleText:
		return r.Formula == "" && len(r.Values) > 0
	case RuleColorScale:
		return r.Formula == "" && len(r.Scale) >= 2 && len(r.Scale) <= 3 && (len(r.Values) == 0 || len(r.Values) == len(r.Scale))
	case RuleDataBar:
		return r.Formula == "" && (len(r.Values) == 0 || len(r.Values) == 2)
	case RuleTop, RuleBottom:
		return len(r.Values) > 0 && r.Values[0].Kind == Number
	case RuleDuplicates, RuleUniques, RuleBlanks, RuleNoBlanks, RuleAboveAverage, RuleBelowAverage:
		return true
	}
	return false
}

// unevaluated returns how many rules of the sheet of g the builder does
// not evaluate: icon sets of an unknown set, rules with a formula operand, and expressions
// whose formula the evaluator does not compute at the format's first
// cell.
func unevaluated(g *grid) int {
	n := 0
	for ci, c := range g.sh.Conditional {
		for i := range c.Rules {
			r := &c.Rules[i]
			if !evaluable(r) {
				n++
				continue
			}
			if r.Kind == RuleExpression && g.cond != nil && len(c.Ranges) > 0 {
				a := c.Ranges[0]
				if _, ok := g.cond.formula(r.Formula, ci, a.FirstRow, a.FirstColumn); !ok {
					n++
				}
			}
		}
	}
	return n
}

// formula evaluates the formula of a rule of the conditional format ci at
// the cell (r, c): its relative references move from the format's first
// cell to the cell (ECMA-376 Part 1, 18.3.1.10: a formula is relative to
// the top-left cell of the first range).
func (cs *conditions) formula(f string, ci, r, c int) (fval, bool) {
	if cs.sheet < 0 || ci < 0 || ci >= len(cs.doc.Sheets[cs.sheet].Conditional) {
		return fval{}, false
	}
	rgs := cs.doc.Sheets[cs.sheet].Conditional[ci].Ranges
	if len(rgs) == 0 {
		return fval{}, false
	}
	if cs.eval == nil {
		cs.eval = newFormulaEval(cs.doc)
	}
	return cs.eval.evalAt(f, cs.sheet, r, c, r-rgs[0].FirstRow, c-rgs[0].FirstColumn)
}

// expressionHolds evaluates an expression rule at the cell (r, c): it
// holds for TRUE or a number other than 0 (ScConditionEntry::IsValid,
// ScConditionMode::Direct).
func (cs *conditions) expressionHolds(cr *condRule, r, c int) bool {
	v, ok := cs.formula(cr.rule.Formula, cr.f.index, r, c)
	if !ok {
		return false
	}
	x := scalar(v)
	switch x.Kind {
	case Number, Bool:
		return x.Number != 0
	case Text, Error, Empty:
	}
	return false
}

// contains reports a cell of the format's ranges.
func (f *condFormat) contains(r, c int) bool {
	if r < f.box.FirstRow || r > f.box.LastRow || c < f.box.FirstColumn || c > f.box.LastColumn {
		return false
	}
	for _, rg := range f.ranges {
		if r >= rg.FirstRow && r <= rg.LastRow && c >= rg.FirstColumn && c <= rg.LastColumn {
			return true
		}
	}
	return false
}

// statistics returns the values of the format's ranges, read once: the
// cells with a value, each cell once.
func (f *condFormat) statistics(g *grid) *condStats {
	if f.stats != nil {
		return f.stats
	}
	st := &condStats{count: map[string]int{}}
	seen := map[[2]int]bool{}
	read := 0
	for _, rg := range f.ranges {
		i := sort.SearchInts(g.rowOrder, rg.FirstRow)
		for ; i < len(g.rowOrder) && g.rowOrder[i] <= rg.LastRow && read < maxCondCells; i++ {
			ri := g.rowOrder[i]
			row := g.rows[ri]
			for k := range row.Cells {
				cell := &row.Cells[k]
				if cell.Column < rg.FirstColumn || cell.Column > rg.LastColumn || !hasContent(cell) {
					continue
				}
				if len(f.ranges) > 1 {
					if seen[[2]int{ri, cell.Column}] {
						continue
					}
					seen[[2]int{ri, cell.Column}] = true
				}
				read++
				switch cell.Value.Kind {
				case Number, Bool:
					st.nums = append(st.nums, cell.Value.Number)
				case Text, Empty, Error:
				}
				if k, ok := valueKey(cell); ok {
					st.count[k]++
				}
			}
		}
	}
	sort.Float64s(st.nums)
	sum := 0.0
	for _, v := range st.nums {
		sum += v
	}
	if len(st.nums) > 0 {
		st.mean = sum / float64(len(st.nums))
	}
	if g.cond != nil && len(f.ranges) > 0 {
		cs, index, first := g.cond, f.index, f.ranges[0]
		st.formula = func(src string) (float64, bool) {
			v, ok := cs.formula(src, index, first.FirstRow, first.FirstColumn)
			x := scalar(v)
			return x.Number, ok && (x.Kind == Number || x.Kind == Bool)
		}
	}
	f.stats = st
	return st
}

// valueKey returns the key two cells holding the same value share for the
// duplicate rules: a number by its value, a text by its lower case.
func valueKey(cell *Cell) (string, bool) {
	if !hasContent(cell) {
		return "", false
	}
	switch cell.Value.Kind {
	case Number, Bool:
		return "n" + strconv.FormatFloat(cell.Value.Number, 'g', -1, 64), true
	case Text:
		return "s" + strings.ToLower(cell.Value.Text), true
	case Error:
		return "e" + cell.Value.Text, true
	case Empty:
	}
	return "", false
}

// evaluate returns what the rules give the cell at (r, c), nil for
// nothing. Calc applies the style of the first rule that holds and the
// first colour scale and data bar (ScConditionalFormat::GetData,
// ScFillInfo); a rule that holds with StopIfTrue ends the evaluation.
func (cs *conditions) evaluate(g *grid, r, c int, cell *Cell) *condResult {
	var res condResult
	hit := false
	for i := range cs.rules {
		cr := &cs.rules[i]
		if !cr.f.contains(r, c) {
			continue
		}
		rule := cr.rule
		holds := false
		switch rule.Kind {
		case RuleColorScale:
			if res.hasScale {
				continue
			}
			if col, ok := colorScale(rule, cr.f.statistics(g), cell); ok {
				res.scale, res.hasScale, holds = col, true, true
			}
		case RuleDataBar:
			if res.bar != nil {
				continue
			}
			if cell != nil && (cell.Value.Kind == Number || cell.Value.Kind == Bool) {
				res.bar, res.barF, holds = rule, cr.f, true
			}
		case RuleExpression:
			if res.style != nil {
				continue
			}
			if holds = cs.expressionHolds(cr, r, c); holds && rule.Style != nil {
				res.style = rule.Style
			}
		case RuleIconSet:
			if res.icon != nil {
				continue
			}
			if cell != nil && (cell.Value.Kind == Number || cell.Value.Kind == Bool) {
				res.icon, res.iconF, holds = rule, cr.f, true
			}
		case RuleCellIs, RuleText, RuleBlanks, RuleNoBlanks, RuleDuplicates, RuleUniques,
			RuleTop, RuleBottom, RuleAboveAverage, RuleBelowAverage:
			if res.style != nil {
				continue
			}
			if holds = cellHolds(rule, cr.f, g, cell); holds && rule.Style != nil {
				res.style = rule.Style
			}
		}
		hit = hit || holds
		if holds && rule.StopIfTrue {
			break
		}
	}
	if !hit {
		return nil
	}
	return &res
}

// cellArg returns the cell's value as a rule compares it: a number, a
// boolean as 0 or 1, an empty cell as 0 when the rule's first operand is
// not a text, else as the empty text; false for an error, which no
// comparison holds for (ScConditionEntry::IsCellValid,
// lcl_GetCellContent).
func cellArg(cell *Cell, textOperand bool) (num float64, str string, isNum, ok bool) {
	if !hasContent(cell) {
		return 0, "", !textOperand, true
	}
	switch cell.Value.Kind {
	case Number, Bool:
		return cell.Value.Number, "", true, true
	case Text:
		return 0, cell.Value.Text, false, true
	case Error, Empty:
	}
	return 0, "", false, false
}

// cellHolds evaluates a rule whose operands are the cell's value and
// constants (ECMA-376 Part 1, 18.3.1.10; ODF 1.3 Part 3, 19.509
// calcext:value; ScConditionEntry::IsValid and IsValidStr).
func cellHolds(rule *Rule, f *condFormat, g *grid, cell *Cell) bool {
	switch rule.Kind {
	case RuleBlanks, RuleNoBlanks:
		blank := !hasContent(cell) || (cell.Value.Kind == Text && strings.TrimSpace(cell.Value.Text) == "")
		return blank == (rule.Kind == RuleBlanks)
	case RuleCellIs:
		return cellIs(rule, cell)
	case RuleText:
		num, str, isNum, ok := cellArg(cell, true)
		if !ok {
			return false
		}
		if isNum {
			str = strconv.FormatFloat(num, 'g', -1, 64)
		}
		return textHolds(rule.Operator, str, operandText(rule.Values[0]))
	case RuleDuplicates, RuleUniques:
		k, ok := valueKey(cell)
		if !ok {
			return false
		}
		n := f.statistics(g).count[k]
		return (n > 1) == (rule.Kind == RuleDuplicates)
	case RuleTop, RuleBottom:
		num, _, isNum, ok := cellArg(cell, false)
		if !ok || !isNum {
			return false
		}
		return ranked(f.statistics(g).nums, num, rule.Values[0].Number, rule.Kind == RuleTop)
	case RuleAboveAverage, RuleBelowAverage:
		num, _, isNum, ok := cellArg(cell, false)
		st := f.statistics(g)
		if !ok || !isNum || len(st.nums) == 0 {
			return false
		}
		if rule.Kind == RuleAboveAverage {
			return num > st.mean
		}
		return num < st.mean
	case RuleExpression, RuleIconSet, RuleColorScale, RuleDataBar:
	}
	return false
}

// operandText returns a constant operand as text.
func operandText(v Value) string {
	if v.Kind == Number || v.Kind == Bool {
		return strconv.FormatFloat(v.Number, 'g', -1, 64)
	}
	return v.Text
}

// textHolds tests the text operators, ignoring case as Calc's import of a
// workbook sets them (ScConditionEntry::IsValidStr).
func textHolds(op, s, arg string) bool {
	s, arg = strings.ToLower(s), strings.ToLower(arg)
	switch op {
	case "beginsWith":
		return strings.HasPrefix(s, arg)
	case "endsWith":
		return strings.HasSuffix(s, arg)
	case "notContains", "notContainsText":
		return !strings.Contains(s, arg)
	}
	return strings.Contains(s, arg)
}

// cellIs compares the cell's value with the rule's operands: numbers with
// numbers, texts with texts ignoring case; a text against a number holds
// only for "notEqual", as in Calc; "between" takes its bounds in either
// order.
func cellIs(rule *Rule, cell *Cell) bool {
	op := rule.Operator
	a := rule.Values[0]
	textOp := a.Kind == Text
	num, str, isNum, ok := cellArg(cell, textOp)
	if !ok {
		return false
	}
	two := op == "between" || op == "notBetween"
	if two && len(rule.Values) < 2 {
		return false
	}
	if isNum {
		if textOp || (two && rule.Values[1].Kind == Text) {
			return op == "notEqual"
		}
		lo := a.Number
		if two {
			lo, hi := lo, rule.Values[1].Number
			if lo > hi {
				lo, hi = hi, lo
			}
			in := (num > lo || approxEqual(num, lo)) && (num < hi || approxEqual(num, hi))
			return in == (op == "between")
		}
		return compare(op, cmpFloat(num, lo))
	}
	if !textOp {
		return op == "notEqual"
	}
	if two {
		lo, hi := strings.ToLower(a.Text), strings.ToLower(operandText(rule.Values[1]))
		if lo > hi {
			lo, hi = hi, lo
		}
		s := strings.ToLower(str)
		in := s >= lo && s <= hi
		return in == (op == "between")
	}
	return compare(op, strings.Compare(strings.ToLower(str), strings.ToLower(a.Text)))
}

// compare applies a comparison operator to the sign of a comparison.
func compare(op string, cmp int) bool {
	switch op {
	case "lessThan":
		return cmp < 0
	case "lessThanOrEqual":
		return cmp <= 0
	case "equal":
		return cmp == 0
	case "notEqual":
		return cmp != 0
	case "greaterThanOrEqual":
		return cmp >= 0
	case "greaterThan":
		return cmp > 0
	}
	return false
}

// cmpFloat compares two numbers, equal when they agree to the precision
// Calc compares them at (rtl::math::approxEqual).
func cmpFloat(a, b float64) int {
	switch {
	case approxEqual(a, b):
		return 0
	case a < b:
		return -1
	}
	return 1
}

// approxEqual reports two numbers equal but for their last few bits.
func approxEqual(a, b float64) bool {
	if a == b {
		return true
	}
	d := math.Abs(a - b)
	return d < math.Max(math.Abs(a), math.Abs(b))*(1.0/(1<<44))
}

// ranked reports a value among the n largest (top) or smallest of the
// sorted nums: fewer than n values beat it, ties all holding
// (ScConditionEntry::IsTopNElement, IsBottomNElement).
func ranked(nums []float64, v, n float64, top bool) bool {
	if n < 1 {
		return false
	}
	beat := sort.SearchFloat64s(nums, v)
	if top {
		beat = len(nums) - sort.Search(len(nums), func(i int) bool { return nums[i] > v })
	}
	return float64(beat) < n
}

// thresholds returns the numbers of a scale's thresholds: each value's
// number, the range's minimum for "min" and maximum for "max", a formula's
// result for a text starting with "="; with no values, the minimum, the
// median when three, and the maximum.
func thresholds(vals []Value, n int, st *condStats) ([]float64, bool) {
	if len(st.nums) == 0 {
		return nil, false
	}
	lo, hi := st.nums[0], st.nums[len(st.nums)-1]
	out := make([]float64, n)
	if len(vals) == 0 {
		out[0], out[n-1] = lo, hi
		if n == 3 {
			out[1] = Percentile(st.nums, 50)
		}
		return out, true
	}
	for i := range out {
		v := vals[i]
		switch {
		case v.Kind == Number:
			out[i] = v.Number
		case v.Kind == Text && v.Text == "min":
			out[i] = lo
		case v.Kind == Text && v.Text == "max":
			out[i] = hi
		case v.Kind == Text && strings.HasPrefix(v.Text, "=") && st.formula != nil:
			x, ok := st.formula(v.Text[1:])
			if !ok {
				return nil, false
			}
			out[i] = x
		default:
			return nil, false
		}
	}
	return out, true
}

// colorScale returns the colour a colour scale paints the cell with: the
// colours of the thresholds on either side of its number mixed in
// proportion, each channel moved from the lower colour by the truncated
// share of the difference: the band is the first whose top the value
// does not reach, or the last; at or past its top the top colour, at or
// below its bottom the bottom colour (ScColorScaleFormat::GetColor, CalcColor). A cell without
// a number is not painted.
//
// # Corpus evidence
//
//   - libreoffice/xlsx: sc/colorscale.xlsx, red to blue over 1 to 4, prints 2 as
//     (170, 0, 85); yellow to cyan between the 10th percentile and 90% of
//     1 to 4 (1.3 and 3.7) prints 2 as (181, 255, 74) and 3 as (75, 255,
//     180).
//   - libreoffice/xlsx: sc/tdf156028.xlsx, a three-colour scale over the one value 1,
//     paints it the maximum's green.
func colorScale(rule *Rule, st *condStats, cell *Cell) (css.Color, bool) {
	if cell == nil || (cell.Value.Kind != Number && cell.Value.Kind != Bool) {
		return css.Color{}, false
	}
	th, ok := thresholds(rule.Values, len(rule.Scale), st)
	if !ok {
		return css.Color{}, false
	}
	v := cell.Value.Number
	// The band moves on while the value reaches its top; at or past the
	// top, the top colour wins, then at or below the bottom, the bottom
	// colour.
	i := 1
	for i < len(th)-1 && v >= th[i] {
		i++
	}
	lo, hi := th[i-1], th[i]
	switch {
	case v >= hi:
		return rule.Scale[i], true
	case v <= lo:
		return rule.Scale[i-1], true
	}
	return mixColor(rule.Scale[i-1], rule.Scale[i], (v-lo)/(hi-lo)), true
}

// mixColor moves each channel of a towards b by the share f, truncated to
// a whole step of 1/255.
func mixColor(a, b css.Color, f float64) css.Color {
	ch := func(x, y float64) float64 {
		x8, y8 := math.Round(x*255), math.Round(y*255)
		return (x8 + math.Trunc((y8-x8)*f)) / 255
	}
	return css.Color{R: ch(a.R, b.R), G: ch(a.G, b.G), B: ch(a.B, b.B), A: 1}
}

// overlayStyle returns base with the differential style diff over it and
// the colour scale's fill: a font colour, bold, italic, an underline or a
// strike diff sets, its fill, its border sides, its number format.
func (cs *conditions) overlayStyle(base *Style, res *condResult) *Style {
	key := condStyleKey{base: base, diff: res.style, scale: res.scale, hasScale: res.hasScale}
	if st, ok := cs.styles[key]; ok {
		return st
	}
	st := *base
	if d := res.style; d != nil {
		if d.Font.Color.A > 0 {
			st.Font.Color = d.Font.Color
		}
		st.Font.Bold = st.Font.Bold || d.Font.Bold
		st.Font.Italic = st.Font.Italic || d.Font.Italic
		st.Font.Strike = st.Font.Strike || d.Font.Strike
		if d.Font.Underline != NoUnderline {
			st.Font.Underline = d.Font.Underline
		}
		if len(d.Font.Families) > 0 {
			st.Font.Families = d.Font.Families
		}
		if d.Font.Size > 0 {
			st.Font.Size = d.Font.Size
		}
		if d.Fill.Color.A > 0 {
			st.Fill = d.Fill
		}
		for i, b := range d.Border {
			if b.Line != LineNone {
				st.Border[i] = b
			}
		}
		if d.Format != "" {
			st.Format = d.Format
		}
	}
	if res.hasScale {
		st.Fill.Color = res.scale
	}
	if len(cs.styles) < maxCondStyles {
		cs.styles[key] = &st
	}
	return &st
}

// condStyle returns the style the cell at (r, c) draws with once the
// sheet's conditional formats apply over its own style base.
func (g *grid) condStyle(r, c int, cell *Cell, base *Style) *Style {
	if g.cond == nil {
		return base
	}
	res := g.cond.evaluate(g, r, c, cell)
	if res == nil || (res.style == nil && !res.hasScale) {
		return base
	}
	return g.cond.overlayStyle(base, res)
}

// dataBar returns the data bar of the cell at (r, c): where zero sits and
// the bar's signed length, each a fraction (zero of the cell, the length
// of zero's side), its colour, and whether it is painted solid; false for
// none. Calc's rules by the rule's axis (ScDataBarFormat::GetDataBarInfo):
// automatic puts zero between the thresholds, a positive range's at the
// left edge; middle at half, each side the larger magnitude, the bar at
// most BarMaxLength of it; none from the left edge, BarMinLength at the
// lower threshold to BarMaxLength at the upper.
//
// # Corpus evidence
//
//   - libreoffice/xlsx: sc/databar.xlsx: over -2 to 3 the axis sits at 2/5 of the
//     cell, 1 paints a third of the positive side and -2 the whole
//     negative side in red; over 1 to 4 with the minimum as the lower
//     threshold, 1 paints nothing and 4 the whole width; a middle axis
//     over 1 to 4 paints 4 across 90% of the right half, 1 a quarter of
//     that.
func (g *grid) dataBar(r, c int, cell *Cell) (zero, length float64, col css.Color, solid, ok bool) {
	if g.cond == nil {
		return 0, 0, css.Color{}, false, false
	}
	res := g.cond.evaluate(g, r, c, cell)
	if res == nil || res.bar == nil {
		return 0, 0, css.Color{}, false, false
	}
	rule := res.bar
	th, ok := thresholds(rule.Values, 2, res.barF.statistics(g))
	if !ok || !ordered(th[0], th[1]) {
		return 0, 0, css.Color{}, false, false
	}
	lo, hi := th[0], th[1]
	minL, maxL := rule.BarMinLength, rule.BarMaxLength
	if minL == 0 && maxL == 0 {
		minL, maxL = 0.1, 0.9
	}
	v := cell.Value.Number
	switch rule.BarAxis {
	case BarAxisNone:
		switch {
		case v <= lo:
			length = minL
		case v >= hi:
			length = maxL
		default:
			length = minL + (v-lo)/(hi-lo)*(maxL-minL)
		}
	case BarAxisMiddle:
		zero = 0.5
		absMax := max(math.Abs(lo), math.Abs(hi))
		if absMax == 0 {
			return 0, 0, css.Color{}, false, false
		}
		switch {
		case v < 0 && lo < 0:
			length = maxL * max(v, lo) / absMax
		case v > hi:
			length = maxL * hi / absMax
		default:
			length = maxL * max(v, lo) / absMax
		}
	case BarAxisAuto:
		if lo < 0 {
			zero = 1
			if hi >= 0 {
				zero = -lo / (hi - lo)
			}
		}
		pos, neg := max(0, lo), min(0, hi)
		switch {
		case v < 0 && lo < 0:
			length = -1
			if v >= lo {
				length = -(v - neg) / (lo - neg)
			}
		case v > hi:
			length = 1
		case v <= lo:
			length = 0
		default:
			length = (v - pos) / (hi - pos)
		}
	}
	if !finite(length) || length == 0 {
		return 0, 0, css.Color{}, false, false
	}
	col = rule.Bar
	if col.A == 0 {
		col = positiveBar
	}
	if v < 0 {
		col = rule.BarNegative
		if col.A == 0 {
			col = negativeBar
		}
	}
	return zero, length, col, rule.BarSolid, true
}

// drawDataBar paints the data bar of the unit u, if any, as Calc draws it
// under the cell's text (sc/source/ui/view/output.cxx, drawDataBars): in
// the cell inset from its edges, from zero across its share of zero's
// side, a gradient from the bar's colour at zero to transparent at the
// value's end or a flat fill, with the zero axis over it when zero is
// inside the cell.
func (b *pageBuilder) drawDataBar(u *unit) {
	zero, length, col, solid, ok := b.g.dataBar(u.r, u.c, u.cell)
	if !ok {
		return
	}
	x0 := u.x + dataBarInset
	w := u.w - 2*dataBarInset
	y := u.y + dataBarInset
	h := u.h - 2*dataBarInset
	if w <= 0 || h <= 0 {
		return
	}
	at := x0 + w*zero
	var a, z float64
	if length < 0 {
		a, z = at, at+(at-x0)*length
	} else {
		a, z = at, at+(x0+w-at)*length
	}
	if math.Abs(z-a) > epsilon {
		left, right := min(a, z), max(a, z)
		c := colorOf(col)
		fill := &compose.Fill{Color: c}
		if !solid {
			clear := c
			clear.A = 0
			fill = &compose.Fill{Shading: &compose.Shading{
				Kind: compose.Axial, From: compose.Point{X: a, Y: y}, To: compose.Point{X: z, Y: y},
				Stops:     []compose.ColorStop{{Offset: 0, Color: c}, {Offset: 1, Color: clear}},
				Transform: compose.Identity,
			}}
		}
		b.fills = append(b.fills, &compose.Path{Ops: rectOps(left, y, right-left, h), Fill: fill})
	}
	if zero > 0 && zero < 1 {
		stroke := dataBarAxis
		b.fills = append(b.fills, &compose.Path{Ops: lineOps(at, y, at, y+h), Stroke: &stroke})
	}
}

// RangeNumbers returns the numbers of the cells of sh in the ranges,
// sorted, each cell once: what a reader resolves the thresholds of a
// colour scale or a data bar against. At most maxCondCells cells are
// read.
func RangeNumbers(sh *Sheet, ranges []Range) []float64 {
	var out []float64
	seen := map[[2]int]bool{}
	read := 0
	for _, rg := range ranges {
		rg = clampRange(rg)
		i := sort.Search(len(sh.Rows), func(i int) bool { return sh.Rows[i].Index >= rg.FirstRow })
		for ; i < len(sh.Rows) && sh.Rows[i].Index <= rg.LastRow && read < maxCondCells; i++ {
			row := &sh.Rows[i]
			for k := range row.Cells {
				cell := &row.Cells[k]
				if cell.Column < rg.FirstColumn || cell.Column > rg.LastColumn {
					continue
				}
				if cell.Value.Kind != Number && cell.Value.Kind != Bool {
					continue
				}
				key := [2]int{row.Index, cell.Column}
				if seen[key] {
					continue
				}
				seen[key] = true
				read++
				out = append(out, cell.Value.Number)
			}
		}
	}
	sort.Float64s(out)
	return out
}

// Threshold returns the number a threshold of a colour scale or a data
// bar stands for over the sorted numbers nums of its range: kind is
// "min", "max", "autoMin" (the minimum, or 0 when it is positive),
// "autoMax" (the maximum, or 0 when it is negative), "percent" (v percent
// of the way from the minimum to the maximum), "percentile" (the v-th
// percentile) or "num" (v itself), as ECMA-376 Part 1, 18.18.13
// ST_CfvoType and ODF 1.3 Part 3, 19.678 calcext:type name them
// (ScColorScaleEntry, ScColorScaleFormat::CalcValue); false for another
// kind or a range without a number.
func Threshold(kind string, v float64, nums []float64) (float64, bool) {
	if kind == "num" {
		return v, true
	}
	if len(nums) == 0 {
		return 0, false
	}
	lo, hi := nums[0], nums[len(nums)-1]
	switch kind {
	case "min":
		return lo, true
	case "max":
		return hi, true
	case "autoMin":
		return min(lo, 0), true
	case "autoMax":
		return max(hi, 0), true
	case "percent":
		return lo + (hi-lo)*v/100, true
	case "percentile":
		return Percentile(nums, v), true
	}
	return 0, false
}

// Percentile returns the p-th percentile of the sorted nums, interpolated
// between the two closest ranks as Calc's PERCENTILE computes it.
func Percentile(nums []float64, p float64) float64 {
	if len(nums) == 0 {
		return 0
	}
	p = min(max(p, 0), 100) / 100
	pos := p * float64(len(nums)-1)
	i := int(math.Floor(pos))
	if i >= len(nums)-1 {
		return nums[len(nums)-1]
	}
	return nums[i] + (pos-float64(i))*(nums[i+1]-nums[i])
}

// iconAt returns the icon a cell shows by its icon set rule, and whether
// the rule hides the cell's value: the icon of the highest threshold the
// cell's number reaches, or passes for a threshold that asks it to, the
// first below them all (ScIconSetFormat::GetIconSetData); nil for none.
func (g *grid) iconAt(r, c int, cell *Cell) (icon, bool) {
	if g.cond == nil {
		return nil, false
	}
	res := g.cond.evaluate(g, r, c, cell)
	if res == nil || res.icon == nil {
		return nil, false
	}
	rule := res.icon
	th, ok := thresholds(rule.Values, len(rule.Values), res.iconF.statistics(g))
	if !ok {
		return nil, false
	}
	v := cell.Value.Number
	band := 0
	for k := 1; k < len(th); k++ {
		above := k < len(rule.IconsAbove) && rule.IconsAbove[k]
		if v > th[k] || (!above && v == th[k]) {
			band = k
			continue
		}
		break
	}
	return iconOf(rule, band), rule.IconsOnly
}

// drawIcon draws the icon of the cell of u at its bottom-left corner, the
// size of its font's icon, as Calc paints an icon set's icon, or as Excel
// does under its rules.
func (b *pageBuilder) drawIcon(u *unit) {
	ic, _ := b.g.iconAt(u.r, u.c, u.cell)
	if ic == nil {
		return
	}
	f := resolveFont(u.st.Font, b.g.doc.Default.Font)
	s := iconSide(f.size)
	if s <= 0 || u.w <= 0 || u.h <= 0 {
		return
	}
	if b.g.doc.ExcelRules {
		s = f.size * excelIconScale
		if s > u.h {
			s = u.h
		}
		b.fills = append(b.fills, placeIcon(ic, u.x+excelIconInsetX, u.y+u.h-excelIconInsetY-s, s))
		return
	}
	b.fills = append(b.fills, placeIcon(ic, u.x+iconInsetX, u.y+u.h-iconInsetY-s, s))
}

// The icon Excel draws for an icon set's rule: a side of 1.08 times the
// cell's font size, at most the row's height, 1.7 points right of the
// cell's left edge (past its grid line) and 0.2 points above its bottom.
//
// # Corpus evidence
//
//   - litho/xlsx: cf_icon_sets, Calibri 11 at a 92.7% fit, draws its icons
//     11.04 points square, 0.7 points right of the grid line and 0.18
//     above the row's bottom edge.
const (
	excelIconScale  = 1.08
	excelIconInsetX = 1.7
	excelIconInsetY = 0.2
)
