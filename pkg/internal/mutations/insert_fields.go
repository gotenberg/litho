package mutations

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// mergeInsertedFields lists in the target's interactive form the fields whose
// widgets the inserted pages brought.
//
// ISO 32000-1, 12.7.2 has /AcroForm /Fields name the document's root fields, and
// a widget presents a field of that form (12.5.6.19). The copier already brought
// each field along with its widget, through /Parent, but a field /Fields does
// not reach is outside the interactive form: qpdf reports its widget as not
// reachable from /AcroForm, and a viewer leaves it out of the form. The roots are
// collected from the copied widgets, as split and merge collect them (see
// [rebuildAcroForm]), so a field whose widgets stayed behind is never listed.
//
// Signature fields are listed too. The Writer's full rewrite already keeps the
// target's own signature dictionaries while moving every byte their /ByteRange
// names, and a copied one is kept on the same terms; dropping it from the form
// would strand its widget on the page instead.
//
// Two root fields cannot share a fully qualified name (ISO 32000-1, 12.7.3.2):
// fields with one name are the same field, which only the kids of one parent
// can be. A copied root whose name the form already uses is renamed with the
// first free "_2", "_3" suffix, since the copy is another field that happens to
// share a name, not a view of the target's.
//
// A target with no form gets one, carrying the source form's appearance
// defaults: /DA and /DR are what a viewer draws a field from when it builds an
// appearance (12.7.3.3), and the copied fields inherit them from that form. A
// target that has a form keeps its own; a copied root that states no /DA takes
// the source form's, and the source's /DR fonts the target's /DR lacks are
// added, so the copied fields still draw as they did.
//
// A target whose form lists no field, or that has none, can still present
// widget fields on its pages, which a form listing only the copies would leave
// outside it. Their fields are listed too. A form that lists fields keeps its
// own list as it was, omissions included.
//
// # Corpus evidence
//
//   - litho: 9 fixtures carried a widget qpdf reported as not reachable from
//     /AcroForm after a page move, and none does now.
//   - All corpora, 617 files that mention a form: the warning went from the
//     page-move output of 221 files to 5. Four carry it in their input.
//   - pdfjs issue15597.pdf, issue17794.pdf, issue18466.pdf: present widget
//     fields with no /AcroForm, or with /Fields misspelled /Field, and a form
//     listing only the copies left the pages' own widgets outside it.
//   - pdfjs issue12963.pdf: the fifth. Its widgets name page-tree nodes,
//     fonts and images as /Parent, which no /Fields entry can describe, and
//     qpdf reports them once the insertion gives the document a form.
func mergeInsertedFields(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	catalogRef ast.PdfIndirectRef,
	targetPages []ast.PdfIndirectRef,
	srcDoc *document.Document,
	copier *writer.Copier,
	copiedRefs []ast.PdfIndirectRef,
) error {
	pageRefs := make([]ast.PdfObject, len(copiedRefs))
	for i, ref := range copiedRefs {
		pageRefs[i] = ref
	}
	roots := collectRootFields(alloc, pageRefs, false)
	if len(roots) == 0 {
		return nil
	}

	catalogDict := ResolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return fmt.Errorf("resolving catalog dict")
	}
	srcForm := sourceAcroForm(ctx, srcDoc)

	var form ast.PdfDict
	formObj, hasForm := catalogDict["AcroForm"]
	if hasForm {
		resolved, err := resolveDictAllocFirst(ctx, alloc, doc, formObj)
		if err != nil {
			doc.Logger.Debug("insert: the target's /AcroForm does not resolve, writing a new one",
				slog.Any("err", err))
		}
		form = resolved
	}

	var existing ast.PdfArray
	if form != nil {
		fields, err := resolveArrayAllocFirst(ctx, alloc, doc, form["Fields"])
		if err == nil {
			existing = slices.Clone(fields)
		}
	}
	if len(existing) == 0 {
		// A form that lists no field describes none of the widgets the pages
		// already present, and a reader asks nothing of them. Once /Fields
		// names the copies, those widgets would fall outside the form it now
		// defines, so their fields are listed first.
		existing = targetRootFields(ctx, alloc, doc, targetPages)
	}
	renameClashingRoots(ctx, alloc, doc, existing, roots)

	fields := make(ast.PdfArray, 0, len(existing)+len(roots))
	fields = append(fields, existing...)
	fields = append(fields, roots...)

	var updated ast.PdfDict
	if form == nil {
		updated = ast.PdfDict{"Fields": fields}
		copyFormDefaults(ctx, srcDoc, copier, srcForm, updated)
	} else {
		updated = make(ast.PdfDict, len(form)+1)
		maps.Copy(updated, form)
		updated["Fields"] = fields
		carryFormDefaults(ctx, alloc, doc, srcDoc, copier, srcForm, updated, roots)
	}

	if ref, isRef := formObj.(ast.PdfIndirectRef); isRef && form != nil {
		alloc.AllocAt(ref.ObjectNum, updated)
		return nil
	}
	newCatalog := make(ast.PdfDict, len(catalogDict))
	maps.Copy(newCatalog, catalogDict)
	newCatalog["AcroForm"] = alloc.Alloc(updated)
	alloc.AllocAt(catalogRef.ObjectNum, newCatalog)
	return nil
}

