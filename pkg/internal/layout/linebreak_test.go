package layout

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

func TestLineBreak_Strictness(t *testing.T) {
	lb := func(v css.LineBreak) func(*css.Style) { return func(s *css.Style) { s.LineBreak = v } }
	tests := []struct {
		name string
		st   *css.Style
		text string
		want []string
	}{
		// Three units a line: a small kana may start a line but under
		// strict.
		{"strict keeps small kana", sty(lb(css.LineBreakStrict)), "漢字漢ぁ漢", []string{"漢字", "漢ぁ漢"}},
		{"normal breaks before small kana", sty(lb(css.LineBreakNormal)), "漢字漢ぁ漢", []string{"漢字漢", "ぁ漢"}},
		{"auto breaks before the prolonged mark", sty(), "漢字漢ー漢", []string{"漢字漢", "ー漢"}},
		{"normal keeps iteration marks", sty(lb(css.LineBreakNormal)), "漢字漢々漢", []string{"漢字", "漢々漢"}},
		{"loose breaks before iteration marks", sty(lb(css.LineBreakLoose)), "漢字漢々漢", []string{"漢字漢", "々漢"}},
		{"no break after an opening bracket", sty(), "漢字「ー漢", []string{"漢字", "「ー漢"}},
		// keep-all closes the opportunities between CJK letters, the ones
		// line-break opens included (CSS Text 3, 5.3).
		{"keep-all keeps small kana", sty(lb(css.LineBreakNormal), func(s *css.Style) { s.WordBreak = css.KeepAll }), "漢字漢ぁ漢", []string{"漢字漢ぁ漢"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := run(t, doc(el(tt.st, txt(tt.st, tt.text))), page(30, 100))
			got, _ := lineTexts(res, 1)
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Errorf("lines %q, want %q", got, tt.want)
			}
		})
	}
}

func TestJustify_CJK(t *testing.T) {
	justify := func(j css.TextJustify) func(*css.Style) {
		return func(s *css.Style) { s.TextAlign, s.TextJustify = css.AlignJustify, j }
	}
	tests := []struct {
		name string
		st   *css.Style
		text string
		want []float64
	}{
		// A 55px line of five 10px units and a sixth that wraps: auto
		// spreads the 5px left over the four gaps between ideographs.
		{"auto between ideographs", sty(justify(css.JustifyAuto)), "漢字漢字漢字", []float64{0, 11.25, 22.5, 33.75, 45}},
		{"inter-word leaves ideographs", sty(justify(css.JustifyInterWord)), "漢字漢字漢字", []float64{0, 10, 20, 30, 40}},
		{"inter-character between letters", sty(justify(css.JustifyInterCharacter)), "abcde f", []float64{0, 11.25, 22.5, 33.75, 45}},
		{"none", sty(justify(css.JustifyNone)), "漢字漢字漢字", []float64{0, 10, 20, 30, 40}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := run(t, doc(el(tt.st, txt(tt.st, tt.text))), page(55, 100))
			var xs []float64
			for _, r := range texts(res, 1) {
				if len(xs) >= 5 {
					break
				}
				for _, g := range r.run.Glyphs {
					if len(xs) < 5 {
						xs = append(xs, (r.x+g.X)/css.Pt)
					}
				}
			}
			for i := range tt.want {
				if i >= len(xs) || !near(xs[i], tt.want[i]) {
					t.Fatalf("glyphs at %v, want %v", xs, tt.want)
				}
			}
		})
	}
}

// TestLineBreak_HangPunctuation checks that an ideographic comma or full
// stop that ends a line and does not fit on it hangs past the line's end
// in a container of css.Office.HangPunctuation, where it would otherwise
// take the character before it to the next line.
//
// # Corpus evidence
//
//   - litho/odt: ja-line-break prints "すべての人間は、" on one line of
//     seven ideographs' width under style:punctuation-wrap hanging, the
//     comma past the line's end, and "すべての人間 / は、" under simple.
func TestLineBreak_HangPunctuation(t *testing.T) {
	hang := func(s *css.Style) { s.HangPunctuation = true }
	tests := []struct {
		name string
		st   *css.Style
		text string
		want []string
	}{
		{"hangs a comma", sty(hang), "漢字漢、漢", []string{"漢字漢、", "漢"}},
		{"hangs a full stop at the end", sty(hang), "漢字漢。", []string{"漢字漢。"}},
		{"no hang without the flag", sty(), "漢字漢、漢", []string{"漢字", "漢、漢"}},
		{"hangs no other punctuation", sty(hang), "漢字漢）漢", []string{"漢字", "漢）漢"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := run(t, doc(el(tt.st, txt(tt.st, tt.text))), page(30, 100))
			got, _ := lineTexts(res, 1)
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Errorf("lines %q, want %q", got, tt.want)
			}
		})
	}
}

