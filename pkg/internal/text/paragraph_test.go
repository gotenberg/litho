package text

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
)

// shape shapes runs, failing the test on error.
func shape(t *testing.T, f *Fonts, runs []ParagraphRun, opts ParagraphOptions) *Paragraph {
	t.Helper()
	p, _, err := f.Shape(context.Background(), runs, opts)
	if err != nil {
		t.Fatalf("Shape: %v", err)
	}
	return p
}

// spanText returns the characters the glyphs of a line stand for, in
// visual order.
func spanText(l ParagraphLine) string {
	var b strings.Builder
	for _, sp := range l.Spans {
		if sp.Text == nil {
			b.WriteString("[obj]")
			continue
		}
		for _, g := range sp.Text.Glyphs {
			b.WriteString(g.Text)
		}
	}
	return b.String()
}

func TestShape_AdvancesAndBreaks(t *testing.T) {
	f := newTestFonts(t, sansFamily()...)
	p := shape(t, f, []ParagraphRun{{Text: "AB CA", Style: style()}}, ParagraphOptions{})
	if p.Len() != 5 || p.Direction() != LeftToRight {
		t.Fatalf("len %d direction %d", p.Len(), p.Direction())
	}
	want := []float64{5, 6, 2.5, 7, 5}
	for i, w := range want {
		if math.Abs(p.Advance(i)-w) > 1e-6 {
			t.Errorf("advance %d = %v, want %v", i, p.Advance(i), w)
		}
	}
	for i := range p.Len() {
		wantBreak := NoBreak
		if i == 3 {
			wantBreak = BreakAllowed
		}
		if p.Break(i) != wantBreak {
			t.Errorf("break before %d = %d, want %d", i, p.Break(i), wantBreak)
		}
	}
	if got := p.MaxContent(); math.Abs(got-25.5) > 1e-6 {
		t.Errorf("max-content %v, want 25.5", got)
	}
	if got := p.MinContent(); math.Abs(got-12) > 1e-6 {
		t.Errorf("min-content %v, want 12", got)
	}
	l := p.Line(0, 3, true)
	if spanText(l) != "AB " || math.Abs(l.Width-11) > 1e-6 {
		t.Errorf("line %q width %v, want \"AB \" of 11 with the space hanging", spanText(l), l.Width)
	}
}

func TestShape_ObjectsAndRuns(t *testing.T) {
	f := newTestFonts(t, sansFamily()...)
	bold := style()
	bold.Weight = 700
	runs := []ParagraphRun{
		{Text: "A", Style: style()},
		{Object: &Object{Width: 4, Ascent: 9, Descent: 1}},
		{Text: "B", Style: bold},
	}
	p := shape(t, f, runs, ParagraphOptions{})
	if p.Len() != 3 || p.RunOf(1) != 1 || p.RunOf(2) != 2 || p.Rune(1) != objectReplacement {
		t.Fatalf("len %d runs %d %d", p.Len(), p.RunOf(1), p.RunOf(2))
	}
	if p.Break(1) != BreakAllowed || p.Break(2) != BreakAllowed {
		t.Errorf("breaks around the object %d %d, want allowed", p.Break(1), p.Break(2))
	}
	p.SetAdvance(1, 6)
	p.SetAdvance(0, 99)
	l := p.Line(0, 3, false)
	if len(l.Spans) != 3 || l.Spans[1].Text != nil || l.Spans[1].Width != 6 || l.Spans[1].Ascent != 9 {
		t.Fatalf("spans %+v", l.Spans)
	}
	if l.Spans[2].X != 11 || l.Spans[2].Run != 2 {
		t.Errorf("third span at %v from run %d, want 11 from run 2", l.Spans[2].X, l.Spans[2].Run)
	}
	if l.Spans[0].Ascent <= 0 || l.Spans[0].Descent <= 0 {
		t.Errorf("text span metrics %v %v", l.Spans[0].Ascent, l.Spans[0].Descent)
	}
}

