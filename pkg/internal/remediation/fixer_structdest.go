package remediation

import (
	"context"
	"fmt"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/mutations"
)

// matchStructDest matches the PDF/UA-2 intra-document destination rules
// (ISO 14289-2 §8.8): explicit destinations and GoTo targets shall be
// structure destinations.
func matchStructDest(v compliance.Violation) bool {
	return v.Rule == compliance.Rule_14289_2_8_8_t01 || v.Rule == compliance.Rule_14289_2_8_8_t02
}

// maxOutlineFixWalk bounds the outline sibling/child walk. Anti-DoS
// guard, not spec-mandated: real outlines hold far fewer items, and the
// visited set already breaks cycles.
const maxOutlineFixWalk = 10000

// fixStructDest gives the destinations of the catalog /OpenAction, of the
// outline items and of the annotations a structure destination, which ISO
// 14289-2, 8.8 asks of every destination whose target lies in the document.
//
// A structure destination is the /SD entry of a GoTo action, beside the page
// destination under /D that a reader knowing no structure destination
// follows (ISO 32000-2, 12.6.4.2, Table 202). A GoTo action gains /SD, and a
// destination array becomes such an action. A dictionary in its place would
// not do: the catalog /OpenAction takes an array or an action dictionary (ISO
// 32000-1, 7.7.2, Table 28), and an outline item's or a link's /Dest takes an
// array, a name or a byte string and may not sit beside /A (Tables 153 and
// 173), so the item loses its /Dest once it holds the action. PDF.js reads a
// dictionary in /Dest or /OpenAction as an action and follows none without /S.
//
// /SD names the first element, in logical order, whose content begins on the
// destination page (see [mutations.StructPageTargets]), with the page
// destination's view: the element a reader following it opens on that page.
// A destination on a page no element begins on keeps its page destination
// alone rather than naming an element on another page. Without a structure
// tree nothing is rewritten.
//
// # Corpus evidence
//
//   - All corpora: the dictionaries this fixer wrote in /Dest and /OpenAction
//     cost PDF.js the target of 933 outline items, 179 links and 280 open
//     actions over 320 of 1126 tagged files remediated to PDF/UA-2. Written as
//     GoTo actions, all 4772 outline items, 1560 links and 1126 open actions
//     open the page they open in the source.
//   - All corpora: 54 destinations keep their page destination alone, where
//     the fixer named an element another page opens: 30 name no page of the
//     document, 23 open a page holding no structure content, and 1 opens a
//     page no element of the tree reaches. litho and veraPDF agree on ISO
//     14289-2, 8.8 on every output.
//   - litho annotstruct_ua2.pdf, complex_ua2.pdf, linkfont_ua2.pdf: the GoTo
//     actions of 7 of their links are direct dictionaries, which the fixer
//     left without /SD, and their outputs now meet ISO 14289-2, 8.8.
func fixStructDest(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	catalogRef, ok := catalogRefForFixer(ctx, doc)
	if !ok {
		return nil, nil
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil, nil
	}
	picker := newStructElemPicker(ctx, alloc, doc, catalogDict)
	if picker == nil {
		return nil, nil
	}

	// The catalog /OpenAction holds either a destination array or an action.
	count := 0
	if catalog, n := picker.fixOwner(ctx, catalogDict, "OpenAction", "OpenAction"); n > 0 {
		if catalog != nil {
			alloc.AllocAt(catalogRef.ObjectNum, catalog)
		}
		count += n
	}

	if outlines, err := document.ResolveDict(ctx, doc, catalogDict["Outlines"]); err == nil && outlines != nil {
		count += picker.fixOutlineLevel(ctx, outlines["First"], make(map[int]bool))
	}

	count += picker.fixAnnots(ctx)

	if count == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("gave %d destination(s) a structure destination", count)}, nil
}

// structElemPicker gives destinations the structure element their page opens
// on.
type structElemPicker struct {
	alloc *writer.ObjAllocator
	doc   *document.Document
	// targets maps a page's object number to the structure element a
	// destination on that page names (see [mutations.StructPageTargets]).
	targets map[int]ast.PdfIndirectRef
}

