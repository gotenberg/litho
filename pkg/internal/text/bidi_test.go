package text

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestShape_ExplicitEmbeddings(t *testing.T) {
	f := newTestFonts(t, append(sansFamily(), fontFile{name: "hebrew.ttf", data: testHebrew()})...)
	s := style()
	tests := []struct {
		name string
		runs []ParagraphRun
		base Direction
		want string
	}{
		// Without an isolate, the Hebrew and the A that follows it in a
		// right-to-left context reorder together.
		{"embedding", []ParagraphRun{{Text: "C ", Style: s}, {Text: "אב A", Style: s, Open: []rune{RLE}, Close: []rune{PDF}}}, LeftToRight, "C A בא"},
		{"isolate", []ParagraphRun{{Text: "C ", Style: s}, {Text: "אב A", Style: s, Open: []rune{RLI}, Close: []rune{PDI}}}, LeftToRight, "C A בא"},
		{"override", []ParagraphRun{{Text: "AB ", Style: s}, {Text: "BC", Style: s, Open: []rune{RLO}, Close: []rune{PDF}}}, LeftToRight, "AB CB"},
		{"left-to-right isolate in right-to-left", []ParagraphRun{{Text: "א ", Style: s}, {Text: "AB C", Style: s, Open: []rune{LRI}, Close: []rune{PDI}}}, RightToLeft, "AB C א"},
		{"first strong isolate", []ParagraphRun{{Text: "AB ", Style: s}, {Text: "אב", Style: s, Open: []rune{FSI}, Close: []rune{PDI}}}, LeftToRight, "AB בא"},
		{"number after right-to-left letters", []ParagraphRun{{Text: "AB אב 12", Style: s}}, LeftToRight, "AB 12 בא"},
		{"number in a right-to-left isolate", []ParagraphRun{{Text: "AB ", Style: s}, {Text: "1,2 אב", Style: s, Open: []rune{RLI}, Close: []rune{PDI}}, {Text: " C", Style: s}}, LeftToRight, "AB בא 1,2 C"},
		{"number after left-to-right letters", []ParagraphRun{{Text: "אב AB 12", Style: s}}, RightToLeft, "AB 12 בא"},
		{"unclosed and stray controls", []ParagraphRun{{Text: "AB", Style: s, Close: []rune{PDI, 'x'}}, {Text: "C", Style: s, Open: []rune{'y', RLI}}}, LeftToRight, "ABC"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := shape(t, f, tt.runs, ParagraphOptions{Direction: tt.base})
			if got := spanText(p.Line(0, p.Len(), false)); got != tt.want {
				t.Errorf("visual order %q, want %q", got, tt.want)
			}
		})
	}
}

func TestShape_IsolateSkippedForBaseDirection(t *testing.T) {
	f := newTestFonts(t, append(sansFamily(), fontFile{name: "hebrew.ttf", data: testHebrew()})...)
	p := shape(t, f, []ParagraphRun{{Text: "אב", Style: style(), Open: []rune{LRI}, Close: []rune{PDI}}, {Text: " A", Style: style()}}, ParagraphOptions{})
	if p.Direction() != LeftToRight {
		t.Errorf("direction %d, want left to right: an isolate's characters do not set it (UAX #9, P2)", p.Direction())
	}
}

// TestExplicitAndNumericLevels_Linear pins that litho's own rules of UAX
// #9 cost time linear in the paragraph when it holds long runs of the
// characters whose rules look past themselves: European terminators (W5),
// tested once a sequence rather than once a character, and first strong
// isolates (X5c), which scan to their PDI only while one can open. A scan
// per character would take minutes over [MaxLayoutRunes] characters.
//
// # Corpus evidence
//
//   - probe, text: a scan per character takes 2.1 s over 40,000
//     terminators and 0.43 s over 20,000 unclosed first strong isolates,
//     four times as long for twice the text.
func TestExplicitAndNumericLevels_Linear(t *testing.T) {
	t.Parallel()
	const n = MaxLayoutRunes / 4
	for _, tc := range []struct {
		name string
		unit string
	}{
		{"terminators", "#"},
		{"unclosed first strong isolates", string(FSI) + "a"},
	} {
		text := []rune(strings.Repeat(tc.unit, n/utf8.RuneCountInString(tc.unit)))
		start := time.Now()
		levels, _ := explicitLevels(text, 0)
		numericRuns(text, levels)
		if d := time.Since(start); d > 5*time.Second {
			t.Errorf("%s: the levels of %d characters resolve in %v", tc.name, len(text), d)
		}
	}
}

