package odt

import (
	"math"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/odf"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// frameFill sets the background of st, the box of a text frame, to the
// area fill its graphic properties p name (ODF 1.3, Part 3, 20.142
// draw:fill): a colour, a gradient or a bitmap, made as transparent as
// draw:opacity, the mean of a draw:opacity-name gradient's ends or
// style:background-transparency say, in that order; w and h are the
// box's size, 0 when unknown. A hatch is drawn as
// the colour draw:fill-hatch-solid puts under it, none without, with a
// warning. It reports whether the fill names one of those kinds.
//
// # Corpus evidence
//
//   - libreoffice/odt: odfimport/fdo81223.odt prints its frame of
//     draw:fill solid and style:background-transparency 100%, whose colour
//     is the default graphic style's, unfilled.
//   - libreoffice/odt: odfexport/framebackgrounds.odt prints frames of a
//     colour at 55% opacity, of a linear gradient and of a tiled bitmap.
//   - libreoffice/odt: odfimport/fdo90130-2.odt prints its frame's
//     radial gradient, yellow at the centre.
func (b *builder) frameFill(st *css.Style, p odf.Props, w, h float64) bool {
	alpha := 1.0
	switch {
	case p.Has("draw:opacity"):
		if v, ok := odf.Percent(p.Get("draw:opacity")); ok {
			alpha = clamp01(v)
		}
	case p.Has("draw:opacity-name"):
		if g := b.draws().Opacities[p.Get("draw:opacity-name")]; g != nil {
			alpha = clamp01(float64(g.StartValue+g.EndValue) / 2)
		}
	case p.Has("style:background-transparency"):
		if v, ok := odf.Percent(p.Get("style:background-transparency")); ok {
			alpha = clamp01(1 - v)
		}
	}
	switch p.Get("draw:fill") {
	case "solid":
		if col, ok := odf.ParseColor(p.Get("draw:fill-color")); ok {
			st.BackgroundColor = cssColor(col)
			st.BackgroundColor.A *= alpha
		}
	case "gradient":
		if g := b.draws().Gradients[p.Get("draw:fill-gradient-name")]; g != nil {
			st.BackgroundGradient = b.cssGradient(g, alpha, w, h)
		}
	case "bitmap":
		b.fillImage(st, p)
	case "hatch":
		b.warn("frame-hatch", warn.ContentUnsupported, warn.Approximated, "a frame's hatch is drawn as the colour under it")
		if p.Get("draw:fill-hatch-solid") == "true" {
			if col, ok := odf.ParseColor(p.Get("draw:fill-color")); ok {
				st.BackgroundColor = cssColor(col)
				st.BackgroundColor.A *= alpha
			}
		}
	default:
		return false
	}
	return true
}

// draws returns the document's drawing styles, indexed on first use.
func (b *builder) draws() *odf.DrawStyles {
	if b.drawStyles == nil {
		b.drawStyles = odf.NewDrawStyles(b.pkg)
	}
	return b.drawStyles
}

// cssGradient returns the CSS gradient that paints the draw:gradient g
// (ODF 1.3, Part 3, 16.42.1) over a box, its colours alpha opaque: a
// linear gradient from the start colour, draw:border of it unblended, to
// the end colour along draw:angle; an axial one from the end colour at
// both edges to the start colour in the middle; a radial one from the end
// colour at draw:cx, draw:cy to the start colour half the diagonal of
// the w by h box away, as LibreOffice sizes it.
// Ellipsoid, square and rectangular gradients are drawn radial, with a
// warning.
func (b *builder) cssGradient(g *odf.Gradient, alpha, w, h float64) *css.Gradient {
	stops := g.Stops
	if stops == nil {
		stops = []odf.GradientStop{{Offset: 0, Color: g.Start}, {Offset: 1, Color: g.End}}
	}
	stop := func(at float64, c odf.Color) css.GradientStop {
		cc := cssColor(c)
		cc.A *= alpha
		return css.GradientStop{Color: cc, Position: css.Dimension{Kind: css.Percent, Value: clamp01(at)}}
	}
	bd := clamp01(g.Border)
	out := &css.Gradient{Kind: css.LinearGradient, Angle: 180 - g.Angle}
	switch g.Style {
	case "axial":
		if bd > 0 {
			out.Stops = append(out.Stops, stop(0, stops[len(stops)-1].Color))
		}
		for _, s := range slices.Backward(stops) {
			out.Stops = append(out.Stops, stop(float64(bd+float64((1-bd)*(1-s.Offset)))/2, s.Color))
		}
		for _, s := range stops[1:] {
			out.Stops = append(out.Stops, stop(1-float64(float64(bd+float64((1-bd)*(1-s.Offset)))/2), s.Color))
		}
		if bd > 0 {
			out.Stops = append(out.Stops, stop(1, stops[len(stops)-1].Color))
		}
	case "radial", "ellipsoid", "square", "rectangular":
		if g.Style != "radial" {
			b.warn("gradient-"+g.Style, warn.ContentUnsupported, warn.Approximated, g.Style+" gradients are drawn radial")
		}
		out.Kind = css.RadialGradient
		out.Circle = true
		out.Size = css.RadialExplicit
		if w > 0 && h > 0 {
			out.Radius[0] = css.Pts(math.Sqrt(float64(w*w)+float64(h*h)) / 2)
		} else {
			// The box's height waits for its content: an ellipse half
			// the diagonal of a square box wide in each direction.
			out.Circle = false
			out.Radius = [2]css.Dimension{{Kind: css.Percent, Value: math.Sqrt2 / 2}, {Kind: css.Percent, Value: math.Sqrt2 / 2}}
		}
		out.Center = [2]css.Dimension{{Kind: css.Percent, Value: g.CX}, {Kind: css.Percent, Value: g.CY}}
		out.Stops = nil
		for _, s := range slices.Backward(stops) {
			out.Stops = append(out.Stops, stop(float64((1-bd)*(1-s.Offset)), s.Color))
		}
		if bd > 0 {
			out.Stops = append(out.Stops, stop(1, stops[0].Color))
		}
	default:
		if bd > 0 {
			out.Stops = append(out.Stops, stop(0, stops[0].Color))
		}
		for _, s := range stops {
			out.Stops = append(out.Stops, stop(bd+float64((1-bd)*s.Offset), s.Color))
		}
	}
	return out
}

// clamp01 returns v held to [0, 1], 0 for NaN.
func clamp01(v float64) float64 {
	if !(v > 0) {
		return 0
	}
	return min(v, 1)
}
