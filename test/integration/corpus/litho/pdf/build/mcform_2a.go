package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("mcform_2a", mcform2a) }

// mcform2a builds mcform_2a.pdf: marked-content /Lang in forms and annotation appearances.
//
// ISO 19005-2, 6.7.4 holds every /Lang entry, "in the document catalog
// dictionary or in a structure element dictionary or property list", to the
// language tag grammar. veraPDF 1.30.2 judges the property list of every BDC
// operator its content walk reaches, and the walk enters the Form XObjects a
// page draws and the appearance streams of its annotations; each shape below
// was measured with a crafted file at PDF/A-1a and PDF/A-2a. litho read the
// page's own content alone, and reported none of them:
//
// | Where                                                       | /Lang | Rule      |
// | ----------------------------------------------------------- | ----- | --------- |
// | inline BDC in a Form XObject the page draws twice           | `5v`  | `6.7.4/1` |
// | inline BDC in an annotation's normal appearance             | `6u`  | `6.7.4/1` |
// | BDC in a form with no /Resources, naming the page's /P0     | `2y`  | `6.7.4/1` |
//
// The form drawn twice counts once. The form with no /Resources resolves /P0
// through the page's resources, as veraPDF does.
//
// The file is tagged, with a structure tree of one /Document element that owns
// no content. Its sibling mclang_2a.pdf carries the shapes veraPDF does not
// judge and litho reported.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func mcform2a() ([]byte, error) {
	pdf := pdfgen.New()

	form := func(body string, resources bool) *pdfgen.Stream {
		f := pdf.Stream([]byte(body))
		f.Set("Type", n("XObject"))
		f.Set("Subtype", n("Form"))
		f.Set("BBox", a(0, 0, 10, 10))
		if resources {
			f.Set("Resources", d())
		}
		pdf.MakeIndirect(f)
		return f
	}

	annot := pdf.Indirect(d(
		"Type", n("Annot"),
		"Subtype", n("Square"),
		"Rect", a(100, 100, 110, 110),
		"F", 4,
		"AP", d("N", form("/Span <</Lang (6u)>> BDC EMC\n", true)),
	))

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d(
			"Properties", d("P0", pdf.Indirect(d("Lang", text("2y")))),
			"XObject", d(
				"Fm0", form("/Span <</Lang (5v)>> BDC EMC\n", true),
				"Fm1", form("/Span /P0 BDC EMC\n", false),
			),
		),
		"Contents", pdf.Stream([]byte("/Artifact BMC /Fm0 Do /Fm0 Do /Fm1 Do EMC\n")),
		"Annots", a(annot),
	))
	pdf.AddPage(page)

	root := pdf.Indirect(d("Type", n("StructTreeRoot")))
	root.Set("K", pdf.Indirect(d("Type", n("StructElem"), "S", n("Document"), "P", root)))
	pdf.Root.Set("StructTreeRoot", root)
	pdf.Root.Set("MarkInfo", d("Marked", true))

	metadata := pdf.Stream(xmpPacket(xmpOpts{Part: 2, Conformance: "A"}))
	metadata.Set("Type", n("Metadata"))
	metadata.Set("Subtype", n("XML"))
	pdf.Root.Set("Metadata", pdf.MakeIndirect(metadata))

	return save(pdf, "1.7", noPreservePDFA, noFixMetadataVersion)
}
