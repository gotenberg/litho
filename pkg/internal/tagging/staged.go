package tagging

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/mutations"
)

// stagedPages returns the pages tagging reads and marks: the document's pages
// as the edits queued in alloc leave its page tree (see
// [mutations.StagedPages]).
//
// Tagging read the document's page tree alone, so after a deletion it marked
// up the deleted page and gave its structure elements a /Pg naming it (ISO
// 32000-1, 14.7.4.3). That reference is live, and the rewrite followed it: the
// output carried the deleted page's content while reporting one page fewer. A
// page an insertion staged was left untagged the same way.
//
// # Corpus evidence
//
//   - All corpora: of 52 files with two pages or more sampled across the
//     seven, AutoTag after deleting page 1 wrote the deleted page back into
//     28, reachable from nothing but the structure tree, and into none now.
//     The PDF/UA-1 remediation of all 116 files sampled writes the same bytes
//     as before.
func stagedPages(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) ([]*document.Page, error) {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("tagging: resolving catalog: %w", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		return nil, fmt.Errorf("tagging: resolving pages: %w", err)
	}
	return mutations.StagedPages(ctx, alloc, doc, pages), nil
}

// stagedObject resolves obj as [document.ResolveObject] does, preferring the
// object alloc has queued at its number (see [stagedValue]).
func stagedObject(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, obj ast.PdfObject) (ast.PdfObject, error) {
	return document.ResolveObject(ctx, doc, stagedValue(alloc, obj))
}

// stagedDict resolves obj to a dictionary as [document.ResolveDict] does,
// preferring the object alloc has queued at its number (see [stagedValue]).
func stagedDict(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, obj ast.PdfObject) (ast.PdfDict, error) {
	return document.ResolveDict(ctx, doc, stagedValue(alloc, obj))
}

// stagedStream resolves obj to a stream as [document.ResolveStream] does,
// preferring the object alloc has queued at its number (see [stagedValue]).
func stagedStream(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, obj ast.PdfObject) (ast.PdfStream, error) {
	return document.ResolveStream(ctx, doc, stagedValue(alloc, obj))
}

// stagedPageDict returns the page dictionary alloc has queued at the page's
// object number, or the document's when none is queued there or the queued
// object is not a dictionary.
func stagedPageDict(alloc *writer.ObjAllocator, page *document.Page) ast.PdfDict {
	if d, ok := stagedValue(alloc, page.Ref).(ast.PdfDict); ok {
		return d
	}
	return page.Dict
}

// stagedResources returns the resource dictionary of the page as alloc stages
// it: the /Resources of its staged dict, resolved alloc-first, or the
// resources the page tree walk merges for it when that dict names none or
// they do not resolve to a dictionary. The merge reaches more than ISO
// 32000-1, 7.7.3.4 hands the page (see [document.Page]), deliberately: a
// form a lenient viewer draws past the page's own dictionary is then still
// judged to paint text, and kept out of an /Artifact.
func stagedResources(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, page *document.Page) ast.PdfDict {
	if res, ok := stagedPageDict(alloc, page)["Resources"]; ok {
		if d, err := stagedDict(ctx, alloc, doc, res); err == nil && d != nil {
			return d
		}
	}
	return page.Resources
}

// stagedValue returns the object alloc has queued at the number obj refers
// to, or obj itself when obj is not a reference or nothing is queued there. A
// nil alloc queues nothing, so the staged readers then read the document alone.
//
// Tagging reads every object this way because it runs after other stages have
// queued their edits in alloc: the fixers a remediation runs before its
// auto-tag step, a remediation whose one kept round left its repairs in the
// Writer's overrides, a stamp. Those edits exist only in alloc until the
// document is written. Reading the document alone, tagging marked up content
// the output no longer held and wrote the marked copy over the edit, so a
// content repair came back, and it judged a document a remediation had tagged
// untagged, replacing that structure tree with a second one.
//
// # Corpus evidence
//
//   - All corpora: over a 386-file sample of every corpus, the PDF/UA-1
//     remediations, default and aggressive, write the same bytes, residuals,
//     structure elements and text as before: no fixer that runs before the
//     auto-tag step queues an object tagging reads.
//   - All corpora: on the same sample, AutoTag queued after a PDF/UA-1
//     remediation tagged 187 documents a second time and, in 36 of them,
//     dropped the 1139 Annot, Link and Form elements the remediation had
//     built, which ISO 14289-1, 7.18.1, 7.18.4 and 7.18.5 require. It now
//     leaves 385 of the 386 outputs byte for byte the remediation's, and
//     veraPDF reports 1134 failed checks fewer on those 36 and no new one on
//     any of the 187.
//   - litho docmdp_2b.pdf, contentstream_4.pdf, pdfjs bug1354114.pdf and
//     poppler-90-0-fuzzed.pdf: AutoTag queued after a PDF/A-2b remediation
//     brought back a number or an operator the remediation had repaired
//     (ISO 19005-2, 6.1.13 and 6.2.2).
//   - litho limits_2b.pdf, pdfjs issue6151.pdf: the name longer than 127 bytes
//     it brought back resolved to nothing, hiding the DeviceN colour space and
//     the unembedded font the remediated output names, which litho and
//     veraPDF now report as they do on that output.
func stagedValue(alloc *writer.ObjAllocator, obj ast.PdfObject) ast.PdfObject {
	if ref, ok := obj.(ast.PdfIndirectRef); ok && alloc != nil {
		if pending, ok := alloc.Pending()[ref.ObjectNum]; ok {
			return pending
		}
	}
	return obj
}
