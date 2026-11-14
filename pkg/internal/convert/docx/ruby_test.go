package docx

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// TestRuby_DrawsBaseAndAnnotation locks that a w:ruby in a run draws its
// base in line and its annotation as a ruby annotation over it, aligned
// as w:rubyAlign says, and that the text of both reaches the page.
func TestRuby_DrawsBaseAndAnnotation(t *testing.T) {
	ruby := func(align string) string {
		return `<w:r><w:ruby><w:rubyPr><w:rubyAlign w:val="` + align + `"/><w:hps w:val="10"/><w:hpsRaise w:val="18"/><w:hpsBaseText w:val="20"/></w:rubyPr>` +
			`<w:rt><w:r><w:rPr><w:sz w:val="10"/></w:rPr><w:t>abcdef</w:t></w:r></w:rt>` +
			`<w:rubyBase><w:r><w:t>XY</w:t></w:r></w:rubyBase></w:ruby></w:r>`
	}
	body := `<w:p><w:r><w:t>Before </w:t></w:r>` + ruby("distributeLetter") + `<w:r><w:t> after</w:t></w:r></w:p>`
	b := build(t, body, nil)
	rubies := find(b.root, func(n *layout.Node) bool { return n.Style != nil && n.Style.Display == css.Ruby })
	if len(rubies) != 1 {
		t.Fatalf("%d rubies, want 1", len(rubies))
	}
	if got := rubies[0].Style.RubyAlign; got != css.RubySpaceBetween {
		t.Errorf("ruby-align %v, want space-between for distributeLetter", got)
	}
	ann := find(rubies[0], func(n *layout.Node) bool { return n.Style != nil && n.Style.Display == css.RubyText })
	if len(ann) != 1 || textOf(ann[0]) != "abcdef" {
		t.Fatalf("annotation %v, want one holding abcdef", ann)
	}
	doc := convertBytes(t, docx(t, body, nil), office.Options{})
	// ISO 32005, Table 5: a Ruby holds RB, RT and RP alone.
	var walk func(e *typeset.StructElem, depth int)
	found := false
	walk = func(e *typeset.StructElem, depth int) {
		if e == nil || depth > 64 {
			return
		}
		if e.Role == "Ruby" {
			found = true
			for _, k := range e.Kids {
				if k.Role != "RB" && k.Role != "RT" && k.Role != "RP" {
					t.Errorf("a Ruby holds a %s", k.Role)
				}
			}
		}
		for _, k := range e.Kids {
			walk(k, depth+1)
		}
	}
	walk(doc.Structure, 0)
	if !found {
		t.Error("no Ruby structure element")
	}
	text := docText(doc)
	for _, want := range []string{"Before", "XY", "abcdef", "after"} {
		if !strings.Contains(text, want) {
			t.Errorf("page text %q lacks %q", text, want)
		}
	}
}
