package remediation

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// encodedType1PDF builds a Type 1 font that states an /Encoding and carries no
// /ToUnicode, no /Widths and no /FontDescriptor: none of the width repair's
// preconditions hold, and none of them are needed to know what its codes mean.
func encodedType1PDF(encoding string) []byte {
	return assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792]" +
			" /Resources << /Font << /F1 4 0 R >> >> >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica " + encoding + " >>",
	})
}

// codes turns bytes into the shown-code set the repair is held to.
func codes(b ...byte) map[byte]bool {
	out := make(map[byte]bool, len(b))
	for _, c := range b {
		out[c] = true
	}
	return out
}

// generatedToUnicode runs the repair for the given shown codes and returns the
// CMap it attached, or "" when it attached none.
func generatedToUnicode(t *testing.T, data []byte, shown map[byte]bool) (string, []string) {
	t.Helper()

	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber())
	fontDict, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 4})
	if err != nil {
		t.Fatalf("resolving the font: %v", err)
	}
	fixes := fixSimpleFontToUnicode(ctx, doc, alloc, fontDict, ast.PdfIndirectRef{ObjectNum: 4}, shown)

	font, _ := alloc.Pending()[4].(ast.PdfDict)
	if font == nil {
		return "", fixes
	}
	ref, ok := font["ToUnicode"].(ast.PdfIndirectRef)
	if !ok {
		return "", fixes
	}
	stream, ok := alloc.Pending()[ref.ObjectNum].(ast.PdfStream)
	if !ok {
		t.Fatalf("/ToUnicode points at %T, want a stream", alloc.Pending()[ref.ObjectNum])
	}
	raw, err := stream.Bytes(ctx)
	if err != nil {
		t.Fatalf("reading the CMap: %v", err)
	}
	return string(raw), fixes
}

// TestFixSimpleFontToUnicodeNeedsNoFontProgram pins that a Unicode mapping is
// generated from the font's /Encoding alone.
//
// The generator used to sit inside the width repair, behind its early returns
// for a missing /FontDescriptor, an unreadable program and an absent /Widths.
// A Type 1 program has no cmap table, so the char-code-to-GID route is nil for
// every one of them, and such a file came out of remediation with "Fixed: 0"
// against a rule that only wants the mapping (ISO 19005-2, 6.2.11.7.2).
//
// # Corpus evidence
//
//   - verapdf PDF_A-2u__6-2-11-7-2-t01-fail-a: came out of remediation with
//     "Fixed: 0" against ISO 19005-2, 6.2.11.7.2.
func TestFixSimpleFontToUnicodeNeedsNoFontProgram(t *testing.T) {
	t.Parallel()

	cmap, fixes := generatedToUnicode(t, encodedType1PDF("/Encoding /WinAnsiEncoding"), codes('A', 'B'))
	if cmap == "" {
		t.Fatalf("no /ToUnicode was generated, fixes=%v", fixes)
	}
	for _, want := range []string{"begincmap", "beginbfchar", "endcmap", "<0041>"} {
		if !strings.Contains(cmap, want) {
			t.Errorf("the CMap is missing %q:\n%s", want, cmap)
		}
	}
}

// TestFixSimpleFontToUnicodeRefusesPartialCoverage pins the gate that keeps the
// repair honest.
//
// The rule asks for a mapping of the codes a page USES. A CMap built from the
// /Encoding need not reach every one of them, and attaching a partial one
// satisfies litho's own presence check while veraPDF still reports the rule:
// remediation then claims a repair the output does not contain.
//
// # Corpus evidence
//
//   - All corpora: twenty-four corpus cells went that way before the gate
//     existed.
func TestFixSimpleFontToUnicodeRefusesPartialCoverage(t *testing.T) {
	t.Parallel()

	// 0x81 is one of the codes WinAnsiEncoding leaves undefined, so no glyph
	// name and no Unicode value can be derived for it.
	cmap, _ := generatedToUnicode(t, encodedType1PDF("/Encoding /WinAnsiEncoding"), codes('A', 0x81))
	if cmap != "" {
		t.Errorf("a CMap was attached that does not map every shown code:\n%s", cmap)
	}
}

