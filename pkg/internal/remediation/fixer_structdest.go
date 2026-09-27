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
// outline items, of the pages' additional actions and of the annotations a
// structure destination, which ISO 14289-2, 8.8 asks of every destination
// whose target lies in the document.
//
// A structure destination is the /SD entry of a GoTo action, beside the page
// destination under /D that a reader knowing no structure destination
// follows (ISO 32000-2, 12.6.4.2, Table 202). A GoTo action gains /SD wherever
// it hangs, under an /AA trigger included, or after another action through
// /Next (see [structElemPicker.fixAction]), and a destination becomes such an
// action. A dictionary in its place would not do: the catalog /OpenAction
// takes an array or an action dictionary (ISO 32000-1, 7.7.2, Table 28), and
// an outline item's or a link's /Dest takes an array, a name or a byte string
// and may not sit beside /A (Tables 153 and 173), so the item loses its /Dest
// once it holds the action. PDF.js reads a dictionary in /Dest or /OpenAction
// as an action and follows none without /S.
//
// A named destination (ISO 32000-1, 12.3.2.3) keeps its name under /D, so the
// name tree stays the one table of targets, and its /SD opens the page the
// value the name maps to opens. A name whose value is a structure destination
// already is one, and stays.
//
// /SD names the first element, in logical order, whose content begins on the
// destination page (see [mutations.StructPageTargets]), with the page
// destination's view: the element a reader following it opens on that page.
// A destination on a page no element begins on keeps its page destination
// alone rather than naming an element on another page, unless the document
// has no other page (see [newStructElemPicker]). A destination naming no page
// of the document, an object outside its page tree among them, keeps it too.
// Without a structure tree nothing is rewritten.
//
// # Corpus evidence
//
//   - All corpora: the dictionaries this fixer wrote in /Dest and /OpenAction
//     cost PDF.js the target of 933 outline items, 179 links and 280 open
//     actions over 320 of 1126 tagged files remediated to PDF/UA-2. Written as
//     GoTo actions, all 4772 outline items, 1560 links and 1126 open actions
//     open the page they open in the source.
//   - All corpora: 53 destinations keep their page destination alone, where
//     the fixer named an element another page opens: 30 name no page of the
//     document and 23 open a page holding no structure content. 27 names map
//     to no destination, and the 114 destinations of 2 files whose tree holds
//     no element keep theirs too. litho and veraPDF agree on ISO 14289-2, 8.8
//     on every output.
//   - litho annotstruct_ua2.pdf, complex_ua2.pdf, linkfont_ua2.pdf: the GoTo
//     actions of 7 of their links are direct dictionaries, which the fixer
//     left without /SD, and their outputs now meet ISO 14289-2, 8.8.
//   - All corpora: of 1127 tagged files remediated to PDF/UA-2, named
//     destinations left 668 destinations without a structure destination,
//     630 under a link's or an outline item's /Dest in 11 files and 38 under
//     a GoTo action's /D in 11, and 57 /Dest entries of 4 govdocs files hold a
//     GoTo action. Given structure destinations, 8.8 fails 52 times on test 1
//     and 142 on test 2, from 740 and 180, over 14 files, from 37, default and
//     aggressive, litho and veraPDF agreeing on every file. Each of 4772
//     outline items, 1543 link destinations and 320 open actions opens in
//     PDF.js the page it opens in the source.
//   - verapdf PDF_UA-2__8.8-t01-fail-a.pdf, PDF_UA-1__7.1-t11-fail-a.pdf and
//     8 test files of PDF/A-2b and PDF/A-4 clauses 6-4-1, 6-5-2 and 6-6-3: one
//     page no element begins on, their GoTo actions under a page's or an
//     annotation's /AA among the destinations, now meet ISO 14289-2, 8.8 at
//     PDF/UA-2 and both WTPDF levels on litho and veraPDF.
//   - verapdf PDF_UA-1__7.15-t01-fail-a.pdf: its outline opens two page objects
//     outside the page tree, which it keeps, and fails 8.8 test 2 four times
//     on both tools.
//   - govdocs thread_000_000904.pdf, pdfjs issue3214.pdf: the two files that
//     run a GoTo action after another action through /Next, a page's /AA /O
//     Named action and an outline item's GoTo action. Remediated to PDF/UA-2,
//     default and aggressive, each output kept that GoTo action without /SD,
//     and veraPDF reported 8.8 test 2 once. Walked through /Next, neither
//     tool reports 8.8, and PDF.js opens every outline item of both outputs on
//     the page it opens in the source.
//   - litho gotochain_ua2.pdf: of its 7 GoTo actions lacking /SD, the 4 run
//     through /Next, one in an array, one in a loop, one after a Named action
//     and one two chains share, kept none, and both tools reported 8.8 test 2
//     four times. Now neither reports anything.
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

	count += picker.fixPages(ctx)

	if count == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("gave %d destination(s) a structure destination", count)}, nil
}

