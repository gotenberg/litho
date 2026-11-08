package sheet

import (
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
)

var (
	green = css.Color{R: 0x92 / 255.0, G: 0xD0 / 255.0, B: 0x50 / 255.0, A: 1}
	red   = css.Color{R: 1, A: 1}
	blue  = css.Color{B: 1, A: 1}
)

// num and str are constant operands.
func num(v float64) Value { return Value{Kind: Number, Number: v} }
func str(s string) Value  { return Value{Kind: Text, Text: s} }

// column returns a sheet whose column A holds the cells.
func column(cells ...Cell) *Sheet {
	sh := &Sheet{Name: "S"}
	for i, c := range cells {
		c.Column = 0
		sh.Rows = append(sh.Rows, Row{Index: i, Cells: []Cell{c}})
	}
	return sh
}

// TestConditional_Rules locks which cells each rule kind restyles, on
// column A of the values given, the rule over A1:A10.
func TestConditional_Rules(t *testing.T) {
	fill := &Style{Fill: Fill{Color: green}}
	nums := []Cell{numCell(0, 1, nil), numCell(0, 2, nil), numCell(0, 3, nil), numCell(0, 2, nil), {}, textCell(0, "Apple pie", nil), textCell(0, "  ", nil)}
	tests := []struct {
		name string
		rule Rule
		want []bool
	}{
		{"less than, an empty cell is 0", Rule{Kind: RuleCellIs, Operator: "lessThan", Values: []Value{num(2)}}, []bool{true, false, false, false, true, false, false}},
		{"between in either order", Rule{Kind: RuleCellIs, Operator: "between", Values: []Value{num(3), num(2)}}, []bool{false, true, true, true, false, false, false}},
		{"not equal holds for a text", Rule{Kind: RuleCellIs, Operator: "notEqual", Values: []Value{num(2)}}, []bool{true, false, true, false, true, true, true}},
		{"equal to a text ignores case", Rule{Kind: RuleCellIs, Operator: "equal", Values: []Value{str("apple PIE")}}, []bool{false, false, false, false, false, true, false}},
		{"contains text", Rule{Kind: RuleText, Operator: "containsText", Values: []Value{str("PIE")}}, []bool{false, false, false, false, false, true, false}},
		{"begins with", Rule{Kind: RuleText, Operator: "beginsWith", Values: []Value{str("app")}}, []bool{false, false, false, false, false, true, false}},
		{"not contains", Rule{Kind: RuleText, Operator: "notContains", Values: []Value{str("2")}}, []bool{true, false, true, false, true, true, true}},
		{"blanks, spaces too", Rule{Kind: RuleBlanks}, []bool{false, false, false, false, true, false, true}},
		{"no blanks", Rule{Kind: RuleNoBlanks}, []bool{true, true, true, true, false, true, false}},
		{"duplicates", Rule{Kind: RuleDuplicates}, []bool{false, true, false, true, false, false, false}},
		{"uniques", Rule{Kind: RuleUniques}, []bool{true, false, true, false, false, true, true}},
		{"top 1", Rule{Kind: RuleTop, Values: []Value{num(1)}}, []bool{false, false, true, false, false, false, false}},
		{"bottom 2, ties hold", Rule{Kind: RuleBottom, Values: []Value{num(2)}}, []bool{true, true, false, true, true, false, false}},
		{"above average", Rule{Kind: RuleAboveAverage}, []bool{false, false, true, false, false, false, false}},
		{"below average, an empty cell is 0", Rule{Kind: RuleBelowAverage}, []bool{true, false, false, false, true, false, false}},
		{"an expression is not applied", Rule{Kind: RuleExpression, Formula: "A1>1"}, []bool{false, false, false, false, false, false, false}},
		{"a formula operand is not applied", Rule{Kind: RuleCellIs, Operator: "equal", Values: []Value{num(1)}, Formula: "B1"}, []bool{false, false, false, false, false, false, false}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sh := column(nums...)
			rule := tc.rule
			rule.Style = fill
			sh.Conditional = []Conditional{{Ranges: []Range{{LastRow: 9}}, Rules: []Rule{rule}}}
			g := newGrid(newDoc(sh), sh)
			for i, want := range tc.want {
				got := g.style(i, 0, g.cell(i, 0)).Fill.Color == green
				if got != want {
					t.Errorf("A%d: restyled %v, want %v", i+1, got, want)
				}
			}
			if g.style(10, 0, nil).Fill.Color == green {
				t.Error("A11, outside the range, restyled")
			}
		})
	}
}

