package layout

import (
	"reflect"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// lineTexts returns the text of every line of page n of res, the runs that
// share a baseline joined, and the left edge and baseline of each line in
// CSS pixels.
func lineTexts(res *Result, n int) ([]string, [][2]float64) {
	var lines []string
	var pos [][2]float64
	for _, r := range texts(res, n) {
		y := r.y / css.Pt
		if len(pos) > 0 && near(pos[len(pos)-1][1], y) {
			lines[len(lines)-1] += r.text
			pos[len(pos)-1][0] = min(pos[len(pos)-1][0], r.x/css.Pt)
			continue
		}
		lines = append(lines, r.text)
		pos = append(pos, [2]float64{r.x / css.Pt, y})
	}
	return lines, pos
}

func TestWhiteSpace_Processing(t *testing.T) {
	ws := func(v css.WhiteSpace) func(*css.Style) { return func(s *css.Style) { s.WhiteSpace = v } }
	tests := []struct {
		name  string
		root  *Node
		width float64
		want  []string
	}{
		// The space that ends the block is removed before shaping (CSS
		// Text 3, 4.1.2), so it kerns with nothing.
		{"collapse across elements", doc(el(sty(), txt(sty(), "  a  "), el(inlineSty(), txt(inlineSty(), "  b ")), txt(sty(), "\n c "))), 200, []string{"a b c"}},
		{"pre keeps spaces and breaks", doc(el(sty(ws(css.Pre)), txt(sty(ws(css.Pre)), " a  b\nc"))), 200, []string{" a  b", "c"}},
		{"pre does not wrap", doc(el(sty(ws(css.Pre)), txt(sty(ws(css.Pre)), "aa bb cc"))), 30, []string{"aa bb cc"}},
		{"pre-wrap wraps and keeps", doc(el(sty(ws(css.PreWrap)), txt(sty(ws(css.PreWrap)), "aa  bb"))), 30, []string{"aa  ", "bb"}},
		{"pre-line breaks and collapses", doc(el(sty(ws(css.PreLine)), txt(sty(ws(css.PreLine)), " a   b \n  c"))), 200, []string{"a b", "c"}},
		{"nowrap does not wrap", doc(el(sty(ws(css.Nowrap)), txt(sty(ws(css.Nowrap)), "aa  bb cc"))), 30, []string{"aa bb cc"}},
		{"normal wraps", doc(el(sty(), txt(sty(), "aa bb cc"))), 30, []string{"aa ", "bb ", "cc"}},
		{"forced break", doc(el(sty(), txt(sty(), "a "), br(sty()), txt(sty(), " b"))), 200, []string{"a ", "b"}},
		// A collapsible space after a preserved newline starts a line and
		// is removed (CSS Text 3, 4.1.2).
		{"space after a preserved newline", doc(el(sty(), txt(inlineSty(ws(css.BreakSpaces)), "ab\n"), txt(sty(), " cd"))), 200, []string{"ab", "cd"}},
		// break-spaces: the spaces take room and wrap after themselves.
		{"break-spaces wraps spaces", doc(el(sty(ws(css.BreakSpaces)), txt(sty(ws(css.BreakSpaces)), "aa  bb"))), 30, []string{"aa ", " bb"}},
		// A control character is drawn as a visible symbol (CSS Text 3,
		// 4.1).
		{"control characters show", doc(el(sty(), txt(sty(), "a\x0eb\u0085"))), 200, []string{"a\u240eb\ufffd"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := run(t, tt.root, page(tt.width, 200))
			got, _ := lineTexts(res, 1)
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Errorf("lines %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTextTransform_Cases(t *testing.T) {
	tr := func(v css.TextTransform) *css.Style { return sty(func(s *css.Style) { s.TextTransform = v }) }
	tests := []struct {
		tt   css.TextTransform
		in   string
		want string
	}{
		{css.Uppercase, "abc é", "ABC É"},
		{css.Lowercase, "ABC", "abc"},
		{css.Capitalize, "hello wide-world don't", "Hello Wide-World Don't"},
	}
	for _, tc := range tests {
		res := run(t, doc(el(tr(tc.tt), txt(tr(tc.tt), tc.in))), page(400, 50))
		if got, _ := lineTexts(res, 1); len(got) != 1 || got[0] != tc.want {
			t.Errorf("transform %d of %q = %q, want %q", tc.tt, tc.in, got, tc.want)
		}
	}
}

func TestTextAlign_Offsets(t *testing.T) {
	al := func(a css.TextAlign, d css.Direction) func(*css.Style) {
		return func(s *css.Style) { s.TextAlign, s.Direction = a, d }
	}
	tests := []struct {
		name string
		mods func(*css.Style)
		want float64
	}{
		{"start", al(css.AlignStart, css.LTR), 0},
		{"right", al(css.AlignRight, css.LTR), 60},
		{"center", al(css.AlignCenter, css.LTR), 30},
		{"end", al(css.AlignEnd, css.LTR), 60},
		{"start in rtl", al(css.AlignStart, css.RTL), 60},
		{"left in rtl", al(css.AlignLeft, css.RTL), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := run(t, doc(el(sty(tt.mods), txt(sty(tt.mods), "ab"))), page(80, 50))
			_, pos := lineTexts(res, 1)
			if len(pos) != 1 || !near(pos[0][0], tt.want) {
				t.Errorf("line at %v, want x %v", pos, tt.want)
			}
		})
	}
}

func TestTextAlign_Justify(t *testing.T) {
	j := func(s *css.Style) { s.TextAlign = css.AlignJustify }
	res := run(t, doc(el(sty(j), txt(sty(j), "a b c dd ee"))), page(60, 50))
	runs := texts(res, 1)
	if len(runs) < 2 {
		t.Fatalf("%d runs", len(runs))
	}
	first := runs[0].run
	// "a b c" is 50px wide in 60px: the two inner spaces take 5px more
	// each, and c ends at the right edge.
	var xs []float64
	for _, g := range first.Glyphs {
		xs = append(xs, (runs[0].x+g.X)/css.Pt)
	}
	want := []float64{0, 10, 25, 35, 50}
	for i, w := range want {
		if i >= len(xs) || !near(xs[i], w) {
			t.Fatalf("justified glyphs at %v, want %v", xs, want)
		}
	}
	if got, _ := lineTexts(res, 1); got[len(got)-1] != "dd ee" {
		t.Errorf("last line %q", got[len(got)-1])
	}
}

func TestLineHeight_HalfLeading(t *testing.T) {
	lh := func(s *css.Style) { s.LineHeight = css.Dimension{Kind: css.Number, Value: 2} }
	rb := boxes(t, doc(el(sty(lh), txt(sty(lh), "a"))), page(50, 50))
	p := kid(t, rb, 0, 0)
	if len(p.lines) != 1 {
		t.Fatalf("%d lines", len(p.lines))
	}
	l := p.lines[0]
	if !near(l.h/css.Pt, 20) || !near(l.baseline/css.Pt, 13) {
		t.Errorf("line height %v baseline %v, want 20 and 13", l.h/css.Pt, l.baseline/css.Pt)
	}
}

func TestVerticalAlign_Kinds(t *testing.T) {
	big := func(va css.VerticalAlign) *Node {
		st := inlineSty(func(s *css.Style) { s.FontSize = px(20); s.VerticalAlign = va })
		return el(st, txt(st, "b"))
	}
	tests := []struct {
		name     string
		va       css.VerticalAlign
		lineH    float64
		baseline float64
	}{
		{"baseline", css.VerticalAlign{}, 20, 16},
		{"top", css.VerticalAlign{Kind: css.AlignTop}, 20, 8},
		{"bottom", css.VerticalAlign{Kind: css.AlignBottom}, 20, 18},
		{"length raises", css.VerticalAlign{Kind: css.AlignLength, Length: css.Pts(px(5))}, 23, 21},
		{"length lowers", css.VerticalAlign{Kind: css.AlignLength, Length: css.Pts(px(-5))}, 20, 11},
		{"sub", css.VerticalAlign{Kind: css.Sub}, 20, 14},
		{"super", css.VerticalAlign{Kind: css.Super}, 21 + 1.0/3, 19 + 1.0/3},
		{"text-top", css.VerticalAlign{Kind: css.TextTop}, 20, 8},
		{"text-bottom", css.VerticalAlign{Kind: css.TextBottom}, 20, 18},
		// Ahem's x-height is 0.8 em: the box's middle sits 4px above the
		// parent's baseline.
		{"middle", css.VerticalAlign{Kind: css.Middle}, 20, 14},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rb := boxes(t, doc(el(sty(), txt(sty(), "a"), big(tt.va))), page(100, 50))
			l := kid(t, rb, 0, 0).lines[0]
			if !near(l.h/css.Pt, tt.lineH) || !near(l.baseline/css.Pt, tt.baseline) {
				t.Errorf("line height %v baseline %v, want %v and %v", l.h/css.Pt, l.baseline/css.Pt, tt.lineH, tt.baseline)
			}
		})
	}
}

// TestVerticalAlign_BlinkScriptShift locks the shift of sub and super
// under BrowserRules: CSS 2.1, 10.8.1 leaves "the proper position" to the
// user agent, and Blink moves the baseline by the fifth (sub) or the third
// (super) of the parent's font size, floored to 1/64 px, plus one pixel.
// The 20 px Ahem box sits under a 10 px parent.
func TestVerticalAlign_BlinkScriptShift(t *testing.T) {
	big := func(va css.VerticalAlign) *Node {
		st := inlineSty(func(s *css.Style) { s.FontSize = px(20); s.VerticalAlign = va })
		return el(st, txt(st, "b"))
	}
	tests := []struct {
		name     string
		va       css.VerticalAlign
		lineH    float64
		baseline float64
	}{
		// Lowered by 2 + 1 px: 16 above, 3 + 4 below.
		{"sub", css.VerticalAlign{Kind: css.Sub}, 20, 13},
		// Raised by 213/64 + 1 px.
		{"super", css.VerticalAlign{Kind: css.Super}, 22 + 21.0/64, 20 + 21.0/64},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pb := page(100, 50)
			rb := boxesWith(t, doc(el(sty(), txt(sty(), "a"), big(tt.va))), Options{Page: pb, BrowserRules: true})
			l := kid(t, rb, 0, 0).lines[0]
			if !near(l.h/css.Pt, tt.lineH) || !near(l.baseline/css.Pt, tt.baseline) {
				t.Errorf("line height %v baseline %v, want %v and %v", l.h/css.Pt, l.baseline/css.Pt, tt.lineH, tt.baseline)
			}
		})
	}
}

