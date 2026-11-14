package text

import (
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
)

// testCJK builds a "Test CJK" face whose ideograph and fullwidth
// punctuation are one em wide, and whose ” is half an em, as the Japanese
// Noto CJK faces draw it proportional.
func testCJK() []byte {
	return fontbuild.TrueType{
		PostScriptName: "TestCJK",
		Family:         "Test CJK",
		Subfamily:      "Regular",
		NotdefAdvance:  1000,
		Glyphs: []fontbuild.Glyph{
			{Name: "han", Unicode: '漢', Advance: 1000},
			{Name: "open", Unicode: '「', Advance: 1000},
			{Name: "close", Unicode: '」', Advance: 1000},
			{Name: "comma", Unicode: '、', Advance: 1000},
			{Name: "colon", Unicode: '：', Advance: 1000},
			{Name: "quoteright", Unicode: '”', Advance: 500},
			{Name: "ideospace", Unicode: '\u3000', Advance: 1000, Empty: true},
		},
	}.Build()
}

func TestShape_TrimsAdjacentPunctuation(t *testing.T) {
	f := newTestFonts(t, fontFile{name: "cjk.ttf", data: testCJK()})
	s := Style{Families: []string{"Test CJK"}, Size: 10, Fallback: FallbackChromium}
	tests := []struct {
		text string
		want []float64
		trim bool
	}{
		{"漢」「漢", []float64{10, 10, 5, 10}, true},
		{"漢、「漢", []float64{10, 10, 5, 10}, true},
		{"漢：「漢", []float64{10, 10, 5, 10}, true},
		{"漢」」漢", []float64{10, 5, 10, 10}, true},
		{"「漢」漢", []float64{10, 10, 10, 10}, true},
		{"漢」「漢", []float64{10, 10, 10, 10}, false},
		// A closing quotation mark half an em wide is no fullwidth
		// punctuation: the opening one after it keeps its width, a closing
		// one before it is trimmed.
		{"漢”「漢", []float64{10, 5, 10, 10}, true},
		{"漢」”漢", []float64{10, 5, 5, 10}, true},
		{"漢」\u3000漢", []float64{10, 5, 10, 10}, true},
	}
	for _, tt := range tests {
		st := s
		st.NoSpacingTrim = !tt.trim
		p := shape(t, f, []ParagraphRun{{Text: tt.text, Style: st}}, ParagraphOptions{})
		for i, w := range tt.want {
			if math.Abs(p.Advance(i)-w) > 1e-6 {
				t.Errorf("%q: advance %d = %v, want %v", tt.text, i, p.Advance(i), w)
			}
		}
	}
}

// TestShape_TrimsByClassOutsideChromium locks that a run under another
// fallback rule is trimmed by the classes of CSS Text 4, 8.3 alone: a
// quotation mark of any width is a punctuation of its class, and a closing
// punctuation before an ideographic space keeps its width.
func TestShape_TrimsByClassOutsideChromium(t *testing.T) {
	f := newTestFonts(t, fontFile{name: "cjk.ttf", data: testCJK()})
	s := Style{Families: []string{"Test CJK"}, Size: 10}
	for _, tt := range []struct {
		text string
		want []float64
	}{
		{"漢”「漢", []float64{10, 5, 5, 10}},
		{"漢」\u3000漢", []float64{10, 10, 10, 10}},
		{"漢」」漢", []float64{10, 5, 10, 10}},
	} {
		p := shape(t, f, []ParagraphRun{{Text: tt.text, Style: s}}, ParagraphOptions{})
		for i, w := range tt.want {
			if math.Abs(p.Advance(i)-w) > 1e-6 {
				t.Errorf("%q: advance %d = %v, want %v", tt.text, i, p.Advance(i), w)
			}
		}
	}
}

