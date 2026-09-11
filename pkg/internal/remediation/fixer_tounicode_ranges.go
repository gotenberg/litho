package remediation

import (
	"context"
	"fmt"
	"log/slog"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/typography/cmap"
)

// matchToUnicodeRanges claims the rules that ask for a Unicode value per
// shown glyph: ISO 19005-1, 6.3.8, ISO 19005-2 and 19005-3, 6.2.11.7.2, ISO
// 14289-1, 7.21.7 and ISO 14289-2, 8.4.5.8. A /ToUnicode range crossing a
// first-byte boundary leaves the glyphs it was meant to cover without one.
func matchToUnicodeRanges(v compliance.Violation) bool {
	return clausePrefix(v, "6.3.8", "6.2.11.7.2", "7.21.7", "8.4.5.8")
}

// fixToUnicodeRangeIssues repairs the /ToUnicode CMap of every font the pages
// use. It only touches a CMap holding a bfrange that crosses a first-byte
// boundary, so running it on a document with none changes nothing.
func fixToUnicodeRangeIssues(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return nil, nil
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		return nil, nil
	}

	var fixes []string
	processed := make(map[int]bool)
	for _, page := range pages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if page.Resources == nil {
			continue
		}
		fontDict, err := document.ResolveDict(ctx, doc, page.Resources["Font"])
		if err != nil || fontDict == nil {
			continue
		}
		// Sorted iteration so the fix list is deterministic across runs.
		for _, fontName := range sortedDictKeys(fontDict) {
			fontEntry := fontDict[fontName]
			var fontRef ast.PdfIndirectRef
			if ref, ok := fontEntry.(ast.PdfIndirectRef); ok {
				if processed[ref.ObjectNum] {
					continue
				}
				processed[ref.ObjectNum] = true
				fontRef = ref
			}
			fd, err := document.ResolveDict(ctx, doc, fontEntry)
			if err != nil || fd == nil {
				continue
			}
			fixes = append(fixes, fixToUnicodeRanges(ctx, doc, alloc, fd, fontRef)...)
		}
	}
	if len(fixes) == 0 {
		return nil, nil
	}
	return fixes, nil
}

// fixToUnicodeRanges rewrites a /ToUnicode CMap whose bfrange entries cross a
// first-byte boundary into entries that map the same codes and do not.
//
// Adobe Technical Note #5411, 1.4.1 rules such an entry invalid, and a
// validator holding to it (veraPDF clamps the range to its first-byte block)
// finds no Unicode value for the glyphs it was meant to cover. libHaru writes
// its identity map as <0000> <D7FF> <0000>, which then credits only codes 00
// to FF (pdfjs issue20232.pdf). The CMap is replaced at its own object number,
// so every font sharing it is repaired, and [cmap.ConformBfRanges] only returns
// a rewrite that reads exactly as before, so extracted text cannot change.
func fixToUnicodeRanges(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, fontDict ast.PdfDict, fontRef ast.PdfIndirectRef) []string {
	// Build on the latest version of the font dictionary: an earlier repair
	// may already have attached a generated /ToUnicode.
	if fontRef.ObjectNum > 0 {
		if d, ok := alloc.Pending()[fontRef.ObjectNum].(ast.PdfDict); ok {
			fontDict = d
		}
	}
	ref, ok := fontDict["ToUnicode"].(ast.PdfIndirectRef)
	if !ok || ref.ObjectNum == 0 {
		return nil
	}
	// A pending stream is one a repair generated or already rewrote for a
	// font sharing it.
	if _, done := alloc.Pending()[ref.ObjectNum]; done {
		return nil
	}
	stream, err := document.ResolveStream(ctx, doc, ref)
	if err != nil || stream.Dict == nil {
		return nil
	}
	data, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		doc.Logger.Debug("remediation: ToUnicode CMap decode failed, ranges left as written",
			slog.Any("err", err))
		return nil
	}
	rewritten, changed, err := cmap.ConformBfRanges(ctx, data)
	if err != nil {
		doc.Logger.Debug("remediation: ToUnicode CMap unreadable, ranges left as written",
			slog.Any("err", err))
		return nil
	}
	if !changed {
		return nil
	}

	// The new stream carries decoded bytes, so the entries describing how the
	// old bytes were stored go (ISO 32000-1, Table 5). Everything else stays.
	dict := make(ast.PdfDict, len(stream.Dict))
	maps.Copy(dict, stream.Dict)
	for _, k := range []ast.PdfName{"Length", "Filter", "DecodeParms", "DL", "F", "FFilter", "FDecodeParms"} {
		delete(dict, k)
	}
	if !alloc.AllocAt(ref.ObjectNum, ast.NewStream(dict, rewritten)) {
		return nil
	}
	return []string{fmt.Sprintf("split /ToUnicode ranges crossing a first-byte boundary for font %s", fontBaseName(fontDict))}
}
