package css

import (
	"testing"
)

func TestFlex_Longhands(t *testing.T) {
	t.Parallel()
	tests := []struct {
		decls string
		check func(*Style) bool
	}{
		{"display: flex", func(s *Style) bool { return s.Display == Flex }},
		{"display: inline-flex", func(s *Style) bool { return s.Display == InlineFlex }},
		{"display: -webkit-flex", func(s *Style) bool { return s.Display == Flex }},
		{"display: inline flex", func(s *Style) bool { return s.Display == InlineFlex }},
		{"display: block flex", func(s *Style) bool { return s.Display == Flex }},
		{"display: inline-flex; float: left", func(s *Style) bool { return s.Display == Flex && s.OuterInline }},
		{"display: inline-flex; position: absolute", func(s *Style) bool { return s.Display == Flex }},
		{"flex-direction: column-reverse", func(s *Style) bool { return s.FlexDirection == FlexColumnReverse }},
		{"flex-direction: sideways", func(s *Style) bool { return s.FlexDirection == FlexRow }},
		{"flex-wrap: wrap-reverse", func(s *Style) bool { return s.FlexWrap == WrapReverse }},
		{"order: -3", func(s *Style) bool { return s.Order == -3 }},
		{"order: 1.5", func(s *Style) bool { return s.Order == 0 }},
		{"flex-grow: 2.5", func(s *Style) bool { return s.FlexGrow == 2.5 }},
		{"flex-grow: -1", func(s *Style) bool { return s.FlexGrow == 0 }},
		{"flex-shrink: 0", func(s *Style) bool { return s.FlexShrink == 0 }},
		{"flex-basis: 10px", func(s *Style) bool { return dimEq(s.FlexBasis, pts(7.5)) }},
		{"flex-basis: 50%", func(s *Style) bool { return s.FlexBasis == pct(0.5) }},
		{"flex-basis: content", func(s *Style) bool { return s.FlexBasis.Kind == FlexContent }},
		{"flex-basis: -1px", func(s *Style) bool { return s.FlexBasis.Kind == Auto }},
		{"justify-content: space-between", func(s *Style) bool { return s.JustifyContent == BoxSpaceBetween }},
		{"justify-content: safe center", func(s *Style) bool { return s.JustifyContent == BoxCenter|BoxSafe }},
		{"justify-content: unsafe flex-end", func(s *Style) bool { return s.JustifyContent == BoxFlexEnd }},
		{"justify-content: left", func(s *Style) bool { return s.JustifyContent == BoxLeft }},
		{"justify-content: baseline", func(s *Style) bool { return s.JustifyContent == BoxNormal }},
		{"align-content: space-evenly", func(s *Style) bool { return s.AlignContent == BoxSpaceEvenly }},
		{"align-content: stretch", func(s *Style) bool { return s.AlignContent == BoxStretch }},
		{"align-content: left", func(s *Style) bool { return s.AlignContent == BoxNormal }},
		{"align-items: last baseline", func(s *Style) bool { return s.AlignItems == BoxLastBaseline }},
		{"align-items: first baseline", func(s *Style) bool { return s.AlignItems == BoxBaseline }},
		{"align-items: self-end", func(s *Style) bool { return s.AlignItems == BoxSelfEnd }},
		{"align-items: auto", func(s *Style) bool { return s.AlignItems == BoxNormal }},
		{"align-items: space-between", func(s *Style) bool { return s.AlignItems == BoxNormal }},
		{"align-self: auto", func(s *Style) bool { return s.AlignSelf == BoxAuto }},
		{"align-self: safe end", func(s *Style) bool { return s.AlignSelf == BoxEnd|BoxSafe }},
		{"align-self: stretch", func(s *Style) bool { return s.AlignSelf == BoxStretch }},
		{"row-gap: 10px; column-gap: 10%", func(s *Style) bool { return dimEq(s.RowGap, pts(7.5)) && s.ColumnGap == pct(0.1) }},
		{"column-gap: normal", func(s *Style) bool { return s.ColumnGap.Kind == Normal }},
		{"grid-row-gap: 4px", func(s *Style) bool { return dimEq(s.RowGap, pts(3)) }},
		{"-webkit-flex-direction: column", func(s *Style) bool { return s.FlexDirection == FlexColumn }},
		{"min-width: auto", func(s *Style) bool { return s.MinWidthAuto && dimEq(s.MinWidth, pts(0)) }},
		{"min-width: 0", func(s *Style) bool { return !s.MinWidthAuto && s.MinHeightAuto }},
		{"min-height: 10px", func(s *Style) bool { return !s.MinHeightAuto && dimEq(s.MinHeight, pts(7.5)) }},
		{"min-height: min-content", func(s *Style) bool { return !s.MinHeightAuto }},
		{"", func(s *Style) bool {
			return s.FlexShrink == 1 && s.FlexGrow == 0 && s.FlexBasis.Kind == Auto && s.AlignSelf == BoxAuto &&
				s.AlignItems == BoxNormal && s.RowGap.Kind == Normal && s.MinWidthAuto && s.MinHeightAuto
		}},
	}
	for _, tt := range tests {
		if s := styleOf(t, tt.decls); !tt.check(s) {
			t.Errorf("%q: got %+v", tt.decls, s)
		}
	}
}

