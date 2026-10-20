package remediation

import (
	"context"
	"fmt"
	"maps"
	"strconv"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// maxDerivedHeadingLevel is the deepest level the heading repair writes. ISO
// 32000-1:2008, 14.8.4.3.2, Table 335 defines H1 to H6 in the PDF 1.7
// namespace, and the repair renames in place, so a deeper level would name a
// type the element's namespace may not define.
const maxDerivedHeadingLevel = 6

// matchHeadingLevel matches ISO 14289-2, 8.2.5.12 t1: a conforming file uses
// the generic H structure type.
func matchHeadingLevel(v compliance.Violation) bool {
	return v.Rule.Specification == "ISO_14289_2" && v.Rule.Clause == "8.2.5.12" && v.Rule.TestNumber == 1
}

// fixHeadingLevel renames each generic H heading to the numbered heading its
// section nesting gives it. ISO 14289-2, 8.2.5.12 forbids H, and the
// replacement is a numbered heading. ISO 32000-1:2008, 14.8.4.3.2, Table 335
// defines H as the label of the division it is the first child of, and H1 to
// H6 as the headings of documents that cannot nest their sections and so
// cannot take a heading's level from its level of nesting. 14.8.4.3.5 calls
// the nesting kind strongly structured: there, the nesting of the sections is
// the outline, and an H states its level by where it sits in it.
//
// The repair writes that level out in two cases.
//
// An H nested in no division, whose ancestors are only Document elements or
// the structure tree root, sits at the outermost level: it becomes H1.
//
// An H inside a division is numbered when the whole document is strongly
// structured in the plain sense the clause describes: every H nested below
// the Document level sits in a chain of divisions (Document elements between
// them are transparent), is the first structure element of its parent and the
// only H there, and the document numbers no heading itself, neither with an
// Hn element nor through a role map. Its level is then the number of
// divisions around it, less that of the outermost H, plus one: the outermost
// headings become H1, and each division nested inside another adds a level.
//
// The divisions are Sect and Art, the two grouping types the strongly
// structured paradigm names as the levels of an outline: ISO 32000-1:2008,
// 14.8.4.3.5 has the grouping elements nest "to reflect the organization of
// the material into articles, sections, subsections", each level a heading
// and its content. ISO/TS 32005, Table 5 marks them as headed divisions:
// Sect-H and Art-H allow at most one H, the heading that states the level.
// Art counts only in the PDF 1.7 namespace, since ISO 32000-2:2020, 14.8.4.4
// defines no Art in the PDF 2.0 one, and an element without /NS is in the
// default, PDF 1.7, namespace (14.7.4).
//
// Part and Div add no level. 14.8.4.3.5 lists them among the grouping
// elements that may nest, but ISO/TS 32005, Table 5 looks through Part, Div
// and NonStruct and judges what they hold as held by the enclosing element,
// so it bounds no Part-H or Div-H, and ISO 32000-1:2008, Table 333 calls a
// Div a generic element and a Part a division grouping articles or sections,
// whose sections carry the outline. The two readings, a wrapper or a level,
// number the same tree differently, and choosing one is a reading of the
// author's outline the repair does not make: an H whose parent is a Part or a
// Div gives no level, and an H in a division inside one is refused with it.
// ISO 32000-2:2020, 14.8.4.4 defines NonStruct as a grouping wrapper whose
// descendants carry the meaning, and an Aside, a DocumentFragment, a list, a
// table cell or a BlockQuote states no level of the document's own outline
// either. An H under any of them gives no level. The nested repair is all or
// nothing: when one H gives no level, every H below the Document level stays
// H, so the outline is not half numbered.
//
// veraPDF checks no derived level at PDF/UA-2 or WTPDF: its profiles test H
// only for 8.2.5.12 t1 and the Table 5 containment rows, and the PDF/UA-1
// sequence rule, ISO 14289-1, 7.4.2, applies to Hn in documents that are not
// strongly structured. The choice of divisions is the outline's reading, not
// a validator's verdict.
//
// The other refusals are where the nesting does not give a level. A
// division holding two H (ISO/TS 32005, Table 5, Sect-H and Art-H) has no
// level to give the second, and ISO 14289-1, 7.4.4 calls a document that uses both H and Hn
// neither strongly nor weakly structured, so its H relate to no outline the
// file states. A level past [maxDerivedHeadingLevel] is refused too.
//
// The repair is also refused where a role map governs a name it writes: the
// legacy /RoleMap mapping H or the target Hn, or the /RoleMapNS of the
// heading's namespace mapping either, since the renamed element would then
// stand for a type the file chose (ISO 32000-2:2020, 14.8.6). An H, Sect,
// Art or Document in a namespace other than the two PDF standard ones is not
// the standard type and gives no level. Only the /S changes: content, attributes
// and children stay as the file wrote them.
//
// The WTPDF levels state 8.2.5.12 with the rest of ISO 14289-2, so the
// repair runs for them too.
//
// # Corpus evidence
//
//   - verapdf PDF_UA-2__8.2.5.12-t01-fail-a: a Document whose only kid is an
//     H. Renamed H1, the file has the shape of the passing
//     PDF_UA-2__8.2.5.12-t01-pass-a, and veraPDF 1.30.2 reports nothing on
//     the PDF/UA-2 output, default and aggressive.
//   - All corpora: 20 H elements in 12 files. Beside the file above, the H of
//     verapdf PDF_UA-1__7.4.4-t01-pass-a and 7.4.4-t02-pass-a each head a
//     Sect of the Document and become H1. The other nine files are refused:
//     two H in one Sect (verapdf 7.4.4-t01-fail-a, litho headings_ua1), an
//     H1 in the same document (verapdf 7.4.4-t02 and t03 fail files, pdfjs
//     bug1722036 and issue16081), an H in a table header cell (pdfjs
//     prefilled_f1040).
//   - verapdf, litho, pdfjs: remediated at PDF/UA-2, default and aggressive,
//     the 12 files fully fixed on litho and veraPDF 1.30.2 alike go from 1
//     to 3, and at each WTPDF level from 0 to 3, since the WTPDF runs did not
//     reach the repair before. No rule appears on either tool, and litho and
//     veraPDF report the same rule set on every output.
//   - All corpora: no H has a standard Art ancestor. pdfjs bug1722036 nests
//     its H in an Article its /RoleMap maps to Art, and the H1 it also uses
//     refuses it.
//   - litho headingart_ua2: the three H of two Art elements of the PDF 1.7
//     namespace and a Sect inside one become H1, H2 and H1, and veraPDF
//     1.30.2 reports nothing on the PDF/UA-2 and WTPDF outputs, default and
//     aggressive.
func fixHeadingLevel(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, profile string) ([]string, error) {
	if compliance.PDFUAPart(profile) != 2 {
		return nil, nil
	}
	catalogRef, ok := catalogRefForFixer(ctx, doc)
	if !ok {
		return nil, nil
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil, nil
	}
	stRef, ok := catalogDict["StructTreeRoot"].(ast.PdfIndirectRef)
	if !ok || stRef.ObjectNum == 0 {
		return nil, nil
	}
	stDict := resolveFromAllocOrDoc(ctx, alloc, doc, stRef)
	if stDict == nil {
		return nil, nil
	}

	legacy, _ := document.ResolveDict(ctx, doc, stDict["RoleMap"]) // Best-effort: nil means no legacy map.
	if roleMapGoverns(legacy, "H", "H1") {
		doc.Logger.Debug("remediation.heading-level: the legacy /RoleMap maps H or H1; left alone")
		return nil, nil
	}

	w := headingWalk{alloc: alloc, doc: doc, legacy: legacy, visited: map[int]bool{stRef.ObjectNum: true}}
	w.walk(ctx, stDict["K"], headingPlace{topLevel: true, divisions: true}, 0)
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("walking the structure tree for H headings: %w", err)
	}
	if !w.numbered {
		w.numbered = roleMapsNumberHeadings(ctx, alloc, doc, legacy, stDict)
	}

	levels := w.levels(ctx)
	renamed := 0
	for _, h := range w.headings {
		level, ok := levels[h.node.ref.ObjectNum]
		if !ok {
			continue
		}
		target := ast.PdfName("H" + strconv.Itoa(level))
		patched := make(ast.PdfDict, len(h.node.dict))
		maps.Copy(patched, h.node.dict)
		patched["S"] = target
		alloc.AllocAt(h.node.ref.ObjectNum, patched)
		renamed++
	}
	if renamed == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("numbered %d H heading(s) by their section nesting", renamed)}, nil
}

