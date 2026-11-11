package office

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
)

func TestShownText(t *testing.T) {
	t.Parallel()
	block, inline := css.Initial(), css.Initial()
	block.Display = css.Block
	inline.Display = css.Inline
	txt := func(s string) *layout.Node { return &layout.Node{Kind: layout.Text, Style: inline, Text: s} }
	p := func(kids ...*layout.Node) *layout.Node {
		return &layout.Node{Kind: layout.Element, Style: block, Children: kids}
	}
	nodes := []*layout.Node{
		p(txt("Hello "), &layout.Node{Kind: layout.Element, Style: inline, Children: []*layout.Node{txt("big")}}, txt("  world")),
		p(txt("next"), &layout.Node{Kind: layout.LineBreak, Style: inline}, txt("line")),
	}
	if got := ShownText(nodes); got != "Hello big world next line" {
		t.Errorf("ShownText = %q", got)
	}
	long := ShownText([]*layout.Node{p(txt(strings.Repeat("é", MaxAltText)))})
	if len(long) > MaxAltText || !strings.HasPrefix(long, "éé") || strings.ContainsRune(long, '�') {
		t.Errorf("long text cut to %d bytes", len(long))
	}
}
