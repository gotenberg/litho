package sheet

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// TestMergeTooLarge locks the bound on the cells of one merge: a merge of
// maxMergeCells cells is kept, one cell more is not, and the whole grid,
// whose 2^34 cells wrap a 32-bit product to 0, is not either.
func TestMergeTooLarge(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		m    Range
		want bool
	}{
		{"a title across eight columns", Range{FirstRow: 0, LastRow: 0, FirstColumn: 0, LastColumn: 7}, false},
		{"exactly the bound", Range{FirstRow: 0, LastRow: 255, FirstColumn: 0, LastColumn: 255}, false},
		{"one row past the bound", Range{FirstRow: 0, LastRow: 256, FirstColumn: 0, LastColumn: 255}, true},
		{"one column of every row", Range{FirstRow: 0, LastRow: maxRows - 1, FirstColumn: 3, LastColumn: 3}, true},
		{"the whole grid", Range{FirstRow: 0, LastRow: maxRows - 1, FirstColumn: 0, LastColumn: maxColumns - 1}, true},
	} {
		if got := mergeTooLarge(tc.m); got != tc.want {
			t.Errorf("%s: mergeTooLarge = %t, want %t", tc.name, got, tc.want)
		}
	}
}

// TestConvert_ReportsAMergePastItsBound checks that a merge over the whole
// grid prints as its first cell on one page, covering no other cell, and
// that the conversion says so once for the sheet, however many such merges
// the sheet holds.
func TestConvert_ReportsAMergePastItsBound(t *testing.T) {
	t.Parallel()
	whole := Range{FirstRow: 0, LastRow: maxRows - 1, FirstColumn: 0, LastColumn: maxColumns - 1}
	sh := &Sheet{
		Name:   "Data",
		Rows:   []Row{{Index: 0, Cells: []Cell{textCell(0, "merged", nil), textCell(1, "beside", nil)}}},
		Merges: []Range{whole, {FirstRow: 4, LastRow: maxRows - 1, FirstColumn: 0, LastColumn: 0}},
	}
	out := convert(t, newDoc(sh), office.Options{})
	if len(out.Pages) != 1 {
		t.Fatalf("%d pages, want 1", len(out.Pages))
	}
	find(t, out.Pages[0], "merged")
	find(t, out.Pages[0], "beside")
	n := 0
	for _, w := range out.Warnings {
		if w.Code == warn.LimitReached && strings.Contains(w.Message, "merges more than") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d warnings of a merge past its bound, want 1: %+v", n, out.Warnings)
	}
}
