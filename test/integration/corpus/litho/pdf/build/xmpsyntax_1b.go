package build

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("xmpsyntax_1b", xmpsyntax1b) }

// xmpsyntax1b builds xmpsyntax_1b.pdf: three defects, one per layer of the file.
//
// | Construct                                          | Rule            |
// | -------------------------------------------------- | --------------- |
// | a header that is not `%PDF-` followed by `n.n`     | `6.1.2/1`       |
// | a cross-reference stream instead of a table        | `6.1.4/3`       |
// | a raw `<` inside the XMP packet's character data   | `6.7.9/1`       |
//
// The three sit in three different layers: the first eight bytes of the file, the
// format of its cross-reference section, and the XML inside its metadata stream.
// That separation is the point. A reader that gave up on the malformed header
// would never reach the cross-reference section, and one that gave up there would
// never reach the metadata, so each rule firing is evidence that the reader kept
// going past the layer above it. A file carrying only one of the three could not
// show that.
//
// `6.1.2/1` reads `headerOffset == 0 && /%PDF-\d\.\d/.test(header)`. Turning
// `%PDF-1.7` into `%PDF-17.` breaks the pattern while leaving the header at offset
// zero and every byte after it exactly where it was, so the cross-reference
// offsets stay valid and the file still parses. Prepending junk instead would move
// every offset in the file, which makes veraPDF fail to parse rather than report.
//
// The raw `<` also costs the file its PDF/A identification, because the packet
// carrying it cannot be parsed, so `6.7.11/1` fires alongside. That is the
// construct working, not a second defect: the identification lives inside the
// packet the `<` breaks.
//
// The save regenerates the XMP packet unless both `preserve_pdfa` and
// `fix_metadata_version` are off, as pikepdf did when it first wrote the
// committed file, and a regenerated packet silently repairs the planted `<`.
// The build asserts the byte survived rather than trusting the flags.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func xmpsyntax1b() ([]byte, error) {
	pdf := pdfgen.New()

	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("Type1"),
		"BaseFont", n("/Helvetica"),
		"Encoding", n("WinAnsiEncoding"),
	))

	// page is one conforming page. The defects live outside the page tree.
	page := func(text string) {
		pdf.AddPage(pdf.Indirect(d(
			"Type", n("Page"),
			"MediaBox", a(0, 0, 612, 792),
			"Resources", d("Font", d("F1", font)),
			"Contents", pdf.Stream([]byte(fmt.Sprintf("BT /F1 12 Tf 50 700 Td (%s) Tj ET\n", text))),
		)))
	}

	// Two pages, both clean. They exist so a reader that recovers from the header
	// and cross-reference defects has something whole to report on, which is what
	// makes the absence of page-level violations meaningful.
	page("header, cross-reference and XMP syntax")
	page("a second page, carrying no defect of its own")

	// 6.7.9 t1: a raw '<' where XML requires an entity. The packet stops being
	// parseable, and with it the PDF/A identification it carries.
	packet := bytes.ReplaceAll(xmpPacket(xmpOpts{Part: 1, Conformance: "B"}),
		[]byte("litho corpus fixture"), []byte("litho < corpus fixture"))

	metadata := pdf.Stream(packet)
	metadata.Set("Type", n("Metadata"))
	metadata.Set("Subtype", n("XML"))
	pdf.Root.Set("Metadata", pdf.MakeIndirect(metadata))

	// 6.1.4 t3: object streams force a cross-reference stream, which ISO 19005-1
	// forbids outright. Deliberately no /OutputIntents, an independent defect that
	// keeps the file-level verdict from resting on the three constructs alone.
	data, err := save(pdf, "1.7", generateObjectStreams, noPreservePDFA, noFixMetadataVersion)
	if err != nil {
		return nil, err
	}

	if !bytes.Contains(data, []byte("litho < corpus fixture")) {
		return nil, errors.New("the planted '<' did not survive the save")
	}

	// 6.1.2 t1: break the version pattern without moving a byte, so every
	// cross-reference offset written above stays correct.
	if string(data[:8]) != "%PDF-1.7" {
		return nil, fmt.Errorf("unexpected header %q", data[:8])
	}
	copy(data[:8], "%PDF-17.")
	return data, nil
}
