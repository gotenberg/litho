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
	// Version is the PDF version string (e.g., "1.4", "2.0"). It is raised to
	// minXRefStreamVersion when the output uses a cross-reference stream,
	// which that construct requires, in the header and in the catalog
	// /Version entry when the source carried one.
	Version string
	// Allocator holds all objects to be written.
	Allocator *ObjAllocator
	// Trailer is the trailer dict (/Root, /Info, /ID, etc.).
	// /Size is set automatically.
	Trailer ast.PdfDict
	// UseXRefStream forces xref stream format instead of a traditional table.
	// A cross-reference stream is a PDF 1.5 construct (ISO 32000-1, 7.5.8),
	// so selecting one raises the version the output declares.
	UseXRefStream bool
	// PackObjectStreams packs eligible objects into /Type /ObjStm streams
	// (ISO 32000-1, 7.5.7). Packing implies a cross-reference stream, since
	// a traditional table cannot express a type-2 entry, and is refused
	// outright for an encrypted document; see [PackObjectStreams]. Both are
	// PDF 1.5 constructs, so a packed file declares at least that version.
	PackObjectStreams bool
	// DedupStreams merges byte-identical stream objects before writing,
	// repointing references at one survivor. Refused for an encrypted
	// document for the reason [DedupStreams] gives. Note that it rewrites
	// references inside Trailer in place.
	DedupStreams bool
}

// WriteRewrite emits a complete PDF file to dst from the objects in the
// allocator. Objects are written in ascending object-number order with
// sequential byte offsets.
func WriteRewrite(ctx context.Context, dst io.Writer, cfg RewriteConfig) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("rewrite canceled: %w", err)
	}

	// Compact before the header goes out and before the objects are
	// enumerated. Packing decides whether this file needs a cross-reference
	// stream, and that decision sets the minimum version the header must
	// declare, so it cannot run once the header is already on the wire.
	// Packing also removes objects from the allocator's pending set and
	// allocates the streams that now hold them, and Pending returns that live
	// map, so an enumeration taken earlier would name objects this file no
	// longer writes directly. Neither compaction pass writes to dst.
	//
	// Encryption has already run over every pending object by the time a
	// rewrite reaches this point, so both compaction passes are refused for
	// an encrypted document: packing would encrypt an object stream's
	// strings a second time, which ISO 32000-1, 7.5.7 forbids, and dedup
	// would compare ciphertext keyed per object number and find nothing.
	// The file is written uncompacted rather than incorrectly.
	_, encrypted := cfg.Trailer["Encrypt"]

	// The catalog /Version overrides the header (ISO 32000-1, 7.5.2), so a
	// file whose catalog says 1.4 keeps reporting 1.4 however the header
	// reads. Raise it before packing, which would otherwise seal it inside a
	// compressed stream, and undo the raise if packing then declines.
	restoreCatalogVersion := func() {}
	if cfg.UseXRefStream || (cfg.PackObjectStreams && !encrypted) {
		restoreCatalogVersion = raiseCatalogVersion(cfg.Allocator, cfg.Trailer, minXRefStreamVersion)
	}

	// Dedup first. Packing removes objects from the pending set and
	// serializes their bodies, so a later dedup would neither see the full
	// population nor be able to repoint what it merged.
	if cfg.DedupStreams && !encrypted {
		if _, err := DedupStreams(ctx, cfg.Allocator, cfg.Trailer); err != nil {
			return fmt.Errorf("deduplicating streams: %w", err)
		}
	}

	useXRefStream := cfg.UseXRefStream
	var packed []PackedObject
	if cfg.PackObjectStreams && !encrypted {
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
	if !useXRefStream {
		// Packing declined, so the output carries nothing that needs 1.5 and
		// must come out byte-identical to an uncompacted rewrite.
		restoreCatalogVersion()
	}

	version := cfg.Version
	if version == "" {
		version = "1.4"
	}
	if useXRefStream {
		// Object streams and cross-reference streams are both PDF 1.5
		// constructs (ISO 32000-1, 7.5.7 and 7.5.8). Declaring anything
		// earlier contradicts the file's own contents: a 1.4 reader cannot
		// parse the cross-reference, and litho's own PDF/A-1 check reports
		// the file as using xref streams (ISO 19005-1, 6.1.4).
		version = atLeastVersion(version, minXRefStreamVersion)
	}
	if encrypted {
		// The same rule as above applied to encryption: AESV2 is a PDF 1.6
		// crypt filter and AESV3 an ISO 32000-2 one, so a file selecting
		// either while declaring 1.4 misdescribes itself. The catalog needs
		// raising too, since its /Version overrides the header, and AES-256
		// needs the /Extensions entry that names it as an extension to 1.7
		// rather than a feature of it.
		if floor := encryptionVersionFloor(cfg.Allocator, cfg.Trailer); floor != "" {
			version = atLeastVersion(version, floor)
			raiseCatalogVersion(cfg.Allocator, cfg.Trailer, floor)
			// The extension declares what PDF 1.7 does not define on its own.
			// A file already declaring 2.0 defines AESV3 natively, so writing
			// /ADBE there is noise, and its /BaseVersion of 1.7 would
			// contradict the version the file declares. qpdf agrees: it emits
			// the extension for a 1.4 source and none for a 2.0 one.
			if floor == minAESV3Version && atLeastVersion(version, isoPDF2Version) != version {
				ensureADBEExtension(cfg.Allocator, cfg.Trailer, minAESV3Version, aesV3ExtensionLevel)
			}
		}
	}

	var offset int64

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
		if _, err := WriteXRefTable(ctx, dst, records, trailer); err != nil {
			return fmt.Errorf("writing xref table: %w", err)
		}
	}

	tail := fmt.Sprintf("startxref\n%d\n%%%%EOF\n", xrefOffset)
	if _, err := io.WriteString(dst, tail); err != nil {
		return fmt.Errorf("writing startxref: %w", err)
	}

	return nil
}
