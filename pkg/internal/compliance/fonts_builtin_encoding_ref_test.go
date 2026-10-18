package compliance

import (
	"fmt"
	"testing"
)

// TestBuiltinProgramEncoding_PredefinedReference pins that a Type 1 program
// naming a predefined encoding still yields a charCode to glyphName mapping.
//
// ISO 32000-1, Table 114 makes an embedded program's own encoding the implicit
// base a /Differences array describes differences from. A program may build that
// encoding code by code ("dup 65 /A put") or simply name a predefined one
// ("/Encoding StandardEncoding def"). litho read only the first form, so every
// code the /Differences array left alone stayed unnamed and the glyph behind it
// could not be judged.
//
// # Corpus evidence
//
//   - govdocs thread_000_000341.pdf: embeds AvantGarde-Book with a program
//     that names StandardEncoding, naming 110 codes in /Differences and leaving
//     the other 146, the space and the digits among them, to StandardEncoding.
func TestBuiltinProgramEncoding_PredefinedReference(t *testing.T) {
	t.Parallel()

	// program is a Type 1 font program's cleartext header. The encoding
	// reference sits before the eexec-encrypted section, so no ciphertext is
	// needed to read it.
	program := func(encoding string) string {
		return readableType1("%!PS-AdobeFont-1.0: Probe 001.001\n" +
			"/FontName /Probe def\n" +
			"/Encoding " + encoding + " def\n" +
			"currentdict end\ncurrentfile eexec\n")
	}

	// pdf shows code 97, which /Differences does not name, through an embedded
	// non-symbolic Type 1 font carrying the given program.
	pdf := func(prog string, shownHex string) []byte {
		stream := fmt.Sprintf("BT /F1 12 Tf 10 50 Td <%s> Tj ET", shownHex)
		return cyclePDF(
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] "+
				"/Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
			"<< /Type /Font /Subtype /Type1 /BaseFont /ABCDEF+Probe "+
				"/FirstChar 65 /LastChar 97 /FontDescriptor 6 0 R "+
				"/Encoding << /Differences [65 /A] >> >>",
			"<< /Type /FontDescriptor /FontName /ABCDEF+Probe /Flags 32 "+
				"/ItalicAngle 0 /Ascent 700 /Descent -200 /CapHeight 700 /StemV 80 "+
				"/FontBBox [0 0 1000 1000] /FontFile 7 0 R >>",
			fmt.Sprintf("<< /Length %d /Length1 %d /Length2 0 /Length3 0 >>\nstream\n%s\nendstream",
				len(prog), len(prog), prog),
		)
	}

	// noEncoding is a program that names no encoding at all.
	noEncoding := readableType1("%!PS-AdobeFont-1.0: Probe 001.001\n/FontName /Probe def\ncurrentdict end\ncurrentfile eexec\n")

	// 61 is 'a', which StandardEncoding names; 18 is a control code no
	// predefined encoding of Annex D names.
	const namedCode, unnamedCode = "61", "18"

	t.Run("a named predefined encoding reaches the shown code", func(t *testing.T) {
		t.Parallel()
		if firesUnicodeRule(t, pdf(program("StandardEncoding"), namedCode)) {
			t.Error("StandardEncoding names code 97 'a', which the AGL maps")
		}
	})

	// ISO 32000-1, 9.6.6.4 and Table 114 give a non-symbolic font whose program
	// offers no built-in encoding StandardEncoding as its implicit base, so the
	// code is reachable even though the program itself named nothing.
	t.Run("a program naming no encoding falls back to StandardEncoding", func(t *testing.T) {
		t.Parallel()
		if firesUnicodeRule(t, pdf(noEncoding, namedCode)) {
			t.Error("Table 114 makes StandardEncoding the implicit base, which names code 97")
		}
	})

	// The fallback reaches only what StandardEncoding names. A control code is
	// outside every Annex D encoding, so it stays without a glyph and so
	// without a Unicode value.
	t.Run("a code no encoding names stays unreachable", func(t *testing.T) {
		t.Parallel()
		if !firesUnicodeRule(t, pdf(noEncoding, unnamedCode)) {
			t.Error("StandardEncoding names nothing at code 24, so the glyph has no Unicode value")
		}
	})
}

// TestBuiltinProgramEncoding_PFBContainerNamesNothing pins that a /FontFile
// holding a PFB container supplies no encoding. ISO 32000-1, 9.9.1 defines the
// embedded Type 1 form without the PFB segment headers, and litho already reports
// such a font as unembedded; reading its encoding anyway gave its glyphs Unicode
// values.
//
// # Corpus evidence
//
//   - pdfjs bug860632.pdf: embeds Helvetica in a PFB container with no
//     /Encoding, and veraPDF reports every glyph it shows under ISO 14289-1,
//     7.21.7 t1.
func TestBuiltinProgramEncoding_PFBContainerNamesNothing(t *testing.T) {
	t.Parallel()

	cleartext := readableType1("%!PS-AdobeFont-1.0: Probe 001.001\n/FontName /Probe def\n" +
		"/Encoding StandardEncoding def\ncurrentdict end\ncurrentfile eexec\n")
	// pfbSegment wraps the cleartext in the PFB ASCII segment header: 0x80, type
	// 1, and a four-byte little-endian length.
	pfbSegment := func(s string) string {
		n := len(s)
		return string([]byte{0x80, 0x01, byte(n), byte(n >> 8), byte(n >> 16), byte(n >> 24)}) + s
	}

	// pdf shows code 0x54 'T' through a symbolic Type 1 font with no /Encoding,
	// so the program's own encoding is the only source of a glyph name.
	pdf := func(prog string) []byte {
		stream := "BT /F1 12 Tf 10 50 Td <54> Tj ET"
		return cyclePDF(
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] "+
				"/Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
			"<< /Type /Font /Subtype /Type1 /BaseFont /ABCDEF+Probe "+
				"/FirstChar 84 /LastChar 84 /Widths [600] /FontDescriptor 6 0 R >>",
			"<< /Type /FontDescriptor /FontName /ABCDEF+Probe /Flags 4 "+
				"/ItalicAngle 0 /Ascent 700 /Descent -200 /CapHeight 700 /StemV 80 "+
				"/FontBBox [0 0 1000 1000] /FontFile 7 0 R >>",
			fmt.Sprintf("<< /Length %d /Length1 %d /Length2 0 /Length3 0 >>\nstream\n%s\nendstream",
				len(prog), len(prog), prog),
		)
	}

	if firesUnicodeRule(t, pdf(cleartext)) {
		t.Error("a Type 1 program naming StandardEncoding names code 84 'T'")
	}
	if !firesUnicodeRule(t, pdf(pfbSegment(cleartext))) {
		t.Error("a PFB container embeds no program, so code 84 has no glyph name and no Unicode value")
	}
}
