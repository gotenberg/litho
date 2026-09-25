package signature

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// maxReferenceWalk bounds how many object numbers [walkReferences] visits. It
// is an anti-DoS guard, not spec-mandated: the walk resolves each object once,
// so its work is linear in what the file references, but a crafted file can
// reference a distinct number every few bytes. 1<<20 objects is far more than
// a real document holds, and a walk that reaches the bound reports an error
// rather than a partial answer.
//
// # Corpus evidence
//
//   - pdfjs bug1978317.pdf: the largest cross-reference index of all corpora
//     holds 65,564 objects, a sixteenth of the bound.
const maxReferenceWalk = 1 << 20

// walkReferences follows every reference it meets from roots through doc,
// resolving each object once, and calls reached with the number of each
// object it walks. keep decides whether a resolved object belongs to the walk:
// one it refuses is neither reported nor walked. visited is shared between
// calls, so a walk over several roots resolves each object once and a cycle
// ends it.
//
// A reference to an object doc does not define, or whose definition does not
// parse, is reported too. ISO 32000-1, 7.3.10 reads it as the null object, and
// an update that later defines the number changes what the reference means.
//
// Direct objects are walked with an explicit stack, so their nesting costs no
// recursion, and the walk stops with an error once it has visited
// [maxReferenceWalk] numbers.
func walkReferences(ctx context.Context, doc *document.Document, roots []ast.PdfObject, keep func(ast.PdfObject) bool, visited map[int]bool, reached func(num int)) error {
	stack := slices.Clone(roots)
	for len(stack) > 0 {
		obj := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if ref, ok := obj.(ast.PdfIndirectRef); ok {
			if visited[ref.ObjectNum] {
				continue
			}
			visited[ref.ObjectNum] = true
			if len(visited) > maxReferenceWalk {
				return fmt.Errorf("the references reach more than %d objects", maxReferenceWalk)
			}
			if len(visited)%100 == 0 {
				if err := ctx.Err(); err != nil {
					return err
				}
			}
			resolved, err := document.ResolveObject(ctx, doc, ref)
			if err != nil {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return ctxErr
				}
				reached(ref.ObjectNum)
				continue
			}
			if !keep(resolved) {
				continue
			}
			reached(ref.ObjectNum)
			obj = resolved
		}
		switch v := obj.(type) {
		case ast.PdfDict:
			for _, entry := range v {
				stack = append(stack, entry)
			}
		case ast.PdfStream:
			for _, entry := range v.Dict {
				stack = append(stack, entry)
			}
		case ast.PdfArray:
			stack = append(stack, v...)
		}
	}
	return nil
}

