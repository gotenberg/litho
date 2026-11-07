package docx

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
)

func TestSetWritingMode_TurnsCellsAndTextBoxes(t *testing.T) {
	t.Parallel()
	for dir, want := range map[string]css.WritingMode{"btLr": css.SidewaysLR, "tbRl": css.VerticalRL, "lrTb": css.HorizontalTB, "": css.HorizontalTB} {
		st := css.Initial()
		setWritingMode(st, dir)
		if st.WritingMode != want {
			t.Errorf("%q: writing mode %v, want %v", dir, st.WritingMode, want)
		}
	}
	body := `<w:tbl><w:tr><w:tc><w:tcPr><w:textDirection w:val="btLr"/></w:tcPr><w:p><w:r><w:t>up</w:t></w:r></w:p></w:tc>` +
		`<w:tc><w:tcPr><w:textDirection w:val="tbRlV"/></w:tcPr><w:p><w:r><w:t>down</w:t></w:r></w:p></w:tc>` +
		`<w:tc><w:p><w:r><w:t>flat</w:t></w:r></w:p></w:tc></w:tr></w:tbl>`
	b := build(t, body, nil)
	cells := role(b.root, "TD")
	if len(cells) != 3 {
		t.Fatalf("%d cells, want 3", len(cells))
	}
	for i, want := range []css.WritingMode{css.SidewaysLR, css.VerticalRL, css.HorizontalTB} {
		if cells[i].Style.WritingMode != want {
			t.Errorf("cell %d writing mode %v, want %v", i, cells[i].Style.WritingMode, want)
		}
	}
	n := &layout.Node{Kind: layout.Element, Style: css.Initial()}
	turn(n, "btLr")
	turn(nil, "btLr")
	if n.Style.WritingMode != css.SidewaysLR {
		t.Errorf("text box writing mode %v, want sideways-lr", n.Style.WritingMode)
	}
}
