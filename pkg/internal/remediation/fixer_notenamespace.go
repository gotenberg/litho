package remediation

import (
	"context"
	"fmt"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// matchNoteNamespace matches ISO 14289-2, 8.2.5.14 t1: the PDF 1.7 standard
// Note type present outside a role mapping into the PDF 2.0 namespace.
func matchNoteNamespace(v compliance.Violation) bool {
	return v.Rule.Specification == "ISO_14289_2" && v.Rule.Clause == "8.2.5.14" && v.Rule.TestNumber == 1
}

// fixNoteNamespace maps the PDF 1.7 standard Note into the PDF 2.0 namespace,
// the escape ISO 14289-2, 8.2.5.14 t1 names: "unless role mapped to a structure
// element in the PDF 2.0 namespace". An element stands for the standard Note
// when its /S is Note, or a type the legacy /RoleMap leads to Note, in no
// namespace or in the PDF 1.7 one. For each such /S the PDF 1.7 namespace
// gains the /RoleMapNS entry /S -> [FENote, PDF 2.0 namespace]
// (ISO 32000-2:2020, 14.8.6.2, Table 356), and an element that declares no
// namespace is bound to the PDF 1.7 one, whose vocabulary it is already judged
// against (ISO 32000-1:2008, 14.8.4). Every /S stays as the file wrote it, and
// a type the legacy map led to Note now leads to FENote in one hop.
//
// The target is not a guess. PDF 2.0 dropped Note and added FENote as its
// replacement (ISO 32000-2:2020, 14.8.4, Table 364): ISO 32000-1:2008,
// Table 336 defines Note as explanatory text such as a footnote or an endnote,
// referred to from the body, and ISO 32000-2:2020 defines FENote as that
// footnote or endnote. No other PDF 2.0 type carries the meaning, and the
// FENote's NoteType defaults to None, which 8.2.5.14 t4 accepts.
//
// The repair is refused, and the Note left to fail t1, where the mapping would
// create a failure rather than state a namespace:
//
//   - the PDF 1.7 namespace declares no /RoleMapNS and an element bound to it
//     has a type the legacy /RoleMap maps: a /RoleMapNS there ends the legacy
//     map's governance of that element (ISO 32000-2:2020, 14.8.6, measured on
//     veraPDF 1.30.2), leaving it unmapped for 8.2.4 t1;
//   - the legacy /RoleMap maps Note itself, or the PDF 1.7 /RoleMapNS already
//     maps a type the repair would map: an entry the file wrote is not
//     replaced;
//   - a Note is in a /Ref association, either way: as an FENote, a one-sided
//     association fails 8.2.5.14 t2 or t3, and completing it would assert a
//     citation the file never made;
//   - a Note carries a NoteType attribute under the FENote owner, which t4
//     would then judge.
//
// # Corpus evidence
//
//   - litho fenote_ua2: one Note bound to the PDF 1.7 namespace. With the
//     entry, veraPDF 1.30.2 drops 8.2.5.14 t1 at PDF/UA-2 and both WTPDF
//     levels, default and aggressive, and reports no other new rule.
//   - litho formmedia_ua1: three Notes in no namespace. Mapping the PDF 1.7
//     namespace alone left all three failing on veraPDF, since an element in
//     no namespace is not governed by it; bound to it, all three clear.
//   - All corpora: of the files pikepdf opens, 33 hold a Note or a type the
//     legacy map leads to Note, and remediation left t1 failing on 31 of them
//     at PDF/UA-2 and both WTPDF levels; every one of the 31 clears on both
//     tools, default and aggressive, and no other output changes. 25 are
//     govdocs Word exports mapping /Footnote and /Endnote to /Note.
//   - govdocs thread_400_400585: its Note holding a heading fails ISO/TS
//     32005 Table 5 as FENote-Hn in place of Note-Hn, on both tools: the
//     same nesting defect, named for the type the Note now stands for.
func fixNoteNamespace(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, profile string) ([]string, error) {
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

	var elems []structNode
	visited := map[int]bool{stRef.ObjectNum: true}
	walkStructElemsWhere(ctx, alloc, doc, stDict["K"], func(ast.PdfDict) bool { return true }, visited, 0, &elems)
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("walking the structure tree for Note elements: %w", err)
	}

	legacy, _ := document.ResolveDict(ctx, doc, stDict["RoleMap"]) // Best-effort: nil means no legacy map.
	if _, mapped := legacy["Note"]; mapped {
		doc.Logger.Debug("remediation.note-namespace: the legacy /RoleMap maps Note; left alone")
		return nil, nil
	}
	classMap, _ := document.ResolveDict(ctx, doc, stDict["ClassMap"]) // Best-effort: nil means no classes.

	referred := make(map[int]bool)
	for _, e := range elems {
		for _, item := range resolveStructKids(ctx, doc, e.dict["Ref"]) {
			if ref, ok := item.(ast.PdfIndirectRef); ok {
				referred[ref.ObjectNum] = true
			}
		}
	}

	// Whether the PDF 1.7 namespace already declares a /RoleMapNS decides whether
	// the legacy map still governs the elements bound to it.
	ns17Declares := false
	if ref, ok := findNamespace(ctx, alloc, doc, stDict, pdf1NamespaceURI); ok {
		_, ns17Declares = resolveFromAllocOrDoc(ctx, alloc, doc, ref)["RoleMapNS"]
	}

	var notes []structNode
	types := make(map[ast.PdfName]bool)
	legacyBound17 := false
	for _, e := range elems {
		uri := declaredNamespace(ctx, alloc, doc, e.dict)
		s := structType(ctx, doc, e.dict)
		var isNote bool
		switch {
		case uri == "", uri == pdf1NamespaceURI && !ns17Declares:
			isNote = legacyChainEndsOnNote(s, legacy)
		case uri == pdf1NamespaceURI:
			isNote = s == "Note"
		}
		if !isNote {
			if _, mapped := legacy[s]; mapped && uri == pdf1NamespaceURI && !ns17Declares {
				legacyBound17 = true
			}
			continue
		}
		if _, hasRef := e.dict["Ref"]; hasRef || referred[e.ref.ObjectNum] {
			doc.Logger.Debug("remediation.note-namespace: a Note is in a /Ref association; left alone",
				"element", e.ref.ObjectNum)
			return nil, nil
		}
		if hasFENoteAttribute(ctx, doc, e.dict, classMap) {
			doc.Logger.Debug("remediation.note-namespace: a Note carries an FENote attribute; left alone",
				"element", e.ref.ObjectNum)
			return nil, nil
		}
		notes = append(notes, e)
		types[s] = true
	}
	if len(notes) == 0 {
		return nil, nil
	}
	if legacyBound17 {
		doc.Logger.Debug("remediation.note-namespace: a /RoleMapNS would end the legacy /RoleMap's governance of the PDF 1.7 namespace; left alone")
		return nil, nil
	}
	if ref, ok := findNamespace(ctx, alloc, doc, stDict, pdf1NamespaceURI); ok {
		rmns, _ := document.ResolveDict(ctx, doc, resolveFromAllocOrDoc(ctx, alloc, doc, ref)["RoleMapNS"]) // Best-effort: nil means no entries.
		for s := range types {
			if _, exists := rmns[s]; exists {
				doc.Logger.Debug("remediation.note-namespace: the PDF 1.7 /RoleMapNS already maps a Note type; left alone",
					"type", string(s))
				return nil, nil
			}
		}
	}

	st := stDict
	var newST ast.PdfDict
	ns20Ref, withNS := ensureNamespace(ctx, alloc, doc, st, pdf2NamespaceURI)
	if withNS != nil {
		st, newST = withNS, withNS
	}
	ns17Ref, withNS := ensureNamespace(ctx, alloc, doc, st, pdf1NamespaceURI)
	if withNS != nil {
		newST = withNS
	}
	wrote := editRoleMapNS(ctx, alloc, doc, ns17Ref.ObjectNum, func(rmns ast.PdfDict) bool {
		for s := range types {
			rmns[s] = ast.PdfArray{ast.PdfName("FENote"), ns20Ref}
		}
		return true
	})
	if !wrote {
		return nil, nil
	}
	if newST != nil {
		alloc.AllocAt(stRef.ObjectNum, newST)
	}

	bound := 0
	for _, n := range notes {
		if _, declares := n.dict["NS"]; declares {
			continue
		}
		patched := make(ast.PdfDict, len(n.dict)+1)
		maps.Copy(patched, n.dict)
		patched["NS"] = ns17Ref
		alloc.AllocAt(n.ref.ObjectNum, patched)
		bound++
	}

	fixes := []string{fmt.Sprintf("role-mapped %d PDF 1.7 Note element(s) to the PDF 2.0 FENote", len(notes))}
	if bound > 0 {
		fixes = append(fixes, fmt.Sprintf("bound %d Note element(s) declaring no namespace to the PDF 1.7 namespace", bound))
	}
	return fixes, nil
}

