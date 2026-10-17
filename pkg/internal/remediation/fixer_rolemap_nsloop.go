package remediation

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// nsRoleEntry names one /RoleMapNS entry: the namespace object that holds it
// and the type it maps.
type nsRoleEntry struct {
	ns   int
	name ast.PdfName
}

// nsLoopRedirect is what [repairNamespaceRoleLoops] knows of an entry it
// redirects: the standard type the chain past it resolves to, empty for none,
// the URIs of the namespaces the chains running through it left before it, the
// entry's own included, whether one of those chains is a cycle, and whether an
// element whose chain runs through it holds a content item directly.
type nsLoopRedirect struct {
	resolves ast.PdfName
	left     map[string]bool
	cycle    bool
	content  bool
}

// repairNamespaceRoleLoops breaks every /RoleMapNS loop a structure element's
// type closes, as ISO 14289-2, 8.2.4 judges it: a chain that comes back to the
// very type it left a namespace from is circular (t2), and one that comes back
// to another type of that namespace maps a type within its own namespace
// (t3). Both leave the element on no standard type (t1). It returns the fix
// descriptions and, when a redirect had to list a namespace the root's
// /Namespaces lacked, the updated root dictionary; otherwise that result is
// nil.
//
// The repair follows the rule [fixRoleMap] applies to the legacy /RoleMap,
// which invents no semantics:
//
//   - a loop through a type a standard namespace defines loses that type's
//     entry, so the type keeps its own meaning and every chain that reached
//     it stops there, on a standard type (ISO 32000-2:2020, 14.8.6: a mapping
//     ends on the first standard type it meets);
//   - a loop of types no standard namespace defines has the entry that closes
//     it redirected to a type of a standard namespace the chain has not left,
//     the PDF 2.0 one, or else the PDF 1.7 one, so no type is mapped back into
//     a namespace it left (t3). The type is the standard type the chain past
//     the entry resolves to, when the target namespace defines it, which is
//     the meaning the producer declared (a same-namespace /Book to /Sect keeps
//     its Sect), and otherwise, for a chain that cycles and so never meets a
//     standard type at all, the neutral
//     grouping type Div (ISO 32000-2:2020, Table 364), or P when an element
//     whose chain runs through the entry holds a content item directly, as
//     the legacy /RoleMap repair does: Div would claim a group of content, P
//     claims the content itself.
//
// Only loops an element reaches are repaired, since each rule judges an
// element: an entry no element's chain runs through is left as the file wrote
// it. An entry held in a namespace that is a direct dictionary, or a namespace
// whose /RoleMapNS is not a dictionary, is left alone: there is no object to
// rewrite it in. So is a loop whose chain has left both the PDF 2.0 and the
// PDF 1.7 namespace before its closing entry, which no redirect can end
// outside a namespace it left, and a chain that ends on no standard type
// without cycling: it names a type, only not a standard one, and giving it Div
// would replace the producer's word with one it never wrote.
//
// # Corpus evidence
//
//   - All corpora: 15 files declare a /RoleMapNS and 5 of them hold an element
//     whose chain comes back to a namespace it left; remediation at PDF/UA-2
//     and both WTPDF levels, default and aggressive, changes the output of 4
//     of them alone.
//   - litho rolecycle_ua2: a Callout whose chain runs through a second
//     namespace and back, and a Panel mapped to a Frame of its own namespace
//     and back. Every output kept 8.2.4 t1, t2 and t3 on both tools, since no
//     fixer edited a /RoleMapNS; with Box and Panel sent to Div, veraPDF finds
//     each output clean.
//   - verapdf PDF_UA-2__8.2.4-t03-fail-b: maps a PDF 2.0 /Q to an XHTML /T and
//     /T back to the PDF 2.0 /P. Redirecting /T into a namespace other than its
//     own still sent it into the PDF 2.0 one the chain had left, so veraPDF kept
//     reporting t3; /T now maps to the PDF 1.7 /P, and every output is clean.
//   - verapdf PDF_UA-2__8.2.4-t02-fail-c: veraPDF reported t2 on every output
//     and finds each clean now.
//   - verapdf PDF_UA-2__8.2.4-t03-fail-a: maps an XHTML /Q to the XHTML /P,
//     which maps on to the PDF 2.0 /P. /Q now maps to the PDF 2.0 /P straight,
//     and veraPDF, which reported t3 on every output, finds each clean.
//   - litho complex_ua2: maps a private /Alpha to a /Beta of its own namespace
//     that nothing maps further. Both tools keep reporting t3 and t1 there, by
//     design: sending the entry to Div cleared both, and invented the meaning.
func repairNamespaceRoleLoops(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, stRef ast.PdfIndirectRef, stDict ast.PdfDict) ([]string, ast.PdfDict) {
	var elems []structNode
	visited := map[int]bool{stRef.ObjectNum: true}
	walkStructElemsWhere(ctx, alloc, doc, stDict["K"], func(ast.PdfDict) bool { return true }, visited, 0, &elems)

	drops := make(map[nsRoleEntry]bool)
	redirects := make(map[nsRoleEntry]nsLoopRedirect)
	for _, elem := range elems {
		if ctx.Err() != nil {
			return nil, nil
		}
		ret, ok := compliance.NamespaceRoleLoop(ctx, doc, elem.dict)
		if !ok {
			continue
		}
		hops := ret.Hops
		closers := hops[ret.Loop : len(hops)-1]
		if slices.ContainsFunc(closers, func(h compliance.NamespaceRoleHop) bool { return h.NS.ObjectNum <= 0 }) {
			doc.Logger.Debug("remediation.rolemap: /RoleMapNS loop runs through a direct namespace; left alone",
				"element", elem.ref.ObjectNum)
			continue
		}
		standard := false
		for _, h := range closers {
			if h.Standard {
				drops[nsRoleEntry{ns: h.NS.ObjectNum, name: h.Name}] = true
				standard = true
			}
		}
		if standard {
			continue
		}
		last := closers[len(closers)-1]
		key := nsRoleEntry{ns: last.NS.ObjectNum, name: last.Name}
		r := redirects[key]
		r.resolves = ret.Resolves
		if r.left == nil {
			r.left = make(map[string]bool)
		}
		for _, h := range hops[:len(hops)-1] {
			r.left[h.NSURI] = true
		}
		r.cycle = r.cycle || ret.Cycle
		r.content = r.content || holdsContentItem(ctx, doc, elem.dict)
		redirects[key] = r
	}
	if len(drops) == 0 && len(redirects) == 0 {
		return nil, nil
	}

	perNS := make(map[int][]ast.PdfName)
	for e := range drops {
		perNS[e.ns] = append(perNS[e.ns], e.name)
	}
	for e := range redirects {
		perNS[e.ns] = append(perNS[e.ns], e.name)
	}

	st := stDict
	var newST ast.PdfDict
	dropped, redirected := 0, 0
	for _, num := range slices.Sorted(maps.Keys(perNS)) {
		names := perNS[num]
		slices.Sort(names)
		editRoleMapNS(ctx, alloc, doc, num, func(rmns ast.PdfDict) bool {
			changed := false
			for _, name := range names {
				if _, ok := rmns[name]; !ok {
					continue
				}
				e := nsRoleEntry{ns: num, name: name}
				if drops[e] {
					delete(rmns, name)
					dropped++
					changed = true
					continue
				}
				r := redirects[e]
				targetURI := pdf2NamespaceURI
				if r.left[targetURI] {
					targetURI = pdf1NamespaceURI
				}
				if r.left[targetURI] {
					doc.Logger.Debug("remediation.rolemap: /RoleMapNS loop left both standard namespaces; left alone",
						"namespace", num, "type", string(name))
					continue
				}
				target, ok := r.target(targetURI)
				if !ok {
					continue
				}
				targetRef, withNS := ensureNamespace(ctx, alloc, doc, st, targetURI)
				if withNS != nil {
					st, newST = withNS, withNS
				}
				rmns[name] = ast.PdfArray{target, targetRef}
				redirected++
				changed = true
			}
			return changed
		})
	}

	var fixes []string
	if dropped > 0 {
		fixes = append(fixes, fmt.Sprintf("removed %d /RoleMapNS entr(ies) of standard types that closed a role-map loop", dropped))
	}
	if redirected > 0 {
		fixes = append(fixes, fmt.Sprintf("redirected %d /RoleMapNS entr(ies) that closed a role-map loop to a standard type", redirected))
	}
	return fixes, newST
}

