package typography

import (
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// overdrawText lays text out as one run in chunk at (x, y), each glyph 6
// points wide at size 12.
func overdrawText(chunk int, text string, x, y float64) []models.TextElement {
	var els []models.TextElement
	for i, r := range text {
		els = append(els, models.TextElement{Text: string(r), X: x + 6*float64(i), Y: y, Width: 6, FontSize: 12, Chunk: chunk})
	}
	return els
}

func overdrawJoin(els []models.TextElement) string {
	var b strings.Builder
	for _, el := range els {
		b.WriteString(el.Text)
	}
	return b.String()
}

func TestDropOverdrawn(t *testing.T) {
	t.Parallel()
	cat := func(parts ...[]models.TextElement) []models.TextElement {
		var out []models.TextElement
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}
	cases := []struct {
		name string
		els  []models.TextElement
		want string
	}{
		{"shadow pair read once", cat(overdrawText(1, "Purpose", 101.2, 498.8), overdrawText(2, "Purpose", 100, 500)), "Purpose"},
		{"fake bold drawn three times", cat(overdrawText(1, "Bold", 100, 500), overdrawText(2, "Bold", 100.3, 500), overdrawText(3, "Bold", 100.6, 500)), "Bold"},
		{"table cells a column apart", cat(overdrawText(1, "0", 100, 500), overdrawText(2, "0", 160, 500), overdrawText(3, "0", 100, 486)), "000"},
		// Helvetica's l advances 0.222 em, under the quarter em bound.
		{"one glyph a run, an advance apart", []models.TextElement{
			{Text: "l", X: 100, Y: 500, Width: 2.664, FontSize: 12, Chunk: 1},
			{Text: "l", X: 102.664, Y: 500, Width: 2.664, FontSize: 12, Chunk: 2},
		}, "ll"},
		{"a word repeated on its line", cat(overdrawText(1, "the", 100, 500), overdrawText(2, "the", 124, 500)), "thethe"},
		{"same words at another size", cat(overdrawText(1, "Title", 100, 500), func() []models.TextElement {
			els := overdrawText(2, "Title", 100.5, 500)
			for i := range els {
				els[i].FontSize = 14
			}
			return els
		}()), "TitleTitle"},
		{"other characters overlapping", cat(overdrawText(1, "ab", 100, 500), overdrawText(2, "cd", 100.5, 500)), "abcd"},
		{"copies cut into chains at other places", cat(
			overdrawText(1, "On", 100, 500), overdrawText(2, "e-", 112, 500), overdrawText(3, "Stop", 124, 500),
			overdrawText(4, "One-S", 100.4, 500.4), overdrawText(5, "top", 130.4, 500.4),
		), "One-Stop"},
		{"another text over it sharing a letter", cat(overdrawText(1, "sect", 100, 500), overdrawText(2, "xyzt", 100.5, 500.5)), "sectxyzt"},
		{"another text over it sharing two letters", cat(overdrawText(1, "rence", 100, 500), overdrawText(2, "mendm", 106.2, 500.2)), "rencemendm"},
		{"each glyph drawn three times in a row", []models.TextElement{
			{Text: "T", X: 100, Y: 500, Width: 6, FontSize: 12, Chunk: 1},
			{Text: "T", X: 100.3, Y: 500, Width: 6, FontSize: 12, Chunk: 1},
			{Text: "T", X: 100.6, Y: 500, Width: 6, FontSize: 12, Chunk: 1},
			{Text: "o", X: 106, Y: 500, Width: 6, FontSize: 12, Chunk: 1},
			{Text: "o", X: 106.3, Y: 500, Width: 6, FontSize: 12, Chunk: 1},
			{Text: "o", X: 106.6, Y: 500, Width: 6, FontSize: 12, Chunk: 1},
		}, "To"},
		{"a lone bullet drawn twice", cat(overdrawText(1, "•", 100, 500), overdrawText(2, "•", 101, 499), overdrawText(3, "Item", 300, 500)), "•Item"},
		{"a doubled letter with a space drawn between", []models.TextElement{
			{Text: "o", X: 101.24, Y: 132, Width: 4.92, FontSize: 10.12, Chunk: 1},
			{Text: " ", X: 105.39, Y: 132, Width: 2.53, FontSize: 10.12, Chunk: 1},
			{Text: "l", X: 107.15, Y: 132, Width: 2.5, FontSize: 10.12, Chunk: 1},
			{Text: " ", X: 108.88, Y: 132, Width: 2.53, FontSize: 10.12, Chunk: 1},
			{Text: "l", X: 109.2, Y: 132, Width: 2.5, FontSize: 10.12, Chunk: 1},
			{Text: " ", X: 110.9, Y: 132, Width: 2.53, FontSize: 10.12, Chunk: 1},
		}, "o l l "},
		{"glyphs that advance nothing keep a doubled letter", []models.TextElement{
			{Text: "p", X: 50, Y: 690, FontSize: 14, Chunk: 1},
			{Text: "p", X: 50, Y: 690, FontSize: 14, Chunk: 1},
		}, "pp"},
		{"a copy whose spacing drifts goes whole", cat(overdrawText(1, "Health", 100, 500), []models.TextElement{
			{Text: "H", X: 98.6, Y: 501.9, Width: 6, FontSize: 12, Chunk: 2},
			{Text: "e", X: 104.6, Y: 501.9, Width: 6, FontSize: 12, Chunk: 2},
			{Text: "a", X: 110.6, Y: 501.9, Width: 6, FontSize: 12, Chunk: 2},
			{Text: "l", X: 117.2, Y: 501.9, Width: 6, FontSize: 12, Chunk: 3},
			{Text: "t", X: 123.3, Y: 501.9, Width: 6, FontSize: 12, Chunk: 3},
			{Text: "h", X: 129.5, Y: 501.9, Width: 6, FontSize: 12, Chunk: 3},
		}), "Health"},
		{"a column of the same figure", cat(overdrawText(1, "1", 100, 500), overdrawText(2, "1", 100, 491), overdrawText(3, "1", 100, 482)), "111"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, _, err := dropOverdrawn(context.Background(), c.els, nil)
			if err != nil {
				t.Fatal(err)
			}
			if s := overdrawJoin(got); s != c.want {
				t.Errorf("dropOverdrawn read %q, want %q", s, c.want)
			}
		})
	}
}

