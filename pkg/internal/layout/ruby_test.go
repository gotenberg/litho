package layout

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// rubyDoc returns a block of 40px Ahem of line height lh holding kids,
// and the styles of a ruby and of its annotation at half the size, as
// the user agent sheet gives them.
func rubyDoc(lh float64, mods ...func(*css.Style)) (*css.Style, *css.Style, *css.Style) {
	block := sty(func(s *css.Style) {
		s.FontSize = px(40)
		s.LineHeight = css.Dimension{Kind: css.Number, Value: lh}
	})
	ruby := inlineSty(func(s *css.Style) {
		s.Display = css.Ruby
		s.FontSize = px(40)
		s.LineHeight = block.LineHeight
	}, func(s *css.Style) {
		for _, m := range mods {
			m(s)
		}
	})
	rt := inlineSty(func(s *css.Style) {
		s.Display = css.RubyText
		s.FontSize = px(20)
		s.LineHeight = css.Dimension{Kind: css.Normal}
		s.RubyPosition, s.RubyAlign, s.RubyOverhang = ruby.RubyPosition, ruby.RubyAlign, ruby.RubyOverhang
	})
	return block, ruby, rt
}

// placed returns the origin of the run of res's first page that reads s,
// in CSS pixels, and fails when there is none.
func placed(t *testing.T, res *Result, s string) (float64, float64) {
	t.Helper()
	for _, p := range texts(res, 1) {
		if p.text == s {
			return p.x / css.Pt, p.y / css.Pt
		}
	}
	t.Fatalf("no run reads %q in %v", s, texts(res, 1))
	return 0, 0
}

// TestRubyColumn locks a ruby column as Chromium lays it out: as wide as
// its annotation, the base centred in it, the annotation's em box on the
// base's, overhanging the text on each side by half the annotation's
// font size, and the line grown by the annotation where nothing above it
// gives it room.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 puts the annotation "abcdefgh" of a 40px
//     Ahem base "AB" after "xx" at x 70 to 230, y 0 to 20, the base at 110
//     to 190, and "yy" at 220; the line is 60px tall.
func TestRubyColumn(t *testing.T) {
	t.Parallel()
	block, ruby, rt := rubyDoc(1)
	root := el(block, txt(block, "xx"), el(ruby, txt(ruby, "AB"), el(rt, txt(rt, "abcdefgh"))), txt(block, "yy"))
	res := run(t, root, page(600, 300))
	cases := []struct {
		s    string
		x, y float64
	}{
		{"xx", 0, 52}, {"AB", 110, 52}, {"abcdefgh", 70, 16}, {"yy", 220, 52},
	}
	for _, c := range cases {
		if x, y := placed(t, res, c.s); !near(x, c.x) || !near(y, c.y) {
			t.Errorf("%q at (%g, %g), want (%g, %g)", c.s, x, y, c.x, c.y)
		}
	}
}

