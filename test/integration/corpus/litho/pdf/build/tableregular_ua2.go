package build

import (
	"fmt"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("tableregular_ua2", tableregularUa2) }

// tableregularUa2 builds tableregular_ua2.pdf: table regularity under PDF/UA-2.
//
// The coverage scan showed ISO 14289-2, 8.2.5.26 as the largest unreached cluster
// in the corpus's weakest profile. `tablespan_ua2.pdf` reaches test 3 through a
// row whose column spans do not add up, and its docstring records tests 1 and 2 as
// constructs it could not produce. Both are produced here.
//
// Test 1 is scoped to `SETableCell` and test 2 to `SETable`, so the two are
// different objects of one clause. Planting them together is the masking probe: a
// check that latches on the first table it finds irregular reports one rule where
// two are due. Two conforming tables sit beside them, one flat and one sectioned,
// so a check that condemns every table once it sees one is equally visible.
//
// The intersection in table A is not the naive one. A `/RowSpan` of 2 does not
// collide with the row below it, because the table model shifts that row's cells
// past the occupied slot rather than overlapping them. An intersection needs a
// row whose own spans reach into a slot a previous row already carries down into:
// here row 1 is TH(RowSpan 2), TH, TH(RowSpan 2) and row 2 is a single
// TH(ColSpan 2), whose second column lands on the slot the last cell of row 1
// occupies.
//
// Table B separates the two tests that a section boundary decides. Its `/RowSpan`
// starts in the THead and reaches into the TBody. Measured on both tools, that is
// test 2 under PDF/UA-2 and NOT a column-count defect. The same table under
// PDF/UA-1 reports the column rule 7.2 t42 instead, because PDF/UA-1 draws no
// section boundary, so the two profiles disagree by design and the fixture
// exercises both.
//
// Tables E and F reach tests 5 and 6, the two halves of header determination.
// Getting there needed the order veraPDF applies, which no amount of probing
// revealed and which the clause does not state: determination is settled by
// `/Scope` first, and the `/Headers` route runs only when at least one TH carries
// no `/Scope`. Both tables therefore leave their single TH unscoped. The cell that
// cannot be resolved sits at row 1 column 1, where no TH shares its row or its
// column, and the cell at row 0 column 0 is exempt from the fallback altogether,
// which is why the TH occupies it. Table E's cell names no header at all (t5) and table F's names one no TH
// declares (t6). veraPDF stops at the first cell it cannot resolve, so the two
// halves need a table each, and table F's earlier cells all carry a valid
// /Headers so its target cell is the one that fails.
//
// Tests 3 and 4 belong to `tablespan_ua2.pdf` and are not repeated here.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func tableregularUa2() ([]byte, error) {
	pdf := pdfgen.New()

	const pdf20NS = "http://iso.org/pdf2/ssn"

	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("Type1"),
		"BaseFont", n("/Helvetica"),
		"Encoding", n("WinAnsiEncoding"),
	))

	const cellCount = 32
	content := ""
	for i := range cellCount {
		content += fmt.Sprintf("/TD <</MCID %d>> BDC\nBT /F1 9 Tf 40 %d Td (c%d) Tj ET\nEMC\n", i, 750-i*17, i)
	}

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d("Font", d("F1", font)),
		"Contents", pdf.Stream([]byte(content)),
		"StructParents", 0,
	))
	pdf.AddPage(page)

	ns20 := pdf.Indirect(d("Type", n("Namespace"), "NS", text(pdf20NS)))
	structRoot := pdf.Indirect(d("Type", n("StructTreeRoot"), "Namespaces", a(ns20)))
	document := pdf.Indirect(d(
		"Type", n("StructElem"),
		"S", n("Document"),
		"P", structRoot,
		"NS", ns20,
		"Lang", text("en-US"),
	))

	var leaves []any
	nextMCID := 0

	// cellOpts are cell's keyword arguments. A zero span, an empty cid and a
	// nil headers are Python's None; noScope is scope=None.
	type cellOpts struct {
		colspan, rowspan int
		scope            string
		noScope          bool
		cid              string
		headers          []string
	}

	// cell is one TH or TD, owning the next marked content sequence on the page.
	//
	// scope=None omits /Scope from a TH, which is what pushes header
	// determination onto the /Headers route.
	cell := func(kind string, parent *pdfgen.Dict, o cellOpts) *pdfgen.Dict {
		attrs := d("O", n("/Table"))
		if o.colspan != 0 {
			attrs.Set("ColSpan", o.colspan)
		}
		if o.rowspan != 0 {
			attrs.Set("RowSpan", o.rowspan)
		}
		scope := o.scope
		if scope == "" {
			scope = "Column"
		}
		if kind == "TH" && !o.noScope {
			attrs.Set("Scope", n(scope))
		}
		if o.headers != nil {
			hs := a()
			for _, h := range o.headers {
				hs.Append(text(h))
			}
			attrs.Set("Headers", hs)
		}
		dd := d(
			"Type", n("StructElem"),
			"S", n(kind),
			"P", parent,
			"NS", ns20,
			"Pg", page,
			"K", nextMCID,
			"A", attrs,
		)
		if o.cid != "" {
			dd.Set("ID", text(o.cid))
		}
		nextMCID++
		made := pdf.Indirect(dd)
		leaves = append(leaves, made)
		return made
	}

	type builder = func(*pdfgen.Dict) []any

	row := func(parent *pdfgen.Dict, build builder) *pdfgen.Dict {
		r := pdf.Indirect(d("Type", n("StructElem"), "S", n("TR"), "P", parent, "NS", ns20, "Pg", page))
		r.Set("K", a(build(r)...))
		return r
	}

	section := func(kind string, parent *pdfgen.Dict, builds ...builder) *pdfgen.Dict {
		s := pdf.Indirect(d("Type", n("StructElem"), "S", n(kind), "P", parent, "NS", ns20, "Pg", page))
		k := a()
		for _, b := range builds {
			k.Append(row(s, b))
		}
		s.Set("K", k)
		return s
	}

	table := func(kids builder) *pdfgen.Dict {
		t := pdf.Indirect(d("Type", n("StructElem"), "S", n("Table"), "P", document, "NS", ns20, "Pg", page))
		t.Set("K", a(kids(t)...))
		return t
	}

	// Table A: row 2's second column lands on the slot row 1's last cell carries
	// down into, which is a genuine cell intersection (t1).
	tableA := table(func(t *pdfgen.Dict) []any {
		return []any{
			row(t, func(r *pdfgen.Dict) []any {
				return []any{
					cell("TH", r, cellOpts{rowspan: 2, cid: "a1"}),
					cell("TH", r, cellOpts{cid: "a2"}),
					cell("TH", r, cellOpts{rowspan: 2, cid: "a3"}),
				}
			}),
			row(t, func(r *pdfgen.Dict) []any { return []any{cell("TH", r, cellOpts{colspan: 2, cid: "a4"})} }),
		}
	})

	// Table B: the /RowSpan starts in the THead and reaches into the TBody, so the
	// grouping the sections form is not regular (t2).
	tableB := table(func(t *pdfgen.Dict) []any {
		return []any{
			section(
				"THead",
				t,
				func(r *pdfgen.Dict) []any {
					return []any{cell("TH", r, cellOpts{rowspan: 2, cid: "b1"}), cell("TH", r, cellOpts{cid: "b2"})}
				},
			),
			section("TBody", t, func(r *pdfgen.Dict) []any {
				return []any{cell("TD", r, cellOpts{}), cell("TD", r, cellOpts{})}
			}),
		}
	})

	// Table C conforms and is sectioned, so it shows the section model accepts a
	// regular grouping rather than merely rejecting every THead.
	tableC := table(func(t *pdfgen.Dict) []any {
		return []any{
			section(
				"THead",
				t,
				func(r *pdfgen.Dict) []any {
					return []any{cell("TH", r, cellOpts{cid: "c1"}), cell("TH", r, cellOpts{cid: "c2"})}
				},
			),
			section(
				"TBody",
				t,
				func(r *pdfgen.Dict) []any { return []any{cell("TD", r, cellOpts{}), cell("TD", r, cellOpts{})} },
				func(r *pdfgen.Dict) []any { return []any{cell("TD", r, cellOpts{}), cell("TD", r, cellOpts{})} },
			),
		}
	})

	// Table D conforms while carrying real spans, so its silence shows the geometry
	// check accepts a legitimate span rather than ignoring the attributes.
	tableD := table(func(t *pdfgen.Dict) []any {
		return []any{
			row(t, func(r *pdfgen.Dict) []any {
				return []any{cell("TH", r, cellOpts{cid: "d1"}), cell("TH", r, cellOpts{cid: "d2"})}
			}),
			row(t, func(r *pdfgen.Dict) []any { return []any{cell("TD", r, cellOpts{colspan: 2})} }),
		}
	})

	// Table E: regular, and its only TH carries no /Scope, so header determination
	// falls through to the /Headers route. The cell at row 1 column 1 has no TH in
	// its row or its column, so no header can be determined for it (t5). The cell
	// at row 0 column 0 is exempt, which is why the TH sits there.
	tableE := table(func(t *pdfgen.Dict) []any {
		return []any{
			row(t, func(r *pdfgen.Dict) []any {
				return []any{cell("TH", r, cellOpts{noScope: true, cid: "e1"}), cell("TD", r, cellOpts{}), cell("TD", r, cellOpts{})}
			}),
			row(t, func(r *pdfgen.Dict) []any {
				return []any{cell("TD", r, cellOpts{}), cell("TD", r, cellOpts{}), cell("TD", r, cellOpts{})}
			}),
		}
	})

	// Table F is the other half of the same split: the undeterminable cell names a
	// header id no TH declares (t6). Every cell before it carries a valid /Headers
	// so that it is the FIRST cell veraPDF cannot resolve, which matters because
	// the fallback stops there. It also makes the fixture profile-agnostic: under
	// PDF/UA-1 an unscoped TH has no default scope at all, so without the valid
	// /Headers an earlier cell would fail first and the file would report the other
	// half of the split under the two profiles.
	tableF := table(func(t *pdfgen.Dict) []any {
		return []any{
			row(
				t,
				func(r *pdfgen.Dict) []any {
					return []any{
						cell("TH", r, cellOpts{noScope: true, cid: "f1"}),
						cell("TD", r, cellOpts{headers: []string{"f1"}}),
						cell("TD", r, cellOpts{headers: []string{"f1"}}),
					}
				},
			),
			row(
				t,
				func(r *pdfgen.Dict) []any {
					return []any{
						cell("TD", r, cellOpts{headers: []string{"f1"}}),
						cell("TD", r, cellOpts{headers: []string{"nosuchheader"}}),
						cell("TD", r, cellOpts{headers: []string{"f1"}}),
					}
				},
			),
		}
	})

	document.Set("K", a(tableA, tableB, tableC, tableD, tableE, tableF))
	structRoot.Set("K", a(document))
	structRoot.Set("ParentTree", pdf.Indirect(d("Nums", a(0, a(leaves...)))))
	structRoot.Set("ParentTreeNextKey", 1)

	pdf.Root.Set("StructTreeRoot", structRoot)
	pdf.Root.Set("MarkInfo", d("Marked", true))
	pdf.Root.Set("Lang", text("en-US"))
	pdf.Root.Set("ViewerPreferences", d("DisplayDocTitle", true))

	// Deliberately no /Metadata, and the font is not embedded, so a table verdict
	// is visibly separate from the metadata and font clauses.
	return save(pdf, "2.0")
}