// changedPage compares each page of the signed revision rev with the same page
// of doc and describes the first change policy does not permit, or returns ""
// when there is none. objects are the objects appended after signing.
//
// ISO 32000-1, 12.8.2.2, Table 254 permits no change at P=1, and at P=2
// filling in forms, instantiating page templates and signing, to which P=3
// adds annotation changes. None of these changes what a page already signed
// draws: a field or a signature reaches a page only as a widget annotation its
// /Annots lists (12.5.6.19, 12.7.3.1), and a template adds a page of its own
// (12.7.6). 12.8.2.2.2 lets a reader compare the signed and current versions,
// which is what this does. The classifier permits a rewritten page, and an
// untyped object such as a content stream, so a change made through either is
// found here:
//
//   - each page dictionary entry other than /Annots and /Parent must be the
//     same, which keeps an update from swapping a page's /Contents,
//     /Resources, boxes or /Rotate;
//   - no appended object may redefine an object the signed page reaches
//     through anything but its /Annots and /Parent ([pageReach]): the
//     references stay as they were signed and resolve to something else;
//   - the attributes a page inherits (7.7.3.4, Table 30: /Resources,
//     /MediaBox, /CropBox, /Rotate) must be the same, which covers a
//     rewritten page tree node or a page moved under another one;
//   - at P=2, each annotation a page lists and did not list when signed must
//     be a widget. The classifier sees an annotation the update wrote, and
//     this sees one the signed revision already held but no page listed.
//
// Pages are matched by object number. A page the update removed is left to
// [removedPage], and a page written directly in /Kids, which 7.7.3.2 forbids,
// is outside the check.
func changedPage(ctx context.Context, doc, rev *document.Document, objects []appendedObject, policy int) (string, error) {
	signedPages, err := pagesOf(ctx, rev)
	if err != nil {
		return "", fmt.Errorf("reading the signed revision's pages: %w", err)
	}
	currentPages, err := pagesOf(ctx, doc)
	if err != nil {
		return "", fmt.Errorf("reading the current pages: %w", err)
	}
	current := make(map[int]*document.Page, len(currentPages))
	for _, p := range currentPages {
		if _, dup := current[p.Ref.ObjectNum]; !dup && p.Ref.ObjectNum > 0 {
			current[p.Ref.ObjectNum] = p
		}
	}

	type pagePair struct {
		index           int
		signed, current *document.Page
	}
	var pairs []pagePair
	compared := make(map[int]bool)
	for i, sp := range signedPages {
		num := sp.Ref.ObjectNum
		cp, ok := current[num]
		if !ok || compared[num] {
			continue
		}
		compared[num] = true
		pairs = append(pairs, pagePair{i + 1, sp, cp})
	}

	added := newAddedAnnotations(doc, rev)
	for i, p := range pairs {
		if i%100 == 0 {
			if err := ctx.Err(); err != nil {
				return "", err
			}
		}
		if key := changedEntry(p.signed.Dict, p.current.Dict); key != "" {
			return fmt.Sprintf("page %d (object %d) changed its /%s, which P=%d does not permit", p.index, p.signed.Ref.ObjectNum, key, policy), nil
		}
		if policy != 2 {
			continue
		}
		if num, desc := added.first(ctx, p.signed.Dict["Annots"], p.current.Dict["Annots"]); num != 0 {
			return fmt.Sprintf("annotation %d (%s) was added to page %d, which P=2 permits only as a form field widget", num, desc, p.index), nil
		}
	}

	if violation, err := redefinedContent(ctx, doc, rev, signedPages, objects, policy); err != nil || violation != "" {
		return violation, err
	}

	for _, p := range pairs {
		if key := changedInheritance(p.signed, p.current); key != "" {
			return fmt.Sprintf("page %d (object %d) inherits a changed /%s, which P=%d does not permit", p.index, p.signed.Ref.ObjectNum, key, policy), nil
		}
	}
	return "", nil
}

