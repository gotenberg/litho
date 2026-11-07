package layout

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// TestTextScale lays scaled text out at its scaled advances and draws it
// under the run's horizontal scale, its glyph positions unscaled.
func TestTextScale(t *testing.T) {
	scaled := func(v float64) func(*css.Style) { return func(s *css.Style) { s.TextScale = v } }
	tests := []struct {
		name  string
		scale float64
		want  string
	}{
		{"half", 0.5, "a@0,8 b@5,8 c@10,8"},
		{"double", 2, "a@0,8 b@20,8 c@40,8"},
		{"natural", 1, "a@0,8 b@10,8 c@20,8"},
		{"zero is natural", 0, "a@0,8 b@10,8 c@20,8"},
		{"clamped to the most", 9, "a@0,8 b@60,8 c@120,8"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			span := inlineSty(scaled(tt.scale))
			res := run(t, doc(el(sty(), el(span, txt(span, "ab")), txt(sty(), "c"))), page(200, 50))
			if got := glyphLayout(res, 1); got != tt.want {
				t.Errorf("glyphs %s, want %s", got, tt.want)
			}
			r := texts(res, 1)[0].run
			if want := textScale(tt.scale); r.HScale != want {
				t.Errorf("HScale %v, want %v", r.HScale, want)
			}
		})
	}
}

// TestTextScale_Draws renders a run scaled to half its width.
func TestTextScale_Draws(t *testing.T) {
	small := func(s *css.Style) { s.FontSize = px(4) }
	span := inlineSty(small, fg(blue), func(s *css.Style) { s.TextScale = 0.5 })
	res := run(t, doc(el(sty(small), el(span, txt(span, "ab")), txt(sty(small, fg(red)), "c"))), page(10, 4))
	assertPixels(t, res, 1, `
		BBBBRRRR__
		BBBBRRRR__
		BBBBRRRR__
		BBBBRRRR__`)
}

// TestTextScale_Justify stretches a scaled line at its word separators in
// line space.
func TestTextScale_Justify(t *testing.T) {
	st := sty(func(s *css.Style) { s.TextScale = 0.5; s.TextAlign = css.AlignJustify })
	res := run(t, doc(el(st, txt(st, "a b cccccccccccccccccccccccccccccccccccccc"))), page(100, 50))
	if got, want := glyphLayout(res, 1)[:21], "a@0,8 b@95,8 c@0,18 c"; got != want {
		t.Errorf("glyphs %s, want %s", got, want)
	}
}
