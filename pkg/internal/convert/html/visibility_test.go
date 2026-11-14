package html

import (
	"strings"
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

func TestHiddenMetadata_DisplayedByTheAuthorDraws(t *testing.T) {
	t.Parallel()
	// HTML Standard, 15.3.1: head, title, style and script are hidden by
	// the user agent sheet's display: none, which an author rule
	// overrides; template stays inert.
	src := `<!DOCTYPE html><html><head><title>T1</title><style>head, title, style, script { display: block } /*S1*/</style></head>` +
		`<body><p>B1</p><script>var x = "J1";</script><template>T2</template><style>/*S2*/</style></body></html>`
	p := prep(t, src, Options{})
	got := nodeText(p.root)
	for _, want := range []string{"T1", "S1", "B1", "J1", "S2"} {
		if !strings.Contains(got, want) {
			t.Errorf("text %q lacks %q", got, want)
		}
	}
	if strings.Contains(got, "T2") {
		t.Errorf("text %q draws the template", got)
	}
	plain := nodeText(prep(t, `<!DOCTYPE html><title>T1</title><style>/*S1*/</style><p>B1</p><script>J1</script>`, Options{}).root)
	if strings.TrimSpace(plain) != "B1" {
		t.Errorf("text %q, want B1 alone", plain)
	}
}
