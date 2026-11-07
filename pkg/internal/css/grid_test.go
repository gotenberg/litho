package css

import (
	"reflect"
	"slices"
	"testing"
)

func lenTrack(v float64) TrackSize {
	b := TrackBreadth{Kind: TrackLength, Value: v * Pt}
	return TrackSize{Min: b, Max: b}
}

func frTrack(v float64) TrackSize {
	return TrackSize{Max: TrackBreadth{Kind: TrackFlex, Value: v}}
}

func TestGrid_TrackLists(t *testing.T) {
	t.Parallel()
	tests := []struct {
		decl  string
		sizes []TrackSize
		names [][]string
		auto  bool
	}{
		{"grid-template-columns: 10px 1fr", []TrackSize{lenTrack(10), frTrack(1)}, [][]string{nil, nil, nil}, false},
		{"grid-template-columns: [a] 10px [b c] auto [d]", []TrackSize{lenTrack(10), {}}, [][]string{{"a"}, {"b", "c"}, {"d"}}, false},
		{"grid-template-columns: repeat(2, [x] 5px)", []TrackSize{lenTrack(5), lenTrack(5)}, [][]string{{"x"}, {"x"}, nil}, false},
		{"grid-template-columns: minmax(10px, 2fr) fit-content(20px)", []TrackSize{
			{Min: TrackBreadth{Kind: TrackLength, Value: 7.5}, Max: TrackBreadth{Kind: TrackFlex, Value: 2}},
			{Max: TrackBreadth{Kind: TrackLength, Value: 15}, FitContent: true},
		}, [][]string{nil, nil, nil}, false},
		{"grid-template-rows: 10px repeat(auto-fill, 20px) 30px", []TrackSize{lenTrack(10), lenTrack(30)}, [][]string{nil, nil, nil, nil}, true},
		{"grid-template-rows: none", nil, nil, false},
	}
	for _, tt := range tests {
		s := styleOf(t, tt.decl)
		l := s.GridTemplateColumns
		if tt.decl[len("grid-template-"):][0] == 'r' {
			l = s.GridTemplateRows
		}
		if !reflect.DeepEqual(l.Sizes, tt.sizes) || !reflect.DeepEqual(l.Names, tt.names) || (l.Auto != nil) != tt.auto {
			t.Errorf("%s: got sizes %v names %q auto %v", tt.decl, l.Sizes, l.Names, l.Auto)
		}
	}
}

func TestGrid_InvalidValues(t *testing.T) {
	t.Parallel()
	for _, decl := range []string{
		"grid-template-columns: 1fr [a] [b]",
		"grid-template-columns: repeat(auto-fill, 1fr)",
		"grid-template-columns: repeat(auto-fill, 10px) repeat(auto-fit, 10px)",
		"grid-template-columns: repeat(0, 10px)",
		"grid-template-columns: minmax(1fr, 10px)",
		"grid-template-columns: subgrid",
		"grid-template-areas: 'a b' 'a'",
		"grid-template-areas: 'a b a'",
		"grid-row-start: 0",
		"grid-row-start: span -1",
		"grid-column: span",
	} {
		s := styleOf(t, decl)
		if len(s.GridTemplateColumns.Sizes) != 0 || s.GridTemplateAreas != nil || !s.GridRowStart.IsAuto() || !s.GridColumnStart.IsAuto() {
			t.Errorf("%s: accepted", decl)
		}
	}
}

func TestGrid_Expand(t *testing.T) {
	t.Parallel()
	s := styleOf(t, "grid-template-columns: [a] 10px [b] repeat(auto-fit, [c] 5px [d]) [e] 20px [f]")
	names, sizes, auto := s.GridTemplateColumns.Expand(2)
	want := [][]string{{"a"}, {"b", "c"}, {"d", "c"}, {"d", "e"}, {"f"}}
	if !reflect.DeepEqual(names, want) || len(sizes) != 4 || auto != 1 || !s.GridTemplateColumns.Auto.Fit {
		t.Errorf("Expand(2) = %q, %v, %d", names, sizes, auto)
	}
}

