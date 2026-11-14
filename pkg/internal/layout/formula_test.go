package layout

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// TestReplaced_PictureBaseline sits an inline vector picture that has a
// baseline of its own on the line by it (MathML Core, 3.1.1), its top
// Baseline above the text's baseline.
func TestReplaced_PictureBaseline(t *testing.T) {
	var drawn typeset.Rect
	pic := &Picture{
		Width: px(20), Height: px(30), NoRatio: true, HasBaseline: true, Baseline: px(10),
		Vector: func(_ context.Context, r typeset.Rect) []typeset.Fragment {
			drawn = r
			return nil
		},
	}
	root := doc(el(sty(), txt(inlineSty(), "X"), &Node{Kind: Replaced, Style: inlineSty(), Picture: pic}))
	res := run(t, root, page(200, 200))
	ts := texts(res, 1)
	if len(ts) == 0 {
		t.Fatal("no text drawn")
	}
	baseline := ts[0].y
	if !near(drawn.Y+px(10), baseline) {
		t.Fatalf("picture top at %g, text baseline at %g: want the top 10px above", drawn.Y, baseline)
	}
}

// TestTransform_MathAuto draws a text of one character as its
// mathematical italic form under text-transform: math-auto, and leaves a
// longer text alone (MathML Core, 4.2).
func TestTransform_MathAuto(t *testing.T) {
	var prev rune
	if got := transform("x", css.MathAuto, "", &prev); got != "\U0001D465" {
		t.Fatalf("math-auto x = %q", got)
	}
	if got := transform("sin", css.MathAuto, "", &prev); got != "sin" {
		t.Fatalf("math-auto sin = %q", got)
	}
}
