package build

import (
	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("afref_3b", afref3b) }

// afref3b builds afref_3b.pdf: file specifications something refers to but no /AF lists.
//
// ISO 19005-3, 6.8 test 4 asks every file specification dictionary of a PDF/A-3
// file to be an associated file, and only an /AF array associates one (ISO
// 32000-2, 14.13). A reference from elsewhere names the file without stating its
// relationship to the document, whether the file is embedded or external and
// whether the dictionary is indirect or written inline. veraPDF reads it that
// way: isAssociatedFile holds only the objects /AF arrays list. Every file below
// is external, so no other 6.8 test applies to it, and each carries /F, /UF and
// /AFRelationship:
//
// | Annotation | Route to the file specification                    | 6.8/4  |
// | ---------- | -------------------------------------------------- | ------ |
// | `FA1`      | a FileAttachment `/FS` naming an indirect filespec | fires  |
// | `FA2`      | a FileAttachment `/FS` written inline              | fires  |
// | `LK`       | a Link's `/GoToR` action whose `/F` is inline      | fires  |
// | `RM`       | a RichMedia `/Assets` tree naming an indirect one  | fires  |
// | `FA3`      | a FileAttachment `/FS` the catalog `/AF` also lists | silent |
//
// Four specifications answer, so a check that accepts one route as an
// association, or stops at the first defect, reports a count other than four.
// `FA3` is the conforming case of the very route `FA1` breaks.
//
// The catalog also carries a `/Names /JavaScript` tree written as an empty
// dictionary. ISO 32000-1, 7.7.4 makes the tree a map of document-level
// JavaScript actions, and an empty one holds none, so no action-type rule
// (ISO 19005-1, 6.6.1 and ISO 19005-2/3, 6.5.1) may fire on it.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func afref3b() ([]byte, error) {
	pdf := pdfgen.New()

	rect := a(10, 10, 30, 30)

	// external is an external file specification, conforming in every key 6.8
	// reads.
	external := func(name string) *pdfgen.Dict {
		return d(
			"Type", n("Filespec"),
			"F", text(name),
			"UF", text(name),
			"AFRelationship", n("/Unspecified"),
		)
	}

	// appearance is an empty normal appearance the size of the annotation.
	appearance := func() *pdfgen.Dict {
		ap := pdf.Stream([]byte{})
		ap.Set("Type", n("XObject"))
		ap.Set("Subtype", n("Form"))
		ap.Set("BBox", a(0, 0, 20, 20))
		return d("N", pdf.MakeIndirect(ap))
	}

	// annot is a printable annotation with an appearance, carrying entries.
	annot := func(subtype string, entries ...any) *pdfgen.Dict {
		dd := d("Type", n("Annot"), "Subtype", n(subtype), "Rect", rect, "F", 4, "AP", appearance())
		for i := 0; i < len(entries); i += 2 {
			dd.Set(entries[i].(string), entries[i+1])
		}
		return pdf.Indirect(dd)
	}

	listed := pdf.Indirect(external("e.pdf"))
	annots := a(
		annot("/FileAttachment", "FS", pdf.Indirect(external("a.pdf"))),
		annot("/FileAttachment", "FS", external("b.pdf")),
		annot("/Link", "A", d("S", n("GoToR"), "F", external("c.pdf"), "D", a(0, n("Fit")))),
		annot(
			"/RichMedia",
			"RichMediaContent", pdf.Indirect(
				d(
					"Type", n("/RichMediaContent"),
					"Assets", d("Names", a(text("d.prc"), pdf.Indirect(external("d.prc")))),
				),
			),
		),
		annot("/FileAttachment", "FS", listed),
	)

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 200, 200),
		"Resources", d(),
		"Annots", annots,
	))
	pdf.AddPage(page)

	pdf.Root.Set("AF", a(listed))
	pdf.Root.Set("Names", d("JavaScript", pdf.Indirect(d())))
	attachXMP(pdf, xmpPacket(xmpOpts{Part: 3, Conformance: "B"}))

	// Deliberately no output intent: nothing on the page paints colour.
	return save(pdf, "1.7")
}
