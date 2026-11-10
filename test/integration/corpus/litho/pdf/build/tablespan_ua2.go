package build

import (
	"fmt"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("tablespan_ua2", tablespanUa2) }

// tablespanUa2 builds tablespan_ua2.pdf: table cell geometry under PDF/UA-2.
//
// The coverage scan showed ISO 14289-2, 8.2.5.26 unreached. It constrains the
// geometry a table's cells describe through /RowSpan and /ColSpan: cells must
// not overlap, and the spans a row or column declares must add up. The existing
// `tables_ua1.pdf` covers header association under PDF/UA-1 but declares no
// spans at all, so none of this clause is reachable from it.
//
// Two tables: one whose row declares spans that do not add up, and one
// conforming. The conforming table carries real spans rather than a trivial
// grid, so its silence shows the geometry check accepts a legitimate span
// rather than merely ignoring the attribute.
//
// Only `t3` of the clause is reached. The overlap test `t1` needs two cells
// claiming one grid position, which a `/RowSpan` cannot produce: under the
// table model a spanning cell shifts the following row's cells rather than
// colliding with them, so the construct reads as a column count mismatch, and
// veraPDF agrees it is not an intersection. `t2` and `t4` need span geometries
// this builder could not produce, and `t5`/`t6` are the header association
// `tables_ua1.pdf` already covers.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func tablespanUa2() ([]byte, error) {
	pdf := pdfgen.New()

	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("Type1"),
		"BaseFont", n("/Helvetica"),
		"Encoding", n("WinAnsiEncoding"),
	))

	// One marked content sequence per cell across both tables.
	var content []byte
	for i := range 8 {
		content = fmt.Appendf(content, "/TD <</MCID %d>> BDC\nBT /F1 10 Tf 50 %d Td (c%d) Tj ET\nEMC\n",
			i, 700-i*20, i)
	}

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d("Font", d("F1", font)),
		"Contents", pdf.Stream(content),
		"StructParents", 0,
	))
	pdf.AddPage(page)

	structRoot := pdf.Indirect(d("Type", n("StructTreeRoot")))
	document := pdf.Indirect(d(
		"Type", n("StructElem"),
		"S", n("Document"),
		"P", structRoot,
		"Lang", text("en-US"),
	))

	// cellOpts are cell's options; a zero value leaves its key out.
	type cellOpts struct {
		colspan, rowspan int
		headers          []string
		cid              string
	}

	cell := func(kind string, parent *pdfgen.Dict, mcid int, o cellOpts) *pdfgen.Dict {
		attrs := d("O", n("/Table"))
		if o.colspan != 0 {
			attrs.Set("ColSpan", o.colspan)
		}
		if o.rowspan != 0 {
			attrs.Set("RowSpan", o.rowspan)
		}
		if o.headers != nil {
			hs := a()
			for _, h := range o.headers {
				hs.Append(text(h))
			}
			attrs.Set("Headers", hs)
		}
		if kind == "TH" {
			attrs.Set("Scope", n("Column"))
		}
		dd := d(
			"Type", n("StructElem"),
			"S", n(kind),
			"P", parent,
			"Pg", page,
			"K", mcid,
			"A", attrs,
		)
		if o.cid != "" {
			dd.Set("ID", text(o.cid))
		}
		return pdf.Indirect(dd)
	}

	row := func(parent *pdfgen.Dict, cells func(*pdfgen.Dict) []any) *pdfgen.Dict {
		r := pdf.Indirect(d("Type", n("StructElem"), "S", n("TR"), "P", parent, "Pg", page))
		r.Set("K", a(cells(r)...))
		return r
	}

	// Table one declares a two column grid, then gives its second row a cell
	// spanning three columns. The row's spans no longer add up to the table's
	// width, which is what 8.2.5.26 constrains.
	tBad := pdf.Indirect(d("Type", n("StructElem"), "S", n("Table"), "P", document, "Pg", page))
	badR1 := row(tBad, func(r *pdfgen.Dict) []any {
		return []any{cell("TH", r, 0, cellOpts{cid: "h1"}), cell("TH", r, 1, cellOpts{cid: "h2"})}
	})
	badR2 := row(tBad, func(r *pdfgen.Dict) []any {
		return []any{
			cell("TD", r, 2, cellOpts{colspan: 3, headers: []string{"h1"}}),
			cell("TD", r, 3, cellOpts{headers: []string{"h2"}}),
		}
	})
	tBad.Set("K", a(badR1, badR2))

	// Table two is a genuine spanning layout: two header columns, then one row
	// whose first cell spans both. The spans add up, every cell points at a
	// header, so nothing here may fire.
	tOK := pdf.Indirect(d("Type", n("StructElem"), "S", n("Table"), "P", document, "Pg", page))
	okR1 := row(tOK, func(r *pdfgen.Dict) []any {
		return []any{cell("TH", r, 4, cellOpts{cid: "g1"}), cell("TH", r, 5, cellOpts{cid: "g2"})}
	})
	okR2 := row(tOK, func(r *pdfgen.Dict) []any {
		return []any{cell("TD", r, 6, cellOpts{colspan: 2, headers: []string{"g1", "g2"}})}
	})
	okR3 := row(tOK, func(r *pdfgen.Dict) []any {
		return []any{cell("TD", r, 7, cellOpts{headers: []string{"g1"}}), cell("TD", r, 3, cellOpts{headers: []string{"g2"}})}
	})
	tOK.Set("K", a(okR1, okR2, okR3))

	document.Set("K", a(tBad, tOK))
	structRoot.Set("K", a(document))

	// Every cell owns exactly one marked content sequence, wired through the
	// parent tree so veraPDF resolves each cell to its content.
	kid := func(r *pdfgen.Dict, i int) pdfgen.Object {
		return r.Get("K").(*pdfgen.Array).Items[i]
	}
	order := []any{
		kid(badR1, 0),
		kid(badR1, 1),
		kid(badR2, 0),
		kid(badR2, 1),
		kid(okR1, 0),
		kid(okR1, 1),
		kid(okR2, 0),
		kid(okR3, 0),
	}
	structRoot.Set("ParentTree", pdf.Indirect(d("Nums", a(0, a(order...)))))
	structRoot.Set("ParentTreeNextKey", 1)

	pdf.Root.Set("StructTreeRoot", structRoot)
	pdf.Root.Set("MarkInfo", d("Marked", true))
	pdf.Root.Set("Lang", text("en-US"))
	pdf.Root.Set("ViewerPreferences", d("DisplayDocTitle", true))

	return save(pdf, "2.0")
}