func TestFlex_Shorthands(t *testing.T) {
	t.Parallel()
	type fl struct {
		grow, shrink float64
		basis        Dimension
	}
	zero := pct(0)
	tests := []struct {
		decls string
		want  fl
	}{
		{"flex: none", fl{0, 0, Dimension{Kind: Auto}}},
		{"flex: auto", fl{1, 1, Dimension{Kind: Auto}}},
		{"flex: initial", fl{0, 1, Dimension{Kind: Auto}}},
		{"flex: 2", fl{2, 1, zero}},
		{"flex: 2 3", fl{2, 3, zero}},
		{"flex: 2 3 10px", fl{2, 3, pts(7.5)}},
		{"flex: 10px", fl{1, 1, pts(7.5)}},
		{"flex: 10px 2", fl{2, 1, pts(7.5)}},
		{"flex: 2 10px", fl{2, 1, pts(7.5)}},
		{"flex: 0 0 0", fl{0, 0, pts(0)}},
		{"flex: 1 1 content", fl{1, 1, Dimension{Kind: FlexContent}}},
		{"flex: 2; flex: 1 2 3 4", fl{2, 1, zero}},
		{"flex: 2; flex: 10px 20px", fl{2, 1, zero}},
		{"flex: 2; flex: 1 10px 2", fl{2, 1, zero}},
		{"flex: 3 0 auto", fl{3, 0, Dimension{Kind: Auto}}},
		{"-webkit-flex: 4", fl{4, 1, zero}},
	}
	for _, tt := range tests {
		s := styleOf(t, tt.decls)
		got := fl{s.FlexGrow, s.FlexShrink, s.FlexBasis}
		if got.grow != tt.want.grow || got.shrink != tt.want.shrink || got.basis.Kind != tt.want.basis.Kind || !dimEq(got.basis, tt.want.basis) {
			t.Errorf("%q: got %+v, want %+v", tt.decls, got, tt.want)
		}
	}

	s := styleOf(t, "flex-flow: wrap column")
	if s.FlexDirection != FlexColumn || s.FlexWrap != Wrap {
		t.Errorf("flex-flow: %v %v", s.FlexDirection, s.FlexWrap)
	}
	s = styleOf(t, "flex-direction: column; flex-wrap: wrap; flex-flow: row-reverse")
	if s.FlexDirection != FlexRowReverse || s.FlexWrap != NoWrap {
		t.Errorf("flex-flow resets: %v %v", s.FlexDirection, s.FlexWrap)
	}
	s = styleOf(t, "gap: 4px 8px")
	if !dimEq(s.RowGap, pts(3)) || !dimEq(s.ColumnGap, pts(6)) {
		t.Errorf("gap = %+v %+v", s.RowGap, s.ColumnGap)
	}
	s = styleOf(t, "gap: 10%")
	if s.RowGap != pct(0.1) || s.ColumnGap != pct(0.1) {
		t.Errorf("gap one value = %+v %+v", s.RowGap, s.ColumnGap)
	}
	s = styleOf(t, "place-content: center space-between")
	if s.AlignContent != BoxCenter || s.JustifyContent != BoxSpaceBetween {
		t.Errorf("place-content = %v %v", s.AlignContent, s.JustifyContent)
	}
	s = styleOf(t, "place-content: safe end")
	if s.AlignContent != BoxEnd|BoxSafe || s.JustifyContent != BoxEnd|BoxSafe {
		t.Errorf("place-content one value = %v %v", s.AlignContent, s.JustifyContent)
	}
	s = styleOf(t, "place-content: baseline")
	if s.AlignContent != BoxBaseline || s.JustifyContent != BoxStart {
		t.Errorf("place-content baseline = %v %v", s.AlignContent, s.JustifyContent)
	}
	s = styleOf(t, "place-items: end center; place-self: last baseline")
	if s.AlignItems != BoxEnd || s.AlignSelf != BoxLastBaseline {
		t.Errorf("place-items, place-self = %v %v", s.AlignItems, s.AlignSelf)
	}
}

func TestFlex_ItemsBlockified(t *testing.T) {
	t.Parallel()
	c := compute(t, `<body><div id=f><span id=a>a</span><img id=b><span id=c></span><table id=d></table><i id=e></i><b id=g></b></div><span id=h></span></body>`,
		`#f { display: flex } #c { display: inline-flex } #d { display: inline-table } #e { display: table-cell } #g { display: inline-block }`)
	for id, want := range map[string]Display{"a": Block, "b": Block, "c": Flex, "d": Table, "e": Block, "g": Block, "h": Inline} {
		if got := c.of(id).Display; got != want {
			t.Errorf("#%s display = %v, want %v", id, got, want)
		}
	}
}
