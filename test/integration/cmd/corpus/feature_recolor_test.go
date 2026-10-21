package main

import (
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
	"github.com/gotenberg/litho/pkg/pdf"
)

// recolorSquare returns a 40 by 40 white image with a 10 by 10 square of c at
// (x, y).
func recolorSquare(x, y int, c color.Color) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, 40, 40))
	for py := range 40 {
		for px := range 40 {
			img.Set(px, py, color.White)
			if px >= x && px < x+10 && py >= y && py < y+10 {
				img.Set(px, py, c)
			}
		}
	}
	return img
}

// TestCompareRecolored_Discriminates pins the two rendering checks against
// the changes they exist to catch, with the unchanged image as control.
func TestCompareRecolored_Discriminates(t *testing.T) {
	t.Parallel()
	red := color.NRGBA{R: 230, G: 30, B: 40, A: 255}
	src := recolorSquare(10, 10, red)

	pixels, moved, drift, _, size := compareRecolored(src, recolorSquare(10, 10, red))
	if pixels != 1600 || moved != 0 || drift != 0 || size {
		t.Errorf("unchanged: pixels %d moved %d drift %d size %v, want 1600 0 0 false", pixels, moved, drift, size)
	}
	// A red a press prints a little duller passes the colour check.
	if _, _, drift, _, _ := compareRecolored(src, recolorSquare(10, 10, color.NRGBA{R: 205, G: 50, B: 55, A: 255})); drift != 0 {
		t.Errorf("a gamut-mapped red drifts on %d pixels", drift)
	}
	// Cyan, the red's inverse, is a channel read backwards.
	if _, moved, drift, maxDE, _ := compareRecolored(src, recolorSquare(10, 10, color.NRGBA{R: 25, G: 225, B: 215, A: 255})); moved != 0 || drift != 100 || maxDE <= recolorDeltaE {
		t.Errorf("inverted: moved %d drift %d max %.1f, want 0, 100 and above %d", moved, drift, maxDE, recolorDeltaE)
	}
	if _, moved, _, _, _ := compareRecolored(src, recolorSquare(25, 25, red)); moved == 0 {
		t.Error("a square that moved is not reported")
	}
	// A pale square a conversion moves a few levels across the ink level
	// stays where it was.
	pale := recolorSquare(10, 10, color.NRGBA{R: 188, G: 210, B: 240, A: 255})
	if _, moved, drift, _, _ := compareRecolored(pale, recolorSquare(10, 10, color.NRGBA{R: 195, G: 212, B: 236, A: 255})); moved != 0 || drift != 0 {
		t.Errorf("a pale square across the ink level: moved %d drift %d, want 0 and 0", moved, drift)
	}
	// A pale square that vanishes is reported.
	if _, moved, _, _, _ := compareRecolored(recolorSquare(10, 10, color.NRGBA{R: 150, G: 150, B: 150, A: 255}), recolorSquare(10, 10, color.White)); moved != 100 {
		t.Errorf("a grey square that vanished: moved %d, want 100", moved)
	}
	if _, _, _, _, size := compareRecolored(src, image.NewNRGBA(image.Rect(0, 0, 41, 40))); !size {
		t.Error("a size change is not reported")
	}
}

// TestInRecolorTarget pins which spaces count as the CMYK target.
func TestInRecolorTarget(t *testing.T) {
	t.Parallel()
	cmyk := &models.ColorSpaceUse{Family: "DeviceCMYK"}
	rgb := &models.ColorSpaceUse{Family: "DeviceRGB"}
	for _, tc := range []struct {
		use  models.ColorSpaceUse
		want bool
	}{
		{models.ColorSpaceUse{Family: "DeviceCMYK"}, true},
		{models.ColorSpaceUse{Family: "Separation", Alternate: cmyk}, true},
		{models.ColorSpaceUse{Family: "DeviceN", Alternate: rgb}, false},
		{models.ColorSpaceUse{Family: "Separation", Alternate: rgb, Colorants: []string{"None"}}, true},
		{models.ColorSpaceUse{Family: "DeviceN", Alternate: rgb, Colorants: []string{"None", "All"}}, true},
		{models.ColorSpaceUse{Family: "DeviceN", Alternate: rgb, Colorants: []string{"None", "Gold"}}, false},
		{models.ColorSpaceUse{Family: "Indexed", Alternate: cmyk}, true},
		{models.ColorSpaceUse{Family: "Pattern"}, true},
		{models.ColorSpaceUse{Family: "Pattern", Alternate: rgb}, false},
		{models.ColorSpaceUse{Family: "DeviceGray"}, false},
		{models.ColorSpaceUse{Family: "ICCBased", Components: 4, Alternate: cmyk}, false},
		{models.ColorSpaceUse{}, false},
	} {
		if got := inRecolorTarget(tc.use); got != tc.want {
			t.Errorf("%s: %v, want %v", spaceString(tc.use), got, tc.want)
		}
	}
}

