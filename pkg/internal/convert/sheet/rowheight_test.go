package sheet

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
)

// TestOptimalHeights_FallbackFaces locks the optimal height of a row
// whose text falls back to a taller face: Chinese text in Liberation Sans
// takes Noto Sans CJK's line, 21 screen pixels with the padding, where a
// Latin row keeps the sheet's 12.78 points.
func TestOptimalHeights_FallbackFaces(t *testing.T) {
	sh := &Sheet{Name: "S", Rows: []Row{
		{Index: 0, Cells: []Cell{textCell(0, "Indent", nil)}},
		{Index: 1, Cells: []Cell{textCell(0, "缩进0", nil)}},
	}}
	doc := newDoc(sh)
	b := newBuilder(context.Background(), doc, pinnedFonts(t), office.Options{})
	g := newGrid(doc, sh)
	b.optimalHeights(g)
	if h := g.rowHeight(0); !near(h, 12.78, 1e-9) {
		t.Errorf("Latin row %v, want 12.78", h)
	}
	if h := g.rowHeight(1); !near(h, 15.65, 0.01) {
		t.Errorf("Chinese row %v, want 15.65", h)
	}
}
