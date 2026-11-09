package build

import (
	"encoding/binary"
	"fmt"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("qshyphen_2b", qshyphen2b) }

// qshyphen2b builds qshyphen_2b.pdf: soft hyphens shown inside q...Q in a text object.
//
// PDF.js does not advance its text position past a soft hyphen (U+00AD), so it
// reads every glyph after one, until a text-positioning operator sets the
// position again, the hyphen's advance before where ISO 32000-1, 9.4.4 puts it.
// It saves its whole text state at q, text matrix included, and restores it at
// Q, as litho's machine does: a soft hyphen shown inside q...Q leaves no shift
// after Q, one shown before q keeps its shift after Q, and a Td inside q...Q is
// undone. ISO 32000-1, 8.2, Figure 9 allows no q or Q inside a text object, so
// no clause says what Q does to the text matrix there. No corpus file shows a
// soft hyphen inside q...Q in a text object.
//
// Font F1 embeds a Type 1 program holding the letters it shows and sfthyphen,
// every glyph 600 units wide, and a /ToUnicode CMap that maps code AD to U+00AD.
// At 12 points a glyph is 7.2 units wide, and the TJ numbers of -300 leave a
// 3.6-unit gap that PDF.js reads as a word space. The page shows five lines:
//
//	(left) q (AD) Q [-300 (right)]           reads "left right"
//	(outer AD) q (AD) Q [-300 (inner)]       reads "outer inner"
//	(deep) q q (AD) Q (AD) Q [-300 (nest)]   reads "deep nest"
//	(soft AD) q 0 -30 Td Q (ware)            reads "software"
//	(plain) (AD) [-300 (text)]               reads "plain text", the control
//
// Carrying the shift of a soft hyphen inside q...Q past Q closed the gap on the
// first three lines; losing the shift of the one before q at the Td opened one
// on the fourth. A CMYK output intent covers the initial DeviceGray and the XMP
// claims PDF/A-2b, so nothing but the text fires at PDF/A-2b.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func qshyphen2b() ([]byte, error) {
	pdf := pdfgen.New()

	const nonsymbolic = 32
	const softHyphen = 0xAD
	const iccHeaderSize = 128
	const iccVersion21 = 0x02100000

	// iccProfile is a structurally minimal ICC profile, as iccbased_4.go
	// builds it.
	iccProfile := func(deviceClass, colourSpace, tag string) []byte {
		tagTable := binary.BigEndian.AppendUint32(nil, 0)
		header := make([]byte, iccHeaderSize)
		copy(header[0:4], iccU32(nil, iccHeaderSize+len(tagTable)))
		copy(header[4:8], tag)
		binary.BigEndian.PutUint32(header[8:12], iccVersion21)
		copy(header[12:16], deviceClass)
		copy(header[16:20], colourSpace)
		copy(header[20:24], "XYZ ")
		copy(header[36:40], "acsp")
		binary.BigEndian.PutUint32(header[68:72], 0x0000F6D6)
		binary.BigEndian.PutUint32(header[72:76], 0x00010000)
		binary.BigEndian.PutUint32(header[76:80], 0x0000D32D)
		return append(header, tagTable...)
	}

	destination := pdf.Stream(iccProfile("prtr", "CMYK", "OUTI"))
	destination.Set("N", 4)
	pdf.Root.Set("OutputIntents", a(
		pdf.Indirect(d(
			"Type", n("OutputIntent"),
			"S", n("GTS_PDFA1"),
			"OutputConditionIdentifier", text("litho destination"),
			"DestOutputProfile", pdf.MakeIndirect(destination),
		)),
	))

	lines := []string{
		"(left) Tj q (\xad) Tj Q [-300 (right)] TJ",
		"(outer\xad) Tj q (\xad) Tj Q [-300 (inner)] TJ",
		"(deep) Tj q q (\xad) Tj Q (\xad) Tj Q [-300 (nest)] TJ",
		"(soft\xad) Tj q 0 -30 Td Q (ware) Tj",
		"(plain) Tj (\xad) Tj [-300 (text)] TJ",
	}
	const words = "left right outer inner deep nest software plain text"
	letters := sortedLetters(words)
	data, l1, l2, l3 := t1Build(t1Options{Name: "LithoSoftHyphen", Glyphs: append(append([]string(nil), letters...), "sfthyphen")})
	program := pdf.Stream(data)
	program.Set("Length1", l1)
	program.Set("Length2", l2)
	program.Set("Length3", l3)

	descriptor := d(
		"Type", n("FontDescriptor"),
		"FontName", n("/LithoSoftHyphen"),
		"Flags", nonsymbolic,
		"FontBBox", a(0, -200, 550, 700),
		"ItalicAngle", 0,
		"Ascent", 800,
		"Descent", -200,
		"CapHeight", 700,
		"StemV", 80,
		"FontFile", pdf.MakeIndirect(program),
	)
	mapping := map[int]int{}
	for _, c := range letters {
		mapping[int(c[0])] = int(c[0])
	}
	mapping[softHyphen] = softHyphen
	widths := a()
	for range 256 {
		widths.Append(t1AdvanceWidth)
	}
	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("Type1"),
		"BaseFont", n("/LithoSoftHyphen"),
		"FirstChar", 0,
		"LastChar", 255,
		"Widths", widths,
		"Encoding", d("Type", n("Encoding"), "Differences", a(softHyphen, n("/sfthyphen"))),
		"FontDescriptor", pdf.Indirect(descriptor),
		"ToUnicode", toUnicodeCMap(pdf, mapping),
	))

	var body []byte
	y := 700
	for _, line := range lines {
		body = fmt.Appendf(body, "BT /F1 12 Tf 72 %d Td %s ET\n", y, line)
		y -= 40
	}

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d("Font", d("F1", font)),
		"Contents", pdf.MakeIndirect(pdf.Stream(body)),
	))
	pdf.AddPage(page)

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))
	return save(pdf, "1.7")
}