func TestTextIndent_FirstLine(t *testing.T) {
	in := func(s *css.Style) { s.TextIndent = css.Pts(px(10)) }
	res := run(t, doc(el(sty(in), txt(sty(in), "aa bb"))), page(40, 50))
	_, pos := lineTexts(res, 1)
	if len(pos) != 2 || !near(pos[0][0], 10) || !near(pos[1][0], 0) {
		t.Errorf("lines at %v, want x 10 then 0", pos)
	}
}

// TestTextIndent_NotAfterABlock locks CSS Text 3, 8.1: text-indent
// indents the element's first formatted line alone, so the anonymous block
// holding text after a block child is not indented, while one before any
// block child is.
func TestTextIndent_NotAfterABlock(t *testing.T) {
	in := func(s *css.Style) { s.TextIndent = css.Pts(px(10)) }
	res := run(t, doc(el(sty(in), txt(sty(in), "aa"), el(sty(in), txt(sty(in), "bb")), txt(sty(in), "cc"))), page(40, 50))
	got, pos := lineTexts(res, 1)
	if len(pos) != 3 || !near(pos[0][0], 10) || !near(pos[1][0], 10) || !near(pos[2][0], 0) {
		t.Errorf("lines %q at %v, want x 10, 10 then 0", got, pos)
	}
}

