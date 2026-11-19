package writer

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sort"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/filter"
	"github.com/gotenberg/litho/pkg/internal/limits"
)

// ErrCompressionDiverged is returned, wrapped, when a stream [CompressStreams]
// compressed no longer compresses to the size it measured when the output is
// serialized: its source bytes changed in between.
var ErrCompressionDiverged = errors.New("a compressed stream no longer compresses as measured")

// StreamCompression configures [CompressStreams]: the budgets that bound the
// pass.
type StreamCompression struct {
	// Limits bounds the pass: a stream whose data passes MaxStreamBytes is
	// left as it is, and MaxStreamBytes also bounds how many compressed bytes
	// the pass keeps in memory.
	Limits limits.Limits
}

// flateFilterEntry is what the /Filter entry adds to a stream's dictionary,
// which the compressed data has to save on top to win.
const flateFilterEntry = len("/Filter /FlateDecode")

// CompressStreams compresses with FlateDecode (ISO 32000-1, 7.4.4) the streams
// pending in alloc that carry no filter, keeping a compression only when the
// stream serializes smaller. It returns how many streams it compressed.
//
// FlateDecode is lossless, so a reader decodes the bytes the stream held: a
// content stream draws the same, and a font program, an ICC profile or an
// embedded file keeps its bytes. Only /Filter and /Length change.
//
// A stream is left as it is when:
//
//   - It is the XMP metadata of the document or of an object: ISO 19005-1,
//     6.7.2 forbids a filter on the catalog's, and XMP is meant to be read
//     without one.
//   - It is an image: [RecompressImages] owns images, and weighs a predictor
//     against plain compression.
//   - It names external data (/F) or carries /DecodeParms.
//   - Its data passes the stream budget, or cannot be read.
//
// Compressed bytes are kept in memory up to the stream budget in total. Past
// it, a stream is compressed again from its source when it is serialized.
// Streams are visited in object-number order, and zlib output depends on its
// input alone, so the result does too.
//
// The caller must not run this on an encrypted document, as for the other
// compaction passes (see [Compact]).
func CompressStreams(ctx context.Context, alloc *ObjAllocator, cfg StreamCompression) (int, error) {
	lim := cfg.Limits.WithDefaults()
	pending := alloc.Pending()

	nums := make([]int, 0, len(pending))
	for num, obj := range pending {
		if s, ok := obj.(ast.PdfStream); ok && compressible(s.Dict) {
			nums = append(nums, num)
		}
	}
	sort.Ints(nums)

	var compressed int
	var retained int64
	for _, num := range nums {
		if err := ctx.Err(); err != nil {
			return compressed, fmt.Errorf("compressing streams: %w", err)
		}
		s := pending[num].(ast.PdfStream) // fits: nums holds streams only
		data, err := s.Bytes(ctx)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return compressed, fmt.Errorf("compressing streams: %w", ctxErr)
			}
			continue
		}
		if int64(len(data)) > lim.MaxStreamBytes {
			continue
		}
		// The compression wins only below the data's size less the entry
		// it adds, so it stops one byte short of that.
		limit := len(data) - flateFilterEntry - 1
		if limit <= 0 {
			continue
		}
		encoded, ok, err := filter.CompressFlate(data, limit)
		if err != nil {
			return compressed, fmt.Errorf("compressing stream %d: %w", num, err)
		}
		if !ok {
			continue
		}
		dict := maps.Clone(s.Dict)
		dict["Filter"] = ast.PdfName("FlateDecode")
		dict["Length"] = ast.PdfInt(len(encoded))
		if size := int64(len(encoded)); retained+size <= lim.MaxStreamBytes {
			retained += size
			alloc.AllocAt(num, ast.NewStream(dict, encoded))
		} else {
			alloc.AllocAt(num, ast.NewLazyStream(dict, recompressor(s.Bytes, len(encoded))))
		}
		compressed++
	}
	return compressed, nil
}

// recompressor returns the loader of a stream whose compression the pass did
// not keep in memory: it compresses the source bytes raw reads again, and
// fails with [ErrCompressionDiverged] when they no longer compress to want
// bytes, which the stream's /Length already says.
func recompressor(raw func(context.Context) ([]byte, error), want int) func(context.Context) ([]byte, error) {
	return func(ctx context.Context) ([]byte, error) {
		data, err := raw(ctx)
		if err != nil {
			return nil, fmt.Errorf("reading a compressed stream's source: %w", err)
		}
		encoded, _, err := filter.CompressFlate(data, -1)
		if err != nil {
			return nil, fmt.Errorf("compressing a stream's source: %w", err)
		}
		if len(encoded) != want {
			return nil, fmt.Errorf("%w: %d bytes where %d were measured", ErrCompressionDiverged, len(encoded), want)
		}
		return encoded, nil
	}
}

// compressible reports whether a stream with dict qualifies for
// [CompressStreams]: no filter, no external data, and neither XMP metadata,
// an image, nor a structure the writer itself compresses.
func compressible(dict ast.PdfDict) bool {
	if hasFilter(dict["Filter"]) {
		return false
	}
	for _, key := range []ast.PdfName{"DecodeParms", "F", "FFilter", "FDecodeParms"} {
		if _, ok := dict[key]; ok {
			return false
		}
	}
	typ, _ := dict["Type"].(ast.PdfName)
	subtype, _ := dict["Subtype"].(ast.PdfName)
	switch {
	case typ == "Metadata", subtype == "XML":
		return false
	case typ == "XRef", typ == "ObjStm":
		return false
	case subtype == "Image":
		return false
	}
	return true
}

// hasFilter reports whether a /Filter value names a filter: a name, or an
// array holding one. An absent, null or empty value names none.
func hasFilter(v ast.PdfObject) bool {
	switch f := v.(type) {
	case nil, ast.PdfNull:
		return false
	case ast.PdfArray:
		return len(f) > 0
	default:
		return true
	}
}
