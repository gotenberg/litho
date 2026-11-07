package css

import "testing"

func TestMulticol_Longhands(t *testing.T) {
	t.Parallel()
	red := Color{R: 1, A: 1}
	tests := []struct {
		decls string
		check func(*Style) bool
	}{
		{"", func(s *Style) bool {
			c := s.Columns
			return c.Count == 0 && c.Width.Kind == Auto && c.RuleStyle == BorderNone && c.RuleWidth == 3*Pt &&
				c.RuleColor == s.Color && !c.SpanAll && c.Fill == FillBalance && c.Height.Kind == Auto && !c.Wrap() && !c.Multicol()
		}},
		{"column-count: 3", func(s *Style) bool { return s.Columns.Count == 3 && s.Columns.Multicol() }},
		{"column-count: 0", func(s *Style) bool { return s.Columns.Count == 0 }},
		{"column-count: 2.5", func(s *Style) bool { return s.Columns.Count == 0 }},
		{"column-count: 3; column-count: auto", func(s *Style) bool { return s.Columns.Count == 0 }},
		{"column-width: 100px", func(s *Style) bool { return dimEq(s.Columns.Width, pts(75)) && s.Columns.Multicol() }},
		{"column-width: 10%", func(s *Style) bool { return s.Columns.Width.Kind == Auto }},
		{"column-width: -1px", func(s *Style) bool { return s.Columns.Width.Kind == Auto }},
		{"column-width: 0", func(s *Style) bool { return dimEq(s.Columns.Width, pts(0)) }},
		{"column-rule-style: dashed", func(s *Style) bool { return s.Columns.RuleStyle == Dashed }},
		{"column-rule-width: thin", func(s *Style) bool { return s.Columns.RuleWidth == Pt }},
		{"column-rule-width: 4px", func(s *Style) bool { return s.Columns.RuleWidth == 3 }},
		{"column-rule-color: red", func(s *Style) bool { return s.Columns.RuleColor == red }},
		{"color: red", func(s *Style) bool { return s.Columns.RuleColor == red }},
		{"column-span: all", func(s *Style) bool { return s.Columns.SpanAll }},
		{"column-span: 2", func(s *Style) bool { return !s.Columns.SpanAll }},
		{"column-fill: auto", func(s *Style) bool { return s.Columns.Fill == FillAuto }},
		{"column-fill: balance-all", func(s *Style) bool { return s.Columns.Fill == FillBalanceAll }},
		{"column-height: 40px", func(s *Style) bool { return dimEq(s.Columns.Height, pts(30)) && s.Columns.Wrap() }},
		{"column-height: 40px; column-wrap: nowrap", func(s *Style) bool { return !s.Columns.Wrap() }},
		{"column-wrap: wrap", func(s *Style) bool { return s.Columns.Wrap() }},
		{"break-before: column", func(s *Style) bool { return s.BreakBefore == Column }},
		{"break-after: avoid-column", func(s *Style) bool { return s.BreakAfter == BreakAvoidColumn }},
		{"break-inside: avoid-column", func(s *Style) bool { return s.BreakInside == InsideAvoidColumn }},
		{"break-before: region", func(s *Style) bool { return s.BreakBefore == BreakAuto }},
		{"box-decoration-break: clone", func(s *Style) bool { return s.BoxDecorationBreak == DecorationClone }},
		{"-webkit-box-decoration-break: clone", func(s *Style) bool { return s.BoxDecorationBreak == DecorationClone }},
		{"box-decoration-break: slice", func(s *Style) bool { return s.BoxDecorationBreak == DecorationSlice }},
	}
	for _, tt := range tests {
		if s := styleOf(t, tt.decls); !tt.check(s) {
			t.Errorf("%q: got %+v", tt.decls, s.Columns)
		}
	}
}

func TestMulticol_Shorthands(t *testing.T) {
	t.Parallel()
	blue := Color{B: 1, A: 1}
	tests := []struct {
		decls string
		count int
		width Dimension
	}{
		{"columns: 3", 3, Dimension{Kind: Auto}},
		{"columns: 10px", 0, pts(7.5)},
		{"columns: 10px 3", 3, pts(7.5)},
		{"columns: 3 10px", 3, pts(7.5)},
		{"columns: auto 3", 3, Dimension{Kind: Auto}},
		{"columns: 3 auto", 3, Dimension{Kind: Auto}},
		{"columns: auto", 0, Dimension{Kind: Auto}},
		{"columns: auto auto", 0, Dimension{Kind: Auto}},
		{"columns: 3; columns: 3 4", 3, Dimension{Kind: Auto}},
		{"columns: 3; columns: 10px 20px", 3, Dimension{Kind: Auto}},
		{"columns: 3; columns: auto auto auto", 3, Dimension{Kind: Auto}},
		{"column-count: 2; columns: 10px", 0, pts(7.5)},
	}
	for _, tt := range tests {
		s := styleOf(t, tt.decls)
		if s.Columns.Count != tt.count || !dimEq(s.Columns.Width, tt.width) {
			t.Errorf("%q: got count %d width %+v", tt.decls, s.Columns.Count, s.Columns.Width)
		}
	}
	rules := []struct {
		decls string
		width float64
		style BorderStyle
		color Color
	}{
		{"column-rule: 2px solid blue", 1.5, Solid, blue},
		{"column-rule: blue dotted", 3 * Pt, Dotted, blue},
		{"column-rule: thick", 5 * Pt, BorderNone, blue},
		{"column-rule: double thin", Pt, Double, blue},
		{"column-rule: solid; column-rule: solid solid", 3 * Pt, Solid, blue},
	}
	for _, tt := range rules {
		c := styleOf(t, tt.decls).Columns
		if c.RuleWidth != tt.width || c.RuleStyle != tt.style || c.RuleColor != tt.color {
			t.Errorf("%q: got %+v", tt.decls, c)
		}
	}
}

func TestMulticol_RuleColorInherit(t *testing.T) {
	t.Parallel()
	c := compute(t, `<div id=a><p id=b></p></div>`, `#a { color: red } #b { color: blue; column-rule-color: inherit }`)
	if got := c.of("b").Columns.RuleColor; got != (Color{B: 1, A: 1}) {
		t.Errorf("an inherited currentColor resolves against the child's color: got %+v", got)
	}
}