// structElemPicker gives destinations the structure element their page opens
// on. One picker serves one run of the fixer. Not safe for concurrent use.
type structElemPicker struct {
	alloc *writer.ObjAllocator
	doc   *document.Document
	// targets maps a page's object number to the structure element a
	// destination on that page names (see [mutations.StructPageTargets] and
	// [newStructElemPicker]).
	targets map[int]ast.PdfIndirectRef
	// walked holds the object numbers of the actions, and of the /Next arrays,
	// the run has walked (see [structElemPicker.fixAction]).
	walked map[int]bool
}

// newStructElemPicker returns nil when the document has no structure tree
// root, or when no element of its tree opens on a page.
//
// A page no element begins on takes no element, since a reader following one
// would open the page its content begins on. The one page of a document of one
// page is the exception: no element begins on it only when the tree holds no
// content at all, and the tree's first element, which a reader can open on no
// other page, stands for it.
func newStructElemPicker(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, catalogDict ast.PdfDict) *structElemPicker {
	root := liveDictValue(ctx, alloc, doc, catalogDict["StructTreeRoot"])
	if root == nil {
		return nil
	}
	resolve := mutations.AllocResolver(ctx, alloc, doc)
	targets := mutations.StructPageTargets(ctx, resolve, root["K"])
	if page, ok := onlyPage(ctx, doc); ok {
		if _, taken := targets[page.ObjectNum]; !taken {
			if elem, found := firstElement(resolve, root["K"]); found {
				targets[page.ObjectNum] = elem
			}
		}
	}
	if len(targets) == 0 {
		return nil
	}
	return &structElemPicker{alloc: alloc, doc: doc, targets: targets, walked: make(map[int]bool)}
}

// onlyPage returns the page of a document of one page, and false for any other
// document.
func onlyPage(ctx context.Context, doc *document.Document) (ast.PdfIndirectRef, bool) {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		doc.Logger.Debug("struct-dest: cannot resolve the catalog", "error", err)
		return ast.PdfIndirectRef{}, false
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		doc.Logger.Debug("struct-dest: cannot resolve the page tree", "error", err)
		return ast.PdfIndirectRef{}, false
	}
	if len(pages) != 1 {
		return ast.PdfIndirectRef{}, false
	}
	return pages[0].Ref, true
}

// firstElement returns the first kid of the structure tree root's /K that is a
// structure element written as an object of its own, the form a structure
// destination names (ISO 32000-2, 12.3.2.3), read through resolve.
func firstElement(resolve func(ast.PdfObject) ast.PdfObject, rootK ast.PdfObject) (ast.PdfIndirectRef, bool) {
	kids, isArr := resolve(rootK).(ast.PdfArray)
	if !isArr {
		kids = ast.PdfArray{rootK}
	}
	for _, kid := range kids {
		ref, isRef := kid.(ast.PdfIndirectRef)
		if !isRef {
			continue
		}
		elem, isDict := resolve(ref).(ast.PdfDict)
		if !isDict {
			continue
		}
		// ISO 32000-1, 14.7.2, Table 323: /S names the element's structure type.
		if _, isName := resolve(elem["S"]).(ast.PdfName); isName {
			return ref, true
		}
	}
	return ast.PdfIndirectRef{}, false
}

