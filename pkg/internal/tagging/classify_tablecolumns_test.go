package tagging

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// sharedBaselineColumns sets two columns of prose line by line across both,
// each line a text object of its own, every line of the right column on the
// baseline of its neighbour in the left one.
func sharedBaselineColumns(lines int) (string, []string) {
	var sb strings.Builder
	var ids []string
	for i := range lines {
		y := 700 - 12*i
		fmt.Fprintf(&sb, "BT /F1 10 Tf 72 %d Td (A%d line of the left column, ten point.) Tj ET\n", y, i)
		fmt.Fprintf(&sb, "BT /F1 10 Tf 320 %d Td (B%d line of the right column, ten point.) Tj ET\n", y, i)
		ids = append(ids, fmt.Sprintf("A%d", i), fmt.Sprintf("B%d", i))
	}
	return sb.String(), ids
}

// TestClassify_ProseColumnsOnSharedBaselinesReadAsColumns pins that two
// columns of running text whose lines share baselines are no table. Every row
// of the two aligned, so the table pass tagged them Table > TR > TD, a grid of
// half-sentences a reader walks row by row across the gutter. ISO 14289-1, 7.2
// asks the tree to follow the reading order and 7.5 reserves Table for tabular
// content: each column reads as one paragraph, left then right.
func TestClassify_ProseColumnsOnSharedBaselinesReadAsColumns(t *testing.T) {
	t.Parallel()
	cs, ids := sharedBaselineColumns(5)
	got := classifiedIDs(t, cs, ids...)
	want := []string{"P:A0+A1+A2+A3+A4", "P:B0+B1+B2+B3+B4"}
	if !slices.Equal(got, want) {
		t.Errorf("elements: got %v, want %v", got, want)
	}
}

// TestClassify_BorderlessTableStaysTable pins the control: a grid of short
// cells with no ruling is a table, whatever its column count.
func TestClassify_BorderlessTableStaysTable(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	var ids []string
	for i := range 4 {
		y := 700 - 14*i
		fmt.Fprintf(&sb, "BT /F1 10 Tf 72 %d Td (K%d Item) Tj ET\n", y, i)
		fmt.Fprintf(&sb, "BT /F1 10 Tf 320 %d Td (V%d 42.00) Tj ET\n", y, i)
		ids = append(ids, fmt.Sprintf("K%d", i), fmt.Sprintf("V%d", i))
	}
	got := classifiedIDs(t, sb.String(), ids...)
	want := []string{"Table:K0+V0+K1+V1+K2+V2+K3+V3"}
	if !slices.Equal(got, want) {
		t.Errorf("elements: got %v, want %v", got, want)
	}
}

// TestClassify_SpacedFiguresStayTable pins that a row of figures one text
// object sets apart is no line of prose, however wide: a producer that sets a
// table's numeric columns in one text object per side leaves a gap a gutter
// wide between its figures.
func TestClassify_SpacedFiguresStayTable(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	var ids []string
	for i := range 4 {
		y := 700 - 14*i
		fmt.Fprintf(&sb, "BT /F1 10 Tf 72 %d Td [(N%d 7.69E-06) -3000 (3.84E-03) -3000 (1.30E+00)] TJ ET\n", y, i)
		fmt.Fprintf(&sb, "BT /F1 10 Tf 320 %d Td [(M%d 2.72E+01) -3000 (8.92E-04) -3000 (6.32E+00)] TJ ET\n", y, i)
		ids = append(ids, fmt.Sprintf("N%d", i), fmt.Sprintf("M%d", i))
	}
	got := classifiedIDs(t, sb.String(), ids...)
	want := []string{"Table:N0+M0+N1+M1+N2+M2+N3+M3"}
	if !slices.Equal(got, want) {
		t.Errorf("elements: got %v, want %v", got, want)
	}
}

// TestClassify_RuledWideGridStaysTable pins that ruling is table evidence: a
// grid of cells as wide as columns of prose, each row ruled off by a stroked
// line across the gutter, stays a table.
func TestClassify_RuledWideGridStaysTable(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	var ids []string
	for i := range 4 {
		y := 700 - 16*i
		fmt.Fprintf(&sb, "BT /F1 10 Tf 72 %d Td (R%d a description written out at length,) Tj ET\n", y, i)
		fmt.Fprintf(&sb, "BT /F1 10 Tf 320 %d Td (S%d and its remark written out at length.) Tj ET\n", y, i)
		fmt.Fprintf(&sb, "0.5 w 72 %d m 540 %d l S\n", y-4, y-4)
		ids = append(ids, fmt.Sprintf("R%d", i), fmt.Sprintf("S%d", i))
	}
	got := classifiedIDs(t, sb.String(), ids...)
	want := []string{"Table:R0+S0+R1+S1+R2+S2+R3+S3"}
	if !slices.Equal(got, want) {
		t.Errorf("elements: got %v, want %v", got, want)
	}
}

