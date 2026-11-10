package layout

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
)

// placedGlyph is one glyph of a page with its origin in CSS pixels.
type placedGlyph struct {
	text string
	x, y float64
}

// glyphsOf returns the glyphs of page n of res in paint order, their
// origins in CSS pixels with the run's horizontal scale applied.
func glyphsOf(res *Result, n int) []placedGlyph {
	var out []placedGlyph
	for _, r := range texts(res, n) {
		hs := hscale(r.run)
		for _, g := range r.run.Glyphs {
			out = append(out, placedGlyph{text: g.Text, x: (r.x + g.X*hs) / css.Pt, y: (r.y + g.Y) / css.Pt})
		}
	}
	return out
}

// glyphLayout renders the glyphs of page n of res as "text@x,y" words,
// positions rounded to tenths of a pixel.
func glyphLayout(res *Result, n int) string {
	var b strings.Builder
	for _, g := range glyphsOf(res, n) {
		if g.text == "" || g.text == " " {
			continue
		}
		fmt.Fprintf(&b, "%s@%g,%g ", g.text, round1(g.x), round1(g.y))
	}
	return strings.TrimSpace(b.String())
}

func round1(v float64) float64 {
	r := float64(int64(v*10+0.5*sign(v))) / 10
	if r == 0 {
		return 0
	}
	return r
}

func sign(v float64) float64 {
	if v < 0 {
		return -1
	}
	return 1
}

// officeSty is an Ahem paragraph of 10px glyphs with Office tab stops at
// the given px positions and alignments, its white space preserved.
func officeSty(stops []css.TabStop, mods ...func(*css.Style)) *css.Style {
	return sty(append([]func(*css.Style){func(s *css.Style) {
		s.WhiteSpace = css.PreWrap
		s.TabStops = stops
		if s.TabStops == nil {
			s.TabStops = []css.TabStop{}
		}
		s.TabSize = css.Pts(px(40))
	}}, mods...)...)
}

// stop returns a tab stop at px position pos.
func stop(pos float64, a css.TabAlign, l css.TabLeader) css.TabStop {
	return css.TabStop{Pos: px(pos), Align: a, Leader: l}
}

