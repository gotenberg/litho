package formula

import (
	"math"

	mt "github.com/gotenberg/litho/pkg/internal/typography/mathtable"
)

// kernGlyph is the one glyph a token's box draws in its first available
// font, at its size, whose math kerning a script reads.
type kernGlyph struct {
	face *face
	gid  uint16
	size float64
}

// kern returns the math kerning of g's corner at height points above its
// baseline, in points (OpenType, MATH, MathKern table).
func (g *kernGlyph) kern(corner mt.Corner, height float64) float64 {
	if g == nil || g.face == nil || g.face.math == nil || g.size <= 0 {
		return 0
	}
	units := float64(height*g.face.upem) / g.size
	h := int(math.Round(min(max(units, math.MinInt16), math.MaxInt16))) // fits int: within the int16 range
	return g.face.units(float64(g.face.math.Kern(g.gid, corner, h)), g.size)
}

// superscriptKern returns how far a superscript, its baseline shift
// above base's, moves toward base by math kerning (OpenType, MATH,
// MathKernInfo table, the kerning algorithm for superscripts): the least
// of the sums of base's top-right kern and the script's bottom-left kern
// at the script's ink bottom and at base's ink top. 0 unless both are
// single glyphs.
func superscriptKern(base, sup *box, shift float64) float64 {
	if base.glyph == nil || sup.glyph == nil {
		return 0
	}
	bottom := shift - sup.inkDesc
	top := base.inkAsc
	a := base.glyph.kern(mt.TopRight, bottom) + sup.glyph.kern(mt.BottomLeft, bottom-shift)
	b := base.glyph.kern(mt.TopRight, top) + sup.glyph.kern(mt.BottomLeft, top-shift)
	return min(a, b)
}

// subscriptKern returns how far a subscript, its baseline shift below
// base's, moves toward base by math kerning (the same algorithm for
// subscripts): the least of the sums of base's bottom-right kern and the
// script's top-left kern at the script's ink top and at base's ink
// bottom.
func subscriptKern(base, sub *box, shift float64) float64 {
	if base.glyph == nil || sub.glyph == nil {
		return 0
	}
	top := sub.inkAsc - shift
	bottom := -base.inkDesc
	a := base.glyph.kern(mt.BottomRight, top) + sub.glyph.kern(mt.TopLeft, top+shift)
	b := base.glyph.kern(mt.BottomRight, bottom) + sub.glyph.kern(mt.TopLeft, bottom+shift)
	return min(a, b)
}
