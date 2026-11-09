package build

import (
	"fmt"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("trailer_2b", trailer2b) }

// trailer2b builds trailer_2b.pdf: trailer and file-structure defects.
//
// The coverage scan against the veraPDF profile showed the whole ISO 19005-2,
// 6.1 family unvisited by this corpus. Most of it needs byte-level malformation
// that pikepdf will not emit, but the trailer rules of 6.1.3 are reachable: two
// by writing the file normally and then editing the bytes, one by encrypting.
//
// All three defects are independent and live in different parts of the file, so
// a check that stops after the first trailer problem reports one rule where
// three are due. Encryption is the interesting one: it changes how every string
// and stream in the file is read, so a reader that gives up on it reports
// nothing at all rather than reporting the encryption itself.
//
// qpdf draws a random AES initialization vector for every encrypted string
// and stream, so two pikepdf runs differ inside every ciphertext. This
// builder derives the vectors instead (see pdfgen.Encryption), so it
// rebuilds byte for byte like every other fixture. The file key, /O, /U and
// /P are the ones qpdf computes from the same passwords and /ID, so the file
// decrypts to the same objects a pikepdf run writes.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func trailer2b() ([]byte, error) {
	pdf := pdfgen.New()

	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("Type1"),
		"BaseFont", n("/Helvetica"),
		"Encoding", n("WinAnsiEncoding"),
	))

	page := d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d("Font", d("F1", font)),
		"Contents", pdf.Stream([]byte("BT /F1 12 Tf 50 700 Td (encrypted body) Tj ET\n")),
	)
	pdf.AddPage(pdf.Indirect(page))

	// Deliberately no /Metadata and no /OutputIntents, so the file still carries
	// the document-level defects the rest of the corpus relies on.
	plain, err := save(pdf, "1.7")
	if err != nil {
		return nil, err
	}

	// Re-save encrypted. ISO 19005-2, 6.1.3 t2 forbids /Encrypt outright, and the
	// owner password keeps the body readable to a tool that implements RC4/AES so
	// the remaining rules stay reachable rather than being masked by a file that
	// cannot be parsed at all. The re-save reads the plain file back, so it
	// keeps that file's /ID[0] and its version, and compresses the content
	// stream as pikepdf's default save does.
	id := idOf(plain)
	if id == nil {
		return nil, fmt.Errorf("trailer_2b: the plain save carries no /ID")
	}
	pdf.Trailer.Set("ID", a(id, id))
	out, err := pdf.Save(pdfgen.SaveOptions{
		MinVersion:      "1.7",
		CompressStreams: true,
		StaticID:        true,
		Encryption:      &pdfgen.Encryption{Owner: "owner", User: "", P: pdfgen.PikepdfDefaultP},
	})
	if err != nil {
		return nil, fmt.Errorf("saving encrypted: %w", err)
	}

	// Append junk after the final %%EOF. ISO 19005-2, 6.1.3 t3 allows only a
	// single optional end-of-line marker there.
	return append(out.Bytes, "trailing junk after the last EOF marker\n"...), nil
}