// TestDropOverdrawnKeepsTheFirstCopy checks the copy kept is the one drawn
// first, where the content stream first draws the text, and that the fonts
// parallel to the elements follow them.
func TestDropOverdrawnKeepsTheFirstCopy(t *testing.T) {
	t.Parallel()
	els := append(overdrawText(1, "Hi", 101, 499), overdrawText(2, "Hi", 100, 500)...)
	fonts := []runFont{{name: "a"}, {name: "a"}, {name: "b"}, {name: "b"}}
	got, gotFonts, err := dropOverdrawn(context.Background(), els, fonts)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Chunk != 1 || got[1].Chunk != 1 {
		t.Errorf("kept %+v, want chunk 1", got)
	}
	if len(gotFonts) != 2 || gotFonts[0].name != "a" || gotFonts[1].name != "a" {
		t.Errorf("kept fonts %+v, want those of chunk 1", gotFonts)
	}
}

// TestDropOverdrawnBoundsACell sets thousands of one character at one place,
// each at another angle, so none matches another: the index bound keeps the
// work linear and every glyph is kept.
func TestDropOverdrawnBoundsACell(t *testing.T) {
	t.Parallel()
	const n = 20000
	els := make([]models.TextElement, n)
	for i := range els {
		els[i] = models.TextElement{Text: "a", X: 100, Y: 500, Width: 6, FontSize: 12, Angle: float64(i) * 0.1, Chunk: i}
	}
	got, _, err := dropOverdrawn(context.Background(), els, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < n-n/10 {
		t.Errorf("kept %d of %d glyphs set at distinct angles", len(got), n)
	}
}