// TestRubyOverhang locks where an annotation hangs over the text beside
// its ruby: not with ruby-overhang: none, not at the start of a line, not
// over another ruby column.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 puts "yy" at 240 after an unhung ruby of
//     160px, the second of two columns after "xx" at 230 (each hangs on
//     the text side alone), and a ruby that starts its line at 0, its
//     base at 40.
func TestRubyOverhang(t *testing.T) {
	t.Parallel()
	block, ruby, rt := rubyDoc(1)
	_, none, rtNone := rubyDoc(1, func(s *css.Style) { s.RubyOverhang = css.RubyOverhangNone })
	rtNone.RubyOverhang = css.RubyOverhangNone
	col := func(st, ann *css.Style, base string) *Node {
		return el(st, txt(st, base), el(ann, txt(ann, "abcdefgh")))
	}
	t.Run("none", func(t *testing.T) {
		t.Parallel()
		res := run(t, el(block, txt(block, "xx"), col(none, rtNone, "AB"), txt(block, "yy")), page(600, 300))
		if x, _ := placed(t, res, "yy"); !near(x, 240) {
			t.Errorf("yy at %g, want 240", x)
		}
	})
	t.Run("two columns", func(t *testing.T) {
		t.Parallel()
		two := el(ruby, txt(ruby, "AB"), el(rt, txt(rt, "abcdefgh")), txt(ruby, "CD"), el(rt, txt(rt, "abcdefgh")))
		res := run(t, el(block, txt(block, "xx"), two, txt(block, "yy")), page(600, 300))
		if x, _ := placed(t, res, "CD"); !near(x, 270) {
			t.Errorf("CD at %g, want 270", x)
		}
		if x, _ := placed(t, res, "yy"); !near(x, 380) {
			t.Errorf("yy at %g, want 380", x)
		}
	})
	t.Run("line start", func(t *testing.T) {
		t.Parallel()
		res := run(t, el(block, col(ruby, rt, "AB"), txt(block, "yy")), page(600, 300))
		if x, _ := placed(t, res, "AB"); !near(x, 40) {
			t.Errorf("AB at %g, want 40", x)
		}
		if x, _ := placed(t, res, "yy"); !near(x, 150) {
			t.Errorf("yy at %g, want 150", x)
		}
	})
}

// TestRubyLineRoom locks the room a line gives the annotation over it:
// its own half-leading, then the margins before its block, before the
// line grows.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 lays a 40px Ahem line of line height 2
//     with a 20px annotation out 80px tall, one of line height 1.5 70px
//     tall, and one after a 10px margin 50px tall.
func TestRubyLineRoom(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		lh, margin, want float64
	}{
		{2, 0, 80}, {1.5, 0, 70}, {1, 10, 50}, {1, 30, 40},
	} {
		block, ruby, rt := rubyDoc(c.lh)
		block.Margin[css.Top] = css.Pts(px(c.margin))
		root := el(sty(), el(block, txt(block, "xx"), el(ruby, txt(ruby, "AB"), el(rt, txt(rt, "abcdefgh")))))
		b := kid(t, boxes(t, root, page(600, 300)), 0)
		if got := rect(b)[3]; !near(got, c.want) {
			t.Errorf("line height %g after a %gpx margin: block %gpx tall, want %g", c.lh, c.margin, got, c.want)
		}
	}
}

// TestRubyUnder locks ruby-position: under, the annotation's em box under
// the base's.
func TestRubyUnder(t *testing.T) {
	t.Parallel()
	block, ruby, rt := rubyDoc(1, func(s *css.Style) { s.RubyPosition = css.RubyUnder })
	res := run(t, el(block, el(ruby, txt(ruby, "AB"), el(rt, txt(rt, "ab")))), page(600, 300))
	_, by := placed(t, res, "AB")
	_, ay := placed(t, res, "ab")
	// The base's em box ends 8px under its baseline, the annotation's
	// starts 16px over its own.
	if !near(by, 32) || !near(ay, 32+8+16) {
		t.Errorf("base at %g, annotation at %g; want 32 and 56", by, ay)
	}
}