// TestClassify_FilledRuleWideGridStaysTable pins that a rule painted as a thin
// filled rectangle rules a grid as a stroked line does: Word, LibreOffice and
// TeX draw table rules as `re f` hairlines, so the grid of wide cells stays a
// table.
func TestClassify_FilledRuleWideGridStaysTable(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	var ids []string
	for i := range 4 {
		y := 700 - 16*i
		fmt.Fprintf(&sb, "BT /F1 10 Tf 72 %d Td (R%d a description written out at length,) Tj ET\n", y, i)
		fmt.Fprintf(&sb, "BT /F1 10 Tf 320 %d Td (S%d and its remark written out at length.) Tj ET\n", y, i)
		fmt.Fprintf(&sb, "72 %d 468 0.48 re f\n", y-5)
		ids = append(ids, fmt.Sprintf("R%d", i), fmt.Sprintf("S%d", i))
	}
	got := classifiedIDs(t, sb.String(), ids...)
	want := []string{"Table:R0+S0+R1+S1+R2+S2+R3+S3"}
	if !slices.Equal(got, want) {
		t.Errorf("elements: got %v, want %v", got, want)
	}
}

// TestClassify_CellByCellFilledRuleStaysTable pins that a border drawn cell by
// cell is one rule: Word fills each cell's segment of a border, and the square
// at each corner, as rectangles of their own, so no one of them crosses the
// gutter but together they do.
func TestClassify_CellByCellFilledRuleStaysTable(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	var ids []string
	for i := range 4 {
		y := 700 - 16*i
		fmt.Fprintf(&sb, "BT /F1 10 Tf 72 %d Td (R%d a description written out at length,) Tj ET\n", y, i)
		fmt.Fprintf(&sb, "BT /F1 10 Tf 320 %d Td (S%d and its remark written out at length.) Tj ET\n", y, i)
		fmt.Fprintf(&sb, "66 %d 0.48 0.48 re f* 66.48 %d 247.52 0.48 re f* 314 %d 0.48 0.48 re f* 314.48 %d 231.52 0.48 re f*\n", y-5, y-5, y-5, y-5)
		ids = append(ids, fmt.Sprintf("R%d", i), fmt.Sprintf("S%d", i))
	}
	got := classifiedIDs(t, sb.String(), ids...)
	want := []string{"Table:R0+S0+R1+S1+R2+S2+R3+S3"}
	if !slices.Equal(got, want) {
		t.Errorf("elements: got %v, want %v", got, want)
	}
}

// TestFilledRules_JoinsAbuttingBarsOnly pins how filled bars become rules: bars
// on one line that meet join, a bar a dash's gap away stays apart, and neither
// a tall area nor an upright bar is a horizontal rule.
func TestFilledRules_JoinsAbuttingBarsOnly(t *testing.T) {
	t.Parallel()
	got := filledRules([][4]float64{
		{200, 100, 300, 100.5},
		{100, 100, 200, 100.5},
		{306, 100, 400, 100.5},
		{100, 200, 400, 260},
		{150, 300, 150.5, 400},
	})
	want := []models.Line{
		{X1: 100, Y1: 100.25, X2: 300, Y2: 100.25},
		{X1: 306, Y1: 100.25, X2: 400, Y2: 100.25},
	}
	if !slices.Equal(got, want) {
		t.Errorf("rules: got %v, want %v", got, want)
	}
}

// TestClassify_ProseColumnsUnderTitleBarReadAsColumns pins that a thin bar
// clear of the grid rules no row of it: a bar under a page title, more than a
// line above the columns, and an underline under one column's heading that
// stops short of the gutter leave two columns of prose read as columns.
func TestClassify_ProseColumnsUnderTitleBarReadAsColumns(t *testing.T) {
	t.Parallel()
	cs, ids := sharedBaselineColumns(5)
	cs = "72 724 468 1.5 re f\n72 712 180 0.5 re f\n" + cs
	got := classifiedIDs(t, cs, ids...)
	want := []string{"P:A0+A1+A2+A3+A4", "P:B0+B1+B2+B3+B4"}
	if !slices.Equal(got, want) {
		t.Errorf("elements: got %v, want %v", got, want)
	}
}

// TestClassify_ProseColumnsOnShadedPanelReadAsColumns pins that a filled area
// is no rule: a shaded panel behind two columns of prose crosses the gutter,
// but it is as tall as the text it sits behind, not a hairline.
func TestClassify_ProseColumnsOnShadedPanelReadAsColumns(t *testing.T) {
	t.Parallel()
	cs, ids := sharedBaselineColumns(5)
	cs = "q 0.9 g 66 640 480 72 re f Q\n" + cs
	got := classifiedIDs(t, cs, ids...)
	want := []string{"P:A0+A1+A2+A3+A4", "P:B0+B1+B2+B3+B4"}
	if !slices.Equal(got, want) {
		t.Errorf("elements: got %v, want %v", got, want)
	}
}