// TestLineHeight_ZeroSizeText locks CSS 2.1, 9.4.2 and 10.8: a line
// holding text at font size 0 is not empty, so it takes the block's
// strut; one holding collapsible white space alone at that size is.
func TestLineHeight_ZeroSizeText(t *testing.T) {
	z := func(s *css.Style) { s.FontSize = 0 }
	for _, tc := range []struct {
		text string
		want float64
	}{{"zero", 10}, {" \n ", 0}} {
		rb := boxes(t, doc(el(sty(), el(sty(display(css.Inline), z), txt(sty(z), tc.text)))), page(100, 100))
		if got := kid(t, rb, 0).h / css.Pt; !near(got, tc.want) {
			t.Errorf("%q: block height %v, want %v", tc.text, got, tc.want)
		}
	}
}

func TestLetterSpacing_Widens(t *testing.T) {
	ls := func(s *css.Style) { s.LetterSpacing = css.Pts(px(2)) }
	res := run(t, doc(el(sty(ls), txt(sty(ls), "abc"))), page(100, 50))
	r := texts(res, 1)[0].run
	if len(r.Glyphs) != 3 || !near(r.Glyphs[2].X/css.Pt, 24) {
		t.Errorf("third glyph at %v, want 24", r.Glyphs[2].X/css.Pt)
	}
}

func TestOverflowWrap_BreaksLongWord(t *testing.T) {
	ow := func(s *css.Style) { s.OverflowWrap = css.Anywhere }
	res := run(t, doc(el(sty(ow), txt(sty(ow), "abcdefg"))), page(30, 50))
	if got, _ := lineTexts(res, 1); strings.Join(got, "|") != "abc|def|g" {
		t.Errorf("lines %q", got)
	}
	res = run(t, doc(el(sty(), txt(sty(), "abcdefg"))), page(30, 50))
	if got, _ := lineTexts(res, 1); strings.Join(got, "|") != "abcdefg" {
		t.Errorf("normal: lines %q, want one overflowing line", got)
	}
	ba := func(s *css.Style) { s.WordBreak = css.BreakAll }
	res = run(t, doc(el(sty(ba), txt(sty(ba), "ab cdefg"))), page(30, 50))
	if got, _ := lineTexts(res, 1); strings.Join(got, "|") != "ab |cde|fg" {
		t.Errorf("break-all: lines %q", got)
	}
}

func TestLineBreak_Anywhere(t *testing.T) {
	lb := func(s *css.Style) { s.LineBreak = css.LineBreakAnywhere }
	res := run(t, doc(el(sty(lb), txt(sty(lb), "abc de"))), page(20, 50))
	if got, _ := lineTexts(res, 1); strings.Join(got, "|") != "ab|c |de" {
		t.Errorf("lines %q, want ab|c |de: a break between any two characters", got)
	}
}

