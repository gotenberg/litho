package remediation

import (
	"context"
	"fmt"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// repairMarkedContentLangs strips an unusable /Lang from every marked-content
// property list on every page, returning how many it removed.
//
// A /Lang may be given inline on a BDC operator (ISO 32000-1, 14.6.2), and the
// language identifier rules of 14.9.2 apply there just as they do on a
// structure element. The structure-tree repair could not reach those: the
// property list lives in the content stream, not in an object, so
// PDF_UA-1__7.2-t29-fail-g through -p came out of remediation still carrying
// tags like "-pt" and "1-pt".
//
// Removing the entry is the repair, exactly as for a structure element: the
// span then inherits the document language rather than declaring an
// unusable one. Nothing is painted by BDC, so no visible content changes.
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
	ops, contentsObj, err := livePageContentOps(ctx, alloc, doc, page)
	if err != nil || contentsObj == nil {
		return 0
	}

	removed := 0
	for i, op := range ops {
		if op.Operator != "BDC" && op.Operator != "DP" {
			continue
		}
		// The property list is the second operand: /Tag properties BDC. A
		// name operand refers to /Resources /Properties, an object the
		// structure-tree repair does not own either, so it is handled below.
		if len(op.Operands) < 2 {
			continue
		}
		props, ok := op.Operands[1].(ast.PdfDict)
		if !ok {
			continue
		}
		if _, has := props["Lang"]; !has {
			continue
		}
		if !markedLangUnusable(ctx, doc, props["Lang"], requireDeclared) {
			continue
		}
		patched := make(ast.PdfDict, len(props))
		for k, v := range props {
			if k != "Lang" {
				patched[k] = v
			}
		}
		ops[i].Operands[1] = patched
		removed++
	}
	if removed == 0 {
		return 0
	}

	if err := replaceContentStream(ctx, alloc, doc, contentsObj, serializeOps(ops), pageNum); err != nil {
		doc.Logger.Debug("remediation: cannot rewrite content stream for marked-content /Lang",
			"page", pageNum, "error", err)
		return 0
	}
	return removed
}

// markedLangUnusable reports whether a marked-content /Lang value cannot serve
// as the language identifier ISO 32000-1, 14.9.2 requires.
func markedLangUnusable(ctx context.Context, doc *document.Document, obj ast.PdfObject, requireDeclared bool) bool {
	if !langTagValid(ctx, doc, obj) {
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
		resources, err := document.ResolveDict(ctx, doc, pageDict["Resources"])
		if err != nil || resources == nil {
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
