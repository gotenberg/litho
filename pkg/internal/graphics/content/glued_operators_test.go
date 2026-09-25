package content

import (
	"context"
	"reflect"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestSplitGluedOperators pins the PDF.js reading of operators written with no
// white space between them, and the operands each one keeps.
//
// # Corpus evidence
//
//   - pdfjs sci-notation.pdf: writes "ETBT" between its two text objects.
//     Read as one undefined operator, the Td after it moved the second line
//     from the first line's origin, inside the first text object, and the
//     line left the extracted text.
func TestSplitGluedOperators(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		src      string
		want     []string
		operands map[int][]ast.PdfObject
	}{
		{
			name: "ET run into BT",
			src:  "BT /F1 24 Tf 50 500 Td (a) Tj ETBT /F1 12 Tf 50 600 Td (b) Tj ET",
			want: []string{"BT", "Tf", "Td", "Tj", "ET", "BT", "Tf", "Td", "Tj", "ET"},
			operands: map[int][]ast.PdfObject{
				6: {ast.PdfName("F1"), ast.PdfInt(12)},
				7: {ast.PdfInt(50), ast.PdfInt(600)},
			},
		},
		{name: "q run into Q", src: "qQ", want: []string{"q", "Q"}},
		{name: "BT run into ET", src: "BTET", want: []string{"BT", "ET"}},
		{name: "an operator run into an unknown one", src: "ETXX", want: []string{"ET", "XX"}},
		{name: "an unknown run no operator starts", src: "XXET", want: []string{"XXET"}},
		{
			name:     "Tf run into T*",
			src:      "/F1 12 TfT*",
			want:     []string{"Tf", "T*"},
			operands: map[int][]ast.PdfObject{0: {ast.PdfName("F1"), ast.PdfInt(12)}, 1: nil},
		},
		{name: "T* run into Tf", src: "T*/F1 12 Tf", want: []string{"T*", "Tf"}},
		{
			name:     "an operator run into its operand",
			src:      "q1 0 0 1 0 0 cm",
			want:     []string{"q", "cm"},
			operands: map[int][]ast.PdfObject{1: {ast.PdfInt(1), ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(1), ast.PdfInt(0), ast.PdfInt(0)}},
		},
		{name: "operators that extend a shorter one", src: "/CS0 SCN /P BMC /P <<>> BDC EMC", want: []string{"SCN", "BMC", "BDC", "EMC"}},
		{
			name:     "operand keywords reached through operators",
			src:      "[true false null] TJ",
			want:     []string{"TJ"},
			operands: map[int][]ast.PdfObject{0: {ast.PdfArray{ast.PdfBool(true), ast.PdfBool(false), ast.PdfNull{}}}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ops, err := ParseStreamData(context.Background(), []byte(tc.src), SplitGluedOperators())
			if err != nil {
				t.Fatalf("ParseStreamData(%q): %v", tc.src, err)
			}
			if got := operators(ops); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ParseStreamData(%q) operators = %v, want %v", tc.src, got, tc.want)
			}
			for i, want := range tc.operands {
				if got := ops[i].Operands; !reflect.DeepEqual(got, want) {
					t.Errorf("ParseStreamData(%q) %s operands = %#v, want %#v", tc.src, ops[i].Operator, got, want)
				}
			}
		})
	}
}

// TestGluedOperatorsStayWholeByDefault pins the reading compliance takes: with
// no option, a run of letters ends only at white space, a delimiter (ISO
// 32000-1, 7.2.2) or a digit, so "ETBT" is the one operator veraPDF reports as
// undefined under ISO 19005-2, 6.2.2 t1.
func TestGluedOperatorsStayWholeByDefault(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		src  string
		want []string
	}{
		{"BT (a) Tj ETBT (b) Tj ET", []string{"BT", "Tj", "ETBT", "Tj", "ET"}},
		{"qQ", []string{"qQ"}},
		{"BTET", []string{"BTET"}},
		{"ETXX", []string{"ETXX"}},
		{"/F1 12 TfT*", []string{"TfT*"}},
	} {
		ops, err := ParseStreamData(context.Background(), []byte(tc.src))
		if err != nil {
			t.Fatalf("ParseStreamData(%q): %v", tc.src, err)
		}
		if got := operators(ops); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("ParseStreamData(%q) operators = %v, want %v", tc.src, got, tc.want)
		}
		compliance, _, _, err := ParseStreamDataForCompliance(context.Background(), []byte(tc.src))
		if err != nil {
			t.Fatalf("ParseStreamDataForCompliance(%q): %v", tc.src, err)
		}
		if got := operators(compliance); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("ParseStreamDataForCompliance(%q) operators = %v, want %v", tc.src, got, tc.want)
		}
	}
}

