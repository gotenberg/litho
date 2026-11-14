package ods

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// TestRead_ShapeTextAndTextBoxes locks that the text of a drawing shape
// and of a text box of the drawing layer is drawn in the shape's box: the
// shape's text at the bottom of a rectangle whose style says so, the text
// box's at its top left, in Liberation Serif at 12 points where no style
// sets a font, and that a text box whose style fills nothing paints no
// fill.
func TestRead_ShapeTextAndTextBoxes(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "..", "test", "integration", "testdata", "fonts", "liberation-2.1.5")
	if _, err := os.Stat(dir); err != nil {
		t.Skip("fixture fonts not fetched (make corpus-fetch CORPUS=fonts)")
	}
	fonts, err := text.NewFonts(context.Background(), nil, []string{dir})
	if err != nil {
		t.Fatal(err)
	}
	f := flat{
		auto: `<style:style style:name="gr1" style:family="graphic"><style:graphic-properties draw:textarea-vertical-align="bottom"/></style:style>` +
			`<style:style style:name="gr2" style:family="graphic"><style:graphic-properties draw:stroke="none" draw:fill="none"/></style:style>`,
		body: `<table:table table:name="S"><table:shapes>` +
			`<draw:rect draw:name="rect" draw:style-name="gr1" svg:x="1in" svg:y="1in" svg:width="2in" svg:height="1in"><text:p>Rect text</text:p></draw:rect>` +
			`<draw:frame draw:name="box" draw:style-name="gr2" svg:x="1in" svg:y="3in" svg:width="2in" svg:height="1in"><draw:text-box><text:p>Box text</text:p></draw:text-box></draw:frame>` +
			`</table:shapes><table:table-row><table:table-cell/></table:table-row></table:table>`,
	}
	doc, w, err := Read(context.Background(), f.bytes(), fonts, office.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if hasWarningAbout(w, warn.ContentUnsupported, "not drawn") {
		t.Errorf("warnings %v", w)
	}
	ds := doc.Sheets[0].Drawings
	if len(ds) != 2 {
		t.Fatalf("%d drawings, want the rectangle and the text box", len(ds))
	}
	for _, tc := range []struct {
		i    int
		want string
		fill bool
		low  bool
	}{{0, "Rect text", true, true}, {1, "Box text", false, false}} {
		d := ds[tc.i]
		frags := d.Picture.Vector(context.Background(), typeset.Rect{Width: d.Width, Height: d.Height})
		var got []string
		var y, size float64
		var walk func([]typeset.Fragment)
		walk = func(frags []typeset.Fragment) {
			for _, f := range frags {
				switch v := f.(type) {
				case *typeset.Text:
					var b strings.Builder
					for _, g := range v.Glyphs {
						b.WriteString(g.Text)
					}
					got = append(got, b.String())
					y, size = v.Origin.Y, v.Size
				case *typeset.Group:
					walk(v.Content)
				}
			}
		}
		walk(frags)
		if strings.Join(got, "") != tc.want || size != 12 {
			t.Errorf("drawing %d draws %q at %v points, want %q at 12", tc.i, got, size, tc.want)
		}
		if low := y > 72-20; low != tc.low {
			t.Errorf("drawing %d's baseline at %v in its 72-point box, at the bottom %v, want %v", tc.i, y, low, tc.low)
		}
		if fill := firstFill(frags) != nil; fill != tc.fill {
			t.Errorf("drawing %d fills %v, want %v", tc.i, fill, tc.fill)
		}
	}
}
