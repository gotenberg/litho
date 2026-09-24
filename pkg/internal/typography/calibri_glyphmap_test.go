package typography

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// identityType0 builds a Type0 font over a CIDFont that states /CIDToGIDMap
// /Identity, the shape of the non-embedded Calibri faces of pdfjs
// issue9195.pdf. The /W array matters: the glyph table is consulted only for
// a font with CID widths.
func identityType0(baseFont, cidSubtype, encoding string, embedded bool) ast.PdfDict {
	fd := ast.PdfDict{
		"Type":     ast.PdfName("FontDescriptor"),
		"FontName": ast.PdfName(baseFont),
		"Flags":    ast.PdfInt(32),
	}
	if embedded {
		fd["FontFile2"] = ast.PdfIndirectRef{ObjectNum: 99}
	}
	cid := ast.PdfDict{
		"Type":        ast.PdfName("Font"),
		"Subtype":     ast.PdfName(cidSubtype),
		"BaseFont":    ast.PdfName(baseFont),
		"CIDToGIDMap": ast.PdfName("Identity"),
		"CIDSystemInfo": ast.PdfDict{
			"Registry":   ast.PdfString("Adobe"),
			"Ordering":   ast.PdfString("Identity"),
			"Supplement": ast.PdfInt(0),
		},
		"FontDescriptor": fd,
		"W":              ast.PdfArray{ast.PdfInt(3), ast.PdfArray{ast.PdfInt(226), ast.PdfInt(579)}},
	}
	return ast.PdfDict{
		"Type":            ast.PdfName("Font"),
		"Subtype":         ast.PdfName("Type0"),
		"BaseFont":        ast.PdfName(baseFont),
		"Encoding":        ast.PdfName(encoding),
		"DescendantFonts": ast.PdfArray{cid},
	}
}

// TestNonEmbeddedCalibriIdentityReadsGlyphIDs pins that a non-embedded
// Calibri under an explicit /CIDToGIDMap /Identity reads its codes as Calibri
// glyph IDs through PDF.js's table, not as code points.
func TestNonEmbeddedCalibriIdentityReadsGlyphIDs(t *testing.T) {
	t.Parallel()

	font, err := ResolveFont(context.Background(), &document.Document{Logger: document.NopLogger()},
		identityType0("Calibri", "CIDFontType2", "Identity-H", false))
	if err != nil {
		t.Fatalf("ResolveFont: %v", err)
	}
	for _, tc := range []struct {
		gid  uint16
		want string
	}{
		{3, " "},    // the space, from PDF.js's standard map
		{4, "A"},    // Calibri's own order, from the Calibri supplement
		{383, "ó"},  // "Kraków" in issue9195.pdf
		{1004, "0"}, // digits sit far above GID 128 in Calibri
		// A glyph ID neither PDF.js table lists stays on the code point
		// fallback.
		{0x4E2D, "中"},
	} {
		got, n, skip := decodeNextChar(font, []byte{byte(tc.gid >> 8), byte(tc.gid)})
		if got != tc.want || n != 2 || skip {
			t.Errorf("GID %d decodes to (%q, %d, %v), want (%q, 2, false)", tc.gid, got, n, skip, tc.want)
		}
	}
}

// TestCalibriGlyphMapMatchesTheFaceName pins the name gate: the four Calibri
// faces, with a subset prefix or a comma style suffix, take the table, and
// any other name, Calibri look-alikes included, keeps the code point
// fallback.
func TestCalibriGlyphMapMatchesTheFaceName(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc := &document.Document{Logger: document.NopLogger()}
	for _, tc := range []struct {
		name string
		want bool
	}{
		{"Calibri", true},
		{"Calibri-Bold", true},
		{"Calibri,Bold", true},
		{"Calibri,Italic", true},
		{"Calibri-BoldItalic", true},
		{"ABCDEF+Calibri", true},
		{"ABCDEF+Calibri,BoldItalic", true},
		{"Arial", false},
		{"Helvetica", false},
		{"Calibri-Light", false},
		{"CalibriLight", false},
		{"MyCalibri", false},
		{"Carlito", false},
	} {
		font := &Font{}
		applyCIDToGIDIdentityFallback(ctx, doc, identityType0(tc.name, "CIDFontType2", "Identity-H", false), font)
		if !font.CIDIdentityFallback {
			t.Fatalf("%q: CIDIdentityFallback = false, want true", tc.name)
		}
		if got := font.GIDToUnicode[4] == 'A'; got != tc.want {
			t.Errorf("%q: Calibri table applied = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestCalibriGlyphMapFollowsThePDFJSGate pins the other conditions PDF.js
// sets before it reaches for the table: no embedded program, whose own cmap
// is authoritative, a CIDFontType2, and an Identity CMap.
func TestCalibriGlyphMapFollowsThePDFJSGate(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc := &document.Document{Logger: document.NopLogger()}
	for _, tc := range []struct {
		desc     string
		fontDict ast.PdfDict
		want     bool
	}{
		{"Identity-V", identityType0("Calibri", "CIDFontType2", "Identity-V", false), true},
		{"embedded program", identityType0("Calibri", "CIDFontType2", "Identity-H", true), false},
		{"CIDFontType0", identityType0("Calibri", "CIDFontType0", "Identity-H", false), false},
		{"non-Identity CMap", identityType0("Calibri", "CIDFontType2", "UniGB-UCS2-H", false), false},
	} {
		font := &Font{}
		applyCIDToGIDIdentityFallback(ctx, doc, tc.fontDict, font)
		if got := font.GIDToUnicode != nil; got != tc.want {
			t.Errorf("%s: Calibri table applied = %v, want %v", tc.desc, got, tc.want)
		}
	}
}
