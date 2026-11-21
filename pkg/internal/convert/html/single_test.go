package html

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

func TestChromiumPaper_CeilsToPointsThenDots(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ in, want float64 }{
		{598.2, 599.04}, {599, 599.04}, {1087.53, 1087.92}, {384, 384}, {595.276, 595.92}, {841.89, 841.92},
	} {
		if got := chromiumPaper(tt.in); !near6(got, tt.want) {
			t.Errorf("chromiumPaper(%v) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

// TestConvert_SinglePageCutWarns states the single page's bound: content
// measured past [Options.FittedLimit] is left out of the single page, and
// the conversion says so, as a text document's does, while content past
// the 14,400 points of ISO 32000-1, Annex C.2, an informative limit, stays
// whole. A page a forced break ends before the bound loses what follows
// the break, as Chromium prints it, and warns of nothing.
func TestConvert_SinglePageCutWarns(t *testing.T) {
	t.Parallel()
	cut := func(doc *typeset.Document) bool {
		for _, w := range doc.Warnings {
			if w.Code == warn.LimitReached && w.Effect == warn.Omitted && strings.Contains(w.Message, "single page") {
				return true
			}
		}
		return false
	}
	src := `<body style="margin:0"><div style="height:20000pt"></div><p>theend</p>`
	whole := conv(t, src, Options{SinglePage: true, MarginTop: 10, MarginBottom: 10})
	if len(whole.Pages) != 1 || whole.Pages[0].Height <= maxPageSide || !whole.Pages[0].Fitted || !strings.Contains(pageText(whole.Pages[0]), "theend") || cut(whole) {
		t.Fatalf("%d pages, the first %v tall, fitted %v; want one whole page past %v points", len(whole.Pages), whole.Pages[0].Height, whole.Pages[0].Fitted, maxPageSide)
	}
	tall := conv(t, src, Options{SinglePage: true, MarginTop: 10, MarginBottom: 10, FittedLimit: 15000})
	if len(tall.Pages) != 1 || tall.Pages[0].Height != 15000 {
		t.Fatalf("%d pages, the first %v tall; want one 15000 tall", len(tall.Pages), tall.Pages[0].Height)
	}
	if strings.Contains(pageText(tall.Pages[0]), "theend") {
		t.Error("the content past the tallest page is drawn")
	}
	if !cut(tall) {
		t.Errorf("no warning of the content left out: %+v", tall.Warnings)
	}
	broken := conv(t, `<body style="margin:0"><p>before</p><p style="break-before:page">after</p>`, Options{SinglePage: true})
	if cut(broken) {
		t.Errorf("a forced break warns of content past the tallest page: %+v", broken.Warnings)
	}
}
