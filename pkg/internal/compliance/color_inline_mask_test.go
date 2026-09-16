package compliance

import (
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestColorScope_InlineImageMask pins ISO 19005-2, 6.2.4.3 t4 on an inline
// image mask. ISO 32000-1, 8.9.7 (Table 93 abbreviates ImageMask as IM) paints
// it with the current fill colour, so with no fill space selected it paints the
// initial DeviceGray, which only the root content stream's DefaultGray covers,
// as for [TestColorScope_InitialGrayBelongsToTheRoot]. Every case was measured
// against veraPDF.
func TestColorScope_InlineImageMask(t *testing.T) {
	t.Parallel()

	st := func(dict, data string) string {
		return fmt.Sprintf("<< %s /Length %d >>\nstream\n%s\nendstream", dict, len(data), data)
	}
	const (
		icc  = "<< /N 1 /Length 4 >>\nstream\nicc.\nendstream"
		dg   = " /ColorSpace << /DefaultGray [/ICCBased 5 0 R] >>"
		mask = "q 8 0 0 1 0 0 cm BI /W 8 /H 1 /IM true /BPC 1 ID \x00 EI Q"
	)
	doc := func(pageResources, content string, objs ...string) []byte {
		return cyclePDF(append([]string{
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources <<" + pageResources + " >> /Contents 4 0 R >>",
			st("", content),
			icc,
		}, objs...)...)
	}

	for _, tc := range []struct {
		name  string
		data  []byte
		fires bool
	}{
		{"a mask with no fill space selected", doc("", mask), true},
		{"a mask under the page's DefaultGray", doc(dg, mask), false},
		{"a mask in a form with its own DefaultGray", doc(" /XObject << /Fm0 6 0 R >>", "q /Fm0 Do Q",
			st("/Type /XObject /Subtype /Form /BBox [0 0 10 10] /Resources <<"+dg+" >>", mask)), true},
		{"a mask after an ICCBased fill space", doc(" /ColorSpace << /CS0 [/ICCBased 5 0 R] >>", "/CS0 cs 0.5 sc "+mask), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := firesRuleProfile(t, tc.data, models.PDFA2b, "6.2.4.3", 4); got != tc.fires {
				t.Errorf("6.2.4.3 t4 violation = %v, want %v", got, tc.fires)
			}
		})
	}
}