// TestFixSimpleFontToUnicodeUsesTheDeclaredEncoding pins that the mapping
// follows the /Encoding the font declares, not a fixed assumption.
//
// Under /MacRomanEncoding code 0xD5 is quoteright (U+2019); under /WinAnsi it
// is a different character entirely.
func TestFixSimpleFontToUnicodeUsesTheDeclaredEncoding(t *testing.T) {
	t.Parallel()

	cmap, _ := generatedToUnicode(t, encodedType1PDF("/Encoding /MacRomanEncoding"), codes(0xD5))
	if cmap == "" {
		t.Fatal("no /ToUnicode was generated for a MacRoman font")
	}
	if !strings.Contains(cmap, "<2019>") {
		t.Errorf("MacRoman 0xD5 was not mapped to U+2019 (quoteright):\n%s", cmap)
	}
}

// TestFixSimpleFontToUnicodeIgnoresAnUnusedFont is the control for a font the
// page never draws with: the rule is about used codes, so there is nothing to
// map.
func TestFixSimpleFontToUnicodeIgnoresAnUnusedFont(t *testing.T) {
	t.Parallel()

	if cmap, _ := generatedToUnicode(t, encodedType1PDF("/Encoding /WinAnsiEncoding"), nil); cmap != "" {
		t.Errorf("a font the page never shows was given a /ToUnicode:\n%s", cmap)
	}
}

// TestFixSimpleFontToUnicodeLeavesAMappedFont is the control: a font that
// already carries a /ToUnicode must not be given another one.
func TestFixSimpleFontToUnicodeLeavesAMappedFont(t *testing.T) {
	t.Parallel()

	data := assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792]" +
			" /Resources << /Font << /F1 4 0 R >> >> >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding /ToUnicode 5 0 R >>",
		"<< /Length 9 >>\nstream\n/CIDInit\nendstream",
	})

	if cmap, fixes := generatedToUnicode(t, data, codes('A')); cmap != "" {
		t.Errorf("a font that already has a /ToUnicode was given another, fixes=%v", fixes)
	}
}

// TestFixSimpleFontToUnicodeSkipsASymbolicFontWithNoEncoding pins that a
// symbolic font stating no /Encoding is left alone.
//
// ISO 32000-1, 9.6.6.1 says such a font is drawn through its built-in encoding
// and a reader shall not substitute a standard one, so there is no trustworthy
// source of Unicode. Deriving it from a standard encoding invented meanings
// that satisfied the compliance rule while corrupting extracted text.
//
// # Corpus evidence
//
//   - verapdf 6-3-8-t01-fail-a: its TeX math fonts are symbolic with no
//     /Encoding, and the generated mapping turned "−∞ ∞" into "−1 1" in
//     extracted text while satisfying the compliance rule.
func TestFixSimpleFontToUnicodeSkipsASymbolicFontWithNoEncoding(t *testing.T) {
	t.Parallel()

	data := assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792]" +
			" /Resources << /Font << /F1 4 0 R >> >> >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /CMMI12 /FontDescriptor 5 0 R >>",
		"<< /Type /FontDescriptor /FontName /CMMI12 /Flags 4 >>",
	})

	if cmap, _ := generatedToUnicode(t, data, codes('A')); cmap != "" {
		t.Errorf("a symbolic font with no /Encoding was given an invented mapping:\n%s", cmap)
	}
}

// TestFixSimpleFontToUnicodeTrustsAStatedEncoding is the control: an explicit
// /Encoding is the producer's own statement of what the codes mean, and is
// trusted even on a symbolic font.
func TestFixSimpleFontToUnicodeTrustsAStatedEncoding(t *testing.T) {
	t.Parallel()

	data := assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792]" +
			" /Resources << /Font << /F1 4 0 R >> >> >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /CMMI12 /Encoding /WinAnsiEncoding /FontDescriptor 5 0 R >>",
		"<< /Type /FontDescriptor /FontName /CMMI12 /Flags 4 >>",
	})

	if cmap, fixes := generatedToUnicode(t, data, codes('A')); cmap == "" {
		t.Errorf("a symbolic font that states its /Encoding was skipped, fixes=%v", fixes)
	}
}
