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

// TestConvert_SinglePageCutWarns states ISO 32000-1, Annex C.2: a page side
// is at most 14,400 units, so content measured past it is left out of the
// single page, and the conversion says so, as a text document's does. A
// page a forced break ends before the bound loses what follows the break,
// as Chromium prints it, and warns of nothing.
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
	tall := conv(t, `<body style="margin:0"><div style="height:20000pt"></div><p>theend</p>`, Options{SinglePage: true, MarginTop: 10, MarginBottom: 10})
	if len(tall.Pages) != 1 || tall.Pages[0].Height != maxPageSide {
		t.Fatalf("%d pages, the first %v tall; want one %v tall", len(tall.Pages), tall.Pages[0].Height, maxPageSide)
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
