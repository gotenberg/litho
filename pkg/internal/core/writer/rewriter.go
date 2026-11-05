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
	// /Size is set automatically. A stream written directly in it is moved
	// into an object of its own, in place (see [HoistDirectStreams]).
	Trailer ast.PdfDict
	// UseXRefStream forces xref stream format instead of a traditional table.
	// A cross-reference stream is a PDF 1.5 construct (ISO 32000-1, 7.5.8),
	// so selecting one raises the version the output declares.
	UseXRefStream bool
	// Packed names the objects [Compact] moved into object streams, so that
	// the cross-reference names each by its stream (ISO 32000-1, 7.5.8.3). A
	// non-empty list implies a cross-reference stream, since a traditional
	// table cannot express a type-2 entry. Object streams and cross-reference
	// streams are both PDF 1.5 constructs, so a packed file declares at least
	// that version.
	Packed []PackedObject
}

// CompactConfig selects the passes [Compact] runs.
type CompactConfig struct {
	// PackObjectStreams packs eligible objects into /Type /ObjStm streams
	// (ISO 32000-1, 7.5.7); see [PackObjectStreams].
	PackObjectStreams bool
	// DedupStreams merges byte-identical stream objects, repointing
	// references at one survivor; see [DedupStreams]. Note that it rewrites
	// references inside the trailer in place.
	DedupStreams bool
	// RecompressImages, when not nil, re-encodes image streams losslessly
	// where that makes them smaller; see [RecompressImages].
	RecompressImages *ImageRecompression
}

// Compact runs the compaction passes cfg selects over the objects pending in
// alloc and the values of trailer, and returns where packing placed each
// object, for [RewriteConfig.Packed]. It runs before [WriteRewrite], and
// before the pending objects are encrypted: a trailer that already carries
// /Encrypt is refused with [ErrCompactEncrypted].
//
// Encryption keys every string and stream to the number of the object holding
// it (ISO 32000-1, 7.6.2, Algorithm 1) and runs over the pending set, so each
// pass has to see the plaintext objects, in their final numbers. Packing
// serializes an object's strings into an object stream, which the sweep then
// encrypts as one stream, as 7.5.7 requires of the strings an object stream
// holds; dedup compares stream bytes; recompression decodes image data. Run
// after the sweep, packing would encrypt strings twice, dedup would compare
// ciphertext keyed per object number and find nothing, and recompression
// would decode ciphertext as image data.
//
// Every stream goes out as an indirect object (ISO 32000-1, 7.3.8.1), and the
// passes must see the objects a direct stream becomes: dedup compares whole
// stream objects, and packing would otherwise seal one inside an object
// stream, which 7.5.7 forbids. So the direct streams are hoisted first.
//
// Recompression runs before dedup, so two copies of one image, which
// re-encode to identical bytes, still merge. Dedup runs before packing, which
// removes objects from the pending set and serializes their bodies, so a
// later dedup would neither see the full population nor be able to repoint
// what it merged.
func Compact(ctx context.Context, alloc *ObjAllocator, trailer ast.PdfDict, cfg CompactConfig) ([]PackedObject, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("compaction canceled: %w", err)
	}
	if _, encrypted := trailer["Encrypt"]; encrypted {
		return nil, ErrCompactEncrypted
	}

	if _, err := HoistDirectStreams(ctx, alloc, trailer); err != nil {
		return nil, fmt.Errorf("hoisting direct streams: %w", err)
	}

	if cfg.RecompressImages != nil {
		if _, err := RecompressImages(ctx, alloc, *cfg.RecompressImages); err != nil {
			return nil, fmt.Errorf("recompressing images: %w", err)
		}
	}

	if cfg.DedupStreams {
		if _, err := DedupStreams(ctx, alloc, trailer); err != nil {
			return nil, fmt.Errorf("deduplicating streams: %w", err)
		}
	}

	if !cfg.PackObjectStreams {
		return nil, nil
	}
	packed, err := PackObjectStreams(ctx, alloc, trailer)
	if err != nil {
		return nil, fmt.Errorf("packing object streams: %w", err)
	}
	return packed, nil
}

// WriteRewrite emits a complete PDF file to dst from the objects in the
// allocator. Objects are written in ascending object-number order with
// sequential byte offsets. The compaction passes run before it, through
// [Compact], and before any encryption of the pending objects.
func WriteRewrite(ctx context.Context, dst io.Writer, cfg RewriteConfig) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("rewrite canceled: %w", err)
	}

	// Every stream goes out as an indirect object (ISO 32000-1, 7.3.8.1). An
	// encrypted document's streams were moved before encryption keyed each to
	// its object number (see [HoistDirectStreams]). Moving one now would
	// leave it keyed to its old container, so the serializer refuses what is
	// left.
	_, encrypted := cfg.Trailer["Encrypt"]
	if !encrypted {
		if _, err := HoistDirectStreams(ctx, cfg.Allocator, cfg.Trailer); err != nil {
			return fmt.Errorf("hoisting direct streams: %w", err)
		}
	}

	useXRefStream := cfg.UseXRefStream || len(cfg.Packed) > 0

	version := cfg.Version
	if version == "" {
		version = "1.4"
	}
	if useXRefStream {
		// Object streams and cross-reference streams are both PDF 1.5
		// constructs (ISO 32000-1, 7.5.7 and 7.5.8). Declaring anything
		// earlier contradicts the file's own contents: a 1.4 reader cannot
		// parse the cross-reference, and litho's own PDF/A-1 check reports
		// the file as using xref streams (ISO 19005-1, 6.1.4). The catalog
		// /Version overrides the header (ISO 32000-1, 7.5.2), so a file
		// whose catalog says 1.4 would keep reporting 1.4 however the header
		// reads: it is raised too. Packing never seals the catalog inside an
		// object stream (see [PackObjectStreams]), so it is still pending.
		version = atLeastVersion(version, minXRefStreamVersion)
		raiseCatalogVersion(cfg.Allocator, cfg.Trailer, minXRefStreamVersion)
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
	for _, p := range cfg.Packed {
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