// TestConditional_Order locks the evaluation order: the lowest priority
// first, the first style that holds winning whole, StopIfTrue ending the
// rules after it, the differential style over the cell's own.
func TestConditional_Order(t *testing.T) {
	bold := &Style{Font: Font{Bold: true}}
	redText := &Style{Font: Font{Color: red}, Format: "0.00"}
	own := &Style{Font: Font{Italic: true, Size: 12}, Fill: Fill{Color: blue}}
	tests := []struct {
		name  string
		rules []Rule
		check func(*Style) bool
	}{
		{"the lower priority wins", []Rule{
			{Kind: RuleCellIs, Operator: "greaterThan", Values: []Value{num(0)}, Priority: 2, Style: bold},
			{Kind: RuleCellIs, Operator: "greaterThan", Values: []Value{num(0)}, Priority: 1, Style: redText},
		}, func(s *Style) bool {
			return !s.Font.Bold && s.Font.Color == red && s.Format == "0.00" && s.Font.Italic && s.Fill.Color == blue
		}},
		{"file order without priorities", []Rule{
			{Kind: RuleCellIs, Operator: "greaterThan", Values: []Value{num(0)}, Style: bold},
			{Kind: RuleCellIs, Operator: "greaterThan", Values: []Value{num(0)}, Style: redText},
		}, func(s *Style) bool { return s.Font.Bold && s.Font.Color != red && s.Font.Size == 12 }},
		{"a rule that does not hold gives way", []Rule{
			{Kind: RuleCellIs, Operator: "lessThan", Values: []Value{num(0)}, Priority: 1, Style: bold},
			{Kind: RuleCellIs, Operator: "greaterThan", Values: []Value{num(0)}, Priority: 2, Style: redText},
		}, func(s *Style) bool { return !s.Font.Bold && s.Font.Color == red }},
		{"stop if true ends the colour scale after it", []Rule{
			{Kind: RuleCellIs, Operator: "greaterThan", Values: []Value{num(0)}, Priority: 1, StopIfTrue: true, Style: bold},
			{Kind: RuleColorScale, Priority: 2, Scale: []css.Color{red, green}},
		}, func(s *Style) bool { return s.Font.Bold && s.Fill.Color == blue }},
		{"a colour scale paints over the style's fill", []Rule{
			{Kind: RuleCellIs, Operator: "greaterThan", Values: []Value{num(0)}, Priority: 1, Style: &Style{Fill: Fill{Color: green}}},
			{Kind: RuleColorScale, Priority: 2, Scale: []css.Color{red, red}},
		}, func(s *Style) bool { return s.Fill.Color == red }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sh := column(numCell(0, 5, own))
			sh.Conditional = []Conditional{{Ranges: []Range{{}}, Rules: tc.rules}}
			g := newGrid(newDoc(sh), sh)
			if s := g.style(0, 0, g.cell(0, 0)); !tc.check(s) {
				t.Errorf("style %+v", *s)
			}
			if own.Font.Bold || own.Font.Color == red {
				t.Error("the cell's own style changed")
			}
		})
	}
}