// runRecolor runs the feature on body with convert planted, and scores it.
func runRecolor(t *testing.T, body string, convert func(context.Context, *pdf.Writer, ...pdf.ColorOption) (*pdf.ColorReport, error)) Verdict {
	t.Helper()
	path := filepath.Join(t.TempDir(), "in.pdf")
	if err := os.WriteFile(path, redactKindPDF(body), 0o600); err != nil {
		t.Fatal(err)
	}
	f := newRecolorFeature()
	f.convert = convert
	out, err := f.Run(context.Background(), path, "in")
	if err != nil {
		t.Fatal(err)
	}
	return f.Compare("in", out)
}

// keepColours plants a conversion that converts nothing and reports success.
func keepColours(context.Context, *pdf.Writer, ...pdf.ColorOption) (*pdf.ColorReport, error) {
	return &pdf.ColorReport{Target: "DeviceCMYK"}, nil
}

// TestRecolorFeature_Plants pins the cell end to end: a source already in
// CMYK passes through a conversion that keeps it, an RGB source the same
// conversion leaves in RGB fails out_of_target, a stamp the conversion adds
// fails ink_moved, and a refusal skips.
func TestRecolorFeature_Plants(t *testing.T) {
	t.Parallel()
	const cmykBody = "0 1 1 0 k 50 50 100 100 re f"

	if v := runRecolor(t, cmykBody, keepColours); !v.Match || v.Metrics[metricRecolorChecked] != 1 {
		t.Errorf("CMYK control: %+v", v)
	}
	// A name the source's resources leave undefined selects no space, in
	// the output as in the source.
	if v := runRecolor(t, "/Undefined cs "+cmykBody, keepColours); !v.Match || v.Metrics[metricOutOfTarget] != 0 {
		t.Errorf("undefined name: %+v", v)
	}
	v := runRecolor(t, "1 0 0 rg 50 50 100 100 re f", keepColours)
	if v.Match || v.Metrics[metricOutOfTarget] != 1 {
		t.Errorf("RGB left in RGB: match %v, out_of_target %d", v.Match, v.Metrics[metricOutOfTarget])
	}
	v = runRecolor(t, cmykBody, func(ctx context.Context, w *pdf.Writer, _ ...pdf.ColorOption) (*pdf.ColorReport, error) {
		if err := w.StampText(pdf.TextStamp{Text: "PLANTED PLANTED", FontSize: 40, Opacity: 1}); err != nil {
			return nil, err
		}
		return keepColours(ctx, w)
	})
	if v.Match || v.Metrics[metricInkMoved] != 1 {
		t.Errorf("stamp: match %v, ink_moved %d; %s", v.Match, v.Metrics[metricInkMoved], mismatchText(v))
	}
	v = runRecolor(t, cmykBody, func(context.Context, *pdf.Writer, ...pdf.ColorOption) (*pdf.ColorReport, error) {
		return nil, errors.Join(pdf.ErrColorConversionRefused, errors.New("planted"))
	})
	if !v.Skipped || v.Metrics[metricRecolorRefused] != 1 {
		t.Errorf("refusal: %+v", v)
	}
	v = runRecolor(t, cmykBody, func(context.Context, *pdf.Writer, ...pdf.ColorOption) (*pdf.ColorReport, error) {
		return nil, errors.New("planted failure")
	})
	if v.Match || v.Metrics[metricRecolorFailed] != 1 {
		t.Errorf("failure: %+v", v)
	}
}

// mismatchText returns v's first mismatch record, for a failure message.
func mismatchText(v Verdict) string {
	if len(v.Mismatches) == 0 {
		return v.Reason
	}
	var out recolorOutput
	if json.Unmarshal(v.Mismatches[0], &out) != nil {
		return string(v.Mismatches[0])
	}
	data, _ := json.Marshal(out.Pages)
	return string(data)
}
