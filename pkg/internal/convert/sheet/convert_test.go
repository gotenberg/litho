package sheet

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
)

// TestConvert_CellPlacement locks where Calc puts cell text: left and
// right a point inside the cell, the baseline a descent and a point above
// a bottom-aligned cell's bottom, a long text over its empty neighbours,
// clipped at the first cell with content.
//
// # Corpus evidence
//
//   - probe, ods: a flat ODS on A4 with half-inch margins, four columns of 80
//     points, Liberation Sans 10, converted by Gotenberg 9.0.0: "Hello"
//     at x 36.99 baseline 796.20, "123" ending at 194.96, the overflowing
//     text from 36.99 to 187.58 on the second row (783.41), the
//     right-aligned long text in the third column from 176.00 to 274.94,
//     the centred one from 95.67 to 216.28, the rows 12.78 points apart
//     (this test leaves out the source's wrapped row, so its last two rows
//     sit one and two rows higher).
func TestConvert_CellPlacement(t *testing.T) {
	right := &Style{Align: Alignment{Horizontal: HRight}}
	center := &Style{Align: Alignment{Horizontal: HCenter}}
	sh := &Sheet{Name: "S1", DefaultColumnWidth: 80, Rows: []Row{
		{Index: 0, Cells: []Cell{textCell(0, "Hello", nil), numCell(1, 123, nil), textCell(3, "x", nil)}},
		{Index: 1, Cells: []Cell{textCell(0, "A long text overflowing to the right", nil)}},
		{Index: 2, Cells: []Cell{textCell(0, "A long text blocked by B", nil), textCell(1, "B", nil)}},
		{Index: 3, Cells: []Cell{textCell(2, "Right aligned long text", right)}},
		{Index: 4, Cells: []Cell{textCell(1, "Centred long text over both", center)}},
	}}
	doc := convert(t, newDoc(sh), office.Options{})
	if len(doc.Pages) != 1 {
		t.Fatalf("%d pages, want 1", len(doc.Pages))
	}
	pg := doc.Pages[0]
	tests := []struct {
		text       string
		x, endX, y float64
	}{
		{"Hello", 36.99, 59.79, 796.20},
		{"123", 178.27, 194.96, 796.20},
		{"A long text overflowing to the right", 36.99, 187.58, 783.41},
		{"Right aligned long text", 176.00, 274.94, 757.85},
		{"Centred long text over both", 95.67, 216.28, 745.07},
	}
	for _, tt := range tests {
		r := find(t, pg, tt.text)
		if !near(r.x, tt.x, 0.6) || !near(r.endX, tt.endX, 0.6) || !near(r.y, tt.y, 0.6) {
			t.Errorf("%q at x %.2f..%.2f y %.2f, want %.2f..%.2f y %.2f\n%s", tt.text, r.x, r.endX, r.y, tt.x, tt.endX, tt.y, dump(pg))
		}
	}
}

// TestConvert_Deterministic locks that a document converts to the same
// bytes twice, and that its structure is a Table of rows of cells.
func TestConvert_Deterministic(t *testing.T) {
	write := func() []byte {
		sh := grid150(a4())
		sh.Page.GridLines = true
		sh.Merges = []Range{{FirstRow: 0, LastRow: 1, FirstColumn: 0, LastColumn: 1}}
		out := convert(t, newDoc(sh), office.Options{})
		var buf bytes.Buffer
		if err := compose.Write(context.Background(), out, &buf, compose.Options{}); err != nil {
			t.Fatalf("Write: %v", err)
		}
		if len(out.Structure.Kids) == 0 || out.Structure.Kids[0].Role != "Table" || out.Structure.Kids[0].Kids[0].Role != "TR" {
			t.Errorf("structure %+v", out.Structure.Kids)
		}
		return buf.Bytes()
	}
	if !bytes.Equal(write(), write()) {
		t.Error("two conversions differ")
	}
}

// cancelSink cancels the conversion's context as the page count is
// announced, before the first page is laid out.
type cancelSink struct{ cancel context.CancelFunc }

func (s cancelSink) Expect(int) { s.cancel() }

func (cancelSink) Page(context.Context, *compose.Page) error { return nil }

// TestConvert_BlankBookFails checks that the page a book without a printed
// cell prints fails the conversion when it cannot be laid out, here for a
// cancelled context, rather than printing a blank page in its place.
func TestConvert_BlankBookFails(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	doc := newDoc(&Sheet{Name: "Empty"})
	_, err := Convert(ctx, doc, pinnedFonts(t), office.Options{Sink: cancelSink{cancel}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
