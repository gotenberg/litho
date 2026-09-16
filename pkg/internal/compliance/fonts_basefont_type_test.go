package compliance

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestFontBaseFontMustBeAName pins that a /BaseFont written as something other
// than a name fails the font required-key test, as an absent one does.
//
// ISO 32000-1, 9.6.2, Table 111 types /BaseFont as a name, and every part
// states the rule as "Subtype == Type3 || fontName != null": ISO 19005-1 6.3.2
// t3, ISO 19005-2 and -3 6.2.11.2 t3, ISO 19005-4 6.2.10.2 t3. A value of any
// other type resolves to no name at all, so it fails exactly as a missing entry
// does. pdfjs issue1350.pdf writes both its fonts that way, as a UTF-16BE hex
// string (/BaseFont<feff004100410043...>), and veraPDF reports it twice with
// "A BaseFont entry is missing or has invalid type" while naming the font null.
func TestFontBaseFontMustBeAName(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	const content = "BT /F0 12 Tf (A) Tj ET"
	for _, tc := range []struct {
		name     string
		baseFont string
		want     int
	}{
		{"a name", "/BaseFont /AAAAAA+Helvetica", 0},
		{"a hex string", "/BaseFont <feff00410042>", 1},
		{"a literal string", "/BaseFont (Helvetica)", 1},
		{"absent", "", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t,
				"<< /Type /Catalog /Pages 2 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R"+
					" /Resources << /Font << /F0 5 0 R >> >> >>",
				fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
				fmt.Sprintf("<< /Type /Font /Subtype /TrueType %s /FirstChar 65 /LastChar 65"+
					" /Widths [500] >>", tc.baseFont),
			)
			for _, p := range []struct {
				profile models.PDFAProfile
				rule    Rule
			}{
				{models.PDFA1b, Rule_19005_1_6_3_2_t03},
				{models.PDFA2b, Rule_19005_2_6_2_11_2_t03},
			} {
				viols, err := ValidatePDFA(ctx, doc, p.profile)
				if err != nil {
					t.Fatalf("ValidatePDFA: %v", err)
				}
				if got := countRuleErrs(t, viols, p.rule); got != tc.want {
					t.Errorf("%s %s t%d fired %d times, want %d",
						p.profile, p.rule.Clause, p.rule.TestNumber, got, tc.want)
				}
			}
		})
	}
}
