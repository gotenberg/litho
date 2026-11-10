package html

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
)

// RenderPDF rasterises page n (1-based) of a PDF at 96 dpi. The external
// test package installs it: pkg/pdf imports this package, so the internal
// tests cannot import pkg/pdf themselves.
var RenderPDF func(data []byte, n int) (image.Image, error)

// drawPage converts src on a page of w by h CSS pixels with o and
// rasterises its first page at one CSS pixel a pixel, as rows of letters:
// R red, _ white, ? anything else.
func drawPage(t *testing.T, src string, w, h float64, o Options) []string {
	t.Helper()
	o.PageWidth, o.PageHeight = w*0.75, h*0.75
	doc := conv(t, src, o)
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
	var rows []string
	bd := img.Bounds()
	for y := bd.Min.Y; y < bd.Max.Y; y++ {
		var row strings.Builder
		for x := bd.Min.X; x < bd.Max.X; x++ {
			c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			switch {
			case c.R > 200 && c.G < 60 && c.B < 60:
				row.WriteByte('R')
			case c.R > 200 && c.G > 200 && c.B > 200:
				row.WriteByte('_')
			default:
				row.WriteByte('?')
			}
		}
		rows = append(rows, row.String())
	}
	return rows
}

func TestDraw_ScaleAndShrinkToFit(t *testing.T) {
	t.Parallel()
	const body = `<body style="margin:0">`
	tests := []struct {
		name string
		src  string
		o    Options
		want string
	}{
		{
			// A 12px block drawn at half its size, the 2px margins kept.
			"scale", body + `<div style="width:12px;height:4px;background:red"></div>`,
			Options{Scale: 0.5, MarginLeft: 1.5, MarginTop: 1.5},
			`________
			 ________
			 __RRRRRR
			 __RRRRRR
			 ________`,
		},
		{
			// A block 1.5 times the page wide is drawn at the page's width.
			"shrink to fit", body + `<div style="width:12px;height:6px;background:red"></div>`,
			Options{},
			`RRRRRRRR
			 RRRRRRRR
			 RRRRRRRR
			 RRRRRRRR
			 ________`,
		},
		{
			// Twice as wide: shrunk 1.5 times, the rest clipped.
			"past the maximum shrink", body + `<div style="width:16px;height:3px;background:red"></div>` +
				`<div style="width:8px;height:3px;margin-left:12px;background:red"></div>`,
			Options{},
			`RRRRRRRR
			 RRRRRRRR
			 ________
			 ________
			 ________`,
		},
		{
			"no shrink", body + `<div style="width:12px;height:6px;background:red"></div>`,
			Options{NoShrinkToFit: true},
			`RRRRRRRR
			 RRRRRRRR
			 RRRRRRRR
			 RRRRRRRR
			 RRRRRRRR`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := drawPage(t, tt.src, 8, 8, tt.o)
			var want []string
			for l := range strings.SplitSeq(tt.want, "\n") {
				want = append(want, strings.TrimSpace(l))
			}
			if g := strings.Join(got[:len(want)], "\n"); g != strings.Join(want, "\n") {
				t.Errorf("pixels:\n%s\nwant:\n%s", g, strings.Join(want, "\n"))
			}
		})
	}
}
