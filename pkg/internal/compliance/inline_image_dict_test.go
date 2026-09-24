package compliance

import (
	"fmt"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestExtractInlineImageFilters pins how an inline image dictionary's filters
// are read: a ']' glued to the last name ends the array, a comment hides what
// it holds (ISO 32000-1, 7.2.4), and the abbreviation /F decides over /Filter
// whichever comes first, as veraPDF reads them.
//
// # Corpus evidence
//
//   - pdfjs issue14256.pdf: writes every one of these.
func TestExtractInlineImageFilters(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		dict string
		want []string
	}{
		{"/W 1 /F [/AHx] /L 3", []string{"AHx"}},
		{"/F [ /AHx /Fl ] /DP [ null << /Predictor 15 >> ]", []string{"AHx", "Fl"}},
		{"/Filter [/ASCIIHexDecode] /Length 1240", []string{"ASCIIHexDecode"}},
		{"%% /F /LZW\n/F /AHx", []string{"AHx"}},
		{"/Filter /LZWDecode /F /AHx", []string{"AHx"}},
		{"/F /LZW /Filter /AHx", []string{"LZW"}},
		{"/W 1 /H 1", nil},
	} {
		t.Run(tt.dict, func(t *testing.T) {
			t.Parallel()
			if got := extractInlineImageFilters([]byte(tt.dict)); !slices.Equal(got, tt.want) {
				t.Errorf("extractInlineImageFilters(%q) = %q, want %q", tt.dict, got, tt.want)
			}
		})
	}
}

// TestInlineImageInterpolateAbbreviationDecides pins ISO 19005-2, 6.2.8 t3 on
// an inline image that writes both /I and /Interpolate: the abbreviation
// decides, whichever comes first, as veraPDF reads it. Every case was measured
// against veraPDF.
func TestInlineImageInterpolateAbbreviationDecides(t *testing.T) {
	t.Parallel()

	doc := func(keys string) []byte {
		content := "q 10 0 0 10 0 0 cm BI /W 1 /H 1 /BPC 8 /CS /G " + keys + " ID \x80 EI Q"
		return cyclePDF(
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << >> /Contents 4 0 R >>",
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		)
	}
	for _, tc := range []struct {
		keys  string
		fires bool
	}{
		{"/I false /Interpolate true", false},
		{"/Interpolate true /I false", false},
		{"/Interpolate false /I true", true},
		{"/Interpolate true", true},
	} {
		t.Run(tc.keys, func(t *testing.T) {
			t.Parallel()
			if got := firesRuleProfile(t, doc(tc.keys), models.PDFA2b, "6.2.8", 3); got != tc.fires {
				t.Errorf("6.2.8 t3 violation = %v, want %v", got, tc.fires)
			}
		})
	}
}
