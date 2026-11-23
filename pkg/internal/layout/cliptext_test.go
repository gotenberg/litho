package layout

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

func clipText(s *css.Style) { s.BackgroundClip = css.TextEdge }

func transparent(s *css.Style) { s.Color = css.Color{} }

// rowsOf returns n rows of the pattern line, then m blank rows of its
// width.
func rowsOf(line string, n, m int) string {
	rows := strings.Repeat(line+"\n", n) + strings.Repeat(strings.Repeat("_", len(line))+"\n", m)
	return strings.TrimSuffix(rows, "\n")
}

func TestBackgroundClip_Text(t *testing.T) {
	// The background paints under the glyphs alone, Ahem's squares here,
	// of the box, its inline blocks and its relatively positioned boxes
	// (CSS Backgrounds 4, 2.4).
	ib := sty(display(css.InlineBlock), transparent)
	rel := inlineSty(transparent, func(s *css.Style) {
		s.Position = css.Relative
		s.Left = css.Pts(px(-10))
	})
	block := func(kids ...*Node) *Node {
		return doc(el(sty(width(30), bg(lime), clipText, transparent), kids...))
	}
	both := rowsOf("GGGGGGGGGG__________GGGGGGGGGG", 10, 10)
	tests := []struct {
		name string
		root *Node
		want string
	}{
		{"block", block(txt(sty(transparent), "X X")), both},
		{"inline block", block(txt(sty(transparent), "X "), el(ib, txt(ib, "X"))), both},
		{
			"relative", block(txt(sty(transparent), "X "), el(rel, txt(rel, "X"))),
			rowsOf("GGGGGGGGGGGGGGGGGGGG__________", 10, 10),
		},
		{"no text", doc(el(sty(width(30), height(10), bg(lime), clipText))), rowsOf(strings.Repeat("_", 30), 20, 0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertPixels(t, run(t, tt.root, page(30, 20)), 1, tt.want)
		})
	}
}

// TestBackgroundClip_TextOnATableRow locks that the background of a table
// row under background-clip: text paints under the glyphs of its cells
// alone (CSS Backgrounds 4, 2.1), not over the cells' rectangles.
func TestBackgroundClip_TextOnATableRow(t *testing.T) {
	root := doc(table(nil, tr(mods(bg(lime), clipText), td(mods(transparent), "X X"))))
	assertPixels(t, run(t, root, page(30, 20)), 1, rowsOf("GGGGGGGGGG__________GGGGGGGGGG", 10, 10))
}

// TestText_TransparentStaysText locks that under BrowserRules text in a
// transparent colour, a heading whose background clips to its glyphs
// among others, paints nothing and is still its element's text: written
// invisible (ISO 32000-1, 9.3.6, text rendering mode 3) and tagged (ISO
// 14289-1, 7.1), as Chromium writes it. Office rules write none.
//
// # Corpus evidence
//
//   - litho/html borders-cards: two headings of background-clip: text and
//     a transparent colour lost their text, the first its H1, which left
//     its first heading of level 2 and failed PDF/UA-1, 7.4.2; Chromium's
//     PDF holds both texts.
func TestText_TransparentStaysText(t *testing.T) {
	st := sty(transparent, bg(lime), clipText)
	for _, browser := range []bool{true, false} {
		res := runWith(t, doc(role("H1", el(st, txt(st, "X")))), Options{Page: page(30, 20), BrowserRules: browser})
		var texts []*typeset.Text
		var walk func([]typeset.Fragment)
		walk = func(fs []typeset.Fragment) {
			for _, f := range fs {
				switch v := f.(type) {
				case *typeset.Text:
					texts = append(texts, v)
				case *typeset.Group:
					walk(v.Content)
				}
			}
		}
		walk(res.Pages[0].Content)
		if !browser {
			if len(texts) != 0 {
				t.Errorf("Office rules wrote %d text runs, want none", len(texts))
			}
			continue
		}
		assertPixels(t, res, 1, rowsOf("GGGGGGGGGG____________________", 10, 10))
		if len(texts) != 1 || !texts[0].Invisible || texts[0].Parent == nil || texts[0].Parent.Role != "H1" {
			t.Fatalf("texts %+v, want the heading's text, invisible and tagged", texts)
		}
	}
}
