package heuristics

import (
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestElementZone_HalfTurnedNegativeSize pins that the zone of a glyph set
// at a negative font size covers the ink it paints: the glyph advances
// leftward and its ink hangs below the baseline, as the rendering matrix
// Tfs × Tm turns it a half turn (ISO 32000-1, 9.4.4).
//
// # Corpus evidence
//
//   - pdfjs issue14415.pdf: "Authority" at -15 Tf from x 576.1, which PDF.js
//     places 81 units wide, below its baseline at y 64.6.
func TestElementZone_HalfTurnedNegativeSize(t *testing.T) {
	t.Parallel()
	el := models.TextElement{Text: "Authority", X: 576.1, Y: 64.6, Width: 81, FontSize: -15, Angle: math.Pi}
	z := ElementZone(el)
	inside := [][2]float64{{500, 55}, {570, 52}}
	for _, p := range inside {
		if p[0] < z.minX || p[0] > z.maxX || p[1] < z.minY || p[1] > z.maxY {
			t.Errorf("painted point %v outside the zone %+v", p, z)
		}
	}
	if z.maxX > 576.1+1e-9 || z.maxY > 64.6+15*glyphDescenderFraction+1e-9 {
		t.Errorf("zone %+v reaches past the glyph's origin side", z)
	}
}

// TestElementZone_MirroredGlyphInksTheRightOfItsAdvance pins that the zone of
// a reflected glyph lies on the right of its advance: a negative Th reverses
// the advance and reflects the glyph, so its ink still rises above a leftward
// baseline (ISO 32000-1, 9.4.4), and a Tm swapping the axes advances up the
// page and inks to its right.
func TestElementZone_MirroredGlyphInksTheRightOfItsAdvance(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name            string
		el              models.TextElement
		inside, outside [][2]float64
	}{
		{
			name:    "leftward",
			el:      models.TextElement{X: 300, Y: 400, Width: 48, FontSize: 12, Angle: math.Pi, InkRight: true},
			inside:  [][2]float64{{260, 410}, {299, 403}},
			outside: [][2]float64{{260, 395}},
		},
		{
			name:    "upward",
			el:      models.TextElement{X: 300, Y: 400, Width: 48, FontSize: 12, Angle: math.Pi / 2, InkRight: true},
			inside:  [][2]float64{{310, 440}, {303, 401}},
			outside: [][2]float64{{295, 440}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			z := ElementZone(tc.el)
			for _, p := range tc.inside {
				if !ZonesOverlap(z, ZoneFromRect([4]float64{p[0], p[1], p[0], p[1]})) {
					t.Errorf("painted point %v outside the zone %+v", p, z)
				}
			}
			for _, p := range tc.outside {
				if ZonesOverlap(z, ZoneFromRect([4]float64{p[0], p[1], p[0], p[1]})) {
					t.Errorf("unpainted point %v inside the zone %+v", p, z)
				}
			}
		})
	}
}
