package layout

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
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
		{"collapse across elements", doc(el(sty(), txt(sty(), "  a  "), el(inlineSty(), txt(inlineSty(), "  b ")), txt(sty(), "\n c "))), 200, []string{"a b c "}},
		{"pre keeps spaces and breaks", doc(el(sty(ws(css.Pre)), txt(sty(ws(css.Pre)), " a  b\nc"))), 200, []string{" a  b", "c"}},
		{"pre does not wrap", doc(el(sty(ws(css.Pre)), txt(sty(ws(css.Pre)), "aa bb cc"))), 30, []string{"aa bb cc"}},
		{"pre-wrap wraps and keeps", doc(el(sty(ws(css.PreWrap)), txt(sty(ws(css.PreWrap)), "aa  bb"))), 30, []string{"aa  ", "bb"}},
		{"pre-line breaks and collapses", doc(el(sty(ws(css.PreLine)), txt(sty(ws(css.PreLine)), " a   b \n  c"))), 200, []string{"a b", "c"}},
		{"nowrap does not wrap", doc(el(sty(ws(css.Nowrap)), txt(sty(ws(css.Nowrap)), "aa  bb cc"))), 30, []string{"aa bb cc"}},
		{"normal wraps", doc(el(sty(), txt(sty(), "aa bb cc"))), 30, []string{"aa ", "bb ", "cc"}},
		{"forced break", doc(el(sty(), txt(sty(), "a "), br(sty()), txt(sty(), " b"))), 200, []string{"a ", "b"}},
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
		{"sub", css.VerticalAlign{Kind: css.Sub}, 20, 14},
		{"super", css.VerticalAlign{Kind: css.Super}, 21 + 1.0/3, 19 + 1.0/3},
		{"text-top", css.VerticalAlign{Kind: css.TextTop}, 20, 8},
		{"text-bottom", css.VerticalAlign{Kind: css.TextBottom}, 20, 18},
		{"middle", css.VerticalAlign{Kind: css.Middle}, 20, 12.5},
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

func TestTextIndent_FirstLine(t *testing.T) {
	in := func(s *css.Style) { s.TextIndent = css.Pts(px(10)) }
	res := run(t, doc(el(sty(in), txt(sty(in), "aa bb"))), page(40, 50))
	_, pos := lineTexts(res, 1)
	if len(pos) != 2 || !near(pos[0][0], 10) || !near(pos[1][0], 0) {
		t.Errorf("lines at %v, want x 10 then 0", pos)
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

func TestTextDecoration_Underline(t *testing.T) {
	u := sty(func(s *css.Style) { s.TextDecoration = css.Underline; s.Color = css.Color{G: 1, A: 1} })
	res := run(t, doc(el(u, txt(sty(fg(black)), "a"))), page(10, 12))
	img := render(t, res, 1)
	rows := grid(img)
	found := false
	for _, r := range rows[8:] {
		if strings.Contains(r, "G") {
			found = true
		}
	}
	if !found {
		t.Errorf("no lime underline below the baseline:\n%s", strings.Join(rows, "\n"))
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
	var walk func([]compose.Fragment)
	walk = func(fs []compose.Fragment) {
		for _, f := range fs {
			switch v := f.(type) {
			case *compose.Path:
				paths++
				if v.Parent != nil {
					t.Errorf("background hangs from %s", v.Parent.Role)
				}
			case *compose.Group:
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
