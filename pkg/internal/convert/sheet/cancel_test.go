package sheet

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
)

// cancelled returns a context already cancelled.
func cancelled(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	return ctx
}

// columnOfSums returns a document of n rows, each holding 1 in column A
// and, in column B, a sum over the two cells of column A from its own row.
func columnOfSums(n int) *Document {
	sh := &Sheet{Name: "S"}
	for r := range n {
		sh.Rows = append(sh.Rows, Row{Index: r, Cells: []Cell{
			{Column: 0, Value: Value{Kind: Number, Number: 1}},
			{Column: 1, Formula: "SUM(A" + strconv.Itoa(r+1) + ":A" + strconv.Itoa(r+2) + ")"},
		}})
	}
	return newDoc(sh)
}

// TestBuilder_StopsOnCancel checks that a cancelled context stops the
// formulas, the pivot tables and the page plan with the context's error.
func TestBuilder_StopsOnCancel(t *testing.T) {
	t.Parallel()
	ctx := cancelled(t)
	doc := columnOfSums(3 * formulaStepsPerCheck)
	if _, err := newBuilder(ctx, doc, nil, office.Options{}).applyFormulas(doc); !errors.Is(err, context.Canceled) {
		t.Errorf("applyFormulas on a cancelled context = %v, want context.Canceled", err)
	}
	pdoc := newDoc(pivotSheet(Pivot{
		Name: "P", Column: 5, SourceSheet: 0, Source: Range{LastRow: 4, LastColumn: 2},
		Data: []PivotData{{Field: 2, Function: PivotSum}},
	}))
	if _, err := newBuilder(ctx, pdoc, nil, office.Options{}).applyPivots(pdoc); !errors.Is(err, context.Canceled) {
		t.Errorf("applyPivots on a cancelled context = %v, want context.Canceled", err)
	}
	ps := a4()
	ps.Scale, ps.FitWidth = 0, 1
	gdoc := newDoc(grid150(ps))
	b := newBuilder(ctx, gdoc, pinnedFonts(t), office.Options{})
	if _, err := b.planSheet(0, gdoc.Sheets[0]); !errors.Is(err, context.Canceled) {
		t.Errorf("planSheet on a cancelled context = %v, want context.Canceled", err)
	}
	sp := newBuilder(t.Context(), gdoc, pinnedFonts(t), office.Options{}).planOf(t, 0, gdoc.Sheets[0])
	if _, err := sp.zoomFor(ctx, Range{LastRow: 149, LastColumn: 149}); !errors.Is(err, context.Canceled) {
		t.Errorf("zoomFor on a cancelled context = %v, want context.Canceled", err)
	}
	if _, _, _, err := sp.layoutPages(ctx, Range{LastRow: 149, LastColumn: 149}, 100); !errors.Is(err, context.Canceled) {
		t.Errorf("layoutPages on a cancelled context = %v, want context.Canceled", err)
	}
}
