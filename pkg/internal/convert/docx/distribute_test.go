package docx

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
)

// TestDistribute_JustifiesEveryLineAndSpreadsALoneWord locks how a
// paragraph of w:jc distribute is laid out (ECMA-376 Part 1, 17.18.44):
// every line justified, the last one too, at its spaces, and a line that
// holds none between its characters.
//
// # Corpus evidence
//
//   - openxmlsdk/docx: format-paragrapsimple's "Distributed" alone on its
//     line prints its letters spread across the column, 40.5 points apart.
//   - litho/docx: para-alignment's distributed paragraph prints its last
//     line justified at its spaces, its words at their own widths.
func TestDistribute_JustifiesEveryLineAndSpreadsALoneWord(t *testing.T) {
	b := build(t, `<w:p><w:pPr><w:jc w:val="distribute"/></w:pPr><w:r><w:t>Distributed</w:t></w:r></w:p>`, nil)
	paras := find(b.root, func(n *layout.Node) bool {
		return n.Style != nil && n.Style.Display == css.Block && n.Style.TextAlignLast == css.AlignJustify
	})
	if len(paras) != 1 {
		t.Fatalf("%d distributed paragraphs, want 1", len(paras))
	}
	if st := paras[0].Style; st.TextAlign != css.AlignJustify || st.TextJustify != css.JustifyWordsElseCharacters {
		t.Errorf("text-align %v, text-justify %v; want justify and words else characters", st.TextAlign, st.TextJustify)
	}
}
