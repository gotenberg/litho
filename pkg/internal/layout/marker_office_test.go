package layout

import (
	"image/color"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// TestMarker_OfficeBox lays a marker whose style has a width out as a box
// of that width outside the item at its start edge, the label aligned on
// the box's start edge, as Word and Writer put a list number on the
// number position and the text at the indent.
//
// # Corpus evidence
//
//   - LibreOffice 9.0.0 image: a .fodt list in label-alignment mode,
//     margin 1in, text indent -0.5in, a list tab at 1in: the label "1."
//     starts at 0.5in when aligned start, ends there when aligned end and
//     centres on it when centred; the text starts at 1in on every line.
func TestMarker_OfficeBox(t *testing.T) {
	tests := []struct {
		name  string
		align css.TextAlign
		rtl   bool
		width float64
		want  string
	}{
		{"start", css.AlignStart, false, 30, "1@20,8 .@30,8 a@50,8"},
		{"left", css.AlignLeft, false, 30, "1@20,8 .@30,8 a@50,8"},
		{"right", css.AlignRight, false, 30, "1@0,8 .@10,8 a@50,8"},
		{"end", css.AlignEnd, false, 30, "1@0,8 .@10,8 a@50,8"},
		{"centre", css.AlignCenter, false, 30, "1@10,8 .@20,8 a@50,8"},
		{"label wider than the box", css.AlignStart, false, 10, "1@40,8 .@50,8 a@50,8"},
		{"right to left start", css.AlignStart, true, 30, ".@160,8 1@170,8 a@140,8"},
		{"right to left end", css.AlignEnd, true, 30, ".@180,8 1@190,8 a@140,8"},
		{"right to left left", css.AlignLeft, true, 30, ".@180,8 1@190,8 a@140,8"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := func(s *css.Style) {
				if tt.rtl {
					s.Direction = css.RTL
				}
			}
			item := sty(display(css.ListItem), dir, margin(0, 50, 0, 50), func(s *css.Style) { s.ListStylePosition = css.Outside })
			mst := sty(dir, func(s *css.Style) { s.Width = css.Pts(px(tt.width)); s.TextAlign = tt.align; s.Display = css.Inline })
			li := el(item, txt(item, "a"))
			li.Marker = el(mst, txt(mst, "1."))
			res := run(t, doc(li), page(200, 50))
			if got := glyphLayout(res, 1); got != tt.want {
				t.Errorf("glyphs %s, want %s", got, tt.want)
			}
		})
	}
}

// TestMarker_OfficeBoxPicture aligns a picture label on the marker box's
// start edge.
func TestMarker_OfficeBoxPicture(t *testing.T) {
	white := css.Color{R: 1, G: 1, B: 1, A: 1}
	item := sty(display(css.ListItem), margin(0, 0, 0, 8), fg(white), func(s *css.Style) { s.ListStylePosition = css.Outside; s.FontSize = px(2) })
	mst := sty(func(s *css.Style) { s.Width = css.Pts(px(6)); s.TextAlign = css.AlignCenter })
	li := el(item, txt(item, "a"))
	li.Marker = el(mst, &Node{Kind: Replaced, Style: mst, Picture: pic(color.NRGBA{B: 255, A: 255}, 2, 2)})
	res := run(t, doc(li), page(10, 2))
	assertPixels(t, res, 1, `
		_BB_______
		_BB_______`)
}

// TestMarker_CSSOutsideUnchanged keeps the CSS outside marker, its end
// edge on the content edge, when the marker style sets no width.
func TestMarker_CSSOutsideUnchanged(t *testing.T) {
	item := sty(display(css.ListItem), margin(0, 0, 0, 50), func(s *css.Style) { s.ListStylePosition = css.Outside })
	mst := sty(func(s *css.Style) { s.Display = css.Inline })
	li := el(item, txt(item, "a"))
	li.Marker = el(mst, txt(mst, "1."))
	res := run(t, doc(li), page(200, 50))
	if got, want := glyphLayout(res, 1), "1@30,8 .@40,8 a@50,8"; got != want {
		t.Errorf("glyphs %s, want %s", got, want)
	}
}
