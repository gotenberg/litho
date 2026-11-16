package layout

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

func TestOnePage_LaysEveryPageBreakOutOnOnePage(t *testing.T) {
	t.Parallel()
	named := func(name string, br css.Break) func(*css.Style) {
		return func(s *css.Style) { s.Page, s.BreakBefore = name, br }
	}
	shared := sty(height(10), named("", css.Page))
	cols := sty(func(s *css.Style) {
		s.Columns.Count, s.Columns.Fill = 2, css.FillAuto
	})
	root := doc(
		el(sty(height(10), named("a", css.BreakAuto))),
		el(shared),
		el(sty(height(10), named("b", css.RightPage))),
		el(sty(height(10), named("", css.LeftPage), func(s *css.Style) { s.BreakAfter = css.BreakAvoidPage })),
		el(shared),
		el(cols, el(sty(height(10))), el(sty(height(10), named("", css.Column)))),
	)
	if err := OnePage(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	body := root.Children[0]
	if body.Children[1].Style != body.Children[4].Style {
		t.Error("a style two nodes shared is no longer shared")
	}
	if body.Children[5].Style.Columns.Fill != css.FillBalance {
		t.Error("columns filled in turn, want balanced")
	}
	if br := body.Children[5].Children[1].Style.BreakBefore; br != css.Column {
		t.Errorf("column break %v, want kept", br)
	}
	res, err := Layout(context.Background(), root, ahem(t), Options{Page: page(20, 1000), FitFirstPage: true})
	if err != nil {
		t.Fatal(err)
	}
	// The same blocks with no break, no page name and balanced columns.
	plain := doc(
		el(sty(height(10))), el(sty(height(10))), el(sty(height(10))), el(sty(height(10))), el(sty(height(10))),
		el(sty(func(s *css.Style) { s.Columns.Count = 2 }), el(sty(height(10))), el(sty(height(10), named("", css.Column)))),
	)
	want, err := Layout(context.Background(), plain, ahem(t), Options{Page: page(20, 1000), FitFirstPage: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Pages) != 1 || !near(res.Pages[0].Height, want.Pages[0].Height) || res.Pages[0].Height < px(60) {
		t.Errorf("%d pages, the first %v tall; want one page %v tall", len(res.Pages), res.Pages[0].Height, want.Pages[0].Height)
	}
}

func TestOnePage_EndsWithItsContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := OnePage(ctx, doc(el(sty()))); err == nil {
		t.Error("no error from an ended context")
	}
}