// target returns the type a redirected entry maps to in the standard namespace
// targetURI: the standard type the chain past it resolves to, when targetURI
// defines it, and otherwise, for an entry a cycle runs through, Div, or P for
// an entry an element holding content runs through
// ([repairNamespaceRoleLoops]). ok is false when the entry is to stay.
func (r nsLoopRedirect) target(targetURI string) (ast.PdfName, bool) {
	if r.resolves != "" && compliance.IsStandardStructTypeInNS(r.resolves, targetURI) {
		return r.resolves, true
	}
	if !r.cycle {
		return "", false
	}
	if r.content {
		return "P", true
	}
	return "Div", true
}

// editRoleMapNS rewrites the /RoleMapNS of the namespace object num: edit
// changes a copy of it, empty when the namespace has none, and reports whether
// it changed it; the changed copy is written back where the dictionary lives,
// in its own object when /RoleMapNS is an indirect reference and in the
// namespace dictionary otherwise. It reports whether it wrote.
//
// A namespace whose /RoleMapNS is present but no dictionary is left alone:
// writing one over it would replace a value the file wrote rather than extend
// it.
func editRoleMapNS(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, num int, edit func(ast.PdfDict) bool) bool {
	nsDict := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: num})
	if nsDict == nil {
		return false
	}
	rmObj, hasRM := nsDict["RoleMapNS"]
	rmRef, rmIndirect := rmObj.(ast.PdfIndirectRef)
	var rmns ast.PdfDict
	if rmIndirect {
		rmns = resolveFromAllocOrDoc(ctx, alloc, doc, rmRef)
	} else if hasRM {
		rmns, _ = rmObj.(ast.PdfDict)
	}
	if hasRM && rmns == nil {
		doc.Logger.Debug("remediation.rolemap: namespace /RoleMapNS is no dictionary; left alone",
			"namespace", num)
		return false
	}
	newRMNS := make(ast.PdfDict, len(rmns))
	maps.Copy(newRMNS, rmns)
	if !edit(newRMNS) {
		return false
	}
	if rmIndirect && rmRef.ObjectNum > 0 {
		alloc.AllocAt(rmRef.ObjectNum, newRMNS)
		return true
	}
	newNS := make(ast.PdfDict, len(nsDict))
	maps.Copy(newNS, nsDict)
	newNS["RoleMapNS"] = newRMNS
	alloc.AllocAt(num, newNS)
	return true
}