// TestNumericRuns_Terminators pins rule W5 of UAX #9 on sequences of
// European terminators after a right-to-left letter: a sequence next to a
// European number on either side joins it (W5), and a terminator then sits
// with the numbers it follows or precedes; one between other characters
// stays apart.
func TestNumericRuns_Terminators(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		text string
		want string
	}{
		{"א##1", "0011"},
		{"א1##", "0111"},
		{"א#a#", "0000"},
		{"א#1#ב##1", "01110011"},
		{"א1#,#", "01100"},
		{"א##", "000"},
	} {
		text := []rune(tc.text)
		var got strings.Builder
		for _, v := range numericRuns(text, make([]int, len(text))) {
			if v {
				got.WriteByte('1')
			} else {
				got.WriteByte('0')
			}
		}
		if got.String() != tc.want {
			t.Errorf("numericRuns(%q) = %s, want %s", tc.text, got.String(), tc.want)
		}
	}
}

// identity returns the index of each of n characters in a bidi text that
// holds them alone.
func identity(n int) []int {
	at := make([]int, n)
	for i := range at {
		at[i] = i
	}
	return at
}

// TestNeedsBidi pins the paragraphs whose levels go-text's algorithm
// resolves: a right-to-left base, letter, Arabic number or explicit
// formatting character; not terminators, numbers, brackets or a
// left-to-right embedding alone.
func TestNeedsBidi(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		text string
		base Direction
		want bool
	}{
		{"abc #1, (x) % ε ж", LeftToRight, false},
		{string(LRE) + "12" + string(PDF), LeftToRight, false},
		{"abc", RightToLeft, true},
		{"abc ש", LeftToRight, true},
		{"abc ع", LeftToRight, true},
		{"abc ١", LeftToRight, true},
		{"abc ‏", LeftToRight, true},
		{string(RLI) + "a" + string(PDI), LeftToRight, true},
	} {
		if got := needsBidi([]rune(tc.text), tc.base); got != tc.want {
			t.Errorf("needsBidi(%q, %v) = %v, want %v", tc.text, tc.base, got, tc.want)
		}
	}
}

// TestResolveLevels_LeftToRightSkipsTheAlgorithm checks that a paragraph
// needsBidi passes resolves as go-text resolves it: every character at
// its explicit level, an even one, terminators and numbers included.
func TestResolveLevels_LeftToRightSkipsTheAlgorithm(t *testing.T) {
	t.Parallel()
	text := []rune("a #1, (b) " + string(LRE) + "c 2%" + string(PDF) + " d")
	got, err := resolveLevels(t.Context(), text, identity(len(text)), LeftToRight)
	if err != nil {
		t.Fatal(err)
	}
	explicit, _ := explicitLevels(text, 0)
	for i, lv := range got {
		if int(lv) != explicit[i] {
			t.Errorf("character %d %q at level %d, want %d", i, text[i], lv, explicit[i])
		}
	}
}

// TestBidiCut checks the pieces a long text is resolved in: none longer
// than maxBidiRunes, each ending after a space when its last characters
// hold one, together the whole text.
func TestBidiCut(t *testing.T) {
	t.Parallel()
	text := []rune(strings.Repeat("abcdefg ", 3*maxBidiRunes/8) + strings.Repeat("x", 2*maxBidiRunes))
	start := 0
	for start < len(text) {
		end := bidiCut(text, start, len(text))
		if end <= start || end-start > maxBidiRunes {
			t.Fatalf("piece [%d, %d)", start, end)
		}
		if end < len(text) && end-start > maxBidiRunes-bidiCutSlack && text[end-1] != ' ' && strings.ContainsRune(string(text[end-bidiCutSlack:end]), ' ') {
			t.Errorf("piece [%d, %d) ends on %q with a space in its last characters", start, end, text[end-1])
		}
		start = end
	}
	if start != len(text) {
		t.Errorf("the pieces end at %d of %d", start, len(text))
	}
}

// TestResolveLevels_Pieces checks a right-to-left paragraph longer than
// maxBidiRunes: a Hebrew letter then European digits resolve in pieces,
// the digits at level 2 as in one paragraph, and a cancelled context stops
// the resolution with its error.
func TestResolveLevels_Pieces(t *testing.T) {
	t.Parallel()
	text := []rune("ש" + strings.Repeat("1", 3*maxBidiRunes))
	got, err := resolveLevels(t.Context(), text, identity(len(text)), LeftToRight)
	if err != nil {
		t.Fatal(err)
	}
	if got[0] != 1 || got[1] != 2 {
		t.Errorf("levels %d and %d, want 1 and 2", got[0], got[1])
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := resolveLevels(ctx, text, identity(len(text)), LeftToRight); !errors.Is(err, context.Canceled) {
		t.Errorf("resolveLevels on a cancelled context = %v, want context.Canceled", err)
	}
}
