package sheet

import (
	"context"
	"math"
	"strings"
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

// TestOptimalHeights_ExcelRules locks the rows of a book laid out by
// Excel's rules: a row keeps its stored height whatever its text needs, a
// row without one keeps the sheet's default for taller text and grows a
// default row a line for wrapped text.
func TestOptimalHeights_ExcelRules(t *testing.T) {
	big := &Style{Font: Font{Size: 30}}
	sh := &Sheet{Name: "S", DefaultRowHeight: 14.52, Rows: []Row{
		{Index: 0, Cells: []Cell{textCell(0, "Tall", big)}},
		{Index: 1, Height: 5.76, Cells: []Cell{textCell(0, "Short", nil)}},
		{Index: 2, Cells: []Cell{textCell(0, "one two three four five six seven eight", &Style{Align: Alignment{Wrap: true}})}},
	}}
	doc := newDoc(sh)
	doc.ExcelRules = true
	b := newBuilder(context.Background(), doc, pinnedFonts(t), office.Options{})
	g := newGrid(doc, sh)
	b.optimalHeights(g)
	if h := g.rowHeight(0); !near(h, 14.52, 1e-9) {
		t.Errorf("row of 30 point text %v, want the default 14.52", h)
	}
	if h := g.rowHeight(1); !near(h, 5.76, 1e-9) {
		t.Errorf("row of 5.76 points %v, want it kept", h)
	}
	if h := g.rowHeight(2); h < 2*14.52-1e-9 || !near(math.Mod(h+1e-9, 14.52), 0, 1e-6) {
		t.Errorf("row of wrapped text %v, want whole default rows of 14.52", h)
	}
	doc.ExcelRules = false
	b = newBuilder(context.Background(), doc, pinnedFonts(t), office.Options{})
	g = newGrid(doc, sh)
	b.optimalHeights(g)
	if h := g.rowHeight(0); h <= 30 {
		t.Errorf("row of 30 point text %v by Calc's rules, want it grown", h)
	}
}

// TestFont_Substitute locks the family list a cell font draws with: its
// families, fontconfig's generic aliases resolved, then Noto Sans, the
// face LibreOffice substitutes for a family nothing serves.
func TestFont_Substitute(t *testing.T) {
	tests := []struct {
		families []string
		want     []string
	}{
		{[]string{"Aptos Narrow"}, []string{"Aptos Narrow", "Noto Sans"}},
		{[]string{"Calibri", "sans"}, []string{"Calibri", "sans-serif", "Noto Sans"}},
	}
	for _, tc := range tests {
		got := font{families: tc.families}.cssFamilies()
		if strings.Join(got, ",") != strings.Join(tc.want, ",") {
			t.Errorf("%v: %v, want %v", tc.families, got, tc.want)
		}
	}
	m := newMeasurer(context.Background(), pinnedFonts(t), "")
	unknown := m.width(font{families: []string{"Aptos Narrow"}, size: 11}, "Monday")
	noto := m.width(font{families: []string{"Noto Sans"}, size: 11}, "Monday")
	if !near(unknown, noto, 1e-9) {
		t.Errorf("an unknown family measures %v, Noto Sans %v", unknown, noto)
	}
}
