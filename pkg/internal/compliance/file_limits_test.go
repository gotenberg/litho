package compliance

import (
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestNameEscapesExpandedOnce pins that the name rules judge the bytes a name
// stands for, with its #XX escapes expanded once, by the lexer (ISO 32000-1,
// 7.3.5). A name written /#2341 stands for the three bytes "#41", not for "A".
// Measured with veraPDF 1.30 on one-page fixtures:
//
//   - 43 times /#2341, 129 bytes, fails the name length test, ISO 19005-1,
//     6.1.12 t4 and ISO 19005-2, 6.1.13 t4, as 130 times /#41 does, and 43
//     times /A passes it.
//   - /BaseFont /Foo#23C3, the valid UTF-8 "Foo#C3", passes ISO 19005-2, 6.1.8
//     t1 and ISO 19005-4, 6.1.7 t1, and /Foo#C3 fails them.
//   - /Version /#2332#2E0, "#32.0", fails ISO 19005-4, 6.1.12 t1, and
//     /Version /2#2E0, "2.0", passes it.
func TestNameEscapesExpandedOnce(t *testing.T) {
	t.Parallel()

	doc := func(catalogExtra, pageExtra, baseFont string) []string {
		content := "BT /F1 12 Tf 10 10 Td (a) Tj ET"
		return []string{
			"<< /Type /Catalog /Pages 2 0 R" + catalogExtra + " >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R " +
				"/Resources << /Font << /F1 5 0 R >> >>" + pageExtra + " >>",
			exponentStream("", content),
			"<< /Type /Font /Subtype /Type1 /BaseFont /" + baseFont + " >>",
		}
	}
	for _, tc := range []struct {
		name    string
		objs    []string
		profile models.PDFAProfile
		rule    Rule
		want    int
	}{
		{
			"a 129-byte name spelling escapes at part 1", doc("", " /Foo /"+strings.Repeat("#2341", 43), "Helvetica"),
			models.PDFA1b, Rule_19005_1_6_1_12_t04, 1,
		},
		{
			"a 129-byte name spelling escapes at part 2", doc("", " /Foo /"+strings.Repeat("#2341", 43), "Helvetica"),
			models.PDFA2b, Rule_19005_2_6_1_13_t04, 1,
		},
		{
			"a 130-byte name written escaped", doc("", " /Foo /"+strings.Repeat("#41", 130), "Helvetica"),
			models.PDFA2b, Rule_19005_2_6_1_13_t04, 1,
		},
		{
			"a 43-byte name", doc("", " /Foo /"+strings.Repeat("A", 43), "Helvetica"),
			models.PDFA2b, Rule_19005_2_6_1_13_t04, 0,
		},
		{
			"a valid UTF-8 name spelling an escape at part 2", doc("", "", "Foo#23C3"),
			models.PDFA2b, Rule_19005_2_6_1_8_t01, 0,
		},
		{
			"a valid UTF-8 name spelling an escape at part 4", doc("", "", "Foo#23C3"),
			models.PDFA4, Rule_19005_4_6_1_7_t01, 0,
		},
		{
			"an invalid UTF-8 name", doc("", "", "Foo#C3"),
			models.PDFA2b, Rule_19005_2_6_1_8_t01, 1,
		},
		{
			"a version spelling an escape", doc(" /Version /#2332#2E0", "", "Helvetica"),
			models.PDFA4, Rule_19005_4_6_1_12_t01, 1,
		},
		{
			"a version written escaped", doc(" /Version /2#2E0", "", "Helvetica"),
			models.PDFA4, Rule_19005_4_6_1_12_t01, 0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d, _ := objsDocPDF(t, tc.objs...)
			viols, err := ValidatePDFA(context.Background(), d, tc.profile)
			if err != nil {
				t.Fatalf("ValidatePDFA: %v", err)
			}
			if got := countRuleErrs(t, viols, tc.rule); got != tc.want {
				t.Errorf("%s test %d fired %d times, want %d: %v", tc.rule.Clause, tc.rule.TestNumber, got, tc.want, viols)
			}
		})
	}
}
