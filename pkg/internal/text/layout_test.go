package text

import (
	"context"
	"errors"
	"math"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/go-text/typesetting/di"

	"github.com/gotenberg/litho/pkg/internal/compose"
)

// near reports whether a and b agree within the 1/4096 point precision of
// the shaping units.
func near(a, b float64) bool {
	return math.Abs(a-b) < 1e-3
}

func TestLayout_PositionsGlyphsByAdvance(t *testing.T) {
	t.Parallel()
	f := newTestFonts(t, sansFamily()...)
	lines, rep := layout(t, f, "ABC", style(), 100, Collapse)
	if len(lines) != 1 || len(lines[0].Runs) != 1 || len(rep.MissingGlyphs) != 0 {
		t.Fatalf("lines %+v, report %+v", lines, rep)
	}
	run := lines[0].Runs[0]
	if run.Size != 10 || run.Color != style().Color || run.Origin.X != 0 || run.Origin.Y != 0 {
		t.Errorf("run size %v, color %v, origin %v", run.Size, run.Color, run.Origin)
	}
	wantX := []float64{0, 5, 11}
	for i, g := range run.Glyphs {
		if !near(g.X, wantX[i]) || g.Y != 0 || g.GID != uint16(i+2) || g.Text != string("ABC"[i]) {
			t.Errorf("glyph %d = %+v, want X %v, GID %d", i, g, wantX[i], i+2)
		}
	}
	if !near(lines[0].Width, 18) {
		t.Errorf("width %v, want 18", lines[0].Width)
	}
	if m := f.Match(style()).Metrics(10); !near(lines[0].Ascent, m.Ascent) || !near(lines[0].Descent, m.Descent) {
		t.Errorf("line ascent %v descent %v, face metrics %+v", lines[0].Ascent, lines[0].Descent, m)
	}
}

func TestLayout_FractionalSizeIsExact(t *testing.T) {
	t.Parallel()
	f := newTestFonts(t, sansFamily()...)
	s := style()
	s.Size = 10.3
	lines, _ := layout(t, f, "AB", s, 100, Collapse)
	if want := 1.1 * 10.3; !near(lines[0].Width, want) {
		t.Errorf("width %v, want %v", lines[0].Width, want)
	}
}

func TestLayout_WrapsWithHangingSpaces(t *testing.T) {
	t.Parallel()
	f := newTestFonts(t, sansFamily()...)
	// "AB AB" is 5+6+2.5+5+6 = 24.5 points; the space after it hangs.
	for _, ws := range []Whitespace{Collapse, Preserve} {
		lines, _ := layout(t, f, "AB AB AB", style(), 24.6, ws)
		if len(lines) != 2 || lineText(lines[0]) != "AB AB " || lineText(lines[1]) != "AB" {
			t.Fatalf("whitespace %d: lines %q", ws, []string{lineText(lines[0])})
		}
		if !near(lines[0].Width, 24.5) || !near(lines[1].Width, 11) {
			t.Errorf("whitespace %d: widths %v, %v, want 24.5, 11", ws, lines[0].Width, lines[1].Width)
		}
	}
}

func TestLayout_PreserveHangsEverySpaceAtTheLineEnd(t *testing.T) {
	t.Parallel()
	f := newTestFonts(t, sansFamily()...)
	lines, _ := layout(t, f, "AB   AB", style(), 20, Preserve)
	if len(lines) != 2 || lineText(lines[0]) != "AB   " || lineText(lines[1]) != "AB" {
		t.Fatalf("lines %q", collect(lines))
	}
	if !near(lines[0].Width, 11) {
		t.Errorf("first line width %v, want 11: trailing spaces must hang", lines[0].Width)
	}
}

func TestLayout_ForcedBreaks(t *testing.T) {
	t.Parallel()
	f := newTestFonts(t, sansFamily()...)
	lines, _ := layout(t, f, "AB\nBA\r\nA\rB\u2028C\n\nAB\n", style(), 100, Preserve)
	want := []string{"AB", "BA", "A", "B", "C", "", "AB"}
	if got := collect(lines); !slices.Equal(got, want) {
		t.Fatalf("lines %q, want %q", got, want)
	}
	blank := lines[5]
	if len(blank.Runs) != 0 || blank.Width != 0 || blank.Ascent <= 0 || blank.Descent <= 0 {
		t.Errorf("blank line %+v", blank)
	}
	if lines, _ := layout(t, f, "", style(), 100, Preserve); len(lines) != 0 {
		t.Errorf("empty text gives %d lines", len(lines))
	}
	if lines, _ := layout(t, f, "\n", style(), 100, Preserve); len(lines) != 1 {
		t.Errorf("a lone break gives %d lines, want 1", len(lines))
	}
}

