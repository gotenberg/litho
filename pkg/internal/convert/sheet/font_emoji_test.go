package sheet

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/text"
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
