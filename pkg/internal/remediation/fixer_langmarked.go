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
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// matchMarkedContentLang claims the same /Lang clauses as the catalog repair.
// It is a separate fixer because repairing an inline property list rewrites a
// content stream, which is safetyContent work: the writer must be able to roll
// it back. Its sibling only edits the catalog and the structure tree and is
// registered structural.
//
// It does not share the sibling's claim on the violations of the fixers that
// write language text ([writesLanguageText]): that text needs the catalog
// /Lang, not a repaired property list, and a content-tier fixer in the plan
// makes a regression roll back rather than fail the run.
func matchMarkedContentLang(v compliance.Violation) bool {
	return matchLangClauses(v)
}

// fixMarkedContentLang strips an unusable /Lang from every marked-content
// property list, inline in a content stream or named through
// /Resources /Properties.
func fixMarkedContentLang(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, profile string) ([]string, error) {
	requireDeclared := requiresDeclaredLang(profile)
	inline, err := repairMarkedContentLangs(ctx, alloc, doc, requireDeclared)
	if err != nil {
		return nil, fmt.Errorf("repairing marked-content /Lang: %w", err)
	}
	return markedContentLangFixes(inline, repairPropertyListLangs(ctx, alloc, doc, requireDeclared)), nil
}

// repairMarkedContentLangs strips an unusable /Lang from the inline property
// list of every marked-content operator in the content streams
// [walkContentStreams] reaches, returning how many it removed.
//
// A /Lang may be given inline on a BDC operator (ISO 32000-1, 14.6.2), and the
// language identifier rules of 14.9.2 apply there just as they do on a
// structure element. The structure-tree repair could not reach those: the
// property list lives in the content stream, not in an object, so such tags
// survived remediation.
//
// The streams are the ones the language check judges, a page's own content,
// the forms it draws and its annotations' appearances, and the tiling patterns
// and Type 3 glyphs besides, which the walk reaches too. Repairing the page's
// own content alone left the tags of the others as residuals.
//
// Removing the entry is the repair, exactly as for a structure element: the
// span then inherits the document language rather than declaring an
// unusable one. Nothing is painted by BDC, so no visible content changes.
//
// # Corpus evidence
//
//   - verapdf PDF_UA-1__7.2-t29-fail-g through -p: came out of remediation
//     still carrying tags like "-pt" and "1-pt" before this repair.
//   - litho mcform_2a.pdf: kept 6.7.4 t1 for the inline BDC of a form the
//     page draws and of an annotation appearance after a PDF/A-2a
//     remediation, on litho and veraPDF alike, and 7.2 t29 at PDF/UA-1 and
//     8.4.4 t2 at PDF/UA-2, while the repair read the page's content alone.
func repairMarkedContentLangs(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, requireDeclared bool) (int, error) {
	total := 0
	_, err := walkContentStreams(ctx, alloc, doc, "marked-content-lang", func(ctx context.Context, data []byte) ([]byte, func()) {
		newData, removed, err := repairContentOps(ctx, data, func() opRepair {
			return markedContentLangRepair(ctx, doc, requireDeclared)
		})
		if err != nil {
			doc.Logger.Debug("remediation: cannot rewrite content stream for marked-content /Lang", "error", err)
			return nil, nil
		}
		if removed == 0 {
			return nil, nil
		}
		return newData, func() { total += removed }
	})
	if err != nil {
		return 0, err
	}
	return total, nil
}

// markedContentLangRepair returns the repair of [repairMarkedContentLangs]
// for the operations of a content stream: a BDC or DP whose inline property
// list carries an unusable /Lang ([markedLangUnusable]) is written without it,
// and every /Lang removed counts as an edit.
func markedContentLangRepair(ctx context.Context, doc *document.Document, requireDeclared bool) opRepair {
	return func(op content.Operation) ([]content.Operation, int) {
		keep := []content.Operation{op}
		// The property list is the second operand: /Tag properties BDC. A
		// name operand refers to /Resources /Properties, an object the
		// structure-tree repair does not own either, so
		// [repairPropertyListLangs] handles it.
		if (op.Operator != "BDC" && op.Operator != "DP") || len(op.Operands) < 2 {
			return keep, 0
		}
		props, ok := op.Operands[1].(ast.PdfDict)
		if !ok {
			return keep, 0
		}
		if lang, has := props["Lang"]; !has || !markedLangUnusable(ctx, doc, lang, requireDeclared) {
			return keep, 0
		}
		patched := maps.Clone(props)
		delete(patched, "Lang")
		operands := slices.Clone(op.Operands)
		operands[1] = patched
		return []content.Operation{{Operator: op.Operator, Operands: operands}}, 1
	}
}

