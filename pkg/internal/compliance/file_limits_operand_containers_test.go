package compliance

import (
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestLimitsInOperandArraysAndDictionaries pins that the implementation limits
// judge every string and number an array or a dictionary operand holds, at any
// depth: a dash array, a TJ array, a marked-content property list and an
// inline image's dictionary, nested arrays and dictionaries included. A lone
// "-" or a "1.5.5" there is the largest integer or real veraPDF gives a number
// it cannot read. Every case was measured with veraPDF 1.30 at PDF/A-1b and
// PDF/A-2b on the same one-page fixtures, which fail the limit as many times as
// the case states.
func TestLimitsInOperandArraysAndDictionaries(t *testing.T) {
	t.Parallel()

	const (
		catalog = "<< /Type /Catalog /Pages 2 0 R >>"
		pages   = "<< /Type /Pages /Kids [3 0 R] /Count 1 >>"
		page    = "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << >> /Contents 4 0 R >>"
		path    = " 0 0 m 10 10 l S"
		image   = "q 10 0 0 10 0 0 cm BI /W 1 /H 1 /BPC 8 /CS /G "
		denorm  = "0.000000000000000000000000000000000000000001"
	)
	long := strings.Repeat("x", 40000)
	for _, tc := range []struct {
		name    string
		content string
		// integer, real1, real2, denormal and string2 count the integer, real
		// and denormal limits and, at PDF/A-2b, the string limit, the real
		// limit at PDF/A-1b in real1 and at PDF/A-2b in real2.
		integer, real1, real2, denormal, string2 int
		saturated                                bool
	}{
		{name: "an integer in a dash array", content: "[99999999999 2] 0 d" + path, integer: 1},
		{name: "an integer in a nested array", content: "[[99999999999] 2] 0 d" + path, integer: 1},
		{name: "a real in a nested array", content: "[[99999.5] 2] 0 d" + path, real1: 1},
		{name: "a lone minus in a dash array", content: "[- 2] 0 d" + path, integer: 1, saturated: true},
		{name: "an integer in a TJ array", content: "BT [(a) 99999999999 (b)] TJ ET", integer: 1},
		{name: "an integer deep in a TJ array", content: "BT [(a) [[<< /K [99999999999] >>]] (b)] TJ ET", integer: 1},
		{name: "a denormal in a TJ array", content: "BT [(a) " + denorm + " (b)] TJ ET", denormal: 1},
		{name: "a long string in a dash array", content: "[(" + long + ") 2] 0 d" + path, string2: 1},
		{name: "two periods in a property list", content: "/Span << /MCID 1.5.5 >> BDC EMC", real1: 1, real2: 1, saturated: true},
		{name: "a lone minus in a property list", content: "/Span << /MCID - >> BDC EMC", integer: 1, saturated: true},
		{name: "an integer in a property list", content: "/Span << /X 99999999999 >> BDC EMC", integer: 1},
		{name: "two integers in a property list", content: "/Span << /MCID 0 /A 99999999999 /B 99999999999 >> BDC EMC", integer: 2},
		{name: "an integer in an array of a property list", content: "/Span << /X [99999999999] >> BDC EMC", integer: 1},
		{name: "a real in a dictionary of a property list", content: "/Span << /X << /Y 99999.5 >> >> BDC EMC", real1: 1},
		{name: "a denormal in a property list", content: "/Span << /X " + denorm + " >> BDC EMC", denormal: 1},
		{name: "a long string in a property list", content: "/Span << /X (" + long + ") >> BDC EMC", string2: 1},
		{name: "an integer in a DP property list", content: "/Tag << /X 99999999999 >> DP", integer: 1},
		{name: "an integer in an inline image", content: image + "/X 99999999999 ID \x00 EI Q", integer: 1},
		{name: "an integer in an inline image's Decode", content: image + "/D [0 99999999999] ID \x00 EI Q", integer: 1},
		{name: "an integer in an inline image's DecodeParms", content: image + "/DP << /K 99999999999 >> ID \x00 EI Q", integer: 1},
		{name: "a real in an inline image", content: image + "/X 99999.5 ID \x00 EI Q", real1: 1},
		{name: "two periods in an inline image", content: image + "/X 1.5.5 ID \x00 EI Q", real1: 1, real2: 1, saturated: true},
		{name: "a long string in an inline image", content: image + "/X (" + long + ") ID \x00 EI Q", string2: 1},
		{name: "numbers in the limits", content: "[3 2.5] 0 d /Span << /MCID 0 /X [1.5 -2] >> BDC EMC" + path},
	} {
		for _, p := range []struct {
			profile                              models.PDFAProfile
			integer, real, denormal, stringLimit Rule
		}{
			{models.PDFA1b, Rule_19005_1_6_1_12_t01, Rule_19005_1_6_1_12_t02, Rule{}, Rule_19005_1_6_1_12_t03},
			{models.PDFA2b, Rule_19005_2_6_1_13_t01, Rule_19005_2_6_1_13_t02, Rule_19005_2_6_1_13_t05, Rule_19005_2_6_1_13_t03},
		} {
			t.Run(tc.name+" "+string(p.profile), func(t *testing.T) {
				t.Parallel()
				doc, _ := objsDocPDF(t, catalog, pages, page, exponentStream("", tc.content))
				viols, err := ValidatePDFA(context.Background(), doc, p.profile)
				if err != nil {
					t.Fatalf("ValidatePDFA: %v", err)
				}
				want := []struct {
					rule Rule
					n    int
				}{{p.integer, tc.integer}, {p.real, tc.real1}, {p.stringLimit, 0}}
				if p.profile == models.PDFA2b {
					want = []struct {
						rule Rule
						n    int
					}{{p.integer, tc.integer}, {p.real, tc.real2}, {p.denormal, tc.denormal}, {p.stringLimit, tc.string2}}
				}
				for _, w := range want {
					if got := countRuleErrs(t, viols, w.rule); got != w.n {
						t.Errorf("%s test %d fired %d times, want %d: %v", w.rule.Clause, w.rule.TestNumber, got, w.n, viols)
					}
				}
				for _, e := range viols {
					v, ok := e.(Violation)
					if !ok || (v.Rule != p.integer && v.Rule != p.real) {
						continue
					}
					if strings.HasSuffix(v.Context, SaturatedNumberNote) != tc.saturated {
						t.Errorf("the limit violation %q names the value veraPDF gives a number it cannot read: %v, want %v", v.Context, !tc.saturated, tc.saturated)
					}
				}
			})
		}
	}
}
