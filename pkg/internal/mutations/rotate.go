package mutations

import (
	"context"
	"fmt"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// RotatePages modifies /Rotate on the specified page dictionaries.
func RotatePages(ctx context.Context, alloc *writer.ObjAllocator, pages []*document.Page, rotation int, pageNums []int) error {
	for _, pageNum := range pageNums {
		if err := ctx.Err(); err != nil {
			return err
		}
		if pageNum < 1 || pageNum > len(pages) {
			return fmt.Errorf("page %d is out of bounds (document has %d pages)", pageNum, len(pages))
		}
		p := pages[pageNum-1]

		// Clone the page dict and set /Rotate.
		newDict := make(ast.PdfDict, len(p.Dict)+1)
		maps.Copy(newDict, p.Dict)
		newDict["Rotate"] = ast.PdfInt(rotation)

		// Write the modified page at its existing object number.
		alloc.AllocAt(p.Ref.ObjectNum, newDict)
	}
	return nil
}