// targetRootFields returns the root field of every widget the target's pages
// present, in page and /Annots order, without duplicates. The pages and their
// fields are read from alloc first, as earlier stages staged them.
func targetRootFields(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pages []ast.PdfIndirectRef) ast.PdfArray {
	seen := make(map[int]bool)
	var out ast.PdfArray
	for _, pageRef := range pages {
		if ctx.Err() != nil {
			return out
		}
		page, err := resolveDictAllocFirst(ctx, alloc, doc, pageRef)
		if err != nil || page == nil {
			continue
		}
		annots, err := resolveArrayAllocFirst(ctx, alloc, doc, page["Annots"])
		if err != nil {
			continue
		}
		for _, annot := range annots {
			annotRef, isRef := annot.(ast.PdfIndirectRef)
			if !isRef {
				continue
			}
			root, ok := rootFieldAllocFirst(ctx, alloc, doc, annotRef)
			if !ok || seen[root.ObjectNum] {
				continue
			}
			seen[root.ObjectNum] = true
			out = append(out, root)
		}
	}
	return out
}

// rootFieldAllocFirst is [rootFieldOf] for objects that may live in the
// document as well as in alloc: it walks a widget's /Parent chain to the field
// that has no parent, reporting whether the annotation presents a field at all.
func rootFieldAllocFirst(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, annotRef ast.PdfIndirectRef) (ast.PdfIndirectRef, bool) {
	current := annotRef
	visited := make(map[int]bool)
	for depth := 0; depth <= maxFieldParentDepth; depth++ {
		if visited[current.ObjectNum] {
			return ast.PdfIndirectRef{}, false
		}
		visited[current.ObjectNum] = true
		dict, err := resolveDictAllocFirst(ctx, alloc, doc, current)
		if err != nil || dict == nil {
			return ast.PdfIndirectRef{}, false
		}
		parent, hasParent := dict["Parent"].(ast.PdfIndirectRef)
		if !hasParent {
			if !isFieldRoot(dict) {
				return ast.PdfIndirectRef{}, false
			}
			return current, true
		}
		current = parent
	}
	return ast.PdfIndirectRef{}, false
}

// sourceAcroForm returns the source document's interactive form, nil when it
// has none.
func sourceAcroForm(ctx context.Context, srcDoc *document.Document) ast.PdfDict {
	catalog, err := document.ResolveCatalog(ctx, srcDoc)
	if err != nil || catalog == nil {
		return nil
	}
	form, err := document.ResolveDict(ctx, srcDoc, catalog.Dict["AcroForm"])
	if err != nil {
		return nil
	}
	return form
}

// copyFormDefaults copies the source form's appearance defaults into a new
// form, as [rebuildAcroForm] does for split and merge. /SigFlags stays behind:
// it describes the source's signatures, not the target's.
func copyFormDefaults(ctx context.Context, srcDoc *document.Document, copier *writer.Copier, srcForm, dst ast.PdfDict) {
	for _, key := range []ast.PdfName{"DR", "DA", "Q", "NeedAppearances"} {
		val, ok := srcForm[key]
		if !ok {
			continue
		}
		copied, err := copier.CopyObject(ctx, val)
		if err != nil {
			srcDoc.Logger.Debug("copying AcroForm entry failed",
				slog.String("key", string(key)), slog.Any("err", err))
			continue
		}
		dst[key] = copied
	}
}

