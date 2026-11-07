package css

import "testing"

func TestTextOverflow_Parse(t *testing.T) {
	tests := []struct {
		decl string
		want OverflowMark
	}{
		{"text-overflow: ellipsis", OverflowMark{Kind: MarkEllipsis}},
		{"text-overflow: clip", OverflowMark{}},
		{`text-overflow: "--"`, OverflowMark{Kind: MarkString, Text: "--"}},
		{"text-overflow: clip ellipsis", OverflowMark{Kind: MarkEllipsis}},
		{"text-overflow: fade", OverflowMark{}},
		{"text-overflow: clip clip clip", OverflowMark{}},
	}
	for _, tt := range tests {
		if got := styleOf(t, tt.decl).TextOverflow; got != tt.want {
			t.Errorf("%s = %+v, want %+v", tt.decl, got, tt.want)
		}
	}
}

func TestLineClamp_Parse(t *testing.T) {
	ell := OverflowMark{Kind: MarkEllipsis}
	tests := []struct {
		decl  string
		lines int
		auto  bool
		clamp bool
		mark  OverflowMark
	}{
		{"line-clamp: 3", 3, false, true, ell},
		{`line-clamp: 3 "(…)"`, 3, false, true, OverflowMark{Kind: MarkString, Text: "(…)"}},
		{"line-clamp: 2 none", 2, false, true, OverflowMark{}},
		{"line-clamp: 2 3", 0, false, false, OverflowMark{}},
		{"line-clamp: none", 0, false, false, OverflowMark{}},
		{"line-clamp: auto", 0, true, true, ell},
		{"line-clamp: 0", 0, false, false, OverflowMark{}},
		{"max-lines: 2", 2, false, true, OverflowMark{}},
		{"max-lines: 2; block-ellipsis: auto", 2, false, true, ell},
		{"continue: discard", 0, false, true, OverflowMark{}},
		{"display: -webkit-box; -webkit-box-orient: vertical; -webkit-line-clamp: 4", 4, false, true, ell},
		{"display: -webkit-box; -webkit-line-clamp: 4", 0, false, false, OverflowMark{}},
		{"display: block; -webkit-box-orient: vertical; -webkit-line-clamp: 4", 0, false, false, OverflowMark{}},
	}
	for _, tt := range tests {
		s := styleOf(t, tt.decl)
		if s.Lines() != tt.lines || s.LineClamp.Auto != tt.auto || s.Clamps() != tt.clamp || s.ClampMark() != tt.mark {
			t.Errorf("%s: lines %d auto %v clamps %v mark %+v", tt.decl, s.Lines(), s.LineClamp.Auto, s.Clamps(), s.ClampMark())
		}
	}
}

func TestBlockEllipsis_Inherits(t *testing.T) {
	c := compute(t, `<div id=a><p id=b>x</p></div>`, `#a { line-clamp: 2 "+" }`)
	if got := c.of("b").LineClamp; got.Ellipsis != (OverflowMark{Kind: MarkString, Text: "+"}) || got.MaxLines != 0 {
		t.Errorf("child clamp = %+v", got)
	}
}
