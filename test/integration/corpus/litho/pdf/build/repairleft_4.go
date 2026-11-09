package build

import (
	"bytes"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("repairleft_4", repairleft4) }

// repairleft4 builds repairleft_4.pdf: repairs that reach inherited colour spaces, keep the
// line a text operator moves to, and touch only what a finding names.
//
// The first page states no /Resources and inherits the root node's, which holds
// every colour space, the font and a form inline (ISO 32000-1, 7.7.3.4). The
// detectors read inherited resources, and so does veraPDF, but three colour
// repairs read the page's own /Resources alone, so they repaired nothing in the
// round that found the defects. One corpus page inherits a DeviceN the first
// repair completes, and none a Separation pair or an ICCBased space the others
// repair.
//
// | Name  | Construct, inherited from the root node                  | A-4         |
// | ----- | -------------------------------------------------------- | ----------- |
// | `CSN` | a DeviceN over SpotA and SpotB with no `/Attributes`     | `6.2.4.4/1` |
// | `CS1` | a Separation naming SpotC over one tint transform        | `6.2.4.4/2` |
// | `CS2` | a Separation naming SpotC over another                   | `6.2.4.4/2` |
// | `CS3` | an ICCBased space over the OutputIntent's own profile    | `6.2.4.2/3` |
// | `Fm0` | a form painting in the same ICCBased space of its own    | `6.2.4.2/3` |
//
// The page also shows text with the two operators that do more than show a
// string: `string '` moves to the next line first, and `aw ac string "` also sets
// the word and character spacing (ISO 32000-1, 9.4.3, Table 109). Each shows
// `(\x01)`, whose code 1 names no glyph under WinAnsiEncoding and shows
// `.notdef`, and a visible line follows each. Stripping the control byte empties
// the string, and the repair dropped the whole operation with it, so every later
// line drew one line higher. No corpus page the `.notdef` repair runs on holds
// either operator.
//
// The font is the conforming non-symbolic TrueType of `ttcmap_2b.pdf`, embedded,
// with `/Widths` from code 1 so the `.notdef` width agrees with the dictionary.
//
// The first page carries its own XMP packet, whose header holds the `bytes`
// attribute every packet is denied (`6.7.2.1/2`), while the catalog packet needs
// no repair. The metadata repair returned as soon as it found nothing to patch in
// the catalog packet, before it reached the page's.
//
// Two more pages paint DeviceCMYK under a transparency group blending in a device
// space: DeviceRGB, which no RGB OutputIntent covers (`6.2.4.3/2`), and
// DeviceCMYK, which the CMYK OutputIntent does. The group repair replaced both
// with ICCBased sRGB, changing how the conforming group composites. No corpus
// file where that repair runs carries a device blending space no finding names.
//
// The tree is built by hand: pikepdf's page list pushes inherited attributes onto
// each page, which would delete the inheritance the fixture exists for.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func repairleft4() ([]byte, error) {
	pdf := pdfgen.New()

	destination := pdf.Stream(iccMinimalProfile("prtr", "CMYK", iccVersion21, "OUTI"))
	destination.Set("N", 4)
	pdf.MakeIndirect(destination)
	pdf.Root.Set("OutputIntents", a(
		pdf.Indirect(d(
			"Type", n("OutputIntent"),
			"S", n("GTS_PDFA1"),
			"OutputConditionIdentifier", text("litho destination"),
			"DestOutputProfile", destination,
		)),
	))

	latin := map[int]string{0x41: "A", 0x42: "B"}
	program := ttBuild([]ttSubtable{{3, 1, latin}, {1, 0, latin}}, nil)
	fontFile := pdf.Stream(program)
	fontFile.Set("Length1", len(program))
	const first, last = 1, 66
	widths := a()
	for i := first; i <= last; i++ {
		widths.Append(ttAdvanceWidth)
	}
	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("TrueType"),
		"BaseFont", n("/LithoRepairSans"),
		"FirstChar", first,
		"LastChar", last,
		"Widths", widths,
		"FontDescriptor", pdf.Indirect(d(
			"Type", n("FontDescriptor"),
			"FontName", n("/LithoRepairSans"),
			"Flags", 32, // non-symbolic, ISO 32000-1, Table 123
			"ItalicAngle", 0,
			"Ascent", 800,
			"Descent", -200,
			"CapHeight", 700,
			"StemV", 80,
			"FontBBox", a(0, -200, 550, 700),
			"FontFile2", pdf.MakeIndirect(fontFile),
		)),
		"Encoding", n("WinAnsiEncoding"),
	))

	// tint is a Type 2 tint transform from one spot component to CMYK.
	tint := func(c1 ...any) *pdfgen.Dict {
		return pdf.Indirect(d(
			"FunctionType", 2, "Domain", a(0, 1), "C0", a(0, 0, 0, 0), "C1", a(c1...), "N", 1))
	}

	tintN := pdf.Stream([]byte("{ pop pop 0 0 0 0 }"))
	tintN.Set("FunctionType", 4)
	tintN.Set("Domain", a(0, 1, 0, 1))
	tintN.Set("Range", a(0, 1, 0, 1, 0, 1, 0, 1))
	// The form's space carries the destination profile's bytes in an object of its
	// own: test 3 compares profiles by content, and a distinct object keeps the
	// form's verdict from folding into the page's.
	formProfile := pdf.Stream(iccMinimalProfile("prtr", "CMYK", iccVersion21, "OUTI"))
	formProfile.Set("N", 4)
	form := pdf.Stream([]byte("/CS0 cs 0 0 0 1 scn 0 0 10 10 re f"))
	form.Set("Type", n("XObject"))
	form.Set("Subtype", n("Form"))
	form.Set("BBox", a(0, 0, 10, 10))
	form.Set("Resources", d("ColorSpace", d("CS0", a(n("ICCBased"), pdf.MakeIndirect(formProfile)))))

	root := pdf.Pages
	root.Set("MediaBox", a(0, 0, 612, 792))
	root.Set("Resources", d(
		"Font", d("F1", font),
		"ColorSpace", d(
			"CSN", a(n("DeviceN"), a(n("/SpotA"), n("/SpotB")), n("DeviceCMYK"), pdf.MakeIndirect(tintN)),
			"CS1", a(n("Separation"), n("/SpotC"), n("DeviceCMYK"), tint(1, 0, 0, 0)),
			"CS2", a(n("Separation"), n("/SpotC"), n("DeviceCMYK"), tint(0, 1, 0, 0)),
			"CS3", a(n("ICCBased"), destination),
		),
		"XObject", d("Fm0", pdf.MakeIndirect(form)),
	))

	content := "q /CSN cs 0.5 0.5 scn 50 50 20 20 re f Q\n" +
		"q /CS1 cs 1 scn 80 50 20 20 re f Q\n" +
		"q /CS2 cs 1 scn 110 50 20 20 re f Q\n" +
		"q /CS3 cs 0 0 0 1 scn 140 50 20 20 re f Q\n" +
		"q 1 0 0 1 170 50 cm /Fm0 Do Q\n" +
		"BT /F1 12 Tf 14 TL 72 700 Td (AB) Tj\n" +
		"2 1 (\x01) \" (AB) Tj\n" +
		"(\x01) ' (BA) Tj ET"
	pagePacket := pdf.Stream([]byte(
		"<?xpacket begin=\"\" id=\"W5M0MpCehiHzreSzNTczkc9d\" bytes=\"512\"?>\n" +
			"<x:xmpmeta xmlns:x=\"adobe:ns:meta/\">\n" +
			" <rdf:RDF xmlns:rdf=\"http://www.w3.org/1999/02/22-rdf-syntax-ns#\">\n" +
			"  <rdf:Description rdf:about=\"\" xmlns:xmp=\"http://ns.adobe.com/xap/1.0/\">\n" +
			"   <xmp:CreatorTool>litho corpus fixture</xmp:CreatorTool>\n" +
			"  </rdf:Description>\n" +
			" </rdf:RDF>\n" +
			"</x:xmpmeta>\n" +
			"<?xpacket end=\"w\"?>",
	))
	pagePacket.Set("Type", n("Metadata"))
	pagePacket.Set("Subtype", n("XML"))
	page := pdf.Indirect(d(
		"Type", n("Page"),
		"Parent", root,
		"Contents", pdf.MakeIndirect(pdf.Stream([]byte(content))),
		"Metadata", pdf.MakeIndirect(pagePacket),
	))

	// groupPage is a page painting DeviceCMYK under a transparency group
	// blending in a device space, with resources of its own.
	groupPage := func(blending pdfgen.Name) *pdfgen.Dict {
		return pdf.Indirect(d(
			"Type", n("Page"),
			"Parent", root,
			"Resources", d(),
			"Group", d("Type", n("Group"), "S", n("Transparency"), "CS", blending),
			"Contents", pdf.MakeIndirect(pdf.Stream([]byte("0 0 0 1 k 50 50 20 20 re f"))),
		))
	}

	root.Set("Kids", a(page, groupPage(n("DeviceRGB")), groupPage(n("DeviceCMYK"))))
	root.Set("Count", 3)

	// The packet ends before the EOL that precedes endstream, which /Length must
	// not count (ISO 32000-2, 7.3.8.1).
	attachXMP(pdf, bytes.TrimRight(xmpPacket(xmpOpts{Part: 4, Rev: 2020}), "\n"))
	return save(pdf, "2.0", noPreservePDFA, noFixMetadataVersion)
}
