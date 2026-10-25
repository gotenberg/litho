package image

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// fuzzICCImageBytes bounds the fuzzed profile and samples so one input stays
// a small document.
const fuzzICCImageBytes = 1 << 16

// FuzzICCImage decodes a one-row image whose ICCBased space carries an
// arbitrary profile of one, three or four components under each rendering
// intent its /Intent names: into sRGB ([DecodeImage], whose grey path reads a
// profile that is not neutral through [tintedGrey]), into its own samples
// ([DecodeImageNative]), and the space's black ([NativeBlack], which searches
// the samples when the conventional black is not dark). It asserts the
// decoders never panic, and the shapes the redaction's blanking indexes by:
// native samples hold width x height x components bytes, and the black one
// byte per component.
func FuzzICCImage(f *testing.F) {
	f.Add(tintedGreyProfile(), []byte{0, 128, 192, 255}, uint8(0), uint8(1))
	f.Add(tintedGreyProfile()[:80], []byte{0, 128}, uint8(0), uint8(0))
	f.Add(flatProfile("RGB ", 25), []byte{0, 0, 0, 255, 255, 255}, uint8(1), uint8(2))
	f.Add(flatProfile("CMYK", 25)[:200], []byte{0, 0, 0, 255}, uint8(2), uint8(3))
	f.Fuzz(func(t *testing.T, profile, samples []byte, nSel, intentSel uint8) {
		if len(profile) > fuzzICCImageBytes || len(samples) > fuzzICCImageBytes {
			t.Skip()
		}
		n := []int{1, 3, 4}[int(nSel)%3]
		width := len(samples) / n
		if width == 0 {
			return
		}
		intent := []string{"Perceptual", "RelativeColorimetric", "Saturation", "AbsoluteColorimetric"}[int(intentSel)%4]
		ctx, doc, page := managedPDF(t, "", "[/ICCBased 5 0 R]", "/Intent /"+intent, width,
			samples[:width*n], profileObj(n, profile))
		// An error is an outcome the decoders may report on such a profile:
		// the assertion is that they return.
		_, _ = DecodeImage(ctx, doc, page, ast.PdfName("Im0"))
		native, ok, err := DecodeImageNative(ctx, doc, page, ast.PdfName("Im0"))
		if err != nil || !ok {
			return
		}
		if len(native.Data) < native.Width*native.Height*native.Components {
			t.Fatalf("native samples hold %d bytes for %d x %d x %d", len(native.Data), native.Width, native.Height, native.Components)
		}
		stream, _, err := lookupImage(ctx, doc, page, ast.PdfName("Im0"))
		if err != nil {
			t.Fatalf("lookupImage: %v", err)
		}
		black, err := NativeBlack(ctx, doc, page, stream.Dict, native.Components)
		if err == nil && len(black) != native.Components {
			t.Fatalf("NativeBlack returned %d bytes for %d components", len(black), native.Components)
		}
	})
}
