package build

import (
	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("formreuse_ua1", formreuseUa1) }

// formreuseUa1 builds formreuse_ua1.pdf: marked forms a cell, a glyph, a form or an appearance references again.
//
// ISO 14289-1, 7.20 has a form XObject whose content holds MCIDs referenced once:
// a form's /StructParents names the one parent tree entry its marked-content
// sequences resolve through (ISO 32000-1, Table 95 and 14.7.4.4), which a second
// reference paints again under the same parents. veraPDF 1.30.2 states test 2
// on the form object a Do or an appearance entry builds: a form carrying
// /StructParents passes at its first reference and fails at every later one. A content stream
// is parsed once for the document, so a Do counts once however many pages or
// streams draw the stream holding it, and an appearance entry counts once per
// annotation. litho counted the Do operators of the pages' own content alone. No
// corpus file references a marked form twice at PDF/UA-1, so this fixture plants
// one path per page, each with a marked form of its own, and every path but the
// page's own content:
//
// | Page | Path                                                 | Answers at PDF/UA-1 |
// | ---- | ---------------------------------------------------- | ------------------- |
// | 1    | the page and a tiling cell it selects, `/F Do` each  | `7.20/2` once       |
// | 2    | the page and a Type 3 glyph it shows, `/F Do` each   | `7.20/2` once       |
// | 3    | the page and a form it draws, `/F Do` each           | `7.20/2` once       |
// | 4    | the normal appearance of two annotations             | `7.20/2` once       |
// | 5    | one annotation's normal and down appearance          | `7.20/2` once       |
// | 6    | the page's `/F Do` and an annotation's appearance    | `7.20/2` once       |
// | 7    | a form, a cell and a glyph drawn twice, each `/F Do` | nothing             |
//
// Page 7 is the must-not-fire control: it draws twice a form that draws a marked
// form once, selects twice a cell that draws another, shows twice a glyph that
// draws a third, and draws twice a form without /StructParents and a form
// carrying /StructParent.
//
// A CMYK OutputIntent covers the DeviceGray every stream paints in.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func formreuseUa1() ([]byte, error) {
	pdf := pdfgen.New()

	destination := pdf.Stream(iccMinimalProfile("prtr", "CMYK", iccVersion21, "OUTI"))
	destination.Set("N", 4)
	pdf.Root.Set("OutputIntents", a(
		pdf.Indirect(d(
			"Type", n("OutputIntent"),
			"S", n("GTS_PDFA1"),
			"OutputConditionIdentifier", text("litho destination"),
			"DestOutputProfile", pdf.MakeIndirect(destination),
		)),
	))

	const markedOps = "/P <</MCID 0>> BDC 0 g 0 0 10 10 re f EMC\n"

	// form is a form XObject; key names the structure entry it carries, if any.
	form := func(content string, resources *pdfgen.Dict, key string) pdfgen.Object {
		xobj := pdf.Stream([]byte(content))
		xobj.Set("Type", n("XObject"))
		xobj.Set("Subtype", n("Form"))
		xobj.Set("BBox", a(0, 0, 20, 20))
		if resources != nil {
			xobj.Set("Resources", resources)
		} else {
			xobj.Set("Resources", d())
		}
		if key != "" {
			xobj.Set(key, 0)
		}
		return pdf.MakeIndirect(xobj)
	}

	// marked is a form whose content is one marked-content sequence with an MCID.
	marked := func() pdfgen.Object {
		return form(markedOps, nil, "StructParents")
	}

	// tiling is a coloured tiling pattern carrying its own /Resources.
	tiling := func(content string, resources *pdfgen.Dict) pdfgen.Object {
		pat := pdf.Stream([]byte(content))
		pat.Set("Type", n("Pattern"))
		pat.Set("PatternType", 1)
		pat.Set("PaintType", 1)
		pat.Set("TilingType", 1)
		pat.Set("BBox", a(0, 0, 20, 20))
		pat.Set("XStep", 20)
		pat.Set("YStep", 20)
		pat.Set("Resources", resources)
		return pdf.MakeIndirect(pat)
	}

	// type3 is a one-glyph Type 3 font; the glyph sets its colour (d0).
	type3 := func(glyph string, resources *pdfgen.Dict) *pdfgen.Dict {
		proc := pdf.MakeIndirect(pdf.Stream([]byte("1000 0 d0\n" + glyph)))
		return pdf.Indirect(d(
			"Type", n("Font"),
			"Subtype", n("Type3"),
			"FontBBox", a(0, 0, 1000, 1000),
			"FontMatrix", a(0.001, 0, 0, 0.001, 0, 0),
			"CharProcs", d("A", proc),
			"Encoding", d("Type", n("Encoding"), "Differences", a(65, n("A"))),
			"FirstChar", 65,
			"LastChar", 65,
			"Widths", a(1000),
			"Resources", resources,
		))
	}

	annot := func(appearances *pdfgen.Dict) *pdfgen.Dict {
		return pdf.Indirect(d(
			"Type", n("Annot"),
			"Subtype", n("Square"),
			"Rect", a(0, 0, 20, 20),
			"F", 4,
			"Contents", text("A square"),
			"AP", appearances,
		))
	}

	const (
		show  = "q 0 g BT /T3 10 Tf 1 1 Td (A) Tj ET Q\n"
		paint = "q /Pattern cs /P1 scn 0 0 100 100 re f Q\n"
		draw  = "/F Do\n"
		plain = "0 g 0 0 10 10 re f\n"
	)

	addPage := func(content string, resources *pdfgen.Dict, annots ...any) {
		page := d(
			"Type", n("Page"),
			"MediaBox", a(0, 0, 200, 200),
			"Resources", resources,
			"Contents", pdf.MakeIndirect(pdf.Stream([]byte(content))),
		)
		if annots != nil {
			page.Set("Annots", a(annots...))
		}
		pdf.AddPage(pdf.Indirect(page))
	}

	draws := func(f pdfgen.Object) *pdfgen.Dict {
		return d("XObject", d("F", f))
	}

	f1 := marked()
	addPage(draw+paint, d("XObject", d("F", f1), "Pattern", d("P1", tiling(draw, draws(f1)))))

	f2 := marked()
	addPage(show+draw, d("XObject", d("F", f2), "Font", d("T3", type3(draw, draws(f2)))))

	f3 := marked()
	addPage("/G Do\n"+draw, d("XObject", d("F", f3, "G", form(draw, draws(f3), ""))))

	f4 := marked()
	addPage(plain, d(), annot(d("N", f4)), annot(d("N", f4)))

	f5 := marked()
	addPage(plain, d(), annot(d("N", f5, "D", f5)))

	f6 := marked()
	addPage(draw, draws(f6), annot(d("N", f6)))

	addPage(
		"/G Do /G Do\n"+paint+paint+show+show+"/N Do /N Do /S Do /S Do\n",
		d(
			"XObject", d(
				"G", form(draw, draws(marked()), ""),
				"N", form(markedOps, nil, ""),
				"S", form(markedOps, nil, "StructParent"),
			),
			"Pattern", d("P1", tiling(draw, draws(marked()))),
			"Font", d("T3", type3(draw, draws(marked()))),
		),
	)

	attachXMP(pdf, xmpPacket(xmpOpts{UAPart: 1}))
	return save(pdf, "1.7")
}