// headingPlace describes where the walk stands in the structure tree.
type headingPlace struct {
	// topLevel holds while every ancestor is a standard Document element.
	topLevel bool
	// divisions holds while every ancestor is a standard Document, Sect or
	// Art element, the chain an H takes its level from.
	divisions bool
	// depth counts the Sect and Art ancestors.
	depth int
}

// headingCandidate is one H the walk found, with what its place gives it.
type headingCandidate struct {
	node     structNode
	topLevel bool
	// placed holds when the H sits in a chain of divisions, is the first
	// structure element of its parent and the only H there.
	placed bool
	depth  int
}

// headingWalk collects the H headings of a structure tree and whether the
// document numbers any heading itself.
type headingWalk struct {
	alloc    *writer.ObjAllocator
	doc      *document.Document
	legacy   ast.PdfDict
	visited  map[int]bool
	headings []headingCandidate
	// numbered holds when the document writes an Hn heading.
	numbered bool
	// unreachable holds when an H below the Document level is a direct object,
	// which the repair cannot rename in place.
	unreachable bool
}

// walk visits the structure elements kObj lists. visited marks the elements
// and the indirect /K arrays walked, so a cycle or a repeated kid is visited
// once.
func (w *headingWalk) walk(ctx context.Context, kObj ast.PdfObject, place headingPlace, depth int) {
	if depth > compliance.MaxStructTreeWalkDepth || ctx.Err() != nil {
		return
	}
	if _, indirect := kObj.(ast.PdfIndirectRef); indirect {
		if _, err := document.ResolveArray(ctx, w.doc, kObj); err == nil && document.VisitedBefore(kObj, w.visited) {
			return
		}
	}
	type kid struct {
		ref  ast.PdfIndirectRef
		dict ast.PdfDict
		s    ast.PdfName
	}
	var elems []kid
	headings := 0
	for _, item := range resolveStructKids(ctx, w.doc, kObj) {
		var k kid
		if ref, ok := item.(ast.PdfIndirectRef); ok {
			if ref.ObjectNum == 0 || w.visited[ref.ObjectNum] {
				continue
			}
			w.visited[ref.ObjectNum] = true
			k.ref, k.dict = ref, resolveFromAllocOrDoc(ctx, w.alloc, w.doc, ref)
		} else {
			k.dict, _ = item.(ast.PdfDict)
		}
		if k.dict == nil {
			continue
		}
		// A marked-content or object reference carries no /S; only
		// structure elements count as the parent's elements.
		if k.s = structType(ctx, w.doc, k.dict); k.s == "" {
			continue
		}
		if k.s == "H" {
			headings++
		}
		elems = append(elems, k)
	}

	for i, k := range elems {
		inner := headingPlace{depth: place.depth}
		switch {
		case k.s == "H":
			if k.ref.ObjectNum == 0 {
				if !place.topLevel {
					w.unreachable = true
				}
			} else {
				w.headings = append(w.headings, headingCandidate{
					node:     structNode{ref: k.ref, dict: k.dict},
					topLevel: place.topLevel,
					placed:   place.divisions && i == 0 && headings == 1,
					depth:    place.depth,
				})
			}
		case isNumberedHeading(k.s):
			w.numbered = true
		case k.s == "Document":
			standard := w.standardUngoverned(ctx, k.dict, "Document")
			inner.topLevel = place.topLevel && standard
			inner.divisions = place.divisions && standard
		case k.s == "Sect":
			inner.divisions = place.divisions && w.standardUngoverned(ctx, k.dict, "Sect")
			inner.depth++
		case k.s == "Art":
			// Per ISO 32000-2:2020, 14.8.4.4 the PDF 2.0 namespace defines no
			// Art, so only the PDF 1.7 one, declared or by default, makes the
			// element the standard article.
			uri := declaredNamespace(ctx, w.alloc, w.doc, k.dict)
			inner.divisions = place.divisions && (uri == "" || uri == pdf1NamespaceURI) &&
				w.standardUngoverned(ctx, k.dict, "Art")
			inner.depth++
		}
		w.walk(ctx, k.dict["K"], inner, depth+1)
	}
}