// TestDefaultReadingEndsOperatorsAtDigits pins that every parse without an
// option ends an operator before a digit, as veraPDF does
// (PDFStreamParser.nextOperator): "q1" is the q veraPDF counts toward the
// nesting limit, the digits are the operands of the operator after them, and a
// run from "n" stays whole, one operator veraPDF reports as undefined. Measured
// with veraPDF 1.30 at PDF/A-1b and PDF/A-2b.
func TestDefaultReadingEndsOperatorsAtDigits(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	src := "q1 0 0 1 0 0 cm Q n1"
	want := []string{"q", "cm", "Q", "n1"}
	cm := []ast.PdfObject{ast.PdfInt(1), ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(1), ast.PdfInt(0), ast.PdfInt(0)}
	check := func(name string, ops []Operation) {
		t.Helper()
		if got := operators(ops); !reflect.DeepEqual(got, want) {
			t.Errorf("%s(%q) operators = %v, want %v", name, src, got, want)
			return
		}
		if len(ops[0].Operands) != 0 || !reflect.DeepEqual(ops[1].Operands, cm) {
			t.Errorf("%s(%q) operands q=%v cm=%v, want none and %v", name, src, ops[0].Operands, ops[1].Operands, cm)
		}
	}

	ops, err := ParseStreamData(ctx, []byte(src))
	if err != nil {
		t.Fatalf("ParseStreamData: %v", err)
	}
	check("ParseStreamData", ops)
	ops, ranges, err := ParseStreamDataWithOffsets(ctx, []byte(src))
	if err != nil {
		t.Fatalf("ParseStreamDataWithOffsets: %v", err)
	}
	check("ParseStreamDataWithOffsets", ops)
	for i, wantRange := range []string{"q", "1 0 0 1 0 0 cm", " Q", " n1"} {
		if got := src[ranges[i].Start:ranges[i].End]; got != wantRange {
			t.Errorf("%s range covers %q, want %q", ops[i].Operator, got, wantRange)
		}
	}
	ops, _ = ParseStreamDataFormatChecked(ctx, []byte(src))
	check("ParseStreamDataFormatChecked", ops)
	ops, _, err = ParseStreamDataWithOverflows(ctx, []byte(src))
	if err != nil {
		t.Fatalf("ParseStreamDataWithOverflows: %v", err)
	}
	check("ParseStreamDataWithOverflows", ops)
	ops, _, _, err = ParseStreamDataForCompliance(ctx, []byte(src))
	if err != nil {
		t.Fatalf("ParseStreamDataForCompliance: %v", err)
	}
	check("ParseStreamDataForCompliance", ops)
}

// TestSplitGluedOperatorsShareTheRunBytes pins the byte ranges of operators
// split from one run: they stay contiguous, so a caller splicing by range
// reaches each operator's own bytes.
func TestSplitGluedOperatorsShareTheRunBytes(t *testing.T) {
	t.Parallel()

	src := "(a) Tj ETBT (b) Tj"
	ops, ranges, err := ParseStreamDataWithOffsets(context.Background(), []byte(src), SplitGluedOperators())
	if err != nil {
		t.Fatalf("ParseStreamDataWithOffsets: %v", err)
	}
	if got, want := operators(ops), []string{"Tj", "ET", "BT", "Tj"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("operators = %v, want %v", got, want)
	}
	want := []string{"(a) Tj", " ET", "BT", " (b) Tj"}
	for i, r := range ranges {
		if got := src[r.Start:r.End]; got != want[i] {
			t.Errorf("%s range covers %q, want %q", ops[i].Operator, got, want[i])
		}
	}
}

// TestGluedOperators pins which keywords GluedOperators takes apart: a run the
// split reading reads as operators alone.
func TestGluedOperators(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		keyword string
		want    []string
	}{
		{"ETBT", []string{"ET", "BT"}},
		{"qQ", []string{"q", "Q"}},
		{"BTET", []string{"BT", "ET"}},
		{"TfT*", []string{"Tf", "T*"}},
		{"T*Tf", []string{"T*", "Tf"}},
		{"qqQQ", []string{"q", "q", "Q", "Q"}},
		{"ETXX", nil},
		{"junk", nil},
		{"q1", nil},
		{"falsef", nil},
		{"ET", nil},
		{"BDC", nil},
		{"ET BT", nil},
		{"BB ", nil},
		{"", nil},
	} {
		if got := GluedOperators(tc.keyword); !slices.Equal(got, tc.want) {
			t.Errorf("GluedOperators(%q) = %q, want %q", tc.keyword, got, tc.want)
		}
	}
}

// TestSplitGluedOperatorsPartialCommandsMatchPDFJS pins the partial commands
// the split reading derives against the ones PDF.js lists by hand as the
// "reserved partial commands for the lexer" of EvaluatorPreprocessor.opMap
// (src/core/evaluator.js, v6.3.289): BM, BD, fa, fal, fals, nu and nul. A
// partial command ends a run the way an operator does, so one more or one
// fewer than PDF.js would split some run where PDF.js does not.
//
// A proper prefix p of an operator or keyword is a partial command exactly when
// the run p~ reads as p first, since no operator holds a tilde.
func TestSplitGluedOperatorsPartialCommandsMatchPDFJS(t *testing.T) {
	t.Parallel()

	words := append(annexAOperators[:], "true", "false", "null")
	isWord := make(map[string]bool, len(words))
	for _, w := range words {
		isWord[w] = true
	}
	var partial []string
	seen := map[string]bool{}
	for _, w := range words {
		for i := 1; i < len(w); i++ {
			p := w[:i]
			if isWord[p] || seen[p] {
				continue
			}
			seen[p] = true
			ops, err := ParseStreamData(context.Background(), []byte(p+"~"), SplitGluedOperators())
			if err != nil {
				t.Fatalf("ParseStreamData(%q): %v", p+"~", err)
			}
			if len(ops) > 0 && ops[0].Operator == p {
				partial = append(partial, p)
			}
		}
	}
	slices.Sort(partial)
	if want := []string{"BD", "BM", "fa", "fal", "fals", "nu", "nul"}; !slices.Equal(partial, want) {
		t.Errorf("partial commands = %v, PDF.js lists %v", partial, want)
	}
}