func TestLineBreak_AnywhereHangsPreservedSpaces(t *testing.T) {
	// CSS Text 3, 5.3 and 4.1.3: under pre-wrap a preserved space hangs at
	// the end of the line before it rather than taking a line of its own.
	lb := func(s *css.Style) { s.LineBreak = css.LineBreakAnywhere; s.WhiteSpace = css.PreWrap }
	res := run(t, doc(el(sty(lb, width(0)), txt(sty(lb), "2. ab"))), page(100, 100))
	if got, _ := lineTexts(res, 1); strings.Join(got, "|") != "2|. |a|b" {
		t.Errorf("lines %q, want 2|. |a|b", got)
	}
}

func TestOverflowWrap_AnywhereInNoRoom(t *testing.T) {
	// CSS Text 3, 5.5: overflow-wrap: anywhere and break-word break an
	// unbreakable word at any grapheme boundary when it does not fit, the
	// first cluster of a line included: in no room each takes a line.
	for _, ow := range []css.OverflowWrap{css.Anywhere, css.BreakWord} {
		o := func(s *css.Style) { s.OverflowWrap = ow }
		res := runWith(t, doc(el(sty(o, width(0)), txt(sty(o), "abc"))), Options{Page: page(100, 100), BrowserRules: true})
		if got, _ := lineTexts(res, 1); strings.Join(got, "|") != "a|b|c" {
			t.Errorf("overflow-wrap %v: lines %q, want a|b|c", ow, got)
		}
	}
}

func TestWordBreak_BreakAllAtSpanEdge(t *testing.T) {
	ba := inlineSty(func(s *css.Style) { s.WordBreak = css.BreakAll })
	res := run(t, doc(el(sty(), txt(sty(), "aa"), el(ba, txt(ba, "bb")), txt(sty(), "cc"))), page(40, 50))
	// break-all on the span opens the boundary after its last letter.
	if got, _ := lineTexts(res, 1); strings.Join(got, "|") != "aabb|cc" {
		t.Errorf("lines %q, want aabb|cc", got)
	}
}

func TestWordBreak_ManualKeepsThaiWords(t *testing.T) {
	// Thai breaks between the words of its word list (UAX #14, class SA,
	// LB1); word-break: manual finds no such boundary (CSS Text 4, 5.2),
	// and a zero width space still breaks.
	res := run(t, doc(el(sty(width(0)), txt(sty(), "กรุงเทพคือสวยงาม"))), page(100, 100))
	if got, _ := lineTexts(res, 1); strings.Join(got, "|") != "กรุงเทพ|คือ|สวยงาม" {
		t.Errorf("word-break: normal: lines %q, want กรุงเทพ|คือ|สวยงาม", got)
	}
	manual := func(s *css.Style) { s.WordBreak = css.WordBreakManual }
	res = run(t, doc(el(sty(manual, width(0)), txt(sty(manual), "กรุงเทพคือ\u200bสวยงาม"))), page(100, 100))
	if got, _ := lineTexts(res, 1); strings.Join(got, "|") != "กรุงเทพคือ\u200b|สวยงาม" {
		t.Errorf("word-break: manual: lines %q, want กรุงเทพคือ\u200b|สวยงาม", got)
	}
}

func TestWordSpaceTransform_ZeroWidthSpaces(t *testing.T) {
	wst := func(s *css.Style) { s.WordSpaceTransform = css.WordSpaceSpace }
	res := run(t, doc(el(sty(wst), txt(sty(wst), "\u200baa\u200bbb\u200b"))), page(200, 50))
	// The word boundary becomes a space; the ones at the ends mark none.
	if got, _ := lineTexts(res, 1); strings.Join(got, "|") != "\u200baa bb\u200b" {
		t.Errorf("lines %q", got)
	}
}

func TestTextAlign_LastLine(t *testing.T) {
	tests := []struct {
		align, last css.TextAlign
		isLast      bool
		want        css.TextAlign
	}{
		{css.AlignJustify, css.AlignAuto, false, css.AlignJustify},
		{css.AlignJustify, css.AlignAuto, true, css.AlignStart},
		{css.AlignJustifyAll, css.AlignAuto, true, css.AlignJustify},
		{css.AlignCenter, css.AlignAuto, true, css.AlignCenter},
		{css.AlignJustify, css.AlignRight, true, css.AlignRight},
		{css.AlignJustify, css.AlignRight, false, css.AlignJustify},
	}
	for _, tt := range tests {
		st := css.Initial()
		st.TextAlign, st.TextAlignLast = tt.align, tt.last
		if got := lineAlign(st, tt.isLast); got != tt.want {
			t.Errorf("text-align %v, text-align-last %v, last %v: %v, want %v", tt.align, tt.last, tt.isLast, got, tt.want)
		}
	}
}

func TestHangingSpaces_DrawnButNotAligned(t *testing.T) {
	pw := func(s *css.Style) { s.WhiteSpace = css.PreWrap; s.FontSize = px(4); s.TextAlign = css.AlignRight }
	span := inlineSty(pw, bg(red), fg(blue))
	res := run(t, doc(el(sty(pw), el(span, txt(span, "a ")))), page(8, 4))
	// The trailing preserved space hangs past the right edge: the glyph
	// aligns flush right, the space's background is not drawn inside.
	assertPixels(t, res, 1, `
		____BBBB
		____BBBB
		____BBBB
		____BBBB`)
}

