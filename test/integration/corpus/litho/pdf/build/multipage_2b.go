package build

import (
	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("multipage_2b", multipage2b) }

// multipage2b builds multipage_2b.pdf: a PDF/A-2b fixture spreading violations across pages.
//
// Every other fixture in this corpus is one page, so none of them can catch a
// page-scoped check that stops after the first page, or one that reports a
// document-level verdict from page one and never revisits. This fixture isolates
// that axis: each page carries a violation class that appears on no other page,
// so the reported rule set is only complete if every page was reached.
//
// Page 1 also carries a conforming counterpart of the constructs that fail on
// pages 2 and 3, so a check that scans only page 1 stays silent rather than
// firing, which makes the failure mode a missing rule rather than a wrong one.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func multipage2b() ([]byte, error) {
	pdf := pdfgen.New()

	font := pdf.Indirect(unembeddedTrueType())

	// Page 1 paints DeviceCMYK and nothing else. ISO 19005-2, 6.2.4.3 t3 needs a
	// CMYK output intent, and this file declares none.
	page1Content := pdf.Stream([]byte("0 0 0 1 k\n50 700 100 40 re f\n"))

	// Page 2 uses an invalid rendering intent (6.2.6 t1) and an image carrying
	// /Interpolate true (6.2.8 t3). Neither construct appears on page 1 or 3.
	imgInterp := rgbImage(pdf, true)
	page2Content := pdf.Stream([]byte(
		"q\n/BadIntent ri\n" +
			"q 200 0 0 100 50 400 cm /Im1 Do Q\n" +
			"Q\n",
	))

	// Page 3 draws a PostScript XObject (6.2.9 t3) and a transparency group with
	// no blending colour space (6.2.10 t2). Again unique to this page.
	psx := pdf.Stream([]byte("%!PS-Adobe-3.0\n0 0 moveto 10 10 lineto stroke\n"))
	psx.Set("Type", n("XObject"))
	psx.Set("Subtype", n("PS"))

	groupForm := pdf.Stream([]byte("0 0 1 rg\n0 0 50 50 re f\n"))
	groupForm.Set("Type", n("XObject"))
	groupForm.Set("Subtype", n("Form"))
	groupForm.Set("BBox", a(0, 0, 50, 50))
	groupForm.Set("Group", d("Type", n("Group"), "S", n("Transparency")))
	groupForm.Set("Resources", d())

	pdf.Stream([]byte(
		"q 1 0 0 1 50 420 cm /PSX Do Q\nq 1 0 0 1 50 300 cm /FmGrp Do Q\n",
	))

	// The font is referenced from page 3 only. ISO 19005-2, 6.2.11.4.1 t1 is
	// reported per rendered font, so a walk that stops early never sees it.
	page3Content := pdf.Stream([]byte(
		"q 1 0 0 1 50 420 cm /PSX Do Q\n" +
			"q 1 0 0 1 50 300 cm /FmGrp Do Q\n" +
			"BT /TT1 12 Tf 50 200 Td (AB) Tj ET\n",
	))

	pages := []struct {
		content   *pdfgen.Stream
		resources *pdfgen.Dict
	}{
		{page1Content, d()},
		{page2Content, d("XObject", d("Im1", imgInterp))},
		{
			page3Content,
			d(
				"XObject", d("PSX", psx, "FmGrp", groupForm),
				"Font", d("TT1", font),
			),
		},
	}

	for _, p := range pages {
		page := d(
			"Type", n("Page"),
			"MediaBox", a(0, 0, 612, 792),
			"Resources", p.resources,
			"Contents", p.content,
		)
		pdf.AddPage(pdf.Indirect(page))
	}

	// A conforming annotation on page 1: F=4, an appearance stream, no /C. It
	// guards against a page-scoped annotation check reporting the wrong page.
	pdf.Page(0).Set("Annots", a(
		pdf.Indirect(d(
			"Type", n("Annot"),
			"Subtype", n("Square"),
			"Rect", a(400, 700, 500, 750),
			"F", 4,
			"AP", d("N", pdf.Stream([]byte{})),
		)),
	))

	// A /Movie annotation on page 3 only: ISO 19005-2, 6.3.1 t1 forbids the type
	// and 6.3.3 t1 catches the absent appearance stream.
	pdf.Page(2).Set("Annots", a(
		pdf.Indirect(d(
			"Type", n("Annot"),
			"Subtype", n("Movie"),
			"Rect", a(400, 600, 500, 650),
			"F", 4,
			"T", text("clip"),
			"Movie", d("F", text("clip.mov")),
		)),
	))

	// Deliberately no /Metadata on the catalog and no /OutputIntents, so
	// ISO 19005-2, 6.6.2.1 t1 fires once at document level.
	return save(pdf, "1.7")
}
