package sheet

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
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
		// A formula is relative to the format's first cell; a text
		// compares above every number.
		{"an expression, relative", Rule{Kind: RuleExpression, Formula: "A1>1"}, []bool{false, true, true, true, false, true, true}},
		{"an expression on the row", Rule{Kind: RuleExpression, Formula: "ISEVEN(ROW())"}, []bool{false, true, false, true, false, true, false}},
		{"an expression the evaluator does not compute", Rule{Kind: RuleExpression, Formula: "VLOOKUP(A1,B1:B2,1)"}, []bool{false, false, false, false, false, false, false}},
		// A formula operand is computed at each cell: B is empty, 0.
		{"a formula operand, relative", Rule{Kind: RuleCellIs, Operator: "equal", Values: []Value{num(1)}, Formula: "B1"}, []bool{false, false, false, false, true, false, false}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sh := column(nums...)
			rule := tc.rule
			rule.Style = fill
			sh.Conditional = []Conditional{{Ranges: []Range{{LastRow: 9}}, Rules: []Rule{rule}}}
			g := newGrid(t.Context(), newDoc(sh), sh)
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
			g := newGrid(t.Context(), newDoc(sh), sh)
			if s := g.style(0, 0, g.cell(0, 0)); !tc.check(s) {
				t.Errorf("style %+v", *s)
			}
			if own.Font.Bold || own.Font.Color == red {
				t.Error("the cell's own style changed")
			}
		})
	}
}

// TestConditional_ExcelRules locks the rules measured on Excel's prints:
// an aboveAverage rule's bound moved by population standard deviations,
// the error rules, and a colour scale that gives way under Excel's rules
// to the fill of a rule of a higher priority.
//
// # Corpus evidence
//
//   - litho/xlsx: cf_rule_types highlights 99 and 60 of 3, 18, 7, 42, 7,
//     25, 99, 1, 42, 60 one standard deviation above their mean (30.4 +
//     29.57, the population's; the sample's 31.17 leaves 60 out); #N/A
//     alone under containsErrors and every other cell, the empty one
//     included, under notContainsErrors; and, under rules of priority 60
//     and 61 filling values above 10 and 5, the colour scale of priority
//     62 on 3 and 1 alone.
func TestConditional_ExcelRules(t *testing.T) {
	fill := &Style{Fill: Fill{Color: green}}
	vals := []float64{3, 18, 7, 42, 7, 25, 99, 1, 42, 60}
	var cells []Cell
	for _, v := range vals {
		cells = append(cells, numCell(0, v, nil))
	}
	errs := []Cell{textCell(0, "apple", nil), {}, {Value: Value{Kind: Error, Text: "#N/A"}}}
	tests := []struct {
		name  string
		cells []Cell
		rule  Rule
		want  []bool
	}{
		{"one deviation above", cells, Rule{Kind: RuleAboveAverage, Values: []Value{num(1)}}, []bool{false, false, false, false, false, false, true, false, false, true}},
		{"one deviation below", cells, Rule{Kind: RuleBelowAverage, Values: []Value{num(1)}}, []bool{false, false, false, false, false, false, false, false, false, false}},
		{"errors", errs, Rule{Kind: RuleErrors}, []bool{false, false, true}},
		{"no errors, an empty cell too", errs, Rule{Kind: RuleNoErrors}, []bool{true, true, false}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sh := column(tc.cells...)
			rule := tc.rule
			rule.Style = fill
			sh.Conditional = []Conditional{{Ranges: []Range{{LastRow: len(tc.cells) - 1}}, Rules: []Rule{rule}}}
			g := newGrid(t.Context(), newDoc(sh), sh)
			for i, want := range tc.want {
				if got := g.style(i, 0, g.cell(i, 0)).Fill.Color == green; got != want {
					t.Errorf("A%d: restyled %v, want %v", i+1, got, want)
				}
			}
		})
	}
	for _, excel := range []bool{false, true} {
		sh := column(numCell(0, 18, nil))
		sh.Conditional = []Conditional{{Ranges: []Range{{}}, Rules: []Rule{
			{Kind: RuleCellIs, Operator: "greaterThan", Values: []Value{num(10)}, Priority: 1, Style: fill},
			{Kind: RuleColorScale, Priority: 2, Scale: []css.Color{red, red}},
		}}}
		doc := newDoc(sh)
		doc.ExcelRules = excel
		g := newGrid(t.Context(), doc, sh)
		want := red
		if excel {
			want = green
		}
		if got := g.style(0, 0, g.cell(0, 0)).Fill.Color; got != want {
			t.Errorf("Excel's rules %v: fill %v, want %v", excel, got, want)
		}
	}
}

