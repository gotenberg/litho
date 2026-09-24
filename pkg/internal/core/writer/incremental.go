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
	// Trailer is the base trailer dict. The writer adds /Prev, updates /Size,
	// and drops any /XRefStm carried over from the source, whose value applies
	// only to the section it came from.
	Trailer ast.PdfDict
	// UseXRefStream forces xref stream format instead of a traditional table.
	UseXRefStream bool
	// Generation reports the generation number the source file gives an
	// object, so replacing it keeps the identity its references already name
	// (ISO 32000-1, 7.3.10). Report 0 for an object the source does not define
	// in use, which is what a newly allocated one gets. A nil func writes every
	// object at generation 0.
	Generation func(objNum int) int
}

// WriteIncremental appends an incremental update to dst. It copies the
// original file bytes, then appends the new/modified objects and a new xref
// section with /Prev pointing to the original startxref offset.
func WriteIncremental(ctx context.Context, dst io.Writer, cfg IncrementalConfig) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("incremental update canceled: %w", err)
	}

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

		genNum := 0
		if cfg.Generation != nil {
			genNum = cfg.Generation(objNum)
		}

		records = append(records, XRefRecord{
			ObjNum:     objNum,
			Offset:     offset,
			Generation: genNum,
		})

		written, err := SerializeIndirect(ctx, dst, objNum, genNum, pending[objNum])
		if err != nil {
			return fmt.Errorf("serializing object %d: %w", objNum, err)
		}
		offset += written
	}

	// A cross-reference section must begin with the object-0 free-list head
	// (ISO 32000-1 §7.5.4). A trailer-only incremental update (e.g. adding /ID)
	// changes no objects, leaving records empty; without the free head the
	// traditional path would emit a subsection-less "xref\ntrailer", which is an
	// invalid xref table. Emit the mandatory free entry so the appended section
	// is well-formed.
	if len(records) == 0 {
		records = append(records, XRefRecord{ObjNum: 0, Generation: 65535, Free: true})
	}

	// Build the new trailer.
	trailer := make(ast.PdfDict, len(cfg.Trailer)+2)
	maps.Copy(trailer, cfg.Trailer)
	trailer["Size"] = ast.PdfInt(cfg.Allocator.NextObjNum())
	trailer["Prev"] = ast.PdfInt(cfg.StartXRefOffset)

	// Per ISO 32000-1 §7.5.8.4, /XRefStm is the byte offset of the
	// cross-reference stream that supplements the cross-reference section whose
	// trailer carries it. The value is section-local: it describes the hybrid
	// half of that one section, not of any section appended later. Copying the
	// source trailer's /XRefStm into the section this update appends would point
	// it at a stream belonging to an older revision, resurrecting that
	// revision's entries for objects this update just rewrote. The appended
	// section is a plain table with no hybrid half, so the key must be dropped.
	// The older section keeps its own /XRefStm and stays reachable through /Prev.
	delete(trailer, "XRefStm")

	// Write xref section.
	xrefOffset := offset

	if cfg.UseXRefStream {
		if _, err := WriteXRefStream(ctx, dst, records, trailer, cfg.Allocator, xrefOffset); err != nil {
			return fmt.Errorf("writing xref stream: %w", err)
		}
	} else {
		if _, err := WriteXRefTable(ctx, dst, records, trailer); err != nil {
			return fmt.Errorf("writing xref table: %w", err)
		}
	}

	tail := fmt.Sprintf("startxref\n%d\n%%%%EOF\n", xrefOffset)
	_, err = io.WriteString(dst, tail)
	if err != nil {
		return fmt.Errorf("writing startxref: %w", err)
	}

	return nil
}
