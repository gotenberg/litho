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
