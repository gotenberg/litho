package build

import (
	"encoding/binary"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("sharedclose_ua1", sharedcloseUa1) }

// sharedcloseUa1 builds sharedclose_ua1.pdf: a marked form and an artifact sequence left open,
// in a content stream two pages share, at PDF/UA-1.
//
// ISO 14289-1, 7.20 has a form XObject whose content holds MCIDs referenced once:
// a form's /StructParents names the one parent tree entry its marked-content
// sequences resolve through (ISO 32000-1, Table 95 and 14.7.4.4). veraPDF
// 1.30.2 parses a content stream once for the document, so a Do in a /Contents
// two pages share is one reference. Remediation closes a marked-content
// sequence the page content leaves open (ISO 32000-1, 14.6) in a stream
// appended to the page's /Contents, and gave each page an array of its own
// naming the shared stream, so veraPDF parsed the stream once for each array and
// the form gained a second reference. formshare_ua1.pdf and sharedtag_ua1.pdf
// reach the content-tag wrapper and the auto-tagger; this fixture reaches the
// fixer closing open sequences, which no corpus file reaches on shared content:
//
// | Page | Path                                                      | Answers at PDF/UA-1 |
// | ---- | --------------------------------------------------------- | ------------------- |
// | 1, 2 | shared content, `/F Do`, then an `/Artifact` BMC unclosed | 7.1 t3 (unmarked)   |
// | 3    | content of its own, the `/Artifact` BMC unclosed, no form | 7.1 t3 (unmarked)   |
//
// Every page is a must-not-fire control for 7.20 t2, before and after
// remediation. The form's marked content belongs to a /P the structure tree
// reaches through the form's /StructParents. The document is otherwise tagged:
// /MarkInfo, /Lang, DisplayDocTitle and an XMP packet carrying the pdfuaid
// claim.
//
// A CMYK OutputIntent covers the DeviceGray every stream paints in.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func sharedcloseUa1() ([]byte, error) {
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

	marked := pdf.Stream([]byte("/P <</MCID 0>> BDC 0 g 0 0 10 10 re f EMC\n"))
	marked.Set("Type", n("XObject"))
	marked.Set("Subtype", n("Form"))
	marked.Set("BBox", a(0, 0, 20, 20))
	marked.Set("Resources", d())
	marked.Set("StructParents", 0)
	pdf.MakeIndirect(marked)

	const open = "/Artifact BMC\n0 G 20 90 m 180 90 l S\n"

	addPage := func(contents obj, resources *pdfgen.Dict) *pdfgen.Dict {
		page := d("Type", n("Page"), "MediaBox", a(0, 0, 200, 200), "Resources", resources, "Contents", contents)
		pdf.Indirect(page)
		pdf.AddPage(page)
		return page
	}

	shared := pdf.MakeIndirect(pdf.Stream([]byte("/F Do\n" + open)))
	resources := pdf.Indirect(d("XObject", d("F", marked)))
	first := addPage(shared, resources)
	addPage(shared, resources)
	addPage(pdf.MakeIndirect(pdf.Stream([]byte(open))), d())

	structRoot := pdf.Indirect(d("Type", n("StructTreeRoot")))
	document := pdf.Indirect(d("Type", n("StructElem"), "S", n("Document"), "P", structRoot))
	paragraph := pdf.Indirect(d(
		"Type", n("StructElem"),
		"S", n("P"),
		"P", document,
		"Pg", first,
		"K", d("Type", n("MCR"), "Pg", first, "Stm", marked, "MCID", 0),
	))
	document.Set("K", a(paragraph))
	structRoot.Set("K", a(document))
	structRoot.Set("ParentTree", pdf.Indirect(d("Nums", a(0, a(paragraph)))))
	structRoot.Set("ParentTreeNextKey", 1)

	pdf.Root.Set("StructTreeRoot", structRoot)
	pdf.Root.Set("MarkInfo", d("Marked", true))
	pdf.Root.Set("Lang", text("en-US"))
	pdf.Root.Set("ViewerPreferences", d("DisplayDocTitle", true))

	attachXMP(pdf, xmpPacket(xmpOpts{UAPart: 1}))
	return save(pdf, "1.7", noFixMetadataVersion)
}
