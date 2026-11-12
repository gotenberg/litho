package sheet

import (
	"math"
	"testing"
)

// TestDrawingCells_FarAbsolute checks that a drawing placed past the
// sheet's last row, or at an infinite or undefined position, covers cells
// inside the sheet: its bottom edge stays within the row index.
func TestDrawingCells_FarAbsolute(t *testing.T) {
	for _, y := range []float64{1e30, math.Inf(1), math.NaN()} {
		sh := &Sheet{Drawings: []Drawing{{Absolute: true, X: 10, Y: y, Width: 20, Height: 20}}}
		g := newGrid(newDoc(sh), sh)
		r, ok := g.drawingCells(&sh.Drawings[0])
		if ok && (r.FirstRow < 0 || r.LastRow >= maxRows || r.LastRow < r.FirstRow) {
			t.Errorf("y %v: cells %+v", y, r)
		}
	}
}
