package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("aakeys_4", aakeys4) }

// aakeys4 builds aakeys_4.pdf: catalog and page additional-actions keys at PDF/A-4.
//
// ISO 19005-4, 6.6.3 does not forbid an /AA entry on the document catalog or a
// page, as parts 1 to 3 do. It restricts the keys of its additional-actions
// dictionary to E, X, D, U, Fo and Bl, and veraPDF's test judges each such
// dictionary by the keys it holds. A remediation that drops the /AA whole loses
// the permitted entries with the forbidden ones, so each dictionary here holds
// one of each:
//
//   - the catalog's /AA holds /WC, a trigger 6.6.3 forbids, and /E;
//   - page 1's /AA holds /O, a trigger 6.6.3 forbids, and /X;
//   - page 2's /AA holds /D alone, the control: nothing to report or remove.
//
// Every action is a GoTo to page 1, a type every PDF/A part permits, so the
// action-type rules stay silent and the key restriction stands alone. The
// catalog /AA is an object of its own, so a fix that edits it must write it back
// rather than a copy of the document's version.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func aakeys4() ([]byte, error) {
	pdf := pdfgen.New()

	for range 2 {
		page := pdf.Indirect(d(
			"Type", n("Page"),
			"MediaBox", a(0, 0, 612, 792),
			"Contents", pdf.Stream([]byte("")),
			"Resources", d(),
		))
		pdf.AddPage(page)
	}

	first := pdf.Page(0)

	// goto is a GoTo action opening page 1 in full.
	gotoFirst := func() *pdfgen.Dict {
		return d("S", n("GoTo"), "D", a(first, n("Fit")))
	}

	pdf.Root.Set("AA", pdf.Indirect(d("WC", gotoFirst(), "E", gotoFirst())))
	pdf.Page(0).Set("AA", d("O", gotoFirst(), "X", gotoFirst()))
	pdf.Page(1).Set("AA", d("D", gotoFirst()))

	// Deliberately no /Metadata.
	return save(pdf, "2.0")
}
