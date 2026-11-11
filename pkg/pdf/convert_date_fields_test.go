package pdf

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// TestConvert_DateFields checks that the date fields of a DOCX and an ODT
// source show the conversion date when WithDate sets one, each in its own
// picture or data style, and what the file stored without it.
func TestConvert_DateFields(t *testing.T) {
	t.Parallel()
	fonts := WithConvertFontDirs(convertFontDir(t))
	docx := wordDOCX(t, `<w:p><w:r><w:t xml:space="preserve">on </w:t></w:r>`+
		`<w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText xml:space="preserve"> DATE \@ "yyyy-MM-dd" </w:instrText></w:r>`+
		`<w:r><w:fldChar w:fldCharType="separate"/></w:r><w:r><w:t>2020-01-01</w:t></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r></w:p>`)
	odt := wordODT(`<number:date-style style:name="N1"><number:year number:style="long"/><number:text>-</number:text>`+
		`<number:month number:style="long"/><number:text>-</number:text><number:day number:style="long"/></number:date-style>`,
		`<text:p>on <text:date style:data-style-name="N1" text:date-value="2020-01-01">2020-01-01</text:date></text:p>`)
	date := WithDate(time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC))
	for _, tc := range []struct {
		name string
		c    *Conversion
		want string
	}{
		{"DOCX without a date", FromDOCX(bytes.NewReader(docx), fonts), "on 2020-01-01"},
		{"DOCX with a date", FromDOCX(bytes.NewReader(docx), fonts, date), "on 2026-11-11"},
		{"ODT without a date", FromODT(strings.NewReader(odt), fonts), "on 2020-01-01"},
		{"ODT with a date", FromODT(strings.NewReader(odt), fonts, date), "on 2026-11-11"},
	} {
		if got := convertedText(t, tc.c); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}
