package pdf

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// TestWithCanvasBackground_DropsEconomyBackgrounds checks that a
// conversion without the canvas background also drops the background of
// an element that asks for print-color-adjust: economy, and keeps a plain
// element's and the economy element's border (CSS Color Adjust 1, 3.1).
func TestWithCanvasBackground_DropsEconomyBackgrounds(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	src := `<style>body { margin: 0 } div { width: 100px; height: 50px; background: blue }</style>
	<div></div><div style="print-color-adjust: economy; border-left: 10px solid lime"></div>`
	var out bytes.Buffer
	c := FromHTML(strings.NewReader(src), WithConvertFontDirs(convertFontDir(t)),
		WithPaper(Paper{Width: 200, Height: 200}), WithMargins(0, 0, 0, 0), WithCanvasBackground(false))
	if _, err := c.WriteTo(ctx, &out); err != nil {
		t.Fatal(err)
	}
	r, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatal(err)
	}
	img, err := r.RenderPage(ctx, 1, WithRenderDPI(72))
	if err != nil {
		t.Fatal(err)
	}
	// At 72 dpi a CSS pixel is 0.75 device pixels: the plain div spans
	// rows 0 to 37, the economy div 37 to 75, its border columns 0 to 7.
	for _, tt := range []struct {
		name    string
		x, y    int
		r, g, b uint32
	}{
		{"plain background", 40, 18, 0, 0, 0xffff},
		{"economy background", 40, 56, 0xffff, 0xffff, 0xffff},
		{"economy border", 3, 56, 0, 0xffff, 0},
	} {
		r, g, b, _ := img.At(tt.x, tt.y).RGBA()
		if r != tt.r || g != tt.g || b != tt.b {
			t.Errorf("%s: colour %x %x %x, want %x %x %x", tt.name, r, g, b, tt.r, tt.g, tt.b)
		}
	}
}