// structDest returns the destination dest stands for, as a GoTo action
// replacing it holds under /D, and the structure destination naming the
// element its page opens on, with the same view. A name or a string stays the
// named destination it is (ISO 32000-1, 12.3.2.3), whose page is the one the
// value it maps to opens. ok is false when dest opens no page some element
// opens on, or names a value that is a structure destination already.
func (p *structElemPicker) structDest(ctx context.Context, dest ast.PdfObject) (_ ast.PdfObject, _ ast.PdfArray, ok bool) {
	resolved, err := document.ResolveObject(ctx, p.doc, dest)
	if err != nil {
		return nil, nil, false
	}
	var arr ast.PdfArray
	switch v := resolved.(type) {
	case ast.PdfArray:
		arr = v
	case ast.PdfName, ast.PdfString:
		if mutations.DestStructDest(ctx, p.doc, v) != nil {
			return nil, nil, false
		}
		arr, err = document.ResolveNamedDest(ctx, p.doc, namedDestKey(v))
		if err != nil {
			p.doc.Logger.Debug("struct-dest: cannot resolve a named destination", "error", err)
			return nil, nil, false
		}
	default:
		return nil, nil, false
	}
	if len(arr) == 0 {
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
	return resolved, append(sd, arr[1:]...), true
}

// namedDestKey returns the key a named destination looks its value up by: a
// name's characters or a string's bytes.
func namedDestKey(name ast.PdfObject) string {
	switch v := name.(type) {
	case ast.PdfName:
		return string(v)
	case ast.PdfString:
		return string(v)
	}
	return ""
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

// destAction returns the GoTo action with a structure destination that
// replaces the destination dest, and the number of destinations it gave a
// structure destination, 0 when there is none.
//
// A dictionary is no destination: ISO 32000-1, Tables 153 and 173 allow an
// array, a name or a byte string under /Dest. Producers write a GoTo action
// there, or a dictionary holding the destination under /D as a named
// destination's value does (12.3.2.3), and readers follow its /D. It becomes
// the GoTo action it stands for when inDest, the actions its /Next runs given
// theirs too (see [structElemPicker.fixAction]), and stays when dest is the
// catalog /OpenAction, where a dictionary is the action itself (Table 28).
func (p *structElemPicker) destAction(ctx context.Context, dest ast.PdfObject, inDest bool) (ast.PdfDict, int) {
	resolved, err := document.ResolveObject(ctx, p.doc, dest)
	if err != nil {
		return nil, 0
	}
	if dict, isDict := resolved.(ast.PdfDict); isDict {
		if !inDest {
			return nil, 0
		}
		if s, _ := document.ResolveName(ctx, p.doc, dict["S"]); s != "" && s != "GoTo" {
			return nil, 0
		}
		act := maps.Clone(dict)
		act["S"] = ast.PdfName("GoTo")
		fixed, ok := p.withStructDest(ctx, act)
		if !ok {
			return nil, 0
		}
		chained, n := p.fixActionDict(ctx, fixed, 0)
		if chained == nil {
			chained = fixed
		}
		return chained, 1 + n
	}
	d, sd, ok := p.structDest(ctx, resolved)
	if !ok {
		return nil, 0
	}
	return ast.PdfDict{"S": ast.PdfName("GoTo"), "D": d, "SD": sd}, 1
}

// fixOwner gives the destination owner holds a structure destination: a
// destination under destKey becomes a GoTo action under actionKey (see
// [structElemPicker.destAction]), and the GoTo actions of the action under
// actionKey gain /SD (see [structElemPicker.fixAction]). destKey is empty for
// an owner that holds an action alone. It returns owner rewritten, or nil when
// owner keeps its entries, an action it names by reference being rewritten at
// its own object number, and the number of destinations given a structure
// destination.
//
// A destination beside an action stays: ISO 32000-1, Tables 153 and 173 have
// a reader follow the action, which the fixer does not replace.
func (p *structElemPicker) fixOwner(ctx context.Context, owner ast.PdfDict, destKey, actionKey ast.PdfName) (ast.PdfDict, int) {
	hasAction := present(owner, actionKey)
	if destKey != "" && (destKey == actionKey || !hasAction) {
		if act, n := p.destAction(ctx, owner[destKey], destKey != actionKey); act != nil {
			out := maps.Clone(owner)
			delete(out, destKey)
			out[actionKey] = act
			return out, n
		}
	}
	if !hasAction {
		return nil, 0
	}
	fixed, n := p.fixAction(ctx, owner[actionKey], 0)
	if fixed == nil {
		return nil, n
	}
	out := maps.Clone(owner)
	out[actionKey] = fixed
	return out, n
}

// fixAction gives the GoTo actions of the action tree obj heads a structure
// destination, by the rules a GoTo action an owner holds itself follows (see
// [structElemPicker.withStructDest]), and leaves every other action's entries
// but its /Next as they are. An action's /Next holds one action or an array
// of them, run after it (ISO 32000-1, 12.6.2, Table 193), and ISO 14289-2, 8.8
// covers every destination whose target lies in the document, the one a GoTo
// action run after another opens among them.
//
// An action or a /Next array written as an object of its own is rewritten at
// its own object number, so every owner naming it sees the change, and a
// direct one comes back rewritten for its owner to hold, or nil when it keeps
// its entries. Each object is walked once a run: a /Next that loops back ends
// there, an action several owners share gains /SD once, and a /Next array
// whose direct actions each name it again cannot fan out 2^depth. The depth
// is capped at [maxActionChainDepth], as deep as compliance judges a chain.
// It returns the number of GoTo actions given a structure destination.
func (p *structElemPicker) fixAction(ctx context.Context, obj ast.PdfObject, depth int) (ast.PdfDict, int) {
	if ctx.Err() != nil || depth > maxActionChainDepth {
		return nil, 0
	}
	ref, isRef := obj.(ast.PdfIndirectRef)
	if isRef {
		if ref.ObjectNum <= 0 || p.walked[ref.ObjectNum] {
			return nil, 0
		}
		p.walked[ref.ObjectNum] = true
	}
	act := liveDictValue(ctx, p.alloc, p.doc, obj)
	if act == nil {
		return nil, 0
	}
	fixed, n := p.fixActionDict(ctx, act, depth)
	if fixed == nil || !isRef {
		return fixed, n
	}
	p.alloc.AllocAt(ref.ObjectNum, fixed)
	return nil, n
}

// fixActionDict returns the action act with /SD when it is a GoTo action that
// takes one, and with its /Next rewritten when a direct action in it changed,
// or nil when act keeps its entries, and the number of GoTo actions of act's
// tree given a structure destination (see [structElemPicker.fixAction]).
func (p *structElemPicker) fixActionDict(ctx context.Context, act ast.PdfDict, depth int) (ast.PdfDict, int) {
	out, ok := p.withStructDest(ctx, act)
	count := 0
	if ok {
		count++
	}
	nextObj, hasNext := act["Next"]
	if !hasNext {
		return out, count
	}
	next, m := p.fixNext(ctx, nextObj, depth+1)
	count += m
	if next == nil {
		return out, count
	}
	if out == nil {
		out = maps.Clone(act)
	}
	out["Next"] = next
	return out, count
}

// fixNext gives the GoTo actions of the /Next value nextObj a structure
// destination (see [structElemPicker.fixAction]). It returns the value
// rewritten for the action holding it, or nil when the action keeps it, and
// the number of GoTo actions given a structure destination.
func (p *structElemPicker) fixNext(ctx context.Context, nextObj ast.PdfObject, depth int) (ast.PdfObject, int) {
	arr, isArr := nextObj.(ast.PdfArray)
	ref, isRef := nextObj.(ast.PdfIndirectRef)
	if isRef {
		arr = liveArray(ctx, p.alloc, p.doc, ref)
		isArr = arr != nil
	}
	if !isArr {
		fixed, n := p.fixAction(ctx, nextObj, depth)
		if fixed == nil {
			return nil, n
		}
		return fixed, n
	}
	if !isRef {
		return p.fixActionArray(ctx, arr, depth)
	}
	if ref.ObjectNum <= 0 || p.walked[ref.ObjectNum] {
		return nil, 0
	}
	p.walked[ref.ObjectNum] = true
	fixed, n := p.fixActionArray(ctx, arr, depth)
	if fixed != nil {
		p.alloc.AllocAt(ref.ObjectNum, fixed)
	}
	return nil, n
}

// fixActionArray gives the GoTo actions of the actions arr lists a structure
// destination (see [structElemPicker.fixAction]). It returns arr rewritten,
// or nil when every entry keeps its value, and the number of GoTo actions
// given a structure destination.
func (p *structElemPicker) fixActionArray(ctx context.Context, arr ast.PdfArray, depth int) (ast.PdfObject, int) {
	var out ast.PdfArray
	count := 0
	for i, item := range arr {
		fixed, n := p.fixAction(ctx, item, depth)
		count += n
		if fixed == nil {
			continue
		}
		if out == nil {
			out = slices.Clone(arr)
		}
		out[i] = fixed
	}
	if out == nil {
		return nil, count
	}
	return out, count
}

// fixPages gives, across all pages, the GoTo actions of the page's /AA
// triggers, the Link annotations' destinations, and the GoTo actions of every
// annotation's /A and /AA a structure destination. ISO 32000-1, Table 173
// gives only a Link annotation a /Dest.
func (p *structElemPicker) fixPages(ctx context.Context) int {
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
		// ISO 32000-1, Table 195 gives a page's /AA the /O and /C triggers alone.
		if aa, n := p.fixAdditionalActions(ctx, pageDict["AA"], []ast.PdfName{"O", "C"}); n > 0 {
			if aa != nil {
				pageDict = maps.Clone(pageDict)
				pageDict["AA"] = aa
				p.alloc.AllocAt(page.Ref.ObjectNum, pageDict)
			}
			count += n
		}
		for _, aObj := range liveArray(ctx, p.alloc, p.doc, pageDict["Annots"]) {
			ref, isRef := aObj.(ast.PdfIndirectRef)
			if !isRef {
				continue // direct annot dicts cannot be re-allocated in place
			}
			count += p.fixAnnot(ctx, ref)
		}
	}
	return count
}

// fixAnnot gives the annotation at ref its destinations' structure
// destinations, and returns how many it gave.
func (p *structElemPicker) fixAnnot(ctx context.Context, ref ast.PdfIndirectRef) int {
	annot := liveAnnotation(ctx, p.alloc, p.doc, ref)
	if annot == nil {
		return 0
	}
	var destKey ast.PdfName
	if st, _ := document.ResolveName(ctx, p.doc, annot["Subtype"]); st == "Link" {
		destKey = "Dest"
	}
	count := 0
	if fixed, n := p.fixOwner(ctx, annot, destKey, "A"); n > 0 {
		if fixed != nil {
			annot = fixed
			p.alloc.AllocAt(ref.ObjectNum, annot)
		}
		count += n
	}
	// Every /AA entry of an annotation is a trigger (ISO 32000-1, Table 194).
	if aa, n := p.fixAdditionalActions(ctx, annot["AA"], nil); n > 0 {
		if aa != nil {
			annot = maps.Clone(annot)
			annot["AA"] = aa
			p.alloc.AllocAt(ref.ObjectNum, annot)
		}
		count += n
	}
	return count
}

// fixAdditionalActions gives the GoTo actions of the additional-actions
// dictionary aaObj a structure destination, those under triggers alone when
// triggers is not nil, the actions each trigger's /Next runs included (see
// [structElemPicker.fixAction]). An action written by reference, and a
// dictionary written by reference, are rewritten at their own object number.
// It returns the dictionary rewritten, or nil when the owner keeps its /AA,
// and the number of actions given a structure destination.
func (p *structElemPicker) fixAdditionalActions(ctx context.Context, aaObj ast.PdfObject, triggers []ast.PdfName) (ast.PdfDict, int) {
	aa := liveDictValue(ctx, p.alloc, p.doc, aaObj)
	if aa == nil {
		return nil, 0
	}
	count := 0
	var out ast.PdfDict
	for _, key := range slices.Sorted(maps.Keys(aa)) {
		if triggers != nil && !slices.Contains(triggers, key) {
			continue
		}
		fixed, n := p.fixAction(ctx, aa[key], 0)
		count += n
		if fixed == nil {
			continue
		}
		if out == nil {
			out = maps.Clone(aa)
		}
		out[key] = fixed
	}
	if out == nil {
		return nil, count
	}
	if ref, isRef := aaObj.(ast.PdfIndirectRef); isRef && ref.ObjectNum > 0 {
		p.alloc.AllocAt(ref.ObjectNum, out)
		return nil, count
	}
	return out, count
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