// TestShape_AlternateWidthsTrimNothing locks that a run turning palt or
// halt on is trimmed nothing, as Chromium draws it, and that turning them
// off, or another feature on, keeps the trim.
func TestShape_AlternateWidthsTrimNothing(t *testing.T) {
	f := newTestFonts(t, fontFile{name: "cjk.ttf", data: testCJK()})
	for _, tt := range []struct {
		features []Feature
		want     float64
	}{
		{nil, 5},
		{[]Feature{{Tag: "palt", Value: 1}}, 10},
		{[]Feature{{Tag: "halt", Value: 1}}, 10},
		{[]Feature{{Tag: "palt", Value: 1}, {Tag: "palt", Value: 0}}, 5},
		{[]Feature{{Tag: "liga", Value: 0}}, 5},
	} {
		s := Style{Families: []string{"Test CJK"}, Size: 10, Fallback: FallbackChromium, Settings: &FontSettings{Features: tt.features}}
		p := shape(t, f, []ParagraphRun{{Text: "漢」」漢", Style: s}}, ParagraphOptions{})
		if got := p.Advance(1); math.Abs(got-tt.want) > 1e-6 {
			t.Errorf("features %v: advance %v, want %v", tt.features, got, tt.want)
		}
	}
}

func TestInkClass(t *testing.T) {
	for _, tt := range []struct {
		x, adv float64
		want   punct
	}{
		{200, 1000, punctClose},
		{500, 1000, punctMiddle},
		{800, 1000, punctOpen},
		{100, 0, punctNone},
	} {
		if got := inkClass(tt.x, tt.adv); got != tt.want {
			t.Errorf("inkClass(%v, %v) = %v, want %v", tt.x, tt.adv, got, tt.want)
		}
	}
}

func TestShape_Autospace(t *testing.T) {
	f := newTestFonts(t, append(sansFamily(), fontFile{name: "cjk.ttf", data: testCJK()})...)
	s := Style{Families: []string{"Test Sans", "Test CJK"}, Size: 10, Autospace: 1.25, AutospaceAlpha: true}
	p := shape(t, f, []ParagraphRun{{Text: "漢A、A漢", Style: s}}, ParagraphOptions{})
	// The ideograph before A and the A before it take the space; the
	// fullwidth comma, punctuation, takes none.
	want := []float64{11.25, 5, 10, 6.25, 10}
	for i, w := range want {
		if math.Abs(p.Advance(i)-w) > 1e-6 {
			t.Errorf("advance %d = %v, want %v", i, p.Advance(i), w)
		}
	}
	s.AutospaceAlpha = false
	if p = shape(t, f, []ParagraphRun{{Text: "漢A", Style: s}}, ParagraphOptions{}); p.Advance(0) != 10 {
		t.Errorf("advance without ideograph-alpha %v, want 10", p.Advance(0))
	}
}

// TestShape_SeparatorsAsSpaces locks that under the Chromium fallback rule
// U+2028 and U+2029 are drawn as the face's space and offer no line
// break, though the face holds a glyph of its own for each and UAX #14
// makes them mandatory breaks; under the other rules they break.
func TestShape_SeparatorsAsSpaces(t *testing.T) {
	data := fontbuild.TrueType{
		PostScriptName: "TestSep",
		Family:         "Test Sep",
		Subfamily:      "Regular",
		NotdefAdvance:  1000,
		Glyphs: []fontbuild.Glyph{
			{Name: "V", Unicode: 'V', Advance: 600},
			{Name: "space", Unicode: ' ', Advance: 250, Empty: true},
			{Name: "ls", Unicode: ' ', Advance: 900},
			{Name: "ps", Unicode: ' ', Advance: 900},
		},
	}.Build()
	f := newTestFonts(t, fontFile{name: "sep.ttf", data: data})
	for _, fb := range []Fallback{FallbackChromium, 0} {
		s := Style{Families: []string{"Test Sep"}, Size: 10, Fallback: fb}
		p := shape(t, f, []ParagraphRun{{Text: "V V V", Style: s}}, ParagraphOptions{})
		for _, i := range []int{1, 3} {
			wantBrk := Break(NoBreak)
			if fb != FallbackChromium {
				wantBrk = BreakMandatory
			} else if got := p.Advance(i); math.Abs(got-2.5) > 1e-6 {
				t.Errorf("separator %d advance %v, want the space's 2.5", i, got)
			}
			if got := p.Break(i + 1); got != wantBrk {
				t.Errorf("fallback %v: break after separator %d is %v, want %v", fb, i, got, wantBrk)
			}
		}
	}
}
