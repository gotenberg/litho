package pdf

import (
	"context"
	"image/color"
	"testing"
)

// TestRenderPage_RedactionReading draws a blanked image under a transfer
// function that paints black near white: a viewer's reading shows the blank
// pale, the redaction reading shows it black, as the pixel check reads it, and
// still shows a picture left unblanked lighter than black.
func TestRenderPage_RedactionReading(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	render := func(samples string, opts ...RenderOption) color.NRGBA {
		t.Helper()
		r := openPixelCheck(t, transferPDF(samples))
		img, err := r.RenderPage(ctx, 1, append([]RenderOption{
			WithRenderDPI(72), WithRenderContent(RenderImages), WithRenderBackground(color.Black),
		}, opts...)...)
		if err != nil {
			t.Fatal(err)
		}
		// The image's upper right sample, drawn over [200 500 300 600].
		return nrgbaAt(t, img, 250, 242)
	}
	const blank, picture = "\x00\x00\x00\x00", "\x00\xff\x80\x40"
	if c := render(blank); c.R < 200 {
		t.Errorf("viewer reading of the blank %v, want the transfer's near white", c)
	}
	if c := render(blank, WithRenderRedactionReading()); c.R > 8 || c.G > 8 || c.B > 8 {
		t.Errorf("redaction reading of the blank %v, want black", c)
	}
	if c := render(picture, WithRenderRedactionReading()); c.R < 200 {
		t.Errorf("redaction reading of a kept white sample %v, want white", c)
	}
}
