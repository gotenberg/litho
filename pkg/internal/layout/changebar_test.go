package layout

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
)

// changeBars returns the change bars of page n (1-based) of res, as
// "x:top-bottom" in page points rounded to hundredths, in paint order.
func changeBars(t *testing.T, res *Result, n int) string {
	t.Helper()
	var out []string
	var walk func(fs []compose.Fragment, m compose.Matrix)
	walk = func(fs []compose.Fragment, m compose.Matrix) {
		for _, f := range fs {
			switch v := f.(type) {
			case *compose.Group:
				walk(v.Content, mul(m, v.Transform))
			case *compose.Path:
				if v.Stroke == nil || v.Stroke.Width != changeBarWidth {
					continue
				}
				if v.Parent != nil || v.Stroke.Color != compose.Black || len(v.Ops) != 2 {
					t.Errorf("change bar %+v: want a black artifact segment", v)
				}
				a, b := v.Ops[0].P[0], v.Ops[1].P[0]
				x0, y0 := m.A*a.X+m.C*a.Y+m.E, m.B*a.X+m.D*a.Y+m.F
				x1, y1 := m.A*b.X+m.C*b.Y+m.E, m.B*b.X+m.D*b.Y+m.F
				if math.Abs(x0-x1) > 1e-9 {
					t.Errorf("change bar not vertical: %v,%v to %v,%v", x0, y0, x1, y1)
				}
				out = append(out, fmt.Sprintf("%.2f:%.2f-%.2f", x0, y0, y1))
			}
		}
	}
	walk(res.Pages[n-1].Content, compose.Identity)
	return strings.Join(out, " ")
}

// changed returns an inline node of the text s marked ChangeBar.
func changed(s string) *Node {
	st := inlineSty()
	n := el(st, txt(st, s))
	n.ChangeBar = true
	return n
}

// barPage returns a page 35 pixels wide inside a left margin of 40 pixels
// and a top margin of 20: one 3-letter Ahem word a line.
func barPage() PageBox {
	pb := page(75, 200)
	pb.Margin[css.Left], pb.Margin[css.Top] = px(40), px(20)
	return pb
}

// TestChangeBar_Lines locks the lines a change bars: the lines that hold
// changed content and the line before one that opens with it, not the
// line after one that ends with it, each bar 7.05 points left of the
// page area over the line box's height.
//
// # Corpus evidence
//
//   - probe, odt: LibreOffice 26.8 (gotenberg/gotenberg:9.4.1) strokes the
//     bars of a page whose left margin is 3 cm at 78 pt, 0.1 pt wide, one
//     a line box; an insertion opening a line, after the last space of the
//     line before, bars that line too; one ending a line, a whole word or
//     the end of one, leaves the next line unbarred, and one inside a word
//     bars its own line alone.
func TestChangeBar_Lines(t *testing.T) {
	t.Parallel()
	x := px(40) - changeBarGap
	line := func(i int) string {
		top := px(20) + float64(i)*px(10)
		return fmt.Sprintf("%.2f:%.2f-%.2f", x, top, top+px(10))
	}
	st := sty()
	tests := []struct {
		name string
		kids []*Node
		want string
	}{
		{"word on line two", []*Node{txt(st, "aaa "), changed("bbb"), txt(st, " ccc ddd")}, line(0) + " " + line(1)},
		{"word with its space", []*Node{txt(st, "aaa "), changed("bbb "), txt(st, "ccc ddd")}, line(0) + " " + line(1)},
		{"end of line one", []*Node{txt(st, "a"), changed("aa"), txt(st, " bbb ccc ddd")}, line(0)},
		{"inside a word", []*Node{txt(st, "aaa b"), changed("b"), txt(st, "b ccc ddd")}, line(1)},
		{"last line", []*Node{txt(st, "aaa bbb ccc "), changed("ddd")}, line(2) + " " + line(3)},
		{"none", []*Node{txt(st, "aaa bbb ccc ddd")}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res := run(t, doc(el(sty(), tc.kids...)), barPage())
			if got := changeBars(t, res, 1); got != tc.want {
				t.Errorf("bars %q, want %q", got, tc.want)
			}
		})
	}
}

// TestChangeBar_EmptyNode bars the line an empty marked node lands on,
// the line of an empty paragraph included.
func TestChangeBar_EmptyNode(t *testing.T) {
	t.Parallel()
	st := sty()
	empty := el(inlineSty())
	empty.ChangeBar = true
	x := px(40) - changeBarGap
	res := run(t, doc(el(st, txt(st, "aaa"), empty), el(st, empty, br(st)), el(st, txt(st, "bbb"))), barPage())
	want := fmt.Sprintf("%.2f:%.2f-%.2f %.2f:%.2f-%.2f", x, px(20), px(30), x, px(30), px(40))
	if got := changeBars(t, res, 1); got != want {
		t.Errorf("bars %q, want %q", got, want)
	}
}

// TestChangeBar_Columns bars a line of the second column left of that
// column.
//
// # Corpus evidence
//
//   - probe, odt: LibreOffice 26.8 (gotenberg/gotenberg:9.4.1) bars a changed
//     line of the second of two columns 7.05 pt left of that column.
func TestChangeBar_Columns(t *testing.T) {
	t.Parallel()
	st := sty()
	pb := page(100, 30)
	pb.Margin[css.Left] = px(20)
	multi := sty(func(s *css.Style) {
		s.Columns.Count = 2
		s.ColumnGap = css.Pts(px(10))
		s.Width = css.Pts(px(70))
		s.Height = css.Pts(px(10))
	})
	res := run(t, doc(el(multi, el(st, changed("aaa")), el(st, changed("bbb")))), pb)
	want := fmt.Sprintf("%.2f:0.00-%.2f %.2f:0.00-%.2f", px(20)-changeBarGap, px(10), px(60)-changeBarGap, px(10))
	if got := changeBars(t, res, 1); got != want {
		t.Errorf("bars %q, want %q", got, want)
	}
}

// TestChangeBar_InlineBlock draws no bar beside the lines of an inline
// block, as LibreOffice draws none beside a text frame's.
func TestChangeBar_InlineBlock(t *testing.T) {
	t.Parallel()
	st := sty()
	frame := el(sty(display(css.InlineBlock)), el(st, changed("aaa")))
	res := run(t, doc(el(st, txt(st, "b "), frame)), barPage())
	if got := changeBars(t, res, 1); got != "" {
		t.Errorf("bars %q, want none", got)
	}
}
