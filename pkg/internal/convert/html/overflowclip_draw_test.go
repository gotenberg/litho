package html

import (
	"bytes"
	"context"
	"fmt"
	"image/color"
	"strings"
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

func TestDraw_OverflowClipsOneAxis(t *testing.T) {
	t.Parallel()
	// CSS Overflow 3, 3.1 and 3.1.2: overflow-x: clip clips the sides at
	// the padding box, square under a border radius, and leaves the
	// bottom open; overflow-clip-margin applies to neither (Chromium
	// applies it only where both axes clip). A 20px box holds a 40px
	// square shifted 10px up and left.
	o := Options{PageWidth: 60 * 0.75, PageHeight: 60 * 0.75}
	doc := conv(t, `<!DOCTYPE html><body style="margin:0"><div style="margin:10px;width:20px;height:20px;border-radius:10px;overflow-x:clip;overflow-clip-margin:5px"><div style="position:relative;left:-10px;top:-10px;width:40px;height:40px;background:#00ff00"></div></div>`, o)
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
	for _, tt := range []struct {
		x, y  int
		green bool
	}{
		{11, 11, true},  // the padding box's corner, under the radius
		{20, 2, true},   // above the box: y is open
		{20, 37, true},  // below the box
		{8, 20, false},  // left of it: x is clipped, the margin ignored
		{32, 20, false}, // right of it
	} {
		c := color.NRGBAModel.Convert(img.At(tt.x, tt.y)).(color.NRGBA)
		if got := c.R < 0x40 && c.G > 0xc0; got != tt.green {
			t.Errorf("(%d, %d) = %v, want green %v", tt.x, tt.y, c, tt.green)
		}
	}
}

func TestPages_LinePushedPastAClipAddsNoPage(t *testing.T) {
	t.Parallel()
	// A scroll container as tall as the page, the shell of an application:
	// the line that straddles the page's end moves to the next page, below
	// the container's end, where its clip shows nothing of it (CSS Overflow
	// 3, 3.1), though the ink of its glyphs reaches back over the cut.
	var lines strings.Builder
	for i := range 40 {
		fmt.Fprintf(&lines, "<div>line %d</div>", i)
	}
	for _, h := range []string{"100vh", "265.9px", "266px"} {
		for _, ov := range []string{"auto", "hidden", "clip"} {
			src := `<!DOCTYPE html><style>@page { size: 400px 266px; margin: 0 } body { margin: 0; font: 20px/10px sans-serif }</style>` +
				`<div style="height: ` + h + `; overflow: ` + ov + `">` + lines.String() + `</div>`
			if doc := convertDoc(t, src, Options{}); len(doc.Pages) != 1 {
				t.Errorf("height %s, overflow %s: %d pages, want 1", h, ov, len(doc.Pages))
			}
		}
	}
}
