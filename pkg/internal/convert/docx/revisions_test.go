package docx

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
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
		{"deleted mark", p(`<w:jc w:val="center"/>`+del, r("abc")) + p(``, r("def")), []string{"abcdef"}, css.AlignStart},
		{"deleted mark before a centred paragraph", p(del, r("abc")) + p(`<w:jc w:val="center"/>`, r("def")), []string{"abcdef"}, css.AlignCenter},
		{"two deleted marks", p(del, r("a")) + p(del, r("b")) + p(``, r("c")) + p(``, r("d")), []string{"abc", "d"}, css.AlignStart},
		{"hidden mark", p(`<w:jc w:val="center"/>`+vanish, r("abc")) + p(``, r("def")), []string{"abcdef"}, css.AlignStart},
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

// views are the three tracked-changes views, in office's order.
var views = []office.TrackedChanges{office.ChangesAccepted, office.ChangesRejected, office.ChangesMarked}

func TestRevisions_Views(t *testing.T) {
	t.Parallel()
	r := func(text string) string { return `<w:r><w:t xml:space="preserve">` + text + `</w:t></w:r>` }
	dr := func(text string) string { return `<w:r><w:delText xml:space="preserve">` + text + `</w:delText></w:r>` }
	ins := func(author, content string) string {
		return `<w:ins w:id="1" w:author="` + author + `" w:date="2026-01-03T09:00:00Z">` + content + `</w:ins>`
	}
	del := func(author, content string) string {
		return `<w:del w:id="2" w:author="` + author + `" w:date="2026-01-03T09:00:00Z">` + content + `</w:del>`
	}
	p := func(ppr, runs string) string { return `<w:p><w:pPr>` + ppr + `</w:pPr>` + runs + `</w:p>` }
	mark := func(kind string) string { return `<w:rPr><w:` + kind + ` w:id="3" w:author="Ada"/></w:rPr>` }
	cellP := func(content string) string { return `<w:tc><w:p>` + content + `</w:p></w:tc>` }
	row := func(trPr, a, b string) string {
		return `<w:tr><w:trPr>` + trPr + `</w:trPr>` + cellP(a) + cellP(b) + `</w:tr>`
	}
	for _, tc := range []struct {
		name string
		body string
		// want holds the paragraphs of each view, joined by "|".
		want [3]string
	}{
		{"insertion", p(``, r("the ")+ins("Ada", r("quick "))+r("fox")), [3]string{"the quick fox", "the fox", "the quick fox"}},
		{"deletion", p(``, r("the ")+del("Ada", dr("slow "))+r("fox")), [3]string{"the fox", "the slow fox", "the slow fox"}},
		{"move", p(``, `<w:moveFrom w:id="4" w:author="Ada">`+r("moved ")+`</w:moveFrom>`+r("stays")) + p(``, r("to ")+`<w:moveTo w:id="5" w:author="Ada">`+r("moved")+`</w:moveTo>`),
			[3]string{"stays|to moved", "moved stays|to ", "moved stays|to moved"}},
		{"deleted paragraph mark", p(mark("del"), r("a")) + p(``, r("b")), [3]string{"ab", "a|b", "a|b"}},
		{"inserted paragraph mark", p(mark("ins"), r("a")) + p(``, r("b")), [3]string{"a|b", "ab", "a|b"}},
		{"moved paragraph mark", p(mark("moveFrom"), r("a")) + p(mark("moveTo"), r("b")) + p(``, r("c")), [3]string{"ab|c", "a|bc", "a|b|c"}},
		{"inserted paragraph", p(mark("ins"), ins("Ada", r("new"))) + p(``, r("next")), [3]string{"new|next", "next", "new|next"}},
		{"block-level insertion", ins("Ada", p(``, r("block"))) + p(``, r("after")), [3]string{"block|after", "after", "block|after"}},
		{"deleted field", p(``, r("page ")+del("Ada", `<w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:delInstrText> PAGE </w:delInstrText></w:r><w:r><w:fldChar w:fldCharType="separate"/></w:r>`+dr("7")+`<w:r><w:fldChar w:fldCharType="end"/></w:r>`)),
			[3]string{"page ", "page 7", "page 7"}},
		{"property change keeps the new properties", p(`<w:jc w:val="center"/><w:pPrChange w:id="6" w:author="Ada"><w:pPr><w:jc w:val="right"/></w:pPr></w:pPrChange>`, `<w:r><w:rPr><w:b/><w:rPrChange w:id="7" w:author="Ada"><w:rPr><w:i/></w:rPr></w:rPrChange></w:rPr><w:t>bold</w:t></w:r>`),
			[3]string{"bold", "bold", "bold"}},
		{"rows", `<w:tbl><w:tblGrid><w:gridCol w:w="2000"/><w:gridCol w:w="2000"/></w:tblGrid>` +
			row(``, r("kept"), r("kept")) + row(`<w:ins w:id="8" w:author="Ada"/>`, ins("Ada", r("in")), ins("Ada", r("in"))) +
			row(`<w:del w:id="9" w:author="Ada"/>`, del("Ada", dr("out")), del("Ada", dr("out"))) +
			`<w:tr><w:tc><w:tcPr><w:cellDel w:id="10" w:author="Ada"/></w:tcPr><w:p>` + r("cell") + `</w:p></w:tc><w:tc><w:tcPr><w:cellIns w:id="11" w:author="Ada"/></w:tcPr><w:p>` + r("cell") + `</w:p></w:tc></w:tr></w:tbl>`,
			[3]string{"kept|kept|in|in|cell|cell", "kept|kept|out|out|cell|cell", "kept|kept|in|in|out|out|cell|cell"}},
	} {
		for i, view := range views {
			b := buildWith(t, tc.body, nil, office.Options{TrackedChanges: view})
			var got []string
			for _, n := range role(b.root, "P") {
				got = append(got, textOf(n))
			}
			if g := strings.Join(got, "|"); g != tc.want[i] {
				t.Errorf("%s, view %d: paragraphs %q, want %q", tc.name, view, g, tc.want[i])
			}
		}
	}
}

