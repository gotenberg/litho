package layout

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// TestTextStroke_OneRun holds the stroke of -webkit-text-stroke to how
// the run is painted (Compatibility Standard, 3.4.6): one text run with
// the stroke's pen, mitred as Chromium strokes it, filled in the fill
// colour, or stroked alone when the fill is transparent, and no path.
func TestTextStroke_OneRun(t *testing.T) {
	red, green := css.Color{R: 1, A: 1}, css.Color{G: 1, A: 1}
	for _, fill := range []css.Color{green, {}} {
		st := sty(func(s *css.Style) { s.GlyphPaint = css.NewGlyphPaint(px(3), red, fill, false) })
		res := run(t, doc(el(st, txt(st, "ab"))), page(100, 40))
		runs := texts(res, 1)
		if len(runs) != 1 {
			t.Fatalf("fill %+v: %d runs, want 1", fill, len(runs))
		}
		r := runs[0].run
		o := r.Outline
		if o == nil || !near(o.Width, px(3)) || o.Color != (typeset.Color{R: 1, A: 1}) || o.Join != typeset.MiterJoin || o.Miter != strokeMiter {
			t.Fatalf("fill %+v: outline %+v", fill, o)
		}
		if r.Color != (typeset.Color{G: fill.G, A: fill.A}) {
			t.Errorf("fill %+v: run colour %+v", fill, r.Color)
		}
		if got := decoPaths(res, red); len(got) != 0 {
			t.Errorf("fill %+v: stroke drawn as paths %+v", fill, got)
		}
	}
}

// TestTextStroke_PaintOrderStrokeFirst holds paint-order: stroke (SVG 2,
// 13.8) to the stroke under the fill: the glyph outlines stroked as a
// path, an artifact, painted before the run, which is filled alone and
// stays the one text run.
func TestTextStroke_PaintOrderStrokeFirst(t *testing.T) {
	red, green := css.Color{R: 1, A: 1}, css.Color{G: 1, A: 1}
	st := sty(func(s *css.Style) { s.GlyphPaint = css.NewGlyphPaint(px(4), red, green, true) })
	res := run(t, doc(el(st, txt(st, "ab"))), page(100, 40))
	runs := texts(res, 1)
	if len(runs) != 1 || runs[0].run.Outline != nil {
		t.Fatalf("runs %+v, want one without an outline", runs)
	}
	var path *typeset.Path
	pathFirst := false
	var walk func([]typeset.Fragment)
	walk = func(fs []typeset.Fragment) {
		for _, f := range fs {
			switch v := f.(type) {
			case *typeset.Path:
				if v.Stroke != nil && v.Stroke.Color == (typeset.Color{R: 1, A: 1}) {
					path = v
				}
			case *typeset.Text:
				pathFirst = path != nil
			case *typeset.Group:
				walk(v.Content)
			}
		}
	}
	walk(res.Pages[0].Content)
	if path == nil || !pathFirst || path.Parent != nil || !near(path.Stroke.Width, px(4)) {
		t.Fatalf("stroke path %+v, painted first %v: want an artifact under the run", path, pathFirst)
	}
}

// TestTextStroke_ShadowCoversTheStroke holds a text shadow to the glyphs
// as they paint, their stroke included, as Chromium casts it.
func TestTextStroke_ShadowCoversTheStroke(t *testing.T) {
	red, blue := css.Color{R: 1, A: 1}, css.Color{B: 1, A: 1}
	st := sty(func(s *css.Style) {
		s.GlyphPaint = css.NewGlyphPaint(px(2), red, css.Color{G: 1, A: 1}, false)
		s.TextShadow = []css.Shadow{{X: px(2), Y: px(2), Color: blue}}
	})
	res := run(t, doc(el(st, txt(st, "a"))), page(100, 40))
	runs := texts(res, 1)
	if len(runs) != 2 {
		t.Fatalf("%d runs, want the shadow and the text", len(runs))
	}
	sh := runs[0].run
	if sh.Outline == nil || sh.Outline.Color != (typeset.Color{B: 1, A: 1}) || sh.Color != (typeset.Color{B: 1, A: 1}) {
		t.Errorf("shadow run colour %+v outline %+v, want both blue", sh.Color, sh.Outline)
	}
}
