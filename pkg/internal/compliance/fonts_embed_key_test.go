package compliance

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestFontFileKeys_CIDFontSubtypeMatrix pins the valid embedding keys per
// ISO 32000-1 Table 126: a CIDFontType0 (CFF) embeds only via /FontFile3, a
// CIDFontType2 (TrueType) via /FontFile2 or an OpenType /FontFile3, and a
// simple font may still use /FontFile. /FontFile never embeds a CIDFont.
func TestFontFileKeys_CIDFontSubtypeMatrix(t *testing.T) {
	t.Parallel()
	cases := []struct {
		subtype string
		want    []ast.PdfName
	}{
		{"CIDFontType0", []ast.PdfName{"FontFile3"}},
		{"CIDFontType2", []ast.PdfName{"FontFile2", "FontFile3"}},
		{"Type1", []ast.PdfName{"FontFile", "FontFile2", "FontFile3"}},
		{"TrueType", []ast.PdfName{"FontFile", "FontFile2", "FontFile3"}},
	}
	for _, tc := range cases {
		got := fontFileKeys(tc.subtype)
		if len(got) != len(tc.want) {
			t.Errorf("fontFileKeys(%q) = %v, want %v", tc.subtype, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("fontFileKeys(%q) = %v, want %v", tc.subtype, got, tc.want)
				break
			}
		}
	}
}

// TestIsFontEmbedded_CIDFontType0RejectsFontFile pins that a CIDFontType0 whose
// program sits in /FontFile (a Type1 program) is not embedded: /FontFile is not
// a valid key for a CIDFont, so veraPDF flags §6.2.10.4.1 t1 where litho used to
// pass. /FontFile3 embeds it correctly.
//
// # Corpus evidence
//
//   - pdfjs issue11740_reduced.pdf: at PDF/A-4, veraPDF flags §6.2.10.4.1 t1
//     for its CIDFontType0 program in /FontFile, where litho used to pass.
func TestIsFontEmbedded_CIDFontType0RejectsFontFile(t *testing.T) {
	t.Parallel()
	ctx := testCtx()
	prog := ast.NewStream(ast.PdfDict{}, []byte(readableType1("%!PS-AdobeFont-1.0\n")))
	// A /FontFile3 states its glyph format; the CFF one embeds a CIDFont.
	cff := ast.NewStream(ast.PdfDict{"Subtype": ast.PdfName("CIDFontType0C")}, []byte(readableCFF()))

	wrongKey := ast.PdfDict{"FontFile": prog}
	if isFontEmbedded(ctx, nopDoc(), wrongKey, "CIDFontType0") {
		t.Error("CIDFontType0 via /FontFile must not count as embedded")
	}

	rightKey := ast.PdfDict{"FontFile3": cff}
	if !isFontEmbedded(ctx, nopDoc(), rightKey, "CIDFontType0") {
		t.Error("CIDFontType0 via /FontFile3 must count as embedded")
	}

	// A simple Type1 font keeps /FontFile as valid embedding.
	if !isFontEmbedded(ctx, nopDoc(), wrongKey, "Type1") {
		t.Error("Type1 via /FontFile must count as embedded")
	}

	// A /FontFile3 naming no glyph format embeds nothing readable.
	noSubtype := ast.PdfDict{"FontFile3": prog}
	if isFontEmbedded(ctx, nopDoc(), noSubtype, "Type1") {
		t.Error("/FontFile3 with no /Subtype must not count as embedded")
	}
}
