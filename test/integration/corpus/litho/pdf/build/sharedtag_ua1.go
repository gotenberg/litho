package build

import (
	"encoding/binary"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("sharedtag_ua1", sharedtagUa1) }

// sharedtagUa1 builds sharedtag_ua1.pdf: untagged text and a marked form two pages share.
//
// ISO 14289-1, 7.20 has a form XObject whose content holds MCIDs referenced once:
// a form's /StructParents names the one parent tree entry its marked-content
// sequences resolve through (ISO 32000-1, Table 95 and 14.7.4.4). veraPDF
// 1.30.2 parses a content stream once for the document, so a Do in a /Contents
// two pages share is one reference. Remediation's auto-tagger marked each page's
// content in a stream of its own, so the form gained a second reference after
// tagging. formshare_ua1.pdf reaches the content-tag wrapper, which tags pages
// with no text; this fixture reaches the auto-tagger, which tags pages that show
// text:
//
// | Page | Path                                                     | Answers at PDF/UA-1 |
// | ---- | -------------------------------------------------------- | ------------------- |
// | 1, 2 | shared content, Type 3 text then `/F Do`, one resources  | 7.1 t3 (untagged)   |
// | 3    | content of its own, the same text, no form               | 7.1 t3 (untagged)   |
//
// Every page is a must-not-fire control for 7.20 t2, before and after
// remediation. The Type 3 font needs no embedding (ISO 14289-1, 7.21.4.1 names
// Type 3 fonts out of scope) and carries a ToUnicode CMap.
//
// A CMYK OutputIntent covers the DeviceGray every stream paints in.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func sharedtagUa1() ([]byte, error) {
	pdf := pdfgen.New()

	const iccHeaderSize = 128
	const iccVersion21 = 0x02100000

	// iccProfile is a structurally minimal ICC profile, as patternglyph_2b.go builds it.
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

	font := type3Font(
		pdf,
		[]type3Proc{{"A", 600, "0 0 500 700 re f"}, {"B", 600, "0 0 500 700 re f"}},
		[]any{600, 600},
	)
	font.Set("ToUnicode", toUnicodeCMap(pdf, map[int]int{65: 0x41, 66: 0x42}))
	pdf.Indirect(font)

	marked := pdf.Stream([]byte("/P <</MCID 0>> BDC 0 g 0 0 10 10 re f EMC\n"))
	marked.Set("Type", n("XObject"))
	marked.Set("Subtype", n("Form"))
	marked.Set("BBox", a(0, 0, 20, 20))
	marked.Set("Resources", d())
	marked.Set("StructParents", 0)
	pdf.MakeIndirect(marked)

	const show = "BT /T 24 Tf 20 150 Td (ABABBABA) Tj ET\n"

	addPage := func(contents obj, resources *pdfgen.Dict) {
		page := d("Type", n("Page"), "MediaBox", a(0, 0, 200, 200), "Resources", resources, "Contents", contents)
		pdf.AddPage(pdf.Indirect(page))
	}

	shared := pdf.MakeIndirect(pdf.Stream([]byte(show + "/F Do\n")))
	resources := pdf.Indirect(d("Font", d("T", font), "XObject", d("F", marked)))
	addPage(shared, resources)
	addPage(shared, resources)
	addPage(pdf.MakeIndirect(pdf.Stream([]byte(show))), d("Font", d("T", font)))

	attachXMP(pdf, xmpPacket(xmpOpts{UAPart: 1}))
	return save(pdf, "1.7")
}