// TestConditional_ColorScale locks the colour scale's interpolation and
// thresholds against what Calc prints for sc/colorscale.xlsx.
func TestConditional_ColorScale(t *testing.T) {
	c8 := func(r, g, b float64) css.Color { return css.Color{R: r / 255, G: g / 255, B: b / 255, A: 1} }
	yellow, cyan := c8(255, 255, 0), c8(0, 255, 255)
	tests := []struct {
		name   string
		rule   Rule
		values []float64
		want   []css.Color
	}{
		{"min to max", Rule{Kind: RuleColorScale, Scale: []css.Color{red, blue}}, []float64{1, 2, 3, 4}, []css.Color{red, c8(170, 0, 85), c8(85, 0, 170), blue}},
		{"numbers, truncated steps", Rule{Kind: RuleColorScale, Scale: []css.Color{yellow, cyan}, Values: []Value{num(1.3), num(3.7)}}, []float64{1, 2, 3, 4}, []css.Color{yellow, c8(181, 255, 74), c8(75, 255, 180), cyan}},
		{"three colours, median", Rule{Kind: RuleColorScale, Scale: []css.Color{red, green, blue}}, []float64{0, 5, 10}, []css.Color{red, green, blue}},
		{"min and max names", Rule{Kind: RuleColorScale, Scale: []css.Color{red, blue}, Values: []Value{str("min"), num(2)}}, []float64{0, 1, 2, 4}, []css.Color{red, c8(128, 0, 127), blue, blue}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var cells []Cell
			for _, v := range tc.values {
				cells = append(cells, numCell(0, v, nil))
			}
			cells = append(cells, textCell(0, "text", nil))
			sh := column(cells...)
			sh.Conditional = []Conditional{{Ranges: []Range{{LastRow: 9}}, Rules: []Rule{tc.rule}}}
			g := newGrid(newDoc(sh), sh)
			for i, want := range tc.want {
				got := g.style(i, 0, g.cell(i, 0)).Fill.Color
				if !sameColor(got, want) {
					t.Errorf("A%d: %v, want %v", i+1, got, want)
				}
			}
			if g.style(len(tc.values), 0, g.cell(len(tc.values), 0)).Fill.Color.A != 0 {
				t.Error("a text cell painted")
			}
		})
	}
}

// sameColor reports two colours within half a step of 1/255.
func sameColor(a, b css.Color) bool {
	d := func(x, y float64) bool { return math.Abs(x-y) < 0.5/255 }
	return d(a.R, b.R) && d(a.G, b.G) && d(a.B, b.B) && d(a.A, b.A)
}

// TestConditional_DataBar locks the data bar's extent, colour and axis
// against what Calc prints for sc/databar.xlsx.
func TestConditional_DataBar(t *testing.T) {
	tests := []struct {
		name     string
		values   []Value
		cells    []float64
		from, to []float64
		axis     float64
		negative []bool
	}{
		{"from the minimum", []Value{str("min"), str("max")}, []float64{1, 2, 4}, []float64{0, 0, 0}, []float64{0, 1.0 / 3, 1}, -1, []bool{false, false, false}},
		{"over zero, an axis", []Value{num(-2), num(3)}, []float64{1, 3, -2}, []float64{0.4, 0.4, 0.4}, []float64{0.6, 1, 0}, 0.4, []bool{false, false, true}},
		{"clamped to the thresholds", []Value{num(0), num(2)}, []float64{1, 5}, []float64{0, 0}, []float64{0.5, 1}, -1, []bool{false, false}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var cells []Cell
			for _, v := range tc.cells {
				cells = append(cells, numCell(0, v, nil))
			}
			sh := column(cells...)
			sh.Conditional = []Conditional{{Ranges: []Range{{LastRow: 9}}, Rules: []Rule{{Kind: RuleDataBar, Values: tc.values, Bar: blue}}}}
			g := newGrid(newDoc(sh), sh)
			for i := range tc.cells {
				from, to, col, axis, ok := g.dataBar(i, 0, g.cell(i, 0))
				if !ok || !near(from, tc.from[i], 1e-9) || !near(to, tc.to[i], 1e-9) || !near(axis, tc.axis, 1e-9) {
					t.Errorf("A%d: %v %v..%v axis %v, want %v..%v axis %v", i+1, ok, from, to, axis, tc.from[i], tc.to[i], tc.axis)
				}
				if (col == red) != tc.negative[i] {
					t.Errorf("A%d: colour %v", i+1, col)
				}
			}
			if _, _, _, _, ok := g.dataBar(9, 0, nil); ok {
				t.Error("an empty cell has a bar")
			}
		})
	}
}

