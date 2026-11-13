package html

import (
	"bytes"
	"context"
	"image/color"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typeset"
)

func TestDraw_OverflowClipMakesNoFormattingContext(t *testing.T) {
	t.Parallel()
	// CSS Overflow 3, 3.1: overflow: clip clips as hidden does but makes
	// no formatting context, so a child's top margin collapses through
	// the clipping box, which then starts 10px down; hidden keeps the
	// margin inside and paints its background from the top.
	tests := []struct {
		overflow string
		top      uint8
	}{
		{"clip", 0xff},
		{"hidden", 0},
	}
	for _, tt := range tests {
		t.Run(tt.overflow, func(t *testing.T) {
			t.Parallel()
			o := Options{PageWidth: 20 * 0.75, PageHeight: 30 * 0.75}
			doc := conv(t, `<body style="margin:0"><div style="background:#000;overflow:`+tt.overflow+`"><div style="margin-top:10px;height:10px;background:#00ff00"></div></div>`, o)
			var b bytes.Buffer
			if err := typeset.Write(context.Background(), doc, &b, typeset.Options{}); err != nil {
				t.Fatal(err)
			}
			if RenderPDF == nil {
				t.Fatal("no PDF renderer installed")
			}
			img, err := RenderPDF(b.Bytes(), 1)
			if err != nil {
				t.Fatal(err)
			}
			if got := color.NRGBAModel.Convert(img.At(10, 5)).(color.NRGBA).R; absDiff(got, tt.top) > 2 {
				t.Errorf("red at (10, 5) %d, want %d", got, tt.top)
			}
		})
	}
}
