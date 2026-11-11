package sheet

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
)

// pivotSheet returns a sheet whose A1:C5 holds field1 (1, 1, 2, 2),
// field2 (a, s, a, s) and field3 (1, 2, 2, 1), with the pivot table p.
func pivotSheet(p Pivot) *Sheet {
	txt := func(s string) Value { return Value{Kind: Text, Text: s} }
	num := func(n float64) Value { return Value{Kind: Number, Number: n} }
	data := [][]Value{
		{txt("field1"), txt("field2"), txt("field3")},
		{num(1), txt("a"), num(1)},
		{num(1), txt("s"), num(2)},
		{num(2), txt("a"), num(2)},
		{num(2), txt("s"), num(1)},
	}
	sh := &Sheet{Name: "S", Pivots: []Pivot{p}}
	for r, row := range data {
		rw := Row{Index: r}
		for c, v := range row {
			rw.Cells = append(rw.Cells, Cell{Column: c, Value: v})
		}
		sh.Rows = append(sh.Rows, rw)
	}
	return sh
}

// pivotCells applies the pivot tables of doc and returns the values and
// styles of its first sheet by "row,column".
func pivotCells(t *testing.T, doc *Document) map[[2]int]Cell {
	t.Helper()
	b := newBuilder(context.Background(), doc, nil, office.Options{})
	out := b.applyPivots(doc)
	cells := map[[2]int]Cell{}
	for _, r := range out.Sheets[0].Rows {
		for _, c := range r.Cells {
			cells[[2]int{r.Index, c.Column}] = c
		}
	}
	return cells
}

