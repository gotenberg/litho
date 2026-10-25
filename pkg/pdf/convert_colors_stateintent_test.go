package pdf

import (
	"bytes"
	"context"
	"image"
	"testing"
)

// TestConvertColors_FollowsStateIntent converts the litho fixtures
// imageintent_2b, whose images convert under the ri, /RI or /Intent they are
// painted under, and cmykintent_2b, whose image and fill in a CMYK profile
// read lighter under the perceptual ri they follow (ISO 32000-1, 8.6.5.8).
// Under the default relative colorimetric option, every page converts to
// what its source draws: the content's intents override the option for
// what they govern.
func TestConvertColors_FollowsStateIntent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, tc := range []struct {
		file   string
		target ColorTarget
		pages  int
	}{
		{"imageintent_2b.pdf", RGBColorTarget(), 2},
		{"imageintent_2b.pdf", CMYKColorTarget(), 2},
		{"cmykintent_2b.pdf", RGBColorTarget(), 2},
	} {
		data := readLithoFixture(t, tc.file)
		src := openColorPDF(t, data)
		w := NewWriter(openColorPDF(t, data))
		report, err := w.ConvertColors(ctx, tc.target)
		if err != nil {
			t.Fatalf("%s: ConvertColors: %v", tc.file, err)
		}
		if report.IntentOperators+report.IntentInlineImages+report.IntentCopies == 0 || report.IntentUnhonoured != 0 {
			t.Errorf("%s: report %+v", tc.file, report)
		}
		var out bytes.Buffer
		if err := w.WriteTo(ctx, &out); err != nil {
			t.Fatalf("WriteTo: %v", err)
		}
		if err := w.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		converted := openColorPDF(t, out.Bytes())
		for page := 1; page <= tc.pages; page++ {
			before, err := src.RenderPage(ctx, page, WithRenderDPI(36))
			if err != nil {
				t.Fatalf("RenderPage: %v", err)
			}
			after, err := converted.RenderPage(ctx, page, WithRenderDPI(36))
			if err != nil {
				t.Fatalf("RenderPage: %v", err)
			}
			// The top rows hold cmykintent_2b's Multiply square, which
			// blends in the group's space before the conversion and in
			// the target's after it.
			crop := image.Rect(0, 5, before.Bounds().Dx(), before.Bounds().Dy())
			if d := worstChannel(subImage(before, crop), subImage(after, crop)); d > 3 {
				t.Errorf("%s to %v, page %d: the converted page draws up to %d levels from its source", tc.file, tc.target, page, d)
			}
		}
	}
}

// subImage returns the part r of img.
func subImage(img image.Image, r image.Rectangle) image.Image {
	return img.(interface {
		SubImage(image.Rectangle) image.Image
	}).SubImage(r.Add(img.Bounds().Min))
}
