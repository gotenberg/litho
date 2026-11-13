package layout

import (
	"fmt"
	"math"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// An initial letter (CSS Inline 3, 4) is laid out as a float of the
// paragraph's start side that the converter makes of a ::first-letter
// whose initial-letter is not normal (Node.InitialLetter): the lines
// beside it shorten, the blocks after a short paragraph wrap around it,
// clear clears it, and it never overlaps another float (4.9.3). What a
// float does not do, the functions here do: its font size spans its lines,
// its cap height sits on the first line's, the first line moves down for a
// raised or sunken letter, and a block that starts with an initial letter
// or establishes a formatting context clears it (4.9.2).

// layoutInitialLetter lays the initial letter f out in the containing block
// cb of bc, the first line of its paragraph at lineTop, as
// [engine.layoutFloat] lays a float out, and returns its index in bc.
//
// Its used font size makes its cap height span size lines (4.4.3): the
// size less one times the paragraph's line height, plus the paragraph's
// cap height, over the cap height of the letter's own font. Its cap
// height then sits on the first line's, where the first line would be
// without the letter, and its content box runs from there, or from its
// glyphs' top when it has a top padding or border, to its baseline or the
// bottom of its glyphs, whichever is lower (4.5.4). Its content box ends
// where its glyphs end, rounded up to a whole pixel, and starts, with no
// padding or border at its start, its glyphs' start side bearing in, in
// whole pixels (4.6.2).
//
// CSS Inline 3, 4.6.1 places the letter by its sink, the first line where
// it is. litho follows Chromium, which aligns the letter's cap height on
// the first line's and moves the first line down by the lines the letter
// rises above its sink ([initialDrop]), and which aligns a letter of a
// fractional size by its top alone.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 draws, in Ahem
//     20px on 30px lines, initial-letter 3 at 95px on the third line's
//     baseline, 2.5 at 76.25px with its cap height on the first line's,
//     3 2 and 3 1 with the first line 30 and 60px lower; 2 at 45px on
//     lines of normal height; text beside it at its right edge, 10 + 95,
//     and 10 + 76.25 rounded up to 87; a 3px padding and a 4px margin
//     widen the room it takes and leave its glyph in place; font-size
//     and line-height on the letter change nothing. A DejaVu Sans 'H'
//     starts 5, 9, 13 and 17px left of its box at 58.85 to 182.42px, its
//     side bearing floored to whole pixels; a 'T', whose bearing is
//     negative, sits on its box's edge, and the text beside it starts
//     where its glyph ends, rounded up.
func (e *engine) layoutInitialLetter(f *box, cb cblock, bc *bfc, lineTop float64) int {
	root := paragraphOf(f)
	il := f.st.InitialLetter
	lh := e.lineHeight(root.st, root.lang)
	rm := e.faceMetrics(root.st, root.lang)
	capP := e.capHeight(root.st, root.lang)
	capTop := lineTop + rm.Ascent + float64((lh-rm.Ascent-rm.Descent)/2) - capP
	capL := float64((il.Size-1)*lh) + capP
	e.sizeInitialLetter(f, capL)
	f.laidOut = true
	resolveEdges(f, cb.w)
	f.w = constrainWidth(f, e.shrinkToFit(f, cb.w), cb.w)
	f.x, f.y = cb.x+f.m[css.Left], lineTop+f.m[css.Top]
	e.layoutOwnContext(f, cb)
	if len(f.lines) == 0 {
		return e.addFloatRect(f, bc, true)
	}
	ink, inked := e.letterInk(f)
	cx, cy := f.contentX(), f.contentY()
	// The content box's top is where the cap height starts, or above it
	// where a top padding or border makes it fit the glyphs; its bottom is
	// the baseline or the glyphs' bottom.
	top, bottom := 0.0, capL
	if inked {
		bottom = max(bottom, capL+ink.bottom)
		if f.pd[css.Top]+f.bd[css.Top] > 0 {
			top = min(top, capL+ink.top)
		}
	}
	first := f.lines[0]
	dy := cy - top + capL - (first.y + first.baseline)
	dx := 0.0
	ltr := f.float != css.FloatRight
	if inked && f.st.Width.Kind == css.Auto {
		left, right := ink.left-cx, cx+f.w-ink.right
		if ltr {
			inset := 0.0
			if f.pd[css.Left]+f.bd[css.Left] == 0 {
				inset = math.Floor(max(left, 0)/css.Pt) * css.Pt
			}
			dx = -inset
			f.w = float64(math.Ceil((ink.right-inset)/css.Pt-eps)*css.Pt) - cx
		} else {
			inset := 0.0
			if f.pd[css.Right]+f.bd[css.Right] == 0 {
				inset = math.Floor(max(right, 0)/css.Pt) * css.Pt
			}
			// The box keeps its right edge and starts where the glyphs,
			// moved inset to the right, start.
			l := float64(math.Floor((ink.left+inset)/css.Pt+eps) * css.Pt)
			dx = inset
			f.x += l - cx
			f.w = cx + f.w - l
		}
		f.w = max(f.w, 0)
	}
	for _, l := range f.lines {
		l.x += dx
		l.y += dy
	}
	f.h = bottom - top
	minY := capTop + top - f.pd[css.Top] - f.bd[css.Top] - f.m[css.Top]
	x, y := bc.placeBeside(f.float, f.marginW(), f.marginH(), max(minY, bc.clearInitial()), cb.x, cb.x+cb.w)
	f.shift(x+f.m[css.Left]-f.x, y+f.m[css.Top]-f.y)
	return e.addFloatRect(f, bc, true)
}

// paragraphOf returns the block container whose inline formatting context
// holds the float f: the nearest ancestor that is no inline box.
func paragraphOf(f *box) *box {
	p := f.parent
	for p != nil && p.kind == kindInline {
		p = p.parent
	}
	if p == nil {
		return f
	}
	return p
}

// initialDrop returns how far the first line of the paragraph of the
// initial letter f moves down: the lines the letter's size, floored,
// takes above its sink, as Chromium moves it (see
// [engine.layoutInitialLetter]), 0 for a dropped letter.
func (e *engine) initialDrop(f *box) float64 {
	il := f.st.InitialLetter
	k := math.Floor(il.Size) - float64(il.Sink)
	if k <= 0 {
		return 0
	}
	root := paragraphOf(f)
	return k * e.lineHeight(root.st, root.lang)
}

// sizeInitialLetter gives the initial letter f the style it is drawn in:
// the font size that makes its cap height capL, and a line height of its
// font's ascent and descent, so its line holds its glyphs and nothing
// more (CSS Inline 3, 4.5.1: font-size and line-height do not apply to
// it). Its text takes that style too.
func (e *engine) sizeInitialLetter(f *box, capL float64) {
	orig := f.st
	if f.node != nil && f.node.Style != nil {
		orig = f.node.Style
	}
	size := e.fontSize(orig)
	ratio := 0.0
	if size > 0 {
		ratio = e.capHeight(orig, f.lang) / size
	}
	if ratio <= 0 || !numeric.PositiveFinite(capL) {
		return
	}
	st := *orig
	st.FontSize = min(capL/ratio, maxFontSize)
	m := e.faceMetrics(&st, f.lang)
	st.LineHeight = css.Pts(m.Ascent + m.Descent)
	if old, ok := e.letters[f]; ok && old.FontSize == st.FontSize {
		return
	}
	if e.letters == nil {
		e.letters = map[*box]*css.Style{}
	}
	e.holdStyle(&st)
	e.letters[f] = &st
	prev := f.st
	f.st = &st
	for _, k := range f.kids {
		if k.st == prev || k.st == orig {
			k.st = &st
		}
	}
	f.ifc, f.measured = nil, false
}

// ink is the bounding box of glyph outlines: left and right in flow
// coordinates, top and bottom from the baseline, down positive.
type ink struct {
	left, right, top, bottom float64
}

// letterInk returns the bounding box of the outlines of the glyphs of the
// initial letter f, laid out, and whether any glyph has an outline: its
// left and right in flow coordinates, its top and bottom from the first
// line's baseline.
func (e *engine) letterInk(f *box) (ink, bool) {
	var b ink
	found := false
	var ops []typeset.PathOp
	base := f.lines[0].y + f.lines[0].baseline
	for _, l := range f.lines {
		for _, it := range l.items {
			t := it.text
			if t == nil || t.Face == nil {
				continue
			}
			face := e.outlineFace(t.Face)
			if face == nil || face.Upem() == 0 {
				continue
			}
			k := t.Size / float64(face.Upem())
			hs := hscale(t)
			x0, y0 := l.x+t.Origin.X, l.y+t.Origin.Y
			for _, g := range t.Glyphs {
				ops = appendGlyph(ops[:0], face, g.GID, x0+float64(g.X*hs), y0+g.Y, k, hs, t.Skew)
				for _, op := range ops {
					n := 1
					if op.Kind == typeset.CurveTo {
						n = 3
					}
					if op.Kind == typeset.ClosePath {
						n = 0
					}
					for _, p := range op.P[:n] {
						if !found {
							b = ink{left: p.X, right: p.X, top: p.Y - base, bottom: p.Y - base}
							found = true
							continue
						}
						b.left, b.right = min(b.left, p.X), max(b.right, p.X)
						b.top, b.bottom = min(b.top, p.Y-base), max(b.bottom, p.Y-base)
					}
				}
			}
		}
	}
	return b, found
}

// capHeight returns the cap height of the first available font of st in
// points: the font's own, else the height of its 'H' as Skia measures a
// font that records none, else its ascent, as CSS Values 4, 6.1.1 takes
// for the cap unit.
func (e *engine) capHeight(st *css.Style, lang string) float64 {
	m := e.faceMetrics(st, lang)
	if m.CapHeight > 0 {
		return m.CapHeight
	}
	size := e.fontSize(st)
	fc := e.fonts.Match(e.textStyle(st, e.familyLang(lang)))
	if fc == nil || size <= 0 {
		return m.Ascent
	}
	tf, err := fc.Typeset(e.ctx)
	if err != nil {
		// Shaping the text in this face reports it.
		return m.Ascent
	}
	face := e.outlineFace(tf)
	if face == nil || face.Upem() == 0 {
		return m.Ascent
	}
	gid, ok := face.NominalGlyph('H')
	if !ok {
		return m.Ascent
	}
	ops := appendGlyph(nil, face, uint16(gid), 0, 0, size/float64(face.Upem()), 1, 0) //nolint:gosec // a glyph ID of a font fits uint16
	h := 0.0
	for _, op := range ops {
		for _, p := range op.P {
			h = max(h, -p.Y)
		}
	}
	if h <= 0 {
		return m.Ascent
	}
	return h
}

// addFloatRect records the margin box of the float f, placed, in bc, and
// returns its index there, -1 past [maxFloats]. initial marks an initial
// letter.
func (e *engine) addFloatRect(f *box, bc *bfc, initial bool) int {
	if len(bc.floats) >= maxFloats {
		e.warnOnce(warn.LimitReached, warn.Approximated, fmt.Sprintf("more than %d floats in one formatting context: later ones do not push content aside", maxFloats), f.y)
		return -1
	}
	bc.floats = append(bc.floats, floatRect{
		side: f.float, x0: f.x - f.m[css.Left], y0: f.y - f.m[css.Top],
		x1: f.x - f.m[css.Left] + f.marginW(), y1: f.y - f.m[css.Top] + f.marginH(), initial: initial,
	})
	return len(bc.floats) - 1
}

// clearInitial returns the bottom of the lowest initial letter of c, -Inf
// for none: a block that starts with an initial letter, or that
// establishes a formatting context, goes below it (CSS Inline 3, 4.9.2).
func (c *bfc) clearInitial() float64 {
	y := math.Inf(-1)
	for _, f := range c.floats {
		if f.initial {
			y = max(y, f.y1)
		}
	}
	return y
}
