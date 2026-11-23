package sheet

import (
	"context"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/warn"
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
		{"HOUR(0.5208333)+MINUTE(0.5208333)/100", num(12.3), ""},
		{`of:=COM.MICROSOFT.SWITCH([.A1];1;"one";2;"two";"other")`, Value{Kind: Text, Text: "two"}, ""},
		{`COM.MICROSOFT.SWITCH(A1,"2","text two","other")`, Value{Kind: Text, Text: "text two"}, ""},
		// A second address without a sheet is on the first one's, not on
		// the formula's: T holds 10 in A1 and nothing below, S holds 2 and 3.
		{"of:=SUM([$T.A1:.A2])", num(10), ""},
		{"A1+", num(-1), ""},
		// AND and OR read the numbers of a range and pass its text over.
		{"AND(A1:B3)", Value{Kind: Bool, Number: 1}, ""},
		{"AND(A1:A3,0)", Value{Kind: Bool}, ""},
		{"OR(B1:B3)", Value{Kind: Error, Text: "#VALUE!"}, ""},
	}
	var formulas []string
	for _, tt := range tests {
		formulas = append(formulas, tt.formula)
	}
	// Q6 refers to the "A1+2" of A6.
	formulas[16] = "A6-1"
	tests[16].want = num(3)
	doc := formulaDoc(formulas...)
	b := newBuilder(context.Background(), doc, nil, office.Options{})
	out := b.formulasOf(t, doc)
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
	if len(b.warnings) != 1 || b.warnings[0].Code != warn.ContentUnsupported {
		t.Errorf("warnings %+v", b.warnings)
	}
}

func TestApplyFormulas_Cycle(t *testing.T) {
	doc := formulaDoc("B6+1", "A6+1")
	out := newBuilder(context.Background(), doc, nil, office.Options{}).formulasOf(t, doc)
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
		e := newFormulaEval(t.Context(), doc)
		e.evalAt(s, 0, 5, 1, 0, 0)
	})
}

// TestFormulaRanges_StartAtTheirRow locks the cost of a formula's range: it
// visits the rows it covers, not the sheet's rows above it, so a column of
// short sums visits a number of rows and cells linear in its length. A
// walk from the sheet's first row for each range took 0.22 s at 20,000
// rows and grows with their square.
func TestFormulaRanges_StartAtTheirRow(t *testing.T) {
	t.Parallel()
	const n = 20_000
	doc := columnOfSums(n)
	e := newFormulaEval(t.Context(), doc)
	for r := range n {
		if v := e.cellValue(0, r, 1, 0); v.v.Kind != Number {
			t.Fatalf("row %d sums to %+v", r+1, v.v)
		}
	}
	if e.steps > 10*n {
		t.Errorf("the sums visited %d rows and cells, want at most %d", e.steps, 10*n)
	}
}

// TestApplyFormulas_Volatile locks NOW() and TODAY() under Calc's rules:
// the print date's moment and day, in the locale's date and time and date
// formats, and a reference to NOW() taking its format; under Excel's
// rules, and without a print date, they keep their stored results.
//
// # Corpus evidence
//
//   - libreoffice/ods: sc/tdf126116.fods prints the year of its =TODAY()
//     as the year it was printed; sc/cachedValue.ods prints =NOW() and
//     =[.A1] as MM/DD/YY HH:MM AM/PM at the moment of printing.
func TestApplyFormulas_Volatile(t *testing.T) {
	at := time.Date(2026, 11, 12, 9, 37, 30, 0, time.UTC)
	doc := formulaDoc("NOW()", "TODAY()", "A6", "NOW(1)")
	out := newBuilder(context.Background(), doc, nil, office.Options{PrintDate: at}).formulasOf(t, doc)
	row := out.Sheets[0].Rows[3]
	want := []struct {
		n      float64
		format string
	}{
		{46338 + (9*3600+37*60+30)/86400.0, "MM/DD/YY HH:MM AM/PM"},
		{46338, "MM/DD/YY"},
		{46338 + (9*3600+37*60+30)/86400.0, "MM/DD/YY HH:MM AM/PM"},
		{-1, ""},
	}
	for i, w := range want {
		c := row.Cells[i]
		format := ""
		if c.Style != nil {
			format = c.Style.Format
		}
		if c.Value.Kind != Number || !near(c.Value.Number, w.n, 1e-9) || format != w.format {
			t.Errorf("cell %d = %+v format %q, want %v %q", i, c.Value, format, w.n, w.format)
		}
	}
	// A reference to NOW() in a date-formatted cell takes the cell's
	// format (sc/dependencyTree.ods prints =[.A5] of a dated =NOW() as
	// 11/12/26).
	dated := formulaDoc("NOW()", "A6")
	dated.Sheets[0].Rows[3].Cells[0].Style = &Style{Format: "MM/DD/YY"}
	out = newBuilder(context.Background(), dated, nil, office.Options{PrintDate: at}).formulasOf(t, dated)
	if c := out.Sheets[0].Rows[3].Cells[1]; c.Style == nil || c.Style.Format != "MM/DD/YY" {
		t.Errorf("a reference to a dated NOW() = %+v, want MM/DD/YY", c.Style)
	}
	for _, o := range []office.Options{{}, {PrintDate: at}} {
		doc := formulaDoc("NOW()")
		doc.ExcelRules = !o.PrintDate.IsZero()
		out := newBuilder(context.Background(), doc, nil, o).formulasOf(t, doc)
		if v := out.Sheets[0].Rows[3].Cells[0].Value; v.Number != -1 {
			t.Errorf("excel %v: NOW() = %+v, want the stored -1", doc.ExcelRules, v)
		}
	}
}
