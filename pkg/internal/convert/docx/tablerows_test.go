package docx

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
)

// cell returns a TD spanning cs columns and rs rows, named by its Alt.
func cell(name string, cs, rs int) *compose.StructElem {
	return &compose.StructElem{Role: "TD", Alt: name, ColSpan: cs, RowSpan: rs}
}

// rowNames lists the cells of row r, "_" for an empty one and its span
// past one after a colon.
func rowNames(r *compose.StructElem) string {
	var out []string
	for _, c := range r.Kids {
		n := c.Alt
		if n == "" {
			n = "_"
		}
		if c.ColSpan > 1 {
			n += ":" + strings.Repeat("|", c.ColSpan)
		}
		out = append(out, n)
	}
	return strings.Join(out, " ")
}

// TestPadTableRows locks that a row leaving grid columns without a cell
// gets an empty cell where they start, the row spans above taken into
// account, so every row counts the table's columns (ISO 14289-1, 7.2).
func TestPadTableRows(t *testing.T) {
	t.Parallel()
	tr := func(cells ...*compose.StructElem) *compose.StructElem {
		return &compose.StructElem{Role: "TR", Kids: cells}
	}
	// Rows of a table whose first rows skip a grid column (gridBefore),
	// whose merges run down, and whose last row spans every column.
	rows := []*compose.StructElem{
		tr(cell("a", 0, 3), cell("b", 7, 0), cell("c", 0, 3), cell("d", 0, 3)),
		tr(cell("e", 0, 0), cell("f", 0, 0), cell("g", 0, 0), cell("h", 0, 0), cell("i", 0, 0), cell("j", 0, 0), cell("k", 0, 0)),
		tr(cell("l", 7, 0)),
		tr(cell("m", 11, 0)),
	}
	// A row whose free column lies before a cell a row above carries.
	gapped := []*compose.StructElem{
		tr(cell("n", 0, 0), cell("o", 0, 0), cell("p", 0, 2)),
		tr(cell("q", 0, 0)),
	}
	root := &compose.StructElem{Role: "Document", Kids: []*compose.StructElem{
		{Role: "Table", Kids: []*compose.StructElem{{Role: "TBody", Kids: rows}}},
		{Role: "Table", Kids: gapped},
	}}
	padTableRows(root)
	want := []string{"a b:||||||| c d _", "e f g h i j k _", "l:||||||| _", "m:|||||||||||"}
	for i, r := range rows {
		if got := rowNames(r); got != want[i] {
			t.Errorf("row %d = %q, want %q", i, got, want[i])
		}
	}
	if got := rowNames(gapped[1]); got != "q _" {
		t.Errorf("gapped row = %q, want an empty cell after q", got)
	}
	if got := rowNames(gapped[0]); got != "n o p" {
		t.Errorf("full row = %q, want it unchanged", got)
	}
}
