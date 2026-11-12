//go:build !race

package sheet

import (
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
)

// pivotScaleDoc returns a document whose first sheet holds a source of n
// records, each its own member of the row field, and whose second sheet
// holds a pivot table over it.
func pivotScaleDoc(n int) (*Document, *Pivot) {
	rows := []Row{{Index: 0, Cells: []Cell{textCell(0, "k", nil), textCell(1, "v", nil)}}}
	for i := 1; i <= n; i++ {
		rows = append(rows, Row{Index: i, Cells: []Cell{numCell(0, float64(i), nil), numCell(1, 1, nil)}})
	}
	doc := newDoc(&Sheet{Name: "S", Rows: rows})
	out := &Sheet{Name: "P", Pivots: []Pivot{{
		Row: 0, Column: 3, Source: Range{LastRow: n, LastColumn: 1},
		Rows: []PivotField{{Field: 0}}, Data: []PivotData{{Field: 1, Function: PivotSum}},
	}}}
	doc.Sheets = append(doc.Sheets, out)
	return doc, &out.Pivots[0]
}

// TestPivot_ManyMembersLinear checks that a row field of 131,072 members
// is laid out in time linear in the records: each member's records are
// gathered in one pass, not by a pass over every record per member, which
// took seconds per hundred thousand members. Left out under the race
// detector, whose slowdown the bound does not allow for.
func TestPivot_ManyMembersLinear(t *testing.T) {
	doc, p := pivotScaleDoc(1 << 17)
	b := newBuilder(t.Context(), doc, pinnedFonts(t), office.Options{})
	start := time.Now()
	b.pivotOutput(p)
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("a pivot table of 131,072 members took %v", d)
	}
}
