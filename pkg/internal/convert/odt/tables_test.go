package odt

import (
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/layout"
)

// TestCell_ClipsANestedTable clips the content of a cell that holds a
// table, which Writer lays out at its own width and cuts at the cell's
// edges, and leaves a cell of paragraphs alone.
func TestCell_ClipsANestedTable(t *testing.T) {
	inner := `<table:table><table:table-column/><table:table-row><table:table-cell><text:p>in</text:p></table:table-cell></table:table-row></table:table>`
	body := `<table:table><table:table-column table:number-columns-repeated="2"/><table:table-row>` +
		`<table:table-cell><text:p>a</text:p>` + inner + `</table:table-cell><table:table-cell><text:p>b</text:p></table:table-cell></table:table-row></table:table>`
	b := newTestBuilder(t, doc{body: body})
	root, _ := b.document(odfBody(b))
	var clips []bool
	walkNodes(root, func(n *layout.Node) {
		if n.Role == "TD" {
			clips = append(clips, n.Style.ClipOnly())
		}
	})
	if want := []bool{true, false, false}; !slices.Equal(clips, want) {
		t.Errorf("cells clip %v, want %v", clips, want)
	}
}
