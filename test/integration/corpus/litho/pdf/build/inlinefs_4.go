package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("inlinefs_4", inlinefs4) }

// inlinefs4 builds inlinefs_4.pdf: file specifications written inline, outside any /FS.
//
// ISO 32000-1, 7.11.3 does not require a file specification to be an indirect
// object, so one can sit wherever a value can. veraPDF judges a /Type /Filespec
// dictionary wherever it meets it, as it walks every value of every object, and
// names it by the object holding it. litho judged one only inside an annotation
// that is an object of its own, and its repairs reached an annotation's /FS
// alone: the media type, /UF and /AFRelationship repairs never saw the others,
// and the PDF/A-3 move to an object of its own walked annotations only.
//
// Four routes, each carrying a different set of defects, so a check or a repair
// that misses one route reports a count other than the one due:
//
// | Route     | Where the specification sits                     | Embeds            | Keys           |
// | --------- | ------------------------------------------------ | ----------------- | -------------- |
// | `LK1`     | the /AF of a Link that is an object of its own   | text, no /Subtype | /F only, no AR |
// | `LK2`     | the /AF of a Link written inline in /Annots      | text, /plain      | /F /UF AR      |
// | `Fm`      | the /AF of a form XObject the page lists         | nothing           | /F /UF, no AR  |
// | `catalog` | the catalog's /AF                                | nothing           | /F /UF AR      |
//
// AR is /AFRelationship. Neither embedded file is a PDF. At PDF/A-4, ISO
// 19005-4, 6.9 judges embedded files alone: t1 (media type) and t3 (not a PDF)
// fire on `LK1` and `LK2`, t2 (/UF) and t4 (/AFRelationship) on `LK1`. At
// PDF/A-3b, ISO 19005-3, 6.8 t1 fires on `LK1` and `LK2`, t2 on `LK1`, t3 on
// `LK1` and `Fm`, and t4 on all four: an /AF array associates only the indirect
// objects it lists, and veraPDF counts an inline one as associated with nothing,
// even one an /AF array lists inline.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func inlinefs4() ([]byte, error) {
	pdf := pdfgen.New()

	rect := a(10, 10, 30, 30)

	// embedded is an embedded file stream, with /Subtype only when subtype is
	// given.
	embedded := func(data, subtype string) *pdfgen.Stream {
		s := pdf.Stream([]byte(data))
		s.Set("Type", n("EmbeddedFile"))
		if subtype != "" {
			s.Set("Subtype", n(subtype))
		}
		pdf.MakeIndirect(s)
		return s
	}

	// appearance is an empty normal appearance the size of the annotation.
	appearance := func() *pdfgen.Dict {
		ap := pdf.Stream([]byte(""))
		ap.Set("Type", n("XObject"))
		ap.Set("Subtype", n("Form"))
		ap.Set("BBox", a(0, 0, 20, 20))
		return d("N", pdf.MakeIndirect(ap))
	}

	// link is a printable Link with an appearance whose /AF lists spec inline.
	link := func(spec *pdfgen.Dict, contents string) *pdfgen.Dict {
		return d(
			"Type", n("Annot"),
			"Subtype", n("Link"),
			"Rect", rect,
			"F", 4,
			"Border", a(0, 0, 0),
			"Contents", text(contents),
			"AP", appearance(),
			"AF", a(spec),
		)
	}

	lk1 := d("Type", n("Filespec"), "F", text("lk1.txt"), "EF", d("F", embedded("lk1\n", "")))
	lk2 := d(
		"Type", n("Filespec"),
		"F", text("lk2.txt"),
		"UF", text("lk2.txt"),
		"AFRelationship", n("Data"),
		"EF", d("F", embedded("lk2\n", "/plain")),
	)

	form := pdf.Stream([]byte(""))
	form.Set("Type", n("XObject"))
	form.Set("Subtype", n("Form"))
	form.Set("BBox", a(0, 0, 1, 1))
	form.Set("AF", a(d("Type", n("Filespec"), "F", text("fm.txt"), "UF", text("fm.txt"))))

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 200, 200),
		// The page lists the form without drawing it: veraPDF judges a file
		// specification wherever it sits, and a drawn form would bring the
		// content rules of the PDF/UA levels in with it.
		"Resources", d("XObject", d("Fm", pdf.MakeIndirect(form))),
		// LK1 is an object of its own; LK2 is written inline in /Annots.
		"Annots", a(pdf.Indirect(link(lk1, "lk1")), link(lk2, "lk2")),
	))
	pdf.AddPage(page)

	pdf.Root.Set("AF", a(
		d(
			"Type", n("Filespec"),
			"F", text("cat.txt"),
			"UF", text("cat.txt"),
			"AFRelationship", n("Supplement"),
		),
	))
	attachXMP(pdf, xmpPacket(xmpOpts{Part: 4, Rev: 2020}))

	// Deliberately no output intent: nothing on the page paints colour.
	return save(pdf, "2.0")
}
