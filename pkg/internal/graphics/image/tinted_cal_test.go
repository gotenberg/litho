package image

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestDecodeImage_TintAlternateIsCIEBased reads the tint transform's output in
// a CalGray alternate of gamma 1, where a luminance of 0.498 is sRGB 187.
// ISO 32000-1, 8.6.6.4 lets the alternate be CIE-based, and reading it as the
// DeviceGray its component count suggests left the level at 127.
func TestDecodeImage_TintAlternateIsCIEBased(t *testing.T) {
	t.Parallel()

	const cs = "[/Separation /Spot [/CalGray << /WhitePoint [0.9505 1 1.089] /Gamma 1 >>] 5 0 R]"
	objs := append(tintedImagePDF(cs, []byte{128, 128, 128, 128}),
		"<< /FunctionType 2 /Domain [0 1] /Range [0 1] /C0 [1] /C1 [0] /N 1 >>")
	ctx, doc, page := imagePDF(t, objs)

	img, err := DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
	if err != nil {
		t.Fatalf("DecodeImage: %v", err)
	}
	if got := greyAt(img, 0, 0); got < 186 || got > 188 {
		t.Errorf("grey %d, want 187: the CalGray alternate's gamma applied", got)
	}
}
