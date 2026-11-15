package docx

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/layout"
)

// undescribed returns a paragraph holding one inline picture its source
// does not describe.
func undescribed(id string) string {
	return `<w:p><w:r><w:drawing><wp:inline><wp:extent cx="1270000" cy="635000"/><wp:docPr id="` + id + `" name="Picture ` + id + `"/>` +
		`<a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/picture"><pic:pic>` +
		`<pic:nvPicPr><pic:cNvPr id="` + id + `" name="image1.png"/><pic:cNvPicPr/></pic:nvPicPr>` +
		`<pic:blipFill><a:blip r:embed="rIda"/></pic:blipFill><pic:spPr/></pic:pic></a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p>`
}

// caption returns a caption paragraph of the sequence seq: a simple SEQ
// field, or a complex one.
func caption(seq, text string, complexField bool) string {
	field := `<w:fldSimple w:instr=" SEQ ` + seq + ` \* ARABIC "><w:r><w:t>1</w:t></w:r></w:fldSimple>`
	if complexField {
		field = `<w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText xml:space="preserve"> SEQ ` + seq + ` \* ARABIC </w:instrText></w:r>` +
			`<w:r><w:fldChar w:fldCharType="separate"/></w:r><w:r><w:t>1</w:t></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r>`
	}
	return `<w:p><w:pPr><w:pStyle w:val="Caption"/></w:pPr><w:r><w:t xml:space="preserve">` + seq + ` </w:t></w:r>` + field +
		`<w:r><w:t xml:space="preserve">: ` + text + `</w:t></w:r></w:p>`
}

// TestCaptions holds the rule that ties a Word caption to a picture
// (document.markCaptions): the caption paragraph beside the paragraph of
// a picture alone, after or before it, unless either could be another's.
func TestCaptions(t *testing.T) {
	t.Parallel()
	text := `<w:p><w:r><w:t>Some text</w:t></w:r></w:p>`
	cases := []struct {
		name, body string
		want       []string
	}{
		{"below", undescribed("1") + caption("Figure", "Sales  by region", false), []string{"Figure 1: Sales by region"}},
		{"above, complex field", text + caption("Figure", "Above", true) + undescribed("1") + text, []string{"Figure 1: Above"}},
		{"another label", undescribed("1") + caption("Abbildung", "Umsatz", false), []string{"Abbildung 1: Umsatz"}},
		{"a table caption", undescribed("1") + caption("Table", "Numbers", false), []string{""}},
		{"no field", undescribed("1") + `<w:p><w:r><w:t>Figure 1: plain text</w:t></w:r></w:p>`, []string{""}},
		{"caption between two pictures", undescribed("1") + caption("Figure", "Which", false) + undescribed("2"), []string{"", ""}},
		{"picture between two captions", caption("Figure", "One", false) + undescribed("1") + caption("Figure", "Two", false), []string{""}},
		{"picture beside text", `<w:p><w:r><w:t>Look:</w:t></w:r>` + undescribed("1")[5:] + caption("Figure", "Beside", false), []string{""}},
		{"a table between", undescribed("1") + `<w:tbl><w:tr><w:tc><w:p/></w:tc></w:tr></w:tbl>` + caption("Figure", "Far", false), []string{""}},
		{"a described picture keeps its description", `<w:p><w:r><w:drawing><wp:inline><wp:extent cx="1270000" cy="635000"/><wp:docPr id="1" name="p" descr="Own"/>` +
			graphic("") + `</wp:inline></w:drawing></w:r></w:p>` + caption("Figure", "Caption", false), []string{"Own"}},
	}
	for _, tc := range cases {
		extra := map[string]string{"word/media/a.png": pngData(t, 4, 4)}
		on := buildWith(t, tc.body, extra, office.Options{DerivedAltText: true})
		pics := find(on.root, func(n *layout.Node) bool { return n.Kind == layout.Replaced })
		if len(pics) != len(tc.want) {
			t.Fatalf("%s: %d pictures, want %d", tc.name, len(pics), len(tc.want))
		}
		for i, p := range pics {
			if p.Role != "Figure" || p.Alt != tc.want[i] {
				t.Errorf("%s: picture %d role %q alt %q, want Figure %q", tc.name, i, p.Role, p.Alt, tc.want[i])
			}
		}
		for i, p := range find(build(t, tc.body, extra).root, func(n *layout.Node) bool { return n.Kind == layout.Replaced }) {
			if want := map[bool]string{true: "Own"}[tc.want[i] == "Own"]; p.Alt != want {
				t.Errorf("%s without the option: picture %d alt %q, want %q", tc.name, i, p.Alt, want)
			}
		}
	}
}
