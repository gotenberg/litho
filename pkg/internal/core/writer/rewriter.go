package writer

import (
	"context"
	"fmt"
	"io"
	"sort"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// RewriteConfig holds the parameters for a full file rewrite.
type RewriteConfig struct {
	// Version is the PDF version string (e.g., "1.4", "2.0").
	Version string
	// Allocator holds all objects to be written.
	Allocator *ObjAllocator
	// Trailer is the trailer dict (/Root, /Info, /ID, etc.).
	// /Size is set automatically.
	Trailer ast.PdfDict
	// UseXRefStream forces xref stream format instead of a traditional table.
	UseXRefStream bool
}

// WriteRewrite emits a complete PDF file to dst from the objects in the
// allocator. Objects are written in ascending object-number order with
// sequential byte offsets.
func WriteRewrite(ctx context.Context, dst io.Writer, cfg RewriteConfig) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("rewrite canceled: %w", err)
	}

	version := cfg.Version
	if version == "" {
		version = "1.4"
	}

	var offset int64

	// Write header.
	header := fmt.Sprintf("%%PDF-%s\n%%\xe2\xe3\xcf\xd3\n", version)
	n, err := io.WriteString(dst, header)
	offset += int64(n)
	if err != nil {
		return fmt.Errorf("writing header: %w", err)
	}

	// Serialize all pending objects in order.
	pending := cfg.Allocator.Pending()
	objNums := make([]int, 0, len(pending))
	for num := range pending {
		objNums = append(objNums, num)
	}
	sort.Ints(objNums)

	records := []XRefRecord{
		{ObjNum: 0, Offset: 0, Generation: 65535, Free: true},
	}

	for _, objNum := range objNums {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("rewrite canceled: %w", err)
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

	// Build trailer.
	trailer := make(ast.PdfDict, len(cfg.Trailer)+1)
	for k, v := range cfg.Trailer {
		if k == "Prev" || k == "Size" {
			continue
		}
		trailer[k] = v
	}
	trailer["Size"] = ast.PdfInt(cfg.Allocator.NextObjNum())

	// Write xref.
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
	if _, err := io.WriteString(dst, tail); err != nil {
		return fmt.Errorf("writing startxref: %w", err)
	}

	return nil
}
