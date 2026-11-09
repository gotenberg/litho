package build

import (
	"fmt"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("namedmcid_ua1", namedmcidUa1) }

// namedmcidUa1 builds namedmcid_ua1.pdf: marked content naming its property list, and
// Links judged per page.
//
// A BDC operator carries its property list inline or names it from the
// /Properties of the current resources (ISO 32000-1, 14.6.2), and the /MCID that
// ties the content to its structure element sits in that list either way.
// veraPDF resolves the name through the stream's own resources and then the
// page's (PDResourcesHandler.getProperties), for a form XObject and an
// annotation appearance alike. litho read the /MCID and /Lang of an inline list
// alone, and resolved a form's names through the form's own resources alone. No
// corpus file names a list carrying an /MCID under a structure tree.
//
// The catalog carries no /Lang, and every structure element but two carries
// one, so a language finding comes from the construct that plants it. One page
// shows:
//
//   - text under /MC0, which the page's /Properties maps to /MCID 0;
//   - form /Fm1, whose text sits under /MC1 from its own /Properties;
//   - form /Fm2, whose text sits under /MC2, which only the page defines;
//   - text under /MC4, /MCID 2, owned by an element with no /Lang: 7.2 t34;
//   - text under /MC5, /MCID 3, whose list carries the /Lang its element lacks;
//   - form /Fm3, showing a glyph its /ToUnicode maps to U+E000 under /MC6, which
//     only the page defines and which carries /ActualText;
//   - form /Fm5, painting a square in a /Span under /MC8, which only the page
//     defines and which carries /ActualText and no /Lang, inside /MC9, whose
//     /MCID names an element with no /Lang: 7.2 t30;
//   - three unmapped /MCID shapes veraPDF passes: /MCID 9 inside the mapped
//     /MCID 1, /MCID 8 wrapping nothing, and /MCID 7 wrapping only an artifact.
//
// Nothing is left untagged, so 7.1 test 3 and ISO 14289-2, 8.2.2 test 1 stay
// silent. A second page shares the first's /Annots array, an object of its own
// holding one Link written inline with no /Contents: veraPDF builds that Link
// once per page, so 7.18.5 test 2 counts it twice. The XMP packet claims
// PDF/UA-1, and the font is not embedded.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func namedmcidUa1() ([]byte, error) {
	pdf := pdfgen.New()

	lang := text("en-US")

	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("Type1"),
		"BaseFont", n("/Helvetica"),
		"Encoding", n("WinAnsiEncoding"),
	))
	// A code the /ToUnicode map sends into the Private Use Area (ISO 14289-2,
	// 8.4.3), which only an /ActualText covers.
	puaFont := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("Type1"),
		"BaseFont", n("/Helvetica"),
		"Encoding", n("WinAnsiEncoding"),
		"ToUnicode", toUnicodeCMap(pdf, map[int]int{0x78: 0xE000}),
	))

	// show is the Python builder's text(y, s, face).
	show := func(y int, s string, face ...string) string {
		f := "F1"
		if len(face) > 0 {
			f = face[0]
		}
		return fmt.Sprintf("BT /%s 12 Tf 50 %d Td (%s) Tj ET\n", f, y, s)
	}

	form := func(content string, resources *pdfgen.Dict, structParents int) *pdfgen.Stream {
		s := pdf.Stream([]byte(content))
		s.Set("Type", n("XObject"))
		s.Set("Subtype", n("Form"))
		s.Set("BBox", a(0, 0, 612, 792))
		s.Set("Resources", resources)
		s.Set("StructParents", structParents)
		pdf.MakeIndirect(s)
		return s
	}

	fonts := d("F1", font, "F2", puaFont)
	fm1 := form(
		"/P /MC1 BDC\n"+show(700, "a form naming its own list")+"EMC\n",
		d("Font", fonts, "Properties", d("MC1", d("MCID", 0))),
		1,
	)
	fm2 := form("/P /MC2 BDC\n"+show(680, "a form naming the page list")+"EMC\n", d("Font", fonts), 2)
	fm3 := form("/Span /MC6 BDC\n"+show(660, "x", "F2")+"EMC\n", d("Font", fonts), 3)
	fm5 := form("/P /MC9 BDC\n/Span /MC8 BDC\n50 600 10 10 re f\nEMC\nEMC\n", d("Font", fonts), 5)

	content := "/P /MC0 BDC\n" +
		show(740, "the page naming its list") +
		"EMC\n" +
		"q /Fm1 Do Q\nq /Fm2 Do Q\nq /Fm3 Do Q\nq /Fm5 Do Q\n" +
		"/P /MC4 BDC\n" + show(560, "owned by an element with no language") + "EMC\n" +
		"/P /MC5 BDC\n" + show(540, "its list names the language") + "EMC\n" +
		"/P <</MCID 1>> BDC\n/Span <</MCID 9>> BDC\n" + show(520, "inside a mapped sequence") + "EMC\nEMC\n" +
		"/P <</MCID 8>> BDC\nEMC\n" +
		"/P <</MCID 7>> BDC\n/Artifact <</Lang (en-US)>> BDC\n" + show(500, "an artifact") + "EMC\nEMC\n"

	properties := d(
		"MC0", d("MCID", 0),
		"MC2", d("MCID", 0),
		"MC4", d("MCID", 2),
		"MC5", d("MCID", 3, "Lang", lang),
		"MC6", d("MCID", 0, "ActualText", text("private")),
		"MC8", d("ActualText", text("a square")),
		"MC9", d("MCID", 0),
	)

	link := d(
		"Type", n("Annot"),
		"Subtype", n("Link"),
		"Rect", a(50, 400, 200, 420),
		"Border", a(0, 0, 0),
		"A", d("S", n("URI"), "URI", text("https://example.com/")),
	)
	annots := pdf.MakeIndirect(a(link))

	page1 := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d(
			"Font", fonts,
			"XObject", d("Fm1", fm1, "Fm2", fm2, "Fm3", fm3, "Fm5", fm5),
			"Properties", properties,
		),
		"Contents", pdf.Stream([]byte(content)),
		"StructParents", 0,
		"Tabs", n("S"),
		"Annots", annots,
	))
	pdf.AddPage(page1)
	page2 := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d(),
		"Tabs", n("S"),
		"Annots", annots,
	))
	pdf.AddPage(page2)
	page1 = pdf.Page(0)

	structRoot := pdf.Indirect(d("Type", n("StructTreeRoot")))
	document := pdf.Indirect(d("Type", n("StructElem"), "S", n("Document"), "P", structRoot))

	elem := func(k any, withLang bool) *pdfgen.Dict {
		dd := d("Type", n("StructElem"), "S", n("P"), "P", document, "Pg", page1, "K", k)
		if withLang {
			dd.Set("Lang", lang)
		}
		return pdf.Indirect(dd)
	}

	mcr := func(stm *pdfgen.Stream) *pdfgen.Dict {
		return d("Type", n("MCR"), "MCID", 0, "Stm", stm)
	}

	pageP0 := elem(0, true)
	pageP1 := elem(1, true)
	nolang := elem(2, false)
	listlang := elem(3, false)
	formP1 := elem(mcr(fm1), true)
	formP2 := elem(mcr(fm2), true)
	formP3 := elem(mcr(fm3), true)
	formP5 := elem(mcr(fm5), false)
	document.Set("K", a(pageP0, pageP1, nolang, listlang, formP1, formP2, formP3, formP5))
	structRoot.Set("K", a(document))
	// The page maps MCIDs 0 to 3 and leaves 7, 8 and 9 unmapped; each form's
	// /StructParents keys its own entry, whose MCID 0 names its element.
	structRoot.Set("ParentTree", pdf.Indirect(d(
		"Nums", a(
			0,
			a(pageP0, pageP1, nolang, listlang),
			1,
			a(formP1),
			2,
			a(formP2),
			3,
			a(formP3),
			5,
			a(formP5),
		),
	)))
	structRoot.Set("ParentTreeNextKey", 6)

	pdf.Root.Set("StructTreeRoot", structRoot)
	pdf.Root.Set("MarkInfo", d("Marked", true))
	pdf.Root.Set("ViewerPreferences", d("DisplayDocTitle", true))
	attachXMP(pdf, xmpPacket(xmpOpts{UAPart: 1}))

	return save(pdf, "1.7")
}
