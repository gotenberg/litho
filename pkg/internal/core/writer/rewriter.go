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
	// PackObjectStreams packs eligible objects into /Type /ObjStm streams
	// (ISO 32000-1, 7.5.7). Packing implies a cross-reference stream, since
	// a traditional table cannot express a type-2 entry, and is refused
	// outright for an encrypted document; see [PackObjectStreams].
	PackObjectStreams bool
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

	// Pack before enumerating. Packing both removes objects from the
	// allocator's pending set and allocates the streams that now hold them,
	// and Pending returns that live map, so an enumeration taken earlier
	// would name objects this file no longer writes directly.
	useXRefStream := cfg.UseXRefStream
	var packed []PackedObject
	if cfg.PackObjectStreams {
		if _, encrypted := cfg.Trailer["Encrypt"]; encrypted {
			// Encryption has already run over every pending object, so the
			// strings are ciphertext already and the stream holding them
			// would be encrypted a second time. ISO 32000-1, 7.5.7 forbids
			// encrypting strings inside an object stream separately, so the
			// file is written unpacked instead of incorrectly.
			packed = nil
		} else {
			p, err := PackObjectStreams(ctx, cfg.Allocator)
			if err != nil {
				return fmt.Errorf("packing object streams: %w", err)
			}
			packed = p
			if len(packed) > 0 {
				// A traditional cross-reference table has no type field and
				// cannot name a compressed object (ISO 32000-1, 7.5.8.3).
				useXRefStream = true
			}
		}
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

		written, err := SerializeIndirect(ctx, dst, objNum, 0, pending[objNum])
		if err != nil {
			return fmt.Errorf("serializing object %d: %w", objNum, err)
		}
		offset += written
	}

	// Objects living inside an object stream are named by a type-2 entry
	// instead of a byte offset (ISO 32000-1, 7.5.8.3).
	for _, p := range packed {
		records = append(records, XRefRecord{
			ObjNum:      p.ObjNum,
			Compressed:  true,
			ObjStmNum:   p.ObjStmNum,
			ObjStmIndex: p.ObjStmIndex,
		})
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

	if useXRefStream {
		if _, err := WriteXRefStream(ctx, dst, records, trailer, cfg.Allocator, xrefOffset); err != nil {
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