// TestTabs_Office lays tabs out as LibreOffice prints the same paragraphs
// of a document converted through Gotenberg's LibreOffice route.
//
// # Corpus evidence
//
//   - LibreOffice 9.0.0 image: a .fodt in Liberation Mono 10pt, tab stops
//     at 1in left, 2.5in centre, 4in right, 5in decimal: "a", "L", "CCCC"
//     centred, "RR" ending on its stop, "12.345" with "12" before it; a
//     dotted leader filling to a right stop; text past a stop going to the
//     next 0.5in default; "abc" before a right stop too close for its text
//     starting the text at "abc"'s end; a stop at 8in, past the line's
//     end, sending the next text to the next line; the default stop at
//     the line's end moving the tab to the next line, where it advances to
//     0.5in; a stop with a word too wide after it leaving a line holding
//     the tab alone; "1234,5" on a French decimal stop.
func TestTabs_Office(t *testing.T) {
	L, C, R, D := css.TabLeft, css.TabCenter, css.TabRight, css.TabDecimal
	none := css.TabLeaderNone
	tests := []struct {
		name  string
		stops []css.TabStop
		text  string
		width float64
		mods  []func(*css.Style)
		lang  string
		want  string
	}{
		{
			"left, centre, right and decimal",
			[]css.TabStop{stop(50, L, none), stop(100, C, none), stop(150, R, none), stop(200, D, none)},
			"a\tL\tCC\tRR\t1.2", 300, nil, "", "a@0,8 L@50,8 C@90,8 C@100,8 R@130,8 R@140,8 1@190,8 .@200,8 2@210,8",
		},
		{
			"past the custom stops the default ones",
			[]css.TabStop{stop(25, L, none)},
			"abc\tx\ty", 300, nil, "", "a@0,8 b@10,8 c@20,8 x@40,8 y@80,8",
		},
		{
			"however close the stop",
			[]css.TabStop{stop(31, L, none)},
			"abc\tx", 300, nil, "", "a@0,8 b@10,8 c@20,8 x@31,8",
		},
		{
			"right text too wide starts at the tab",
			[]css.TabStop{stop(50, R, none)},
			"ab\tRRRRR", 300, nil, "", "a@0,8 b@10,8 R@20,8 R@30,8 R@40,8 R@50,8 R@60,8",
		},
		{
			"centre text too wide starts at the tab",
			[]css.TabStop{stop(30, C, none)},
			"ab\tCCCC", 300, nil, "", "a@0,8 b@10,8 C@20,8 C@30,8 C@40,8 C@50,8",
		},
		{
			"decimal without a separator ends on the stop",
			[]css.TabStop{stop(100, D, none)},
			"x\t12", 300, nil, "", "x@0,8 1@80,8 2@90,8",
		},
		{
			"decimal comma of the language",
			[]css.TabStop{stop(100, D, none)},
			"x\t12,5", 300, nil, "fr", "x@0,8 1@80,8 2@90,8 ,@100,8 5@110,8",
		},
		{
			"decimal full stop of English ignores a comma",
			[]css.TabStop{stop(100, D, none)},
			"x\t12,5", 300, nil, "en", "x@0,8 1@60,8 2@70,8 ,@80,8 5@90,8",
		},
		{
			"a stop past the line's end fills the line",
			[]css.TabStop{stop(150, L, none)},
			"a\tb\tc", 100, nil, "", "a@0,8 b@0,18 c@0,28",
		},
		{
			"the default stop at the line's end moves the tab", nil,
			"aaaaaaaa\tx", 100,
			[]func(*css.Style){func(s *css.Style) { s.TabSize = css.Pts(px(50)) }},
			"", "a@0,8 a@10,8 a@20,8 a@30,8 a@40,8 a@50,8 a@60,8 a@70,8 x@50,18",
		},
		{
			"a word too wide after its stop leaves the tab alone",
			[]css.TabStop{stop(60, L, none)},
			"a\tbbbbb", 100, nil, "", "a@0,8 b@0,28 b@10,28 b@20,28 b@30,28 b@40,28",
		},
		{
			"the line breaks before the tab, not inside the text",
			[]css.TabStop{stop(60, L, none)},
			"a b\tbbbbb", 100, nil, "", "a@0,8 b@20,8 b@0,28 b@10,28 b@20,28 b@30,28 b@40,28",
		},
		{
			"a right stop on the next line",
			[]css.TabStop{stop(90, R, none)},
			"aaaaaaa\tRRRR", 100, nil, "", "a@0,8 a@10,8 a@20,8 a@30,8 a@40,8 a@50,8 a@60,8 R@50,18 R@60,18 R@70,18 R@80,18",
		},
		{
			"measured from the content box",
			[]css.TabStop{stop(50, L, none)},
			"a\tb", 300,
			[]func(*css.Style){padding(0, 0, 0, 20)},
			"", "a@20,8 b@70,8",
		},
		{
			"right to left from the right edge",
			[]css.TabStop{stop(50, L, none)},
			"a\tb", 200,
			[]func(*css.Style){func(s *css.Style) { s.Direction = css.RTL }},
			"", "b@140,8 a@190,8",
		},
		{
			"a centred line aligns after the tabs",
			[]css.TabStop{stop(50, L, none)},
			"a\tb", 200,
			[]func(*css.Style){func(s *css.Style) { s.TextAlign = css.AlignCenter }},
			"", "a@70,8 b@120,8",
		},
		{
			"a justified line stretches after its last tab",
			[]css.TabStop{stop(50, L, none)},
			"a b\tc d e f g kkkkkk", 200,
			[]func(*css.Style){func(s *css.Style) { s.TextAlign = css.AlignJustify }},
			"",
			"a@0,8 b@20,8 c@50,8 d@85,8 e@120,8 f@155,8 g@190,8 k@0,18 k@10,18 k@20,18 k@30,18 k@40,18 k@50,18",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := officeSty(tt.stops, tt.mods...)
			p := el(st, txt(st, tt.text))
			p.Lang = tt.lang
			res := run(t, doc(p), page(tt.width, 100))
			if got := glyphLayout(res, 1); got != tt.want {
				t.Errorf("glyphs\n got %s\nwant %s", got, tt.want)
			}
		})
	}
}

