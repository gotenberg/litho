package build

import (
	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("redactcopies_2b", redactcopies2b) }

// redactcopies2b builds redactcopies_2b.pdf: copies of page text no decoded value spells.
//
// Redaction verifies its output by looking for every removed string in what a
// rewrite writes. Two copies here sit where no decoded value spells them, and a
// rewrite copies both:
//
//   - The catalog's /PieceInfo holds a FlateDecode stream whose stored data is
//     a zlib stream of other words followed by the paragraph. Decoding stops at
//     the zlib checksum (RFC 1950, 2.2), so the decoded data never shows the
//     words, but the stored bytes carry them.
//   - The catalog's /Dests names a destination after the paragraph. Its keys are
//     names (ISO 32000-1, 12.3.2.3), which a comparison of values never reads.
//
// Before, redacting page 1 wrote both copies out and reported success. Now the
// redaction refuses and names both survivors.
//
// Page 2 draws text no redaction of page 1 touches.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func redactcopies2b() ([]byte, error) {
	pdf := pdfgen.New()

	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("Type1"),
		"BaseFont", n("/Helvetica"),
		"Encoding", n("WinAnsiEncoding"),
	))
	resources := d("Font", d("F1", font))

	const words = "the secret plan"

	page1 := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", resources,
		"Contents", pdf.Stream([]byte("BT /F1 12 Tf 72 700 Td ("+words+") Tj ET\n")),
	))
	page2 := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", resources,
		"Contents", pdf.Stream([]byte("BT /F1 12 Tf 72 700 Td (page two) Tj ET\n")),
	))
	pdf.AddPage(page1)
	pdf.AddPage(page2)

	// The stored data is written as it stands: save keeps every declared
	// filter and its bytes, so the words after the checksum stay in the file.
	private := pdf.Stream(append(zlibCompress([]byte("unrelated notes")), words...))
	private.Set("Filter", n("FlateDecode"))
	pdf.Root.Set("PieceInfo", d(
		"LithoFixture", d("LastModified", text("D:20260101000000Z"), "Private", private),
	))

	pdf.Root.Set("Dests", pdf.Indirect(d(words, a(page1, n("Fit")))))

	return save(pdf, "1.7")
}
