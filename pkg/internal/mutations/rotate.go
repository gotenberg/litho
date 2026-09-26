package mutations

import (
	"context"
	"fmt"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// RotatePages modifies /Rotate on the specified page dictionaries. An empty
// pageNums rotates every page, matching [StampText] and the "default: all"
// the rotate command advertises for its -pages flag.
//
// Without this, rotating a whole document was a silent no-op: the CLI leaves
// its page list empty when -pages is absent, the loop below ran zero times, and
// litho reported success over a byte-identical file.
//
// Every page is every page the document holds as alloc stages it, so a page
// an earlier insertion added is rotated too (see [pagesToEdit]).
func RotatePages(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pages []*document.Page, rotation int, pageNums []int) error {
	pages, pageNums = pagesToEdit(ctx, alloc, doc, pages, pageNums)

	// Fold the angle into the four values /Rotate is documented to take.
	// ISO 32000-1, Table 30 only requires a multiple of 90, so 720 and -90 are
	// legal and veraPDF accepts them, but writing them contradicts what
	// pdf.Writer.RotatePages promises and leaves a reader to normalise a full
	// turn or a negative angle on litho's behalf.
	rotation = ((rotation % 360) + 360) % 360
	for _, pageNum := range pageNums {
		if err := ctx.Err(); err != nil {
			return err
		}
		if pageNum < 1 || pageNum > len(pages) {
			return fmt.Errorf("page %d is out of bounds (document has %d pages)", pageNum, len(pages))
		}
		p := pages[pageNum-1]

		// Clone the page dict and set /Rotate. Start from whatever the
		// allocator already holds: inside `pipe` an earlier stage may have
		// rewritten this page, and copying the source dict would throw its
		// edits away.
		base := latestPageDict(alloc, p)
		newDict := make(ast.PdfDict, len(base)+1)
		maps.Copy(newDict, base)
		newDict["Rotate"] = ast.PdfInt(rotation)

		// Write the modified page at its existing object number. A page
		// inlined in a /Kids array has none, and there is nothing to replace in
		// an incremental update; say so rather than report a rotation that
		// never reached the file.
		if !alloc.AllocAt(p.Ref.ObjectNum, newDict) {
			return fmt.Errorf("rotating page %d: %w", pageNum, ErrPageNotAddressable)
		}
	}
	return nil
}
