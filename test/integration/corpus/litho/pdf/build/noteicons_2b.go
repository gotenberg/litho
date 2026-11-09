package build

import (
	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("noteicons_2b", noteicons2b) }

// noteicons2b builds noteicons_2b.pdf: Text annotations with no appearance stream, one
// for each predefined icon name, at PDF/A-2b.
//
// A closed Text annotation appears as an icon (ISO 32000-1, 12.5.6.4), and Table
// 172 names the icons a viewer provides: Comment, Key, Note (the default), Help,
// NewParagraph, Paragraph and Insert. The renderer draws its own icon for each,
// 24 units square from the upper-left corner of /Rect, its background /C. Text
// annotations neither scale nor rotate with the page (12.5.6.4). The render
// goldens of the corpora held only Text annotations with no /Name and no /C, on
// unturned pages, so the render cell compared no named icon, no background and
// no upright icon on a turned page.
//
// Page 1, 200 by 200, holds the seven names, an unknown name (/Star, drawn as
// Note) and a Note with no /C and no /Name, all outside the centred quarter and
// the band across the middle, except a Note whose /Rect is 4 units square at
// (60, 136): its icon hangs to (84, 116), inside the quarter, which removes it.
// The Key's background is dark, so its symbol is white.
//
// Page 2, 200 by 200, turns 90 degrees clockwise. A Comment whose /Rect is 4
// units square at (60, 66) hangs its icon upright from (60, 70): in default user
// space it spans x 60 to 84 and y 70 to 94, inside the centred quarter, while
// the icon turned with the page would reach y 46, across its edge.
//
// Each annotation sets /F 4 and lacks the /AP that ISO 19005-2, 6.3.3 requires,
// so 6.3.3 t1 fires. The file declares no OutputIntent, and the /C arrays are
// the only colour it uses.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func noteicons2b() ([]byte, error) {
	pdf := pdfgen.New()

	note := func(rect *pdfgen.Array, name string, colour *pdfgen.Array) *pdfgen.Dict {
		dd := d("Type", n("Annot"), "Subtype", n("Text"), "Rect", rect, "F", 4)
		if name != "" {
			dd.Set("Name", n("/"+name))
		}
		if colour != nil {
			dd.Set("C", colour)
		}
		return pdf.Indirect(dd)
	}

	page1 := d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 200, 200),
		"Resources", d(),
		"Annots", a(
			note(a(10, 166, 34, 190), "Comment", a(1, 1, 0)),
			note(a(45, 166, 69, 190), "Key", a(0, 0, 0.6)),
			note(a(80, 166, 104, 190), "Note", a(0.7, 0.85, 1)),
			note(a(115, 166, 139, 190), "Help", a(0.6, 0.9, 0.6)),
			note(a(150, 166, 174, 190), "NewParagraph", a(1, 0.8, 0.6)),
			note(a(10, 10, 34, 34), "Paragraph", a(0.9, 0.9, 0.9)),
			note(a(45, 10, 69, 34), "Insert", a(1, 0.6, 0.6)),
			note(a(80, 10, 104, 34), "Star", a(1, 1, 0.6)),
			note(a(150, 10, 174, 34), "", nil),
			note(a(60, 136, 64, 140), "", a(0.8, 0.8, 1)),
		),
	)
	page2 := d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 200, 200),
		"Rotate", 90,
		"Resources", d(),
		"Annots", a(note(a(60, 66, 64, 70), "Comment", a(1, 0.6, 0.6))),
	)
	pdf.AddPage(pdf.Indirect(page1))
	pdf.AddPage(pdf.Indirect(page2))

	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))
	return save(pdf, "1.7")
}
