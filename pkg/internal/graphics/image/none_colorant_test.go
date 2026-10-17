package image

import (
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// noneTint is a tint transform that would paint a real Separation black at
// full tint, so a decode that ran it would show ink where None shows none.
const noneTint = "<< /FunctionType 2 /Domain [0 1] /Range [0 1] /C0 [1] /C1 [0] /N 1 >>"

// TestDecodeImage_NoneColorantPaintsNothing pins that an image in a space over
// the colorant None decodes fully transparent: ISO 32000-1, 8.6.6.4 says the
// colorant never produces visible output, and 8.6.6.5 extends that to a
// DeviceN space naming only None. Running the tint transform instead painted
// the image as though the colorant were a real ink.
func TestDecodeImage_NoneColorantPaintsNothing(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		cs      string
		samples []byte
	}{
		{"separation", "[/Separation /None /DeviceGray 5 0 R]", []byte{0, 85, 170, 255}},
		{"devicen", "[/DeviceN [/None /None] /DeviceGray 5 0 R]", []byte{0, 0, 85, 85, 170, 170, 255, 255}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, doc, page := imagePDF(t, append(tintedImagePDF(tc.cs, tc.samples), noneTint))

			img, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
			if err != nil {
				t.Fatalf("DecodeImage refused a None image: %v", err)
			}
			if got := img.Bounds(); got.Dx() != 4 || got.Dy() != 1 {
				t.Fatalf("bounds = %v, want 4x1", got)
			}
			for x := range 4 {
				if _, _, _, a := img.At(x, 0).RGBA(); a != 0 {
					t.Errorf("pixel %d has alpha %d, want 0: None paints nothing", x, a>>8)
				}
			}
		})
	}
}

// TestDecodeImage_PartialNoneDeviceNStillPaints pins that a DeviceN space
// naming None beside a real colorant is decoded through its tint transform:
// per ISO 32000-1, 8.6.6.5 only a space whose names are ALL None paints
// nothing.
func TestDecodeImage_PartialNoneDeviceNStillPaints(t *testing.T) {
	t.Parallel()

	const cs = "[/DeviceN [/Spot /None] /DeviceGray 5 0 R]"
	samples := []byte{0, 0, 255, 0, 0, 255, 255, 255}
	// Grey 255 at no Spot ink and 0 at full Spot ink, whatever None holds.
	table := []byte{255, 0, 255, 0}
	objs := append(tintedImagePDF(cs, samples),
		fmt.Sprintf("<< /FunctionType 0 /Domain [0 1 0 1] /Range [0 1]"+
			" /Size [2 2] /BitsPerSample 8 /Length %d >>\nstream\n%s\nendstream",
			len(table), table))
	ctx, doc, page := imagePDF(t, objs)

	img, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
	if err != nil {
		t.Fatalf("DecodeImage refused a partial-None DeviceN image: %v", err)
	}
	r, _, _, a := img.At(1, 0).RGBA()
	if a>>8 != 255 || r>>8 != 0 {
		t.Errorf("pixel 1 = grey %d alpha %d, want opaque 0 through the tint transform", r>>8, a>>8)
	}
}

// TestDecodeImageNative_NoneColorantKeepsItsTints pins that a native read of a
// None image returns its tints, /Decode applied, so a caller writing them back
// under the same /ColorSpace keeps the image painting nothing.
func TestDecodeImageNative_NoneColorantKeepsItsTints(t *testing.T) {
	t.Parallel()

	objs := tintedImagePDF("[/Separation /None /DeviceGray 5 0 R]", nil)
	objs[3] = "<< /Type /XObject /Subtype /Image /Width 4 /Height 1" +
		" /ColorSpace [/Separation /None /DeviceGray 5 0 R] /Decode [1 0]" +
		" /BitsPerComponent 8 /Length 4 >>\nstream\n\x00\x55\xaa\xff\nendstream"
	ctx, doc, page := imagePDF(t, append(objs, noneTint))

	native, ok, err := DecodeImageNative(ctx, doc, page, ast.PdfName("Im0"))
	if err != nil || !ok {
		t.Fatalf("DecodeImageNative = ok %v, err %v; want the tints", ok, err)
	}
	if native.Components != 1 || native.Width != 4 || native.Height != 1 {
		t.Fatalf("native shape = %d comps %dx%d, want 1 comp 4x1", native.Components, native.Width, native.Height)
	}
	for i, want := range []byte{255, 170, 85, 0} {
		if native.Data[i] != want {
			t.Errorf("sample %d = %d, want %d under /Decode [1 0]", i, native.Data[i], want)
		}
	}
}
