package css

import (
	"reflect"
	"testing"
)

func TestGCPM_Properties(t *testing.T) {
	t.Parallel()
	tests := []struct {
		decls string
		check func(*Style) bool
	}{
		{`position: running(header)`, func(s *Style) bool { return s.Running == "header" && s.Position == Static }},
		{`position: running(header); position: absolute`, func(s *Style) bool { return s.Running == "" && s.Position == Absolute }},
		{`position: running()`, func(s *Style) bool { return s.Running == "" && s.Position == Static }},
		{`float: footnote`, func(s *Style) bool { return s.Footnote && s.Float == FloatNone }},
		{`float: footnote; float: left`, func(s *Style) bool { return !s.Footnote && s.Float == FloatLeft }},
		{`footnote-display: inline; footnote-policy: line`, func(s *Style) bool {
			return s.FootnoteDisplay == FootnoteInline && s.FootnotePolicy == PolicyLine
		}},
		{`footnote-display: compact; footnote-policy: block`, func(s *Style) bool {
			return s.FootnoteDisplay == FootnoteCompact && s.FootnotePolicy == PolicyBlock
		}},
		{`string-set: title content(), page "p" counter(page) content(before) attr(x)`, func(s *Style) bool {
			return reflect.DeepEqual(s.StringSet, []NamedString{
				{Name: "title", Items: []ContentItem{{Kind: ItemContent, Text: "text"}}},
				{Name: "page", Items: []ContentItem{{Kind: ItemString, Text: "p"}, {Kind: ItemCounter, Text: "page", Style: Decimal}, {Kind: ItemContent, Text: "before"}, {Kind: ItemAttr, Text: "x"}}},
			})
		}},
		{`string-set: none`, func(s *Style) bool { return s.StringSet == nil }},
		{`string-set: title content(bogus)`, func(s *Style) bool { return s.StringSet == nil }},
		{`string-set: title`, func(s *Style) bool { return s.StringSet == nil }},
		{`string-set: title url(a.png)`, func(s *Style) bool { return s.StringSet == nil }},
	}
	for _, tt := range tests {
		if st := styleOf(t, tt.decls); !tt.check(st) {
			t.Errorf("%s: Running %q Position %v Footnote %v StringSet %+v", tt.decls, st.Running, st.Position, st.Footnote, st.StringSet)
		}
	}
}

func TestGCPM_UsedBleed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		ps   PageStyle
		want float64
	}{
		{PageStyle{}, 0},
		{PageStyle{Marks: MarksCross}, 0},
		{PageStyle{Marks: MarksCrop}, 6},
		{PageStyle{Marks: MarksCrop, Bleed: Pts(2)}, 2},
		{PageStyle{Bleed: Pts(3)}, 3},
	}
	for _, tt := range tests {
		if got := tt.ps.UsedBleed(); got != tt.want {
			t.Errorf("%+v: %v, want %v", tt.ps, got, tt.want)
		}
	}
}

func TestGCPM_PageBleedAndMarks(t *testing.T) {
	t.Parallel()
	tests := []struct {
		css   string
		bleed Dimension
		marks Marks
		fixed bool
	}{
		{`@page { bleed: 3pt; marks: crop cross }`, Pts(3), MarksCrop | MarksCross, false},
		{`@page { marks: cross crop; marks: none }`, Dimension{}, 0, false},
		{`@page { bleed: auto; marks: crop crop }`, Dimension{Kind: Auto}, 0, false},
		{`@page { bleed: 1px 2px; background: red fixed }`, Dimension{}, 0, true},
		{`@page { bleed: 50%; background-attachment: fixed }`, Dimension{}, 0, true},
	}
	for _, tt := range tests {
		r := NewResolver(a4, []Sheet{{Sheet: mustParse(t, tt.css), Origin: Author}})
		ps := r.Page(PageSelector{First: true, Right: true})
		fixed := ps.Background != nil && ps.Background.BackgroundAttachment == AttachFixed
		if ps.Bleed != tt.bleed || ps.Marks != tt.marks || fixed != tt.fixed {
			t.Errorf("%s: bleed %+v marks %v fixed %v", tt.css, ps.Bleed, ps.Marks, fixed)
		}
	}
}