// TestConvert_ConditionalDrawing locks what a page draws for the
// conditional formats: a restyled cell's fill, a data bar's gradient
// inset from its cell, and a warning for the rules not applied.
func TestConvert_ConditionalDrawing(t *testing.T) {
	sh := column(numCell(0, 1, nil), numCell(0, 4, nil))
	sh.Conditional = []Conditional{
		{Ranges: []Range{{LastRow: 1}}, Rules: []Rule{{Kind: RuleDataBar, Bar: blue, Values: []Value{num(0), num(4)}}}},
		{Ranges: []Range{{FirstRow: 1, LastRow: 1}}, Rules: []Rule{
			{Kind: RuleCellIs, Operator: "equal", Values: []Value{num(4)}, Style: &Style{Fill: Fill{Color: green}}},
			{Kind: RuleExpression, Formula: "TRUE()"},
		}},
	}
	out := convert(t, newDoc(sh), office.Options{})
	var bars, fills int
	var walk func([]compose.Fragment)
	walk = func(fs []compose.Fragment) {
		for _, f := range fs {
			switch v := f.(type) {
			case *compose.Path:
				if v.Fill != nil && v.Fill.Shading != nil {
					bars++
					x0 := v.Ops[0].P[0].X
					x1 := v.Ops[1].P[0].X
					if (bars == 1 && !near(x1-x0, (64-2*dataBarInset)/4, 1e-6)) || !near(x0, dataBarInset, 1e-6) {
						t.Errorf("bar %d spans %v..%v", bars, x0, x1)
					}
				} else if v.Fill != nil && v.Fill.Color == colorOf(green) {
					fills++
				}
			case *compose.Group:
				walk(v.Content)
			}
		}
	}
	walk(out.Pages[0].Content)
	if bars != 2 || fills != 1 {
		t.Errorf("%d bars and %d fills, want 2 and 1", bars, fills)
	}
	warned := false
	for _, w := range out.Warnings {
		warned = warned || w.Code == CodeConditional
	}
	if !warned {
		t.Error("no warning for the expression")
	}
}

// TestThreshold locks the thresholds a reader resolves.
func TestThreshold(t *testing.T) {
	nums := []float64{1, 2, 3, 4}
	tests := []struct {
		kind string
		v    float64
		want float64
		ok   bool
	}{
		{"min", 0, 1, true}, {"max", 0, 4, true}, {"autoMin", 0, 0, true}, {"autoMax", 0, 4, true},
		{"percent", 90, 3.7, true}, {"percentile", 10, 1.3, true}, {"percentile", 50, 2.5, true},
		{"num", 7, 7, true}, {"formula", 0, 0, false},
	}
	for _, tc := range tests {
		got, ok := Threshold(tc.kind, tc.v, nums)
		if ok != tc.ok || !near(got, tc.want, 1e-9) {
			t.Errorf("%s %v: %v %v, want %v %v", tc.kind, tc.v, got, ok, tc.want, tc.ok)
		}
	}
	if _, ok := Threshold("min", 0, nil); ok {
		t.Error("min of no number")
	}
	sh := column(numCell(0, 3, nil), textCell(0, "x", nil), numCell(0, 1, nil))
	if got := RangeNumbers(sh, []Range{{LastRow: 5}, {LastRow: 0}}); len(got) != 2 || got[0] != 1 || got[1] != 3 {
		t.Errorf("RangeNumbers %v", got)
	}
}
