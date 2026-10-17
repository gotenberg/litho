package typography

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// implicitIdentityType0 is [identityType0] without /CIDToGIDMap, which then
// defaults to Identity (ISO 32000-1, Table 117): the shape of the
// non-embedded Arial faces of pdfjs issue19550.pdf and issue20453.pdf.
func implicitIdentityType0(baseFont string) ast.PdfDict {
	font := identityType0(baseFont, "CIDFontType2", "Identity-H", false)
	cid := font["DescendantFonts"].(ast.PdfArray)[0].(ast.PdfDict)
	delete(cid, "CIDToGIDMap")
	return font
}

func decodeGIDs(t *testing.T, font *Font, gids ...uint16) string {
	t.Helper()
	out := ""
	for _, gid := range gids {
		char, n, skip := decodeNextChar(font, []byte{byte(gid >> 8), byte(gid)})
		if n != 2 || skip {
			t.Fatalf("GID %d: consumed %d bytes, skip %v, want 2 and false", gid, n, skip)
		}
		out += char
	}
	return out
}

// TestNonEmbeddedStandardFaceReadsTheArialOrder pins that a non-embedded
// standard face under an Identity CMap reads its codes as glyph IDs of the
// Arial order, through PDF.js's whole standard map, Cyrillic included, and
// that a glyph ID the map does not name reads as the code itself, as in
// PDF.js, not shifted by 29.
func TestNonEmbeddedStandardFaceReadsTheArialOrder(t *testing.T) {
	t.Parallel()

	doc := &document.Document{Logger: document.NopLogger()}
	for _, name := range []string{"Arial", "Arial,Bold", "ArialMT", "TimesNewRomanPSMT"} {
		font, err := ResolveFont(context.Background(), doc, implicitIdentityType0(name))
		if err != nil {
			t.Fatalf("%s: ResolveFont: %v", name, err)
		}
		// "Угловая" in pdfjs issue20453.pdf.
		if got := decodeGIDs(t, font, 589, 605, 613, 616, 604, 602, 633); got != "Угловая" {
			t.Errorf("%s: Cyrillic glyph IDs decode to %q, want %q", name, got, "Угловая")
		}
		if got := decodeGIDs(t, font, 37, 3, 0x4E2D); got != "B 中" {
			t.Errorf("%s: decodes to %q, want %q", name, got, "B 中")
		}
	}
}

// TestNonEmbeddedArialBlackTakesItsSupplement pins PDF.js's Arial Black
// supplement over the standard map.
func TestNonEmbeddedArialBlackTakesItsSupplement(t *testing.T) {
	t.Parallel()

	font, err := ResolveFont(context.Background(), &document.Document{Logger: document.NopLogger()},
		implicitIdentityType0("ArialBlack,Bold"))
	if err != nil {
		t.Fatalf("ResolveFont: %v", err)
	}
	if got := decodeGIDs(t, font, 227, 51); got != "łP" {
		t.Errorf("decodes to %q, want %q", got, "łP")
	}
}

// TestNonStandardFaceKeepsTheOffset pins that a name PDF.js maps to no
// standard font keeps litho's CID + 29 reading.
func TestNonStandardFaceKeepsTheOffset(t *testing.T) {
	t.Parallel()

	font, err := ResolveFont(context.Background(), &document.Document{Logger: document.NopLogger()},
		implicitIdentityType0("Tahoma"))
	if err != nil {
		t.Fatalf("ResolveFont: %v", err)
	}
	if got := decodeGIDs(t, font, 37, 590); got != "B"+string(rune(590+29)) {
		t.Errorf("decodes to %q, want the CID + 29 reading", got)
	}
}

// TestStandardFaceTableWithoutWidths pins that the glyph table is read for a
// CIDFont that states no /W, which ISO 32000-1, Table 117 makes optional: the
// shape of pdfjs issue15594_reduced.pdf.
func TestStandardFaceTableWithoutWidths(t *testing.T) {
	t.Parallel()

	fontDict := implicitIdentityType0("Calibri")
	delete(fontDict["DescendantFonts"].(ast.PdfArray)[0].(ast.PdfDict), "W")
	font, err := ResolveFont(context.Background(), &document.Document{Logger: document.NopLogger()}, fontDict)
	if err != nil {
		t.Fatalf("ResolveFont: %v", err)
	}
	if got := decodeGIDs(t, font, 4, 9); got != "AÄ" {
		t.Errorf("decodes to %q, want %q", got, "AÄ")
	}
}
