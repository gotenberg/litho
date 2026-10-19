package tagging

import (
	"fmt"
	"slices"
	"strings"
	"testing"
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
