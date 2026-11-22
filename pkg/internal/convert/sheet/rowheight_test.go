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
	g := newGrid(t.Context(), doc, sh)
	b.heightsOf(t, g)
	if h := g.layoutRowHeight(0); !near(h, 12.78, 1e-9) {
		t.Errorf("Latin row %v, want 12.78", h)
	}
	if h := g.layoutRowHeight(1); !near(h, 15.65, 0.01) {
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
	doc := excelDoc(sh)
	b := newBuilder(context.Background(), doc, pinnedFonts(t), office.Options{})
	g := newGrid(t.Context(), doc, sh)
	b.heightsOf(t, g)
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
	g = newGrid(t.Context(), doc, sh)
	b.heightsOf(t, g)
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

// TestOptimalHeights_ExcelFitRows locks the rows of a book Excel measures
// as it opens it (FitRows): a row without a custom height fits the tallest
// font of its cells on the screen, at 121 printer pixels a 20-pixel
// standard row, an empty styled cell's font included, Calibri 14 at 25
// pixels, 151; a row stored shorter than its text, 14.45 points, prints
// at its fit; a row of smaller text keeps the default; a custom row keeps
// its height.
func TestOptimalHeights_ExcelFitRows(t *testing.T) {
	c14 := &Style{Font: Font{Families: []string{"Carlito"}, Size: 14}}
	sh := &Sheet{Name: "S", Rows: []Row{
		{Index: 0, Cells: []Cell{textCell(0, "label", nil), textCell(1, "fourteen", c14)}},
		{Index: 1, Height: 116 * excelPixel, Cells: []Cell{textCell(0, "stored", nil)}},
		{Index: 2, Cells: []Cell{textCell(0, "label", nil), {Column: 1, Style: c14}}},
		{Index: 3, Height: 116 * excelPixel, CustomHeight: true, Cells: []Cell{textCell(0, "custom", c14)}},
		// Text smaller than the Normal font keeps the default row.
		{Index: 4, Cells: []Cell{textCell(0, "small", &Style{Font: Font{Families: []string{"Carlito"}, Size: 8}})}},
	}}
	for _, fit := range []bool{false, true} {
		doc := excelDoc(sh)
		doc.StandardScreenRow, doc.FitRows = 20, fit
		b := newBuilder(context.Background(), doc, pinnedFonts(t), office.Options{})
		g := newGrid(t.Context(), doc, sh)
		b.heightsOf(t, g)
		want := []float64{121, 116, 121, 116, 121}
		if fit {
			want = []float64{151, 121, 151, 116, 121}
		}
		for r, w := range want {
			if got := math.Round(g.rowHeight(r) / excelPixel); got != w {
				t.Errorf("FitRows %v: row %d %v printer pixels, want %v", fit, r, got, w)
			}
		}
	}
}
