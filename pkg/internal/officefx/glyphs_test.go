package officefx

import (
	"context"
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/effect"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
)

// blockRun returns a run of one glyph of size points whose outline is a
// rectangle 0.6 sizes wide and half a size tall, standing on the baseline
// at y 100 from x 10.
func blockRun(size float64) *typeset.Text {
	prog := fontbuild.TrueType{Glyphs: []fontbuild.Glyph{{Name: "a", Unicode: 'a', Advance: 600}}}.Build()
	return &typeset.Text{
		Face: &typeset.Face{Program: prog, Key: "block"}, Size: size, Color: typeset.Color{A: 1},
		Origin: typeset.Point{X: 10, Y: 100}, Glyphs: []typeset.Glyph{{GID: 1}},
	}
}

// TestGlyphEffects_PowerPointReflectionMovesByTheSize checks that the
// reflection of a run on PowerPoint's rules lies its distance times the
// size over 15.2 below the baseline, the copy as tall as the glyphs.
func TestGlyphEffects_PowerPointReflectionMovesByTheSize(t *testing.T) {
	refl := &css.OfficeReflection{StartAlpha: 0.5, EndPos: 1, FadeDir: 90, Dist: 8, Dir: 90, SX: 1, SY: -1, Align: css.RectAlign{Y: 1}}
	for _, size := range []float64{12, 30, 72} {
		var px effect.Page
		frags, _, err := glyphEffects(context.Background(), blockRun(size), &css.OfficeEffects{Reflection: refl}, &px)
		if err != nil || len(frags) != 1 {
			t.Fatalf("%v points: %d fragments, error %v, want the reflection", size, len(frags), err)
		}
		r := frags[0].(*typeset.Image).Rect
		shift := 8 * size / pptReflectionDist
		if top := r.Y; math.Abs(top-(100+shift)) > 0.5 {
			t.Errorf("%v points: reflection starts at %.2f, want %.2f", size, top, 100+shift)
		}
		if end := r.Y + r.Height; math.Abs(end-(100+shift+size/2)) > 0.5 {
			t.Errorf("%v points: reflection ends at %.2f, want %.2f", size, end, 100+shift+size/2)
		}
	}
}
