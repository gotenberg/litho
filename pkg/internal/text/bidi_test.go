package text

import "testing"

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
