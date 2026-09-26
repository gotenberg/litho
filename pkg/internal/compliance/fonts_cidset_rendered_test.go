package compliance

import (
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestCIDSetRendered_ProgramHoldsTheCID pins that the PDF/A-1 /CIDSet check
// asks the set only for a rendered CID the font program holds; see
// [checkCIDSetCompletenessTrueTypeRendered].
//
// ISO 19005-1, 6.3.5 has a /CIDSet identify the CIDs present in the font
// program. veraPDF (GFPDCIDFont.getcidSetListsAllGlyphs) reads that at PDF/A-1
// as every rendered CID but 0 the program contains
// (CIDFontType2Program.containsCID): one the /CIDToGIDMap covers whose glyph
// lies below maxp.numGlyphs, whatever its outline. litho exempted a glyph with
// no outline but asked the set for a CID past the program.
//
// Every case was measured with veraPDF 1.30.2 at PDF/A-1b on the PDFs these
// objects build, over a 38-glyph program whose glyphs 0 and 37 alone carry an
// outline, and a /CIDSet identifying CID 1 alone.
func TestCIDSetRendered_ProgramHoldsTheCID(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, gidMap, show string
		want               int
	}{
		{"a CID with an outline the set leaves out", "", "<0025>", 1},
		{"a CID the set identifies", "", "<0001>", 0},
		{"a CID past the program", "", "<0028>", 0},
		{"a CID with an empty outline the set leaves out", "", "<0002>", 1},
		{"a CID past a /CIDToGIDMap stream", "\x00\x00\x00\x25", "<0005>", 0},
		{"a CID the map sends past the program", "\x00\x00\x00\x00\x00\x64", "<0002>", 0},
		{"a CID the map sends to a glyph the set leaves out", "\x00\x00\x00\x00\x00\x25", "<0002>", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			font := subsetType0Font(t, " /CIDSet 9 0 R")
			if tc.gidMap != "" {
				font[1] = strings.Replace(font[1], "/CIDToGIDMap /Identity", "/CIDToGIDMap 11 0 R", 1)
			}
			objs := append(subsetFontObjs(font, tc.show, "3 0 R"), exponentStream("", tc.gidMap))
			doc, _ := objsDocPDF(t, objs...)
			viols, err := ValidatePDFA(context.Background(), doc, models.PDFA1b)
			if err != nil {
				t.Fatalf("ValidatePDFA: %v", err)
			}
			if got := countRuleErrs(t, viols, Rule_19005_1_6_3_5_t03); got != tc.want {
				t.Errorf("6.3.5 test 3 fired %d times, want %d", got, tc.want)
			}
		})
	}
}
