package tagging

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// borderlessTable sets rows of two short cells at size, below y, each cell a
// text object of its own, and returns the content with the cell ids.
func borderlessTable(y, rows int, size float64) (string, []string) {
	var sb strings.Builder
	var ids []string
	for i := range rows {
		row := y - 14*i
		fmt.Fprintf(&sb, "BT /F1 %g Tf 72 %d Td (K%d Item) Tj ET\n", size, row, i)
		fmt.Fprintf(&sb, "BT /F1 %g Tf 320 %d Td (V%d 42.00) Tj ET\n", size, row, i)
		ids = append(ids, fmt.Sprintf("K%d", i), fmt.Sprintf("V%d", i))
	}
	return sb.String(), ids
}

// TestClassify_TitleOverTableIsHeading pins that a title keeps its level when
// the block below it is a table. The carve left the title alone beside the
// table, the body was its own size, and it read as a P where the same title
// over paragraphs reads as an H1 (ISO 14289-1, 7.4).
func TestClassify_TitleOverTableIsHeading(t *testing.T) {
	t.Parallel()
	table, ids := borderlessTable(690, 4, 10)
	cs := "BT /F1 16 Tf 72 720 Td (T0 A title over a table) Tj ET\n" + table
	got := classifiedIDs(t, cs, append([]string{"T0"}, ids...)...)
	want := []string{"H1:T0", "Table:" + strings.Join(ids, "+")}
	if !slices.Equal(got, want) {
		t.Errorf("elements: got %v, want %v", got, want)
	}
}

// TestClassify_CaptionUnderTableStaysParagraph pins that a caption set at the
// size of the cells is no heading when the title over the table is one.
func TestClassify_CaptionUnderTableStaysParagraph(t *testing.T) {
	t.Parallel()
	table, ids := borderlessTable(690, 4, 10)
	cs := "BT /F1 16 Tf 72 720 Td (T0 A title over a table) Tj ET\n" + table +
		"BT /F1 10 Tf 72 620 Td (C0 Table 1) Tj ET\n"
	got := classifiedIDs(t, cs, append([]string{"T0", "C0"}, ids...)...)
	want := []string{"H1:T0", "Table:" + strings.Join(ids, "+"), "P:C0"}
	if !slices.Equal(got, want) {
		t.Errorf("elements: got %v, want %v", got, want)
	}
}

// TestClassify_ProseOverSmallerTableStaysParagraph pins the control: running
// text over a table set smaller than it is body text, not a run of headings.
func TestClassify_ProseOverSmallerTableStaysParagraph(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for i := range 4 {
		fmt.Fprintf(&sb, "BT /F1 12 Tf 72 %d Td (R%d A line of running text over the table.) Tj ET\n", 740-14*i, i)
	}
	table, ids := borderlessTable(670, 4, 8)
	got := classifiedIDs(t, sb.String()+table, append([]string{"R0", "R1", "R2", "R3"}, ids...)...)
	want := []string{"P:R0+R1+R2+R3", "Table:" + strings.Join(ids, "+")}
	if !slices.Equal(got, want) {
		t.Errorf("elements: got %v, want %v", got, want)
	}
}

// TestClassify_LargerHeaderRowStaysInTable pins that a header row set larger
// than the cells under it stays a row of the table: the heading pass never
// sees a carved cell.
func TestClassify_LargerHeaderRowStaysInTable(t *testing.T) {
	t.Parallel()
	table, ids := borderlessTable(686, 4, 10)
	cs := "BT /F1 16 Tf 72 720 Td (T0 A title over a table) Tj ET\n" +
		"BT /F1 12 Tf 72 700 Td (HA Item) Tj ET\nBT /F1 12 Tf 320 700 Td (HB Price) Tj ET\n" + table
	got := classifiedIDs(t, cs, append([]string{"T0", "HA", "HB"}, ids...)...)
	want := []string{"H1:T0", "Table:HA+HB+" + strings.Join(ids, "+")}
	if !slices.Equal(got, want) {
		t.Errorf("elements: got %v, want %v", got, want)
	}
}
