package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestCharSetOfAnUnembeddedSubset pins ISO 19005-1, 6.3.5 t2 on a Type 1
// subset whose program the file does not embed. The clause asks the /CharSet
// to list the names the subset defines, and with no program in the file no
// /CharSet can be shown to do that, so the part-1 rule fails whether or not
// the entry is present. Parts 2 and 3 scope the rule to an embedded Type 1
// font (ISO 19005-2, 6.2.11.4.2), so their test passes the same font.
//
// veraPDF reads it that way: charSetListsAllGlyphs is false while no program
// parsed, and only the part-2 profile exempts a font that carries no font
// file. A font without a subset prefix is out of both rules.
//
// # Corpus evidence
//
//   - verapdf PDF_A-2b__veraPDF_test_suite_6-2-11-4-1-t01-fail-a.pdf: the
//     unembedded QUAJCC+TrajanPro-Regular carries /CharSet
//     (/space/H/W/d/e/l/o/r), and veraPDF fails 6.3.5 t2 at PDF/A-1a and 1b
//     while litho passed it.
func TestCharSetOfAnUnembeddedSubset(t *testing.T) {
	t.Parallel()

	font := func(name, charSet string) []string {
		return []string{
			"<< /Type /Font /Subtype /Type1 /BaseFont /" + name + " /FirstChar 65 /LastChar 66 /Widths [600 600] " +
				"/Encoding /WinAnsiEncoding /FontDescriptor 6 0 R >>",
			"<< /Type /FontDescriptor /FontName /" + name + " /Flags 32 /Ascent 700 /Descent -200 /CapHeight 700 " +
				"/StemV 80 /FontBBox [0 0 500 700] /ItalicAngle 0" + charSet + " >>",
		}
	}

	tests := []struct {
		name    string
		font    []string
		profile models.PDFAProfile
		rule    Rule
		want    int
	}{
		{"a subset carrying /CharSet, PDF/A-1b", font("ABCDEF+Lato", " /CharSet (/A/B)"), models.PDFA1b, Rule_19005_1_6_3_5_t02, 1},
		{"a subset carrying /CharSet, PDF/A-1a", font("ABCDEF+Lato", " /CharSet (/A/B)"), models.PDFA1a, Rule_19005_1_6_3_5_t02, 1},
		{"a subset with no /CharSet, PDF/A-1b", font("ABCDEF+Lato", ""), models.PDFA1b, Rule_19005_1_6_3_5_t02, 1},
		{"a font with no subset prefix, PDF/A-1b", font("Lato", " /CharSet (/A/B)"), models.PDFA1b, Rule_19005_1_6_3_5_t02, 0},
		{"a subset carrying /CharSet, PDF/A-2b", font("ABCDEF+Lato", " /CharSet (/A/B)"), models.PDFA2b, Rule_19005_2_6_2_11_4_2_t01, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t, subsetFontObjs(tt.font, "(AB)", "3 0 R")...)
			viols, err := ValidatePDFA(context.Background(), doc, tt.profile)
			if err != nil {
				t.Fatalf("ValidatePDFA: %v", err)
			}
			if got := countRuleErrs(t, viols, tt.rule); got != tt.want {
				t.Errorf("%s test %d fired %d times, want %d", tt.rule.Clause, tt.rule.TestNumber, got, tt.want)
			}
		})
	}
}