// TestConditional_TimePeriods locks the time period rules around the print
// date, a week from Sunday, and that none holds without a print date.
//
// # Corpus evidence
//
//   - litho/xlsx: cf_rule_types, printed by Excel on Thursday 2026-11-12,
//     highlights among 30 September and 2, 8, 9, 10, 12 November and 9
//     December: 12 November for today, 8 to 12 November for last7Days and
//     thisWeek, 2 to 12 November for thisMonth, none for yesterday,
//     tomorrow and lastMonth.
func TestConditional_TimePeriods(t *testing.T) {
	day := func(m time.Month, d int) Cell {
		return numCell(0, time.Date(2026, m, d, 0, 0, 0, 0, time.UTC).Sub(nullDate).Hours()/24+0.5, nil)
	}
	days := []Cell{day(time.September, 30), day(time.November, 2), day(time.November, 8), day(time.November, 9), day(time.November, 10), day(time.November, 12), day(time.December, 9), day(time.November, 1), day(time.October, 31), day(time.November, 15)}
	fill := &Style{Fill: Fill{Color: green}}
	tests := []struct {
		period string
		want   []bool
	}{
		{"today", []bool{false, false, false, false, false, true, false, false, false, false}},
		{"yesterday", []bool{false, false, false, false, false, false, false, false, false, false}},
		{"last7Days", []bool{false, false, true, true, true, true, false, false, false, false}},
		{"thisWeek", []bool{false, false, true, true, true, true, false, false, false, false}},
		{"lastWeek", []bool{false, true, false, false, false, false, false, true, false, false}},
		{"nextWeek", []bool{false, false, false, false, false, false, false, false, false, true}},
		{"thisMonth", []bool{false, true, true, true, true, true, false, true, false, true}},
		{"lastMonth", []bool{false, false, false, false, false, false, false, false, true, false}},
		{"nextMonth", []bool{false, false, false, false, false, false, true, false, false, false}},
	}
	for _, tc := range tests {
		for _, dated := range []bool{false, true} {
			sh := column(days...)
			sh.Conditional = []Conditional{{Ranges: []Range{{LastRow: len(days) - 1}}, Rules: []Rule{{Kind: RuleTimePeriod, Operator: tc.period, Style: fill}}}}
			g := newGrid(t.Context(), newDoc(sh), sh)
			if dated {
				g.cond.setToday(time.Date(2026, time.November, 12, 18, 30, 0, 0, time.FixedZone("", -5*3600)))
			}
			for i, want := range tc.want {
				if got := g.style(i, 0, g.cell(i, 0)).Fill.Color == green; got != (want && dated) {
					t.Errorf("%s, dated %v: row %d restyled %v", tc.period, dated, i+1, got)
				}
			}
			if n := unevaluated(g); n != 1-btoi(dated) {
				t.Errorf("%s, dated %v: %d rules reported", tc.period, dated, n)
			}
		}
	}
}

// btoi returns 1 for true.
func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
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
		{"one value takes the top colour", Rule{Kind: RuleColorScale, Scale: []css.Color{red, green, blue}}, []float64{1}, []css.Color{blue}},
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
			g := newGrid(t.Context(), newDoc(sh), sh)
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

