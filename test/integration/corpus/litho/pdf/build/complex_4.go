package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("complex_4", complex4) }

// complex4 builds complex_4.pdf: a PDF/A-4 fixture built around part-4 specifics.
//
// ISO 19005-4 is the PDF 2.0 part, and it draws its lines in places no earlier
// part does: a pdfaid:rev of 2020, a document information dictionary that may
// only carry ModDate, page-level output intents, associated files with a
// mandatory AFRelationship, and outright bans on Requirements and PresSteps.
// Every defect below is independent, so a rule that short-circuits shows up as
// a missing class rather than a different verdict.
//
// Four must-not-fire controls guard against over-suppression:
//   - the header is pinned to %PDF-2.0, which 6.1.2 requires,
//   - pdfaid:part is 4 and pdfaid:conformance is absent, both correct,
//   - one file specification carries /AFRelationship next to one that does not,
//   - a second image carries /Interpolate false next to one set true.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func complex4() ([]byte, error) {
	pdf := pdfgen.New()

	tt := pdf.Indirect(unembeddedTrueType("/Helvetica"))

	imgInterp := rgbImage(pdf, true)
	imgOK := rgbImage(pdf, false)
	imgOK.Set("Interpolate", false)

	// Transfer function in a graphics state parameter dictionary: ISO 19005-4,
	// 6.2.5 bans /TR outright and restricts /TR2 to /Default.
	gs := pdf.Indirect(d("Type", n("ExtGState"), "TR", n("Identity"), "TR2", n("Identity")))

	// Reference XObject: ISO 19005-4, 6.2.8.2.
	refForm := pdf.Stream([]byte("0 0 10 10 re f\n"))
	refForm.Set("Type", n("XObject"))
	refForm.Set("Subtype", n("Form"))
	refForm.Set("BBox", a(0, 0, 10, 10))
	refForm.Set("Resources", d())
	refForm.Set("Ref", d(
		"F", d("Type", n("Filespec"), "F", text("target.pdf")), "Page", 0,
	))

	// Transparency group with no blending colour space. With no PDF/A output
	// intent anywhere, ISO 19005-4, 6.2.9 requires one or the other.
	groupForm := pdf.Stream([]byte("0 0 1 rg 0 0 50 50 re f\n"))
	groupForm.Set("Type", n("XObject"))
	groupForm.Set("Subtype", n("Form"))
	groupForm.Set("BBox", a(0, 0, 50, 50))
	groupForm.Set("Group", d("Type", n("Group"), "S", n("Transparency")))
	groupForm.Set("Resources", d())

	content := `q
/GS0 gs
1 0 0 rg
50 700 100 50 re f
0 0 0 1 k
50 640 100 50 re f
0.5 g
50 620 100 10 re f
BT /TT1 12 Tf 50 590 Td (AB) Tj ET
q 100 0 0 60 50 500 cm /Im1 Do Q
q 100 0 0 60 200 500 cm /Im2 Do Q
q 1 0 0 1 50 400 cm /FmRef Do Q
q 1 0 0 1 200 400 cm /FmGrp Do Q
Q
`

	// Page-level output intents (ISO 32000-2, 14.11.5), a PDF 2.0 construct that
	// earlier PDF/A parts have no place for. Neither entry carries a destination
	// profile, so none of them is a PDF/A output intent and none of the ISO
	// 19005-4, 6.2.3 tests has anything to reject: a firing here would be a false
	// positive on a legal PDF 2.0 construct.
	outputIntent := func(condition string) *pdfgen.Dict {
		return pdf.Indirect(d(
			"Type", n("OutputIntent"),
			"S", n("/GTS_PDFA1"),
			"OutputConditionIdentifier", text(condition),
			"RegistryName", text("http://www.color.org"),
		))
	}

	page := d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d(
			"Font", d("TT1", tt),
			"XObject", d(
				"Im1", imgInterp, "Im2", imgOK, "FmRef", refForm, "FmGrp", groupForm,
			),
			"ExtGState", d("GS0", gs),
		),
		"Contents", pdf.Stream([]byte(content)),
		"OutputIntents", a(outputIntent("CGATS TR 001"), outputIntent("FOGRA39")),
		// ISO 19005-4, 6.11 bans /PresSteps on a page.
		"PresSteps", d("Type", n("NavNode")),
	)
	pdf.AddPage(pdf.Indirect(page))

	// Movie is not in the ISO 32000-2 annotation table, so 6.3.1 rejects it.
	movie := d(
		"Type", n("Annot"),
		"Subtype", n("Movie"),
		"Rect", a(400, 700, 500, 750),
		"F", 4,
		"T", text("clip"),
		"Movie", d("F", text("clip.mov")),
	)
	// Control: correct flags, an appearance stream, an allowed subtype. Must stay
	// silent while the Movie annotation fires.
	okAP := pdf.Stream([]byte("0 0 20 20 re f\n"))
	okAP.Set("Type", n("XObject"))
	okAP.Set("Subtype", n("Form"))
	okAP.Set("BBox", a(0, 0, 20, 20))
	okAP.Set("Resources", d())
	okAnnot := d(
		"Type", n("Annot"),
		"Subtype", n("Square"),
		"Rect", a(400, 600, 420, 620),
		"F", 4,
		"AP", d("N", pdf.MakeIndirect(okAP)),
	)
	pdf.Page(0).Set("Annots", a(pdf.Indirect(movie), pdf.Indirect(okAnnot)))

	// Launch is one of the actions ISO 19005-4, 6.6.1 still bans; JavaScript is
	// not, which is a part-4 departure from parts 1 to 3.
	pdf.Root.Set("OpenAction", d(
		"Type", n("Action"), "S", n("Launch"), "F", text("helper.exe"),
	))
	pdf.Root.Set("NeedsRendering", true)
	// ISO 19005-4, 6.12 bans /Requirements outright.
	pdf.Root.Set("Requirements", a(
		d("Type", n("Requirement"), "S", n("/EnableJavaScripts")),
	))

	// Associated files. The catalog-level one is fully formed; the one in the
	// /EmbeddedFiles name tree carries no /AFRelationship, which ISO 19005-4,
	// 6.9 requires on every embedded file specification.
	afOK := filespec(pdf, "data.xml", []byte("<litho/>\n"), "/Data", "/text#2Fxml")
	afBad := filespec(pdf, "payload.txt", []byte("litho corpus\n"), "", "")
	pdf.Root.Set("AF", a(afOK))
	pdf.Root.Set("Names", d(
		"EmbeddedFiles", d("Names", a(text("payload.txt"), afBad)),
	))

	// Optional content configuration with no /Name (ISO 19005-4, 6.10).
	ocg := pdf.Indirect(d("Type", n("OCG"), "Name", text("layer")))
	pdf.Root.Set("OCProperties", d("OCGs", a(ocg), "D", d("Order", a())))

	// A document information dictionary is allowed only when the catalog carries
	// /PieceInfo, and then only with /ModDate (ISO 19005-4, 6.1.3). This one has
	// neither, so both tests fire. /ModDate is left out on purpose: supplying it
	// would drag in the separate docinfo-versus-XMP date consistency check and
	// entangle two otherwise independent defects.
	docinfo(pdf).Set("Title", text("litho corpus fixture"))

	// pdfaid:part is 4 (correct) but pdfaid:rev is 2019, which 6.7.3 rejects.
	attachXMP(pdf, xmpPacket(xmpOpts{Part: 4, Rev: 2019}))

	return save(pdf, "2.0")
}