func TestFloat_AtParagraphEndBesideLastLine(t *testing.T) {
	f := el(sty(float(css.FloatRight), width(10), height(10)))
	rb := boxes(t, doc(el(sty(), txt(sty(), "a"), f)), page(100, 100))
	p := kid(t, rb, 0, 0)
	if got := rect(p.kids[1]); got[1] != 0 {
		t.Errorf("float at %v, want y 0: beside the line it ends", got)
	}
}

func TestTabs_ToTabStops(t *testing.T) {
	pre := func(s *css.Style) { s.WhiteSpace = css.Pre; s.TabSize = css.Dimension{Kind: css.Number, Value: 4} }
	res := run(t, doc(el(sty(pre), txt(sty(pre), "a\tb"))), page(200, 50))
	runs := texts(res, 1)
	last := runs[len(runs)-1]
	g := last.run.Glyphs[len(last.run.Glyphs)-1]
	if x := (last.x + g.X) / css.Pt; !near(x, 40) {
		t.Errorf("b at %v, want 40: the first tab stop of 4 spaces", x)
	}
}

func TestDirection_RTLOrders(t *testing.T) {
	rtl := func(s *css.Style) { s.Direction = css.RTL }
	res := run(t, doc(el(sty(rtl), txt(sty(rtl), "ab cd"))), page(100, 50))
	_, pos := lineTexts(res, 1)
	if len(pos) != 1 || !near(pos[0][0], 50) {
		t.Errorf("line at %v, want x 50: aligned right", pos)
	}
	ov := inlineSty(func(s *css.Style) { s.UnicodeBidi = css.BidiOverride; s.Direction = css.RTL })
	res = run(t, doc(el(sty(), el(ov, txt(ov, "abc")))), page(100, 50))
	r := texts(res, 1)[0]
	var b strings.Builder
	for _, g := range r.run.Glyphs {
		b.WriteString(g.Text)
	}
	if b.String() != "cba" {
		t.Errorf("override drew %q, want cba", b.String())
	}
}

func TestInlineBox_EdgesAndPainting(t *testing.T) {
	span := inlineSty(bg(red), padding(0, 2), margin(0, 1), func(s *css.Style) { s.FontSize = px(4) })
	res := run(t, doc(el(sty(func(s *css.Style) { s.FontSize = px(4) }), el(span, txt(span, "")), el(span, txt(sty(func(s *css.Style) { s.FontSize = px(4); s.Color = css.Color{B: 1, A: 1} }), "a")))), page(16, 4))
	// The empty span takes its edges: 1 + 2 + 2 + 1; the second draws its
	// padding around a 4px blue square.
	assertPixels(t, res, 1, `
		_RRRR__RRBBBBRR_
		_RRRR__RRBBBBRR_
		_RRRR__RRBBBBRR_
		_RRRR__RRBBBBRR_`)
}

func TestInlineBox_SplitAcrossLinesRTL(t *testing.T) {
	sm := func(s *css.Style) { s.FontSize = px(4); s.Direction = css.RTL }
	span := inlineSty(sm, func(s *css.Style) {
		s.Border = [4]css.Border{{}, {Width: px(1), Style: css.Solid, Color: red}, {}, {Width: px(1), Style: css.Solid, Color: red}}
	}, fg(blue))
	res := run(t, doc(el(sty(sm), el(span, txt(span, "a b")))), page(8, 8))
	// A right-to-left box starts on its right: the first line draws the
	// right border, the second the left one (CSS 2.1, 8.6).
	assertPixels(t, res, 1, `
		___BBBBR
		___BBBBR
		___BBBBR
		___BBBBR
		___RBBBB
		___RBBBB
		___RBBBB
		___RBBBB`)
}

func TestInlineBox_SplitAcrossLines(t *testing.T) {
	sm := func(s *css.Style) { s.FontSize = px(4) }
	span := inlineSty(sm, func(s *css.Style) {
		s.Border = [4]css.Border{{}, {Width: px(1), Style: css.Solid, Color: red}, {}, {Width: px(1), Style: css.Solid, Color: red}}
	}, fg(blue))
	res := run(t, doc(el(sty(sm), el(span, txt(span, "a b")))), page(8, 8))
	// The start edge sits on the first line, the end edge on the second
	// (box-decoration-break: slice).
	assertPixels(t, res, 1, `
		RBBBB___
		RBBBB___
		RBBBB___
		RBBBB___
		BBBBR___
		BBBBR___
		BBBBR___
		BBBBR___`)
}

