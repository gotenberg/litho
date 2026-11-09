package build

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"unicode/utf16"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
	"github.com/gotenberg/litho/test/integration/fixture/pdfgen/zdeflate"
)

func init() { register("redactvectors_4", redactvectors4) }

// redactvectors4 builds redactvectors_4.pdf: copies of page text no comparison of bytes finds.
//
// Redaction verifies its output by looking for every removed string in what a
// rewrite writes. Four copies of page 1's paragraph sit where that comparison
// cannot see them, and before, redacting page 1 wrote every one out and reported
// success:
//
//   - The catalog's /OpenAction and page 1's /AA /O are JavaScript actions that
//     build the paragraph at run time from two pieces (ISO 32000-1, 12.6.4.16).
//   - The catalog's /Names /JavaScript tree runs a script held in a text stream,
//     UTF-16BE behind its byte order mark (7.9.3), which interleaves every letter
//     with a zero byte.
//   - Page 1's /Metadata is an XMP packet carrying a page thumbnail as base64
//     (14.3.2; xmpGImg:image), here bytes standing in for its pixels.
//   - The catalog's /PieceInfo holds private data (14.5, Table 368): a stream with
//     no filter whose bytes are a zlib stream of the paragraph, an application's
//     own compressed format.
//
// Redaction now removes every script, the redacted page's /Metadata and the
// catalog's private data, keeping the /PieceInfo dictionary and its
// /LastModified.
//
// Two attachments hold no copy and must stay: a zip archive and a PDF whose
// content stream is Flate-compressed, each about page two. The scan reads into
// both and finds nothing.
//
// Page 2 draws text no redaction of page 1 touches.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func redactvectors4() ([]byte, error) {
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

	const splitScript = "var plan = 'the sec' + 'ret plan';"

	// script is a JavaScript action building the paragraph from two pieces.
	script := func() *pdfgen.Dict {
		return d("S", n("JavaScript"), "JS", text(splitScript))
	}

	pdf.Root.Set("OpenAction", script())
	pdf.Page(0).Set("AA", d("O", script()))

	wideData := []byte{0xfe, 0xff}
	for _, u := range utf16.Encode([]rune("app.alert('the secret plan');")) {
		wideData = binary.BigEndian.AppendUint16(wideData, u)
	}
	wide := pdf.Stream(wideData)
	pdf.Root.Set("Names", d(
		"JavaScript", d(
			"Names", a(text("init"), pdf.Indirect(d("S", n("JavaScript"), "JS", wide))),
		),
	))

	thumbnail := base64.StdEncoding.EncodeToString([]byte("thumbnail pixels of page one as it was"))
	xmp := pdf.Stream([]byte(
		`<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF ` +
			`xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` +
			`<rdf:Description rdf:about="" xmlns:xmpGImg="http://ns.adobe.com/xap/1.0/g/img/">` +
			"<xmpGImg:image>" + thumbnail + "</xmpGImg:image>" +
			"</rdf:Description></rdf:RDF></x:xmpmeta>",
	))
	xmp.Set("Type", n("Metadata"))
	xmp.Set("Subtype", n("XML"))
	pdf.Page(0).Set("Metadata", pdf.MakeIndirect(xmp))

	// No /Filter: the bytes are the application's own compressed format, which no
	// decode reads, so the stored zlib stream hides the paragraph from the scan.
	private := pdf.Stream(zdeflate.Compress(bytes.Repeat([]byte(words), 8), 9))
	pdf.Root.Set("PieceInfo", d(
		"LithoFixture", d("LastModified", text("D:20260101000000Z"), "Private", pdf.MakeIndirect(private)),
	))

	// notesZip is a zip archive of one deflated member about page two,
	// deterministic. It writes the bytes Python's zipfile writes for one
	// ZipInfo member: a raw deflate stream at zlib's default level, no data
	// descriptor, Unix as the creating system and 0o600 as the mode.
	notesZip := func() []byte {
		member := bytes.Repeat([]byte("notes about page two, reviewed.\n"), 32)
		zlibbed := zdeflate.Compress(member, -1)
		deflated := zlibbed[2 : len(zlibbed)-4]
		name := "notes.txt"
		// The MS-DOS time and date of (2026, 1, 1, 0, 0, 0).
		const dosTime, dosDate = 0, (2026-1980)<<9 | 1<<5 | 1
		crc := crc32.ChecksumIEEE(member)
		fields := func(b []byte) []byte {
			b = binary.LittleEndian.AppendUint16(b, 20) // version needed to extract
			b = binary.LittleEndian.AppendUint16(b, 0)  // flags
			b = binary.LittleEndian.AppendUint16(b, 8)  // deflated
			b = binary.LittleEndian.AppendUint16(b, dosTime)
			b = binary.LittleEndian.AppendUint16(b, dosDate)
			b = binary.LittleEndian.AppendUint32(b, crc)
			b = binary.LittleEndian.AppendUint32(b, uint32(len(deflated))) //nolint:gosec // a few dozen bytes.
			b = binary.LittleEndian.AppendUint32(b, uint32(len(member)))   //nolint:gosec // 1 KiB.
			b = binary.LittleEndian.AppendUint16(b, uint16(len(name)))     //nolint:gosec // 9.
			return binary.LittleEndian.AppendUint16(b, 0)                  // extra field length
		}
		out := fields([]byte("PK\x03\x04"))
		out = append(out, name...)
		out = append(out, deflated...)
		central := len(out)
		out = append(out, "PK\x01\x02"...)
		out = binary.LittleEndian.AppendUint16(out, 3<<8|20) // made by: Unix, 2.0
		out = fields(out)
		out = binary.LittleEndian.AppendUint16(out, 0)         // comment length
		out = binary.LittleEndian.AppendUint16(out, 0)         // disk number start
		out = binary.LittleEndian.AppendUint16(out, 0)         // internal attributes
		out = binary.LittleEndian.AppendUint32(out, 0o600<<16) // external attributes
		out = binary.LittleEndian.AppendUint32(out, 0)         // local header offset
		out = append(out, name...)
		end := len(out)
		out = append(out, "PK\x05\x06"...)
		out = binary.LittleEndian.AppendUint16(out, 0)                   // this disk
		out = binary.LittleEndian.AppendUint16(out, 0)                   // central directory disk
		out = binary.LittleEndian.AppendUint16(out, 1)                   // entries on this disk
		out = binary.LittleEndian.AppendUint16(out, 1)                   // entries
		out = binary.LittleEndian.AppendUint32(out, uint32(end-central)) //nolint:gosec // small.
		out = binary.LittleEndian.AppendUint32(out, uint32(central))     //nolint:gosec // small.
		return binary.LittleEndian.AppendUint16(out, 0)                  // comment length
	}

	// notesPDF is a one-page PDF whose Flate-compressed content is about page
	// two.
	notesPDF := func() []byte {
		content := zdeflate.Compress(bytes.Repeat([]byte("BT /F1 12 Tf 72 700 Td (notes about page two) Tj ET\n"), 8), 9)
		objects := [][]byte{
			[]byte("<< /Type /Catalog /Pages 2 0 R >>"),
			[]byte("<< /Type /Pages /Kids [3 0 R] /Count 1 >>"),
			[]byte("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R >>"),
			append(append([]byte(fmt.Sprintf("<< /Filter /FlateDecode /Length %d >>\nstream\n", len(content))), content...), "\nendstream"...),
		}
		out := []byte("%PDF-1.7\n")
		offsets := make([]int, 0, len(objects))
		for i, body := range objects {
			offsets = append(offsets, len(out))
			out = append(out, fmt.Sprintf("%d 0 obj\n", i+1)...)
			out = append(out, body...)
			out = append(out, "\nendobj\n"...)
		}
		xref := len(out)
		out = append(out, "xref\n0 5\n0000000000 65535 f\r\n"...)
		for _, offset := range offsets {
			out = append(out, fmt.Sprintf("%010d 00000 n\r\n", offset)...)
		}
		out = append(out, fmt.Sprintf("trailer\n<< /Size 5 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xref)...)
		return out
	}

	// filespec is a file specification embedding data as name.
	filespec := func(name string, data []byte, subtype string) *pdfgen.Dict {
		stream := pdf.Stream(data)
		stream.Set("Type", n("EmbeddedFile"))
		stream.Set("Subtype", n(subtype))
		pdf.MakeIndirect(stream)
		return pdf.Indirect(d(
			"Type", n("Filespec"),
			"F", text(name),
			"UF", text(name),
			"AFRelationship", n("Supplement"),
			"EF", d("F", stream, "UF", stream),
		))
	}

	notes := filespec("notes.zip", notesZip(), "/application/zip")
	draft := filespec("draft.pdf", notesPDF(), "/application/pdf")
	pdf.Root.Get("Names").(*pdfgen.Dict).Set("EmbeddedFiles", d("Names", a(text("draft.pdf"), draft, text("notes.zip"), notes)))
	pdf.Root.Set("AF", a(draft, notes))

	return save(pdf, "2.0")
}
