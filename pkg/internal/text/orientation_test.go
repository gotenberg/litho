package text

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-text/typesetting/di"
	"github.com/go-text/typesetting/shaping"
)

// fixtureNotoSansJP is Noto Sans JP among the pinned fixture fonts, whose
// glyphs carry vertical metrics and a vert feature. A test that needs it
// skips when the fixture fonts are absent.
const fixtureNotoSansJP = "../../../test/integration/testdata/fonts/noto-cjk-sans-2.004/NotoSansJP-Regular.otf"

// TestShape_VerticalOrientation sets the characters of a vertical line
// as CSS Writing Modes 4, 5.1 asks: under text-orientation: mixed a
// character whose Vertical_Orientation is U, Tu or Tr (UAX #50) stands
// upright, advancing by its vertical advance, its vertical form when the
// face has one, and centred on the central baseline; a Latin letter,
// whose orientation is R, stays sideways; upright sets every character
// upright.
func TestShape_VerticalOrientation(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(fixtureNotoSansJP)
	if err != nil {
		t.Skipf("fixture fonts absent: %v", err)
	}
	f := newTestFonts(t, fontFile{name: filepath.Base(fixtureNotoSansJP), data: data})
	st := style()
	st.Families = []string{"Noto Sans JP"}
	st.Size = 20
	line := func(o Orientation, s string) ParagraphLine {
		st := st
		st.Orientation = o
		p := shape(t, f, []ParagraphRun{{Text: s, Style: st}}, ParagraphOptions{})
		return p.Line(0, p.Len(), false)
	}
	mixed := line(OrientMixed, "日A、")
	if len(mixed.Spans) != 3 {
		t.Fatalf("%d spans, want 3", len(mixed.Spans))
	}
	for i, want := range []bool{true, false, true} {
		if got := mixed.Spans[i].Text.Upright; got != want {
			t.Errorf("mixed span %d upright %v, want %v", i, got, want)
		}
	}
	ideograph, comma := mixed.Spans[0], mixed.Spans[2]
	if math.Abs(ideograph.Width-20) > 1e-6 {
		t.Errorf("upright ideograph advances %v, want its vertical advance, 20", ideograph.Width)
	}
	g := ideograph.Text.Glyphs[0]
	// The em box of Noto Sans JP spans 880 units above the baseline and
	// 120 below: the central baseline lies 7.6 points above the
	// alphabetic one at 20 points, and the glyph's horizontal origin
	// half its 20-point advance below it, across the line.
	if math.Abs(g.Y-(-7.6+10)) > 1e-6 {
		t.Errorf("upright ideograph across the line at %v, want 2.4", g.Y)
	}
	if hg, mg := line(OrientNone, "、").Spans[0].Text.Glyphs[0].GID, comma.Text.Glyphs[0].GID; hg == mg {
		t.Errorf("upright 、 drawn as glyph %d, its horizontal form", mg)
	}
	if latin, want := mixed.Spans[1].Width, line(OrientNone, "A").Width; latin != want {
		t.Errorf("sideways A advances %v, want its horizontal advance %v", latin, want)
	}
	for i, sp := range line(OrientUpright, "日A").Spans {
		if !sp.Text.Upright {
			t.Errorf("upright: span %d sideways", i)
		}
	}
}

// TestSplitByOrientation keeps a combining mark with its base: an upright
// ideograph's variation selector and a sideways letter's accent stay in
// their base's piece (UAX #50, 4.2).
func TestSplitByOrientation(t *testing.T) {
	t.Parallel()
	text := []rune("葛\U000E0100é日")
	in := shaping.Input{Text: text, RunStart: 0, RunEnd: len(text), Direction: di.DirectionLTR}
	got := splitByOrientation(in, OrientMixed)
	want := []struct {
		start, end int
		vertical   bool
	}{{0, 2, true}, {2, 4, false}, {4, 5, true}}
	if len(got) != len(want) {
		t.Fatalf("%d pieces, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].RunStart != w.start || got[i].RunEnd != w.end || got[i].Direction.IsVertical() != w.vertical {
			t.Errorf("piece %d: [%d, %d) vertical %v, want [%d, %d) %v", i, got[i].RunStart, got[i].RunEnd, got[i].Direction.IsVertical(), w.start, w.end, w.vertical)
		}
	}
}
