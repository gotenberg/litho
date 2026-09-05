package image

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestResolveColorSpaceICCBasedComponents pins that an ICCBased space reports
// the device space its component count names.
//
// Per ISO 32000-1, 8.6.5.5, Table 66 the profile stream's /N shall be 1, 3 or 4,
// standing for DeviceGray, DeviceRGB and DeviceCMYK. Every ICCBased space
// answered DeviceRGB, which mislabelled the CMYK images in pdfjs
// issue11052.pdf. The answer drives [DecodeImage]'s pixel loop, so a
// four-component image was read three bytes to the pixel.
func TestResolveColorSpaceICCBasedComponents(t *testing.T) {
	t.Parallel()

	iccSpace := func(profile ast.PdfDict) ast.PdfDict {
		return ast.PdfDict{
			"ColorSpace": ast.PdfArray{
				ast.PdfName("ICCBased"),
				ast.NewStream(profile, nil),
			},
		}
	}

	for _, tc := range []struct {
		name string
		dict ast.PdfDict
		want string
	}{
		{"N 1 is grayscale", iccSpace(ast.PdfDict{"N": ast.PdfInt(1)}), "DeviceGray"},
		{"N 3 is RGB", iccSpace(ast.PdfDict{"N": ast.PdfInt(3)}), "DeviceRGB"},
		{"N 4 is CMYK", iccSpace(ast.PdfDict{"N": ast.PdfInt(4)}), "DeviceCMYK"},
		{
			"an out-of-range N falls back to /Alternate",
			iccSpace(ast.PdfDict{"N": ast.PdfInt(7), "Alternate": ast.PdfName("DeviceCMYK")}),
			"DeviceCMYK",
		},
		{
			"/Alternate is used when /N is absent",
			iccSpace(ast.PdfDict{"Alternate": ast.PdfName("DeviceGray")}),
			"DeviceGray",
		},
		{
			"/N wins over a contradicting /Alternate",
			iccSpace(ast.PdfDict{"N": ast.PdfInt(4), "Alternate": ast.PdfName("DeviceRGB")}),
			"DeviceCMYK",
		},
		{"a profile stating neither stays RGB", iccSpace(ast.PdfDict{}), "DeviceRGB"},
		{
			"a non-device /Alternate is ignored",
			iccSpace(ast.PdfDict{"Alternate": ast.PdfName("Pattern")}),
			"DeviceRGB",
		},
		{
			"an indexed image takes its base profile's components",
			ast.PdfDict{"ColorSpace": ast.PdfArray{
				ast.PdfName("Indexed"),
				ast.PdfArray{ast.PdfName("ICCBased"), ast.NewStream(ast.PdfDict{"N": ast.PdfInt(4)}, nil)},
				ast.PdfInt(255),
				ast.PdfString(""),
			}},
			"DeviceCMYK",
		},
	} {
		doc := &document.Document{Logger: document.NopLogger()}
		if got := resolveColorSpace(context.Background(), doc, tc.dict); got != tc.want {
			t.Errorf("%s: resolveColorSpace = %q, want %q", tc.name, got, tc.want)
		}
	}
}