func TestApplyPivots_Layouts(t *testing.T) {
	type want struct {
		r, c int
		text string
		num  float64
		bold bool
	}
	base := func() Pivot {
		return Pivot{
			Name: "P", Row: 0, Column: 5, Clear: &Range{FirstRow: 0, FirstColumn: 5, LastRow: 9, LastColumn: 8},
			SourceSheet: 0, Source: Range{FirstRow: 0, FirstColumn: 0, LastRow: 4, LastColumn: 2},
			Data:     []PivotData{{Field: 2, Function: PivotSum, Caption: "Sum of field3"}},
			RowGrand: true, ColumnGrand: true, ExpandCollapse: true,
		}
	}
	tests := []struct {
		name  string
		pivot func() Pivot
		want  []want
		empty [][2]int
	}{
		{
			// sc/pivottable_outline_mode.xlsx: the subtotal of field1 in
			// its title row, field2's members below it.
			name: "outline, subtotals at the top",
			pivot: func() Pivot {
				p := base()
				p.Rows = []PivotField{{Field: 0, Layout: PivotOutlineTop, Subtotals: []PivotFunction{PivotAuto}}, {Field: 1, Layout: PivotOutlineTop}}
				return p
			},
			want: []want{
				{0, 5, "field1", 0, false},
				{0, 6, "field2", 0, false},
				{0, 7, "Sum of field3", 0, false},
				{1, 5, "", 1, false},
				{1, 7, "", 3, false},
				{2, 6, "a", 0, false},
				{2, 7, "", 1, false},
				{3, 6, "s", 0, false},
				{3, 7, "", 2, false},
				{4, 5, "", 2, false},
				{4, 7, "", 3, false},
				{7, 5, "Total Result", 0, true},
				{7, 7, "", 6, true},
			},
		},
		{
			// A tabular field starts its first child on its own row and
			// shows its subtotal on a row of its own below them.
			name: "tabular, subtotals below",
			pivot: func() Pivot {
				p := base()
				p.Rows = []PivotField{{Field: 0, Layout: PivotTabular, Subtotals: []PivotFunction{PivotAuto}}, {Field: 1}}
				return p
			},
			want: []want{
				{1, 5, "", 1, false},
				{1, 6, "a", 0, false},
				{1, 7, "", 1, false},
				{2, 6, "s", 0, false},
				{3, 5, "1 Result", 0, true},
				{3, 7, "", 3, true},
				{4, 5, "", 2, false},
				{6, 5, "2 Result", 0, true},
				{7, 5, "Total Result", 0, true},
				{7, 7, "", 6, true},
			},
		},
		{
			// A column field spreads its members across, the grand total
			// in a column of its own; a hidden member leaves its records
			// out.
			name: "column field, a hidden member",
			pivot: func() Pivot {
				p := base()
				p.Rows = []PivotField{{Field: 0}}
				p.Columns = []PivotField{{Field: 1, Hidden: []Value{{Kind: Text, Text: "s"}}}}
				return p
			},
			want: []want{
				{0, 5, "Sum of field3", 0, false},
				{0, 6, "field2", 0, false},
				{1, 5, "field1", 0, false},
				{1, 6, "a", 0, false},
				{1, 7, "Total Result", 0, true},
				{2, 5, "", 1, false},
				{2, 6, "", 1, false},
				{2, 7, "", 1, true},
				{4, 5, "Total Result", 0, true},
				{4, 7, "", 3, true},
			},
		},
		{
			// A page field selecting a member filters the records, the
			// table moving down a row past the page fields and their
			// empty row.
			name: "page field",
			pivot: func() Pivot {
				p := base()
				sel := Value{Kind: Text, Text: "a"}
				p.Pages = []PivotField{{Field: 1, Selected: &sel}}
				p.Rows = []PivotField{{Field: 0}}
				return p
			},
			want: []want{
				{0, 5, "field2", 0, false},
				{0, 6, "a", 0, false},
				{2, 5, "field1", 0, false},
				{2, 6, "Sum of field3", 0, false},
				{3, 6, "", 1, false},
				{4, 6, "", 2, false},
				{5, 6, "", 3, true},
			},
		},
		{
			// A field named by a label the source lacks is left out, as
			// Calc misses a cache field whose name its source lost.
			name: "a field whose name the source lacks",
			pivot: func() Pivot {
				p := base()
				p.Rows = []PivotField{{Field: 1, Name: "gone"}}
				return p
			},
			want:  []want{{0, 5, "Sum of field3", 0, false}, {1, 5, "", 6, false}},
			empty: [][2]int{{2, 5}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cells := pivotCells(t, newDoc(pivotSheet(tt.pivot())))
			for _, w := range tt.want {
				c, ok := cells[[2]int{w.r, w.c}]
				if !ok {
					t.Errorf("(%d,%d): no cell", w.r, w.c)
					continue
				}
				switch {
				case w.text != "" && (c.Value.Kind != Text || c.Value.Text != w.text):
					t.Errorf("(%d,%d) = %+v, want %q", w.r, w.c, c.Value, w.text)
				case w.text == "" && (c.Value.Kind != Number || c.Value.Number != w.num):
					t.Errorf("(%d,%d) = %+v, want %v", w.r, w.c, c.Value, w.num)
				}
				if c.Style == nil || c.Style.Font.Bold != w.bold {
					t.Errorf("(%d,%d) bold = %v, want %v", w.r, w.c, c.Style != nil && c.Style.Font.Bold, w.bold)
				}
			}
			for _, e := range tt.empty {
				if c, ok := cells[e]; ok && hasContent(&c) {
					t.Errorf("(%d,%d) = %+v, want empty", e[0], e[1], c.Value)
				}
			}
		})
	}
}

