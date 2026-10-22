package pdf

import (
	"context"
	"os"
	"testing"
)

// TestLimits_JBIG2MaskBoundedByBytes pins that the image budget bounds a
// JBIG2 bitmap by the bytes it holds, one bit a pixel, rather than by its
// pixels. The mask is the 52x66 page of the JBIG2Decode example of ISO
// 32000-1, 7.4.7: 3432 pixels in 462 bytes. A budget of 2000 pixels scales
// the bitmap cap to 2000 bytes, and the pixels the codec may decode in total
// to 8000, so the mask, past the budget in pixels, is read reduced and its
// image drawn; a budget of 400 pixels scales the cap below the mask's bytes,
// so the mask cannot be read and its image is left out rather than painted
// unmasked.
func TestLimits_JBIG2MaskBoundedByBytes(t *testing.T) {
	t.Parallel()

	page, errP := os.ReadFile("../internal/filter/jbig2/testdata/iso32000_jbig2_page.jb2")
	globals, errG := os.ReadFile("../internal/filter/jbig2/testdata/iso32000_jbig2_globals.jb2")
	if errP != nil || errG != nil {
		t.Fatalf("reading the example: %v %v", errP, errG)
	}
	data := limitsPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 52 66] /Resources << /XObject << /Im0 4 0 R >> >> /Contents 7 0 R >>",
		streamObj("/Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8 /Mask 5 0 R", []byte{0}),
		streamObj("/Type /XObject /Subtype /Image /Width 52 /Height 66 /ImageMask true /BitsPerComponent 1"+
			" /Filter /JBIG2Decode /DecodeParms << /JBIG2Globals 6 0 R >>", page),
		streamObj("", globals),
		streamObj("", []byte("q 52 0 0 66 0 0 cm /Im0 Do Q")),
	)

	dark := func(maxImagePixels int64) int {
		t.Helper()
		r := openLimited(t, data, Limits{MaxImagePixels: maxImagePixels})
		defer r.Close()
		img, err := r.RenderPage(context.Background(), 1, WithRenderDPI(72))
		if err != nil {
			t.Fatalf("RenderPage under %d pixels: %v", maxImagePixels, err)
		}
		n := 0
		b := img.Bounds()
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				if r, _, _, _ := img.At(x, y).RGBA(); r>>8 < 128 {
					n++
				}
			}
		}
		return n
	}

	whole := dark(0)
	if whole == 0 || whole == 52*66 {
		t.Fatalf("default budget: %d dark pixels of %d, want the mask's shape", whole, 52*66)
	}
	if got := dark(2000); got == 0 || got == 52*66 {
		t.Errorf("budget of 2000 pixels: %d dark pixels of %d, want the mask read reduced (%d whole)", got, 52*66, whole)
	}
	if got := dark(400); got != 0 {
		t.Errorf("budget of 400 pixels: %d dark pixels, want the image left out", got)
	}
}
