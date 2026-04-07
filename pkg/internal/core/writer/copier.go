package writer

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// Copier deep-copies objects from a source document, renumbering all indirect
// references. Streams are copied as raw bytes without decoding/re-encoding.
type Copier struct {
	src       *document.Document
	allocator *ObjAllocator
	mapping   map[int]int // src objNum -> dst objNum
}

// NewCopier creates a Copier that copies objects from src into the allocator's
// namespace.
func NewCopier(src *document.Document, allocator *ObjAllocator) *Copier {
	return &Copier{
		src:       src,
		allocator: allocator,
		mapping:   make(map[int]int),
	}
}

// CopyRef resolves an indirect reference from the source document, deep-copies
// the object graph, and returns the new reference in the destination namespace.
func (c *Copier) CopyRef(ctx context.Context, ref ast.PdfIndirectRef) (ast.PdfIndirectRef, error) {
	// Already mapped?
	if dstNum, ok := c.mapping[ref.ObjectNum]; ok {
		return ast.PdfIndirectRef{ObjectNum: dstNum, GenNum: 0}, nil
	}

	// Reserve a destination object number first (before resolving) to handle
	// circular references.
	dstRef := c.allocator.Alloc(ast.PdfNull{}) // placeholder
	c.mapping[ref.ObjectNum] = dstRef.ObjectNum

	// Resolve the source object.
	srcObj, err := c.src.Resolve(ctx, ref)
	if err != nil {
		return dstRef, fmt.Errorf("resolving source object %d: %w", ref.ObjectNum, err)
	}

	// Deep-copy.
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
	for k, v := range d {
		copied, err := c.CopyObject(ctx, v)
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

// Mapping returns the source-to-destination object number mapping.
func (c *Copier) Mapping() map[int]int {
	return c.mapping
}
