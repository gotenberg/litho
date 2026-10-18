package compliance

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestFontEmbedding_FontFile3Container pins that a /FontFile3 embeds a font only
// when its bytes hold a font container at all. Every format it may name is a
// bare CFF ("Type1C", "CIDFontType0C") or an sfnt ("OpenType") per ISO 32000-1,
// Table 126, so bytes that are neither embed nothing.
//
// One leniency is pinned deliberately: the CFF major version is unread, so a
// CFF2 program still counts.
//
// The container the /Subtype names is enforced, but only for the fonts that
// read it. veraPDF picks the parser from the font, so a simple TrueType font
// parses /FontFile3 as an sfnt whatever the subtype says, while the /Type1 font
// this fixture builds dispatches on the subtype and cannot read the other
// container: see [fontFile3ParsesForFont] and
// [TestFontFile3ContainerMatchesFont], where every pairing was measured.
//
// # Corpus evidence
//
//   - pdfjs issue4668.pdf: declares /Type1C over a TrueType sfnt and is
//     embedded.
func TestFontEmbedding_FontFile3Container(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// fires reports whether the embedding rule fails for a simple font whose
	// descriptor carries a /FontFile3 of the given subtype and program bytes.
	fires := func(t *testing.T, subtype, program string) bool {
		t.Helper()
		doc, _ := objsDocPDF(t,
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
			"<< /Type /Font /Subtype /Type1 /BaseFont /Test /FirstChar 65 /LastChar 65"+
				" /Widths [722] /FontDescriptor 5 0 R >>",
			"<< /Type /FontDescriptor /FontName /Test /Flags 32 /FontFile3 6 0 R >>",
			fmt.Sprintf("<< /Subtype /%s /Length %d >>\nstream\n%s\nendstream",
				subtype, len(program), program),
		)
		font, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 4})
		if err != nil {
			t.Fatalf("resolve font: %v", err)
		}
		got := checkFontEmbedding(ctx, doc, font, "F1", 1, "PDF/A-2b", false)
		return hasRule(got, Rule_19005_2_6_2_11_4_1_t01)
	}

	var (
		cff  = readableCFF()
		cff2 = "\x02" + cff[1:]
		sfnt = readableTrueType()
	)

	cases := []struct {
		name    string
		subtype string
		program string
		want    bool
	}{
		{"a CFF program embeds a Type1C font", "Type1C", cff, false},
		{"a CFF2 major version still embeds it", "Type1C", cff2, false},
		{"a CFF program embeds a CIDFontType0C font", "CIDFontType0C", cff, false},
		{"zero bytes embed nothing", "Type1C", "\x00\x00\x00\x00\x00\x00\x00\x00", true},
		{"a header shorter than the fixed record does not", "Type1C", "\x01\x00\x04", true},
		{"a header reaching past the program does not", "Type1C", "\x01\x00\x64\x02rest", true},
		{"an offset size out of range does not", "Type1C", "\x01\x00\x04\x05rest", true},
		{"a Type1 font cannot read an sfnt in a Type1C slot", "Type1C", sfnt, true},
		{"an sfnt program embeds an OpenType font", "OpenType", sfnt, false},
		{"a Type1 font cannot read a bare CFF in an OpenType slot", "OpenType", cff, true},
		{"zero bytes in an OpenType slot do not", "OpenType", "\x00\x00\x00\x00", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := fires(t, tc.subtype, tc.program); got != tc.want {
				t.Errorf("/FontFile3 /%s embedding violation = %v, want %v",
					tc.subtype, got, tc.want)
			}
		})
	}
}