// TestShape_ObjectBreaksAs locks Object.BreakAs: an object that breaks as
// a letter keeps to the letters beside it, where U+FFFC breaks on both
// sides (UAX #14, LB20 and LB28).
// TestShape_BreaksThaiBetweenWords: a run of Thai, class SA, whose
// breaks UAX #14 leaves to a language-specific analysis (LB1), offers a
// break at each boundary of the Thai word list, ICU's, and none inside a
// word or before a mark.
func TestShape_BreaksThaiBetweenWords(t *testing.T) {
	f := newTestFonts(t, sansFamily()...)
	// มนุษย์|ทั้ง|หลาย, then a space and เกิด|มา.
	p := shape(t, f, []ParagraphRun{{Text: "มนุษย์ทั้งหลาย เกิดมา", Style: style()}}, ParagraphOptions{})
	want := map[int]Break{6: BreakWord, 10: BreakWord, 15: BreakAllowed, 19: BreakWord}
	for i := range p.Len() {
		if got := p.Break(i); got != want[i] {
			t.Errorf("break before %d (%q) = %d, want %d", i, p.Rune(i), got, want[i])
		}
	}
}

func TestShape_ObjectBreaksAs(t *testing.T) {
	f := newTestFonts(t, sansFamily()...)
	runs := []ParagraphRun{
		{Text: "A", Style: style()},
		{Object: &Object{Width: 4, BreakAs: 'B'}},
		{Text: "A", Style: style()},
	}
	p := shape(t, f, runs, ParagraphOptions{})
	if p.Break(1) != NoBreak || p.Break(2) != NoBreak {
		t.Errorf("breaks around an object breaking as B: %d %d, want none", p.Break(1), p.Break(2))
	}
	if p.Rune(1) != objectReplacement {
		t.Errorf("object reads %q, want U+FFFC", p.Rune(1))
	}
}

func TestShape_Directions(t *testing.T) {
	f := newTestFonts(t, append(sansFamily(), fontFile{name: "hebrew.ttf", data: testHebrew()})...)
	p := shape(t, f, []ParagraphRun{{Text: "אב AB", Style: style()}}, ParagraphOptions{})
	if p.Direction() != RightToLeft {
		t.Errorf("auto direction %d, want right to left", p.Direction())
	}
	l := p.Line(0, p.Len(), false)
	if got := spanText(l); got != "ABבא " && got != "AB בא" {
		t.Errorf("visual order %q", got)
	}
	p = shape(t, f, []ParagraphRun{{Text: "AB", Style: style(), Override: RightToLeft}}, ParagraphOptions{Direction: LeftToRight})
	if got := spanText(p.Line(0, 2, false)); got != "BA" {
		t.Errorf("override drew %q, want BA", got)
	}
}

// UAX #9, L1: a tab takes the paragraph's level, inside an isolate too,
// so the text on either side of it orders as separate segments.
func TestShape_TabSeparatesSegments(t *testing.T) {
	f := newTestFonts(t, append(sansFamily(), fontFile{name: "hebrew.ttf", data: testHebrew()})...)
	runs := []ParagraphRun{
		{Text: "A ", Style: style()},
		{Text: "\u05d0\u05d1", Style: style(), Open: []rune{RLI}},
		{Object: &Object{Width: 5, Tab: true}},
		{Text: "\u05d2\u05d3", Style: style(), Close: []rune{PDI}},
		{Text: " D", Style: style()},
	}
	p := shape(t, f, runs, ParagraphOptions{Direction: LeftToRight})
	if got := spanText(p.Line(0, p.Len(), false)); got != "A \u05d1\u05d0[obj]\u05d3\u05d2 D" {
		t.Errorf("visual order %q, want the tab between the isolate's two segments", got)
	}
}

func TestShape_Spacing(t *testing.T) {
	f := newTestFonts(t, sansFamily()...)
	s := style()
	s.LetterSpacing = 1
	s.WordSpacing = 2
	p := shape(t, f, []ParagraphRun{{Text: "A B", Style: s}}, ParagraphOptions{})
	if got := []float64{p.Advance(0), p.Advance(1), p.Advance(2)}; math.Abs(got[0]-6) > 1e-6 || math.Abs(got[1]-5.5) > 1e-6 || math.Abs(got[2]-7) > 1e-6 {
		t.Errorf("advances %v, want [6 5.5 7]", got)
	}
}

