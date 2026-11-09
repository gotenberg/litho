package build

import (
	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("noresources_4", noresources4) }

// noresources4 builds noresources_4.pdf: page content with no resource dictionary at all.
//
// ISO 32000-2, 7.7.3.3, Table 31 makes /Resources a required, inheritable entry
// of a page: a page that needs no resources states an empty dictionary. A page
// that states none, under a page tree that states none either, leaves its content
// stream with no resource dictionary to associate, which ISO 19005-4, 6.2.2
// requires of every content stream. veraPDF fails 6.2.2 t3 once for each such
// stream, whatever it paints, and names DeviceGray, the initial colour space it
// resolves through the missing dictionary. litho stated nothing for a stream
// that names no resource.
//
// | Page | /Contents                          | /Resources        | 6.2.2 t3 |
// | ---- | ---------------------------------- | ----------------- | -------- |
// | 1    | paints a rectangle in DeviceGray   | none              | once     |
// | 2    | an empty stream                    | none              | once     |
// | 3    | `q Q`                              | `null`            | once     |
// | 4    | `q Q`, one stream shared with 5    | none              | once     |
// | 5    | the stream page 4 shows            | none              | none     |
// | 6    | `q Q`                              | an empty dict     | none     |
// | 7    | none                               | none              | none     |
//
// Page 1 also fails 6.2.4.3 t4 (DeviceGray with no OutputIntent), which veraPDF
// still judges on a page with no resources. Page 2 carries a Square annotation
// whose appearance stream states no /Resources either: veraPDF gives a form
// XObject resources of its own even when it states none, so the appearance fails
// nothing. Pages 5 to 7 are the controls: a content stream is judged once however
// many pages show it, and a page with an empty dictionary or no content has
// nothing to fail.
//
// No stream names a resource, so 6.2.2 t3 reaches the rule set through the
// missing dictionaries alone, and a check that misses them drops the rule.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func noresources4() ([]byte, error) {
	pdf := pdfgen.New()

	addPage := func(contents, resources any) *pdfgen.Dict {
		dd := d("Type", n("Page"), "MediaBox", a(0, 0, 612, 792))
		if contents != nil {
			dd.Set("Contents", contents)
		}
		page := pdf.Indirect(dd)
		pdf.AddPage(page)
		// pikepdf adds a /Resources to a page on append; the pages that must state
		// none have it removed again.
		page.Del("Resources")
		if resources != nil {
			page.Set("Resources", resources)
		}
		return page
	}

	addPage(pdf.Stream([]byte("0 g 50 700 200 50 re f\n")), nil)

	page2 := addPage(pdf.Stream([]byte{}), nil)
	appearance := pdf.Stream([]byte("q Q\n"))
	appearance.Set("Type", n("XObject"))
	appearance.Set("Subtype", n("Form"))
	appearance.Set("BBox", a(0, 0, 100, 50))
	page2.Set("Annots", a(
		pdf.Indirect(d(
			"Type", n("Annot"),
			"Subtype", n("Square"),
			"Rect", a(100, 600, 200, 650),
			"F", 4,
			"Contents", text("an annotation whose appearance states no resources"),
			"AP", d("N", pdf.MakeIndirect(appearance)),
		)),
	))

	// /Resources null is patched in after the save: pikepdf drops a null value.
	addPage(pdf.Stream([]byte("q Q\n")), n("/LithoNullRes"))

	shared := pdf.MakeIndirect(pdf.Stream([]byte("q Q\n")))
	addPage(shared, nil)
	addPage(shared, nil)

	addPage(pdf.Stream([]byte("q Q\n")), d())

	addPage(nil, nil)

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 4, Rev: 2020}))
	data, err := save(pdf, "2.0")
	if err != nil {
		return nil, err
	}

	// Same length, so no offset moves.
	return patchOnce(data, []byte("/Resources /LithoNullRes"), []byte("/Resources null         "),
		"the null /Resources placeholder")
}