func TestTextDecoration_Lines(t *testing.T) {
	lime := css.Color{G: 1, A: 1}
	tests := []struct {
		name  string
		line  css.TextDecoration
		text  css.Color
		set   func(*css.Style)
		where func(row int) bool
	}{
		// An underline paints under the glyphs: with transparent text it
		// shows below the baseline (CSS Text Decoration 3, 2.4).
		{"underline", css.Underline, css.Color{}, nil, func(r int) bool { return r >= 8 }},
		// A line-through paints over the glyphs, above the baseline.
		{"line-through", css.LineThrough, black, nil, func(r int) bool { return r < 8 }},
		// text-underline-offset moves the underline below the glyphs.
		{"offset", css.Underline, black, func(s *css.Style) { s.TextUnderlineOffset = css.Pts(3) }, func(r int) bool { return r >= 10 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := sty(func(s *css.Style) {
				s.TextDecoration = tt.line
				s.TextDecorationColor = lime
				if tt.set != nil {
					tt.set(s)
				}
			})
			res := run(t, doc(el(u, txt(sty(fg(tt.text)), "a"))), page(10, 14))
			rows := grid(render(t, res, 1))
			found := false
			for r, row := range rows {
				if strings.Contains(row, "G") {
					if !tt.where(r) {
						t.Fatalf("lime at row %d:\n%s", r, strings.Join(rows, "\n"))
					}
					found = true
				}
			}
			if !found {
				t.Errorf("no lime line:\n%s", strings.Join(rows, "\n"))
			}
		})
	}
}

func TestBreak_MakesEmptyLine(t *testing.T) {
	rb := boxes(t, doc(el(sty(), txt(sty(), "a"), br(sty()), br(sty()), txt(sty(), "b"))), page(50, 50))
	if n := len(kid(t, rb, 0, 0).lines); n != 3 {
		t.Errorf("%d lines, want 3: a, an empty one, b", n)
	}
}

func TestAnonymousBlocks_WrapInlineRuns(t *testing.T) {
	rb := boxes(t, doc(el(sty(), txt(sty(), "a"), el(sty(height(5))), txt(sty(), "b"))), page(50, 50))
	p := kid(t, rb, 0, 0)
	if len(p.kids) != 3 || !p.kids[0].anon || p.kids[1].anon || !p.kids[2].anon {
		t.Fatalf("kids %d, want an anonymous block, the block, an anonymous block", len(p.kids))
	}
	if got := rect(p.kids[2])[1]; got != 15 {
		t.Errorf("second anonymous block at %v, want 15", got)
	}
}

func TestBlockInInline_Splits(t *testing.T) {
	span := inlineSty(border(1, css.Solid, red))
	rb := boxes(t, doc(el(sty(), el(span, txt(span, "a"), el(sty(height(5))), txt(span, "b")))), page(50, 50))
	p := kid(t, rb, 0, 0)
	if len(p.kids) != 3 {
		t.Fatalf("%d kids, want 3", len(p.kids))
	}
	first := p.kids[0].kids[0]
	last := p.kids[2].kids[0]
	if !first.first || first.last || last.first || !last.last {
		t.Errorf("pieces first/last %v %v, %v %v", first.first, first.last, last.first, last.last)
	}
}

func TestStructure_RolesAndArtifacts(t *testing.T) {
	p := el(sty(), txt(sty(), "a"))
	p.Role = "P"
	h := el(sty(bg(red)), txt(sty(), "b"))
	h.Role = "H1"
	h.Lang = "fr"
	res := run(t, doc(h, p), page(50, 50))
	root := res.Structure
	if root.Role != "Document" || len(root.Kids) != 2 || root.Kids[0].Role != "H1" || root.Kids[1].Role != "P" || root.Kids[0].Lang != "fr" {
		t.Fatalf("structure %+v", root)
	}
	for _, r := range texts(res, 1) {
		if r.run.Parent == nil {
			t.Errorf("run %q is an artifact", r.text)
		}
	}
	var paths int
	var walk func([]typeset.Fragment)
	walk = func(fs []typeset.Fragment) {
		for _, f := range fs {
			switch v := f.(type) {
			case *typeset.Path:
				paths++
				if v.Parent != nil {
					t.Errorf("background hangs from %s", v.Parent.Role)
				}
			case *typeset.Group:
				walk(v.Content)
			}
		}
	}
	walk(res.Pages[0].Content)
	if paths == 0 {
		t.Error("no background painted")
	}
}

func TestDraw_AhemSquares(t *testing.T) {
	st := sty(fg(blue), func(s *css.Style) { s.FontSize = px(4); s.LineHeight = css.Dimension{Kind: css.Number, Value: 2} })
	res := run(t, doc(el(st, txt(st, "a b"))), page(10, 16))
	assertPixels(t, res, 1, `
		__________
		__________
		BBBB______
		BBBB______
		BBBB______
		BBBB______
		__________
		__________
		__________
		__________
		BBBB______
		BBBB______
		BBBB______
		BBBB______
		__________
		__________`)
}

func TestFloats_InLineMovesTheLine(t *testing.T) {
	f := el(sty(float(css.FloatLeft), width(20), height(5)))
	rb := boxes(t, doc(el(sty(), txt(sty(), "aa "), f, txt(sty(), "bb"))), page(80, 50))
	p := kid(t, rb, 0, 0)
	if len(p.lines) != 1 || !near(p.lines[0].x/css.Pt, 20) {
		t.Fatalf("lines %d at %v, want one line beside the float at 20", len(p.lines), p.lines[0].x/css.Pt)
	}
	if got := rect(p.kids[1]); got[0] != 0 || got[1] != 0 {
		t.Errorf("float at %v, want the line's top left", got)
	}
	rb = boxes(t, doc(el(sty(), txt(sty(), "aa "), f, txt(sty(), "bb cc dd"))), page(80, 50))
	p = kid(t, rb, 0, 0)
	if got := rect(p.kids[1]); got[1] != 10 {
		t.Errorf("float at %v, want below the full first line", got)
	}
}

