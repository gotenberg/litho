package mutations

import (
	"context"
	"fmt"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// ExcisePageSpans removes byte spans from a page's content and writes what
// remains back as the page's only content stream.
//
// The spans address the buffer [collectLatestPageContents] returns for this
// page, the page as alloc stages it, so derive them by parsing that same
// buffer with [content.ParseStreamDataWithOffsets]. Offsets taken over any other assembly
// of the page describe different bytes: litho assembles page content in more
// than one way, and the variants differ in their separators and in which
// streams they include, so a span from one applied to another removes content
// the caller never selected.
//
// It refuses where other mutations skip. A page inlined in a /Kids array
// (ISO 32000-1, 7.7.3.2) has no object number to write back to, so the content
// would survive in a document whose caller believes it gone; that reports
// [ErrPageNotAddressable] instead. [collectLatestPageContents] refuses on the same
// grounds, failing on a stream it cannot decode rather than offering a partial
// buffer to excise against.
//
// Removing nothing leaves the page alone rather than rewriting its /Contents
// to say the same thing in a single stream.
func ExcisePageSpans(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	page *document.Page,
	spans []content.ByteRange,
) error {
	if len(spans) == 0 {
		return nil
	}

	// Checked before anything is allocated: a replacement stream for a page
	// that cannot be written back is an orphan in the output.
	if page.Ref.ObjectNum == 0 {
		return ErrPageNotAddressable
	}

	data, err := collectLatestPageContents(ctx, alloc, doc, page)
	if err != nil {
		return fmt.Errorf("assembling page content: %w", err)
	}

	out, err := ExciseSpans(data, spans)
	if err != nil {
		return fmt.Errorf("excising page content: %w", err)
	}

	// Read the page dict alloc-first so an edit an earlier stage queued at this
	// object number survives rather than being overwritten.
	baseDict := latestPageDict(alloc, page)
	newDict := make(ast.PdfDict, len(baseDict))
	maps.Copy(newDict, baseDict)
	newDict["Contents"] = alloc.Alloc(ast.NewStream(ast.PdfDict{"Length": ast.PdfInt(len(out))}, out))

	if !alloc.AllocAt(page.Ref.ObjectNum, newDict) {
		return ErrPageNotAddressable
	}
	return nil
}