// TestRubyRoomBetweenLines locks the room an annotation under a line
// takes before the next line moves: its line's half-leading, then the
// room above the next line's content; at the end of a block, its end
// padding. An annotation over a line takes the room under the content of
// the line before, a line of ruby included.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 sets the line
//     after a 24px line of 20px Ahem holding a 10px annotation under its
//     base 30px lower, and grows the block by 8px less its end padding
//     when that line is its last; two such lines with the annotation over
//     their bases are 30px apart.
func TestRubyRoomBetweenLines(t *testing.T) {
	t.Parallel()
	// 48px lines of 40px Ahem: the annotation's em box ends 8 + 20px under
	// the baseline, 16px under the line, and the next line's half-leading
	// takes 4px of it.
	block, ruby, rt := rubyDoc(1.2, func(s *css.Style) { s.RubyPosition = css.RubyUnder })
	res := run(t, el(block, el(ruby, txt(ruby, "AB"), el(rt, txt(rt, "ab"))), br(block), txt(block, "yy")), page(600, 300))
	if _, y := placed(t, res, "yy"); !near(y, 48+12+36) {
		t.Errorf("next line's baseline at %g, want %g", y, 48+12+36.0)
	}
	for _, c := range []struct{ pad, want float64 }{{0, 64}, {6, 64}, {20, 68}} {
		block, ruby, rt := rubyDoc(1.2, func(s *css.Style) { s.RubyPosition = css.RubyUnder })
		block.Padding[css.Bottom] = css.Pts(px(c.pad))
		root := el(sty(), el(block, el(ruby, txt(ruby, "AB"), el(rt, txt(rt, "ab")))))
		b := kid(t, boxes(t, root, page(600, 300)), 0)
		if got := rect(b)[3]; !near(got, c.want) {
			t.Errorf("end padding %gpx: block %gpx tall, want %g", c.pad, got, c.want)
		}
	}
	// Annotations over: the first line grows by 16px, the second by 16px
	// less the 4px of half-leading under the first line's base.
	block, ruby, rt = rubyDoc(1.2)
	res = run(t, el(block, el(ruby, txt(ruby, "AB"), el(rt, txt(rt, "ab"))), br(block), el(ruby, txt(ruby, "CD"), el(rt, txt(rt, "cd")))), page(600, 300))
	if _, y := placed(t, res, "CD"); !near(y, 64+12+36) {
		t.Errorf("second line's baseline at %g, want %g", y, 64+12+36.0)
	}
}

// TestRubyPairing locks how a ruby's children pair (CSS Ruby 1, 2.2 and
// 2.5.1): a base per run of other children, white space alone between
// segments dropped, a hidden child separating nothing.
func TestRubyPairing(t *testing.T) {
	t.Parallel()
	block, ruby, rt := rubyDoc(2)
	hidden := inlineSty(func(s *css.Style) { s.Display = css.DisplayNone })
	r := el(ruby, txt(ruby, "A"), el(hidden, txt(hidden, "(")), el(rt, txt(rt, "a")), el(hidden, txt(hidden, ")")),
		txt(ruby, " "), txt(ruby, "B"), el(rt, txt(rt, "b")), txt(ruby, " "))
	rb := boxes(t, el(block, r), page(600, 300))
	var cols []*box
	var walk func(*box)
	walk = func(b *box) {
		if b.ruby != nil {
			cols = append(cols, b)
		}
		for _, k := range b.kids {
			walk(k)
		}
	}
	walk(rb)
	if len(cols) != 2 {
		t.Fatalf("%d columns, want 2", len(cols))
	}
	for i, want := range []rune{'A', 'B'} {
		if got := cols[i].ruby.breakAs; got != want {
			t.Errorf("column %d breaks as %q, want %q", i, got, want)
		}
		if cols[i].ruby.ann == nil {
			t.Errorf("column %d has no annotation", i)
		}
	}
}

// TestRubyNested locks a ruby whose base holds a ruby: its annotation
// sits over the inner one, on the side its own ruby's ruby-position
// gives, whatever the annotation sets, and the line makes room for both
// once, from the margin above it.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 puts the annotation "efgh" of a ruby
//     whose base is a ruby of "AB" over "ab" 40px over the inner one,
//     over it although the annotation sets ruby-position: under, both in
//     the 40px margin above the 40px line.
func TestRubyNested(t *testing.T) {
	t.Parallel()
	block, ruby, rt := rubyDoc(1)
	block.Margin[css.Top] = css.Pts(px(40))
	under := *rt
	under.RubyPosition = css.RubyUnder
	inner := el(ruby, txt(ruby, "AB"), el(rt, txt(rt, "ab")))
	root := el(sty(), el(block, el(ruby, inner, el(&under, txt(&under, "efgh")))))
	res := run(t, root, page(600, 300))
	for _, c := range []struct {
		s string
		y float64
	}{{"AB", 72}, {"ab", 36}, {"efgh", 16}} {
		if _, y := placed(t, res, c.s); !near(y, c.y) {
			t.Errorf("%q at y %g, want %g", c.s, y, c.y)
		}
	}
	if b := kid(t, boxes(t, root, page(600, 300)), 0); !near(rect(b)[3], 40) {
		t.Errorf("block %gpx tall, want 40", rect(b)[3])
	}
}