// legacyChainEndsOnNote reports whether a type governed by the legacy
// /RoleMap stands for the standard Note: the map applies before a standard
// name is accepted (ISO 32000-2:2020, 14.8.6), so the chain is followed to a
// type the map does not map further, and that type is Note. The walk is
// bounded by the number of entries, since each step visits a new one; a cycle
// ends on no type.
func legacyChainEndsOnNote(s ast.PdfName, legacy ast.PdfDict) bool {
	visited := map[ast.PdfName]bool{s: true}
	cur := s
	for range len(legacy) + 1 {
		next, mapped := legacy[cur].(ast.PdfName)
		if !mapped || next == cur {
			return cur == "Note"
		}
		if visited[next] {
			return false
		}
		visited[next] = true
		cur = next
	}
	return false
}

// findNamespace returns the indirect namespace dictionary for uri listed in
// the structure tree root's /Namespaces, without creating one.
func findNamespace(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, stDict ast.PdfDict, uri string) (ast.PdfIndirectRef, bool) {
	arr, err := document.ResolveArray(ctx, doc, stDict["Namespaces"])
	if err != nil {
		return ast.PdfIndirectRef{}, false
	}
	for _, item := range arr {
		ref, ok := item.(ast.PdfIndirectRef)
		if !ok {
			continue
		}
		if got, _ := document.ResolveString(ctx, doc, resolveFromAllocOrDoc(ctx, alloc, doc, ref)["NS"]); string(got) == uri {
			return ref, true
		}
	}
	return ast.PdfIndirectRef{}, false
}

