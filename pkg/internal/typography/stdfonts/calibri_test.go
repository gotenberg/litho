package stdfonts

import "testing"

// TestCalibriTableComposesThePDFJSMaps pins the composition PDF.js applies to
// a non-embedded Calibri: its standard map first, then the Calibri
// supplement over it.
func TestCalibriTableComposesThePDFJSMaps(t *testing.T) {
	t.Parallel()

	calibri := IdentityGlyphMap("Calibri")
	want := len(pdfjsStandardFontGlyphs)
	for gid, r := range pdfjsCalibriGlyphs {
		if _, shared := pdfjsStandardFontGlyphs[gid]; !shared {
			want++
		}
		if calibri[gid] != r {
			t.Errorf("GID %d = %q, want the supplement's %q", gid, calibri[gid], r)
		}
	}
	for gid, r := range pdfjsStandardFontGlyphs {
		if _, over := pdfjsCalibriGlyphs[gid]; !over && calibri[gid] != r {
			t.Errorf("GID %d = %q, want the standard map's %q", gid, calibri[gid], r)
		}
	}
	if len(calibri) != want {
		t.Errorf("table holds %d entries, want %d", len(calibri), want)
	}

	// Spot values, so a regenerated table that parsed to nothing, or to the
	// wrong PDF.js function, fails here too.
	for gid, r := range map[uint16]rune{1: ' ', 3: ' ', 4: 'A', 383: 'ó', 856: '.', 1004: '0'} {
		if calibri[gid] != r {
			t.Errorf("GID %d = %q, want %q", gid, calibri[gid], r)
		}
	}
}
