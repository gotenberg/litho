package odp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/convert/slides"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// TestRead_SpanPastThirtyTwoBits pins that a cell spanning past 2^31 columns
// spans to the end of its grid where int is 32 bits and where it is 64, and
// that the table lays out: the span is capped before the slide adds it to
// the cell's column, a sum that wraps where int is 32 bits.
func TestRead_SpanPastThirtyTwoBits(t *testing.T) {
	t.Parallel()

	doc, _ := read(t, flat{body: page(`<draw:frame svg:width="8cm" svg:height="3cm"><table:table>` +
		`<table:table-column table:number-columns-repeated="3"/>` +
		`<table:table-row><table:table-cell/><table:table-cell table:number-columns-spanned="3000000000"/><table:covered-table-cell/></table:table-row>` +
		`</table:table></draw:frame>`)}.bytes())
	if len(doc.Slides) != 1 || len(doc.Slides[0].Shapes) != 1 || doc.Slides[0].Shapes[0].Table == nil {
		t.Fatalf("slides = %+v, want one table", doc.Slides)
	}
	if got := doc.Slides[0].Shapes[0].Table.Rows[0].Cells[1].ColSpan; got != maxTableCells {
		t.Errorf("ColSpan = %d, want %d", got, maxTableCells)
	}
	dir := filepath.Join("..", "..", "..", "..", "test", "integration", "testdata", "fonts", "liberation-2.1.5")
	if _, err := os.Stat(dir); err != nil {
		t.Skip("fixture fonts not fetched (make corpus-fetch CORPUS=fonts)")
	}
	fonts, err := text.NewFonts(context.Background(), nil, []string{dir})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := slides.Convert(context.Background(), doc, fonts, office.Options{}); err != nil {
		t.Fatalf("Convert: %v", err)
	}
}

// TestRead_ZIndexPastThirtyTwoBits pins that two shapes whose draw:z-index
// passes 2^31 stack by their indices where int is 32 bits and where it is
// 64: ODF 1.3, Part 3, 19.231 types the index as an unbounded
// nonNegativeInteger, which saturating would tie.
func TestRead_ZIndexPastThirtyTwoBits(t *testing.T) {
	t.Parallel()

	doc, _ := read(t, flat{body: page(
		`<draw:rect draw:name="upper" draw:z-index="4000000000" svg:width="1cm" svg:height="1cm"/>` +
			`<draw:rect draw:name="lower" draw:z-index="3000000000" svg:width="1cm" svg:height="1cm"/>`)}.bytes())
	var names []string
	for _, s := range doc.Slides[0].Shapes {
		names = append(names, s.Name)
	}
	if len(names) != 2 || names[0] != "lower" || names[1] != "upper" {
		t.Errorf("stacking order = %q, want lower then upper", names)
	}
}
