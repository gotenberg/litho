package css

import "testing"

// TestRuby_UserAgentStyles locks what the user agent sheet gives ruby
// markup, as Chromium 156 computes it: a ruby container, an annotation
// half its ruby's size aligned at the start, an rt outside a ruby inline,
// an rp hidden, and the block-level children of a ruby inlinified.
func TestRuby_UserAgentStyles(t *testing.T) {
	t.Parallel()
	c := compute(t, `<body><div style="text-align:center"><ruby id=r>A<rp id=p>(</rp><rt id=rt>a</rt><div id=d>b</div></ruby><span><rt id=lone>x</rt></span></div></body>`,
		`body { font-size: 20px }`)
	if got := c.of("r").Display; got != Ruby {
		t.Errorf("ruby display %v, want Ruby", got)
	}
	rt := c.of("rt")
	if rt.Display != RubyText || !near(rt.FontSize, 7.5) || rt.TextAlign != AlignStart || rt.LineHeight.Kind != Normal {
		t.Errorf("rt: display %v, size %v, align %v, line height %+v", rt.Display, rt.FontSize, rt.TextAlign, rt.LineHeight)
	}
	if got := c.of("p").Display; got != DisplayNone {
		t.Errorf("rp display %v, want none", got)
	}
	if got := c.of("d").Display; got != InlineBlock {
		t.Errorf("div in a ruby: display %v, want InlineBlock", got)
	}
	lone := c.of("lone")
	if lone.Display != Inline || !near(lone.FontSize, 15) || lone.TextAlign != AlignCenter {
		t.Errorf("rt outside a ruby: display %v, size %v, align %v", lone.Display, lone.FontSize, lone.TextAlign)
	}
}

// TestRuby_Properties locks the values of the ruby properties Chromium
// 156 parses (CSS Ruby 1, 4) and the display values of CSS Ruby 1, 2.1.
func TestRuby_Properties(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		decls string
		check func(*Style) bool
	}{
		{"ruby-position: under", func(s *Style) bool { return s.RubyPosition == RubyUnder }},
		{"ruby-position: under; ruby-position: alternate", func(s *Style) bool { return s.RubyPosition == RubyUnder }},
		{"ruby-position: under; ruby-position: inter-character", func(s *Style) bool { return s.RubyPosition == RubyUnder }},
		{"ruby-align: start", func(s *Style) bool { return s.RubyAlign == RubyStart }},
		{"ruby-align: center", func(s *Style) bool { return s.RubyAlign == RubyCenter }},
		{"ruby-align: space-between", func(s *Style) bool { return s.RubyAlign == RubySpaceBetween }},
		{"", func(s *Style) bool {
			return s.RubyAlign == RubySpaceAround && s.RubyPosition == RubyOver && s.RubyOverhang == RubyOverhangAuto
		}},
		{"ruby-overhang: none", func(s *Style) bool { return s.RubyOverhang == RubyOverhangNone }},
		{"ruby-overhang: none; ruby-overhang: spaces", func(s *Style) bool { return s.RubyOverhang == RubyOverhangNone }},
		{"display: ruby", func(s *Style) bool { return s.Display == Ruby }},
		{"display: inline ruby", func(s *Style) bool { return s.Display == Ruby }},
		{"display: block ruby", func(s *Style) bool { return s.Display == BlockRuby }},
		{"display: ruby-text", func(s *Style) bool { return s.Display == RubyText }},
		{"display: inline; display: ruby-base", func(s *Style) bool { return s.Display == Inline }},
		{"display: inline; display: ruby-text-container", func(s *Style) bool { return s.Display == Inline }},
		{"display: ruby; float: left", func(s *Style) bool { return s.Display == BlockRuby }},
		{"display: ruby-text; position: absolute", func(s *Style) bool { return s.Display == Block }},
	} {
		if s := styleOf(t, c.decls); !c.check(s) {
			t.Errorf("%q: display %v, position %v, align %v, overhang %v", c.decls, s.Display, s.RubyPosition, s.RubyAlign, s.RubyOverhang)
		}
	}
}

// TestRuby_PropertiesInherit locks the inheritance of the ruby properties
// (CSS Ruby 1, 4).
func TestRuby_PropertiesInherit(t *testing.T) {
	t.Parallel()
	c := compute(t, `<body><p style="ruby-position: under; ruby-align: start; ruby-overhang: none"><span id=s>x</span></p></body>`, ``)
	if s := c.of("s"); s.RubyPosition != RubyUnder || s.RubyAlign != RubyStart || s.RubyOverhang != RubyOverhangNone {
		t.Errorf("inherited %v %v %v", s.RubyPosition, s.RubyAlign, s.RubyOverhang)
	}
}