// TestRubyTags locks the structure of a ruby (ISO 32000-1, 14.8.4.4.2):
// a Ruby element for each base and its annotation, holding an RB, the
// base's own when the markup gives one, and the RT; the annotation of an
// empty base, and a base without one, are plain content; a hidden rp
// makes no element; emphasis marks are artifacts.
func TestRubyTags(t *testing.T) {
	t.Parallel()
	block, ruby, rt := rubyDoc(2)
	hidden := inlineSty(func(s *css.Style) { s.Display = css.DisplayNone })
	in := inlineSty()
	r := el(ruby, txt(ruby, "A"), role("RP", el(hidden, txt(hidden, "("))), role("RT", el(rt, txt(rt, "a"))),
		role("RB", el(in, txt(in, "B"))), role("RT", el(rt, txt(rt, "b"))),
		role("RT", el(rt, txt(rt, "c"))), txt(ruby, "C"))
	res := run(t, doc(role("P", el(block, r))), page(600, 300))
	want := "Document[P[Ruby[RB# RT#] Ruby[RB# RT#] Span# Span#]]"
	if got := shape(res, res.Structure); got != want {
		t.Errorf("structure %s, want %s", got, want)
	}
}

// TestRubyLineBesideFloat locks the width of a line a ruby annotation
// under the line before moves down: the floats beside its content, not
// beside where it would start, shorten it.
//
// # Corpus evidence
//
//   - wpt: css-inline/initial-letter/initial-letter-block-position-drop-under-ruby-tall.
func TestRubyLineBesideFloat(t *testing.T) {
	t.Parallel()
	// 48px lines of 40px Ahem: the line after the ruby moves 12px down,
	// its content past the 60px float.
	block, ruby, rt := rubyDoc(1.2, func(s *css.Style) { s.RubyPosition = css.RubyUnder })
	fl := sty(func(s *css.Style) {
		s.Float = css.FloatLeft
		s.Width, s.Height = css.Pts(px(100)), css.Pts(px(60))
	})
	res := run(t, el(block, el(fl), el(ruby, txt(ruby, "AB"), el(rt, txt(rt, "ab"))), br(block), txt(block, "yy")), page(600, 300))
	if x, y := placed(t, res, "yy"); !near(x, 0) || !near(y, 48+12+36) {
		t.Errorf("next line at (%g, %g), want (0, %g)", x, y, 48+12+36.0)
	}
}