// markedLangUnusable reports whether a marked-content /Lang value cannot serve
// as the language identifier ISO 32000-1, 14.9.2 requires.
func markedLangUnusable(ctx context.Context, doc *document.Document, obj ast.PdfObject, requireDeclared bool) bool {
	if !langTagValid(ctx, doc, obj, requireDeclared) {
		return true
	}
	return requireDeclared && langUnknown(ctx, doc, obj)
}

// repairPropertyListLangs strips an unusable /Lang from the named property
// lists a BDC reaches by name through /Resources /Properties rather than
// inline, returning how many it removed. Those are objects, not content, so
// no content stream is rewritten.
//
// The lists are the ones the language check resolves a name against: those
// of the /Resources a page resolves its names against, and those of every
// form it draws and every appearance stream of its annotations, which the
// walk reaches through [propertyListWalk].
func repairPropertyListLangs(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, requireDeclared bool) int {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return 0
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		return 0
	}

	w := &propertyListWalk{alloc: alloc, doc: doc, requireDeclared: requireDeclared, visited: make(map[int]bool)}
	for _, pe := range pageTreeEntries(ctx, alloc, doc, pages) {
		if ctx.Err() != nil {
			return w.removed
		}
		w.page(ctx, pageTreeEntry(ctx, alloc, doc, pe))
	}
	return w.removed
}

// propertyListWalk carries one [repairPropertyListLangs] run over the
// resource dictionaries a document's pages resolve marked-content names
// against: a page's own or inherited /Resources, and the /Resources of the
// forms and annotation appearances it draws, recursively.
//
// A property list with an object number of its own is written there; one
// written directly in a /Properties dictionary is written at the first link
// above it that has one ([writeResourceCategory]), up to the page, ancestor,
// form or appearance stream holding the /Resources. Every object is read
// through the allocator, so the write builds on what an earlier fixer queued
// there, the form nested-resources gave /Resources of its own among them.
//
// visited holds the object number of every stream and property list already
// walked, so a shared one is repaired once and a cyclic chain of forms cannot
// loop. The nesting is bounded by the document's form-nesting budget, the
// bound the language check applies.
type propertyListWalk struct {
	alloc           *writer.ObjAllocator
	doc             *document.Document
	requireDeclared bool
	visited         map[int]bool
	// removed counts the /Lang entries removed by writes that landed.
	removed int
}

// page walks the /Resources a page resolves its names against, read from the
// node of the page tree holding them ([resourcesHolder]), then its
// annotations' appearances. pe carries the page's dictionary read through the
// allocator ([pageTreeEntry]).
func (w *propertyListWalk) page(ctx context.Context, pe annotPageEntry) {
	if holder, ok := resourcesHolder(ctx, w.alloc, w.doc, pe); ok {
		work := maps.Clone(holder.dict)
		if pending, dirty := w.resources(ctx, work, 0); dirty && writeTreeNode(ctx, w.alloc, w.doc, holder, work) {
			w.removed += pending
		}
	}
	w.annotations(ctx, pe.dict)
}

// resources repairs the property lists of the /Resources owner holds, owner
// being the working copy of the page, ancestor or stream dictionary holding
// them, then walks the forms those /Resources name. It returns how many /Lang
// entries it removed from lists written directly in them, which land with the
// caller's write of owner when dirty is set.
func (w *propertyListWalk) resources(ctx context.Context, owner ast.PdfDict, depth int) (int, bool) {
	res := liveDictValue(ctx, w.alloc, w.doc, owner["Resources"])
	if res == nil {
		return 0, false
	}
	pending, dirty := w.properties(ctx, owner, res)
	if depth < w.doc.Limits().MaxFormDepth {
		xobjects := liveDictValue(ctx, w.alloc, w.doc, res["XObject"])
		for _, name := range sortedDictKeys(xobjects) {
			w.form(ctx, xobjects[name], depth+1, false)
		}
	}
	return pending, dirty
}