// TestLineBreak_KeepHangul checks that css.Office.KeepHangul keeps two
// Hangul syllables on one line, Korean breaking at its spaces alone,
// while ideographs still break between each other.
//
// # Corpus evidence
//
//   - probe, odt: through gotenberg/gotenberg:9.4.1, Korean paragraphs
//     wrap at their spaces alone, Han text between any two ideographs.
//   - litho/ods: cjk-cells and litho/odp: cjk-slides wrap their Korean
//     articles at spaces alone.
func TestLineBreak_KeepHangul(t *testing.T) {
	keep := func(s *css.Style) { s.KeepHangul = true }
	tests := []struct {
		name string
		st   *css.Style
		text string
		want []string
	}{
		{"breaks between syllables", sty(), "가나다라마", []string{"가나다", "라마"}},
		{"keeps syllables together", sty(keep), "가나다라마", []string{"가나다라마"}},
		{"breaks at a space", sty(keep), "가 나다라", []string{"가 ", "나다라"}},
		{"breaks ideographs", sty(keep), "漢字漢字漢", []string{"漢字漢", "字漢"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := run(t, doc(el(tt.st, txt(tt.st, tt.text))), page(30, 100))
			got, _ := lineTexts(res, 1)
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Errorf("lines %q, want %q", got, tt.want)
			}
		})
	}
}

// TestLineBreak_KeepEmoji checks that css.Office.KeepEmoji keeps two
// emoji on one line, a skin tone modifier ending the first, two flags,
// an emoji with the letters before and after it and an emoji after a
// closing parenthesis, the run of them moving whole to the next line,
// while a space and a Han character before an emoji still break.
//
// # Corpus evidence
//
//   - probe, docx: Word sets "A " and ten 36 point emoji, 49.43 points
//     each, on a line 468 points wide as "A" alone, then nine emoji, then
//     the tenth before " Z": no break between two emoji, where UAX #14
//     fills the first line with emoji. It sets "Abcdef" and ten emoji
//     as "Abcdef" and seven emoji, and an emoji before "abc" on the line
//     of the letters (see text.EmojiJoins).
//   - probe, docx: Word breaks no line between two flags nor after ")"
//     before an emoji, and breaks between a Han character and an emoji.
func TestLineBreak_KeepEmoji(t *testing.T) {
	keep := func(s *css.Style) { s.KeepEmoji = true }
	tests := []struct {
		name string
		st   *css.Style
		text string
		want []string
	}{
		{"breaks between emoji", sty(), "A \U0001F600\U0001F602\U0001F970", []string{"A \U0001F600\U0001F602", "\U0001F970"}},
		{"keeps emoji together", sty(keep), "A \U0001F600\U0001F602\U0001F970", []string{"A ", "\U0001F600\U0001F602\U0001F970"}},
		{"after a modifier", sty(keep), "A \U0001F44D\U0001F3FD\U0001F602", []string{"A ", "\U0001F44D\U0001F3FD\U0001F602"}},
		{"breaks after letters", sty(), "A ab\U0001F600\U0001F602", []string{"A ab", "\U0001F600\U0001F602"}},
		{"keeps letters and emoji", sty(keep), "A ab\U0001F600\U0001F602", []string{"A ", "ab\U0001F600\U0001F602"}},
		{"keeps emoji and letters", sty(keep), "A \U0001F600\U0001F602ab", []string{"A ", "\U0001F600\U0001F602ab"}},
		{"breaks between flags", sty(), "A \U0001F1EB\U0001F1F7\U0001F1EF\U0001F1F5", []string{"A \U0001F1EB\U0001F1F7", "\U0001F1EF\U0001F1F5"}},
		{"keeps flags together", sty(keep), "A \U0001F1EB\U0001F1F7\U0001F1EF\U0001F1F5", []string{"A ", "\U0001F1EB\U0001F1F7\U0001F1EF\U0001F1F5"}},
		{"breaks after a parenthesis", sty(), "A ()\U0001F600", []string{"A ()", "\U0001F600"}},
		{"keeps a parenthesis and an emoji", sty(keep), "A ()\U0001F600", []string{"A ", "()\U0001F600"}},
		{"breaks after Han", sty(keep), "A \u6F22\U0001F600\U0001F602", []string{"A \u6F22", "\U0001F600\U0001F602"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := run(t, doc(el(tt.st, txt(tt.st, tt.text))), page(40, 100))
			got, _ := lineTexts(res, 1)
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Errorf("lines %q, want %q", got, tt.want)
			}
		})
	}
}

