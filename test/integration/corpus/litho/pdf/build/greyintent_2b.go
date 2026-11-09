package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("greyintent_2b", greyintent2b) }

// greyintent2b builds greyintent_2b.pdf: transparency on pages whose group names no
// blending colour space, under a grey OutputIntent, at PDF/A-2b.
//
// ISO 32000-1, 11.4.7 and Table 147 leave the blending colour space of a page
// group with no /CS to the reader, and 14.11.5 has the output intent describe
// the device the document is prepared for, so `Writer.FlattenTransparency`
// composites such a page in the intent's device space: DeviceGray here. The
// corpora's 30 files with a grey intent flatten nothing. Every colour is
// DeviceGray, which the intent characterises:
//
// | Page | Draws                                                                   |
// | ---- | ----------------------------------------------------------------------- |
// | 1    | no `/Group`: a light square at ca 0.5 over a dark backdrop              |
// | 2    | a `/Group` with no `/CS`: a light square Multiply over a mid backdrop   |
//
// See ../FIXTURES.md for the authoritative expected firing set.
func greyintent2b() ([]byte, error) {
	pdf := pdfgen.New()

	// greyDisplayProfile is a version 2.1 grey display profile: one tone
	// curve to the PCS.
	greyDisplayProfile := func() []byte {
		tags := []iccTag{{"desc", iccTextDescription("litho grey display")}}
		tags = append(tags, iccCommonTags...)
		tags = append(tags, iccTag{"kTRC", iccGammaCurve(2.2)})
		return iccProfile("mntr", "GRAY", "XYZ ", tags)
	}

	destination := pdf.Stream(greyDisplayProfile())
	destination.Set("N", 1)
	pdf.Root.Set("OutputIntents", a(
		pdf.Indirect(d(
			"Type", n("OutputIntent"),
			"S", n("GTS_PDFA1"),
			"OutputConditionIdentifier", text("litho grey"),
			"DestOutputProfile", pdf.MakeIndirect(destination),
		)),
	))

	addPage := func(content string, states, group *pdfgen.Dict) {
		page := d(
			"Type", n("Page"),
			"MediaBox", a(0, 0, 100, 100),
			"Resources", d("ExtGState", states),
			"Contents", pdf.Stream([]byte(content)),
		)
		if group != nil {
			page.Set("Group", group)
		}
		pdf.AddPage(pdf.Indirect(page))
	}

	addPage(
		"0.2 g 0 0 100 100 re f q /A gs 0.8 g 20 20 50 50 re f Q\n",
		d("A", d("Type", n("ExtGState"), "ca", 0.5)),
		nil,
	)
	addPage(
		"0.5 g 0 0 100 100 re f q /M gs 0.6 g 20 20 50 50 re f Q\n",
		d("M", d("Type", n("ExtGState"), "BM", n("Multiply"))),
		d("Type", n("Group"), "S", n("Transparency")),
	)

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))
	return save(pdf, "1.7")
}
