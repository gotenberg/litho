package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("formuntagged_ua1", formuntaggedUa1) }

// formuntaggedUa1 builds formuntagged_ua1.pdf: an untagged document whose widgets paint, at
// PDF/UA-1.
//
// ISO 14289-1, 7.1 and ISO 14289-2, 8.2.2 require every piece of page content
// of a document claiming conformance to be tagged as real content or marked as
// an artifact. A widget is not page content, so a document with no structure
// tree passes those tests while its page draws nothing. Flattening the form
// draws each appearance into the page, where there is no structure element to
// tag it under, so in a document claiming PDF/UA, as this one does, litho marks
// it as an artifact (ISO 32000-1, 14.8.2.2). The other litho fixtures with
// painting widgets are tagged, and the untagged ones paint nothing or claim no
// PDF/UA, so this fixture gives the gate the shape:
//
// | Field   | Kind       | /Rect             | Appearance                              |
// | ------- | ---------- | ----------------- | --------------------------------------- |
// | name    | text       | [40 220 240 250]  | a light box, a dark corner and a border |
// | agree   | check box  | [40 170 60 190]   | /On: a filled square; /Off: a border    |
// | comment | text       | [40 60 360 140]   | the box, its /BBox offset from 0,0      |
//
// The page's own content draws nothing. The file has no /StructTreeRoot and no
// /MarkInfo; the XMP packet claims PDF/UA-1, and an RGB OutputIntent covers the
// DeviceRGB the appearances paint.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func formuntaggedUa1() ([]byte, error) {
	const printFlag = 4

	pdf := pdfgen.New()

	destination := pdf.Stream(iccRGBDisplayProfile())
	destination.Set("N", 3)
	pdf.Root.Set("OutputIntents", a(
		pdf.Indirect(d(
			"Type", n("OutputIntent"),
			"S", n("GTS_PDFA1"),
			"OutputConditionIdentifier", text("litho RGB display"),
			"DestOutputProfile", pdf.MakeIndirect(destination),
		)),
	))

	form := func(content string, bbox ...any) *pdfgen.Stream {
		s := pdf.Stream([]byte(content))
		s.Set("Type", n("XObject"))
		s.Set("Subtype", n("Form"))
		s.Set("BBox", a(bbox...))
		s.Set("Resources", d())
		pdf.MakeIndirect(s)
		return s
	}

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"Parent", pdf.Pages,
		"MediaBox", a(0, 0, 400, 300),
		"Resources", d(),
		"Contents", pdf.MakeIndirect(pdf.Stream([]byte(""))),
	))
	pdf.Pages.Set("Kids", a(page))
	pdf.Pages.Set("Count", 1)

	name := pdf.Indirect(d(
		"Type", n("Annot"),
		"Subtype", n("Widget"),
		"FT", n("Tx"),
		"T", text("name"),
		"TU", text("Name"),
		"Rect", a(40, 220, 240, 250),
		"P", page,
		"F", printFlag,
		"AP", d(
			"N", form(
				"0.9 0.93 1 rg 0 0 200 30 re f 0 0 0.5 rg 0 0 30 10 re f 0.7 0 0 RG 2 w 1 1 198 28 re S\n",
				0, 0, 200, 30,
			),
		),
	))
	agree := pdf.Indirect(d(
		"Type", n("Annot"),
		"Subtype", n("Widget"),
		"FT", n("Btn"),
		"T", text("agree"),
		"TU", text("I agree"),
		"V", n("On"),
		"AS", n("On"),
		"Rect", a(40, 170, 60, 190),
		"P", page,
		"F", printFlag,
		"AP", d(
			"N", d(
				"On", form("0 0 0 RG 1 w 0.5 0.5 19 19 re S 0 0.4 0 rg 4 4 12 12 re f\n", 0, 0, 20, 20),
				"Off", form("0 0 0 RG 1 w 0.5 0.5 19 19 re S\n", 0, 0, 20, 20),
			),
		),
	))
	comment := pdf.Indirect(d(
		"Type", n("Annot"),
		"Subtype", n("Widget"),
		"FT", n("Tx"),
		"T", text("comment"),
		"TU", text("Comment"),
		"Rect", a(40, 60, 360, 140),
		"P", page,
		"F", printFlag,
		"AP", d(
			"N", form(
				"1 0.95 0.85 rg 100 100 320 80 re f 0 0 0.5 rg 100 100 40 20 re f 0.7 0 0 RG 2 w 101 101 318 78 re S\n",
				100, 100, 420, 180,
			),
		),
	))

	page.Set("Annots", a(name, agree, comment))
	pdf.Root.Set("AcroForm", d("Fields", a(name, agree, comment)))
	pdf.Root.Set("Lang", text("en-US"))
	pdf.Root.Set("ViewerPreferences", d("DisplayDocTitle", true))
	attachXMP(pdf, xmpPacket(xmpOpts{UAPart: 1}))
	return save(pdf, "1.7")
}
