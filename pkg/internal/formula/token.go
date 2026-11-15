package formula

import (
	"fmt"
	"strings"

	gofont "github.com/go-text/typesetting/font"

	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// maxLineWidth is the width a token is shaped in, wide enough that no
// token wraps: MathML Core, 2.2.2 treats white-space as nowrap on every
// MathML element.
const maxLineWidth = 1e6

// token lays out the token element e, styled st, by the layout of mtext
// (MathML Core, 3.2.1.1), its text transformed by text-transform:
// math-auto first (4.2).
func (l *layouter) token(e *Element, st *Style) *box {
	s := strings.Join(strings.Fields(e.Text), " ")
	if st.MathAuto {
		if rs := []rune(s); len(rs) == 1 {
			if r, ok := ItalicForm(rs[0]); ok {
				if st.FontVariants {
					it := *st
					it.Italic = true
					st = &it
				} else {
					s = string(r)
				}
			}
		}
	}
	b := l.textBox(s, st)
	if e.Name != "mo" {
		// Under distances, text takes its font's box; an operator, a
		// symbol, keeps its ink.
		l.faceOf(st).fontBoxed(b, st.Size, 0)
	}
	return b
}

// textBox shapes s in st and returns its box: its advance, and its ink
// above and below the baseline as its ink and line metrics (MathML Core,
// 3.2.1.1). A text of one glyph takes that glyph's italics correction and
// top accent attachment from the MATH table of the first available font
// when it is drawn in that font.
func (l *layouter) textBox(s string, st *Style) *box {
	b := &box{}
	if s == "" {
		return b
	}
	lines, rep, err := l.s.fonts.Layout(l.ctx, []text.Run{{Text: s, Style: textStyle(st)}}, maxLineWidth, text.Options{})
	if err != nil {
		l.fail(fmt.Errorf("shaping a formula token: %w", err))
		return b
	}
	if len(rep.MissingGlyphs) > 0 {
		l.warn(warn.MissingGlyph, warn.Approximated, fmt.Sprintf("no font draws %q of a formula", string(rep.MissingGlyphs)))
	}
	if len(lines) == 0 {
		return b
	}
	line := lines[0]
	b.w = line.Width
	first := true
	glyphCount := 0
	var only uint16
	var onlyKey string
	for _, run := range line.Runs {
		glyphCount += len(run.Glyphs)
		if len(run.Glyphs) > 0 {
			only, onlyKey = run.Glyphs[0].GID, run.Face.Key
		}
		g := l.s.program(run.Face)
		if g == nil {
			continue
		}
		k := run.Size / float64(g.Upem())
		for _, gl := range run.Glyphs {
			ext, ok := g.GlyphExtents(gofont.GID(gl.GID))
			if !ok || ext.Height == 0 {
				continue
			}
			top := float64(float64(ext.YBearing) * k)
			bottom := float64(float64(ext.YBearing+ext.Height) * k)
			asc, desc := top-gl.Y, gl.Y-bottom
			if first {
				b.inkAsc, b.inkDesc, first = asc, desc, false
				continue
			}
			b.inkAsc, b.inkDesc = max(b.inkAsc, asc), max(b.inkDesc, desc)
		}
	}
	if !l.mem.Take(int64(glyphCount), limits.GlyphCost, "formula glyphs") {
		l.fail(l.mem.Err())
		return b
	}
	b.asc, b.desc = b.inkAsc, b.inkDesc
	if glyphCount == 1 {
		if f := l.faceOf(st); f != nil && f.ts.Key == onlyKey {
			b.glyph = &kernGlyph{face: f, gid: only, size: st.Size}
			if v, ok := f.math.ItalicsCorrection(only); ok {
				b.italic = f.units(float64(v), st.Size)
			}
			if v, ok := f.math.TopAccentAttachment(only); ok {
				b.accent, b.hasAccent = f.units(float64(v), st.Size), true
			}
		}
	}
	if st.Hidden {
		return b
	}
	for _, run := range line.Runs {
		b.items = append(b.items, item{x: run.Origin.X, y: run.Origin.Y, run: run})
	}
	return b
}
