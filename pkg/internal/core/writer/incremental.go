package writer

import (
	"context"
	"fmt"
	"io"
	"maps"
	"sort"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// IncrementalConfig holds the parameters for an incremental update.
type IncrementalConfig struct {
	// Source is the original PDF file.
	Source io.ReaderAt
	// SourceSize is the byte length of the original file.
	SourceSize int64
	// StartXRefOffset is the byte offset of the last xref section in the
	// original file (becomes /Prev in the new trailer).
	StartXRefOffset int64
	// Allocator holds the new/modified objects to append.
	Allocator *ObjAllocator
	// Trailer is the base trailer dict. The writer adds /Prev and updates
	// /Size automatically.
	Trailer ast.PdfDict
	// UseXRefStream forces xref stream format instead of a traditional table.
	UseXRefStream bool
}

// WriteIncremental appends an incremental update to dst. It copies the
// original file bytes, then appends the new/modified objects and a new xref
// section with /Prev pointing to the original startxref offset.
func WriteIncremental(ctx context.Context, dst io.Writer, cfg IncrementalConfig) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("incremental update canceled: %w", err)
	}

	// Copy original file bytes.
	src := io.NewSectionReader(cfg.Source, 0, cfg.SourceSize)
	n, err := io.Copy(dst, src)
	if err != nil {
		return fmt.Errorf("copying original PDF bytes: %w", err)
	}
	offset := n // current write position

	// Ensure a newline separates the original content from the appended data.
	nn, err := io.WriteString(dst, "\n")
	offset += int64(nn)
	if err != nil {
		return fmt.Errorf("writing separator: %w", err)
	}

	// Serialize each pending object, recording byte offsets.
	pending := cfg.Allocator.Pending()
	objNums := make([]int, 0, len(pending))
	for num := range pending {
		objNums = append(objNums, num)
	}
	sort.Ints(objNums)

	records := make([]XRefRecord, 0, len(objNums))
	for _, objNum := range objNums {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("incremental update canceled: %w", err)
		}

		records = append(records, XRefRecord{
			ObjNum: objNum,
			Offset: offset,
		})

		written, err := SerializeIndirect(ctx, dst, objNum, pending[objNum])
		if err != nil {
			return fmt.Errorf("serializing object %d: %w", objNum, err)
		}
		offset += written
	}

	// Build the new trailer.
	trailer := make(ast.PdfDict, len(cfg.Trailer)+2)
	maps.Copy(trailer, cfg.Trailer)
	trailer["Size"] = ast.PdfInt(cfg.Allocator.NextObjNum())
	trailer["Prev"] = ast.PdfInt(cfg.StartXRefOffset)

	// Write xref section.
	xrefOffset := offset

	if cfg.UseXRefStream {
		if _, err := WriteXRefStream(ctx, dst, records, trailer, cfg.Allocator); err != nil {
			return fmt.Errorf("writing xref stream: %w", err)
		}
	} else {
		if _, _, err := WriteXRefTable(ctx, dst, records, trailer); err != nil {
			return fmt.Errorf("writing xref table: %w", err)
		}
	}

	// Write startxref + %%EOF.
	tail := fmt.Sprintf("startxref\n%d\n%%%%EOF\n", xrefOffset)
	_, err = io.WriteString(dst, tail)
	if err != nil {
		return fmt.Errorf("writing startxref: %w", err)
	}

	return nil
}
