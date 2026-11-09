package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("threed_4e", threed4e) }

// threed4e builds threed_4e.pdf: the 3D stream subtypes ISO 19005-4 permits.
//
// PDF/A-4e is the only conformance level that admits a 3D annotation at all, and
// ISO 19005-4, 6.1.6.1 test 3 is the only rule that reads the 3D stream it points
// at: `Subtype == 'U3D' || Subtype == 'PRC'`. No fixture reached it.
//
// Three 3D annotations sit on the page, each with its own `/3DD` stream:
//
// | Annotation | `/3DD` `/Subtype` | Answers       |
// | ---------- | ----------------- | ------------- |
// | first      | `/U3D`            | nothing       |
// | second     | `/PRC`            | nothing       |
// | third      | `/LithoNot3D`     | `6.1.6.1/3`   |
//
// Both permitted values are planted as controls, not just one: the test is a
// two-way disjunction, and a check that hardcodes only `U3D` passes the file
// anyway unless a `PRC` sits beside it. The defective stream is otherwise
// identical to the two conforming ones, so a verdict attached to the wrong stream
// answers on a control.
//
// Every annotation carries an appearance stream and a `/Contents` string, so
// ISO 19005-4, 6.3.3 stays silent and the 6.1.6.1 verdict stands alone. The
// annotations are also the reason the file has to be read at PDF/A-4e: at every
// other profile a 3D annotation is a forbidden subtype, which is a different
// clause on a different object.
//
// An unembedded Helvetica paints the page, and there is neither `/Metadata` nor
// an output intent, so three further clauses answer from elsewhere in the
// specification. A checker that stops at its first verdict reports one rule where
// four are due.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func threed4e() ([]byte, error) {
	pdf := pdfgen.New()

	// ISO 19005-4, 6.1.6.1 t3 names both, so both are planted.
	permitted := []string{"U3D", "PRC"}
	const forbidden = "LithoNot3D"

	font := pdf.Indirect(d(
		"Type", n("Font"), "Subtype", n("Type1"),
		"BaseFont", n("/Helvetica"), "Encoding", n("WinAnsiEncoding")))

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d("Font", d("F1", font)),
		"Contents", pdf.Stream([]byte("BT /F1 12 Tf 50 720 Td (three dimensional) Tj ET\n"))))
	pdf.AddPage(page)

	// artwork is one 3D stream, ISO 32000-2, 13.6.3. The payload is opaque to
	// the test.
	artwork := func(subtype string) *pdfgen.Stream {
		stream := pdf.Stream([]byte("litho corpus 3D artwork"))
		stream.Set("Type", n("/3D"))
		stream.Set("Subtype", n(subtype))
		pdf.MakeIndirect(stream)
		return stream
	}

	// annotation is a 3D annotation with an appearance, so only the stream
	// subtype answers.
	annotation := func(subtype string, index int) *pdfgen.Dict {
		appearance := pdf.Stream([]byte("0.5 0.5 0.5 rg 0 0 150 50 re f\n"))
		appearance.Set("Type", n("XObject"))
		appearance.Set("Subtype", n("Form"))
		appearance.Set("BBox", a(0, 0, 150, 50))
		return pdf.Indirect(d(
			"Type", n("Annot"),
			"Subtype", n("/3D"),
			"Rect", a(50, 400+index*70, 200, 450+index*70),
			"F", 4,
			"Contents", text("3D artwork "+subtype),
			"AP", d("N", pdf.MakeIndirect(appearance)),
			"3DD", artwork(subtype)))
	}

	annots := a()
	for i, s := range append(permitted, forbidden) {
		annots.Append(annotation(s, i))
	}
	page.Set("Annots", annots)

	// Deliberately no /Metadata and no output intent.
	return save(pdf, "2.0")
}
