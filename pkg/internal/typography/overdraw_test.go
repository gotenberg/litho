package typography

import (
	"context"
	"math"
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
// each advancing nothing, so none matches another: the index bound keeps the
// work linear and every glyph is kept.
func TestDropOverdrawnBoundsACell(t *testing.T) {
	t.Parallel()
	const n = 20000
	els := make([]models.TextElement, n)
	for i := range els {
		els[i] = models.TextElement{Text: "a", X: 100, Y: 500, FontSize: 12, Chunk: i}
	}
	got, _, err := dropOverdrawn(context.Background(), els, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < n-n/10 {
		t.Errorf("kept %d of %d glyphs that advance nothing", len(got), n)
	}
}

// TestOverdrawCopies names, for each glyph a copy draws again, the glyph of
// the earlier copy it lies on, and none for the first copy and for text
// drawn once.
func TestOverdrawCopies(t *testing.T) {
	t.Parallel()
	var els []models.TextElement
	els = append(els, overdrawText(1, "Bold", 100, 500)...)
	els = append(els, overdrawText(2, "Once", 100, 400)...)
	els = append(els, overdrawText(3, "Bold", 100.3, 500)...)
	got, err := OverdrawCopies(context.Background(), els)
	if err != nil {
		t.Fatal(err)
	}
	want := []int{-1, -1, -1, -1, -1, -1, -1, -1, 0, 1, 2, 3}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	single, err := OverdrawCopies(context.Background(), overdrawText(1, "A", 0, 0))
	if err != nil || len(single) != 1 || single[0] != -1 {
		t.Errorf("one glyph: %v, %v; want [-1]", single, err)
	}
}

// TestDropOverdrawnCurvedCopies reads once text set glyph by glyph along a
// curve and drawn again along a second curve a little apart, each glyph
// turned from its copy by up to a fifth of a radian (govdocs
// thread_400_400902.pdf, "Calcine storage in bin sets" and its shadow), and
// keeps apart the glyphs a reader tells apart on a curve or across a turn.
func TestDropOverdrawnCurvedCopies(t *testing.T) {
	t.Parallel()
	curved := []models.TextElement{
		{Text: "s", X: 140.361, Y: 114.218, Angle: 3.1416, Width: 4.680, FontSize: 9, Chunk: 92},
		{Text: "t", X: 135.686, Y: 114.218, Angle: 3.1416, Width: 3.942, FontSize: 9, Chunk: 92},
		{Text: "o", X: 131.802, Y: 114.218, Angle: 3.1416, Width: 4.617, FontSize: 9, Chunk: 92},
		{Text: "r", X: 127.342, Y: 114.138, Angle: 3.0013, Width: 3.258, FontSize: 9, Chunk: 93},
		{Text: "a", X: 124.615, Y: 114.403, Angle: 2.8073, Width: 4.653, FontSize: 9, Chunk: 94},
		{Text: "g", X: 120.533, Y: 115.776, Angle: 2.5574, Width: 4.554, FontSize: 9, Chunk: 95},
		{Text: "e", X: 117.012, Y: 118.093, Angle: 2.3209, Width: 4.086, FontSize: 9, Chunk: 96},
		{Text: "i", X: 113.335, Y: 122.588, Angle: 2.0658, Width: 1.980, FontSize: 9, Chunk: 97},
		{Text: "n", X: 112.421, Y: 124.073, Angle: 1.9310, Width: 4.581, FontSize: 9, Chunk: 98},
		{Text: "b", X: 110.390, Y: 130.180, Angle: 1.7169, Width: 4.437, FontSize: 9, Chunk: 99},
		{Text: "i", X: 109.808, Y: 134.453, Angle: 1.6399, Width: 1.980, FontSize: 9, Chunk: 100},
		{Text: "n", X: 109.712, Y: 136.381, Angle: 1.5878, Width: 4.581, FontSize: 9, Chunk: 101},
		{Text: "s", X: 109.602, Y: 143.210, Angle: 1.5815, Width: 4.680, FontSize: 9, Chunk: 102},
		{Text: "e", X: 109.552, Y: 147.881, Angle: 1.5760, Width: 4.086, FontSize: 9, Chunk: 103},
		{Text: "t", X: 109.530, Y: 151.908, Angle: 1.5685, Width: 3.942, FontSize: 9, Chunk: 104},
		{Text: "s", X: 109.531, Y: 155.827, Angle: 1.5526, Width: 4.680, FontSize: 9, Chunk: 105},
		{Text: "s", X: 140.735, Y: 113.983, Angle: 3.1416, Width: 4.680, FontSize: 9, Chunk: 109},
		{Text: "t", X: 136.060, Y: 113.983, Angle: 3.1416, Width: 3.942, FontSize: 9, Chunk: 109},
		{Text: "o", X: 132.158, Y: 113.983, Angle: 3.1416, Width: 4.617, FontSize: 9, Chunk: 109},
		{Text: "r", X: 127.723, Y: 113.911, Angle: 2.9809, Width: 3.258, FontSize: 9, Chunk: 110},
		{Text: "a", X: 125.001, Y: 114.238, Angle: 2.7917, Width: 4.653, FontSize: 9, Chunk: 111},
		{Text: "g", X: 120.928, Y: 115.685, Angle: 2.5550, Width: 4.554, FontSize: 9, Chunk: 112},
		{Text: "e", X: 117.399, Y: 118.022, Angle: 2.3313, Width: 4.086, FontSize: 9, Chunk: 113},
		{Text: "i", X: 113.648, Y: 122.487, Angle: 2.0853, Width: 1.980, FontSize: 9, Chunk: 114},
		{Text: "n", X: 112.705, Y: 123.958, Angle: 1.9525, Width: 4.581, FontSize: 9, Chunk: 115},
		{Text: "b", X: 110.543, Y: 130.015, Angle: 1.7363, Width: 4.437, FontSize: 9, Chunk: 116},
		{Text: "i", X: 109.882, Y: 134.259, Angle: 1.6572, Width: 1.980, FontSize: 9, Chunk: 117},
		{Text: "n", X: 109.698, Y: 136.207, Angle: 1.6205, Width: 4.581, FontSize: 9, Chunk: 118},
		{Text: "s", X: 109.417, Y: 142.944, Angle: 1.5642, Width: 4.680, FontSize: 9, Chunk: 119},
		{Text: "e", X: 109.447, Y: 147.544, Angle: 1.5166, Width: 4.086, FontSize: 9, Chunk: 120},
		{Text: "t", X: 109.653, Y: 151.523, Angle: 1.4523, Width: 3.942, FontSize: 9, Chunk: 121},
		{Text: "s", X: 110.045, Y: 155.326, Angle: 1.3296, Width: 4.680, FontSize: 9, Chunk: 122},
	}
	turned := func(els []models.TextElement, angle float64) []models.TextElement {
		for i := range els {
			els[i].Angle = angle
		}
		return els
	}
	cases := []struct {
		name string
		els  []models.TextElement
		want string
	}{
		{"a curved shadow read once", curved, "storageinbinsets"},
		{"copies on each side of a half turn", append(turned(overdrawText(1, "Bold", 100, 500), math.Pi-0.002), turned(overdrawText(2, "Bold", 100.3, 500), -math.Pi+0.002)...), "Bold"},
		{"a doubled narrow letter on a curve", []models.TextElement{
			{Text: "l", X: 100, Y: 500, Width: 2.664, FontSize: 12, Chunk: 1},
			{Text: "l", X: 102.664, Y: 500.13, Angle: 0.1, Width: 2.664, FontSize: 12, Chunk: 2},
		}, "ll"},
		{"one figure on two axes a quarter turn apart", []models.TextElement{
			{Text: "0", X: 100, Y: 500, Width: 6, FontSize: 12, Chunk: 1},
			{Text: "0", X: 100.5, Y: 500.5, Angle: math.Pi / 2, Width: 6, FontSize: 12, Chunk: 2},
		}, "00"},
		{"a wide glyph turned past a quarter em at its far end", []models.TextElement{
			{Text: "W", X: 100, Y: 500, Width: 36, FontSize: 12, Chunk: 1},
			{Text: "W", X: 100.5, Y: 500, Angle: 0.15, Width: 36, FontSize: 12, Chunk: 2},
		}, "WW"},
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
