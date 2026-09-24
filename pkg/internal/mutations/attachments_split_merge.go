package mutations

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// rebuildEmbeddedFiles writes the embedded file attachments of every source
// onto dst, or leaves dst untouched when no source carries any.
//
// Attachments hang off the catalog name tree at /Names /EmbeddedFiles (ISO
// 32000-1, 7.7.4) and the catalog /AF array names the ones a PDF/A-3 or
// Factur-X document treats as associated files (ISO 32000-2, 14.13). Split and
// merge build a fresh catalog and copied neither, so every attachment was lost,
// and a Factur-X invoice came out as a file that is no longer a Factur-X
// document.
//
// Merging two documents that attach the same name yields one name tree that
// cannot hold both, since keys in a name tree are unique and sorted (ISO
// 32000-1, 7.9.6). Later duplicates are suffixed rather than dropped: losing an
// attachment is the bug being fixed here, so a renamed copy is the better
// answer, and a merge of a document with itself keeps both payloads.
//
// # Corpus evidence
//
//   - facturx: all 150 fixtures came out of a merge without the
//     ZUGFeRD-invoice.xml that IS the invoice.
func rebuildEmbeddedFiles(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	sources []acroFormSource,
	dst ast.PdfDict,
) error {
	var entries []EFEntry
	var associated ast.PdfArray
	used := make(map[string]int)

	for _, src := range sources {
		names, err := document.ResolveDict(ctx, src.Doc, src.Catalog["Names"])
		if err == nil && names != nil {
			var srcEntries []EFEntry
			// Losing an attachment is the bug this function exists to fix, so
			// a source tree it cannot read whole is refused rather than carried
			// across short.
			if collectSourceEmbeddedFiles(ctx, src.Doc, names["EmbeddedFiles"], 0, &srcEntries) {
				return fmt.Errorf("reading a source document's attachments: %w", ErrAttachmentTreeIncomplete)
			}
			for _, e := range srcEntries {
				copied, err := src.Copier.CopyObject(ctx, e.Ref)
				if err != nil {
					src.Doc.Logger.Debug("copying embedded file failed",
						slog.String("name", e.Name),
						slog.Any("err", err),
					)
					continue
				}
				entries = append(entries, EFEntry{Name: uniqueEFName(used, e.Name), Ref: copied})
			}
		}

		af, err := document.ResolveArray(ctx, src.Doc, src.Catalog["AF"])
		if err != nil || af == nil {
			continue
		}
		for _, spec := range af {
			copied, err := src.Copier.CopyObject(ctx, spec)
			if err != nil {
				src.Doc.Logger.Debug("copying associated file failed", slog.Any("err", err))
				continue
			}
			associated = append(associated, copied)
		}
	}

	if len(entries) > 0 {
		dst["Names"] = alloc.Alloc(ast.PdfDict{
			"EmbeddedFiles": alloc.Alloc(ast.PdfDict{"Names": embeddedFilesArray(entries)}),
		})
	}
	if len(associated) > 0 {
		dst["AF"] = alloc.Alloc(associated)
	}
	return nil
}

// uniqueEFName returns name, or name with a counter appended when an earlier
// source already claimed it, so a name tree keeps one key per attachment.
func uniqueEFName(used map[string]int, name string) string {
	n, taken := used[name]
	if !taken {
		used[name] = 1
		return name
	}
	for {
		n++
		candidate := fmt.Sprintf("%s (%d)", name, n)
		if _, clash := used[candidate]; !clash {
			used[name] = n
			used[candidate] = 1
			return candidate
		}
	}
}

// collectSourceEmbeddedFiles flattens a source document's /EmbeddedFiles name
// tree (ISO 32000-1, 7.9.6) into out.
//
// [CollectEmbeddedFileEntries] resolves through an allocator first, which is
// right when reading back what a mutation has queued. Here the tree belongs to
// a SOURCE document while the allocator holds the OUTPUT, and the two number
// their objects independently, so resolving through it would answer with an
// unrelated output object that happens to share a number.
// It reports whether the walk stopped at [maxAttachmentTreeDepth] with part of
// the source tree unread; [rebuildEmbeddedFiles] writes the destination's whole
// tree from what this collects, so a truncated read loses attachments.
func collectSourceEmbeddedFiles(ctx context.Context, doc *document.Document, nodeObj ast.PdfObject, depth int, out *[]EFEntry) (truncated bool) {
	collectSourceEmbeddedFilesAt(ctx, doc, nodeObj, depth, make(map[int]bool), out, &truncated)
	return truncated
}

// collectSourceEmbeddedFilesAt is [collectSourceEmbeddedFiles] carrying the set
// of nodes already walked.
//
// seen visits each node once, exactly as [document.LookupNameTree] does: a name
// tree is a strict tree (ISO 32000-1, 7.9.6), so a /Kids entry reached twice is
// malformed. The depth bound alone does not contain that, because this walk
// fans out: a node whose /Kids lists the same child twice costs 2^depth, which
// at maxAttachmentTreeDepth is a denial of service rather than a slow walk.
//
// It also keeps the answer right. Every entry collected here is written back
// into a rebuilt tree, so traversing one leaf twice appends it twice and
// [uniqueEFName] renames the copy, inventing an attachment the file never had.
//
// # Corpus evidence
//
//   - pdfjs issue18444.pdf: writes a name-tree key as an indirect reference.
func collectSourceEmbeddedFilesAt(
	ctx context.Context,
	doc *document.Document,
	nodeObj ast.PdfObject,
	depth int,
	seen map[int]bool,
	out *[]EFEntry,
	truncated *bool,
) {
	// The destination tree is rebuilt from out, so anything left uncollected
	// here is an attachment the split or merge drops.
	if depth > maxAttachmentTreeDepth {
		*truncated = true
		return
	}
	if ref, ok := nodeObj.(ast.PdfIndirectRef); ok {
		if seen[ref.ObjectNum] {
			return
		}
		seen[ref.ObjectNum] = true
	}
	node, err := document.ResolveDict(ctx, doc, nodeObj)
	if err != nil || node == nil {
		return
	}
	if kids, err := document.ResolveArray(ctx, doc, node["Kids"]); err == nil && kids != nil {
		for _, kid := range kids {
			collectSourceEmbeddedFilesAt(ctx, doc, kid, depth+1, seen, out, truncated)
		}
		return
	}
	arr, err := document.ResolveArray(ctx, doc, node["Names"])
	if err != nil || arr == nil {
		return
	}
	for i := 0; i+1 < len(arr); i += 2 {
		// The key may be an indirect reference like any other object, and
		// real producers write one: a bare type assertion drops the entry and
		// the attachment with it.
		name, err := document.ResolveString(ctx, doc, arr[i])
		if err != nil {
			doc.Logger.Debug("resolving embedded file name", slog.Any("err", err))
			continue
		}
		*out = append(*out, EFEntry{Name: document.DecodePdfString(name), Ref: arr[i+1]})
	}
}
