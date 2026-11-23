package odt

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
)

// TestConvert_ListIndentFromTheStyle keeps the indent of a paragraph
// style that sets both its list style and an indent, and gives the list
// level's indent to a paragraph whose automatic style names the list style
// over a common style of an indent, as LibreOffice prints them.
func TestConvert_ListIndentFromTheStyle(t *testing.T) {
	styles := `<text:list-style style:name="L"><text:list-level-style-number text:level="1" style:num-suffix="." style:num-format="1"><style:list-level-properties text:list-level-position-and-space-mode="label-alignment"><style:list-level-label-alignment text:label-followed-by="listtab" text:list-tab-stop-position="1cm" fo:text-indent="-1cm" fo:margin-left="1cm"/></style:list-level-properties></text:list-level-style-number></text:list-style>` +
		`<style:style style:name="With" style:family="paragraph" style:list-style-name="L"><style:paragraph-properties fo:margin-left="5cm" fo:text-indent="-1cm"/></style:style>` +
		`<style:style style:name="Without" style:family="paragraph"><style:paragraph-properties fo:margin-left="5cm" fo:text-indent="-1cm"/></style:style>`
	auto := `<style:style style:name="P1" style:family="paragraph" style:parent-style-name="Without" style:list-style-name="L"/>`
	body := `<text:list text:style-name="L"><text:list-item><text:p text:style-name="With">styled</text:p></text:list-item></text:list>` +
		`<text:list text:style-name="L"><text:list-item><text:p text:style-name="P1">automatic</text:p></text:list-item></text:list>`
	res := convertDoc(t, doc{styles: styles, contentAuto: auto, body: body}.pack(t), office.Options{})
	want := map[string]float64{"styled": 56.69 + 141.73, "automatic": 56.69 + 28.35}
	for _, r := range runs(res.Pages[0]) {
		if x, ok := want[r.text]; ok {
			delete(want, r.text)
			if !near(r.x, x, 0.5) {
				t.Errorf("%s at %.2f, want %.2f", r.text, r.x, x)
			}
		}
	}
	if len(want) > 0 {
		t.Errorf("not drawn: %v", want)
	}
}

// TestConvert_ListHeadingStaysAHeading locks the structure of a heading
// that is a list item, numbered by its list: an LI whose Lbl holds the
// number and whose LBody holds the heading at its outline level (ISO
// 32000-1, 14.8.4.3.3; ISO 14289-1, 7.4.2), so that a reader meets the
// document's headings in the order of their levels.
//
// # Corpus evidence
//
//   - libreoffice/odt layout/abi11870-2.odt: its numbered headings of
//     levels 1 and 2 read as list items, its first heading left of level 3,
//     which failed PDF/UA-1, 7.4.2.
func TestConvert_ListHeadingStaysAHeading(t *testing.T) {
	auto := `<text:list-style style:name="L1"><text:list-level-style-number text:level="1" style:num-format="1" style:num-suffix="."/>` +
		`<text:list-level-style-number text:level="2" style:num-format="a" style:num-suffix=")"/></text:list-style>`
	body := `<text:list text:style-name="L1"><text:list-item><text:h text:outline-level="1">Title</text:h>` +
		`<text:list><text:list-item><text:h text:outline-level="2">Part</text:h></text:list-item></text:list></text:list-item></text:list>` +
		`<text:p>Body</text:p>`
	res := convertDoc(t, doc{contentAuto: auto, body: body}.pack(t), office.Options{})
	if got, want := roles(res.Structure), "Document,L,LI,Lbl,LBody,H1,L,LI,Lbl,LBody,H2,P"; got != want {
		t.Errorf("structure %s, want %s", got, want)
	}
}
