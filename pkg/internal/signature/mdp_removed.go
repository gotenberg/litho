package signature

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// signedAnnotation is an annotation a page of the signed revision lists.
type signedAnnotation struct {
	num  int
	desc string
}

// removedAnnotation compares each page's /Annots in the signed revision rev
// with the same page in the current document, and describes the first
// annotation that is no longer there. It returns "" when none was removed.
//
// ISO 32000-1, 12.8.2.2, Table 254 permits annotation deletion only at P=3,
// and deleting one appends no annotation: it rewrites the page's /Annots, or
// frees the annotation's object, so a classifier of appended objects never
// sees it. 12.8.2.2.2 lets a reader compare the signed and current versions,
// which is what this does. A widget annotation is not reported: widgets carry
// the form's fields (12.5.6.19), and P=2 permits filling in forms and
// instantiating page templates, which rearrange them.
//
// Pages are matched by object number, so a page the update removed from the
// tree is outside this check, as is a page written directly in /Kids (7.7.3.2
// requires references) and an annotation written directly in /Annots (Table
// 30 requires references).
//
// The work is linear in the /Annots bytes of both revisions, whatever the
// file shares: a page is compared once however often the tree lists it, a
// pair of shared /Annots arrays once however many pages share them, and each
// array is read once. A page's check stops at its first missing annotation,
// so it costs at most one step more than the entries its current /Annots
// holds.
func removedAnnotation(ctx context.Context, doc, rev *document.Document) (string, error) {
	signedPages, err := pagesOf(ctx, rev)
	if err != nil {
		return "", fmt.Errorf("reading the signed revision's pages: %w", err)
	}
	currentPages, err := pagesOf(ctx, doc)
	if err != nil {
		return "", fmt.Errorf("reading the current pages: %w", err)
	}
	current := make(map[int]ast.PdfDict, len(currentPages))
	for _, p := range currentPages {
		if _, dup := current[p.Ref.ObjectNum]; !dup && p.Ref.ObjectNum > 0 {
			current[p.Ref.ObjectNum] = p.Dict
		}
	}

	signedLists := make(map[int][]signedAnnotation)
	currentSets := make(map[int]map[int]bool)
	comparedPairs := make(map[[2]int]bool)
	comparedPages := make(map[int]bool)
	for i, sp := range signedPages {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if comparedPages[sp.Ref.ObjectNum] {
			continue
		}
		comparedPages[sp.Ref.ObjectNum] = true
		cur, ok := current[sp.Ref.ObjectNum]
		if !ok {
			continue
		}
		before, after := sp.Dict["Annots"], cur["Annots"]
		beforeRef, beforeIsRef := before.(ast.PdfIndirectRef)
		afterRef, afterIsRef := after.(ast.PdfIndirectRef)
		if beforeIsRef && afterIsRef {
			pair := [2]int{beforeRef.ObjectNum, afterRef.ObjectNum}
			if comparedPairs[pair] {
				continue
			}
			comparedPairs[pair] = true
		}

		candidates := nonWidgetAnnotations(ctx, rev, before, signedLists)
		if len(candidates) == 0 {
			continue
		}
		listed := listedAnnotations(ctx, doc, after, currentSets)
		for _, a := range candidates {
			if !listed[a.num] || !annotationExists(ctx, doc, a.num) {
				return fmt.Sprintf("annotation %d (%s) was removed from page %d, which P=2 does not permit", a.num, a.desc, i+1), nil
			}
		}
	}
	return "", nil
}

// pagesOf returns the pages of doc's page tree.
func pagesOf(ctx context.Context, doc *document.Document) ([]*document.Page, error) {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return nil, err
	}
	return document.ResolvePages(ctx, doc, catalog)
}

// nonWidgetAnnotations returns the annotations an /Annots value of the signed
// revision lists by reference, widgets left out, each once. The result for an
// indirect array is kept in cache under its object number.
func nonWidgetAnnotations(ctx context.Context, rev *document.Document, annots ast.PdfObject, cache map[int][]signedAnnotation) []signedAnnotation {
	ref, isRef := annots.(ast.PdfIndirectRef)
	if isRef {
		if list, ok := cache[ref.ObjectNum]; ok {
			return list
		}
	}
	var list []signedAnnotation
	if annots != nil {
		arr, err := document.ResolveArray(ctx, rev, annots)
		if err != nil {
			// An /Annots the signed revision cannot read lists nothing
			// this check can compare.
			rev.Logger.Debug("resolving the signed revision's /Annots", slog.Any("err", err))
		}
		seen := make(map[int]bool, len(arr))
		for _, entry := range arr {
			r, ok := entry.(ast.PdfIndirectRef)
			if !ok || seen[r.ObjectNum] {
				continue
			}
			seen[r.ObjectNum] = true
			dict, err := document.ResolveDict(ctx, rev, r)
			if err != nil || dict == nil {
				continue
			}
			subtype := dictName(dict, "Subtype")
			if subtype == "Widget" {
				continue
			}
			list = append(list, signedAnnotation{num: r.ObjectNum, desc: objDescription(appendedObject{IsAnnot: true, Subtype: subtype})})
		}
	}
	if isRef {
		cache[ref.ObjectNum] = list
	}
	return list
}

// listedAnnotations returns the object numbers an /Annots value of the current
// document lists by reference. The result for an indirect array is kept in
// cache under its object number.
func listedAnnotations(ctx context.Context, doc *document.Document, annots ast.PdfObject, cache map[int]map[int]bool) map[int]bool {
	ref, isRef := annots.(ast.PdfIndirectRef)
	if isRef {
		if set, ok := cache[ref.ObjectNum]; ok {
			return set
		}
	}
	var arr ast.PdfArray
	if annots != nil {
		var err error
		arr, err = document.ResolveArray(ctx, doc, annots)
		if err != nil {
			// An unreadable /Annots lists no annotation, so every one the
			// signed page listed reads as removed.
			doc.Logger.Debug("resolving the current /Annots", slog.Any("err", err))
		}
	}
	set := make(map[int]bool, len(arr))
	for _, entry := range arr {
		if r, ok := entry.(ast.PdfIndirectRef); ok {
			set[r.ObjectNum] = true
		}
	}
	if isRef {
		cache[ref.ObjectNum] = set
	}
	return set
}

// annotationExists reports whether the current document still defines the
// annotation num as a dictionary. An update can delete an annotation by
// freeing its object in a cross-reference section, or by redefining it as
// null, while the page still lists the reference: 7.3.10 reads a reference to
// either as the null object.
func annotationExists(ctx context.Context, doc *document.Document, num int) bool {
	dict, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: num})
	return err == nil && dict != nil
}
