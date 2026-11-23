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