// holdsContentItem reports whether elem's /K holds a content item directly: a
// marked-content identifier, a marked-content reference or an object reference
// (ISO 32000-2:2020, 14.7.5).
func holdsContentItem(ctx context.Context, doc *document.Document, elem ast.PdfDict) bool {
	kids := ast.PdfArray{elem["K"]}
	if arr, err := document.ResolveArray(ctx, doc, elem["K"]); err == nil {
		kids = arr
	}
	for _, kid := range kids {
		if _, ok := kid.(ast.PdfInt); ok {
			return true
		}
		if ref, ok := kid.(ast.PdfIndirectRef); ok && ref.ObjectNum > 0 {
			// An indirect kid is a structure element unless it is a
			// reference dictionary.
			d, err := document.ResolveDict(ctx, doc, ref)
			if err != nil || d == nil {
				continue
			}
			if isContentReference(ctx, doc, d) {
				return true
			}
			continue
		}
		if d, ok := kid.(ast.PdfDict); ok && isContentReference(ctx, doc, d) {
			return true
		}
	}
	return false
}

// isContentReference reports whether d is a marked-content reference or an
// object reference (ISO 32000-2:2020, Tables 357 and 358).
func isContentReference(ctx context.Context, doc *document.Document, d ast.PdfDict) bool {
	t, _ := document.ResolveName(ctx, doc, d["Type"])
	return t == "MCR" || t == "OBJR"
}
