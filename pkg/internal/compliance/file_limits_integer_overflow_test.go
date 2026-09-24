package compliance

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestIntegerLiteralPastInt64Range pins ISO 19005-1, 6.1.12 and ISO 19005-2,
// 6.1.13 on an integer literal too large for 64 bits. ISO 32000-1, 7.3.3 makes
// a signed run of digits an integer object whatever its magnitude, so the
// literal fails the integer test (t1) although litho holds its value as a real,
// while a literal written with a decimal point stays a real and is judged by
// its magnitude (t2). Every case was measured against veraPDF.
//
// # Corpus evidence
//
//   - pdfjs issue20872.pdf: writes four such integers.
func TestIntegerLiteralPastInt64Range(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	const plain = "q 0 0 10 10 re f Q"
	for _, tt := range []struct {
		name    string
		catalog string
		content string
		a2, a1  [2]int // t1 and t2 counts under PDF/A-2b and PDF/A-1b
	}{
		{"a dictionary integer", " /Big 99999999999999999999999999999999999999999", plain, [2]int{1, 0}, [2]int{1, 0}},
		{"a negative dictionary integer", " /Big -99999999999999999999999999999999999999999", plain, [2]int{1, 0}, [2]int{1, 0}},
		{"an integer the real limit allows", " /Big 20000000000000000000", plain, [2]int{1, 0}, [2]int{1, 0}},
		{"an array integer", " /Big [1 99999999999999999999999999999999999999999]", plain, [2]int{1, 0}, [2]int{1, 0}},
		{"a content stream integer", "", "q 1 0 0 1 99999999999999999999999999999999999999999 0 cm 0 0 10 10 re f Q", [2]int{1, 0}, [2]int{1, 0}},
		{"a real literal", " /Big 99999999999999999999999999999999999999999.0", plain, [2]int{0, 1}, [2]int{0, 1}},
		{"a real literal the real limit allows", " /Big 20000000000000000000.0", plain, [2]int{0, 0}, [2]int{0, 1}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			for _, pc := range []struct {
				profile models.PDFAProfile
				t1, t2  Rule
				want    [2]int
			}{
				{models.PDFA2b, Rule_19005_2_6_1_13_t01, Rule_19005_2_6_1_13_t02, tt.a2},
				{models.PDFA1b, Rule_19005_1_6_1_12_t01, Rule_19005_1_6_1_12_t02, tt.a1},
			} {
				doc, _ := objsDocPDF(t,
					"<< /Type /Catalog /Pages 2 0 R"+tt.catalog+" >>",
					"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
					"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R /Resources << >> >>",
					fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(tt.content), tt.content),
				)
				viols, err := ValidatePDFA(ctx, doc, pc.profile)
				if err != nil {
					t.Fatalf("ValidatePDFA(%s): %v", pc.profile, err)
				}
				for i, rule := range []Rule{pc.t1, pc.t2} {
					if got := countRuleErrs(t, viols, rule); got != pc.want[i] {
						t.Errorf("%s %s t%d fired %d times, want %d", pc.profile, rule.Clause, rule.TestNumber, got, pc.want[i])
					}
				}
			}
		})
	}
}
