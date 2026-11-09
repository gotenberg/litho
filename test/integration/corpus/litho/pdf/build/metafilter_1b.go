package build

import (
	"fmt"
	"regexp"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("metafilter_1b", metafilter1b) }

// metafilter1b builds metafilter_1b.pdf: a filtered metadata stream, and one that may be.
//
// | Construct                                          | Rule            |
// | -------------------------------------------------- | --------------- |
// | the catalog's XMP packet carries `/Filter`         | `6.7.2/2`       |
//
// ISO 19005-1, 6.7.2 test 2 reads
//
//	isCatalogMetadata == false || Filter == null
//
// so the prohibition is on the CATALOG's packet alone. A metadata stream hanging
// off anything else may be filtered. The file carries both, encoded identically:
// the catalog's packet offends, and a page's packet with the same `/Filter
// /FlateDecode` and the same compressed bytes is the control. A checker matching
// on the filter alone rather than on whose packet it is condemns both, and a
// checker that skipped metadata streams entirely condemns neither, so the pair
// separates the two failure modes a single stream cannot.
//
// Both packets are well-formed XML and the catalog's carries a correct PDF/A-1b
// claim, so the filter is the only thing either can answer for.
//
// qpdf strips `/Filter` off a metadata stream on every save path tried, including
// with `preserve_pdfa` and `fix_metadata_version` off, so the construct cannot
// survive pikepdf. Each stream is therefore planted after the save, its dictionary
// padded with spaces so the object keeps its byte length and no cross-reference
// offset moves. Padding after `endobj` instead would cost a `6.1.8/1`.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func metafilter1b() ([]byte, error) {
	pdf := pdfgen.New()

	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("Type1"),
		"BaseFont", n("/Helvetica"),
		"Encoding", n("WinAnsiEncoding"),
	))

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d("Font", d("F1", font)),
		"Contents", pdf.Stream([]byte("BT /F1 12 Tf 50 700 Td (metadata stream filters) Tj ET\n")),
	))
	pdf.AddPage(page)

	catalogPacket := xmpPacket(xmpOpts{Part: 1, Conformance: "B"})

	// The control's packet carries no PDF/A claim of its own: a second claim would
	// be a defect in itself and would stop the filter answering alone.
	pagePacket := xmpPacket(xmpOpts{})

	catalogMeta := pdf.Stream(catalogPacket, "Type", n("Metadata"), "Subtype", n("XML"))
	pdf.MakeIndirect(catalogMeta)
	pdf.Root.Set("Metadata", catalogMeta)

	pageMeta := pdf.Stream(pagePacket, "Type", n("Metadata"), "Subtype", n("XML"))
	pdf.MakeIndirect(pageMeta)
	page.Set("Metadata", pageMeta)

	// Deliberately no /OutputIntents, an independent defect that keeps the file's
	// verdict from resting on the metadata streams alone.
	data, err := save(pdf, "1.7")
	if err != nil {
		return nil, err
	}

	object := regexp.MustCompile(`(?s)(\d+ 0 obj\n)(<<[^<>]*?/Type /Metadata[^<>]*?>>)(\nstream\n)(.*?)(\nendstream\nendobj\n)`)

	// plantFilter rewrites one metadata object as Flate data, keeping its byte
	// length.
	var planted []byte
	last, count := 0, 0
	for _, m := range object.FindAllSubmatchIndex(data, -1) {
		header, oldDict, openKw := data[m[2]:m[3]], data[m[4]:m[5]], data[m[6]:m[7]]
		payload, closeKw := data[m[8]:m[9]], data[m[10]:m[11]]
		encoded := zlibCompress(payload)
		newDict := fmt.Sprintf("<< /Subtype /XML /Type /Metadata /Filter /FlateDecode /Length %d", len(encoded))
		slack := len(oldDict) + len(payload) - len(newDict) - len(" >>") - len(encoded)
		if slack < 0 {
			return nil, fmt.Errorf("compressed packet does not fit the original object")
		}
		planted = append(planted, data[last:m[0]]...)
		planted = append(planted, header...)
		planted = append(planted, newDict...)
		for range slack {
			planted = append(planted, ' ')
		}
		planted = append(planted, " >>"...)
		planted = append(planted, openKw...)
		planted = append(planted, encoded...)
		planted = append(planted, closeKw...)
		last = m[1]
		count++
	}
	planted = append(planted, data[last:]...)
	if count != 2 {
		return nil, fmt.Errorf("expected 2 metadata objects, patched %d", count)
	}
	if len(planted) != len(data) {
		return nil, fmt.Errorf("patch moved bytes: %d -> %d", len(data), len(planted))
	}
	return planted, nil
}