func TestGrid_Shorthands(t *testing.T) {
	t.Parallel()
	tests := []struct {
		decl  string
		check func(*Style) bool
	}{
		{"grid-area: a", func(s *Style) bool {
			return s.GridRowStart.Name == "a" && s.GridColumnStart.Name == "a" && s.GridRowEnd.Name == "a" && s.GridColumnEnd.Name == "a"
		}},
		{"grid-area: 1 / 2 / 3", func(s *Style) bool {
			return s.GridRowStart.N == 1 && s.GridColumnStart.N == 2 && s.GridRowEnd.N == 3 && s.GridColumnEnd.IsAuto()
		}},
		{"grid-column: 2 / span 3 x", func(s *Style) bool {
			return s.GridColumnStart.N == 2 && s.GridColumnEnd == GridLine{Span: true, N: 3, Name: "x"}
		}},
		{"grid-template: 'h h' 10px 'm s' 1fr / 30px auto", func(s *Style) bool {
			a, ok := s.GridTemplateAreas.Area("h")
			return ok && a.Col1 == 2 && len(s.GridTemplateRows.Sizes) == 2 && len(s.GridTemplateColumns.Sizes) == 2
		}},
		{"grid: auto-flow dense 10px / 1fr 1fr", func(s *Style) bool {
			return s.GridAutoFlow == GridFlowDense && len(s.GridAutoRows) == 1 && len(s.GridTemplateColumns.Sizes) == 2
		}},
		{"grid: 10px / auto-flow 20px", func(s *Style) bool {
			return s.GridAutoFlow == GridFlowColumn && len(s.GridAutoColumns) == 1 && len(s.GridTemplateRows.Sizes) == 1
		}},
		{"grid-auto-flow: column dense", func(s *Style) bool { return s.GridAutoFlow == GridFlowColumn|GridFlowDense }},
		{"place-items: center end", func(s *Style) bool { return s.AlignItems == BoxCenter && s.JustifyItems == BoxEnd }},
		{"justify-self: safe right", func(s *Style) bool { return s.JustifySelf == BoxRight|BoxSafe }},
		{"justify-items: legacy center", func(s *Style) bool { return s.JustifyItems == BoxNormal }},
		{"display: inline grid", func(s *Style) bool { return s.Display == InlineGrid }},
		{"width: stretch; min-width: min-content; max-height: fit-content", func(s *Style) bool {
			return s.Width.Kind == Stretch && s.MinWidth.Kind == MinContent && s.MaxHeight.Kind == FitContent
		}},
	}
	for _, tt := range tests {
		if s := styleOf(t, tt.decl); !tt.check(s) {
			t.Errorf("%s: wrong computed value", tt.decl)
		}
	}
}

func TestGrid_ItemsBlockified(t *testing.T) {
	t.Parallel()
	c := compute(t, `<div id=g><span id=i>x</span></div>`, `#g { display: inline-grid } #i { display: inline }`)
	if got := c.c.Style(byID(c.doc, "i")).Display; got != Block {
		t.Errorf("grid item display = %v, want Block", got)
	}
}

func TestGrid_AreaCells(t *testing.T) {
	t.Parallel()
	cells, ok := areaCells(" a ..  b-1\tc ")
	if !ok || !slices.Equal(cells, []string{"a", "", "b-1", "c"}) {
		t.Errorf("areaCells = %q, %v", cells, ok)
	}
	if _, ok := areaCells("a # b"); ok {
		t.Error("a trash token was accepted")
	}
}

// FuzzTrackList checks the track list, template and placement parsers
// never panic and keep their bounds.
func FuzzTrackList(f *testing.F) {
	for _, s := range []string{
		"10px 1fr", "[a] repeat(3, [b] minmax(10px, 1fr)) [c]", "repeat(auto-fill, 100px)",
		"fit-content(20%) min-content max-content auto", "repeat(1000000, 1px)",
		"'a a' 10px [x] 'b c' / 1fr 2fr", "auto-flow dense / 1fr", "span 2 foo",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		v, _ := parseComponentValues(s, 1<<12)
		v = significant(v)
		if l, ok := parseTrackList(v, false); ok && len(l.sizes)+repeatLen(l.auto) > MaxGridTracks {
			t.Fatalf("%q: %d tracks", s, len(l.sizes))
		}
		parseTemplate(v)
		parseGridLine(v)
		if g, ok := parseGridLine(v); ok && (g.N > MaxGridLine || g.N < -MaxGridLine) {
			t.Fatalf("%q: line %d", s, g.N)
		}
		if l, ok := parseTrackList(v, false); ok && l.auto != nil {
			tl := (&styler{}).computeTrackList(l)
			names, sizes, _ := tl.Expand(3)
			if len(names) != len(sizes)+1 {
				t.Fatalf("%q: %d names for %d tracks", s, len(names), len(sizes))
			}
		}
		var strs []string
		for _, t := range v {
			if t.typ == tString {
				strs = append(strs, t.value)
			}
		}
		parseAreas(strs)
	})
}
