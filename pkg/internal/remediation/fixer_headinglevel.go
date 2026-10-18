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

// matchHeadingLevel matches ISO 14289-2, 8.2.5.12 t1: a conforming file uses
// the generic H structure type.
func matchHeadingLevel(v compliance.Violation) bool {
	return v.Rule.Specification == "ISO_14289_2" && v.Rule.Clause == "8.2.5.12" && v.Rule.TestNumber == 1
}

// fixHeadingLevel renames to H1 every generic H heading that sits in no
// division, the one case where its level is fixed without judging the
// outline. ISO 14289-2, 8.2.5.12 forbids H, and the replacement is a
// numbered heading. ISO 32000-1:2008, 14.8.4.3.2, Table 335 defines H as the
// heading of the division it is the first child of, whose level a reader
// takes from the nesting of the divisions, and H1 to H6 as the same headings
// with the level written out. An H whose ancestors are only the Document, or
// that hangs off the structure tree root, is nested in no division, so the
// level a reader takes from its nesting is the first: H1 states it, and
// changes no heading level a reader conveys.
//
// An H inside any other element is left to fail t1. Which elements count as
// a level of nesting (Sect, Part, Art, Div, a list, a table cell) is a
// reading of the author's outline, not a rule, and a division holding two
// H headings (ISO/TS 32005, Table 5. Sect-H) has no level to give either.
// The repair is also refused where a role map governs H or H1: the legacy
// /RoleMap mapping either name, or the /RoleMapNS of the heading's namespace
// mapping either, since the renamed element would then stand for a type the
// file chose (ISO 32000-2:2020, 14.8.6). An H in a namespace other than the
// two PDF standard ones is not the standard H and is left alone. Only the
// /S changes: content, attributes and children stay as the file wrote them.
//
// # Corpus evidence
//
//   - verapdf PDF_UA-2__8.2.5.12-t01-fail-a: a Document whose only kid is an
//     H. Renamed H1, the file has the shape of the passing
//     PDF_UA-2__8.2.5.12-t01-pass-a, and veraPDF 1.30.2 reports nothing on
//     the PDF/UA-2 output, default and aggressive.
//   - litho headings_ua1: its three H headings sit in two Sect elements, one
//     of them holding two; all three are left H and keep failing t1.
func fixHeadingLevel(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, profile string) ([]string, error) {
	if profile != "PDF/UA-2" {
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

	var headings []structNode
	visited := map[int]bool{stRef.ObjectNum: true}
	collectTopLevelHeadings(ctx, alloc, doc, stDict["K"], legacy, visited, 0, &headings)
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("walking the structure tree for top-level H headings: %w", err)
	}

	renamed := 0
	for _, h := range headings {
		if !inStandardNamespaceUngoverned(ctx, alloc, doc, h.dict, "H", "H1") {
			doc.Logger.Debug("remediation.heading-level: an H outside the standard namespaces, or one its namespace role-maps; left alone",
				"element", h.ref.ObjectNum)
			continue
		}
		patched := make(ast.PdfDict, len(h.dict))
		maps.Copy(patched, h.dict)
		patched["S"] = ast.PdfName("H1")
		alloc.AllocAt(h.ref.ObjectNum, patched)
		renamed++
	}
	if renamed == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("renamed %d H heading(s) nested in no division to H1", renamed)}, nil
}

// collectTopLevelHeadings gathers the indirect H elements kObj lists directly,
// and those listed by the standard Document elements it lists, recursively:
// the H headings nested in no division. Every other element ends the descent.
// visited marks the elements and the indirect /K arrays walked, so a cycle or
// a repeated kid is visited once.
func collectTopLevelHeadings(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, kObj ast.PdfObject, legacy ast.PdfDict, visited map[int]bool, depth int, out *[]structNode) {
	if depth > compliance.MaxStructTreeWalkDepth || ctx.Err() != nil {
		return
	}
	if _, indirect := kObj.(ast.PdfIndirectRef); indirect {
		if _, err := document.ResolveArray(ctx, doc, kObj); err == nil && document.VisitedBefore(kObj, visited) {
			return
		}
	}
	kids := resolveStructKids(ctx, doc, kObj)
	for _, item := range kids {
		ref, ok := item.(ast.PdfIndirectRef)
		if !ok || ref.ObjectNum == 0 || visited[ref.ObjectNum] {
			continue
		}
		visited[ref.ObjectNum] = true
		elem := resolveFromAllocOrDoc(ctx, alloc, doc, ref)
		if elem == nil {
			continue
		}
		switch structType(ctx, doc, elem) {
		case "H":
			*out = append(*out, structNode{ref: ref, dict: elem})
		case "Document":
			if roleMapGoverns(legacy, "Document") || !inStandardNamespaceUngoverned(ctx, alloc, doc, elem, "Document") {
				continue
			}
			collectTopLevelHeadings(ctx, alloc, doc, elem["K"], legacy, visited, depth+1, out)
		}
	}
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
