package text

import (
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
