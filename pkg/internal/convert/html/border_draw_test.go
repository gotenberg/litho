package html

import (
	"bytes"
	"context"
	"image/color"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
)

func TestDraw_TwoToneBorderShades(t *testing.T) {
	t.Parallel()
	// A 20px box of 10px ridge borders: the outer half of the top side
	// draws the light shade, the inner half the dark one. Measured on
	// Chromium 9.0.0's print.
	tests := []struct {
		name        string
		style       string
		light, dark uint8
	}{
		{"currentColor is #eeeeee", "border:10px ridge", 0xee, 0x9a},
		{"currentColor whatever color", "border:10px ridge;color:#800000", 0xee, 0x9a},
		{"black is too dark to darken", "border:10px ridge black", 168, 84},
		{"#202020 is too dark to darken", "border:10px ridge #202020", 200, 116},
		{"#404040 darkens", "border:10px ridge #404040", 148, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			o := Options{PageWidth: 20 * 0.75, PageHeight: 20 * 0.75}
			doc := conv(t, `<body style="margin:0"><div style="box-sizing:border-box;width:20px;height:20px;`+tt.style+`"></div>`, o)
			var b bytes.Buffer
			if err := compose.Write(context.Background(), doc, &b, compose.Options{}); err != nil {
				t.Fatal(err)
			}
			if RenderPDF == nil {
				t.Fatal("no PDF renderer installed")
			}
			img, err := RenderPDF(b.Bytes(), 1)
			if err != nil {
				t.Fatal(err)
			}
			grey := func(x, y int) uint8 { return color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA).G }
			if got := grey(10, 2); absDiff(got, tt.light) > 2 {
				t.Errorf("outer half %d, want %d", got, tt.light)
			}
			if got := grey(10, 7); absDiff(got, tt.dark) > 2 {
				t.Errorf("inner half %d, want %d", got, tt.dark)
			}
		})
	}
}

func absDiff(a, b uint8) uint8 {
	if a > b {
		return a - b
	}
	return b - a
}
