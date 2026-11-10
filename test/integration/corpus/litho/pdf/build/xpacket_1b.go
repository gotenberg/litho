package build

import (
	"bytes"
	"fmt"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("xpacket_1b", xpacket1b) }

// xpacket1b builds xpacket_1b.pdf: the attributes an XMP packet header may not carry.
//
// | Construct                                          | Rule            |
// | -------------------------------------------------- | --------------- |
// | `bytes` on the catalog packet's xpacket header     | `6.7.5/1`       |
// | `encoding` on the same header                      | `6.7.5/2`       |
//
// ISO 19005-1, 6.7.5 reads `bytes == null` and `encoding == null` against
// veraPDF's XMPPackage. Both attributes ride in the `<?xpacket ...?>` processing
// instruction, not in any PDF dictionary, so reaching the clause means writing the
// packet header rather than setting a key.
//
// The two are carried together and answered separately, so a checker that
// recognised one attribute and not the other is visible as a missing rule rather
// than hidden behind its neighbour.
//
// A second packet hangs off the page carrying the same two attributes. It is the
// control: measured against veraPDF, a packet outside the catalog answers for
// neither test, so a checker matching any packet header rather than the document's
// own is visible as an extra firing. That scope was measured, not assumed.
//
// The save regenerates the packet unless both `preserve_pdfa` and
// `fix_metadata_version` are off, as pikepdf did when it first wrote the
// committed file, and the regenerated header carries neither attribute. The build asserts both survived rather than trusting the flags.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func xpacket1b() ([]byte, error) {
	pdf := pdfgen.New()

	plainHeader := []byte("<?xpacket begin=\"\xef\xbb\xbf\" id=\"W5M0MpCehiHzreSzNTczkc9d\"?>")
	offendingHeader := []byte("<?xpacket begin=\"\xef\xbb\xbf\" id=\"W5M0MpCehiHzreSzNTczkc9d\"" +
		" bytes=\"2048\" encoding=\"UTF-8\"?>")

	// withOffendingHeader rewrites a packet's xpacket header to carry bytes and
	// encoding.
	withOffendingHeader := func(packet []byte) ([]byte, error) {
		if !bytes.Contains(packet, plainHeader) {
			return nil, fmt.Errorf("xmp_packet no longer emits the expected header")
		}
		return bytes.ReplaceAll(packet, plainHeader, offendingHeader), nil
	}

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
		"Contents", pdf.Stream([]byte("BT /F1 12 Tf 50 700 Td (xpacket header attributes) Tj ET\n")),
	))
	pdf.AddPage(page)

	catalogPacket, err := withOffendingHeader(xmpPacket(xmpOpts{Part: 1, Conformance: "B"}))
	if err != nil {
		return nil, err
	}
	catalogMeta := pdf.Stream(catalogPacket, "Type", n("Metadata"), "Subtype", n("XML"))
	pdf.MakeIndirect(catalogMeta)
	pdf.Root.Set("Metadata", catalogMeta)

	// The control. It carries no PDF/A claim of its own, so a second claim cannot be
	// what keeps it silent: only its position outside the catalog can.
	pagePacket, err := withOffendingHeader(xmpPacket(xmpOpts{}))
	if err != nil {
		return nil, err
	}
	pageMeta := pdf.Stream(pagePacket, "Type", n("Metadata"), "Subtype", n("XML"))
	pdf.MakeIndirect(pageMeta)
	page.Set("Metadata", pageMeta)

	// Deliberately no /OutputIntents, an independent defect that keeps the file's
	// verdict from resting on the packet headers alone.
	data, err := save(pdf, "1.7", noPreservePDFA, noFixMetadataVersion)
	if err != nil {
		return nil, err
	}
	if bytes.Count(data, offendingHeader) != 2 {
		return nil, fmt.Errorf("the planted xpacket headers did not survive the save")
	}
	return data, nil
}
