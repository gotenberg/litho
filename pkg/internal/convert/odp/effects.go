package odp

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/convert/slides"
	"github.com/gotenberg/litho/pkg/internal/odf"
)

// The shadow Impress's item pool gives a shape whose styles set
// draw:shadow visible and nothing else: 0.2 cm right and down, grey
// 0x808080, opaque, hard.
//
// # Corpus evidence
//
//   - probe, odp: a flat presentation without styles whose rectangle sets
//     draw:shadow visible alone prints a copy 5.67 pt right and down in
//     0x808080.
var (
	poolShadowOffset = 0.2 / 2.54 * 72
	poolShadowColor  = odf.Color{R: 0x80, G: 0x80, B: 0x80}
)

// maxEffectRadius caps a blur, glow or soft edge radius and a shadow's
// offset, in points. Anti-DoS guard, not spec-mandated: 1000 pt is wider
// than any slide, so a larger value draws the same.
const maxEffectRadius = 1000

// effects fills the shadow, glow and soft edge of s from the graphic
// properties p: draw:shadow visible with draw:shadow-offset-x,
// draw:shadow-offset-y, draw:shadow-color and draw:shadow-opacity (ODF
// 1.3, Part 3, 20.158 to 20.161) and LibreOffice's loext:shadow-blur; the
// glow of loext:glow-radius, loext:glow-color (black by default) and
// loext:glow-transparency; the soft edge of loext:softedge-radius. What
// the styles leave unset takes Impress's pool defaults. A text glow
// (loext:glow-text-radius) prints nothing, with a warning.
//
// # Corpus evidence
//
//   - libreoffice/odp: sd/shadow-blur's oval prints the shadow its 0.67 by 0.994 cm
//     offsets and 0.388 cm blur give, as sd/shape-blur-effect.pptx does;
//     a glow without loext:glow-color glows black.
func (r *reader) effects(p odf.Props, s *slides.Shape) {
	length := func(key string, def float64) float64 {
		v, ok := odf.Length(p.Get(key))
		if !ok || math.IsNaN(v) {
			return def
		}
		return min(max(v, -maxEffectRadius), maxEffectRadius)
	}
	if p.Get("draw:shadow") == "visible" {
		c, ok := odf.ParseColor(p.Get("draw:shadow-color"))
		if !ok {
			c = poolShadowColor
		}
		op := 1.0
		if v, ok := odf.Percent(p.Get("draw:shadow-opacity")); ok {
			op = min(max(v, 0), 1)
		}
		s.Shadow = &slides.Shadow{
			Color: cssColor(c), Opacity: op, ScaleX: 1, ScaleY: 1,
			OffsetX: length("draw:shadow-offset-x", poolShadowOffset),
			OffsetY: length("draw:shadow-offset-y", poolShadowOffset),
			Blur:    max(length("loext:shadow-blur", 0), 0),
		}
	}
	if rad := length("loext:glow-radius", 0); rad > 0 {
		c, ok := odf.ParseColor(p.Get("loext:glow-color"))
		if !ok {
			c = odf.Color{}
		}
		op := 1.0
		if v, ok := odf.Percent(p.Get("loext:glow-transparency")); ok {
			op = min(max(1-v, 0), 1)
		}
		if op > 0 {
			s.Glow = &slides.Glow{Color: cssColor(c), Opacity: op, Radius: rad}
		}
	}
	if rad := length("loext:softedge-radius", 0); rad > 0 {
		s.SoftEdge = rad
	}
	if rad := length("loext:glow-text-radius", 0); rad > 0 {
		r.warn("glow", "text glows are not drawn")
	}
}
