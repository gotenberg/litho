package layout

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

func TestLineClamp_KeepsLines(t *testing.T) {
	clamp := func(n int, mark css.OverflowMark) func(*css.Style) {
		return func(s *css.Style) {
			s.LineClamp.MaxLines, s.LineClamp.Continue, s.LineClamp.Ellipsis = n, css.ContinueDiscard, mark
		}
	}
	auto := css.OverflowMark{Kind: css.MarkEllipsis}
	ws := func(s *css.Style) { s.WhiteSpace = css.Pre }
	tests := []struct {
		name  string
		root  *Node
		width float64
		want  []string
	}{
		{"count with ellipsis", doc(el(sty(clamp(2, auto)), txt(sty(), "aa bb cc dd"))), 30, []string{"aa", "bb…"}},
		{"ellipsis cuts the line", doc(el(sty(clamp(2, auto)), txt(sty(), "aaa bbb ccc"))), 30, []string{"aaa", "bb…"}},
		{"string mark", doc(el(sty(clamp(1, css.OverflowMark{Kind: css.MarkString, Text: "+"})), txt(sty(), "a b c"))), 30, []string{"a +"}},
		{"no mark", doc(el(sty(clamp(1, css.OverflowMark{})), txt(sty(), "a b c"))), 30, []string{"a b"}},
		{"no ellipsis when nothing follows", doc(el(sty(clamp(2, auto)), txt(sty(), "aa bb"))), 30, []string{"aa", "bb"}},
		{"lines of nested blocks count", doc(el(sty(clamp(2, auto)), el(sty(ws), txt(sty(ws), "a\nb")), el(sty(), txt(sty(), "c")))), 100, []string{"a", "b…"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := run(t, tt.root, page(tt.width, 200))
			got, _ := lineTexts(res, 1)
			for i := range got {
				got[i] = strings.TrimRight(got[i], " ")
			}
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Errorf("lines %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLineClamp_SizesTheBox(t *testing.T) {
	st := sty(func(s *css.Style) { s.LineClamp.MaxLines, s.LineClamp.Continue = 2, css.ContinueDiscard })
	b := boxes(t, doc(el(st, txt(sty(), "a b c d"))), page(20, 200))
	if got := kid(t, b, 0).h; !near(got, px(20)) {
		t.Errorf("height %v, want two lines of %v", got, px(20))
	}
}

// TestLineClamp_FloatsDoNotGrowTheBox checks that a float in a line-clamp
// container makes it no taller (CSS Overflow 4, 5.3): its height ends
// with its lines, clamped or not.
func TestLineClamp_FloatsDoNotGrowTheBox(t *testing.T) {
	for _, n := range []int{2, 3} {
		st := sty(func(s *css.Style) { s.LineClamp.MaxLines, s.LineClamp.Continue = n, css.ContinueDiscard })
		fl := el(sty(float(css.FloatLeft), width(1), height(100)))
		b := boxes(t, doc(el(st, fl, txt(sty(), "a b"))), page(20, 200))
		if got := kid(t, b, 0).h; !near(got, px(20)) {
			t.Errorf("max-lines %d: height %vpx, want the two lines' 20px", n, got/px(1))
		}
	}
}

func TestLineClamp_AutoStopsAtMaxHeight(t *testing.T) {
	st := sty(func(s *css.Style) {
		s.LineClamp.Auto, s.LineClamp.Continue = true, css.ContinueDiscard
		s.LineClamp.Ellipsis = css.OverflowMark{Kind: css.MarkEllipsis}
		s.MaxHeight = css.Pts(px(25))
	})
	res := run(t, doc(el(st, txt(sty(), "a b c d"))), page(20, 200))
	got, _ := lineTexts(res, 1)
	if len(got) != 2 || !strings.HasSuffix(got[1], "…") {
		t.Errorf("lines %q, want two, the last ending with an ellipsis", got)
	}
}

func TestTextOverflow_Ellipsis(t *testing.T) {
	over := func(m css.OverflowMark) func(*css.Style) {
		return func(s *css.Style) {
			s.Overflow, s.TextOverflow, s.WhiteSpace = css.OverflowHidden, m, css.Nowrap
		}
	}
	tests := []struct {
		name string
		st   *css.Style
		want string
	}{
		{"ellipsis", sty(over(css.OverflowMark{Kind: css.MarkEllipsis})), "ab…"},
		{"string", sty(over(css.OverflowMark{Kind: css.MarkString, Text: "~~"})), "a~~"},
		{"clip", sty(over(css.OverflowMark{})), "abcdef"},
		{"visible overflow shows no mark", sty(func(s *css.Style) {
			s.TextOverflow, s.WhiteSpace = css.OverflowMark{Kind: css.MarkEllipsis}, css.Nowrap
		}), "abcdef"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := run(t, doc(el(tt.st, txt(sty(), "abcdef"))), page(30, 100))
			got, _ := lineTexts(res, 1)
			if strings.Join(got, "|") != tt.want {
				t.Errorf("lines %q, want %q", got, tt.want)
			}
		})
	}
}
