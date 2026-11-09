package build

import (
	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("complex_2b", complex2b) }

// complex2b builds complex_2b.pdf: a PDF/A-2b fixture packing many independent violations.
//
// Rule of this corpus: a firing rule must never hide a rule that should also
// fire. Each construct below is independent of the others, and two of them
// are deliberate MUST-NOT-FIRE controls guarding against over-suppression in
// litho's usage-scoping logic.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func complex2b() ([]byte, error) {
	pdf := pdfgen.New()

	// Type3 T3A: code 65 is rendered and its /Widths entry (500) contradicts the
	// CharProc d0 advance (800). Real mismatch, must fire.
	t3a := type3Font(pdf,
		[]type3Proc{{"gA", 800, "0 0 700 700 re f"}, {"gB", 800, "0 0 600 600 re f"}},
		[]any{500, 800}, 65)

	// Type3 T3B: code 66 is never rendered, so its skewed /Widths entry (100 vs a
	// d0 advance of 900) must stay silent. Guards the usage-scoping fix in
	// c55de28 against over-suppression: code 65 of the same font is consistent,
	// so a firing here would be a false positive.
	t3b := type3Font(pdf,
		[]type3Proc{{"gC", 600, "0 0 500 500 re f"}, {"gD", 900, "0 0 400 400 re f"}},
		[]any{600, 100}, 65)

	tt := unembeddedTrueType("/Helvetica")

	img := rgbImage(pdf, true)

	// LZWDecode is forbidden outright (ISO 19005-2, 6.1.7). The payload really is
	// LZW so the validator must reject the filter name, not a decode failure.
	// Stream.write with an explicit filter installs the pre-encoded bytes; the
	// plain Stream constructor would let pikepdf transcode them to Flate on save.
	lzwPayload := []byte("0 0 1 rg 5 5 20 20 re f\n")
	lzwForm := pdf.Stream([]byte{})
	lzwForm.Data = lzwEncode(lzwPayload, 1)
	lzwForm.Set("Filter", n("LZWDecode"))
	lzwForm.Set("Type", n("XObject"))
	lzwForm.Set("Subtype", n("Form"))
	lzwForm.Set("BBox", a(0, 0, 30, 30))
	lzwForm.Set("Resources", d())

	// Transparency group form XObject with no blending colour space.
	form := pdf.Stream([]byte("0 0 1 rg 0 0 50 50 re f\n"))
	form.Set("Type", n("XObject"))
	form.Set("Subtype", n("Form"))
	form.Set("BBox", a(0, 0, 50, 50))
	form.Set("Group", d("Type", n("Group"), "S", n("Transparency")))
	form.Set("Resources", d())

	// PostScript XObject: forbidden by ISO 19005-2, 6.2.7.
	psxobj := pdf.Stream([]byte("%!PS\n"))
	psxobj.Set("Type", n("XObject"))
	psxobj.Set("Subtype", n("PS"))

	content := `q
/BadIntent ri
1 0 0 rg
50 700 100 50 re f
0 0 0 1 k
50 640 100 50 re f
0.5 g
50 620 100 10 re f
BT /T3A 12 Tf 50 600 Td (AB) Tj ET
BT /T3B 12 Tf 50 570 Td (A) Tj ET
BT /TT1 12 Tf 50 540 Td (AB) Tj ET
q 200 0 0 100 50 400 cm /Im1 Do Q
q 1 0 0 1 50 300 cm /Fm1 Do Q
q 1 0 0 1 150 300 cm /FmLZW Do Q
q 1 0 0 1 250 300 cm /PSX Do Q
Q
`

	page := d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d(
			"Font", d(
				"T3A", pdf.Indirect(t3a),
				"T3B", pdf.Indirect(t3b),
				"TT1", pdf.Indirect(tt),
			),
			"XObject", d(
				"Im1", img, "Fm1", form, "FmLZW", lzwForm, "PSX", pdf.MakeIndirect(psxobj),
			),
		),
		"Contents", pdf.Stream([]byte(content)),
	)
	pdf.AddPage(pdf.Indirect(page))

	// Forbidden annotation type plus an annotation with no appearance stream.
	movie := d(
		"Type", n("Annot"),
		"Subtype", n("Movie"),
		"Rect", a(400, 700, 500, 750),
		"F", 4,
		"T", text("clip"),
		"Movie", d("F", text("clip.mov")),
	)
	square := d(
		"Type", n("Annot"),
		"Subtype", n("Square"),
		"Rect", a(400, 600, 500, 650),
		"F", 4,
		"C", a(1, 0, 0),
	)
	// Hidden annotation: ISO 19005-2, 6.3.2 requires Print set and Hidden clear,
	// so the flags are the defect. It still owes an appearance stream.
	hidden := d(
		"Type", n("Annot"),
		"Subtype", n("Square"),
		"Rect", a(400, 500, 500, 550),
		"F", 2,
		"C", a(0, 1, 0),
	)
	// Zero-area annotation with no appearance stream. ISO 19005-2, 6.3.3 exempts
	// annotations whose Rect collapses to a point, so this must stay silent while
	// the three above fire. Guards the exemption against under-scoping.
	zeroArea := d(
		"Type", n("Annot"),
		"Subtype", n("Square"),
		"Rect", a(400, 300, 400, 300),
		"F", 4,
	)
	pdf.Page(0).Set("Annots", a(
		pdf.Indirect(movie),
		pdf.Indirect(square),
		pdf.Indirect(hidden),
		pdf.Indirect(zeroArea),
	))

	// Catalog-level violations: a JavaScript OpenAction, /NeedsRendering, an
	// embedded file, and an optional content group with no /Name.
	pdf.Root.Set("OpenAction", d("Type", n("Action"), "S", n("JavaScript"), "JS", text("app.alert('litho');")))
	pdf.Root.Set("NeedsRendering", true)
	pdf.Root.Set("Names", d(
		"EmbeddedFiles", d(
			"Names", a(text("payload.txt"), filespec(pdf, "payload.txt", []byte("litho corpus\n"), "", "")),
		),
	))
	ocg := pdf.Indirect(d("Type", n("OCG")))
	pdf.Root.Set("OCProperties", d("OCGs", a(ocg), "D", d("Order", a(), "AS", a())))

	// Deliberately no /Metadata on the catalog and no /OutputIntents.
	return save(pdf, "1.7")
}