// standardUngoverned reports whether elem is the standard type s: no role map
// governs the name, and elem sits in one of the two PDF standard namespaces.
func (w *headingWalk) standardUngoverned(ctx context.Context, elem ast.PdfDict, s ast.PdfName) bool {
	return !roleMapGoverns(w.legacy, s) && inStandardNamespaceUngoverned(ctx, w.alloc, w.doc, elem, s)
}

// levels returns the level each renamed H takes, keyed by object number. An
// H nested in no division takes level 1. The H below the Document level take
// their Sect depth relative to the outermost H, all of them or none.
func (w *headingWalk) levels(ctx context.Context) map[int]int {
	out := make(map[int]int)
	outermost := -1
	nested := false
	for _, h := range w.headings {
		if outermost < 0 || h.depth < outermost {
			outermost = h.depth
		}
		if h.topLevel {
			if w.standardUngoverned(ctx, h.node.dict, "H") && inStandardNamespaceUngoverned(ctx, w.alloc, w.doc, h.node.dict, "H1") {
				out[h.node.ref.ObjectNum] = 1
			}
			continue
		}
		nested = true
	}
	if !nested {
		return out
	}
	reason := ""
	switch {
	case w.numbered:
		reason = "the document numbers headings itself"
	case w.unreachable:
		reason = "an H is a direct object"
	}
	derived := make(map[int]int)
	for _, h := range w.headings {
		if reason != "" {
			break
		}
		if h.topLevel {
			continue
		}
		level := h.depth - outermost + 1
		target := ast.PdfName("H" + strconv.Itoa(level))
		switch {
		case !h.placed:
			reason = "an H sits outside a chain of divisions, after other content or beside another H"
		case level > maxDerivedHeadingLevel:
			reason = "the nesting is deeper than H6"
		case !w.standardUngoverned(ctx, h.node.dict, "H") || roleMapGoverns(w.legacy, target) ||
			!inStandardNamespaceUngoverned(ctx, w.alloc, w.doc, h.node.dict, target):
			reason = "a role map governs H or its numbered name, or the H is outside the standard namespaces"
		default:
			derived[h.node.ref.ObjectNum] = level
		}
	}
	if reason != "" {
		w.doc.Logger.Debug("remediation.heading-level: the H headings below the Document level are left alone", "reason", reason)
		return out
	}
	maps.Copy(out, derived)
	return out
}

