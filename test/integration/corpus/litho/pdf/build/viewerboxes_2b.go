package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("viewerboxes_2b", viewerboxes2b) }

// viewerboxes2b builds viewerboxes_2b.pdf: page boundaries that ISO 19005-2, 6.1.13 t11
// passes and pdf.js and poppler read as no box, which every rewrite writes as a
// viewer reads them, at PDF/A-2b.
//
// ISO 32000-1, 7.9.5 writes a rectangle as four numbers, and Table 30 gives each
// page boundary but the media box a default: the media box for the crop box, the
// crop box for the bleed, trim and art boxes. pdf.js and poppler read any other
// array as no box and draw the page on its default, US Letter for the media box.
// veraPDF measures a box on its first four entries, a missing one or one that is
// no number as 0, and passes every box below. A rewrite wrote each as that
// measured rectangle, which a viewer then drew the page by: page 1 became a page
// 290 by 10 points. pageboxes_2b.pdf holds boxes of these kinds that enclose no
// area once measured; no corpus file holds one that encloses an area, a media box
// holding a name, or a crop box a viewer uses on a media box it does not.
//
// | Page | Box        | As written          | A viewer reads  | Written as        |
// | ---- | ---------- | ------------------- | --------------- | ----------------- |
// | 1    | /MediaBox  | [10 10 300]         | US Letter       | US Letter         |
// | 2    | /CropBox   | [0 0 300 400 9]     | the media box   | none              |
// | 2    | /ArtBox    | [10 10 320]         | the crop box    | none              |
// | 3    | /MediaBox  | [0 /Foo 600 800]    | US Letter       | US Letter         |
// | 3    | /CropBox   | [0 0 700 900]       | cut to Letter   | the same          |
// | 4    | /CropBox   | [0 /Foo 300 400]    | the media box   | none              |
// | 4    | /TrimBox   | [0 0 310 410 9]     | the crop box    | none              |
// | 4    | /BleedBox  | [5 5 320]           | the crop box    | none              |
// | 5    | /CropBox   | node 2's, 5 entries | the media box   | the media box     |
//
// Pages 2 and 4 state /MediaBox [0 0 600 800] and page 5 [0 0 400 500]. Page 5
// sits under two nodes: the outer states /CropBox [0 0 200 300], the inner
// /CropBox [0 0 300 400 9]. pdf.js takes the nearest crop box and reads no box,
// so page 5 shows its media box; dropping the inner one would let the outer show
// through, so the page states its media box as its crop box instead. poppler
// passes over the inner box and shows the outer one: the two viewers part, and
// litho reads the page as pdf.js does.
//
// The pinned pdf.js shows the pages at 612 by 792, 600 by 800, 612 by 792, 600
// by 800 and 400 by 500, and so does every rewrite of the file. No page draws
// anything, so the pages read the same under every mutation.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func viewerboxes2b() ([]byte, error) {
	pdf := pdfgen.New()

	root := pdf.Indirect(d("Type", n("Pages"), "Resources", d()))
	page1 := pdf.Indirect(
		d("Type", n("Page"), "Parent", root, "MediaBox", a(10, 10, 300)),
	)
	page2 := pdf.Indirect(d(
		"Type", n("Page"),
		"Parent", root,
		"MediaBox", a(0, 0, 600, 800),
		"CropBox", a(0, 0, 300, 400, 9),
		"ArtBox", a(10, 10, 320),
	))
	page3 := pdf.Indirect(d(
		"Type", n("Page"),
		"Parent", root,
		"MediaBox", a(0, n("/Foo"), 600, 800),
		"CropBox", a(0, 0, 700, 900),
	))
	page4 := pdf.Indirect(d(
		"Type", n("Page"),
		"Parent", root,
		"MediaBox", a(0, 0, 600, 800),
		"CropBox", a(0, n("/Foo"), 300, 400),
		"TrimBox", a(0, 0, 310, 410, 9),
		"BleedBox", a(5, 5, 320),
	))
	outer := pdf.Indirect(
		d("Type", n("Pages"), "Parent", root, "CropBox", a(0, 0, 200, 300)),
	)
	inner := pdf.Indirect(
		d("Type", n("Pages"), "Parent", outer, "CropBox", a(0, 0, 300, 400, 9)),
	)
	page5 := pdf.Indirect(
		d("Type", n("Page"), "Parent", inner, "MediaBox", a(0, 0, 400, 500)),
	)

	inner.Set("Kids", a(page5))
	inner.Set("Count", 1)
	outer.Set("Kids", a(inner))
	outer.Set("Count", 1)
	root.Set("Kids", a(page1, page2, page3, page4, outer))
	root.Set("Count", 5)

	// The tree is built by hand: pikepdf's page list flattens it, which would
	// delete the nodes the fixture exists for.
	pdf.Root.Set("Pages", root)

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))
	return save(pdf, "1.7")
}
