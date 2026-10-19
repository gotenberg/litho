package remediation

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// fixMarkedContentClose closes the marked-content sequences a page's content
// opens and never closes. ISO 32000-1, 14.6 delimits a sequence by its BMC or
// BDC and the matching EMC, so content after an opener no EMC closes lies in no
// sequence, and ISO 14289-1, 7.1 t3 and ISO 14289-2, 8.2.2 t1 judge it as
// neither tagged nor an artifact. The content-tag fixer leaves such a page
// alone, since it already holds marked content, so this one appends the
// missing EMCs: what the opener's author marked becomes the sequence they
// opened. It shares [matchContentTag], the matcher of the findings it repairs.
//
// A page's /Contents streams are one stream for 14.6 (ISO 32000-1, 7.8.2), so
// the EMCs go in a new stream appended to the page's /Contents and no stream
// is edited: another page may share it. When the content ends inside a string
// it never terminates (7.3.4.2, 7.3.4.3), an EMC appended after it would be
// read as string bytes, so the page's content is instead rewritten as one new
// stream that stops at [content.TerminatedEnd], where the unterminated token
// that paints nothing is left out, followed by the EMCs.
//
// Pages alone are repaired. A form XObject must close the sequences it opens
// within itself too, and the compliance walk, as veraPDF's semantic content
// stream does, judges what follows a form's open opener in the marking around
// the Do. Closing it would mark that content with the form's opener in every
// place the form is drawn, and an /Artifact opener in a form drawn inside
// tagged content would then be an artifact in tagged content (ISO 14289-1,
// 7.1 t1) where nothing was reported. Type 3 glyph procedures and annotation
// appearances are not walked for marking by either tool, so a sequence they
// leave open draws no finding to repair.
//
// # Corpus evidence
//
//   - All corpora: read whole, as remediation reads them, two pages leave
//     a sequence open, both one level deep and painting after the opener,
//     and no form XObject does.
//   - litho unclosedmc_ua1.pdf: an /Artifact BMC before a stroked rule with
//     no EMC kept ISO 14289-1, 7.1 t3 and ISO 14289-2, 8.2.2 t1 after
//     remediation on both tools, and now fully fixes.
//   - pdfjs issue11678.pdf: page 2 leaves its sequence open past where the
//     veraPDF reading stops, so the closing EMC moves no rule set on either
//     tool at any of the thirteen levels.
//   - pdfjs bug1077808.pdf: the veraPDF reading stops 1.9 MB into the page's
//     10 MB of content inside a /Layer sequence, but every sequence closes
//     in the whole reading, so the fixer leaves it, and a /Layer sequence
//     would tag nothing anyway.
func fixMarkedContentClose(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return nil, nil //nolint:nilerr // No catalog holds no content to close; validation reports the broken document.
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		return nil, nil //nolint:nilerr // No page tree holds no content to close; validation reports the broken document.
	}

	var count int
	for _, page := range pages {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if page.Ref.ObjectNum == 0 {
			continue
		}
		// Read alloc-first so a content rewrite an earlier fixer queued, the
		// auto-tagger's among them, is the one closed and extended.
		pageDict := livePageDict(ctx, alloc, doc, page)
		contentsObj, ok := pageDict["Contents"]
		if !ok {
			continue
		}
		data, err := liveContentBytes(ctx, alloc, doc, contentsObj)
		if err != nil || len(data) == 0 {
			if err != nil {
				doc.Logger.Debug("remediation: marked-content close skipped an unreadable page",
					slog.Int("page", page.Ref.ObjectNum), slog.Any("err", err))
			}
			continue
		}
		ops, err := content.ParseStreamData(ctx, data)
		if err != nil {
			doc.Logger.Debug("remediation: marked-content close skipped an unparsable page",
				slog.Int("page", page.Ref.ObjectNum), slog.Any("err", err))
			continue
		}
		_, open := content.UnclosedMarkedContent(ops)
		if open == 0 {
			continue
		}

		// The closing stream starts on a new line: /Contents streams are read
		// as one, and an EMC glued to the last operator of the stream before
		// it would be read as part of that operator's name.
		closing := append([]byte{'\n'}, bytes.Repeat([]byte("EMC\n"), open)...)
		var contents ast.PdfObject
		if end := content.TerminatedEnd(ctx, data); end < len(data) {
			body := make([]byte, 0, end+len(closing))
			body = append(body, data[:end]...)
			body = append(body, closing...)
			contents = alloc.Alloc(ast.NewStream(ast.PdfDict{"Length": ast.PdfInt(len(body))}, body))
		} else {
			closingRef := alloc.Alloc(ast.NewStream(ast.PdfDict{"Length": ast.PdfInt(len(closing))}, closing))
			contents = append(contentElements(ctx, alloc, doc, contentsObj), closingRef)
		}

		newPage := make(ast.PdfDict, len(pageDict))
		maps.Copy(newPage, pageDict)
		newPage["Contents"] = contents
		alloc.AllocAt(page.Ref.ObjectNum, newPage)
		count++
	}
	if count == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("closed the marked content left open on %d page(s)", count)}, nil
}

// contentElements returns the elements of a page's /Contents as a new array, as
// an earlier fixer left them: the elements of an array, direct or reached
// through a reference, or the single stream. The array is a copy, so an
// indirect /Contents array another page shares is never extended in place.
func contentElements(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, contentsObj ast.PdfObject) ast.PdfArray {
	resolved := contentsObj
	if ref, ok := contentsObj.(ast.PdfIndirectRef); ok {
		resolved = liveObject(ctx, alloc, doc, ref)
	}
	if arr, ok := resolved.(ast.PdfArray); ok {
		return append(ast.PdfArray(nil), arr...)
	}
	return ast.PdfArray{contentsObj}
}