// declaredNamespace returns the URI of the namespace elem's /NS names, empty
// when it names none or it cannot be read.
func declaredNamespace(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, elem ast.PdfDict) string {
	var nsDict ast.PdfDict
	if ref, ok := elem["NS"].(ast.PdfIndirectRef); ok {
		nsDict = resolveFromAllocOrDoc(ctx, alloc, doc, ref)
	} else {
		nsDict, _ = document.ResolveDict(ctx, doc, elem["NS"])
	}
	uri, _ := document.ResolveString(ctx, doc, nsDict["NS"])
	return string(uri)
}

// hasFENoteAttribute reports whether elem holds an attribute object owned by
// FENote, in its /A or in a class its /C names (ISO 32000-2:2020, 14.7.6), the
// places veraPDF reads NoteType from for ISO 14289-2, 8.2.5.14 t4.
func hasFENoteAttribute(ctx context.Context, doc *document.Document, elem, classMap ast.PdfDict) bool {
	owned := func(obj ast.PdfObject) bool {
		for _, item := range resolveStructKids(ctx, doc, obj) {
			d, err := document.ResolveDict(ctx, doc, item)
			if err != nil || d == nil {
				continue
			}
			if o, _ := document.ResolveName(ctx, doc, d["O"]); o == "FENote" {
				return true
			}
		}
		return false
	}
	if owned(elem["A"]) {
		return true
	}
	classes := ast.PdfArray{elem["C"]}
	if arr, err := document.ResolveArray(ctx, doc, elem["C"]); err == nil {
		classes = arr
	}
	for _, c := range classes {
		name, _ := document.ResolveName(ctx, doc, c)
		if name != "" && owned(classMap[name]) {
			return true
		}
	}
	return false
}