func TestApplyPivots_Frames(t *testing.T) {
	p := Pivot{
		Name: "P", Row: 0, Column: 5, SourceSheet: 0, Source: Range{LastRow: 4, LastColumn: 2},
		Rows:     []PivotField{{Field: 0, Layout: PivotOutlineTop, Subtotals: []PivotFunction{PivotAuto}}, {Field: 1}},
		Data:     []PivotData{{Field: 2, Function: PivotSum}},
		RowGrand: true, ExpandCollapse: true,
	}
	cells := pivotCells(t, newDoc(pivotSheet(p)))
	tests := []struct {
		name string
		r, c int
		side css.Side
		w    float64
	}{
		{"the table's top edge", 0, 5, css.Top, pivotOuterFrame},
		{"the table's left edge", 3, 5, css.Left, pivotOuterFrame},
		{"the field button's right", 0, 5, css.Right, pivotInnerFrame},
		{"the member block's right", 2, 5, css.Right, pivotInnerFrame},
		{"the grand total's top", 7, 6, css.Top, pivotInnerFrame},
		{"the table's bottom edge", 7, 7, css.Bottom, pivotOuterFrame},
		{"the table's right edge", 4, 7, css.Right, pivotOuterFrame},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := cells[[2]int{tt.r, tt.c}]
			if c.Style == nil || c.Style.Border[tt.side].Width != tt.w || c.Style.Border[tt.side].Line != LineSolid {
				t.Errorf("(%d,%d) side %d = %+v, want a %v-point line", tt.r, tt.c, tt.side, c.Style, tt.w)
			}
		})
	}
	if c := cells[[2]int{1, 5}]; c.Style == nil || c.Style.Align.Indent != pivotIndent || c.Style.Align.Horizontal != HLeft {
		t.Errorf("field1's member: %+v, want left-aligned with one indent step", c.Style)
	}
	if c := cells[[2]int{2, 6}]; c.Style == nil || c.Style.Align.Indent != 0 {
		t.Errorf("field2's member: %+v, want no indent", c.Style)
	}
}

func TestApplyPivots_Functions(t *testing.T) {
	tests := []struct {
		fn   PivotFunction
		want Value
	}{
		{PivotSum, Value{Kind: Number, Number: 6}},
		{PivotCount, Value{Kind: Number, Number: 4}},
		{PivotAverage, Value{Kind: Number, Number: 1.5}},
		{PivotMax, Value{Kind: Number, Number: 2}},
		{PivotMin, Value{Kind: Number, Number: 1}},
		{PivotProduct, Value{Kind: Number, Number: 4}},
		{PivotMedian, Value{Kind: Number, Number: 1.5}},
		{PivotVarP, Value{Kind: Number, Number: 0.25}},
	}
	for _, tt := range tests {
		t.Run(functionName(tt.fn), func(t *testing.T) {
			p := Pivot{Name: "P", Column: 5, Source: Range{LastRow: 4, LastColumn: 2}, Data: []PivotData{{Field: 2, Function: tt.fn}}}
			cells := pivotCells(t, newDoc(pivotSheet(p)))
			if got := cells[[2]int{1, 5}].Value; got != tt.want {
				t.Errorf("result = %+v, want %+v", got, tt.want)
			}
			if got := cells[[2]int{0, 5}].Value.Text; got != functionName(tt.fn)+" - field3" {
				t.Errorf("caption = %q", got)
			}
		})
	}
}

func TestApplyPivots_SourceNotASheet(t *testing.T) {
	p := Pivot{Name: "P", Column: 5, SourceSheet: 3, Source: Range{LastRow: 4, LastColumn: 2}, Data: []PivotData{{Field: 2}}}
	doc := newDoc(pivotSheet(p))
	b := newBuilder(context.Background(), doc, nil, office.Options{})
	if out := b.applyPivots(doc); out.Sheets[0] != doc.Sheets[0] {
		t.Errorf("the sheet changed")
	}
	if len(b.warnings) != 1 || b.warnings[0].Code != CodePivot {
		t.Errorf("warnings = %v", b.warnings)
	}
}

// TestApplyPivots_ExcelRules locks the pivot table of a book laid out by
// Excel's rules: its saved cells print, nothing rebuilt.
func TestApplyPivots_ExcelRules(t *testing.T) {
	p := Pivot{Name: "P", Column: 5, Source: Range{LastRow: 4, LastColumn: 2}, Rows: []PivotField{{Field: 0}}, Data: []PivotData{{Field: 2}}}
	doc := newDoc(pivotSheet(p))
	doc.ExcelRules = true
	b := newBuilder(context.Background(), doc, nil, office.Options{})
	if out := b.applyPivots(doc); out != doc || len(b.warnings) != 0 {
		t.Errorf("the book changed, warnings %v", b.warnings)
	}
}