// properties strips an unusable /Lang from every property list the
// /Properties dictionary of res holds. See [propertyListWalk.resources] for
// the results.
func (w *propertyListWalk) properties(ctx context.Context, owner, res ast.PdfDict) (int, bool) {
	props := liveDictValue(ctx, w.alloc, w.doc, res["Properties"])
	var inline ast.PdfDict
	for _, name := range sortedDictKeys(props) {
		entry := props[name]
		ref, isRef := entry.(ast.PdfIndirectRef)
		if isRef {
			if w.visited[ref.ObjectNum] {
				continue
			}
			w.visited[ref.ObjectNum] = true
		}
		list := liveDictValue(ctx, w.alloc, w.doc, entry)
		if list == nil {
			continue
		}
		patched := maps.Clone(list)
		if !removeInvalidLang(ctx, w.doc, patched, w.requireDeclared) {
			continue
		}
		if isRef {
			if w.alloc.AllocAt(ref.ObjectNum, patched) {
				w.removed++
			}
			continue
		}
		if inline == nil {
			inline = make(ast.PdfDict)
		}
		inline[name] = patched
	}
	if len(inline) == 0 {
		return 0, false
	}
	rebuilt := maps.Clone(props)
	maps.Copy(rebuilt, inline)
	written, dirty := writeResourceCategory(w.alloc, owner, res, "Properties", rebuilt)
	if written {
		w.removed += len(inline)
	}
	if dirty {
		return len(inline), true
	}
	return 0, false
}

// form walks the form entry names, when the walk has not reached it yet: its
// /Resources' property lists and the forms they name. An appearance stream is
// a form whether or not it states /Subtype /Form (ISO 32000-1, 12.5.5); an
// XObject is one only when it does (8.10.2). The stream is written back with
// its body when a repair lands in its dictionary.
func (w *propertyListWalk) form(ctx context.Context, entry ast.PdfObject, depth int, appearance bool) {
	// A stream is always an indirect object (ISO 32000-1, 7.3.8.1).
	ref, isRef := entry.(ast.PdfIndirectRef)
	if !isRef || ctx.Err() != nil || w.visited[ref.ObjectNum] {
		return
	}
	stream, ok := liveStream(ctx, w.alloc, w.doc, ref)
	if !ok {
		return
	}
	// Best-effort: a /Subtype that is no name reads as absent.
	subtype, _ := document.ResolveName(ctx, w.doc, stream.Dict["Subtype"])
	if subtype != "Form" && (!appearance || subtype != "") {
		return
	}
	w.visited[ref.ObjectNum] = true
	work := maps.Clone(stream.Dict)
	if pending, dirty := w.resources(ctx, work, depth); dirty && w.alloc.AllocAt(ref.ObjectNum, ast.NewLazyStream(work, stream.Bytes)) {
		w.removed += pending
	}
}

// annotations walks the normal, rollover and down appearances of a page's
// annotations, each a stream or a subdictionary of appearance states (ISO
// 32000-1, 12.5.5), one level below the page.
func (w *propertyListWalk) annotations(ctx context.Context, pageDict ast.PdfDict) {
	for _, annotObj := range liveArray(ctx, w.alloc, w.doc, pageDict["Annots"]) {
		if ctx.Err() != nil {
			return
		}
		annot := liveDictValue(ctx, w.alloc, w.doc, annotObj)
		ap := liveDictValue(ctx, w.alloc, w.doc, annot["AP"])
		for _, key := range []ast.PdfName{"N", "R", "D"} {
			entry, ok := ap[key]
			if !ok {
				continue
			}
			if ref, isRef := entry.(ast.PdfIndirectRef); isRef {
				if _, isStream := liveStream(ctx, w.alloc, w.doc, ref); isStream {
					w.form(ctx, entry, 1, true)
					continue
				}
			}
			states := liveDictValue(ctx, w.alloc, w.doc, entry)
			for _, state := range sortedDictKeys(states) {
				w.form(ctx, states[state], 1, true)
			}
		}
	}
}

// markedContentLangFixes describes what the two marked-content repairs removed.
func markedContentLangFixes(inline, named int) []string {
	var fixes []string
	if inline > 0 {
		fixes = append(fixes, fmt.Sprintf("removed invalid /Lang from %d marked-content property list(s)", inline))
	}
	if named > 0 {
		fixes = append(fixes, fmt.Sprintf("removed invalid /Lang from %d named property list(s)", named))
	}
	return fixes
}
