package build

import (
	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("fontdict_2b", fontdict2b) }

// fontdict2b builds fontdict_2b.pdf: font dictionary keys and page box limits.
//
// A coverage scan showed the corpus never reached ISO 19005-2, 6.2.11.2 (the
// required keys of a font dictionary) or 6.1.13 (implementation limits). Both
// are authorable without a font program, so they cost nothing to plant.
//
// The interesting part is the shape rather than the count. Four fonts are drawn
// on the same page and each carries a *different* defect, so a check that
// reports the first offending font and stops reports one rule where four are
// due. A fifth, fully conforming font is drawn alongside them, so
// over-suppression is visible too.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func fontdict2b() ([]byte, error) {
	pdf := pdfgen.New()

	descriptor := func(name string) *pdfgen.Dict {
		return d(
			"Type", n("FontDescriptor"),
			"FontName", n("/"+name),
			"Flags", 32,
			"FontBBox", a(-166, -225, 1000, 931),
			"ItalicAngle", 0,
			"Ascent", 718,
			"Descent", -207,
			"CapHeight", 718,
			"StemV", 88,
		)
	}

	// simpleFont is a TrueType font; first, last and widths nil leave their
	// keys out.
	simpleFont := func(name string, first, last any, widths []int, base bool) *pdfgen.Dict {
		dd := d("Type", n("Font"), "Subtype", n("TrueType"), "Encoding", n("WinAnsiEncoding"))
		if base {
			dd.Set("BaseFont", n("/"+name))
			dd.Set("FontDescriptor", pdf.Indirect(descriptor(name)))
		}
		if first != nil {
			dd.Set("FirstChar", first)
		}
		if last != nil {
			dd.Set("LastChar", last)
		}
		if widths != nil {
			dd.Set("Widths", widths)
		}
		return pdf.Indirect(dd)
	}

	// F1 omits /FirstChar (ISO 19005-2, 6.2.11.2 t4). The font is not one of the
	// standard 14, so the exemption does not apply.
	f1 := simpleFont("LithoAlpha", nil, 66, []int{722, 722}, true)

	// F2 omits /LastChar (t5).
	f2 := simpleFont("LithoBeta", 65, nil, []int{722, 722}, true)

	// F3 declares three codes but supplies two widths (t6).
	f3 := simpleFont("LithoGamma", 65, 67, []int{722, 722}, true)

	// F4 omits /BaseFont entirely (t3).
	f4 := simpleFont("LithoDelta", 65, 66, []int{722, 722}, false)

	// F5 conforms in every respect. A firing here would be a false positive, and
	// its silence shows the check did not latch on to the first offender.
	f5 := simpleFont("LithoEpsilon", 65, 66, []int{722, 722}, true)

	content := `BT /F1 12 Tf 50 700 Td (AB) Tj ET
BT /F2 12 Tf 50 670 Td (AB) Tj ET
BT /F3 12 Tf 50 640 Td (AB) Tj ET
BT /F4 12 Tf 50 610 Td (AB) Tj ET
BT /F5 12 Tf 50 580 Td (AB) Tj ET
`

	page1 := d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d("Font", d("F1", f1, "F2", f2, "F3", f3, "F4", f4, "F5", f5)),
		"Contents", pdf.Stream([]byte(content)),
	)
	pdf.AddPage(pdf.Indirect(page1))

	// Page 2 is two units square. ISO 19005-2, 6.1.13 t11 requires every page box
	// edge to span at least 3 and at most 14400 units, so both edges fail.
	page2 := d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 2, 2),
		"Resources", d(),
		"Contents", pdf.Stream([]byte("0 0 1 1 re f\n")),
	)
	pdf.AddPage(pdf.Indirect(page2))

	// Deliberately no /Metadata and no /OutputIntents.
	return save(pdf, "1.7")
}
