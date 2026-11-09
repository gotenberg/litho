package build

import (
	"fmt"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("structkinds_ua2", structkindsUa2) }

// structkindsUa2 builds structkinds_ua2.pdf: five ISO 14289-2 structure-element kinds at once.
//
// The coverage scan left five PDF/UA-2 structure tests unreached, one per kind of
// element. They are independent objects of the same walk, so planting them
// together is the masking probe: a checker that reports the first structural
// complaint and stops reports one rule where five are due.
//
// | Element                                | Rule           |
// | -------------------------------------- | -------------- |
// | a root holding two `Document` elements | `8.2.5.2/1`    |
// | `Ruby` whose kids are RB, RP, RT       | `8.2.5.23/1`   |
// | `Warichu` whose kids are WP, WT        | `8.2.5.24/1`   |
// | `LI` holding a marked-content item     | `8.2.5.25/2`   |
// | `math` whose parent is a `P`           | `8.2.5.29/1`   |
//
// The root defect is the one that interacts, since 8.2.5.2 t1 judges the very
// kids the other four hang from. Two `Document` elements break it while leaving
// both subtrees walkable, which is measured rather than assumed: the `Warichu`
// pair lives under the SECOND `Document` on purpose, so a walk that reads only
// the first root kid loses 8.2.5.24 t1 outright. The construct also fires
// ISO/TS 32005 `Table 5. StructTreeRoot-Document`, which counts `Document` kids
// rather than comparing the whole kid sequence.
//
// Controls sit beside every defect, because each of these tests judges a shape
// rather than a kind: a `Ruby` of RB, RT and another of RB, RP, RT, RP, a
// `Warichu` of WP, WT, WP, an `LI` holding `Lbl` and `LBody` beside one holding
// `LBody` alone, and a `math` element parented by a `Formula`. veraPDF reports a
// rule once however many elements break it, so rule-set agreement alone cannot
// see these; the per-rule failed-check counts are what prove them silent.
//
// The MathML namespace is load-bearing and was measured on both tools. veraPDF
// decides `SEMathMLStructElem` by namespace, not by type name: the same element
// declared in the PDF 2.0 namespace, or declaring no namespace at all, is not a
// math element to it and reports 8.2.4 t1 instead. So the `math` elements here
// bind to http://www.w3.org/1998/Math/MathML deliberately, and the namespace is
// declared in the root's `/Namespaces`.
//
// The font is not embedded, so a structure verdict is visibly separate from the
// font clause.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func structkindsUa2() ([]byte, error) {
	pdf := pdfgen.New()

	const pdf20NS = "http://iso.org/pdf2/ssn"
	const mathmlNS = "http://www.w3.org/1998/Math/MathML"

	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("Type1"),
		"BaseFont", n("/Helvetica"),
		"Encoding", n("WinAnsiEncoding"),
	))

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d("Font", d("F1", font)),
		"StructParents", 0,
	))
	pdf.AddPage(page)

	ns20 := pdf.Indirect(d("Type", n("Namespace"), "NS", text(pdf20NS)))
	nsmath := pdf.Indirect(d("Type", n("Namespace"), "NS", text(mathmlNS)))
	structRoot := pdf.Indirect(d("Type", n("StructTreeRoot"), "Namespaces", a(ns20, nsmath)))

	// 8.2.5.2 t1: the root shall hold a single Document as its only child. Two of
	// them break it without hiding anything: both subtrees stay reachable, which
	// the Warichu pair under the second one proves.
	docA := pdf.Indirect(d(
		"Type", n("StructElem"),
		"S", n("Document"),
		"P", structRoot,
		"NS", ns20,
		"Lang", text("en-US"),
	))
	docB := pdf.Indirect(d(
		"Type", n("StructElem"),
		"S", n("Document"),
		"P", structRoot,
		"NS", ns20,
		"Lang", text("en-US"),
	))

	var leaves []any
	var tags []string

	// leaf is one element owning the next marked content sequence on the page.
	//
	// The sequence is emitted after the tree is built, so every MCID has exactly
	// one owner: an unowned one fires 8.2.2 t1 and drowns out the structure
	// verdicts this file exists to separate. A nil namespace is ns20.
	leaf := func(kind string, parent, namespace *pdfgen.Dict) *pdfgen.Dict {
		if namespace == nil {
			namespace = ns20
		}
		mcid := len(tags)
		tags = append(tags, kind)
		made := pdf.Indirect(d(
			"Type", n("StructElem"),
			"S", n(kind),
			"P", parent,
			"NS", namespace,
			"Pg", page,
			"K", mcid,
		))
		leaves = append(leaves, made)
		return made
	}

	// branch is one element whose children are structure elements rather than
	// content. A nil namespace is ns20.
	branch := func(kind string, parent *pdfgen.Dict, build func(*pdfgen.Dict) []any, namespace *pdfgen.Dict, attrs []any) *pdfgen.Dict {
		if namespace == nil {
			namespace = ns20
		}
		dd := d(
			"Type", n("StructElem"),
			"S", n(kind),
			"P", parent,
			"NS", namespace,
			"Pg", page,
		)
		if attrs != nil {
			dd.Set("A", d(attrs...))
		}
		made := pdf.Indirect(dd)
		made.Set("K", a(build(made)...))
		return made
	}

	listAttrs := []any{"O", n("/List"), "ListNumbering", n("/Unordered")}

	// 8.2.5.23 t1: a Ruby of RB, RP, RT. The clause admits RB, RT and the
	// four-element RB, RP, RT, RP, so a three-element run is a real subsequence of
	// the longer form rather than an unrelated shape.
	rubyBad := branch("P", docA, func(p *pdfgen.Dict) []any {
		return []any{branch("Ruby", p, func(r *pdfgen.Dict) []any {
			return []any{leaf("RB", r, nil), leaf("RP", r, nil), leaf("RT", r, nil)}
		}, nil, nil)}
	}, nil, nil)

	// Both conforming forms, so the check is visibly about the sequence rather
	// than about Ruby elements as such.
	rubyOK2 := branch("P", docA, func(p *pdfgen.Dict) []any {
		return []any{branch("Ruby", p, func(r *pdfgen.Dict) []any {
			return []any{leaf("RB", r, nil), leaf("RT", r, nil)}
		}, nil, nil)}
	}, nil, nil)
	rubyOK4 := branch("P", docA, func(p *pdfgen.Dict) []any {
		return []any{branch("Ruby", p, func(r *pdfgen.Dict) []any {
			return []any{leaf("RB", r, nil), leaf("RP", r, nil), leaf("RT", r, nil), leaf("RP", r, nil)}
		}, nil, nil)}
	}, nil, nil)

	// 8.2.5.25 t2: real content inside an LI shall be wrapped in Lbl or LBody. This
	// LI owns a marked content sequence directly.
	listBad := branch("L", docA, func(l *pdfgen.Dict) []any {
		return []any{leaf("LI", l, nil)}
	}, nil, listAttrs)

	// The control: one LI wrapping both, one wrapping LBody alone. /ListNumbering
	// is present because 8.2.5.25 t1 asks for it once any LI carries a label, and
	// that rule is not what this file is testing.
	listOK := branch("L", docA, func(l *pdfgen.Dict) []any {
		return []any{
			branch("LI", l, func(li *pdfgen.Dict) []any {
				return []any{leaf("Lbl", li, nil), leaf("LBody", li, nil)}
			}, nil, nil),
			branch("LI", l, func(li *pdfgen.Dict) []any {
				return []any{leaf("LBody", li, nil)}
			}, nil, nil),
		}
	}, nil, listAttrs)

	// 8.2.5.29 t1: a math element shall be a child of Formula. This one hangs off a
	// paragraph. The MathML namespace is what makes it a math element at all.
	mathBad := branch("P", docA, func(p *pdfgen.Dict) []any {
		return []any{leaf("math", p, nsmath)}
	}, nil, nil)

	// The control: the same element under a Formula.
	mathOK := branch("Formula", docA, func(f *pdfgen.Dict) []any {
		return []any{leaf("math", f, nsmath)}
	}, nil, nil)

	// 8.2.5.24 t1: a Warichu shall be the three-element run WP, WT, WP. This one
	// stops at two. It lives under the second Document so that a walk which reads
	// only the first root kid reports nothing for the clause.
	wariBad := branch("P", docB, func(p *pdfgen.Dict) []any {
		return []any{branch("Warichu", p, func(w *pdfgen.Dict) []any {
			return []any{leaf("WP", w, nil), leaf("WT", w, nil)}
		}, nil, nil)}
	}, nil, nil)

	// The control, also under the second Document.
	wariOK := branch("P", docB, func(p *pdfgen.Dict) []any {
		return []any{branch("Warichu", p, func(w *pdfgen.Dict) []any {
			return []any{leaf("WP", w, nil), leaf("WT", w, nil), leaf("WP", w, nil)}
		}, nil, nil)}
	}, nil, nil)

	docA.Set("K", a(rubyBad, rubyOK2, rubyOK4, listBad, listOK, mathBad, mathOK))
	docB.Set("K", a(wariBad, wariOK))
	structRoot.Set("K", a(docA, docB))
	structRoot.Set("ParentTree", pdf.Indirect(d("Nums", a(0, a(leaves...)))))
	structRoot.Set("ParentTreeNextKey", 1)

	var content []byte
	for i, tag := range tags {
		content = fmt.Appendf(content, "/%s <</MCID %d>> BDC\nBT /F1 9 Tf 50 %d Td (%s %d) Tj ET\nEMC\n",
			tag, i, 740-i*26, tag, i)
	}
	page.Set("Contents", pdf.Stream(content))

	pdf.Root.Set("StructTreeRoot", structRoot)
	pdf.Root.Set("MarkInfo", d("Marked", true))
	pdf.Root.Set("Lang", text("en-US"))
	pdf.Root.Set("ViewerPreferences", d("DisplayDocTitle", true))

	xmp := `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
 <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
  <rdf:Description rdf:about=""
    xmlns:dc="http://purl.org/dc/elements/1.1/"
    xmlns:pdfuaid="http://www.aiim.org/pdfua/ns/id/">
   <dc:title><rdf:Alt><rdf:li xml:lang="x-default">structure element kinds</rdf:li></rdf:Alt></dc:title>
   <pdfuaid:part>2</pdfuaid:part>
   <pdfuaid:rev>2024</pdfuaid:rev>
  </rdf:Description>
 </rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`
	metadata := pdf.Stream([]byte(xmp))
	metadata.Set("Type", n("Metadata"))
	metadata.Set("Subtype", n("XML"))
	pdf.Root.Set("Metadata", pdf.MakeIndirect(metadata))

	return save(pdf, "2.0")
}