// Under BrowserRules a float that fits beside the content before it goes
// at the top of its line, however full the line, which is broken again
// beside it, as Chromium places it.
func TestFloats_InFullLineBesideTheContentBefore(t *testing.T) {
	f := el(sty(float(css.FloatLeft), width(20), height(5)))
	rb := boxesWith(t, doc(el(sty(), txt(sty(), "aa "), f, txt(sty(), "bb cc dd"))), Options{Page: page(80, 50), BrowserRules: true})
	p := kid(t, rb, 0, 0)
	if got := rect(p.kids[1]); got[0] != 0 || got[1] != 0 {
		t.Errorf("float at %v, want the first line's top left", got)
	}
	if len(p.lines) == 0 || !near(p.lines[0].x/css.Pt, 20) {
		t.Errorf("first line not beside the float")
	}
	for _, l := range p.lines {
		if w := contentWidth(l); w > l.w+eps {
			t.Errorf("line content %v wider than the line, %v", w/css.Pt, l.w/css.Pt)
		}
	}
}

func TestSmallCaps_Synthesised(t *testing.T) {
	sc := func(s *css.Style) { s.FontVariant = css.SmallCaps }
	res := run(t, doc(el(sty(sc), txt(sty(sc), "Ab1c"))), page(100, 20))
	runs := texts(res, 1)
	type piece struct {
		text string
		size float64
	}
	var got []piece
	for _, r := range runs {
		got = append(got, piece{r.text, r.run.Size / css.Pt})
	}
	// CSS Fonts 4, 6.6: the lowercase letters become capitals at 0.7 em,
	// the rest keeps the size.
	want := []piece{{"A", 10}, {"B", 7}, {"1", 10}, {"C", 7}}
	if len(got) != len(want) {
		t.Fatalf("runs %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i].text != want[i].text || !near(got[i].size, want[i].size) {
			t.Fatalf("runs %+v, want %+v", got, want)
		}
	}
	// B starts after A's 10px em square, 1 after B's 7px one.
	if !near(runs[1].x-runs[0].x, px(10)) || !near(runs[2].x-runs[1].x, px(7)) {
		t.Errorf("runs at %v, %v, %v", runs[0].x, runs[1].x, runs[2].x)
	}
}

// TestSmallCaps_Pieces checks how a font-variant-caps value cuts and maps
// text in a face by its support, after Chromium's OpenTypeCapsSupport:
// synthesis uppercases the lowercase letters at the small size, a
// combining mark goes with its base, all-small-caps shrinks every
// character, unicase in a face with smcp lowercases the capitals and
// shapes them with smcp, and a face with the features shapes them.
// Under the full case mapping, as Chromium cases them, a letter whose
// capital is two characters is uppercased too.
func TestSmallCaps_Pieces(t *testing.T) {
	small := capsPlan{caps: css.SmallCaps, support: capsNone, synth: synthLower, allowed: true}
	full := small
	full.full = true
	smcp := []text.Feature{{Tag: "smcp", Value: 1}}
	for _, tc := range []struct {
		in   string
		plan capsPlan
		want []smallCapsPiece
	}{
		{"abc", capsPlan{}, []smallCapsPiece{{text: "abc"}}},
		{"abc", small, []smallCapsPiece{{text: "ABC", small: true, source: "abc"}}},
		{"Hé 2", small, []smallCapsPiece{{text: "H"}, {text: "É", small: true, source: "é"}, {text: " 2"}}},
		{"He\u0301!", small, []smallCapsPiece{{text: "H"}, {text: "E\u0301", small: true, source: "e\u0301"}, {text: "!"}}},
		// ß has no one-rune uppercase: under the simple mapping it keeps
		// its form and size.
		{"ß", small, []smallCapsPiece{{text: "ß"}}},
		// j and a combining caron are canonically U+01F0, which has no
		// one-rune uppercase either (CSS Fonts 4, 5.4).
		{"aj\u030C", small, []smallCapsPiece{{text: "A", small: true, source: "a"}, {text: "j\u030C"}}},
		{"ß", full, []smallCapsPiece{{text: "SS", small: true, source: "ß"}}},
		{"aj\u030C", full, []smallCapsPiece{{text: "AJ\u030C", small: true, source: "aj\u030C"}}},
		{"\u01F0H\u1E96", full, []smallCapsPiece{{text: "J\u030C", small: true, source: "\u01F0"}, {text: "H"}, {text: "H\u0331", small: true, source: "\u1E96"}}},
		{"", small, nil},
		{"aB1", capsPlan{caps: css.AllSmallCaps, support: capsNone, synth: synthBoth, allowed: true},
			[]smallCapsPiece{{text: "A", small: true, source: "a"}, {text: "B1", small: true}}},
		{"aB", capsPlan{caps: css.Unicase, support: capsFallback, synth: synthUpper, allowed: true},
			[]smallCapsPiece{{text: "a"}, {text: "b", features: smcp, source: "B"}}},
		{"aB", capsPlan{caps: css.Unicase, support: capsNone, synth: synthUpper, allowed: true},
			[]smallCapsPiece{{text: "a"}, {text: "B", small: true}}},
		{"aB", capsPlan{caps: css.SmallCaps, allowed: true}, []smallCapsPiece{{text: "aB", features: smcp}}},
		{"aB", capsPlan{caps: css.SmallCaps, support: capsNone, synth: synthLower}, []smallCapsPiece{{text: "aB"}}},
		{"aB", capsPlan{caps: css.TitlingCaps, support: capsNone, allowed: true}, []smallCapsPiece{{text: "aB"}}},
	} {
		if got := smallCaps(tc.in, tc.plan); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("smallCaps(%q, %+v) = %+v, want %+v", tc.in, tc.plan, got, tc.want)
		}
	}
}

