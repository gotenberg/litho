package compliance

import (
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestColorScope_InlineImageMaskColorSpaceIgnored pins that a /CS written on an
// inline image mask selects no colour: ISO 32000-1, 8.9.5, Table 89 does not
// allow the entry on a mask, which paints with the current fill colour. veraPDF
// ignores it, as it ignores /ColorSpace on an image mask XObject, while an
// inline image that is no mask still uses its /CS. Every case was measured
// against veraPDF.
func TestColorScope_InlineImageMaskColorSpaceIgnored(t *testing.T) {
	t.Parallel()

	doc := func(content string) []byte {
		return cyclePDF(
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << /ColorSpace << /CS0 [/ICCBased 5 0 R] >> >>"+
				" /Contents 4 0 R >>",
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
			"<< /N 1 /Length 4 >>\nstream\nicc.\nendstream",
		)
	}
	const icc = "q /CS0 cs 0.5 sc "

	for _, tc := range []struct {
		name  string
		data  []byte
		test  int // 6.2.4.3 test: 2 DeviceRGB, 4 DeviceGray
		fires bool
	}{
		{"a mask declaring /CS /RGB", doc(icc + "8 0 0 1 0 0 cm BI /W 8 /H 1 /IM true /CS /RGB /BPC 1 ID \x00 EI Q"), 2, false},
		{"a mask declaring /CS /G", doc(icc + "8 0 0 1 0 0 cm BI /W 8 /H 1 /IM true /CS /G /BPC 1 ID \x00 EI Q"), 4, false},
		{"an image declaring /CS /RGB", doc(icc + "BI /W 1 /H 1 /CS /RGB /BPC 8 ID \x00\x00\x00 EI Q"), 2, true},
		{"a mask declaring /CS /RGB under a gray fill", doc("q 0.5 g 8 0 0 1 0 0 cm BI /W 8 /H 1 /IM true /CS /RGB /BPC 1 ID \x00 EI Q"), 2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := firesRuleProfile(t, tc.data, models.PDFA2b, "6.2.4.3", tc.test); got != tc.fires {
				t.Errorf("6.2.4.3 t%d violation = %v, want %v", tc.test, got, tc.fires)
			}
		})
	}
}
