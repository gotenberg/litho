package html

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

func TestContentVisibility_HiddenSkipsTheContents(t *testing.T) {
	t.Parallel()
	// CSS Contain 2, 4.1: hidden keeps the element's box, under size
	// containment, and skips its contents; auto lays them out on paper.
	src := `<div id=h style="content-visibility: hidden">a<b>b</b></div><div id=a style="content-visibility: auto">c</div>`
	p := prep(t, src, Options{})
	h := byID(t, p.root, "h")
	if len(h.Children) != 0 || h.Style.Contain&css.ContainSize == 0 {
		t.Errorf("hidden: %d children, contain %v; want none and size", len(h.Children), h.Style.Contain)
	}
	if a := byID(t, p.root, "a"); len(a.Children) == 0 {
		t.Error("auto: the contents were skipped")
	}
}
