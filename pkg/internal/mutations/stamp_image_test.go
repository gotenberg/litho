package mutations

import (
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// jpegHeader returns the segments of a JPEG up to its frame header: an Adobe
// APP14 segment when adobe is set, then a SOF0 of the given component count.
func jpegHeader(components int, adobe bool) []byte {
	b := []byte{0xFF, 0xD8}
	if adobe {
		b = append(b, 0xFF, 0xEE, 0x00, 0x0E, 'A', 'd', 'o', 'b', 'e', 0x00, 0x64, 0x00, 0x00, 0x00, 0x00, 0x00)
	}
	sof := []byte{0x08, 0x00, 0x01, 0x00, 0x01, byte(components)}
	for c := range components {
		sof = append(sof, byte(c+1), 0x11, 0x00)
	}
	b = append(b, 0xFF, 0xC0, 0x00, byte(2+len(sof)))
	return append(append(b, sof...), 0xFF, 0xD9)
}

// TestBuildJPEGXObject_AdobeCMYK inverts an Adobe-marked CMYK JPEG through
// /Decode, since its samples are stored inverted and the DCTDecode filter
// hands them on as stored, and leaves every other JPEG without one.
func TestBuildJPEGXObject_AdobeCMYK(t *testing.T) {
	t.Parallel()

	inverted := ast.PdfArray{
		ast.PdfInt(1), ast.PdfInt(0), ast.PdfInt(1), ast.PdfInt(0),
		ast.PdfInt(1), ast.PdfInt(0), ast.PdfInt(1), ast.PdfInt(0),
	}
	for _, tc := range []struct {
		name       string
		components int
		adobe      bool
		want       ast.PdfArray
	}{
		{"Adobe CMYK", 4, true, inverted},
		{"unmarked CMYK", 4, false, nil},
		{"Adobe RGB", 3, true, nil},
	} {
		stream, _, _, err := buildJPEGXObject(jpegHeader(tc.components, tc.adobe))
		if err != nil {
			t.Fatalf("%s: buildJPEGXObject: %v", tc.name, err)
		}
		got, _ := stream.Dict["Decode"].(ast.PdfArray)
		if !slices.Equal(got, tc.want) {
			t.Errorf("%s: /Decode = %v, want %v", tc.name, got, tc.want)
		}
	}
}