func TestLayout_CollapseVersusPreserve(t *testing.T) {
	t.Parallel()
	f := newTestFonts(t, sansFamily()...)
	src := "  AB \t\n  BA  "
	lines, _ := layout(t, f, src, style(), 100, Collapse)
	if got := collect(lines); !slices.Equal(got, []string{"AB BA"}) {
		t.Errorf("collapse: %q", got)
	}
	if !near(lines[0].Width, 24.5) {
		t.Errorf("collapse width %v, want 24.5", lines[0].Width)
	}
	lines, _ = layout(t, f, src, style(), 100, Preserve)
	// The tab after "  AB " (5 columns) advances to column 8.
	if got := collect(lines); !slices.Equal(got, []string{"  AB    ", "  BA  "}) {
		t.Fatalf("preserve: %q", got)
	}
	if first := lines[1].Runs[0].Glyphs[2]; !near(first.X, 5) || first.Text != "B" {
		t.Errorf("preserve keeps leading spaces: B at %+v, want X 5", first)
	}
	if !near(lines[1].Width, 16) {
		t.Errorf("preserve width %v, want 16 (trailing spaces hang)", lines[1].Width)
	}
}

func TestLayout_CollapseAcrossRuns(t *testing.T) {
	t.Parallel()
	f := newTestFonts(t, sansFamily()...)
	bold := style()
	bold.Weight = 700
	lines, _, err := f.Layout(context.Background(), []Run{{Text: "A  ", Style: style()}, {Text: " \nB", Style: bold}}, 100, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := collect(lines); !slices.Equal(got, []string{"A B"}) || len(lines[0].Runs) != 2 {
		t.Fatalf("lines %q, %d runs", got, len(lines[0].Runs))
	}
	if r := lines[0].Runs[1]; !near(r.Origin.X, 7.5) || !strings.Contains(r.Face.Key, "sans_bold") {
		t.Errorf("bold run at %v with face %s", r.Origin.X, r.Face.Key)
	}
}

func TestLayout_BreaksALongWordAtGraphemes(t *testing.T) {
	t.Parallel()
	f := newTestFonts(t, sansFamily()...)
	lines, _ := layout(t, f, "ABABABABAB", style(), 16.1, Collapse)
	if got := collect(lines); !slices.Equal(got, []string{"ABA", "BA", "BA", "BA", "B"}) {
		t.Fatalf("lines %q", got)
	}
	for i, l := range lines {
		if l.Width > 16.1 {
			t.Errorf("line %d is %v wide", i, l.Width)
		}
	}
	// A grapheme wider than the line still lands on a line of its own.
	lines, _ = layout(t, f, "CC", style(), 1, Collapse)
	if got := collect(lines); !slices.Equal(got, []string{"C", "C"}) {
		t.Errorf("narrow lines %q", got)
	}
}

func TestLayout_Spacing(t *testing.T) {
	t.Parallel()
	f := newTestFonts(t, sansFamily()...)
	s := style()
	s.LetterSpacing = 2
	lines, _ := layout(t, f, "ABC", s, 100, Collapse)
	if !near(lines[0].Width, 18+2*2) {
		t.Errorf("letter spacing: width %v, want 22", lines[0].Width)
	}
	s = style()
	s.WordSpacing = 3
	lines, _ = layout(t, f, "A B", s, 100, Collapse)
	if !near(lines[0].Width, 13.5+3) {
		t.Errorf("word spacing: width %v, want 16.5", lines[0].Width)
	}
}

func TestLayout_FallsBackPerCharacter(t *testing.T) {
	t.Parallel()
	f := newTestFonts(t, append(sansFamily(), fontFile{name: "digits.ttf", data: testDigits()})...)
	lines, rep := layout(t, f, "ADB", style(), 100, Collapse)
	if len(rep.MissingGlyphs) != 0 || len(lines) != 1 || len(lines[0].Runs) != 3 {
		t.Fatalf("report %+v, lines %+v", rep, lines)
	}
	keys := []string{}
	for _, r := range lines[0].Runs {
		keys = append(keys, r.Face.Key)
	}
	if !strings.Contains(keys[0], "sans_regular") || !strings.Contains(keys[1], "digits.ttf#0") || keys[2] != keys[0] {
		t.Errorf("faces %q", keys)
	}
	if lineText(lines[0]) != "ADB" || !near(lines[0].Runs[1].Origin.X, 5) || !near(lines[0].Width, 19) {
		t.Errorf("text %q, D at %v, width %v", lineText(lines[0]), lines[0].Runs[1].Origin.X, lines[0].Width)
	}
}

func TestLayout_FallbackKeepsWeightAndStyle(t *testing.T) {
	t.Parallel()
	// DejaVu Sans covers B; LithoOutlines does not. The Test Sans faces
	// cover B too, and the fallback for a bold run picks the bold one.
	f := newTestFonts(t, append(sansFamily(), repoFont(t, "outlines.ttf", outlinesA))...)
	s := style()
	s.Families = []string{"LithoOutlines"}
	s.Weight = 700
	lines, rep := layout(t, f, "AB", s, 100, Collapse)
	if len(rep.MissingGlyphs) != 0 || len(lines[0].Runs) != 2 {
		t.Fatalf("report %+v, runs %d", rep, len(lines[0].Runs))
	}
	if k := lines[0].Runs[1].Face.Key; !strings.Contains(k, "sans_bold") {
		t.Errorf("fallback for B is %s, want the bold face", k)
	}
}

func TestLayout_ReportsMissingGlyphs(t *testing.T) {
	t.Parallel()
	f := newTestFonts(t, sansFamily()...)
	lines, rep := layout(t, f, "AQZQ", style(), 100, Collapse)
	if !slices.Equal(rep.MissingGlyphs, []rune{'Q', 'Z'}) {
		t.Errorf("missing %q", rep.MissingGlyphs)
	}
	if len(lines[0].Runs) != 1 || !strings.Contains(lines[0].Runs[0].Face.Key, "sans_regular") {
		t.Fatalf("runs %+v", lines[0].Runs)
	}
	g := lines[0].Runs[0].Glyphs[1]
	if g.GID != 0 || g.Text != "Q" {
		t.Errorf("Q draws %+v, want .notdef", g)
	}
}

func TestLayout_RightToLeft(t *testing.T) {
	t.Parallel()
	f := newTestFonts(t, append(sansFamily(), fontFile{name: "hebrew.ttf", data: testHebrew()})...)
	lines, rep := layout(t, f, "אב AB", style(), 100, Collapse)
	if len(rep.MissingGlyphs) != 0 || len(lines) != 1 || len(lines[0].Runs) != 2 {
		t.Fatalf("report %+v, lines %+v", rep, lines)
	}
	// The paragraph is right to left: the Latin run sits at the left, then
	// the Hebrew run with the space it holds, which draws its logical
	// order "alef bet space" reversed.
	runs := lines[0].Runs
	if lineText(Line{Runs: runs[:1]}) != "AB" || !strings.Contains(runs[1].Face.Key, "hebrew") || !near(runs[1].Origin.X, 11) {
		t.Errorf("visual order %q, second run face %s at %v", lineText(lines[0]), runs[1].Face.Key, runs[1].Origin.X)
	}
	heb := runs[1].Glyphs
	if len(heb) != 3 || heb[0].Text != " " || heb[1].Text != "ב" || heb[2].Text != "א" || !near(heb[2].X, 7) {
		t.Errorf("hebrew glyphs %+v", heb)
	}
	if !near(lines[0].Width, 11+2.5+8.5) {
		t.Errorf("width %v, want 22", lines[0].Width)
	}
}

func TestParagraphDirection(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		text string
		want di.Direction
	}{
		{"abc", di.DirectionLTR},
		{"123 אב", di.DirectionRTL},
		{"\u2067אב\u2069 abc", di.DirectionLTR},
		{"123", di.DirectionLTR},
		{"", di.DirectionLTR},
	} {
		if got := paragraphDirection([]rune(tc.text)); got != tc.want {
			t.Errorf("paragraphDirection(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
}

func TestLayout_IsDeterministic(t *testing.T) {
	t.Parallel()
	files := append(sansFamily(), fontFile{name: "digits.ttf", data: testDigits()}, fontFile{name: "hebrew.ttf", data: testHebrew()})
	runs := []Run{
		{Text: "AB ABD אב BA\tCC ", Style: style()},
		{Text: "Q ABC AB", Style: Style{Families: []string{"sans-serif"}, Size: 12, Weight: 700, LetterSpacing: 0.5}},
	}
	lay := func(f *Fonts) ([]Line, Report) {
		lines, rep, err := f.Layout(context.Background(), runs, 40, Options{Whitespace: Preserve})
		if err != nil {
			t.Fatal(err)
		}
		return lines, rep
	}
	f := newTestFonts(t, files...)
	l1, r1 := lay(f)
	l2, r2 := lay(f)
	l3, r3 := lay(newTestFonts(t, files...))
	if !reflect.DeepEqual(l1, l2) || !reflect.DeepEqual(r1, r2) {
		t.Error("two layouts on one index differ")
	}
	// The two indexes read the same bytes from different directories, so
	// only the face keys may differ.
	for _, ls := range [][]Line{l1, l3} {
		for _, l := range ls {
			for _, r := range l.Runs {
				r.Face = &compose.Face{Key: filepath.Base(r.Face.Key), Program: r.Face.Program}
			}
		}
	}
	if !reflect.DeepEqual(l1, l3) || !reflect.DeepEqual(r1, r3) {
		t.Error("layouts on two indexes of the same files differ")
	}
}

func TestLayout_ConcurrentCalls(t *testing.T) {
	t.Parallel()
	f := newTestFonts(t, append(sansFamily(), fontFile{name: "digits.ttf", data: testDigits()})...)
	texts := []string{"AB AB ABD", "ADA BCB", "CCC DDD AAA", "QAB D"}
	styles := []Style{style(), {Families: []string{"Test Sans"}, Size: 9, Weight: 700}, {Families: []string{"Test Digits"}, Size: 11, Italic: true}}
	type result struct {
		lines []Line
		rep   Report
	}
	want := make([]result, len(texts)*len(styles))
	for i := range want {
		lines, rep := layout(t, f, texts[i%len(texts)], styles[i/len(texts)], 30, Collapse)
		want[i] = result{lines, rep}
	}
	var wg sync.WaitGroup
	errs := make(chan string, 8*len(want))
	for range 8 {
		wg.Go(func() {
			for i := range want {
				lines, rep, err := f.Layout(context.Background(), []Run{{Text: texts[i%len(texts)], Style: styles[i/len(texts)]}}, 30, Options{})
				if err != nil || !reflect.DeepEqual(lines, want[i].lines) || !reflect.DeepEqual(rep, want[i].rep) {
					errs <- texts[i%len(texts)]
				}
				f.Match(styles[i%len(styles)])
			}
		})
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Errorf("concurrent layout of %q differs", e)
	}
}

func TestLayout_SentinelErrors(t *testing.T) {
	t.Parallel()
	f := newTestFonts(t, sansFamily()...)
	ctx := context.Background()
	one := []Run{{Text: "AB", Style: style()}}
	for _, w := range []float64{0, -1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, _, err := f.Layout(ctx, one, w, Options{}); !errors.Is(err, ErrInvalidWidth) {
			t.Errorf("width %v: %v", w, err)
		}
	}
	if lines, _, err := f.Layout(ctx, one, math.MaxFloat64, Options{}); err != nil || len(lines) != 1 {
		t.Errorf("huge width: %v, %d lines", err, len(lines))
	}
	if _, _, err := f.Layout(ctx, make([]Run, MaxLayoutRuns+1), 10, Options{}); !errors.Is(err, ErrTooManyRuns) {
		t.Errorf("too many runs: %v", err)
	}
	long := []Run{{Text: strings.Repeat("A", MaxLayoutRunes/2), Style: style()}, {Text: strings.Repeat("B", MaxLayoutRunes/2+1), Style: style()}}
	if _, _, err := f.Layout(ctx, long, 10, Options{}); !errors.Is(err, ErrTextTooLong) {
		t.Errorf("too long: %v", err)
	}
	for _, mut := range []func(*Style){
		func(s *Style) { s.Size = 0 },
		func(s *Style) { s.Size = math.NaN() },
		func(s *Style) { s.Size = maxFontSize + 1 },
		func(s *Style) { s.LetterSpacing = math.Inf(1) },
		func(s *Style) { s.WordSpacing = math.NaN() },
	} {
		s := style()
		mut(&s)
		if _, _, err := f.Layout(ctx, []Run{{Text: "A", Style: s}}, 10, Options{}); !errors.Is(err, ErrInvalidStyle) {
			t.Errorf("style %+v: %v", s, err)
		}
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := f.Layout(canceled, one, 10, Options{}); !errors.Is(err, context.Canceled) {
		t.Errorf("canceled: %v", err)
	}
	empty := newTestFonts(t)
	if _, _, err := empty.Layout(ctx, one, 10, Options{}); !errors.Is(err, ErrNoFonts) {
		t.Errorf("no fonts: %v", err)
	}
}

// collect returns the text of every line.
func collect(lines []Line) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = lineText(l)
	}
	return out
}

func TestLayout_CutsAVeryLongWord(t *testing.T) {
	t.Parallel()
	f := newTestFonts(t, sansFamily()...)
	word := strings.Repeat("AB", 1500)
	lines, _ := layout(t, f, word+" A", style(), maxLineWidth, Collapse)
	if got := collect(lines); len(got) != 3 || len([]rune(got[0])) != maxWordRunes || strings.Join(got, "") != word+" A" {
		t.Errorf("%d lines of %v characters", len(got), []int{len(got[0]), len(got[len(got)-1])})
	}
}
