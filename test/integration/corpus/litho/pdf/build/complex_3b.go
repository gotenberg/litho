package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("complex_3b", complex3b) }

// complex3b builds complex_3b.pdf: PDF/A-3b, embedded and associated files.
//
// Part 3 is the part that lets a conforming file carry arbitrary attachments,
// and it pays for that with four requirements on every embedded file
// specification: a real MIME type, both name keys, a declared relationship,
// and membership of an /AF array. ISO 19005-3 6.8 states them as four
// independent tests, so this fixture breaks each one in a separate file
// specification and leaves a fifth one fully conformant.
//
// Four must-not-fire controls guard against over-suppression:
//   - a fully conformant associated file, which no 6.8 test may touch,
//   - each defective file specification is correct in every respect except
//     the one it is there to break, so a rule that latches on the first bad
//     specification reports the wrong count,
//   - a conforming annotation with correct flags and an appearance stream.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func complex3b() ([]byte, error) {
	pdf := pdfgen.New()

	// attachment builds one embedded-file specification, defective in at most
	// one way. An empty mime or relationship is Python's None.
	attachment := func(name string, payload []byte, mime string, uf bool, relationship string) *pdfgen.Dict {
		ef := pdf.Stream(payload)
		ef.Set("Type", n("EmbeddedFile"))
		if mime != "" {
			ef.Set("Subtype", n(mime))
		}
		ef.Set("Params", d("Size", len(payload)))
		spec := d(
			"Type", n("Filespec"),
			"F", text(name),
			"EF", d("F", pdf.MakeIndirect(ef)),
			"Desc", text("litho corpus attachment"),
		)
		if uf {
			spec.Set("UF", text(name))
		}
		if relationship != "" {
			spec.Set("AFRelationship", n(relationship))
		}
		return pdf.Indirect(spec)
	}

	// One defect per specification, so the four ISO 19005-3 6.8 tests stay
	// independent of one another.
	noMime := attachment("no-mime.bin", []byte("litho corpus\n"), "", true, "/Data")                    // t1
	noUF := attachment("no-uf.txt", []byte("litho corpus\n"), "/text#2Fplain", false, "/Data")          // t2
	noRel := attachment("no-rel.txt", []byte("litho corpus\n"), "/text#2Fplain", true, "")              // t3
	notAssociated := attachment("orphan.txt", []byte("litho corpus\n"), "/text#2Fplain", true, "/Data") // t4
	// Control: correct in every respect, and reachable from /AF. No 6.8 test may
	// fire on it, so a firing is a false positive that the four above would hide.
	conformant := attachment("good.txt", []byte("litho corpus\n"), "/text#2Fplain", true, "/Data")

	tt := unembeddedTrueType("/Helvetica")
	img := rgbImage(pdf, true)

	lzwForm := pdf.Stream(nil)
	lzwForm.Data = lzwEncode([]byte("0 0 1 rg 5 5 20 20 re f\n"), 1)
	lzwForm.Set("Filter", n("LZWDecode"))
	lzwForm.Set("Type", n("XObject"))
	lzwForm.Set("Subtype", n("Form"))
	lzwForm.Set("BBox", a(0, 0, 30, 30))
	lzwForm.Set("Resources", d())

	groupForm := pdf.Stream([]byte("0 0 1 rg 0 0 50 50 re f\n"))
	groupForm.Set("Type", n("XObject"))
	groupForm.Set("Subtype", n("Form"))
	groupForm.Set("BBox", a(0, 0, 50, 50))
	groupForm.Set("Group", d("Type", n("Group"), "S", n("Transparency")))
	groupForm.Set("Resources", d())

	psxobj := pdf.Stream([]byte("%!PS\n"))
	psxobj.Set("Type", n("XObject"))
	psxobj.Set("Subtype", n("PS"))

	content := `q
/BadIntent ri
1 0 0 rg
50 700 100 50 re f
0 0 0 1 k
50 660 100 30 re f
0.5 g
50 640 100 10 re f
BT /TT1 12 Tf 50 610 Td (AB) Tj ET
q 100 0 0 60 50 520 cm /Im1 Do Q
q 1 0 0 1 50 430 cm /Fm1 Do Q
q 1 0 0 1 150 430 cm /FmLZW Do Q
q 1 0 0 1 250 430 cm /PSX Do Q
Q
`

	page := d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d(
			"Font", d("TT1", pdf.Indirect(tt)),
			"XObject", d(
				"Im1", img,
				"Fm1", groupForm,
				"FmLZW", lzwForm,
				"PSX", pdf.MakeIndirect(psxobj),
			),
		),
		"Contents", pdf.Stream([]byte(content)),
		// A page-level /AF array, which ISO 32000-2 14.13 allows and part 3
		// accepts: the specification it points at is fully conformant.
		"AF", a(conformant),
	)
	pdf.AddPage(pdf.Indirect(page))

	movie := d(
		"Type", n("Annot"),
		"Subtype", n("Movie"),
		"Rect", a(400, 700, 500, 750),
		"F", 4,
		"T", text("clip"),
		"Movie", d("F", text("clip.mov")),
	)
	hidden := d(
		"Type", n("Annot"),
		"Subtype", n("Square"),
		"Rect", a(400, 600, 500, 650),
		"F", 2,
		"C", a(0, 1, 0),
	)
	okAP := pdf.Stream([]byte("0 0 20 20 re f\n"))
	okAP.Set("Type", n("XObject"))
	okAP.Set("Subtype", n("Form"))
	okAP.Set("BBox", a(0, 0, 20, 20))
	okAP.Set("Resources", d())
	okAnnot := d(
		"Type", n("Annot"),
		"Subtype", n("Square"),
		"Rect", a(400, 500, 420, 520),
		"F", 4,
		"AP", d("N", pdf.MakeIndirect(okAP)),
	)
	pdf.Page(0).Set("Annots", a(
		pdf.Indirect(movie), pdf.Indirect(hidden), pdf.Indirect(okAnnot),
	))

	// Everything except orphan.txt is an associated file. orphan.txt is reachable
	// only through the /EmbeddedFiles name tree, which is exactly what 6.8 t4
	// rejects.
	pdf.Root.Set("AF", a(noMime, noUF, noRel))
	pdf.Root.Set("Names", d(
		"EmbeddedFiles", d(
			"Names", a(
				text("good.txt"),
				conformant,
				text("no-mime.bin"),
				noMime,
				text("no-rel.txt"),
				noRel,
				text("no-uf.txt"),
				noUF,
				text("orphan.txt"),
				notAssociated,
			),
		),
	))

	pdf.Root.Set("OpenAction", d(
		"Type", n("Action"), "S", n("JavaScript"), "JS", text("app.alert('litho');"),
	))
	pdf.Root.Set("NeedsRendering", true)
	ocg := pdf.Indirect(d("Type", n("OCG"), "Name", text("layer")))
	pdf.Root.Set("OCProperties", d(
		"OCGs", a(ocg), "D", d("Order", a(), "AS", a()),
	))

	// The conformance claim is correct: part 3, level B. The defects are in the
	// attachments, not in the claim.
	attachXMP(pdf, xmpPacket(xmpOpts{Part: 3, Conformance: "B"}))

	return save(pdf, "1.7")
}
