package html

import (
	"bytes"
	"context"
	"image/color"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typeset"
)

func TestDraw_TwoToneBorderShades(t *testing.T) {
	t.Parallel()
	// A 20px box of 10px ridge borders: the outer half of the top side
	// draws the light shade, the inner half the dark one. Measured on
	// Chromium 9.0.0's print; the table cases on the pinned Chromium's: a
	// table box paints the shades of its computed colour, a lone cell the
	// currentColor ones.
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
		{"a table box takes its colour's shades", "display:table;border:10px ridge", 168, 84},
		{"a lone cell keeps the currentColor shades", "display:table-cell;border:10px ridge", 0xee, 0x9a},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			o := Options{PageWidth: 20 * 0.75, PageHeight: 20 * 0.75}
			doc := conv(t, `<body style="margin:0"><div style="box-sizing:border-box;width:20px;height:20px;`+tt.style+`"></div>`, o)
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

func TestDraw_CellScrollerTakesItsMinHeight(t *testing.T) {
	t.Parallel()
	// A table cell 100px tall holds a scroll container of height 100%
	// around 200px of content: the cell stays 100px tall and the scroller
	// clips. A visible overflow grows the cell to 200px. Measured on the
	// pinned Chromium's print.
	tests := []struct {
		name     string
		overflow string
		want     bool
	}{
		{"scroll container", "auto", false},
		{"visible overflow", "visible", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			o := Options{PageWidth: 40 * 0.75, PageHeight: 300 * 0.75}
			doc := conv(t, `<body style="margin:0"><div style="display:table-cell;width:40px;height:100px;background:#00ff00"><div style="overflow:`+tt.overflow+`;height:100%"><div style="height:200px"></div></div></div><div style="height:20px;background:#0000ff"></div>`, o)
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
			c := color.NRGBAModel.Convert(img.At(20, 150)).(color.NRGBA)
			if grown := c.G > 200 && c.B < 50; grown != tt.want {
				t.Errorf("pixel 150px down is %v, want the cell grown %v", c, tt.want)
			}
		})
	}
}

func TestDraw_DashedBordersFollowBlink(t *testing.T) {
	t.Parallel()
	// CSS Backgrounds 3, 4.2 leaves the dash pattern to the user agent;
	// the HTML converter draws Blink's, as the pinned Chromium draws it: a
	// 2px dashed side 150px long starts with a 6px dash and a gap of 4 to
	// 5px, a 3px dotted one with a square end dot a pixel longer, a dashed collapsed cell
	// border runs its pattern from the table's outer corner.
	tests := []struct {
		name, src, want string
	}{
		{"box side", `<div style="box-sizing:border-box;width:150px;height:20px;border:2px dashed red"></div>`, "RRRRRR___"},
		{"square dots", `<div style="box-sizing:border-box;width:150px;height:20px;border:3px dotted red"></div>`, "RRRR___RRR___R"},
		{"collapsed cell", `<table style="border-collapse:collapse"><td style="width:146px;height:20px;padding:0;border:2px dashed red"></td></table>`, "RRRRRR___"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rows := drawPage(t, `<body style="margin:0">`+tt.src, 160, 30, Options{})
			if got := rows[1][:len(tt.want)]; got != tt.want {
				t.Errorf("top side %q, want %q", got, tt.want)
			}
		})
	}
}
