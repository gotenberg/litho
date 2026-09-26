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
func matchMarkedContentLang(v compliance.Violation) bool {
	return matchCatalogLang(v)
}

// fixMarkedContentLang strips an unusable /Lang from every marked-content
// property list, inline in a content stream or named through
// /Resources /Properties.
func fixMarkedContentLang(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, profile string) ([]string, error) {
	requireDeclared := requiresDeclaredLang(profile)
	return markedContentLangFixes(
		repairMarkedContentLangs(ctx, alloc, doc, requireDeclared),
		repairPropertyListLangs(ctx, alloc, doc, requireDeclared),
	), nil
}

// repairMarkedContentLangs strips an unusable /Lang from every marked-content
// property list on every page, returning how many it removed.
//
// A /Lang may be given inline on a BDC operator (ISO 32000-1, 14.6.2), and the
// language identifier rules of 14.9.2 apply there just as they do on a
// structure element. The structure-tree repair could not reach those: the
// property list lives in the content stream, not in an object, so such tags
// survived remediation.
//
// Removing the entry is the repair, exactly as for a structure element: the
// span then inherits the document language rather than declaring an
// unusable one. Nothing is painted by BDC, so no visible content changes.
//
// # Corpus evidence
//
//   - verapdf PDF_UA-1__7.2-t29-fail-g through -p: came out of remediation
//     still carrying tags like "-pt" and "1-pt" before this repair.
func repairMarkedContentLangs(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, requireDeclared bool) int {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return 0
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		return 0
	}

	total := 0
	for i, page := range pages {
		if ctx.Err() != nil {
			return total
		}
		total += repairPageMarkedContentLangs(ctx, alloc, doc, page, i+1, requireDeclared)
	}
	return total
}

// repairPageMarkedContentLangs rewrites one page's content stream when a
// marked-content property list carries an unusable /Lang.
func repairPageMarkedContentLangs(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, page *document.Page, pageNum int, requireDeclared bool) int {
	data, contentsObj, err := livePageContentData(ctx, alloc, doc, page)
	if err != nil || contentsObj == nil {
		return 0
	}
	newData, removed, err := repairContentOps(ctx, data, func() opRepair {
		return markedContentLangRepair(ctx, doc, requireDeclared)
	})
	if err != nil {
		doc.Logger.Debug("remediation: cannot rewrite content stream for marked-content /Lang",
			"page", pageNum, "error", err)
		return 0
	}
	if removed == 0 {
		return 0
	}
	if err := replaceContentStream(ctx, alloc, doc, contentsObj, newData, pageNum); err != nil {
		doc.Logger.Debug("remediation: cannot rewrite content stream for marked-content /Lang",
			"page", pageNum, "error", err)
		return 0
	}
	return removed
}

// markedContentLangRepair returns the repair of [repairPageMarkedContentLangs]
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
// lists a page's /Resources /Properties holds, which BDC reaches by name
// rather than inline. Those are ordinary objects, so no content stream is
// rewritten.
func repairPropertyListLangs(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, requireDeclared bool) int {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return 0
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		return 0
	}

	visited := make(map[int]bool)
	total := 0
	for _, page := range pages {
		if ctx.Err() != nil {
			return total
		}
		pageDict := resolveFromAllocOrDoc(ctx, alloc, doc, page.Ref)
		if pageDict == nil {
			pageDict = page.Dict
		}
		// A page with no /Resources of its own names property lists from the
		// ones it inherits (ISO 32000-1, 7.7.3.4; a null entry is absent,
		// 7.3.7), as the language check reads them.
		resources := page.Resources
		if own, ok := pageDict["Resources"]; ok {
			if _, null := own.(ast.PdfNull); !null {
				resources = liveDictValue(ctx, alloc, doc, own)
			}
		}
		if resources == nil {
			continue
		}
		properties, err := document.ResolveDict(ctx, doc, resources["Properties"])
		if err != nil || properties == nil {
			continue
		}
		for _, name := range sortedDictKeys(properties) {
			ref, isRef := properties[name].(ast.PdfIndirectRef)
			if !isRef || ref.ObjectNum == 0 || visited[ref.ObjectNum] {
				continue
			}
			visited[ref.ObjectNum] = true
			dict := resolveFromAllocOrDoc(ctx, alloc, doc, ref)
			if dict == nil {
				continue
			}
			patched := make(ast.PdfDict, len(dict))
			maps.Copy(patched, dict)
			if !removeInvalidLang(ctx, doc, patched, requireDeclared) {
				continue
			}
			alloc.AllocAt(ref.ObjectNum, patched)
			total++
		}
	}
	return total
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