// TestTabs_CSSUnchanged keeps the CSS tab of a block without TabStops: a
// multiple of tab-size, the next one when closer than half a space.
func TestTabs_CSSUnchanged(t *testing.T) {
	pre := func(s *css.Style) { s.WhiteSpace = css.PreWrap; s.TabSize = css.Pts(px(40)) }
	res := run(t, doc(el(sty(pre), txt(sty(pre), "abc\tx"))), page(200, 50))
	if got, want := glyphLayout(res, 1), "a@0,8 b@10,8 c@20,8 x@40,8"; got != want {
		t.Errorf("glyphs %s, want %s", got, want)
	}
	res = run(t, doc(el(sty(pre), txt(sty(pre), "abcd\tx"))), page(200, 50))
	if got, want := glyphLayout(res, 1), "a@0,8 b@10,8 c@20,8 d@30,8 x@80,8"; got != want {
		t.Errorf("glyphs %s, want %s: a stop within half a space is skipped", got, want)
	}
}

// TestTabs_Leaders fills the room a tab leaves with its leader's character
// in the paragraph's font, as many copies as whole advances fit spread
// over the tab, the first at its start and the last ending at its end.
func TestTabs_Leaders(t *testing.T) {
	tests := []struct {
		leader css.TabLeader
		char   string
	}{
		{css.TabLeaderDots, "."},
		{css.TabLeaderHyphens, "-"},
		{css.TabLeaderUnderscore, "_"},
		{css.TabLeaderMiddleDots, "·"},
	}
	for _, tt := range tests {
		t.Run(tt.char, func(t *testing.T) {
			st := officeSty([]css.TabStop{stop(145, css.TabRight, tt.leader)})
			res := run(t, doc(el(st, txt(st, "T\t9"))), page(200, 50))
			var fill []float64
			for _, g := range glyphsOf(res, 1) {
				if g.text == tt.char {
					fill = append(fill, g.x)
				}
			}
			// The tab runs from 10 to 135: twelve whole copies, 125/12
			// apart.
			if len(fill) != 12 || !near(fill[0], 10) || !near(fill[11], 10+11*125.0/12) {
				t.Errorf("leader copies at %v, want 12 from 10 to %.2f", fill, 10+11*125.0/12)
			}
			if got := glyphLayout(res, 1); !strings.HasSuffix(got, "9@135,8") {
				t.Errorf("glyphs %s, want 9 ending on the stop at 145", got)
			}
		})
	}
	st := officeSty([]css.TabStop{stop(145, css.TabRight, css.TabLeaderNone)})
	res := run(t, doc(el(st, txt(st, "T\t9"))), page(200, 50))
	if got, want := glyphLayout(res, 1), "T@0,8 9@135,8"; got != want {
		t.Errorf("glyphs %s, want %s: no leader draws nothing", got, want)
	}
}

// TestTabs_Bar draws a bar stop as a one pixel rule across every line,
// in the text colour, and sends no tab to it.
func TestTabs_Bar(t *testing.T) {
	st := officeSty([]css.TabStop{stop(25, css.TabBar, css.TabLeaderNone), stop(50, css.TabLeft, css.TabLeaderNone)}, fg(blue), func(s *css.Style) { s.FontSize = px(4) })
	res := run(t, doc(el(st, txt(st, "\ta"))), page(60, 4))
	assertPixels(t, res, 1, `
		_________________________B________________________BBBB______
		_________________________B________________________BBBB______
		_________________________B________________________BBBB______
		_________________________B________________________BBBB______`)
	var rules int
	var walk func([]compose.Fragment)
	walk = func(fs []compose.Fragment) {
		for _, f := range fs {
			switch v := f.(type) {
			case *compose.Path:
				rules++
			case *compose.Group:
				walk(v.Content)
			}
		}
	}
	walk(res.Pages[0].Content)
	if rules != 1 {
		t.Errorf("%d paths, want the one rule", rules)
	}
}

// TestTabs_OfficeIntrinsic sizes a shrink-to-fit Office paragraph by its
// tab stops.
func TestTabs_OfficeIntrinsic(t *testing.T) {
	st := officeSty([]css.TabStop{stop(50, css.TabRight, css.TabLeaderNone)}, display(css.InlineBlock))
	res := run(t, doc(el(sty(), el(st, txt(st, "a\tbb")))), page(200, 50))
	if got, want := glyphLayout(res, 1), "a@0,8 b@30,8 b@40,8"; got != want {
		t.Errorf("glyphs %s, want %s", got, want)
	}
}