// TestShape_NonNegativeAdvance checks that Style.NonNegativeAdvance
// floors at zero an advance that negative letter spacing takes below it,
// and leaves one it does not alone: "AB i" at -5 points, where the space
// and the "i" are narrower than 5 points and the "A" wider.
func TestShape_NonNegativeAdvance(t *testing.T) {
	f := newTestFonts(t, sansFamily()...)
	s := style()
	plain := shape(t, f, []ParagraphRun{{Text: "AB i", Style: s}}, ParagraphOptions{})
	s.LetterSpacing = -5
	free := shape(t, f, []ParagraphRun{{Text: "AB i", Style: s}}, ParagraphOptions{})
	s.NonNegativeAdvance = true
	floored := shape(t, f, []ParagraphRun{{Text: "AB i", Style: s}}, ParagraphOptions{})
	for i := range 4 {
		want := max(plain.Advance(i)-5, 0)
		if got := floored.Advance(i); math.Abs(got-want) > 1e-6 {
			t.Errorf("character %d advance %v, want %v", i, got, want)
		}
	}
	if free.Advance(2) >= 0 {
		t.Errorf("space advance %v without the floor, want it negative", free.Advance(2))
	}
}

func TestShape_FormatCharactersTakeNoSpacing(t *testing.T) {
	f := newTestFonts(t, sansFamily()...)
	s := style()
	plain := shape(t, f, []ParagraphRun{{Text: "A\u200bB", Style: s}}, ParagraphOptions{})
	s.LetterSpacing = 1
	spaced := shape(t, f, []ParagraphRun{{Text: "A\u200bB", Style: s}}, ParagraphOptions{})
	// CSS Text 3, 8.2: a zero width space is no typographic character
	// unit.
	if got, want := spaced.Advance(1), plain.Advance(1); math.Abs(got-want) > 1e-6 {
		t.Errorf("zero width space advance %v, want %v", got, want)
	}
	if got, want := spaced.Advance(0), plain.Advance(0)+1; math.Abs(got-want) > 1e-6 {
		t.Errorf("A advance %v, want %v", got, want)
	}
}

func TestShape_CursiveLettersTakeNoSpacing(t *testing.T) {
	f := newTestFonts(t, sansFamily()...)
	s := style()
	text := "\u0645\u0631 A\u0710"
	plain := shape(t, f, []ParagraphRun{{Text: text, Style: s}}, ParagraphOptions{})
	s.LetterSpacing = 1
	spaced := shape(t, f, []ParagraphRun{{Text: text, Style: s}}, ParagraphOptions{})
	// CSS Text 3, 7.2.1: the Arabic and Syriac letters take no spacing,
	// the space and the Latin letter do.
	for i, extra := range []float64{0, 0, 1, 1, 0} {
		if got, want := spaced.Advance(i), plain.Advance(i)+extra; math.Abs(got-want) > 1e-6 {
			t.Errorf("character %d advance %v, want %v", i, got, want)
		}
	}
}

func TestLineTrimmed_HangingSpacesKeepAdvance(t *testing.T) {
	f := newTestFonts(t, sansFamily()...)
	p := shape(t, f, []ParagraphRun{{Text: "A  ", Style: style()}}, ParagraphOptions{})
	hung := p.Line(0, 3, true).Width
	kept := p.LineTrimmed(0, 3, 2).Width
	if math.Abs(kept-(hung+p.Advance(1))) > 1e-6 || hung != p.Advance(0) {
		t.Errorf("Line width %v, LineTrimmed width %v, advances %v %v", hung, kept, p.Advance(0), p.Advance(1))
	}
}

func TestShape_ControlCharactersDrawNothing(t *testing.T) {
	f := newTestFonts(t, sansFamily()...)
	p := shape(t, f, []ParagraphRun{{Text: "A\x01B", Style: style()}}, ParagraphOptions{})
	if p.Len() != 3 || p.Break(1) != NoBreak || p.Break(2) != NoBreak {
		t.Fatalf("len %d breaks %d %d", p.Len(), p.Break(1), p.Break(2))
	}
	if got := spanText(p.Line(0, 3, false)); got != "AB" {
		t.Errorf("drew %q, want AB", got)
	}
}

