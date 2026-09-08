package compliance

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestCheckGlyphWidths_UncoveredCode pins that a shown code the /Widths array
// does not cover is width-checked against /MissingWidth.
//
// Per ISO 32000-1, Table 111, /MissingWidth is "the width to use for character
// codes whose widths are not specified in a font dictionary's Widths array",
// defaulting to 0, so such a code has a declared width like any other. Walking
// only the array left every code below /FirstChar or past its end unexamined:
// govdocs thread_100_100945.pdf shows a space at code 32 against a /FirstChar
// of 48 and a 222-unit program glyph, which veraPDF reports under ISO 19005-2
// §6.2.11.5.
//
// The fixture is Lato-Bold with FontMatrix 0.0005 (2000 units/em): its "space"
// glyph is 486 program units, so 486×1000/2000 = 243 in text space.
func TestCheckGlyphWidths_UncoveredCode(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cff, err := os.ReadFile("testdata/lato_bold_subset.cff")
	if err != nil {
		t.Fatalf("read CFF fixture: %v", err)
	}

	// fires reports whether the width rule fails when code is shown by a font
	// whose /Widths starts at 48 and whose descriptor carries missingWidth.
	fires := func(t *testing.T, missingWidth string, code byte) bool {
		t.Helper()
		doc, _ := objsDocPDF(t,
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
			"<< /Type /Font /Subtype /Type1 /BaseFont /TEEUUH+Lato-Bold /FirstChar 48 /LastChar 48"+
				" /Widths [243] /Encoding /WinAnsiEncoding /FontDescriptor 5 0 R >>",
			"<< /Type /FontDescriptor /FontName /TEEUUH+Lato-Bold /Flags 34 "+missingWidth+" /FontFile3 6 0 R >>",
			fmt.Sprintf("<< /Length %d /Subtype /Type1C >>\nstream\n%s\nendstream", len(cff), string(cff)),
		)
		fontDict, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 4})
		if err != nil {
			t.Fatalf("resolve font dict: %v", err)
		}
		usage := &fontUsageInfo{visible: true, charCodes: map[byte]bool{code: true}}
		return hasRule(
			checkGlyphWidths(ctx, doc, fontDict, "T1_0", 1, "PDF/A-2b", usage),
			Rule_19005_2_6_2_11_5_t01,
		)
	}

	cases := []struct {
		name         string
		missingWidth string
		code         byte
		want         bool
	}{
		{"an absent /MissingWidth declares 0 and mismatches", "", 32, true},
		{"a /MissingWidth matching the program does not fire", "/MissingWidth 243", 32, false},
		{"a /MissingWidth disagreeing with the program fires", "/MissingWidth 500", 32, true},
		// Code 33 is "exclam", which the subset does not carry, so it falls to
		// GID 0; the .notdef advance is not the width of a glyph the font
		// selects, and glyph presence is the rule for the absence itself.
		{"a code resolving to .notdef stays unchecked", "", 33, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := fires(t, tc.missingWidth, tc.code); got != tc.want {
				t.Errorf("width violation = %v, want %v", got, tc.want)
			}
		})
	}
}
