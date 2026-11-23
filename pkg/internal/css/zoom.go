package css

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/numeric"
)

// maxZoom bounds an element's effective zoom, and minZoom keeps it from
// reaching 0. Anti-DoS guard, not spec-mandated: CSS Viewport 1, 4 sets
// no bound, and a zoom of 1e30, or a thousand nested zooms of 2, would
// otherwise overflow every length it multiplies. Real pages zoom by
// factors between a quarter and four; lengths are clamped to maxLength
// past these bounds anyway.
const (
	maxZoom = 1 << 16
	minZoom = 1.0 / (1 << 16)
)

// zoomSpec is a specified zoom: the factor a number or a percentage
// gives.
type zoomSpec float64

// parseZoom parses zoom (CSS Viewport 1, 4): <number [0,∞]> |
// <percentage [0,∞]>, a percentage being the factor times 100. Negative
// values are invalid. normal, which the specification no longer lists,
// is the factor 1, as Chromium parses it.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 lays out a
//     child of zoom: normal, and one of zoom: -1, inside a parent of zoom:
//     2 at twice its size, as a child of no zoom.
func parseZoom(v []token, _ *declCtx) (any, bool) {
	t, ok := single(v)
	if !ok {
		return nil, false
	}
	if t.typ == tIdent {
		if lower(t.value) == "normal" {
			return zoomSpec(1), true
		}
		return nil, false
	}
	n, ok := numberOrPercent(t)
	if !ok || !numeric.NonNegative(n) || math.IsInf(n, 1) {
		return nil, false
	}
	return zoomSpec(n), true
}

// zoomFactor returns the factor a specified zoom v multiplies the
// parent's effective zoom by, 1 for none. 0 is treated as 1, as CSS
// Viewport 1, 4 says for web compatibility.
func zoomFactor(v any) float64 {
	z, ok := v.(zoomSpec)
	if !ok || z == 0 {
		return 1
	}
	return float64(z)
}

// effectiveZoom returns the effective zoom of an element whose parent's is
// parent and whose own factor is f (CSS Viewport 1, 4): their product,
// within minZoom and maxZoom.
func effectiveZoom(parent, f float64) float64 {
	z := float64(parent * f)
	if !numeric.Finite(z) || z > maxZoom {
		return maxZoom
	}
	return max(z, minZoom)
}

// UsedZoom returns the element's effective zoom (CSS Viewport 1, 4): the
// product of its zoom and its ancestors', 1 for a style that sets none.
// Lengths of the style are already multiplied by it; the layout
// multiplies the natural sizes of pictures by it.
func (s *Style) UsedZoom() float64 {
	if s.Zoom > 0 {
		return s.Zoom
	}
	return 1
}

// zoomInherited multiplies by f the inherited longhand id's lengths in st,
// copied from a parent whose effective zoom is f times smaller: CSS
// Viewport 1, 4 multiplies the used value of every length by the element's
// effective zoom, and an inherited computed length is the parent's,
// already multiplied by the parent's.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 lays out the
//     child of zoom: 2 of a parent of font-size 10px, line-height 20px,
//     text-indent 10px and letter-spacing 10px with a font size of 20px,
//     lines 40px apart, an indent of 20px and 20px between letters.
func zoomInherited(id propID, st *Style, f float64) {
	scaleDim := func(d *Dimension) {
		if (d.Kind == Points || d.Kind == Calc) && d.expr == nil {
			d.Value = clampLen(float64(d.Value * f))
		}
	}
	switch id { //nolint:exhaustive // the inherited longhands that hold no length keep their value
	case pFontSize:
		st.FontSize = math.Max(0, math.Min(float64(st.FontSize*f), maxFontSize))
	case pLineHeight:
		scaleDim(&st.LineHeight)
	case pTextIndent:
		scaleDim(&st.TextIndent)
	case pLetterSpacing:
		scaleDim(&st.LetterSpacing)
	case pWordSpacing:
		scaleDim(&st.WordSpacing)
	case pTabSize:
		scaleDim(&st.TabSize)
	case pTextUnderlineOffset:
		scaleDim(&st.TextUnderlineOffset)
	case pBorderSpacing:
		st.BorderSpacing = [2]float64{clampLen(float64(st.BorderSpacing[0] * f)), clampLen(float64(st.BorderSpacing[1] * f))}
	case pTextShadow:
		if len(st.TextShadow) > 0 {
			sh := make([]Shadow, len(st.TextShadow))
			for i, s := range st.TextShadow {
				s.X, s.Y = clampLen(float64(s.X*f)), clampLen(float64(s.Y*f))
				s.Blur, s.Spread = clampLen(float64(s.Blur*f)), clampLen(float64(s.Spread*f))
				sh[i] = s
			}
			st.TextShadow = sh
		}
	case pTextStrokeWidth:
		if g := st.GlyphPaint; g != nil && g.StrokeWidth != 0 {
			c := *g
			c.StrokeWidth = clampLen(float64(c.StrokeWidth * f))
			st.GlyphPaint = &c
		}
	}
}