// TestLineBreak_KeepInWord checks that css.Office.KeepInWord keeps a
// closing parenthesis or bracket, a solidus, a question or an exclamation
// mark on one line with what follows it, the run moving whole to the next
// line, while a space or a hyphen after it still breaks.
func TestLineBreak_KeepInWord(t *testing.T) {
	keep := func(s *css.Style) { s.KeepInWord = true }
	tests := []struct {
		name string
		st   *css.Style
		text string
		want []string
	}{
		{"breaks after a parenthesis", sty(), "A (b)+cd", []string{"A ", "(b)", "+cd"}},
		{"keeps a parenthesis with a sign", sty(keep), "A (b)+cd", []string{"A ", "(b)+cd"}},
		{"keeps a bracket with a parenthesis", sty(keep), "A [b](cd", []string{"A ", "[b](cd"}},
		{"breaks at a space after it", sty(keep), "A (b) cd", []string{"A ", "(b) ", "cd"}},
		{"breaks after a solidus", sty(), "A b/cd", []string{"A b/", "cd"}},
		{"keeps a solidus with a letter", sty(keep), "A b/cd", []string{"A ", "b/cd"}},
		{"keeps a question mark with a letter", sty(keep), "A b?cd", []string{"A ", "b?cd"}},
		{"keeps an exclamation mark with a letter", sty(keep), "A b!cd", []string{"A ", "b!cd"}},
		{"breaks after a hyphen", sty(keep), "A b-cd", []string{"A b-", "cd"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := run(t, doc(el(tt.st, txt(tt.st, tt.text))), page(40, 100))
			got, _ := lineTexts(res, 1)
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Errorf("lines %q, want %q", got, tt.want)
			}
		})
	}
}

// TestLineBreak_ClusterAlone checks that css.Office.ClusterAlone sets a
// character too wide for its line alone on the line under overflow-wrap
// break-word, its space hanging after it, where an Office paragraph lets
// the word overflow.
func TestLineBreak_ClusterAlone(t *testing.T) {
	wrap := func(s *css.Style) { s.OverflowWrap = css.BreakWord }
	alone := func(s *css.Style) { s.OverflowWrap, s.ClusterAlone = css.BreakWord, true }
	tests := []struct {
		name string
		st   *css.Style
		want []string
	}{
		{"overflows by the word", sty(wrap), []string{"ab ", "cd"}},
		{"one character a line", sty(alone), []string{"a", "b ", "c", "d"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := run(t, doc(el(tt.st, txt(tt.st, "ab cd"))), page(1, 100))
			got, _ := lineTexts(res, 1)
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Errorf("lines %q, want %q", got, tt.want)
			}
		})
	}
}

// TestLineBreak_HyphenBeforeDigit checks the opportunity Chromium opens
// after a hyphen-minus between an ASCII letter or digit and a digit, where
// UAX #14, LB25 keeps the hyphen with the number, under
// [Options.BrowserRules] alone.
func TestLineBreak_HyphenBeforeDigit(t *testing.T) {
	keepAll := sty(func(s *css.Style) { s.WordBreak = css.KeepAll })
	tests := []struct {
		name    string
		st      *css.Style
		text    string
		browser bool
		want    []string
	}{
		{"digits", sty(), "12-34-56", true, []string{"12-", "34-", "56"}},
		{"keep-all", keepAll, "12-34-56", true, []string{"12-", "34-", "56"}},
		{"after a letter", sty(), "a-12", true, []string{"a-", "12"}},
		{"minus sign", sty(), "%-12", true, []string{"%-12"}},
		{"office", sty(), "12-34-56", false, []string{"12-34-56"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Three Ahem letters a line.
			res := runWith(t, doc(el(tt.st, txt(tt.st, tt.text))), Options{Page: page(30, 100), BrowserRules: tt.browser})
			got, _ := lineTexts(res, 1)
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Errorf("lines %q, want %q", got, tt.want)
			}
		})
	}
}

// TestLineBreak_WordKeepsASolidus checks that under Word's rules no line
// breaks after a solidus: an address too long for its line breaks where
// the line ends, as overflow-wrap breaks a long word.
func TestLineBreak_WordKeepsASolidus(t *testing.T) {
	word := func(s *css.Style) { s.WordRules, s.OverflowWrap = 15, css.BreakWord }
	css2 := func(s *css.Style) { s.OverflowWrap = css.BreakWord }
	tests := []struct {
		name string
		st   *css.Style
		want []string
	}{
		{"word breaks where the line ends", sty(word), []string{"ab/c", "def"}},
		{"css breaks after the solidus", sty(css2), []string{"ab/", "cdef"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := run(t, doc(el(tt.st, txt(tt.st, "ab/cdef"))), page(40, 100))
			got, _ := lineTexts(res, 1)
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Errorf("lines %q, want %q", got, tt.want)
			}
		})
	}
}
