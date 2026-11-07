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
