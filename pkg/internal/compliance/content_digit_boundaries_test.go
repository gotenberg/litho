package compliance

import (
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestUndefinedOperatorsEndAtDigits pins that compliance reads an operator
// written against a digit as veraPDF reads it (PDFStreamParser.nextOperator):
// "q1" is q then the number 1, and no undefined operator, while "xx1" is the
// undefined operator xx, and a run from "n" or "B" stays one undefined
// operator. Measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b on the same
// one-page fixtures, 29 levels of "q1" also fail the q/Q nesting test, as 29 of
// "q" do.
func TestUndefinedOperatorsEndAtDigits(t *testing.T) {
	t.Parallel()

	const (
		catalog = "<< /Type /Catalog /Pages 2 0 R >>"
		pages   = "<< /Type /Pages /Kids [3 0 R] /Count 1 >>"
		page    = "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << >> /Contents 4 0 R >>"
	)
	for _, tc := range []struct {
		name      string
		content   string
		undefined int
		nesting   int
	}{
		{"an operator then its operand", "q1 0 0 1 0 0 cm Q", 0, 0},
		{"an operator then the operand of the next", "2 w1 J 0 0 m 10 10 l S", 0, 0},
		{"an undefined operator then a number", "xx1 0 0 1 0 0 cm", 1, 0},
		{"a run from n", "0 0 10 10 re W n1 0 0 1 0 0 cm", 1, 0},
		{"a run from B", "BT1 ET", 1, 0},
		{"29 levels of q written against a digit", strings.Repeat("q1 0 0 1 0 0 cm\n", 29) + strings.Repeat("Q\n", 29), 0, 1},
	} {
		for _, p := range []struct {
			profile   models.PDFAProfile
			undefined Rule
			nesting   Rule
		}{
			{models.PDFA1b, Rule_19005_1_6_2_10_t01, Rule_19005_1_6_1_12_t08},
			{models.PDFA2b, Rule_19005_2_6_2_2_t01, Rule_19005_2_6_1_13_t08},
		} {
			t.Run(tc.name+" "+string(p.profile), func(t *testing.T) {
				t.Parallel()
				doc, _ := objsDocPDF(t, catalog, pages, page, exponentStream("", tc.content))
				viols, err := ValidatePDFA(context.Background(), doc, p.profile)
				if err != nil {
					t.Fatalf("ValidatePDFA: %v", err)
				}
				if got := countRuleErrs(t, viols, p.undefined); got != tc.undefined {
					t.Errorf("undefined-operator test fired %d times, want %d: %v", got, tc.undefined, viols)
				}
				if got := countRuleErrs(t, viols, p.nesting); got != tc.nesting {
					t.Errorf("nesting test fired %d times, want %d: %v", got, tc.nesting, viols)
				}
			})
		}
	}
}

// TestNumbersEndAsVeraPDFEndsThem pins that compliance reads a number in a
// content stream as veraPDF reads it (BaseParser.readNumber): the number ends
// before the first character other than a digit or a period, and a run that
// spells no number of ISO 32000-1, 7.3.3 is the largest integer or real veraPDF
// gives a number, which fails the implementation limits where it is the operand
// of an operator ISO 32000-1 defines. Every case was measured with veraPDF 1.30
// at PDF/A-1b and PDF/A-2b on the same one-page fixtures.
func TestNumbersEndAsVeraPDFEndsThem(t *testing.T) {
	t.Parallel()

	const (
		catalog = "<< /Type /Catalog /Pages 2 0 R >>"
		pages   = "<< /Type /Pages /Kids [3 0 R] /Count 1 >>"
		page    = "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << >> /Contents 4 0 R >>"
	)
	for _, tc := range []struct {
		name                     string
		content                  string
		undefined, integer, real int
	}{
		{"an operator after a number", "0 0 m 10 10l S 1 0 0 1 0 0cm", 0, 0, 0},
		{"a sign after a number", "5-3 m 10 10 l S", 0, 0, 0},
		{"two periods", "0 0 m .020.594 10 l S", 0, 0, 1},
		{"two periods in an array", "[1.2.3 2] 0 d 0 0 m 10 10 l S", 0, 0, 1},
		{"a lone minus", "0 - m 10 10 l S", 0, 1, 0},
		{"a minus before a negative number", "--5 0 m 10 10 l S", 0, 1, 0},
		{"an exponent", "1e2 w", 1, 0, 0},
		{"a signed exponent", "-1e-2 w", 1, 0, 0},
		{"a number sign", "12#3 w", 1, 0, 0},
		{"a period in an operator", "0 0 m 95.c.455 1 1 1 1 1 c S", 1, 0, 0},
		{"a lone minus before an undefined operator", "- xx 0 0 m 10 10 l S", 1, 0, 0},
		{"two periods before an undefined operator", "1.5.5 xx 0 0 m 10 10 l S", 1, 0, 0},
		{"a large integer before an undefined operator", "99999999999 xx 0 0 m 10 10 l S", 1, 0, 0},
	} {
		for _, p := range []struct {
			profile                  models.PDFAProfile
			undefined, integer, real Rule
		}{
			{models.PDFA1b, Rule_19005_1_6_2_10_t01, Rule_19005_1_6_1_12_t01, Rule_19005_1_6_1_12_t02},
			{models.PDFA2b, Rule_19005_2_6_2_2_t01, Rule_19005_2_6_1_13_t01, Rule_19005_2_6_1_13_t02},
		} {
			t.Run(tc.name+" "+string(p.profile), func(t *testing.T) {
				t.Parallel()
				doc, _ := objsDocPDF(t, catalog, pages, page, exponentStream("", tc.content))
				viols, err := ValidatePDFA(context.Background(), doc, p.profile)
				if err != nil {
					t.Fatalf("ValidatePDFA: %v", err)
				}
				for _, want := range []struct {
					rule Rule
					n    int
				}{{p.undefined, tc.undefined}, {p.integer, tc.integer}, {p.real, tc.real}} {
					if got := countRuleErrs(t, viols, want.rule); got != want.n {
						t.Errorf("%s test %d fired %d times, want %d: %v", want.rule.Clause, want.rule.TestNumber, got, want.n, viols)
					}
				}
				for _, e := range viols {
					if v, ok := e.(Violation); ok && (v.Rule == p.integer || v.Rule == p.real) && !strings.HasSuffix(v.Context, SaturatedNumberNote) {
						t.Errorf("the limit violation %q does not name the value veraPDF reads", v.Context)
					}
				}
			})
		}
	}
}

// TestUndefinedOperatorsRunOnThroughDelimiters pins that compliance reads an
// operator as veraPDF reads it (PDFStreamParser.nextOperator): ")", ">", "{",
// "}" and "%" do not end it where an operator stands, while a string, an array
// or a dictionary does, and a keyword inside a dictionary ends at them. Every
// case was measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b on the same
// one-page fixtures.
func TestUndefinedOperatorsRunOnThroughDelimiters(t *testing.T) {
	t.Parallel()

	const (
		catalog = "<< /Type /Catalog /Pages 2 0 R >>"
		pages   = "<< /Type /Pages /Kids [3 0 R] /Count 1 >>"
		page    = "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << >> /Contents 4 0 R >>"
	)
	for _, tc := range []struct {
		name      string
		content   string
		undefined int
	}{
		{"a comment", "q Q%comment\n", 1},
		{"a comment and the words after it", "q Q%xx yy zz\n", 3},
		{"a comment at the end of the stream", "q Q%xx", 1},
		{"a closing parenthesis", "q Q) \n", 1},
		{"a dictionary close", "q Q>> \n", 1},
		{"braces", "q Q{ \n", 1},
		{"an operator after the parenthesis", "BT (a) T)Tj ET\n", 1},
		{"a string ends the operator", "q Q(x) Tj\n", 0},
		{"a keyword inside a dictionary", "/P <</MCID 0 /A xx>> BDC EMC\n", 0},
		{"white space before the comment", "q Q %comment\n", 0},
	} {
		for _, p := range []struct {
			profile   models.PDFAProfile
			undefined Rule
		}{
			{models.PDFA1b, Rule_19005_1_6_2_10_t01},
			{models.PDFA2b, Rule_19005_2_6_2_2_t01},
		} {
			t.Run(tc.name+" "+string(p.profile), func(t *testing.T) {
				t.Parallel()
				doc, _ := objsDocPDF(t, catalog, pages, page, exponentStream("", tc.content))
				viols, err := ValidatePDFA(context.Background(), doc, p.profile)
				if err != nil {
					t.Fatalf("ValidatePDFA: %v", err)
				}
				if got := countRuleErrs(t, viols, p.undefined); got != tc.undefined {
					t.Errorf("undefined-operator test fired %d times, want %d: %v", got, tc.undefined, viols)
				}
			})
		}
	}
}

// TestStrayDelimitersAreUndefinedOperators pins that compliance reads a ")",
// ">", "{" or "}" where an operator stands as veraPDF reads it
// (PDFStreamParser.nextOperator): the start of an undefined operator, which
// runs on as any operator does, while inside a dictionary it is no operator.
// Every case was measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b on the
// same one-page fixtures.
func TestStrayDelimitersAreUndefinedOperators(t *testing.T) {
	t.Parallel()

	const (
		catalog = "<< /Type /Catalog /Pages 2 0 R >>"
		pages   = "<< /Type /Pages /Kids [3 0 R] /Count 1 >>"
		page    = "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << >> /Contents 4 0 R >>"
	)
	for _, tc := range []struct {
		name      string
		content   string
		undefined int
	}{
		{"a closing parenthesis", "q ) Q\n", 1},
		{"an opening brace", "q { Q\n", 1},
		{"a closing brace", "q } Q\n", 1},
		{"a greater-than sign", "q > Q\n", 1},
		{"a dictionary close", "q >> Q\n", 1},
		{"an operator run on", "q )Tj) Q\n", 1},
		{"a digit after a brace", "q }1 w Q\n", 1},
		{"a name after a greater-than sign", "q >/F1 12 Tf Q\n", 1},
		{"between the operands of an operator", "0 0 m 5 ) 10 10 l S\n", 1},
		{"inside a dictionary", "/P <</A )>> BDC EMC\n", 0},
		{"a bracket inside a dictionary", "/P <</A ]>> BDC EMC\n", 0},
	} {
		for _, p := range []struct {
			profile   models.PDFAProfile
			undefined Rule
		}{
			{models.PDFA1b, Rule_19005_1_6_2_10_t01},
			{models.PDFA2b, Rule_19005_2_6_2_2_t01},
		} {
			t.Run(tc.name+" "+string(p.profile), func(t *testing.T) {
				t.Parallel()
				doc, _ := objsDocPDF(t, catalog, pages, page, exponentStream("", tc.content))
				viols, err := ValidatePDFA(context.Background(), doc, p.profile)
				if err != nil {
					t.Fatalf("ValidatePDFA: %v", err)
				}
				if got := countRuleErrs(t, viols, p.undefined); got != tc.undefined {
					t.Errorf("undefined-operator test fired %d times, want %d: %v", got, tc.undefined, viols)
				}
			})
		}
	}
}
