package build

import (
	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("megafont_2b", megafont2b) }

// megafont2b builds megafont2b.pdf: a deliberately complex PDF/A-2b fixture packing MANY
// independent violations plus several "must NOT fire" constructs that exercise the
// usage-scoping guards added by the recent font-width fixes.
//
// Rule 2 of the corpus task: a firing rule must not hide an unmatched rule.
// Specifically this checks that the usage-scoping guards (unrendered glyphs skipped)
// do not suppress a TRUE positive on a rendered glyph of the SAME font, and that
// each violation class is reported independently.
//
// Intended firings (litho and veraPDF must agree exactly):
//
//	A. Type3 font T3A: RENDERED code 65 width mismatch          -> 6.2.11.5
//	B. Type3 font T3B: UNRENDERED code 66 width mismatch        -> must NOT fire
//	C. simple TrueType font, not embedded                        -> 6.2.11.4.1
//	D. DeviceRGB fill with no OutputIntent                       -> 6.2.4.3
//	E. DeviceCMYK fill with no OutputIntent                      -> 6.2.4.3
//	F. invalid rendering intent name in content                  -> 6.2.6
//	G. /Interpolate true on an image                             -> 6.2.8.3
//	H. transparency group with no blending colour space          -> 6.2.10
//	I. no XMP metadata on the catalog                            -> 6.6.4
//	J. /Movie annotation (forbidden annotation type)             -> 6.3.2
func megafont2b() ([]byte, error) {
	pdf := pdfgen.New()

	// --- Type3 font T3A: code 65 IS rendered, its /Widths entry disagrees with d0.
	// CharProc declares an advance of 800; /Widths says 500 -> real mismatch.
	t3aA := pdf.Stream([]byte("800 0 d0\n0 0 700 700 re f\n"))
	t3aB := pdf.Stream([]byte("800 0 d0\n0 0 600 600 re f\n"))
	t3a := d(
		"Type", n("Font"),
		"Subtype", n("Type3"),
		"FontBBox", a(0, 0, 1000, 1000),
		"FontMatrix", a(0.001, 0, 0, 0.001, 0, 0),
		"CharProcs", d("gA", t3aA, "gB", t3aB),
		"Encoding", d(
			"Type", n("Encoding"), "Differences", a(65, n("/gA"), 66, n("/gB")),
		),
		"FirstChar", 65,
		"LastChar", 66,
		// code 65 rendered -> 500 vs d0 800 = MISMATCH (must fire)
		// code 66 rendered -> 600 matches d0? no: d0 is 800 too. Keep 66 consistent
		// by rendering it with a matching width so only 65 is the violation.
		"Widths", a(500, 800),
		"Resources", d(),
	)

	// --- Type3 font T3B: code 66 is NEVER rendered but its /Widths disagrees.
	// Must NOT fire (usage-scoping guard, commit c55de28).
	t3bA := pdf.Stream([]byte("600 0 d0\n0 0 500 500 re f\n"))
	t3bB := pdf.Stream([]byte("900 0 d0\n0 0 400 400 re f\n"))
	t3b := d(
		"Type", n("Font"),
		"Subtype", n("Type3"),
		"FontBBox", a(0, 0, 1000, 1000),
		"FontMatrix", a(0.001, 0, 0, 0.001, 0, 0),
		"CharProcs", d("gC", t3bA, "gD", t3bB),
		"Encoding", d(
			"Type", n("Encoding"), "Differences", a(65, n("/gC"), 66, n("/gD")),
		),
		"FirstChar", 65,
		"LastChar", 66,
		// code 65 rendered, 600 == d0 600 -> OK.
		// code 66 NOT rendered, 100 != d0 900 -> must be skipped.
		"Widths", a(600, 100),
		"Resources", d(),
	)

	// --- Simple TrueType font with no font program at all.
	tt := d(
		"Type", n("Font"),
		"Subtype", n("TrueType"),
		"BaseFont", n("/Helvetica"),
		"Encoding", n("WinAnsiEncoding"),
		"FirstChar", 65,
		"LastChar", 66,
		"Widths", a(722, 722),
		"FontDescriptor", d(
			"Type", n("FontDescriptor"),
			"FontName", n("/Helvetica"),
			"Flags", 32,
			"FontBBox", a(-166, -225, 1000, 931),
			"ItalicAngle", 0,
			"Ascent", 718,
			"Descent", -207,
			"CapHeight", 718,
			"StemV", 88,
			// deliberately no /FontFile2
		),
	)

	// --- An image with /Interpolate true, in DeviceRGB.
	raw := []byte{255, 0, 0, 0, 255, 0, 0, 0, 255, 255, 255, 0}
	img := pdf.Stream(zlibCompress(raw))
	img.Set("Type", n("XObject"))
	img.Set("Subtype", n("Image"))
	img.Set("Width", 2)
	img.Set("Height", 2)
	img.Set("ColorSpace", n("DeviceRGB"))
	img.Set("BitsPerComponent", 8)
	img.Set("Filter", n("FlateDecode"))
	img.Set("Interpolate", true)

	// --- A transparency-group form XObject with no /CS on the group.
	form := pdf.Stream([]byte("0 0 1 rg 0 0 50 50 re f\n"))
	form.Set("Type", n("XObject"))
	form.Set("Subtype", n("Form"))
	form.Set("BBox", a(0, 0, 50, 50))
	form.Set("Group", d("Type", n("Group"), "S", n("Transparency")))
	form.Set("Resources", d())

	content := `q
/BadIntent ri
1 0 0 rg
50 700 100 50 re f
0 0 0 1 k
50 640 100 50 re f
BT /T3A 12 Tf 50 600 Td (AB) Tj ET
BT /T3B 12 Tf 50 570 Td (A) Tj ET
BT /TT1 12 Tf 50 540 Td (AB) Tj ET
q 200 0 0 100 50 400 cm /Im1 Do Q
q 1 0 0 1 50 300 cm /Fm1 Do Q
` + "Q\n"

	page := d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d(
			"Font", d("T3A", pdf.Indirect(t3a), "T3B", pdf.Indirect(t3b), "TT1", pdf.Indirect(tt)),
			"XObject", d("Im1", img, "Fm1", form),
		),
		"Contents", pdf.Stream([]byte(content)),
	)
	pdf.AddPage(pdf.Indirect(page))

	// --- A forbidden /Movie annotation.
	movie := d(
		"Type", n("Annot"),
		"Subtype", n("Movie"),
		"Rect", a(400, 700, 500, 750),
		"F", 4,
		"T", text("clip"),
		"Movie", d("F", text("clip.mov")),
	)
	pdf.Page(0).Set("Annots", a(pdf.Indirect(movie)))

	// Deliberately no /Metadata on the catalog and no /OutputIntents.
	return save(pdf, "1.7")
}
