package writer

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"strconv"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/filter"
)

// maxObjStmObjects bounds how many objects one object stream carries. It is a
// packing choice, not spec-mandated: a reader must inflate and scan the whole
// stream to reach any object in it, so an unbounded stream turns a single
// object lookup into a large decode. 200 keeps each stream cheap to decode
// while still amortising the per-object "N 0 obj"/"endobj" overhead that
// packing exists to remove. Real producers sit in the same range.
const maxObjStmObjects = 200

// maxObjStmPayload bounds the uncompressed bytes in one object stream. It is
// an anti-DoS guard on the WRITE side mirroring maxObjStmCacheBytes on the
// read side: a document of unusually large dictionaries must not produce a
// single stream whose inflation dwarfs the file it came from. 8 MiB is far
// above any real dictionary population and still bounded.
const maxObjStmPayload = 8 << 20

// objStmFixedOverhead is what one object stream costs beyond its compressed
// payload: the "N 0 obj"/"endobj" wrapper, the "stream"/"endstream" keywords,
// and a dictionary carrying /Type, /N, /First, /Length and /Filter. Measured
// at just under 100 bytes for realistic object numbers, and rounded up so the
// packing decision errs toward leaving objects alone.
const objStmFixedOverhead = 110

// directCost returns what the given objects cost written directly, which is
// their serialized bodies plus each one's "N 0 obj\n" header and "\nendobj\n"
// trailer. bodiesLen already includes the one-byte separator each body
// carries inside an object stream, so the comparison slightly favours writing
// directly, which is the conservative direction.
func directCost(objNums []int, bodiesLen int) int {
	const wrapper = len(" 0 obj\n") + len("\nendobj\n")
	cost := bodiesLen
	for _, n := range objNums {
		cost += len(strconv.Itoa(n)) + wrapper
	}
	return cost
}

// PackedObject records where one packed object landed.
type PackedObject struct {
	ObjNum      int
	ObjStmNum   int
	ObjStmIndex int
}

// packable reports whether obj may live inside an object stream.
//
// ISO 32000-1, 7.5.7 excludes streams, since a stream cannot nest inside
// another, and requires every contained object to have generation 0. Object 0
// is the free-list head and never holds an object at all. The /Encrypt
// dictionary is excluded document-wide by refusing to pack an encrypted file;
// see [PackObjectStreams].
func packable(objNum int, obj ast.PdfObject) bool {
	if objNum <= 0 || obj == nil {
		return false
	}
	_, isStream := obj.(ast.PdfStream)
	return !isStream
}

// PackObjectStreams moves eligible objects out of the allocator's pending set
// and into newly allocated /Type /ObjStm streams, returning the placement of
// each packed object. Objects that cannot be packed are left untouched.
//
// The caller must NOT pack an encrypted document. Encryption runs over the
// pending map before serialization, so packing afterwards would place already
// encrypted strings inside a stream that is then encrypted again, while
// ISO 32000-1, 7.5.7 requires strings in an object stream never to be
// encrypted separately. [WriteRewrite] enforces this by refusing to pack when
// the trailer carries /Encrypt.
//
// Objects are packed in ascending object-number order so the output is
// deterministic, as the reproducible-output rule requires.
func PackObjectStreams(ctx context.Context, alloc *ObjAllocator) ([]PackedObject, error) {
	pending := alloc.Pending()

	candidates := make([]int, 0, len(pending))
	for num, obj := range pending {
		if packable(num, obj) {
			candidates = append(candidates, num)
		}
	}
	// Packing a lone object spends a stream dictionary and a Flate header to
	// save one "N 0 obj"/"endobj" pair, which is a net loss. Two is the
	// smallest grouping that can win.
	if len(candidates) < 2 {
		return nil, nil
	}
	sort.Ints(candidates)

	var placements []PackedObject
	for start := 0; start < len(candidates); {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("packing object streams: %w", err)
		}
		end := min(start+maxObjStmObjects, len(candidates))

		placed, consumed, err := writeOneObjStm(ctx, alloc, pending, candidates[start:end])
		if err != nil {
			return nil, err
		}
		if consumed == 0 {
			// One object larger than the payload bound stays unpacked
			// rather than stalling the loop.
			start++
			continue
		}
		placements = append(placements, placed...)
		start += consumed
	}
	return placements, nil
}

// writeOneObjStm builds one object stream from as many of batch as fit within
// [maxObjStmPayload], removes those objects from pending, and reports their
// placements along with the number consumed.
func writeOneObjStm(
	ctx context.Context,
	alloc *ObjAllocator,
	pending map[int]ast.PdfObject,
	batch []int,
) (placed []PackedObject, consumed int, err error) {
	var header, bodies bytes.Buffer

	for _, objNum := range batch {
		var body bytes.Buffer
		if _, err := Serialize(ctx, &body, pending[objNum]); err != nil {
			return nil, 0, fmt.Errorf("serializing object %d for its object stream: %w", objNum, err)
		}
		if bodies.Len()+body.Len() > maxObjStmPayload && consumed > 0 {
			break
		}
		// Each header pair is the object number and the body's offset from
		// /First, so the offset is measured against the bodies written so
		// far and never against the header that precedes them.
		header.WriteString(strconv.Itoa(objNum))
		header.WriteByte(' ')
		header.WriteString(strconv.Itoa(bodies.Len()))
		header.WriteByte(' ')
		bodies.Write(body.Bytes())
		// Objects are whitespace-delimited inside the payload: without a
		// separator two adjacent numeric objects lex as a single token.
		bodies.WriteByte('\n')
		consumed++
	}
	if consumed == 0 {
		return nil, 0, nil
	}

	// Built into a fresh slice rather than by appending to header.Bytes(),
	// which aliases the buffer's own storage and would write the bodies into
	// its spare capacity.
	payload := make([]byte, 0, header.Len()+bodies.Len())
	payload = append(payload, header.Bytes()...)
	payload = append(payload, bodies.Bytes()...)

	compressed, err := filter.ApplyEncodeFilter(payload, "FlateDecode")
	if err != nil {
		return nil, 0, fmt.Errorf("compressing object stream: %w", err)
	}

	// Decline a batch that packing would not shrink. Measured on the corpus,
	// two ~1 KB files of five objects each came out 42 bytes LARGER packed,
	// because the stream dictionary and Flate header cost more than the
	// "N 0 obj"/"endobj" wrappers they replace. Growth is the regression this
	// packing exists to remove, so never-worse matters more than packing
	// everything: those objects stay pending and are written directly.
	if len(compressed)+objStmFixedOverhead >= directCost(batch[:consumed], bodies.Len()) {
		// Report the objects actually judged, not the whole batch: the
		// payload bound can stop the loop early, and claiming the rest were
		// consumed would leave objects this call never looked at unpacked
		// for good. They stay pending either way, so declining costs only
		// the packing, never the object.
		return nil, consumed, nil
	}

	dict := ast.PdfDict{
		"Type":   ast.PdfName("ObjStm"),
		"N":      ast.PdfInt(consumed),
		"First":  ast.PdfInt(header.Len()),
		"Length": ast.PdfInt(len(compressed)),
		"Filter": ast.PdfName("FlateDecode"),
	}
	streamRef := alloc.Alloc(ast.NewStream(dict, compressed))

	placed = make([]PackedObject, 0, consumed)
	for i, objNum := range batch[:consumed] {
		delete(pending, objNum)
		placed = append(placed, PackedObject{
			ObjNum:      objNum,
			ObjStmNum:   streamRef.ObjectNum,
			ObjStmIndex: i,
		})
	}
	return placed, consumed, nil
}
