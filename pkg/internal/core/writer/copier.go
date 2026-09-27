package writer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// Copier deep-copies objects from a source document, renumbering all indirect
// references. Streams are copied as raw bytes without decoding/re-encoding.
type Copier struct {
	src       *document.Document
	allocator *ObjAllocator
	mapping   map[int]int // src objNum -> dst objNum
	overrides map[int]ast.PdfObject
	// stoodIn holds the source objects [Copier.MapTo] stood an output object
	// in for, which the copier never copied.
	stoodIn map[int]bool
}

// NewCopier creates a Copier that copies objects from src into the allocator's
// namespace.
func NewCopier(src *document.Document, allocator *ObjAllocator) *Copier {
	return &Copier{
		src:       src,
		allocator: allocator,
		mapping:   make(map[int]int),
		stoodIn:   make(map[int]bool),
	}
}

// WithOverrides registers pre-patched versions of source objects. When
// the copier is asked to copy an indirect reference whose object number
// appears in overrides, it copies that object instead of the one
// persisted in the source document. Returns the copier for chaining.
func (c *Copier) WithOverrides(overrides map[int]ast.PdfObject) *Copier {
	c.overrides = overrides
	return c
}

// Override returns the object the copier copies in place of source object
// objNum, and false when it copies the source's own (see
// [Copier.WithOverrides]).
func (c *Copier) Override(objNum int) (ast.PdfObject, bool) {
	obj, ok := c.overrides[objNum]
	return obj, ok
}

// Mapping exposes the src-to-dst object number map assembled during the
// copy pass. Callers use it to translate references allocated in the
// source namespace into the destination namespace after copy completes.
// It holds the entries [Copier.MapTo] recorded too.
func (c *Copier) Mapping() map[int]int {
	return c.mapping
}

// MapTo records dst as the copy of source object srcObjNum: every reference to
// it the copier meets is rewritten to dst, and the object itself is never
// copied. It stands one object of the output in for one of the source, as a
// page insertion stands the target's structure element in for the root of the
// source's structure tree. A mapping the copier already holds is replaced, so
// call it before the copy that should honour it.
func (c *Copier) MapTo(srcObjNum int, dst ast.PdfIndirectRef) {
	c.mapping[srcObjNum] = dst.ObjectNum
	c.stoodIn[srcObjNum] = true
}

// StandsIn reports whether [Copier.MapTo] stood an output object in for source
// object srcObjNum, so the object [Copier.Mapping] gives for it is not one the
// copier wrote.
func (c *Copier) StandsIn(srcObjNum int) bool {
	return c.stoodIn[srcObjNum]
}

// CopyRef resolves an indirect reference from the source document, deep-copies
// the object graph, and returns the new reference in the destination namespace.
func (c *Copier) CopyRef(ctx context.Context, ref ast.PdfIndirectRef) (ast.PdfIndirectRef, error) {
	if dstNum, ok := c.mapping[ref.ObjectNum]; ok {
		return ast.PdfIndirectRef{ObjectNum: dstNum, GenNum: 0}, nil
	}

	// Reserve a destination object number first (before resolving) to handle
	// circular references.
	dstRef := c.allocator.Alloc(ast.PdfNull{}) // placeholder
	c.mapping[ref.ObjectNum] = dstRef.ObjectNum

	// Prefer a caller-supplied override of the source object so
	// remediation-stage patches flow through the copier intact.
	var srcObj ast.PdfObject
	if override, ok := c.overrides[ref.ObjectNum]; ok {
		srcObj = override
	} else {
		var err error
		srcObj, err = c.src.Resolve(ctx, ref)
		if err != nil {
			// Cancellation and deadline errors must abort the whole rewrite;
			// never mask them as a recovered object.
			if ctx.Err() != nil {
				return dstRef, err
			}
			// A source object that is missing (ISO 32000-1 §7.3.10: a reference
			// to an absent object is the null object) or unparseable (a damaged
			// or garbage object, e.g. `7 0 obj 10P1 endobj`) is copied as null
			// rather than aborting the whole rewrite, so the rest of the document
			// still round-trips. The placeholder allocated above is already null.
			if errors.Is(err, document.ErrObjectNotFound) {
				c.src.Logger.Debug("copier: dangling reference copied as null",
					slog.Int("obj", ref.ObjectNum))
			} else {
				c.src.Logger.Debug("copier: unparseable source object copied as null",
					slog.Int("obj", ref.ObjectNum), slog.Any("err", err))
			}
			return dstRef, nil
		}
	}

	copied, err := c.CopyObject(ctx, srcObj)
	if err != nil {
		return dstRef, err
	}

	// Replace the placeholder.
	c.allocator.AllocAt(dstRef.ObjectNum, copied)
	return dstRef, nil
}

// CopyObject deep-copies a PDF object, renumbering all indirect references.
func (c *Copier) CopyObject(ctx context.Context, obj ast.PdfObject) (ast.PdfObject, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	switch v := obj.(type) {
	case nil:
		return ast.PdfNull{}, nil
	case ast.PdfNull, ast.PdfBool, ast.PdfInt, ast.PdfReal, ast.PdfName, ast.PdfString:
		return v, nil // immutable scalars
	case ast.PdfIndirectRef:
		ref, err := c.CopyRef(ctx, v)
		if err != nil {
			return nil, err
		}
		return ref, nil
	case ast.PdfArray:
		return c.copyArray(ctx, v)
	case ast.PdfDict:
		return c.copyDict(ctx, v)
	case ast.PdfStream:
		return c.copyStream(ctx, v)
	default:
		return nil, fmt.Errorf("unsupported object type %T", obj)
	}
}

func (c *Copier) copyArray(ctx context.Context, arr ast.PdfArray) (ast.PdfArray, error) {
	result := make(ast.PdfArray, len(arr))
	for i, elem := range arr {
		copied, err := c.CopyObject(ctx, elem)
		if err != nil {
			return nil, err
		}
		result[i] = copied
	}
	return result, nil
}

func (c *Copier) copyDict(ctx context.Context, d ast.PdfDict) (ast.PdfDict, error) {
	result := make(ast.PdfDict, len(d))
	// Walk keys in a stable order. copyDict recurses into child refs that
	// allocate new dst object numbers in traversal order; without a fixed
	// order the dst numbering differs run-to-run, which makes byte-level
	// diffing of full-rewrite output impossible and hides real regressions.
	keys := make([]string, 0, len(d))
	for k := range d {
		keys = append(keys, string(k))
	}
	sort.Strings(keys)
	for _, ks := range keys {
		k := ast.PdfName(ks)
		copied, err := c.CopyObject(ctx, d[k])
		if err != nil {
			return nil, err
		}
		result[k] = copied
	}
	return result, nil
}

func (c *Copier) copyStream(ctx context.Context, s ast.PdfStream) (ast.PdfStream, error) {
	// Copy the dict (renumbering refs inside it).
	newDict, err := c.copyDict(ctx, s.Dict)
	if err != nil {
		return ast.PdfStream{}, err
	}

	// Copy raw bytes without decoding. The stream's Bytes method reads
	// lazily from the source document.
	return ast.NewLazyStream(newDict, s.Bytes), nil
}
