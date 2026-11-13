package layout

import (
	"slices"
	"unicode"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// maxInkDilation is the most an underline or overline stops short of a
// glyph it skips, in points: Chromium widens each gap by the line's
// thickness up to 13 CSS pixels.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 (Gotenberg 9.4.1) leaves 1, 4, 10 and 13px
//     between an Ahem glyph and underlines 1, 4, 10 and 20px thick.
const maxInkDilation = 13 * css.Pt

// curveSteps is the number of chords a glyph's curve is cut into to find
// where it crosses a decoration line.
const curveSteps = 8

// inkGaps returns the gaps, as left and right edges in page coordinates
// in increasing order, that an underline or overline from top to bottom
// leaves where it crosses the glyphs of runs of line l, moved by dx and
// dy (CSS Text Decoration 4, 2.10.5): from the leftmost to the rightmost
// point of each glyph's outline inside the line, widened on each side by
// the line's thickness up to [maxInkDilation]. A run whose
// text-decoration-skip-ink is none adds no gap. Auto, which lets the user
// agent choose, skips as Chromium does with [Options.BrowserRules], every
// glyph but those of Han, kana, Bopomofo and Hangul characters, and
// skips none without, as Word and LibreOffice draw underlines.
func (p *painter) inkGaps(runs []*lineItem, l *lineBox, dx, dy, top, bottom float64) [][2]float64 {
	widen := min(bottom-top, maxInkDilation)
	var gaps [][2]float64
	var ops []typeset.PathOp
	for _, it := range runs {
		t := it.text
		mode := it.box.st.TextDecorationSkipInk
		if t == nil || t.Face == nil || mode == css.SkipInkNone || mode == css.SkipInkAuto && !p.blink() {
			continue
		}
		f := p.outlineFace(t.Face)
		if f == nil || f.Upem() == 0 {
			continue
		}
		k := t.Size / float64(f.Upem())
		hs := hscale(t)
		x0, y0 := l.x+t.Origin.X+dx, l.y+t.Origin.Y+dy
		for _, g := range t.Glyphs {
			if mode == css.SkipInkAuto && ideographic(g.Text) {
				continue
			}
			ops = appendGlyph(ops[:0], f, g.GID, x0+float64(g.X*hs), y0+g.Y, k, hs, t.Skew)
			if lo, hi, ok := crossing(ops, top, bottom); ok {
				gaps = append(gaps, [2]float64{lo - widen, hi + widen})
			}
		}
	}
	slices.SortFunc(gaps, func(a, b [2]float64) int {
		switch {
		case a[0] < b[0]:
			return -1
		case a[0] > b[0]:
			return 1
		}
		return 0
	})
	return gaps
}

// ideographic reports characters that are all Han, Hiragana, Katakana,
// Bopomofo or Hangul, whose glyphs an underline of skip-ink auto crosses
// in Chromium; false for none.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 (Gotenberg 9.4.1) draws an auto underline
//     through Han, kana and Hangul glyphs, and skips Latin, Cyrillic and
//     Arabic ones, a Latin letter between two Han ones and a Latin letter
//     a CJK face draws included; skip-ink all skips them all.
func ideographic(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Bopomofo, unicode.Hangul) {
			return false
		}
	}
	return true
}

// crossing returns the leftmost and rightmost points of the outline ops
// strictly between top and bottom, false when it does not enter that
// band.
func crossing(ops []typeset.PathOp, top, bottom float64) (float64, float64, bool) {
	lo, hi, ok := 0.0, 0.0, false
	seg := func(a, b typeset.Point) {
		if max(a.Y, b.Y) <= top+metricSlack || min(a.Y, b.Y) >= bottom-metricSlack {
			return
		}
		xs := [2]float64{a.X, b.X}
		if a.Y != b.Y {
			at := func(y float64) float64 { return a.X + (b.X-a.X)*(y-a.Y)/(b.Y-a.Y) }
			ya, yb := min(max(a.Y, top), bottom), min(max(b.Y, top), bottom)
			xs = [2]float64{at(ya), at(yb)}
		}
		for _, x := range xs {
			if !ok {
				lo, hi, ok = x, x, true
				continue
			}
			lo, hi = min(lo, x), max(hi, x)
		}
	}
	var start, cur typeset.Point
	for _, op := range ops {
		switch op.Kind {
		case typeset.MoveTo:
			start, cur = op.P[0], op.P[0]
		case typeset.LineTo:
			seg(cur, op.P[0])
			cur = op.P[0]
		case typeset.CurveTo:
			prev := cur
			for i := 1; i <= curveSteps; i++ {
				u := float64(float64(i) / curveSteps)
				v := 1 - u
				q := typeset.Point{
					X: float64(v*v*v*cur.X) + float64(3*v*v*u*op.P[0].X) + float64(3*v*u*u*op.P[1].X) + float64(u*u*u*op.P[2].X),
					Y: float64(v*v*v*cur.Y) + float64(3*v*v*u*op.P[0].Y) + float64(3*v*u*u*op.P[1].Y) + float64(u*u*u*op.P[2].Y),
				}
				seg(prev, q)
				prev = q
			}
			cur = op.P[2]
		case typeset.ClosePath:
			seg(cur, start)
			cur = start
		}
	}
	return lo, hi, ok
}

// inkPieces returns the parts of [x0, x1] outside gaps, sorted by their left
// edge.
func inkPieces(x0, x1 float64, gaps [][2]float64) [][2]float64 {
	out := make([][2]float64, 0, len(gaps)+1)
	at := x0
	for _, g := range gaps {
		if g[1] <= at {
			continue
		}
		if g[0] >= x1 {
			break
		}
		if g[0] > at {
			out = append(out, [2]float64{at, g[0]})
		}
		at = g[1]
	}
	if at < x1 {
		out = append(out, [2]float64{at, x1})
	}
	return out
}