// isNumberedHeading reports whether s names a numbered heading, Hn with n a
// positive integer (ISO 32000-2:2020, 14.8.4.3.2).
func isNumberedHeading(s ast.PdfName) bool {
	if len(s) < 2 || s[0] != 'H' || s[1] == '0' {
		return false
	}
	n, err := strconv.Atoi(string(s[1:]))
	return err == nil && n > 0
}

// roleMapsNumberHeadings reports whether the legacy /RoleMap, or the
// /RoleMapNS of a namespace the structure tree root lists, maps a type to a
// numbered heading: a document that numbers its headings that way is not one
// whose nesting alone states its outline.
func roleMapsNumberHeadings(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, legacy, stDict ast.PdfDict) bool {
	for _, target := range legacy {
		if name, ok := target.(ast.PdfName); ok && isNumberedHeading(name) {
			return true
		}
	}
	namespaces, _ := document.ResolveArray(ctx, doc, stDict["Namespaces"]) // Best-effort: nil means no namespaces.
	for _, item := range namespaces {
		var ns ast.PdfDict
		if ref, ok := item.(ast.PdfIndirectRef); ok {
			ns = resolveFromAllocOrDoc(ctx, alloc, doc, ref)
		} else {
			ns, _ = item.(ast.PdfDict)
		}
		rmns, _ := document.ResolveDict(ctx, doc, ns["RoleMapNS"]) // Best-effort: nil means no entries.
		for _, target := range rmns {
			switch t := target.(type) {
			case ast.PdfName:
				if isNumberedHeading(t) {
					return true
				}
			case ast.PdfArray:
				if len(t) > 0 {
					if name, ok := t[0].(ast.PdfName); ok && isNumberedHeading(name) {
						return true
					}
				}
			}
		}
	}
	return false
}

// roleMapGoverns reports whether the role map m maps any of names.
func roleMapGoverns(m ast.PdfDict, names ...ast.PdfName) bool {
	for _, n := range names {
		if _, mapped := m[n]; mapped {
			return true
		}
	}
	return false
}

// inStandardNamespaceUngoverned reports whether elem declares no namespace or
// one of the two PDF standard namespaces (ISO 32000-2:2020, 14.8.6), and that
// namespace's /RoleMapNS maps none of names.
func inStandardNamespaceUngoverned(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, elem ast.PdfDict, names ...ast.PdfName) bool {
	if _, declares := elem["NS"]; !declares {
		return true
	}
	var nsDict ast.PdfDict
	if ref, ok := elem["NS"].(ast.PdfIndirectRef); ok {
		nsDict = resolveFromAllocOrDoc(ctx, alloc, doc, ref)
	} else {
		nsDict, _ = document.ResolveDict(ctx, doc, elem["NS"]) // Best-effort: nil reads as no namespace URI.
	}
	uri, _ := document.ResolveString(ctx, doc, nsDict["NS"]) // Best-effort: empty fails the URI test below.
	if string(uri) != pdf1NamespaceURI && string(uri) != pdf2NamespaceURI {
		return false
	}
	rmns, _ := document.ResolveDict(ctx, doc, nsDict["RoleMapNS"]) // Best-effort: nil means no entries.
	return !roleMapGoverns(rmns, names...)
}
