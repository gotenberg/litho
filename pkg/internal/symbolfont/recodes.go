package symbolfont

import "strings"

// Recodes reports whether family is a symbol font whose codes [Recode]
// recodes through LibreOffice's tables (fontcvt.cxx), as opposed to
// OpenSymbol, StarSymbol or a text font, whose characters stay.
func Recodes(family string) bool {
	_, ok := symbolFonts[strings.ReplaceAll(strings.ToLower(strings.TrimSpace(family)), " ", "")]
	return ok
}
