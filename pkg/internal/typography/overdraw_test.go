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
		{"shadow pair keeps the copy drawn last", cat(overdrawText(1, "Purpose", 101.2, 498.8), overdrawText(2, "Purpose", 100, 500)), "Purpose"},
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
		{"a copy moved glyph by glyph is no copy", cat(overdrawText(1, "ab", 100, 500), []models.TextElement{
			{Text: "a", X: 100.5, Y: 500, Width: 6, FontSize: 12, Chunk: 2},
			{Text: "b", X: 108, Y: 500, Width: 6, FontSize: 12, Chunk: 2},
		}), "abab"},
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

// TestDropOverdrawnKeepsTheLastCopy checks the copy kept is the one drawn
// last, which lies on top.
func TestDropOverdrawnKeepsTheLastCopy(t *testing.T) {
	t.Parallel()
	els := append(overdrawText(1, "Hi", 101, 499), overdrawText(2, "Hi", 100, 500)...)
	got, _, err := dropOverdrawn(context.Background(), els, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Chunk != 2 {
		t.Errorf("kept %+v, want chunk 2", got)
	}
}