func TestShape_Errors(t *testing.T) {
	f := newTestFonts(t, sansFamily()...)
	if _, _, err := f.Shape(context.Background(), []ParagraphRun{{Text: "A", Style: Style{Size: -1}}}, ParagraphOptions{}); !errors.Is(err, ErrInvalidStyle) {
		t.Errorf("bad size: %v", err)
	}
	if _, _, err := f.Shape(context.Background(), []ParagraphRun{{Object: &Object{Width: math.NaN()}}}, ParagraphOptions{}); !errors.Is(err, ErrInvalidStyle) {
		t.Errorf("bad object: %v", err)
	}
	empty := newTestFonts(t)
	if _, _, err := empty.Shape(context.Background(), []ParagraphRun{{Text: "A", Style: style()}}, ParagraphOptions{}); !errors.Is(err, ErrNoFonts) {
		t.Errorf("no fonts: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := f.Shape(ctx, []ParagraphRun{{Text: "A", Style: style()}}, ParagraphOptions{}); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: %v", err)
	}
	p := shape(t, f, nil, ParagraphOptions{})
	if p.Len() != 0 || p.MaxContent() != 0 || len(p.Line(0, 5, true).Spans) != 0 {
		t.Errorf("empty paragraph")
	}
}

func TestVisualOrder_L2(t *testing.T) {
	levels := []int{0, 1, 1, 2, 0}
	got := visualOrder(len(levels), func(i int) int { return levels[i] })
	want := []int{0, 3, 2, 1, 4}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order %v, want %v", got, want)
		}
	}
}

func TestShape_Scale(t *testing.T) {
	f := newTestFonts(t, sansFamily()...)
	st := style()
	st.Scale = 0.5
	st.LetterSpacing = 1
	p := shape(t, f, []ParagraphRun{{Text: "AB", Style: st}}, ParagraphOptions{})
	// A and B are 5 and 6 points wide: halved, then spaced by a point.
	for i, w := range []float64{3.5, 4} {
		if math.Abs(p.Advance(i)-w) > 1e-6 {
			t.Errorf("advance %d = %v, want %v", i, p.Advance(i), w)
		}
	}
	l := p.Line(0, 2, false)
	if len(l.Spans) != 1 || l.Spans[0].Text == nil || math.Abs(l.Width-7.5) > 1e-6 {
		t.Fatalf("line %+v, want one run 7.5 wide", l)
	}
	run := l.Spans[0].Text
	if run.HScale != 0.5 || math.Abs(run.Glyphs[1].X-7) > 1e-6 {
		t.Errorf("HScale %v, B at %v: want 0.5 and 7, its 3.5 unscaled", run.HScale, run.Glyphs[1].X)
	}
	for _, bad := range []float64{math.NaN(), 0.001, 7, -1} {
		st.Scale = bad
		if _, _, err := f.Shape(context.Background(), []ParagraphRun{{Text: "A", Style: st}}, ParagraphOptions{}); !errors.Is(err, ErrInvalidStyle) {
			t.Errorf("scale %v: %v", bad, err)
		}
	}
}

func TestLine_GlyphRunsTakeNoSpareCapacity(t *testing.T) {
	f := newTestFonts(t, sansFamily()...)
	p := shape(t, f, []ParagraphRun{{Text: strings.Repeat("AB C\x01", 40), Style: style()}}, ParagraphOptions{})
	l := p.Line(3, p.Len(), true)
	if len(l.Spans) == 0 {
		t.Fatal("no span")
	}
	for _, sp := range l.Spans {
		if sp.Text == nil || len(sp.Text.Glyphs) == 0 || cap(sp.Text.Glyphs) != len(sp.Text.Glyphs) {
			t.Fatalf("span %+v: a run holds every glyph it draws and no spare room", sp)
		}
	}
	if got, want := spanText(l), strings.Repeat("AB C", 40)[3:]; got != want {
		t.Errorf("drew %q, want %q", got, want)
	}
}