// TestConditional_DataBar locks the data bar's zero, length and colour
// by axis against what Calc prints for sc/databar.xlsx.
func TestConditional_DataBar(t *testing.T) {
	tests := []struct {
		name     string
		axis     BarAxis
		minL     float64
		maxL     float64
		values   []Value
		cells    []float64
		zero     float64
		length   []float64
		negative []bool
	}{
		{"automatic from the minimum", BarAxisAuto, 0, 1, []Value{str("min"), str("max")}, []float64{1, 2, 4}, 0, []float64{0, 1.0 / 3, 1}, []bool{false, false, false}},
		{"automatic over zero", BarAxisAuto, 0, 1, []Value{num(-2), num(3)}, []float64{1, 3, -2}, 0.4, []float64{1.0 / 3, 1, -1}, []bool{false, false, true}},
		{"automatic clamped", BarAxisAuto, 0, 1, []Value{num(0), num(2)}, []float64{1, 5}, 0, []float64{0.5, 1}, []bool{false, false}},
		{"middle", BarAxisMiddle, 0.1, 0.9, []Value{num(1), num(4)}, []float64{1, 4}, 0.5, []float64{0.225, 0.9}, []bool{false, false}},
		{"none, the lengths", BarAxisNone, 0.1, 0.9, []Value{num(1), num(5)}, []float64{1, 3, 9}, 0, []float64{0.1, 0.5, 0.9}, []bool{false, false, false}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var cells []Cell
			for _, v := range tc.cells {
				cells = append(cells, numCell(0, v, nil))
			}
			sh := column(cells...)
			sh.Conditional = []Conditional{{Ranges: []Range{{LastRow: 9}}, Rules: []Rule{{
				Kind: RuleDataBar, Values: tc.values, Bar: blue, BarAxis: tc.axis, BarMinLength: tc.minL, BarMaxLength: tc.maxL,
			}}}}
			g := newGrid(t.Context(), newDoc(sh), sh)
			for i := range tc.cells {
				zero, length, col, _, ok := g.dataBar(i, 0, g.cell(i, 0))
				if tc.length[i] == 0 {
					if ok {
						t.Errorf("A%d: a bar of length %v", i+1, length)
					}
					continue
				}
				if !ok || !near(zero, tc.zero, 1e-9) || !near(length, tc.length[i], 1e-9) {
					t.Errorf("A%d: %v zero %v length %v, want %v %v", i+1, ok, zero, length, tc.zero, tc.length[i])
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
		{Ranges: []Range{{LastRow: 1}}, Rules: []Rule{{Kind: RuleDataBar, Bar: blue, Values: []Value{num(0), num(4)}, BarMaxLength: 1}}},
		{Ranges: []Range{{FirstRow: 1, LastRow: 1}}, Rules: []Rule{
			{Kind: RuleCellIs, Operator: "equal", Values: []Value{num(4)}, Style: &Style{Fill: Fill{Color: green}}},
			{Kind: RuleExpression, Formula: "TRUE()"},
		}},
	}
	out := convert(t, newDoc(sh), office.Options{})
	var bars, fills int
	var walk func([]typeset.Fragment)
	walk = func(fs []typeset.Fragment) {
		for _, f := range fs {
			switch v := f.(type) {
			case *typeset.Path:
				if v.Fill != nil && v.Fill.Shading != nil {
					bars++
					x0 := v.Ops[0].P[0].X
					x1 := v.Ops[1].P[0].X
					if (bars == 1 && !near(x1-x0, (calcPrinted(64)-2*dataBarInset)/4, 1e-6)) || !near(x0, dataBarInset, 1e-6) {
						t.Errorf("bar %d spans %v..%v", bars, x0, x1)
					}
				} else if v.Fill != nil && v.Fill.Color == colorOf(green) {
					fills++
				}
			case *typeset.Group:
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
		warned = warned || w.Code == warn.ContentUnsupported
	}
	if !warned {
		t.Error("no warning for the expression")
	}
}

// TestConvert_ExcelDataBars locks the data bars Excel prints: inset 16
// printer pixels at the left, top and bottom and 8 at the right, shaded
// opaque from the bar's colour to 85% of the way to white across the
// bar, outlined 8 pixels inside, right to left from the right inset,
// and a dashed axis 8 pixels wide whose edges the bars start from.
func TestConvert_ExcelDataBars(t *testing.T) {
	c8 := func(r, g, b float64) css.Color { return css.Color{R: r / 255, G: g / 255, B: b / 255, A: 1} }
	bar := c8(0x63, 0x8E, 0xC6)
	type drawn struct {
		x0, x1, y0, y1 float64
		from, to       typeset.Color
		stroke         *typeset.Stroke
	}
	draw := func(rule Rule, v float64) []drawn {
		sh := &Sheet{Name: "S", Page: a4(), Columns: []Column{{First: 0, Last: 0, Width: 73.32}}, Rows: []Row{{Index: 0, Height: 14.52, Cells: []Cell{numCell(0, v, nil)}}}}
		sh.Rows = append(sh.Rows, Row{Index: 1, Height: 14.52, Cells: []Cell{numCell(0, -100, nil)}}, Row{Index: 2, Height: 14.52, Cells: []Cell{numCell(0, 100, nil)}})
		rule.Kind, rule.Bar = RuleDataBar, bar
		sh.Conditional = []Conditional{{Ranges: []Range{{LastRow: 2}}, Rules: []Rule{rule}}}
		doc := newDoc(sh)
		doc.ExcelRules = true
		out := convert(t, doc, office.Options{})
		var got []drawn
		var walk func([]typeset.Fragment)
		walk = func(fs []typeset.Fragment) {
			for _, f := range fs {
				switch p := f.(type) {
				case *typeset.Path:
					if len(p.Ops) < 2 {
						continue
					}
					d := drawn{x0: p.Ops[0].P[0].X, y0: p.Ops[0].P[0].Y, x1: p.Ops[1].P[0].X, y1: p.Ops[1].P[0].Y, stroke: p.Stroke}
					if len(p.Ops) > 2 {
						d.y1 = p.Ops[2].P[0].Y
					}
					switch {
					case p.Fill != nil && p.Fill.Shading != nil:
						d.from, d.to = p.Fill.Shading.Stops[0].Color, p.Fill.Shading.Stops[1].Color
						if p.Fill.Shading.From.X > p.Fill.Shading.To.X {
							d.from, d.to = d.to, d.from
						}
					case p.Fill != nil:
						d.from = p.Fill.Color
					}
					if p.Stroke != nil || (p.Fill != nil && (p.Fill.Shading != nil || p.Fill.Color == colorOf(bar))) {
						got = append(got, d)
					}
				case *typeset.Group:
					walk(p.Content)
				}
			}
		}
		walk(out.Pages[0].Content)
		return got
	}
	px := excelPixel
	// A gradient bar from the left: 0..100 over the room, 50 half of it.
	g := draw(Rule{Values: []Value{num(0), num(100)}, BarMaxLength: 1, BarAxis: BarAxisNone}, 50)
	if len(g) == 0 {
		t.Fatal("no bar drawn")
	}
	room := 73.32 - 24*px
	if b := g[0]; !near(b.x0, 16*px, 1e-6) || !near(b.x1, 16*px+room/2, 1e-6) || !near(b.y0, 16*px, 1e-6) || !near(b.y1, 14.52-16*px, 1e-6) {
		t.Errorf("bar %+v, want x %v..%v", b, 16*px, 16*px+room/2)
	}
	if b := g[0]; b.from != colorOf(bar) || b.to != colorOf(c8(231, 238, 246)) {
		t.Errorf("shaded %v to %v", b.from, b.to)
	}
	// Right to left, solid and outlined.
	g = draw(Rule{Values: []Value{num(0), num(100)}, BarMaxLength: 1, BarAxis: BarAxisNone, BarSolid: true, BarRightToLeft: true, BarBorder: red}, 50)
	if len(g) < 2 || !near(max(g[0].x0, g[0].x1), 73.32-8*px, 1e-6) || !near(min(g[0].x0, g[0].x1), 73.32-8*px-room/2, 1e-6) {
		t.Fatalf("right-to-left bar %+v", g)
	}
	if s := g[1].stroke; s == nil || s.Color != colorOf(red) || !near(s.Width, 8*px, 1e-9) {
		t.Errorf("outline %+v", g[1])
	}
	// An automatic axis over -100..100 sits halfway, dashed.
	g = draw(Rule{Values: []Value{str("min"), str("max")}, BarAxis: BarAxisAuto, BarSolid: true}, 50)
	var axis *drawn
	for i := range g {
		if g[i].stroke != nil && len(g[i].stroke.Dash) == 2 {
			axis = &g[i]
		}
	}
	mid := 16*px + room/2
	if axis == nil || !near(axis.x0, mid, 1e-6) || !near(axis.stroke.Dash[0], 24*px, 1e-9) || !near(axis.stroke.Width, 8*px, 1e-9) {
		t.Fatalf("axis %+v", axis)
	}
	if !near(g[0].x0, mid+4*px, 1e-6) {
		t.Errorf("bar starts at %v, want the axis's edge %v", g[0].x0, mid+4*px)
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
		{"min", 0, 1, true},
		{"max", 0, 4, true},
		{"autoMin", 0, 0, true},
		{"autoMax", 0, 4, true},
		{"percent", 90, 3.7, true},
		{"percentile", 10, 1.3, true},
		{"percentile", 50, 2.5, true},
		{"num", 7, 7, true},
		{"formula", 0, 0, false},
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

// TestConditional_FormulaThreshold locks a threshold given as a formula,
// computed over the sheet: sc/databar.xlsx's upper threshold 3*A1+2.
func TestConditional_FormulaThreshold(t *testing.T) {
	sh := column(numCell(0, 1, nil), numCell(0, 3, nil), numCell(0, 5, nil))
	sh.Conditional = []Conditional{{Ranges: []Range{{FirstRow: 1, LastRow: 2}}, Rules: []Rule{{
		Kind: RuleDataBar, Values: []Value{num(0), str("=3*$A$1+2")}, Bar: blue, BarAxis: BarAxisAuto, BarMaxLength: 1,
	}}}}
	g := newGrid(t.Context(), newDoc(sh), sh)
	if _, length, _, _, ok := g.dataBar(1, 0, g.cell(1, 0)); !ok || !near(length, 0.6, 1e-9) {
		t.Errorf("length %v (%v), want 3 of 5", length, ok)
	}
}

// TestConvert_DataBarAlone draws a data bar whose rule hides the value
// without the cell's number (ECMA-376 Part 1, 18.3.1.28 dataBar
// showValue), the cell whose bar has no length included; with it
// otherwise.
func TestConvert_DataBarAlone(t *testing.T) {
	for _, only := range []bool{false, true} {
		sh := column(numCell(0, 0, nil), numCell(0, 1, nil), numCell(0, 4, nil))
		sh.Conditional = []Conditional{{Ranges: []Range{{LastRow: 2}}, Rules: []Rule{{
			Kind: RuleDataBar, Bar: blue, Values: []Value{num(0), num(4)}, BarMaxLength: 1, BarOnly: only,
		}}}}
		out := convert(t, newDoc(sh), office.Options{})
		got := strings.Join(texts(out.Pages[0]), "|")
		if want := map[bool]string{false: "0|1|4", true: ""}[only]; got != want {
			t.Errorf("bar only %v: texts %q, want %q", only, got, want)
		}
	}
}