// newStructElemPicker returns nil when the document has no structure tree
// root, or when no element of its tree begins on a page.
func newStructElemPicker(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, catalogDict ast.PdfDict) *structElemPicker {
	root := liveDictValue(ctx, alloc, doc, catalogDict["StructTreeRoot"])
	if root == nil {
		return nil
	}
	targets := mutations.StructPageTargets(ctx, mutations.AllocResolver(ctx, alloc, doc), root["K"])
	if len(targets) == 0 {
		return nil
	}
	return &structElemPicker{alloc: alloc, doc: doc, targets: targets}
}

// structDest returns the destination array dest resolves to and the structure
// destination naming the element its page opens on, with the same view.
// ok is false when dest is no array naming a page some element begins on.
func (p *structElemPicker) structDest(ctx context.Context, dest ast.PdfObject) (_ ast.PdfArray, _ ast.PdfArray, ok bool) {
	arr, isArr := dest.(ast.PdfArray)
	if !isArr {
		resolved, err := document.ResolveObject(ctx, p.doc, dest)
		if err != nil {
			return nil, nil, false
		}
		arr, isArr = resolved.(ast.PdfArray)
	}
	if !isArr || len(arr) == 0 {
		return nil, nil, false
	}
	page, isRef := arr[0].(ast.PdfIndirectRef)
	if !isRef {
		return nil, nil, false
	}
	elem, found := p.targets[page.ObjectNum]
	if !found {
		return nil, nil, false
	}
	sd := make(ast.PdfArray, 0, len(arr))
	sd = append(sd, elem)
	return arr, append(sd, arr[1:]...), true
}

// withStructDest returns the GoTo action act with /SD naming the structure
// destination of its /D, and false when act is no GoTo action, already names
// one, or its /D names no page an element begins on.
func (p *structElemPicker) withStructDest(ctx context.Context, act ast.PdfDict) (ast.PdfDict, bool) {
	if s, _ := document.ResolveName(ctx, p.doc, act["S"]); s != "GoTo" {
		return nil, false
	}
	if present(act, "SD") {
		return nil, false
	}
	_, sd, ok := p.structDest(ctx, act["D"])
	if !ok {
		return nil, false
	}
	out := maps.Clone(act)
	out["SD"] = sd
	return out, true
}

// fixOwner gives the destination owner holds a structure destination: a
// destination array under destKey becomes a GoTo action under actionKey, and
// a GoTo action under actionKey gains /SD. destKey is empty for an owner that
// holds an action alone. It returns owner rewritten, or nil when owner keeps
// its entries, a GoTo action it names by reference being rewritten at its own
// object number, and the number of destinations given a structure
// destination.
//
// A destination beside an action stays: ISO 32000-1, Tables 153 and 173 have
// a reader follow the action, which the fixer does not replace.
func (p *structElemPicker) fixOwner(ctx context.Context, owner ast.PdfDict, destKey, actionKey ast.PdfName) (ast.PdfDict, int) {
	hasAction := present(owner, actionKey)
	if destKey != "" && (destKey == actionKey || !hasAction) {
		if dest, sd, ok := p.structDest(ctx, owner[destKey]); ok {
			out := maps.Clone(owner)
			delete(out, destKey)
			out[actionKey] = ast.PdfDict{"S": ast.PdfName("GoTo"), "D": dest, "SD": sd}
			return out, 1
		}
	}
	if !hasAction {
		return nil, 0
	}
	if ref, isRef := owner[actionKey].(ast.PdfIndirectRef); isRef {
		if p.fixGoToAction(ctx, ref) {
			return nil, 1
		}
		return nil, 0
	}
	act, isDict := owner[actionKey].(ast.PdfDict)
	if !isDict {
		return nil, 0
	}
	fixed, ok := p.withStructDest(ctx, act)
	if !ok {
		return nil, 0
	}
	out := maps.Clone(owner)
	out[actionKey] = fixed
	return out, 1
}

