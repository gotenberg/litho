package docx

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

func TestRevisions_JoinedParagraphs(t *testing.T) {
	t.Parallel()
	p := func(ppr, runs string) string { return `<w:p><w:pPr>` + ppr + `</w:pPr>` + runs + `</w:p>` }
	r := func(text string) string { return `<w:r><w:t>` + text + `</w:t></w:r>` }
	hidden := func(text string) string { return `<w:r><w:rPr><w:vanish/></w:rPr><w:t>` + text + `</w:t></w:r>` }
	del := `<w:rPr><w:del w:id="1" w:author="a"/></w:rPr>`
	vanish := `<w:rPr><w:vanish/></w:rPr>`
	for _, tc := range []struct {
		name  string
		body  string
		want  []string
		align css.TextAlign
	}{
		{"deleted mark", p(`<w:jc w:val="center"/>`+del, r("abc")) + p(``, r("def")), []string{"abcdef"}, css.AlignCenter},
		{"two deleted marks", p(del, r("a")) + p(del, r("b")) + p(``, r("c")) + p(``, r("d")), []string{"abc", "d"}, css.AlignStart},
		{"hidden mark", p(`<w:jc w:val="center"/>`+vanish, r("abc")) + p(``, r("def")), []string{"abcdef"}, css.AlignCenter},
		{"hidden paragraph", p(`<w:jc w:val="center"/>`+vanish, hidden("abc")) + p(``, r("def")), []string{"def"}, css.AlignStart},
		{"last paragraph", p(``, r("x")) + p(del, r("y")), []string{"x", "y"}, css.AlignStart},
	} {
		b := build(t, tc.body, nil)
		var got []string
		ps := role(b.root, "P")
		for _, n := range ps {
			got = append(got, textOf(n))
		}
		if strings.Join(got, "|") != strings.Join(tc.want, "|") {
			t.Errorf("%s: paragraphs %q, want %q", tc.name, got, tc.want)
			continue
		}
		if ps[0].Style.TextAlign != tc.align {
			t.Errorf("%s: alignment %v, want %v", tc.name, ps[0].Style.TextAlign, tc.align)
		}
	}
}
