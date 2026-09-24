package compliance

import "testing"

// TestFontFileSubtypeSeparatesEmbeddingFromPermission pins that the two
// questions a FontFile3 /Subtype answers stay separate.
//
// "Does this embed a program a consumer can read" is not "does this part permit
// that program". ISO 19005-1, 6.3.2 test 7 forbids OpenType, yet an OpenType
// FontFile3 still holds a glyph program, so veraPDF reports the font as
// embedded and answers the subtype under 6.3.2 alone. Asking the permission
// question in the embedding check made litho add a 6.3.4 test 1 "font not
// embedded" finding veraPDF never raises.
//
// # Corpus evidence
//
//   - All corpora: the extra 6.3.4 test 1 finding showed on six cells of the
//     remediation differential.
func TestFontFileSubtypeSeparatesEmbeddingFromPermission(t *testing.T) {
	t.Parallel()

	// OpenType is a real glyph format in every part, and permitted from part 2.
	if !namesAFontProgram("OpenType") {
		t.Error("OpenType does not name a font program, so a font using it reads as unembedded")
	}
	if isValidFontFileSubtype(1, "OpenType") {
		t.Error("OpenType is permitted under PDF/A-1, which ISO 19005-1 6.3.2 t7 forbids")
	}
	if !isValidFontFileSubtype(2, "OpenType") {
		t.Error("OpenType is not permitted under PDF/A-2, which allows it")
	}
}

// TestFontFileSubtypeStillRejectsAnUnknownFormat is the control for the case
// the embedding check exists to catch: a subtype naming no format at all states
// no glyph program, so the font is not embedded.
func TestFontFileSubtypeStillRejectsAnUnknownFormat(t *testing.T) {
	t.Parallel()

	for _, subtype := range []string{"", "Bogus", "TrueType", "Type1"} {
		if namesAFontProgram(subtype) {
			t.Errorf("%q names a font program, but no FontFile3 format goes by that name", subtype)
		}
	}
}

// TestFontFileSubtypeAcceptsTheCFFFormats is the control for the ordinary case
// that must keep working in every part.
func TestFontFileSubtypeAcceptsTheCFFFormats(t *testing.T) {
	t.Parallel()

	for _, subtype := range []string{"Type1C", "CIDFontType0C"} {
		if !namesAFontProgram(subtype) {
			t.Errorf("%q does not name a font program", subtype)
		}
		for part := 1; part <= 4; part++ {
			if !isValidFontFileSubtype(part, subtype) {
				t.Errorf("%q is not permitted under PDF/A-%d", subtype, part)
			}
		}
	}
}