// TestLineHeight_EmptyInline locks CSS 2.1, 10.8: an empty inline box
// keeps its line height, which grows the line it sits on, while a line of
// empty inline boxes alone is no line (9.4.2).
func TestLineHeight_EmptyInline(t *testing.T) {
	t.Parallel()
	tall := func(s *css.Style) { s.LineHeight = css.Dimension{Kind: css.Number, Value: 5} }
	rb := boxes(t, doc(el(sty(), el(inlineSty(tall)), txt(sty(), "X"))), page(100, 200))
	if got := rect(kid(t, rb, 0, 0))[3]; got != 50 {
		t.Errorf("block height %v, want 50: the empty span's line height", got)
	}
	rb = boxes(t, doc(el(sty(), el(inlineSty(tall)))), page(100, 200))
	if got := rect(kid(t, rb, 0, 0))[3]; got != 0 {
		t.Errorf("block height %v, want 0: an empty span alone makes no line", got)
	}
}

// TestVerticalAlign_TopAlignsTheSubtree locks CSS 2.1, 10.8.1: a top
// aligned box aligns the top of its aligned subtree, its descendants'
// boxes included, with the top of the line box, which grows to hold it.
func TestVerticalAlign_TopAlignsTheSubtree(t *testing.T) {
	t.Parallel()
	top := inlineSty(func(s *css.Style) { s.VerticalAlign = css.VerticalAlign{Kind: css.AlignTop} })
	big := inlineSty(func(s *css.Style) { s.FontSize = px(20) })
	rb := boxes(t, doc(el(sty(), txt(sty(), "a"), el(top, el(big, txt(big, "b")), txt(top, "c")))), page(100, 50))
	l := kid(t, rb, 0, 0).lines[0]
	if !near(l.h/css.Pt, 20) || !near(l.baseline/css.Pt, 8) {
		t.Errorf("line height %v baseline %v, want 20 and 8", l.h/css.Pt, l.baseline/css.Pt)
	}
	for _, it := range l.items {
		if it.text != nil && it.box.st == big && !near(it.baseline/css.Pt, 16) {
			t.Errorf("the 20px text sits on %v, want 16: its top at the line's", it.baseline/css.Pt)
		}
	}
}

// TestLetterSpacing_PercentOfFontSize checks a percentage of
// letter-spacing and word-spacing, one of the element's own font size
// (CSS Text 4), resolved on each element that inherits it.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 spaces the letters of a 32px span inside
//     a 16px paragraph of letter-spacing: 50% by 16px, and its words
//     under word-spacing: 100% by 32px more than a space.
func TestLetterSpacing_PercentOfFontSize(t *testing.T) {
	t.Parallel()
	ls := func(s *css.Style) { s.LetterSpacing = css.Dimension{Kind: css.Percent, Value: 0.2} }
	big := func(s *css.Style) { s.FontSize *= 2; s.LetterSpacing = css.Dimension{Kind: css.Percent, Value: 0.2} }
	res := run(t, doc(el(sty(ls), txt(sty(ls), "abc"))), page(100, 50))
	small := texts(res, 1)[0].run
	res = run(t, doc(el(sty(big), txt(sty(big), "abc"))), page(200, 50))
	large := texts(res, 1)[0].run
	if len(small.Glyphs) != 3 || len(large.Glyphs) != 3 {
		t.Fatalf("glyphs %d and %d", len(small.Glyphs), len(large.Glyphs))
	}
	if a, b := small.Glyphs[2].X, large.Glyphs[2].X; !near(b, 2*a) {
		t.Errorf("third glyph at %v and %v: the spacing does not follow the font size", a, b)
	}
	plain := run(t, doc(el(sty(), txt(sty(), "abc"))), page(100, 50))
	if p := texts(plain, 1)[0].run.Glyphs[2].X; !(small.Glyphs[2].X > p) {
		t.Errorf("third glyph at %v, unspaced %v", small.Glyphs[2].X, p)
	}
}