func TestRevisions_Marks(t *testing.T) {
	t.Parallel()
	r := func(text string) string { return `<w:r><w:t xml:space="preserve">` + text + `</w:t></w:r>` }
	body := `<w:p>` + r("plain ") +
		`<w:ins w:id="1" w:author="Ada">` + `<w:r><w:rPr><w:color w:val="00FF00"/><w:u w:val="double"/></w:rPr><w:t xml:space="preserve">ada </w:t></w:r></w:ins>` +
		`<w:ins w:id="2" w:author="Grace">` + r("grace ") + `</w:ins>` +
		`<w:del w:id="3" w:author="Ada"><w:r><w:delText>gone</w:delText></w:r></w:del></w:p>`
	b := buildWith(t, body, nil, office.Options{TrackedChanges: office.ChangesMarked})
	color := func(hex string) css.Color {
		c, _, ok := css.ParseColor("#" + hex)
		if !ok {
			t.Fatalf("colour %s", hex)
		}
		return c
	}
	for _, tc := range []struct {
		text  string
		deco  css.TextDecoration
		color css.Color
	}{
		{"ada", css.Underline, color(revisionColors[0])},
		{"grace", css.Underline, color(revisionColors[1])},
		{"gone", css.LineThrough, color(revisionColors[0])},
	} {
		st := runWith(t, b.root, tc.text).Style
		if st.TextDecoration != tc.deco || st.TextDecorationStyle != css.DecorationSolid || st.Color != tc.color {
			t.Errorf("%q: decoration %v (style %v), colour %v; want %v in %v", tc.text, st.TextDecoration, st.TextDecorationStyle, st.Color, tc.deco, tc.color)
		}
	}
	if st := runWith(t, b.root, "plain").Style; st.TextDecoration != 0 {
		t.Errorf("plain run decorated: %v", st.TextDecoration)
	}
}
