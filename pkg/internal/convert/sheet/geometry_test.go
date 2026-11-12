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
		g := newGrid(t.Context(), newDoc(sh), sh)
		r, ok := g.drawingCells(&sh.Drawings[0])
		if ok && (r.FirstRow < 0 || r.LastRow >= maxRows || r.LastRow < r.FirstRow) {
			t.Errorf("y %v: cells %+v", y, r)
		}
	}
}

// TestCalcPrinted checks the lengths Calc prints rows and columns at,
// measured in its PDFs: whole twips truncated to 1/100 mm.
func TestCalcPrinted(t *testing.T) {
	tests := []struct{ twips, hmm float64 }{
		{256, 451}, {283, 499}, {288, 508}, {299, 527}, {300, 529},
		{306, 539}, {307, 541}, {340, 599}, {480, 846}, {960, 1693},
		{1280, 2257}, {1600, 2822}, {0.4, 1},
	}
	for _, tt := range tests {
		if got, want := calcPrinted(tt.twips/20), tt.hmm*72/2540; math.Abs(got-want) > 1e-9 {
			t.Errorf("calcPrinted(%v twips) = %v, want %v", tt.twips, got, want)
		}
	}
	for _, v := range []float64{0, -1, math.NaN()} {
		if got := calcPrinted(v); got != 0 {
			t.Errorf("calcPrinted(%v) = %v, want 0", v, got)
		}
	}
}

// TestGrid_PrintedAndLayout checks that under Calc's rules the cells add
// up printed heights while pages and drawings keep the layout ones, and
// that Excel's rules print the layout heights as they are.
func TestGrid_PrintedAndLayout(t *testing.T) {
	rows := make([]Row, 20)
	for i := range rows {
		rows[i] = Row{Index: i, Height: 14.95, CustomHeight: true}
	}
	sh := &Sheet{Rows: rows, Columns: []Column{{First: 0, Last: 0, Width: 48}}}
	doc := newDoc(sh)
	g := newGrid(t.Context(), doc, sh)
	if got, want := g.rowY(20), 20*527*72/2540.0; math.Abs(got-want) > 1e-9 {
		t.Errorf("rowY(20) = %v, want %v", got, want)
	}
	if got := g.layoutRowY(20); math.Abs(got-299) > 1e-9 {
		t.Errorf("layoutRowY(20) = %v, want 299", got)
	}
	if got, want := g.colX(1), 1693*72/2540.0; math.Abs(got-want) > 1e-9 {
		t.Errorf("colX(1) = %v, want %v", got, want)
	}
	if got := g.layoutColX(1); got != 48 {
		t.Errorf("layoutColX(1) = %v, want 48", got)
	}
	doc.ExcelRules = true
	g = newGrid(t.Context(), doc, sh)
	if got := g.rowY(20); math.Abs(got-299) > 1e-9 {
		t.Errorf("Excel rowY(20) = %v, want 299", got)
	}
}

// TestCalcOrigin checks where Calc starts a page's cells, measured in its
// PDFs: the margin and header in twips, truncated to 1/100 mm, at the
// zoom's scale.
func TestCalcOrigin(t *testing.T) {
	// hmm is the page's hundredths: the unzoomed ones scaled by z.
	tests := []struct{ twips, z, hmm float64 }{
		{355, 1, 626}, {700, 1, 1234}, {1141, 1, 2012}, {1559, 1, 2749}, {1134, 1, 2000},
		{1134, 0.5, 2000}, {355, 0.5, 626}, {700, 0.73, 1691 * 0.73},
	}
	for _, tt := range tests {
		got := calcOrigin(tt.twips/20, tt.z)
		if want := tt.hmm * 72 / 2540; math.Abs(got-want) > 1e-9 {
			t.Errorf("calcOrigin(%v twips, %v) = %v, want %v", tt.twips, tt.z, got, want)
		}
	}
}
