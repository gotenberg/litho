package html

import (
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// containerRun converts src, counting the layouts and keeping the extents
// of the last one.
func containerRun(t *testing.T, src string) (int, *layout.Result, []string) {
	t.Helper()
	n := 0
	var last *layout.Result
	lay := func(ctx context.Context, root *layout.Node, fonts *text.Fonts, opts layout.Options) (*layout.Result, error) {
		n++
		res, err := layout.Layout(ctx, root, fonts, opts)
		last = res
		return res, err
	}
	doc, err := convert(context.Background(), strings.NewReader(src), testFonts(t), Options{}, lay)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	var msgs []string
	for _, w := range doc.Warnings {
		if w.Code == warn.LimitReached {
			msgs = append(msgs, w.Message)
		}
	}
	return n, last, msgs
}

// TestContainerQueries_LaidOutAgainWithMeasuredSizes locks the passes of a
// document whose styles query its containers' sizes (CSS Conditional 5,
// 6.1 and 7): the first layout measures them, the next styles the
// document with them, and the passes end once a layout measures the
// sizes its styles read. A document that queries no container's size is
// laid out once.
func TestContainerQueries_LaidOutAgainWithMeasuredSizes(t *testing.T) {
	t.Parallel()
	const head = `<style>body { margin: 0 } p { margin: 0 } .c { container-type: inline-size }</style>`
	tests := []struct {
		name, src string
		layouts   int
		id        string
		x         float64
	}{
		{"no container", head + `<p id=r>x</p><p id=p style="margin-left: 10cqw; width: 10cqw">x</p>`, 1, "p", -1},
		{"style query alone", head + `<style>@container style(--a: b) { #p { margin-left: 75pt } }</style><div style="--a: b"><p id=r>x</p><p id=p>x</p></div>`, 1, "p", 75},
		{"one level", head + `<style>@container (width > 200px) { #p { margin-left: 75pt } }</style><div class=c style="width: 300px"><p id=r>x</p><p id=p>x</p></div>`, 2, "p", 75},
		{"container units", head + `<div class=c style="width: 400px"><p id=r>x</p><p id=p style="margin-left: 10cqw">x</p></div>`, 2, "p", 30},
		{
			"a container sized by an outer query",
			head + `<style>.i { width: 350px } @container (width > 300px) { .i { width: 200px } } @container (width < 250px) { #p { margin-left: 75pt } }</style>` +
				`<div class=c style="width: 400px"><div class="c i"><p id=r>x</p><p id=p>x</p></div></div>`,
			3, "p", 75,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			n, res, msgs := containerRun(t, tt.src)
			if n != tt.layouts {
				t.Errorf("%d layouts, want %d", n, tt.layouts)
			}
			if len(msgs) != 0 {
				t.Errorf("warnings %q", msgs)
			}
			p, r := res.Extents[tt.id], res.Extents["r"]
			want := tt.x
			if want < 0 {
				// Outside any container the units are the small
				// viewport's: the margin is as wide as the box.
				want = p.Width
			}
			if got := p.X - r.X; got < want-0.01 || got > want+0.01 {
				t.Errorf("#%s %v right of #r, want %v", tt.id, got, want)
			}
		})
	}
}

func TestContainerQueries_LayoutsAreBounded(t *testing.T) {
	t.Parallel()
	// Each container's width depends on a query of the one around it: one
	// more layout a level, past maxContainerLayouts the last sizes answer.
	src := `<style>body { margin: 0 } div { container-type: inline-size; width: 100px }
#a { width: 500px !important }
@container (width > 450px) { #b { width: 440px } }
@container (width > 400px) { #c { width: 390px } }
@container (width > 350px) { #d { width: 340px } }
@container (width > 300px) { #e { width: 290px } }</style>
<div id=a><div id=b><div id=c><div id=d><div id=e>x</div></div></div></div></div>`
	n, _, msgs := containerRun(t, src)
	if n != maxContainerLayouts {
		t.Errorf("%d layouts, want %d", n, maxContainerLayouts)
	}
	if len(msgs) != 1 || !strings.Contains(msgs[0], "container queries") {
		t.Errorf("warnings %q, want the container layouts' one", msgs)
	}
}