// TestRubyOverhangSpaces locks ruby-overhang: spaces (CSS Ruby 1, 5.1 of
// the editor's draft): the annotation hangs over the spaces beside its
// column as far as they reach, and over nothing else.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 lays a 60px
//     annotation over a 20px base between two no-break spaces out as wide
//     as the annotation, and between two letters 40px wider.
func TestRubyOverhangSpaces(t *testing.T) {
	t.Parallel()
	spaces := func(s *css.Style) { s.RubyOverhang = css.RubyOverhangSpaces }
	for _, c := range []struct {
		sep   string
		after float64
	}{
		// The 120px annotation reaches 40px past its base on each side:
		// over the 40px no-break spaces, whose run after the column then
		// starts where the base ends, or over nothing.
		{"\u00a0", 160}, {"", 200},
	} {
		block, ruby, rt := rubyDoc(1, spaces)
		res := run(t, el(block, txt(block, "xx"+c.sep), el(ruby, txt(ruby, "A"), el(rt, txt(rt, "abcdef"))), txt(block, c.sep+"yy")), page(600, 300))
		if x, _ := placed(t, res, "abcdef"); !near(x, 80) {
			t.Errorf("separator %q: annotation at %g, want 80", c.sep, x)
		}
		if x, _ := placed(t, res, c.sep+"yy"); !near(x, c.after) {
			t.Errorf("separator %q: text after at %g, want %g", c.sep, x, c.after)
		}
	}
	for _, c := range []struct {
		r            rune
		after        bool
		share        float64
		hangsOnAfter bool
	}{
		{' ', false, 1, true}, {'　', true, 1, true}, {'\t', true, 1, true},
		{'（', true, 0.5, false}, {'（', false, 0, false},
		{'）', false, 0.5, false}, {'・', true, 0.25, false}, {'a', true, 0, false},
	} {
		if share, on := overhangSpace(c.r, c.after); share != c.share || on != c.hangsOnAfter {
			t.Errorf("%U after %t: %g, %t; want %g, %t", c.r, c.after, share, on, c.share, c.hangsOnAfter)
		}
	}
}

// TestRubyOverhangIntrinsic locks the max-content width of a line whose
// annotation hangs over the text beside its ruby: the overhang counts.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 makes a
//     max-content box of a 60px annotation over a 20px base between two
//     20px characters 90px wide.
func TestRubyOverhangIntrinsic(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		overhang css.RubyOverhang
		want     float64
	}{
		// A 120px annotation over a 40px base between two 40px letters:
		// it hangs 10px over each, or over neither.
		{css.RubyOverhangAuto, 180}, {css.RubyOverhangSpaces, 200},
	} {
		block, ruby, rt := rubyDoc(1, func(s *css.Style) { s.RubyOverhang = c.overhang })
		block.Float = css.FloatLeft
		root := el(sty(), el(block, txt(block, "x"), el(ruby, txt(ruby, "A"), el(rt, txt(rt, "abcdef"))), txt(block, "y")))
		b := kid(t, boxes(t, root, page(600, 300)), 0)
		if got := rect(b)[2]; !near(got, c.want) {
			t.Errorf("overhang %d: %gpx wide, want %g", c.overhang, got, c.want)
		}
	}
}

// TestRubyBaseKeepsEndSpaces locks the spaces other than collapsible ones
// at the end of a ruby base: they stay in its content, which its column
// aligns, as Chromium lays them out (an ideographic space after the base's
// text keeps it where it would be with any other character).
//
// # Corpus evidence
//
//   - wpt: css-ruby/ruby-overhang-spaces-003-ref and its tests, whose
//     reference centres "　あ　" under a 60px annotation.
func TestRubyBaseKeepsEndSpaces(t *testing.T) {
	t.Parallel()
	_, ruby, rt := rubyDoc(1, func(s *css.Style) { s.RubyAlign = css.RubyCenter })
	block, _, _ := rubyDoc(1)
	spaced := run(t, el(block, el(ruby, txt(ruby, "　A　"), el(rt, txt(rt, "abcdefghijklmnop")))), page(800, 300))
	lettered := run(t, el(block, el(ruby, txt(ruby, "　AB"), el(rt, txt(rt, "abcdefghijklmnop")))), page(800, 300))
	if a, b := runX(t, spaced, "A"), runX(t, lettered, "A"); !near(a, b) {
		t.Errorf("base text at %g with an ideographic space after it, at %g with a letter", a, b)
	}
}

// runX returns the x of the first glyph s of res's first page, in CSS
// pixels.
func runX(t *testing.T, res *Result, s string) float64 {
	t.Helper()
	for _, p := range texts(res, 1) {
		x := p.x
		for _, g := range p.run.Glyphs {
			if g.Text == s {
				return float64(x+g.X) / css.Pt
			}
		}
	}
	t.Fatalf("no glyph %q in %v", s, texts(res, 1))
	return 0
}
