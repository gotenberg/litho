package odt

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
)

// TestConvert_ShapeTable draws the loext:table LibreOffice writes in a
// shape's text between the paragraphs around it.
func TestConvert_ShapeTable(t *testing.T) {
	body := `<text:p><draw:custom-shape text:anchor-type="paragraph" svg:width="10cm" svg:height="6cm" svg:x="1cm" svg:y="1cm"><text:p>Before</text:p>` +
		`<loext:table table:name="T1"><loext:table-column/><loext:table-column/><loext:table-row><loext:table-cell office:value-type="string"><text:p>A1</text:p></loext:table-cell><loext:table-cell office:value-type="string"><text:p>B1</text:p></loext:table-cell></loext:table-row></loext:table>` +
		`<text:p>After</text:p><draw:enhanced-geometry svg:viewBox="0 0 21600 21600" draw:type="rectangle" draw:enhanced-path="M 0 0 L 21600 0 21600 21600 0 21600 Z N"/></draw:custom-shape></text:p>`
	res := convertDoc(t, doc{body: body}.pack(t), office.Options{})
	text := pageText(res.Pages[0])
	for _, want := range []string{"Before", "A1", "B1", "After"} {
		if !strings.Contains(text, want) {
			t.Errorf("page text %q lacks %q", text, want)
		}
	}
}
