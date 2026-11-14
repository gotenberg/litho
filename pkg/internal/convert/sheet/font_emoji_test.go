package sheet

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// TestTextStyle_ExcelDrawsEmojiAsText locks that cell text under Excel's
// rules asks for the text presentation of emoji and under Calc's leaves
// it to the text stack (see [excelEmoji]).
func TestTextStyle_ExcelDrawsEmojiAsText(t *testing.T) {
	f := font{families: []string{"Calibri"}, size: 11}
	if s := f.textStyle("en", text.FallbackFontconfig).Settings; s == nil || s.Emoji != text.EmojiText {
		t.Errorf("Excel settings %+v, want text presentation", s)
	}
	if s := f.textStyle("en", text.FallbackLibreOffice).Settings; s != nil {
		t.Errorf("Calc settings %+v, want none", s)
	}
}

// TestConvert_ExcelPrintsEmojiAsText locks that a cell's emoji prints in
// a face of outlines under Excel's rules, as it is measured, and in the
// colour face under Calc's.
func TestConvert_ExcelPrintsEmojiAsText(t *testing.T) {
	for _, excel := range []bool{false, true} {
		sh := &Sheet{Name: "S", Rows: []Row{{Index: 0, Cells: []Cell{textCell(0, "a \U0001F600", nil)}}}}
		doc := newDoc(sh)
		doc.ExcelRules = excel
		var key string
		var walk func([]typeset.Fragment)
		walk = func(fs []typeset.Fragment) {
			for _, f := range fs {
				switch v := f.(type) {
				case *typeset.Group:
					walk(v.Content)
				case *typeset.Text:
					for _, g := range v.Glyphs {
						if g.Text == "\U0001F600" && v.Face != nil {
							key = v.Face.Key
						}
					}
				}
			}
		}
		walk(convert(t, doc, office.Options{}).Pages[0].Content)
		if key == "" {
			t.Fatalf("excel %v: no emoji drawn", excel)
		}
		if colour := strings.Contains(key, "ColorEmoji"); colour == excel {
			t.Errorf("excel %v: emoji drawn in %s", excel, key)
		}
	}
}
