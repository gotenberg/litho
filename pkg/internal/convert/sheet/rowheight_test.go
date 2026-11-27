package sheet

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
)

// TestOptimalHeights_FallbackFaces locks the optimal height of a row
// whose text of two scripts falls back to a taller face: Chinese text and
// a digit in Liberation Sans, which Calc's edit engine lays out, take
// Noto Sans CJK's line, 21 screen pixels with the padding, where a Latin
// row keeps the sheet's 12.78 points.
func TestOptimalHeights_FallbackFaces(t *testing.T) {
	mixed := textCell(0, "缩进0", nil)
	mixed.Runs = []Run{{Text: "缩进"}, {Text: "0"}}
	sh := &Sheet{Name: "S", Rows: []Row{
		{Index: 0, Cells: []Cell{textCell(0, "Indent", nil)}},
		{Index: 1, Cells: []Cell{mixed}},
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
// at its fit, its wrapped lines uncounted; a row of smaller text keeps the
// default; a custom row keeps its height.
func TestOptimalHeights_ExcelFitRows(t *testing.T) {
	c14 := &Style{Font: Font{Families: []string{"Carlito"}, Size: 14}}
	sh := &Sheet{Name: "S", Rows: []Row{
		{Index: 0, Cells: []Cell{textCell(0, "label", nil), textCell(1, "fourteen", c14)}},
		{Index: 1, Height: 116 * excelPixel, Cells: []Cell{textCell(0, "stored", nil)}},
		{Index: 2, Cells: []Cell{textCell(0, "label", nil), {Column: 1, Style: c14}}},
		{Index: 3, Height: 116 * excelPixel, CustomHeight: true, Cells: []Cell{textCell(0, "custom", c14)}},
		// Text smaller than the Normal font keeps the default row.
		{Index: 4, Cells: []Cell{textCell(0, "small", &Style{Font: Font{Families: []string{"Carlito"}, Size: 8}})}},
		// A cell without a format is measured in the Normal font, whatever
		// its column's.
		{Index: 5, Cells: []Cell{textCell(2, "unformatted", nil)}},
		// A row storing a height is measured by its fonts, not its lines.
		{Index: 6, Height: 116 * excelPixel, Cells: []Cell{textCell(0, "small words that wrap over several lines", &Style{Font: Font{Families: []string{"Carlito"}, Size: 8}, Align: Alignment{Wrap: true}})}},
		// A face no font serves is measured as the Default font.
		{Index: 7, Cells: []Cell{textCell(0, "unserved", &Style{Font: Font{Families: []string{"No Such Face"}, Size: 14}})}},
	}, Columns: []Column{{First: 2, Last: 2, Style: c14}}}
	for _, fit := range []bool{false, true} {
		doc := excelDoc(sh)
		doc.StandardScreenRow, doc.FitRows = 20, fit
		b := newBuilder(context.Background(), doc, pinnedFonts(t), office.Options{})
		g := newGrid(t.Context(), doc, sh)
		b.heightsOf(t, g)
		want := []float64{121, 116, 121, 116, 121, 121, 116, 121}
		if fit {
			want = []float64{151, 121, 151, 116, 121, 121, 116, 121}
		}
		for r, w := range want {
			if got := math.Round(g.rowHeight(r) / excelPixel); got != w {
				t.Errorf("FitRows %v: row %d %v printer pixels, want %v", fit, r, got, w)
			}
		}
	}
}

// TestOptimalHeights_ExcelRefitStored locks the rows of a book whose fonts
// Excel knows (RefitStored) that store a height without customHeight:
// each takes the fit of its cells' fonts, lower or higher than the
// stored height, an empty styled cell's font included, and sets its
// bottom-aligned text above the deepest font; a row of wrapped text, a
// custom row, a row without a stored height, a row of a thick border and
// a row holding a font of the theme's scheme keep theirs.
func TestOptimalHeights_ExcelRefitStored(t *testing.T) {
	c20 := &Style{Font: Font{Families: []string{"Carlito"}, Size: 20}}
	wrap := &Style{Align: Alignment{Wrap: true}}
	theme := &Style{Font: Font{Families: []string{"Carlito"}, Size: 20, Theme: true}}
	stored := 250 * excelPixel
	sh := &Sheet{Name: "S", Rows: []Row{
		{Index: 0, Height: stored, Cells: []Cell{textCell(0, "label", nil)}},
		{Index: 1, Height: 10, Cells: []Cell{textCell(0, "label", nil)}},
		{Index: 2, Height: stored, Cells: []Cell{textCell(0, "label", nil), textCell(1, "twenty", c20)}},
		{Index: 3, Height: stored, Cells: []Cell{textCell(0, "label", nil), {Column: 1, Style: c20}}},
		{Index: 4, Height: stored, Cells: []Cell{textCell(0, "label", wrap)}},
		{Index: 5, Height: stored, CustomHeight: true, Cells: []Cell{textCell(0, "custom", nil)}},
		{Index: 6, Cells: []Cell{textCell(0, "label", nil), textCell(1, "twenty", c20)}},
		{Index: 7, Height: stored, Thick: true, Cells: []Cell{textCell(0, "label", nil)}},
		{Index: 8, Height: stored, Cells: []Cell{textCell(0, "label", nil), textCell(1, "theme", theme)}},
	}}
	for _, refit := range []bool{false, true} {
		doc := excelDoc(sh)
		doc.StandardScreenRow, doc.RefitStored = 20, refit
		b := newBuilder(context.Background(), doc, pinnedFonts(t), office.Options{})
		g := newGrid(t.Context(), doc, sh)
		b.heightsOf(t, g)
		want := []float64{250, math.Round(10 / excelPixel), 250, 250, 250, 250, 121, 250, 250}
		if refit {
			want = []float64{121, 121, 211, 211, 250, 250, 121, 250, 250}
		}
		for r, w := range want {
			if got := math.Round(g.rowHeight(r) / excelPixel); got != w {
				t.Errorf("RefitStored %v: row %d %v printer pixels, want %v", refit, r, got, w)
			}
		}
		if d := g.drops[2]; refit != (d > 0) || g.drops[0] != 0 || g.drops[6] != 0 {
			t.Errorf("RefitStored %v: drops %v, want one under the Calibri 20 rows alone", refit, g.drops)
		}
	}
}

// TestCalcStandardHeight locks Calc's optimal height of a row of one
// plain line: the size in twips times 1.18, truncated, plus the top and
// bottom margins, less 23 twips, the face playing no part.
func TestCalcStandardHeight(t *testing.T) {
	one := [4]float64{1, 1, 1, 1}
	tests := []struct {
		size float64
		pad  [4]float64
		want float64
	}{
		{11, one, 13.8},
		{12, one, 15},
		{24, one, 29.15},
		{40, one, 48.05},
		{10.5, one, 13.2},
		{10, [4]float64{1.3896, 1, 1.3896, 1}, 13.45},
	}
	for _, tc := range tests {
		if got := calcStandardHeight(tc.size, tc.pad); !near(got, tc.want, 1e-9) {
			t.Errorf("size %v padding %v: %v, want %v", tc.size, tc.pad, got, tc.want)
		}
	}
}

// TestOptimalHeights_CalcStandard locks the rows Calc measures by their
// cells' font sizes: a stored height Calc's measure lowers, an empty
// styled cell, the document default's floor, and the rows it leaves alone,
// a custom one and one of wrapped text taller than its stored height.
func TestOptimalHeights_CalcStandard(t *testing.T) {
	carlito := &Style{Font: Font{Families: []string{"Carlito"}, Size: 11}}
	big := &Style{Font: Font{Size: 18}}
	sh := &Sheet{Name: "S", Rows: []Row{
		{Index: 0, Height: 14.5, Cells: []Cell{textCell(0, "Carlito", carlito)}},
		{Index: 1, Height: 12.8, Cells: []Cell{textCell(0, "a", nil), {Column: 1, Style: big}}},
		{Index: 2, Height: 30, Cells: []Cell{textCell(0, "small", nil)}},
		{Index: 3, Height: 30, CustomHeight: true, Cells: []Cell{textCell(0, "small", nil)}},
		{Index: 4, Height: 12.8, Cells: []Cell{textCell(0, "Liberation Sans", &Style{Font: Font{Size: 24}})}},
	}}
	doc := newDoc(sh)
	b := newBuilder(context.Background(), doc, pinnedFonts(t), office.Options{})
	g := newGrid(t.Context(), doc, sh)
	b.heightsOf(t, g)
	// The grid prints a row in whole hundredths of a millimetre, as Calc:
	// 276 twips print 13.78 points.
	for ri, want := range []float64{13.78, 22.03, 12.78, 29.99, 29.14} {
		if h := g.rowHeight(ri); !near(h, want, 0.01) {
			t.Errorf("row %d: %v, want %v", ri, h, want)
		}
	}
}
