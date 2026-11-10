package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("complex_1b", complex1b) }

// complex1b builds complex_1b.pdf: a PDF/A-1b fixture built around part-1 prohibitions.
//
// ISO 19005-1 bans outright several constructs later parts allow: any
// transparency, embedded files, optional content, and the LZWDecode filter.
// Those sit alongside the profile-independent defects so a part-1 run has to
// report all of them at once.
//
// Two must-not-fire controls guard against over-suppression:
//   - a Type3 glyph whose /Widths entry is skewed but which is never rendered,
//   - a hidden annotation with no appearance stream, exempted by 6.5.3.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func complex1b() ([]byte, error) {
	pdf := pdfgen.New()

	t3a := type3Font(pdf,
		[]type3Proc{{"gA", 800, "0 0 700 700 re f"}, {"gB", 800, "0 0 600 600 re f"}},
		[]any{500, 800})
	t3b := type3Font(pdf,
		[]type3Proc{{"gC", 600, "0 0 500 500 re f"}, {"gD", 900, "0 0 400 400 re f"}},
		[]any{600, 100})
	tt := unembeddedTrueType()

	img := rgbImage(pdf, true)

	// Soft mask on an image: transparency, forbidden by ISO 19005-1, 6.4.
	smask := pdf.Stream([]byte{0, 128, 200, 255},
		"Type", n("XObject"), "Subtype", n("Image"), "Width", 2, "Height", 2,
		"ColorSpace", n("DeviceGray"), "BitsPerComponent", 8)
	pdf.MakeIndirect(smask)
	img.Set("SMask", smask)

	lzwForm := pdf.Stream(nil)
	lzwForm.Data = lzwEncode([]byte("0 0 1 rg 5 5 20 20 re f\n"), 1)
	lzwForm.Set("Filter", n("LZWDecode"))
	lzwForm.Set("Type", n("XObject"))
	lzwForm.Set("Subtype", n("Form"))
	lzwForm.Set("BBox", a(0, 0, 30, 30))
	lzwForm.Set("Resources", d())

	// Transparency group form XObject.
	groupForm := pdf.Stream([]byte("0 0 1 rg 0 0 50 50 re f\n"),
		"Type", n("XObject"), "Subtype", n("Form"), "BBox", a(0, 0, 50, 50),
		"Group", d("Type", n("Group"), "S", n("Transparency")), "Resources", d())

	// Reference XObject: forbidden by ISO 19005-1, 6.2.6.
	refForm := pdf.Stream([]byte("0 0 10 10 re f\n"),
		"Type", n("XObject"), "Subtype", n("Form"), "BBox", a(0, 0, 10, 10), "Resources", d(),
		"Ref", d("F", d("Type", n("Filespec"), "F", text("target.pdf")), "Page", 0))

	psxobj := pdf.Stream([]byte("%!PS\n"), "Type", n("XObject"), "Subtype", n("PS"))

	// Blend mode plus constant alpha: two independent transparency defects.
	gs := pdf.Indirect(d("Type", n("ExtGState"), "BM", n("Multiply"), "ca", 0.5, "CA", 0.4))

	content := `q
/BadIntent ri
/GS0 gs
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
q 1 0 0 1 250 300 cm /FmRef Do Q
q 1 0 0 1 350 300 cm /PSX Do Q
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
				"Im1", img,
				"Fm1", groupForm,
				"FmLZW", lzwForm,
				"FmRef", refForm,
				"PSX", pdf.MakeIndirect(psxobj),
			),
			"ExtGState", d("GS0", gs),
		),
		"Contents", pdf.Stream([]byte(content)),
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
	square := d(
		"Type", n("Annot"),
		"Subtype", n("Square"),
		"Rect", a(400, 600, 500, 650),
		"F", 4,
		"C", a(1, 0, 0),
	)
	// Hidden annotation: ISO 19005-1, 6.5.3 requires Print set and Hidden clear,
	// so the flags themselves are the defect here.
	hidden := d(
		"Type", n("Annot"),
		"Subtype", n("Square"),
		"Rect", a(400, 500, 500, 550),
		"F", 2,
		"C", a(0, 1, 0),
	)
	// Fully conforming annotation: correct flags, an appearance stream, no /C.
	// Must stay silent while the three above fire, proving the annotation rules
	// are evaluated per object rather than latched on the first failure.
	okAP := pdf.Stream([]byte("0 0 20 20 re f\n"),
		"Type", n("XObject"), "Subtype", n("Form"), "BBox", a(0, 0, 20, 20), "Resources", d())
	okAnnot := d(
		"Type", n("Annot"),
		"Subtype", n("Square"),
		"Rect", a(400, 400, 420, 420),
		"F", 4,
		"AP", d("N", pdf.MakeIndirect(okAP)),
	)
	pdf.Page(0).Set("Annots", a(
		pdf.Indirect(movie),
		pdf.Indirect(square),
		pdf.Indirect(hidden),
		pdf.Indirect(okAnnot),
	))

	pdf.Root.Set("OpenAction", d("Type", n("Action"), "S", n("JavaScript"), "JS", text("app.alert('litho');")))
	pdf.Root.Set("Names", d(
		"EmbeddedFiles", d(
			"Names", a(text("payload.txt"), filespec(pdf, "payload.txt", []byte("litho corpus\n"), "", "")),
		),
	))
	ocg := pdf.Indirect(d("Type", n("OCG"), "Name", text("layer")))
	pdf.Root.Set("OCProperties", d("OCGs", a(ocg), "D", d("Order", a())))

	// Deliberately no /Metadata on the catalog and no /OutputIntents.
	return save(pdf, "1.4")
}
