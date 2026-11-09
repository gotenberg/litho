package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("repairroom_2b", repairroom2b) }

// repairroom2b builds repairroom_2b.pdf: repairs that keep what a text operator moves and
// reach the colour space of a shading.
//
// The `.notdef` repair strips a code a font gives no glyph from the strings a
// page shows. Two things the stripped operation did besides showing a glyph are
// planted here, each with a visible string after it on the same line. No corpus
// page the repair runs on carries the first; the second is on 18 corpus files,
// litho repairleft_4.pdf and inlineimage_2b.pdf among them:
//
// | Line | Operations                          | What stripping `\x01` must keep       |
// | ---- | ----------------------------------- | -------------------------------------- |
// | 1    | `(AB) Tj [(\x01) -3000] TJ (BA) Tj` | the 3000-unit move of the emptied TJ   |
// | 2    | `(A\x01B) Tj (AB) Tj`               | the 600-unit width of the `.notdef`    |
//
// A number in a TJ array moves the text position (ISO 32000-1, 9.4.3), and a
// glyph moves it by its width (9.4.4). The repair dropped an emptied TJ whole
// and removed the glyph with its width, so poppler drew `BA` 43.2 units and the
// second `B` and `AB` 7.2 units further left than the source.
//
// The page also paints with two DeviceN colour spaces over spot colorants with
// no `/Colorants`, which ISO 19005-2, 6.2.4.4 t1 fails, each in a shading
// rather than the page's `/ColorSpace` resources:
//
// | Name  | Where                                         | Colorants          |
// | ----- | --------------------------------------------- | ------------------ |
// | `P0`  | a shading pattern `scn` paints with           | SpotX and SpotY    |
// | `Sh0` | a shading `sh` paints, in `/Shading`          | SpotX and SpotZ    |
//
// `CSX`, a Separation of SpotX the page also paints with, is the definition a
// completed `/Colorants` must reuse (6.2.4.4 t2). The `/Colorants` repair walked
// the page's `/ColorSpace` resources alone, so both kept t1. Of the five corpus
// files whose shadings use a DeviceN short of colorants, pdfjs issue17065.pdf is
// the one a remediation left short, and the remediation corpus runs none of
// them.
//
// The font is the conforming non-symbolic TrueType of `repairleft_4.pdf`,
// embedded, with `/Widths` from code 1 so the `.notdef` width agrees with the
// dictionary. A CMYK OutputIntent covers the DeviceCMYK alternates.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func repairroom2b() ([]byte, error) {
	pdf := pdfgen.New()

	const iccVersion21 = 0x02100000

	// iccProfile is a structurally minimal ICC profile, as iccbased_4.go
	// builds it.
	iccProfile := func(deviceClass, colourSpace, tag string) []byte {
		return iccMinimalProfile(deviceClass, colourSpace, iccVersion21, tag)
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

	// spotTint is a Type 2 tint transform from one spot component to CMYK.
	spotTint := func(c1 *pdfgen.Array) *pdfgen.Dict {
		return pdf.Indirect(
			d("FunctionType", 2, "Domain", a(0, 1), "C0", a(0, 0, 0, 0), "C1", c1, "N", 1),
		)
	}

	// devicen is a DeviceN over SpotX and second, with no /Attributes.
	devicen := func(second string) *pdfgen.Array {
		tint := pdf.Stream([]byte("{ pop " + "pop 0 0 0 1 }"))
		tint.Set("FunctionType", 4)
		tint.Set("Domain", a(0, 1, 0, 1))
		tint.Set("Range", a(0, 1, 0, 1, 0, 1, 0, 1))
		return a(n("DeviceN"), a(n("/SpotX"), n(second)), n("DeviceCMYK"), pdf.MakeIndirect(tint))
	}

	// axial is an axial shading over colourSpace, both components 0 to 1.
	axial := func(colourSpace *pdfgen.Array) *pdfgen.Dict {
		return d(
			"ShadingType", 2,
			"ColorSpace", colourSpace,
			"Coords", a(0, 0, 100, 0),
			"Function", pdf.Indirect(
				d("FunctionType", 2, "Domain", a(0, 1), "C0", a(0, 0), "C1", a(1, 1), "N", 1),
			),
		)
	}

	pattern := pdf.Indirect(d("Type", n("Pattern"), "PatternType", 2, "Shading", axial(devicen("/SpotY"))))

	content := "BT /F1 12 Tf 72 700 Td (AB) Tj [(\x01) -3000] TJ (BA) Tj ET\n" +
		"BT /F1 12 Tf 72 670 Td (A\x01B) Tj (AB) Tj ET\n" +
		"q /CSX cs 1 scn 50 500 40 40 re f Q\n" +
		"q /Pattern cs /P0 scn 100 500 100 40 re f Q\n" +
		"q 250 500 100 40 re W n 1 0 0 1 250 500 cm /Sh0 sh Q\n"
	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d(
			"Font", d("F1", font),
			"ColorSpace", d("CSX", a(n("Separation"), n("/SpotX"), n("DeviceCMYK"), spotTint(a(0, 0, 1, 0)))),
			"Pattern", d("P0", pattern),
			"Shading", d("Sh0", axial(devicen("/SpotZ"))),
		),
		"Contents", pdf.MakeIndirect(pdf.Stream([]byte(content))),
	))
	pdf.AddPage(page)

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))
	return save(pdf, "1.7", noFixMetadataVersion)
}
