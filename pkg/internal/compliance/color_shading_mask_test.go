package compliance

import (
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// colorScopePDF builds a one-page PDF with no OutputIntent, whose page carries
// the given resources and content. Without an OutputIntent any device colour
// space the page uses violates ISO 19005-2 §6.2.4.3.
func colorScopePDF(resources, content string, extraObjs ...string) []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources " + resources + " /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
	}
	objs = append(objs, extraObjs...)
	return cyclePDF(objs...)
}

// TestColorScope_MeshShading pins that a mesh shading's colour space counts.
//
// Per ISO 32000-1, 8.7.4.5, a shading of type 1 to 3 is a dictionary while a
// mesh shading of type 4 to 7 is a stream carrying the same entries in its
// stream dictionary. litho resolved only the dictionary form, so every mesh
// shading was skipped and its /ColorSpace never counted.
//
// # Corpus evidence
//
//   - pdfjs issue17848.pdf: paints a type 4 shading in DeviceRGB on a page with
//     no OutputIntent, which veraPDF reports under §6.2.4.3 t2.
func TestColorScope_MeshShading(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		shading string
		want    bool
	}{
		{
			"a type 4 mesh shading is a stream and counts",
			"<< /ShadingType 4 /ColorSpace /DeviceRGB /BitsPerCoordinate 8 /BitsPerComponent 8 " +
				"/BitsPerFlag 8 /Decode [0 1 0 1 0 1 0 1 0 1] /Length 3 >>\nstream\nabc\nendstream",
			true,
		},
		{
			"a type 2 axial shading is a dictionary and still counts",
			"<< /ShadingType 2 /ColorSpace /DeviceRGB /Coords [0 0 1 0] >>",
			true,
		},
		{
			"a mesh shading in a device-independent space does not",
			"<< /ShadingType 4 /ColorSpace [/CalRGB << /WhitePoint [0.95 1 1.09] >>] /BitsPerCoordinate 8 " +
				"/BitsPerComponent 8 /BitsPerFlag 8 /Decode [0 1 0 1 0 1 0 1 0 1] /Length 3 >>\nstream\nabc\nendstream",
			false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data := colorScopePDF(
				"<< /Shading << /Sh0 5 0 R >> >>",
				"/Sh0 sh",
				tc.shading,
			)
			if got := firesRuleProfile(t, data, models.PDFA2b, "6.2.4.3", 2); got != tc.want {
				t.Errorf("DeviceRGB violation = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestColorScope_ImageMaskUsesFillColour pins that painting an image mask
// selects a colour space.
//
// An image mask carries no colour space of its own: ISO 32000-1, 8.9.6.2 paints
// its unmasked areas with the current fill colour, so drawing one selects a
// space exactly as "f" does, and with no cs/rg/g/k before the Do that colour is
// the initial DeviceGray of 8.6.8.
//
// # Corpus evidence
//
//   - pdfjs issue7308.pdf: draws a JBIG2 image mask on a page that sets no
//     colour at all, which veraPDF reports under §6.2.4.3 t4.
func TestColorScope_ImageMaskUsesFillColour(t *testing.T) {
	t.Parallel()

	const mask = "<< /Type /XObject /Subtype /Image /Width 8 /Height 8 /ImageMask true " +
		"/BitsPerComponent 1 /Length 8 >>\nstream\n\x00\x00\x00\x00\x00\x00\x00\x00\nendstream"

	cases := []struct {
		name    string
		content string
		want    bool
	}{
		{"a mask drawn with the initial fill colour counts", "q /Im0 Do Q", true},
		// An explicit calibrated fill space before the Do supplies the colour
		// instead, so the initial DeviceGray never applies.
		{"a mask drawn under a selected space does not", "q /CS0 cs 0 0 0 sc /Im0 Do Q", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data := colorScopePDF(
				"<< /XObject << /Im0 5 0 R >> /ColorSpace << /CS0 [/CalRGB << /WhitePoint [0.95 1 1.09] >>] >> >>",
				tc.content,
				mask,
			)
			if got := firesRuleProfile(t, data, models.PDFA2b, "6.2.4.3", 4); got != tc.want {
				t.Errorf("DeviceGray violation = %v, want %v", got, tc.want)
			}
		})
	}
}