// fixGoToAction adds /SD to the GoTo action at ref, rewritten at its own
// object number so every owner naming it sees the change, and reports
// whether it did.
func (p *structElemPicker) fixGoToAction(ctx context.Context, ref ast.PdfIndirectRef) bool {
	if ref.ObjectNum <= 0 {
		return false
	}
	act := resolveFromAllocOrDoc(ctx, p.alloc, p.doc, ref)
	if act == nil {
		return false
	}
	fixed, ok := p.withStructDest(ctx, act)
	if !ok {
		return false
	}
	p.alloc.AllocAt(ref.ObjectNum, fixed)
	return true
}

// fixAnnots gives the Link annotations' destinations and the GoTo actions of
// every annotation's /A, and those its /AA names by reference, a structure
// destination, across all pages. ISO 32000-1, Table 173 gives only a Link
// annotation a /Dest.
func (p *structElemPicker) fixAnnots(ctx context.Context) int {
	catalog, err := document.ResolveCatalog(ctx, p.doc)
	if err != nil {
		return 0
	}
	pages, err := document.ResolvePages(ctx, p.doc, catalog)
	if err != nil {
		return 0
	}
	count := 0
	for _, page := range pages {
		if ctx.Err() != nil {
			return count
		}
		pageDict := livePageDict(ctx, p.alloc, p.doc, page)
		if pageDict == nil {
			continue
		}
		for _, aObj := range liveArray(ctx, p.alloc, p.doc, pageDict["Annots"]) {
			ref, isRef := aObj.(ast.PdfIndirectRef)
			if !isRef {
				continue // direct annot dicts cannot be re-allocated in place
			}
			annot := liveAnnotation(ctx, p.alloc, p.doc, ref)
			if annot == nil {
				continue
			}
			var destKey ast.PdfName
			if st, _ := document.ResolveName(ctx, p.doc, annot["Subtype"]); st == "Link" {
				destKey = "Dest"
			}
			if fixed, n := p.fixOwner(ctx, annot, destKey, "A"); n > 0 {
				if fixed != nil {
					p.alloc.AllocAt(ref.ObjectNum, fixed)
				}
				count += n
			}
			if aa, err := document.ResolveDict(ctx, p.doc, annot["AA"]); err == nil && aa != nil {
				for _, v := range aa {
					if actRef, isActRef := v.(ast.PdfIndirectRef); isActRef && p.fixGoToAction(ctx, actRef) {
						count++
					}
				}
			}
		}
	}
	return count
}

// fixOutlineLevel walks one outline level, giving each item's destination a
// structure destination, and recurses into children. visited breaks crafted
// /Next and /First cycles.
func (p *structElemPicker) fixOutlineLevel(ctx context.Context, obj ast.PdfObject, visited map[int]bool) int {
	count := 0
	for obj != nil && len(visited) < maxOutlineFixWalk {
		if ctx.Err() != nil {
			return count
		}
		ref, isRef := obj.(ast.PdfIndirectRef)
		if !isRef || visited[ref.ObjectNum] {
			return count
		}
		visited[ref.ObjectNum] = true
		item := resolveFromAllocOrDoc(ctx, p.alloc, p.doc, ref)
		if item == nil {
			return count
		}
		if fixed, n := p.fixOwner(ctx, item, "Dest", "A"); n > 0 {
			if fixed != nil {
				p.alloc.AllocAt(ref.ObjectNum, fixed)
			}
			count += n
		}
		count += p.fixOutlineLevel(ctx, item["First"], visited)
		obj = item["Next"]
	}
	return count
}

// present reports whether dict holds key with a value other than null, which
// ISO 32000-1, 7.3.7 makes the same as no entry.
func present(dict ast.PdfDict, key ast.PdfName) bool {
	v, ok := dict[key]
	if !ok {
		return false
	}
	_, isNull := v.(ast.PdfNull)
	return !isNull
}
