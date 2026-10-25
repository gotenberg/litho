package pdf

import (
	"context"
	"fmt"
	"math"
	"testing"
)

// contrastInkPDF is a 400-point square page whose content is drawn as
// prefix, then content, with an /Inv graphics state inverting every channel
// through its transfer function and a /Diff one painting under the
// Difference blend mode.
func contrastInkPDF(content string) []byte {
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 400 400] /Resources << /Font << /F1 5 0 R >> /ExtGState << /Inv << /TR << /FunctionType 2 /Domain [0 1] /C0 [1] /C1 [0] /N 1 >> >> /Diff << /BM /Difference >> >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	})
}

// TestCheckTextContrastTransfer judges text under a transfer function in the
// colour the drawing shows it in: under an inverting transfer a white page
// shows black, black text white and 0.6 grey text 0.4 grey, so the black text
// passes and the grey falls short of 4.5:1. Read from the content stream
// alone, black on what the drawing shows black fails at 1:1.
func TestCheckTextContrastTransfer(t *testing.T) {
	t.Parallel()
	r := openLimited(t, contrastInkPDF("q /Inv gs 1 g 0 0 400 400 re f\n"+
		"BT 0 g /F1 12 Tf 20 360 Td (SHOWNWHITE) Tj ET\n"+
		"BT 0.6 g /F1 12 Tf 20 320 Td (SHOWNGREY) Tj ET Q\n"), Limits{})
	rep, err := r.CheckTextContrast(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Judged != 2 || rep.Undetermined != 0 {
		t.Errorf("judged %d, undetermined %d; want 2 and 0", rep.Judged, rep.Undetermined)
	}
	got := failuresByText(rep)
	if _, ok := got["SHOWNWHITE"]; ok {
		t.Errorf("SHOWNWHITE reported, white on black: %+v", got["SHOWNWHITE"])
	}
	grey, ok := got["SHOWNGREY"]
	if !ok {
		t.Fatalf("SHOWNGREY not reported: %v", got)
	}
	if grey.BackgroundColor != [3]uint8{0, 0, 0} || math.Abs(float64(grey.TextColor[0])-102) > 1 {
		t.Errorf("SHOWNGREY colours %v on %v, want 0.4 grey on black", grey.TextColor, grey.BackgroundColor)
	}
}

// TestCheckTextContrastLaterBlend leaves undetermined text a later painting
// recolours: a white rectangle painted under the Difference blend mode over
// black text on white inverts both, so the page shows white text on black and
// the content stream's black is not the colour shown (ISO 32000-1, 11.3.5).
// Text the rectangle leaves alone stays judged.
func TestCheckTextContrastLaterBlend(t *testing.T) {
	t.Parallel()
	r := openLimited(t, contrastInkPDF("1 g 0 0 400 400 re f\nBT 0 g /F1 12 Tf 20 360 Td (UNDERBLEND) Tj ET\n"+
		"BT 0.6 g /F1 12 Tf 20 100 Td (BESIDE) Tj ET\n"+
		"q /Diff gs 1 g 0 300 400 100 re f Q\n"), Limits{})
	rep, err := r.CheckTextContrast(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Judged != 1 || rep.Undetermined != 1 {
		t.Errorf("judged %d, undetermined %d; want 1 and 1", rep.Judged, rep.Undetermined)
	}
	got := failuresByText(rep)
	if _, ok := got["UNDERBLEND"]; ok {
		t.Errorf("UNDERBLEND judged by the content stream's colour: %+v", got["UNDERBLEND"])
	}
	if _, ok := got["BESIDE"]; !ok {
		t.Errorf("BESIDE, 0.6 grey on white, not reported: %v", got)
	}
}

func TestHullDistance(t *testing.T) {
	t.Parallel()
	white, black, red := [3]float64{1, 1, 1}, [3]float64{0, 0, 0}, [3]float64{1, 0, 0}
	for _, tc := range []struct {
		name string
		p    [3]float64
		pts  [][3]float64
		want float64
	}{
		{"on the segment", [3]float64{0.5, 0.5, 0.5}, [][3]float64{white, black}, 0},
		{"off the segment", [3]float64{1, 0, 0}, [][3]float64{white, black}, math.Sqrt(2.0 / 3)},
		{"inside the triangle", [3]float64{0.6, 0.3, 0.3}, [][3]float64{white, black, red}, 0},
		{"a point", [3]float64{0, 0, 1}, [][3]float64{black}, 1},
	} {
		if got := hullDistance(tc.p, tc.pts); math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}