// changedEntry returns the first key, in byte order, whose value differs
// between the signed page dictionary before and the current one after, and
// "" when only /Annots and /Parent do. Values are compared as written, with
// references by object number: what a reference resolves to is
// [redefinedContent]'s to compare.
//
// /Annots is left to [addedAnnotations] and [removedAnnotation]. /Parent may
// change when the page tree is rebuilt, and what a page inherits through it is
// compared by [changedInheritance].
func changedEntry(before, after ast.PdfDict) string {
	keys := slices.Collect(maps.Keys(before))
	for key := range after {
		if _, ok := before[key]; !ok {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	for _, key := range keys {
		if key == "Annots" || key == "Parent" {
			continue
		}
		if !sameObject(before[key], after[key]) {
			return string(key)
		}
	}
	return ""
}

// changedInheritance returns the first inheritable page attribute (ISO
// 32000-1, 7.7.3.4, Table 30) whose value, inherited or stated, differs
// between the signed page before and the current page after, and "" when none
// does.
func changedInheritance(before, after *document.Page) string {
	switch {
	case !sameObject(before.Resources, after.Resources):
		return "Resources"
	case !sameObject(before.MediaBox, after.MediaBox):
		return "MediaBox"
	case !sameObject(before.DeclaredCropBox, after.DeclaredCropBox):
		return "CropBox"
	case before.Rotate != after.Rotate:
		return "Rotate"
	}
	return ""
}

// addedAnnotations finds annotations other than widgets that a page lists and
// did not list in the signed revision.
//
// The work is linear in the /Annots bytes of both revisions. Each /Annots
// array is read once, and each annotation resolved once. A page's check stops
// at the first annotation its signed /Annots does not list, and at most as
// many as that array lists come before it, so a large current array that many
// pages share costs each page no more than its signed array.
type addedAnnotations struct {
	doc, rev  *document.Document
	signed    map[int]map[int]bool // indirect signed /Annots arrays to the annotations they list
	annotated map[int][]int        // indirect current /Annots arrays to the annotations they list other than widgets
	described map[int]string       // annotations to their description, "" for a widget or a non-annotation
}

// newAddedAnnotations returns an empty [addedAnnotations] comparing doc with
// its signed revision rev.
func newAddedAnnotations(doc, rev *document.Document) *addedAnnotations {
	return &addedAnnotations{
		doc: doc, rev: rev,
		signed: make(map[int]map[int]bool), annotated: make(map[int][]int), described: make(map[int]string),
	}
}

// first returns the number and description of the first annotation other
// than a widget that the current /Annots after lists and the signed /Annots
// before does not, or 0 when there is none. An entry that resolves to no
// annotation dictionary is not reported: a reader draws nothing for it.
func (a *addedAnnotations) first(ctx context.Context, before, after ast.PdfObject) (int, string) {
	listed := listedAnnotations(ctx, a.rev, before, a.signed)
	for _, num := range a.nonWidgets(ctx, after) {
		if !listed[num] {
			return num, a.described[num]
		}
	}
	return 0, ""
}

// nonWidgets returns the annotations other than widgets that the current
// /Annots value annots lists by reference, each once and in order. The result
// for an indirect array is kept under its object number.
func (a *addedAnnotations) nonWidgets(ctx context.Context, annots ast.PdfObject) []int {
	ref, isRef := annots.(ast.PdfIndirectRef)
	if isRef {
		if list, ok := a.annotated[ref.ObjectNum]; ok {
			return list
		}
	}
	var list []int
	if annots != nil {
		arr, err := document.ResolveArray(ctx, a.doc, annots)
		if err != nil {
			// An /Annots that cannot be read lists nothing a reader draws.
			a.doc.Logger.Debug("resolving the current /Annots", slog.Any("err", err))
		}
		seen := make(map[int]bool, len(arr))
		for _, entry := range arr {
			r, ok := entry.(ast.PdfIndirectRef)
			if !ok || seen[r.ObjectNum] {
				continue
			}
			seen[r.ObjectNum] = true
			if a.describe(ctx, r) != "" {
				list = append(list, r.ObjectNum)
			}
		}
	}
	if isRef {
		a.annotated[ref.ObjectNum] = list
	}
	return list
}

// describe returns the description of the annotation ref names in the current
// document, or "" when it is a widget or no annotation.
func (a *addedAnnotations) describe(ctx context.Context, ref ast.PdfIndirectRef) string {
	if desc, ok := a.described[ref.ObjectNum]; ok {
		return desc
	}
	var desc string
	dict, err := document.ResolveDict(ctx, a.doc, ref)
	if err == nil && dict != nil && isAnnotationDict(dict) {
		if subtype := dictName(dict, "Subtype"); subtype != "Widget" {
			desc = objDescription(appendedObject{IsAnnot: true, Subtype: subtype})
		}
	}
	a.described[ref.ObjectNum] = desc
	return desc
}

// redefinedContent describes the first of objects that redefines an object a
// page of the signed revision rev reaches ([pageReach]) with a definition that
// differs from the signed one, or returns "" when none does.
func redefinedContent(ctx context.Context, doc, rev *document.Document, pages []*document.Page, objects []appendedObject, policy int) (string, error) {
	var appended bool
	for _, obj := range objects {
		appended = appended || (!obj.IsXRef && obj.Unread == "")
	}
	if !appended {
		return "", nil
	}

	reach := make(map[int]int)
	visited := make(map[int]bool)
	for i, page := range pages {
		err := walkReferences(ctx, rev, pageRoots(page), pageReach, visited, func(num int) {
			if _, ok := reach[num]; !ok {
				reach[num] = i + 1
			}
		})
		if err != nil {
			return "", fmt.Errorf("walking page %d of the signed revision: %w", i+1, err)
		}
	}

	for _, obj := range objects {
		if obj.IsXRef || obj.Unread != "" {
			continue
		}
		page, ok := reach[obj.ObjNum]
		if !ok || sameDefinition(ctx, doc, rev, obj.ObjNum) {
			continue
		}
		return fmt.Sprintf("object %d (%s) redefines part of page %d, which P=%d does not permit", obj.ObjNum, objDescription(obj), page, policy), nil
	}
	return "", nil
}

// pageRoots returns where a walk over what page draws and does starts: every
// entry of its dictionary but /Annots and /Parent, and the resources it
// inherits (ISO 32000-1, 7.7.3.4).
func pageRoots(page *document.Page) []ast.PdfObject {
	roots := make([]ast.PdfObject, 0, len(page.Dict)+1)
	for key, value := range page.Dict {
		if key != "Annots" && key != "Parent" {
			roots = append(roots, value)
		}
	}
	if page.Resources != nil {
		roots = append(roots, page.Resources)
	}
	return roots
}

// pageReach reports whether a resolved object belongs to what a page draws and
// does: its content streams (ISO 32000-1, 7.8.2), the resources they draw with
// (7.8.3), and whatever else its entries name, such as its actions (12.6.3,
// Table 194), transparency group (11.6.6) or thumbnail (12.3.4). A page, a page
// tree node, the catalog, an annotation, a signature or a document timestamp
// ends the walk: each is compared or classified in its own right, and an entry
// such as a destination (12.3.2) names a page without being part of it.
func pageReach(obj ast.PdfObject) bool {
	var dict ast.PdfDict
	switch v := obj.(type) {
	case ast.PdfDict:
		dict = v
	case ast.PdfStream:
		dict = v.Dict
	default:
		return true
	}
	if isAnnotationDict(dict) {
		return false
	}
	switch dictName(dict, "Type") {
	case "Page", "Pages", "Catalog", "Sig", "DocTimeStamp":
		return false
	}
	return true
}

// sameDefinition reports whether object num is defined in doc as it was in the
// signed revision rev: an update that writes an object again unchanged does
// not change it. Dictionaries and arrays are compared with references by
// object number, and a stream by its dictionary and its bytes as stored.
func sameDefinition(ctx context.Context, doc, rev *document.Document, num int) bool {
	ref := ast.PdfIndirectRef{ObjectNum: num}
	before, err := document.ResolveObject(ctx, rev, ref)
	if err != nil {
		return false
	}
	after, err := document.ResolveObject(ctx, doc, ref)
	if err != nil {
		return false
	}
	beforeStream, ok := before.(ast.PdfStream)
	if !ok {
		return sameObject(before, after)
	}
	afterStream, ok := after.(ast.PdfStream)
	if !ok || !sameObject(beforeStream.Dict, afterStream.Dict) {
		return false
	}
	// The dictionaries agree, /Length included, so both reads are bounded by
	// the same stored length, and they happen only for an object an update
	// redefined on a signed page.
	beforeData, err := beforeStream.Bytes(ctx)
	if err != nil {
		return false
	}
	afterData, err := afterStream.Bytes(ctx)
	return err == nil && bytes.Equal(beforeData, afterData)
}
