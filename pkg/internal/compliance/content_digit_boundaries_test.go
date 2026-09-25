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