func TestClusterText_MatchesItsCharacters(t *testing.T) {
	for _, tc := range []struct {
		in   []rune
		want string
	}{
		{[]rune{'A'}, "A"},
		{[]rune{' '}, " "},
		{[]rune{0x7e}, "~"},
		{[]rune{0x7f}, ""},
		{[]rune{'\n'}, ""},
		{[]rune{'é'}, "é"},
		{[]rune{'e', 0x301}, "e\u0301"},
		{[]rune{'f', 'i'}, "fi"},
		{[]rune{'A', 0x01}, "A"},
		{nil, ""},
	} {
		if got := clusterText(tc.in); got != tc.want {
			t.Errorf("clusterText(%q) = %q, want %q", string(tc.in), got, tc.want)
		}
	}
}

func TestPackGlyph_KeepsEveryIndex(t *testing.T) {
	for _, tc := range []struct {
		gid        uint16
		cluster, n int
	}{
		{0, 0, 0},
		{1, 2, 3},
		{0xffff, MaxLayoutRunes, MaxLayoutRunes},
		{0x8000, MaxLayoutRunes - 1, 1},
	} {
		g := shapedGlyph{packed: packGlyph(tc.gid, tc.cluster, tc.n)}
		if g.gid() != tc.gid || g.cluster() != tc.cluster || g.clusterLen() != tc.n {
			t.Errorf("packGlyph(%d, %d, %d) reads back %d, %d, %d", tc.gid, tc.cluster, tc.n, g.gid(), g.cluster(), g.clusterLen())
		}
	}
}

func TestShape_EveryRunIndexFits(t *testing.T) {
	f := newTestFonts(t, sansFamily()...)
	runs := make([]ParagraphRun, MaxLayoutRuns)
	for i := range runs {
		runs[i] = ParagraphRun{Text: "A", Style: style()}
	}
	runs[len(runs)-1] = ParagraphRun{Object: &Object{Width: 7}}
	p := shape(t, f, runs, ParagraphOptions{})
	last := p.Len() - 1
	if p.Len() != MaxLayoutRuns || p.RunOf(last) != MaxLayoutRuns-1 || p.Rune(last) != objectReplacement || p.Advance(last) != 7 {
		t.Fatalf("len %d, last run %d, rune %q, advance %v", p.Len(), p.RunOf(last), p.Rune(last), p.Advance(last))
	}
	p.SetAdvance(last, 9)
	p.SetAdvance(0, 9)
	if p.Advance(last) != 9 || p.Advance(0) == 9 {
		t.Errorf("SetAdvance moved the object to %v and the text to %v", p.Advance(last), p.Advance(0))
	}
}

// TestShape_LeadingSpaceTakesItsRunsFace locks that a space opening a run
// is drawn in that run's face, not in the face of the run before it: a
// space selects no face of its own, and Chromium draws the space after an
// element of another family in the family of the text that holds it.
//
// # Corpus evidence
//
//   - wpt: css/CSS2/fonts/font-family-013 (a space and an em dash after a
//     span of Ahem) draws its reference in Chromium 156.
//   - probe, docx: LibreOffice of gotenberg/gotenberg:9.4.1 draws " X"
//     in Liberation Serif after an X in Liberation Mono with a 9 pt space
//     at 36 pt (Serif's), and " X" in Mono after Serif with a 21.6 pt one
//     (Mono's).
//   - probe, odt: the same runs as text spans draw the same widths.
func TestShape_LeadingSpaceTakesItsRunsFace(t *testing.T) {
	t.Parallel()
	f := newTestFonts(t, repoFont(t, "Ahem.ttf", ahemPath), repoFont(t, "ab.ttf", dejavuAB))
	ahem, dejavu := style(), style()
	ahem.Families, dejavu.Families = []string{"Ahem"}, []string{"DejaVu Sans"}
	ahem.Size, dejavu.Size = 20, 20
	width := func(runs ...ParagraphRun) float64 {
		return shape(t, f, runs, ParagraphOptions{}).Line(0, 3, false).Width
	}
	a := width(ParagraphRun{Text: "A", Style: ahem})
	rest := shape(t, f, []ParagraphRun{{Text: " A", Style: dejavu}}, ParagraphOptions{}).Line(0, 2, false).Width
	got := width(ParagraphRun{Text: "A", Style: ahem}, ParagraphRun{Text: " A", Style: dejavu})
	if math.Abs(got-(a+rest)) > 1e-6 {
		t.Errorf("width %v, want %v: the space after the Ahem run is drawn in DejaVu Sans", got, a+rest)
	}
}