// carryFormDefaults keeps the copied fields drawing as they did under the
// source form when they join the target's: each copied root without a /DA of
// its own takes the source form's (/DA is inheritable, ISO 32000-1, 12.7.3.3),
// and the fonts the source form's /DR names that the target's lacks are added
// to it.
func carryFormDefaults(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	srcDoc *document.Document,
	copier *writer.Copier,
	srcForm, dst ast.PdfDict,
	roots ast.PdfArray,
) {
	if srcDA, ok := srcForm["DA"]; ok && !sameString(ctx, alloc, doc, srcDoc, dst["DA"], srcDA) {
		daCopy, err := copier.CopyObject(ctx, srcDA)
		if err == nil {
			for _, root := range roots {
				ref, isRef := root.(ast.PdfIndirectRef)
				if !isRef {
					continue
				}
				field, ok := alloc.Pending()[ref.ObjectNum].(ast.PdfDict)
				if !ok {
					continue
				}
				if _, has := field["DA"]; has {
					continue
				}
				updated := make(ast.PdfDict, len(field)+1)
				maps.Copy(updated, field)
				updated["DA"] = daCopy
				alloc.AllocAt(ref.ObjectNum, updated)
			}
		}
	}

	srcDR, err := document.ResolveDict(ctx, srcDoc, srcForm["DR"])
	if err != nil || srcDR == nil {
		return
	}
	srcFonts, err := document.ResolveDict(ctx, srcDoc, srcDR["Font"])
	if err != nil || len(srcFonts) == 0 {
		return
	}
	dr, err := resolveDictAllocFirst(ctx, alloc, doc, dst["DR"])
	if err != nil {
		return
	}
	fonts, err := resolveDictAllocFirst(ctx, alloc, doc, dr["Font"])
	if err != nil {
		return
	}
	merged := make(ast.PdfDict, len(fonts)+len(srcFonts))
	maps.Copy(merged, fonts)
	added := false
	for _, name := range slices.Sorted(maps.Keys(srcFonts)) {
		if _, taken := merged[name]; taken {
			continue
		}
		copied, err := copier.CopyObject(ctx, srcFonts[name])
		if err != nil {
			continue
		}
		merged[name] = copied
		added = true
	}
	if !added {
		return
	}
	newDR := make(ast.PdfDict, len(dr)+1)
	maps.Copy(newDR, dr)
	newDR["Font"] = merged
	dst["DR"] = newDR
}

// sameString reports whether the target's string and the source's hold the same
// bytes, so a default both forms state alike is not written again.
func sameString(ctx context.Context, alloc *writer.ObjAllocator, doc, srcDoc *document.Document, target, source ast.PdfObject) bool {
	if target == nil {
		return false
	}
	t, err := resolveAllocFirst(ctx, alloc, doc, target)
	if err != nil {
		return false
	}
	s, err := document.ResolveObject(ctx, srcDoc, source)
	if err != nil {
		return false
	}
	ts, tOK := t.(ast.PdfString)
	ss, sOK := s.(ast.PdfString)
	return tOK && sOK && ts == ss
}

// renameClashingRoots renames each copied root field whose partial name another
// root already uses, in the target or among the copies before it. A root's
// partial name is its fully qualified name (ISO 32000-1, 12.7.3.2), and every
// descendant's name starts with it, so the roots are where two forms clash.
func renameClashingRoots(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, existing, roots ast.PdfArray) {
	taken := make(map[string]bool, len(existing)+len(roots))
	for _, root := range existing {
		dict, err := resolveDictAllocFirst(ctx, alloc, doc, root)
		if err != nil || dict == nil {
			continue
		}
		if name, ok := partialName(ctx, alloc, doc, dict); ok {
			taken[name] = true
		}
	}
	for _, root := range roots {
		ref, isRef := root.(ast.PdfIndirectRef)
		if !isRef {
			continue
		}
		field, ok := alloc.Pending()[ref.ObjectNum].(ast.PdfDict)
		if !ok {
			continue
		}
		name, ok := partialName(ctx, alloc, doc, field)
		if !ok {
			continue
		}
		if !taken[name] {
			taken[name] = true
			continue
		}
		// len(taken)+2 candidates cannot all be taken, so the loop always ends
		// on a free name.
		for n := 2; n <= len(taken)+2; n++ {
			candidate := fmt.Sprintf("%s_%d", name, n)
			if taken[candidate] {
				continue
			}
			updated := make(ast.PdfDict, len(field))
			maps.Copy(updated, field)
			updated["T"] = document.EncodePdfString(candidate)
			alloc.AllocAt(ref.ObjectNum, updated)
			taken[candidate] = true
			break
		}
	}
}

// partialName returns a field's /T decoded, and false when it has none. A
// copied field's references name objects in alloc, so /T resolves there first.
func partialName(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, field ast.PdfDict) (string, bool) {
	obj, has := field["T"]
	if !has {
		return "", false
	}
	resolved, err := resolveAllocFirst(ctx, alloc, doc, obj)
	if err != nil {
		return "", false
	}
	s, ok := resolved.(ast.PdfString)
	if !ok {
		return "", false
	}
	return document.DecodePdfString(s), true
}
