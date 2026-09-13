package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestUA1TableHeaderVerdictPerTable pins ISO 14289-1, 7.5 t1 as veraPDF counts
// it: the subject is the TD (SETD, hasConnectedHeader), a TH without /Scope is
// no subject of its own, and the header fallback stops at the first cell whose
// headers it cannot determine, so a table yields one verdict however many of
// its cells are unconnected. Every case was measured against veraPDF.
func TestUA1TableHeaderVerdictPerTable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	th := func(scope string) capNode {
		if scope == "" {
			return capNode{"TH", nil}
		}
		return capNode{"TH /A << /O /Table /Scope /" + scope + " >>", nil}
	}
	td := capNode{"TD", nil}
	tr := func(cells ...capNode) capNode { return capNode{"TR", cells} }
	table := func(rows ...capNode) capNode { return capNode{"Table", rows} }

	tests := []struct {
		name string
		tree capNode
		want int
	}{
		{"unscoped column headers over two cells", table(tr(th(""), th("")), tr(td, td)), 1},
		{"unscoped headers over six cells", table(tr(th(""), th(""), th("")), tr(td, td, td), tr(td, td, td)), 1},
		{"one scoped and one unscoped header", table(tr(th("Column"), th("")), tr(td, td)), 1},
		{"unscoped row headers", table(tr(th(""), td), tr(th(""), td)), 1},
		{"column-scoped headers", table(tr(th("Column"), th("Column")), tr(td, td)), 0},
		{"row-scoped headers", table(tr(th("Row"), td), tr(th("Row"), td)), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			viols, err := ValidatePDFUA(ctx, buildCapDoc(t, tt.tree, "<< >>"), models.PDFUA1)
			if err != nil {
				t.Fatalf("ValidatePDFUA: %v", err)
			}
			got := 0
			for _, e := range viols {
				if v, ok := e.(Violation); ok && v.Rule == Rule_14289_1_7_5_t01 {
					got++
				}
			}
			if got != tt.want {
				t.Errorf("7.5 t1 fired %d times, want %d", got, tt.want)
			}
		})
	}
}
