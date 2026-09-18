package compliance

import (
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestColorScope_ShadingPatternMesh pins that the colour space of a mesh
// shading reached through a shading PATTERN counts.
//
// Per ISO 32000-1, 8.7.4.5, a mesh shading of type 4 to 7 is a stream, while a
// shading pattern (PatternType 2) is a dictionary holding it under /Shading.
// The pattern branch resolved that /Shading only as a dictionary, so a mesh
// behind a pattern was dropped and its /ColorSpace never counted: the same gap
// [TestColorScope_MeshShading] pins for the sh path. pdfjs
// coons-allflags-withfunction.pdf and tensor-allflags-withfunction.pdf each
// select a PatternType 2 whose /Shading is a type 6 or 7 stream in DeviceRGB on
// a page with no OutputIntent, and veraPDF reports both under §6.2.4.3 t2 while
// litho counted no device colour space for the page at all.
func TestColorScope_ShadingPatternMesh(t *testing.T) {
	t.Parallel()

	const (
		meshRGB = "<< /ShadingType 6 /ColorSpace /DeviceRGB /BitsPerCoordinate 8 /BitsPerComponent 8 " +
			"/BitsPerFlag 8 /Decode [0 1 0 1 0 1 0 1 0 1] /Length 3 >>\nstream\nabc\nendstream"
		axialRGB = "<< /ShadingType 2 /ColorSpace /DeviceRGB /Coords [0 0 1 0] >>"
		meshCal  = "<< /ShadingType 6 /ColorSpace [/CalRGB << /WhitePoint [0.95 1 1.09] >>] " +
			"/BitsPerCoordinate 8 /BitsPerComponent 8 /BitsPerFlag 8 " +
			"/Decode [0 1 0 1 0 1 0 1 0 1] /Length 3 >>\nstream\nabc\nendstream"
	)

	cases := []struct {
		name    string
		shading string
		want    bool
	}{
		{"a mesh shading behind a pattern is a stream and counts", meshRGB, true},
		{"an axial shading behind a pattern is a dictionary and still counts", axialRGB, true},
		{"a mesh shading behind a pattern in a device-independent space does not", meshCal, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data := colorScopePDF(
				"<< /Pattern << /P0 5 0 R >> >>",
				"/Pattern cs /P0 scn 0 0 10 10 re f",
				"<< /Type /Pattern /PatternType 2 /Shading 6 0 R >>",
				tc.shading,
			)
			if got := firesRuleProfile(t, data, models.PDFA2b, "6.2.4.3", 2); got != tc.want {
				t.Errorf("DeviceRGB violation = %v, want %v", got, tc.want)
			}
		})
	}
}
