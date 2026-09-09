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
// could not be judged. govdocs thread_000_000341.pdf embeds AvantGarde-Book
// that way, naming 110 codes in /Differences and leaving the other 146, the
// space and the digits among them, to StandardEncoding.
func TestBuiltinProgramEncoding_PredefinedReference(t *testing.T) {
	t.Parallel()

	// program is a Type 1 font program's cleartext header. The encoding
	// reference sits before the eexec-encrypted section, so no ciphertext is
	// needed to read it.
	program := func(encoding string) string {
		return "%!PS-AdobeFont-1.0: Probe 001.001\n" +
			"/FontName /Probe def\n" +
			"/Encoding " + encoding + " def\n" +
			"currentdict end\ncurrentfile eexec\n"
	}

	// pdf shows code 97, which /Differences does not name, through an embedded
	// non-symbolic Type 1 font carrying the given program.
	pdf := func(prog string) []byte {
		const stream = "BT /F1 12 Tf 10 50 Td (a) Tj ET"
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

	t.Run("a named predefined encoding reaches the shown code", func(t *testing.T) {
		t.Parallel()
		if firesUnicodeRule(t, pdf(program("StandardEncoding"))) {
			t.Error("StandardEncoding names code 97 'a', which the AGL maps")
		}
	})

	t.Run("a program naming no encoding leaves the code unreachable", func(t *testing.T) {
		t.Parallel()
		if !firesUnicodeRule(t, pdf("%!PS-AdobeFont-1.0: Probe 001.001\n/FontName /Probe def\ncurrentdict end\ncurrentfile eexec\n")) {
			t.Error("nothing names code 97, so its glyph has no Unicode value")
		}
	})
}
