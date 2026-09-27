package writer

import (
	"context"
	"fmt"
	"maps"
	"sort"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// HoistDirectStreams gives every stream nested inside another object an
// indirect object of its own, and puts a reference to it in its place. It
// walks the objects pending in alloc and the values of trailer, and returns the
// number of streams it moved.
//
// ISO 32000-1, 7.3.8.1 requires every stream to be an indirect object. qpdf
// reads an object holding one inline as damaged: it takes the stream data for
// unknown tokens and gives up on the object. litho's parser accepts one, as
// veraPDF's does, and code that resolves a reference and stores the object it
// names builds one, so a writer cannot trust the objects it is given to hold
// none. A stream at the top of a pending object is where it belongs and stays
// there. An object stream may hold no stream either (ISO 32000-1, 7.5.7), so
// this runs before [PackObjectStreams].
//
// RC4 and AES-128 key each string and stream to the number of the object
// holding it (ISO 32000-1, 7.6.2, Algorithm 1), so a caller that encrypts the
// pending objects runs this first: a stream moved afterwards carries data
// keyed to its old container and decrypts to noise.
//
// The walk copies a container only on the path to a stream it moves, so an
// object the allocator shares with a source document is never modified in
// place. trailer is the exception: the write that follows uses the map it is
// given, so its values are replaced in place. Objects are visited in ascending
// object number and dictionary keys in sorted order, so the numbers the moved
// streams take are the same on every run. A stream's own dictionary is walked
// before the stream is allocated, so a stream nested in it moves too.
//
// The walk recurses as deep as the objects nest, which the parser bounds for
// every object it reads, and [Serialize] recurses the same depth right after.
//
// # Corpus evidence
//
//   - All corpora: no file holds a stream inside another object among the
//     objects its cross-reference data lists, and none of 5220 files comes out
//     of a full rewrite, an incremental update, encryption, object streams or
//     remediation holding one.
//   - pdfjs poppler-937-0-fuzzed.pdf, issue13783.pdf: came out of split, merge
//     and insert holding their content stream inside a copied /CropBox, which
//     [document.ResolveRectangle] had resolved into the page box.
//   - litho directstream_2b.pdf: holds four streams inline, two of them in
//     inline forms, and came out of every writer path holding them there.
func HoistDirectStreams(ctx context.Context, alloc *ObjAllocator, trailer ast.PdfDict) (int, error) {
	h := hoister{alloc: alloc}

	pending := alloc.Pending()
	objNums := make([]int, 0, len(pending))
	for num, obj := range pending {
		if holdsDirectStream(obj, true) {
			objNums = append(objNums, num)
		}
	}
	sort.Ints(objNums)

	for _, num := range objNums {
		if err := ctx.Err(); err != nil {
			return h.moved, fmt.Errorf("hoisting direct streams of object %d: %w", num, err)
		}
		obj, changed := h.object(pending[num])
		if changed {
			alloc.AllocAt(num, obj)
		}
	}

	if holdsDirectStream(trailer, false) {
		if err := ctx.Err(); err != nil {
			return h.moved, fmt.Errorf("hoisting direct streams of the trailer: %w", err)
		}
		for _, key := range sortedKeys(trailer) {
			if v, changed := h.value(trailer[key]); changed {
				trailer[key] = v
			}
		}
	}
	return h.moved, nil
}

// hoister moves direct streams into objects of their own for
// [HoistDirectStreams].
type hoister struct {
	alloc *ObjAllocator
	moved int
}

// object returns obj, the value of an indirect object, with every stream
// nested in it replaced by a reference, and whether anything changed. obj
// itself may be a stream.
func (h *hoister) object(obj ast.PdfObject) (ast.PdfObject, bool) {
	switch v := obj.(type) {
	case ast.PdfStream:
		dict, changed := h.dict(v.Dict)
		if !changed {
			return v, false
		}
		// The copy shares the loader, so the data stays where it is.
		v.Dict = dict
		return v, true
	case ast.PdfDict:
		return h.dict(v)
	case ast.PdfArray:
		return h.array(v)
	default:
		return obj, false
	}
}

// value returns obj, a value nested in another object, with a stream moved
// into an object of its own and replaced by a reference to it.
func (h *hoister) value(obj ast.PdfObject) (ast.PdfObject, bool) {
	stream, ok := obj.(ast.PdfStream)
	if !ok {
		return h.object(obj)
	}
	clean, _ := h.object(stream)
	h.moved++
	return h.alloc.Alloc(clean), true
}

func (h *hoister) dict(d ast.PdfDict) (ast.PdfDict, bool) {
	if !holdsDirectStream(d, true) {
		return d, false
	}
	out := maps.Clone(d)
	for _, key := range sortedKeys(d) {
		if v, changed := h.value(d[key]); changed {
			out[key] = v
		}
	}
	return out, true
}

func (h *hoister) array(arr ast.PdfArray) (ast.PdfArray, bool) {
	if !holdsDirectStream(arr, true) {
		return arr, false
	}
	out := make(ast.PdfArray, len(arr))
	for i, elem := range arr {
		out[i], _ = h.value(elem)
	}
	return out, true
}

// holdsDirectStream reports whether a stream lies nested anywhere inside obj.
// top reports whether obj is the value of an indirect object, where a stream
// is not nested.
func holdsDirectStream(obj ast.PdfObject, top bool) bool {
	switch v := obj.(type) {
	case ast.PdfStream:
		return !top || holdsDirectStream(v.Dict, true)
	case ast.PdfDict:
		for _, e := range v {
			if holdsDirectStream(e, false) {
				return true
			}
		}
	case ast.PdfArray:
		for _, e := range v {
			if holdsDirectStream(e, false) {
				return true
			}
		}
	}
	return false
}

func sortedKeys(d ast.PdfDict) []ast.PdfName {
	keys := make([]ast.PdfName, 0, len(d))
	for k := range d {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}
