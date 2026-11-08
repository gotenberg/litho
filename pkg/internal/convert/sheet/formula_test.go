package sheet

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
)

// formulaDoc returns a document of two sheets: S holds 2, 3, 4 in A1:A3,
// "x" in B1 and a date-formatted 45000 in C1; T holds 10 in A1.
func formulaDoc(formulas ...string) *Document {
	s := &Sheet{Name: "S", Rows: []Row{
		{Index: 0, Cells: []Cell{numCell(0, 2, nil), textCell(1, "x", nil), numCell(2, 45000, &Style{Format: "YYYY-MM-DD"})}},
		{Index: 1, Cells: []Cell{numCell(0, 3, nil)}},
		{Index: 2, Cells: []Cell{numCell(0, 4, nil)}},
	}}
	row := Row{Index: 5}
	for i, f := range formulas {
		row.Cells = append(row.Cells, Cell{Column: i, Formula: f, Value: Value{Kind: Number, Number: -1}})
	}
	s.Rows = append(s.Rows, row)
	t := &Sheet{Name: "T", Rows: []Row{{Index: 0, Cells: []Cell{numCell(0, 10, nil)}}}}
	doc := newDoc(s)
	doc.Sheets = append(doc.Sheets, t)
	return doc
}

func TestApplyFormulas(t *testing.T) {
	num := func(n float64) Value { return Value{Kind: Number, Number: n} }
	tests := []struct {
		formula string
		want    Value
		format  string
	}{
		{"A1+2", num(4), ""},
		{"=SUM(A1:A3)", num(9), ""},
		{"of:=SUM([.A1:.A3];[.A1])", num(11), ""},
		{"AVERAGE(A1:A3)", num(3), ""},
		{"MAX(A1:A3)-MIN(A1:A3)", num(2), ""},
		{"COUNT(A1:B3)", num(3), ""},
		{"A1/0", Value{Kind: Error, Text: "#DIV/0!"}, ""},
		{"A1>1", Value{Kind: Bool, Number: 1}, ""},
		{`"x"&A1`, Value{Kind: Text, Text: "x2"}, ""},
		{"2^3*(1+1)", num(16), ""},
		{"-A1+10%", num(-1.9), percentFormat},
		{"B1+1", Value{Kind: Error, Text: "#VALUE!"}, ""},
		{"A9+1", num(1), ""},
		{"T!A1*2", num(20), ""},
		{"[$T.A1]+[.A1]", num(12), ""},
		{"C1+1", num(45001), "YYYY-MM-DD"},
		{"F6+1", num(3), ""},
		{"VLOOKUP(1,A1:A3,1)", num(-1), ""},
		{"A1+", num(-1), ""},
	}
	var formulas []string
	for _, tt := range tests {
		formulas = append(formulas, tt.formula)
	}
	// F6 refers to the "A1+2" a few cells left: G6 below reads it.
	formulas[16] = "A6-1"
	tests[16].want = num(3)
	doc := formulaDoc(formulas...)
	b := newBuilder(context.Background(), doc, nil, office.Options{})
	out := b.applyFormulas(doc)
	row := out.Sheets[0].Rows[3]
	for i, tt := range tests {
		c := row.Cells[i]
		if c.Value != tt.want {
			t.Errorf("%s = %+v, want %+v", formulas[i], c.Value, tt.want)
		}
		format := ""
		if c.Style != nil {
			format = c.Style.Format
		}
		if format != tt.format {
			t.Errorf("%s format %q, want %q", formulas[i], format, tt.format)
		}
	}
	if doc.Sheets[0].Rows[3].Cells[0].Value != num(-1) {
		t.Error("the document changed")
	}
	if len(b.warnings) != 1 || b.warnings[0].Code != CodeFormula {
		t.Errorf("warnings %+v", b.warnings)
	}
}

func TestApplyFormulas_Cycle(t *testing.T) {
	doc := formulaDoc("B6+1", "A6+1")
	out := newBuilder(context.Background(), doc, nil, office.Options{}).applyFormulas(doc)
	for _, c := range out.Sheets[0].Rows[3].Cells {
		if c.Value.Kind == Number && c.Value.Number > 10 {
			t.Errorf("a cycle computed %+v", c.Value)
		}
	}
}

func FuzzFormula(f *testing.F) {
	for _, s := range []string{"A1+2*3", "of:=SUM([.A1:.B3];1)", `"a"&"b"=TRUE`, "'S'!$A$1:B2", "-(1+2)%^2", "#N/A", "MAX(T!A1,[$T.A1])"} {
		f.Add(s)
	}
	doc := formulaDoc()
	f.Fuzz(func(t *testing.T, s string) {
		e := newFormulaEval(doc)
		e.eval(s, 0, 0, 0, 0)
	})
}
