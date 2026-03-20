package document

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"unicode/utf16"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/ioat"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
	"github.com/gotenberg/litho/pkg/internal/core/parser"
	"github.com/gotenberg/litho/pkg/internal/filter"
)

// GetFloat safely extracts a float64 from a PdfObject.
// Returns 0 if the object is neither a PdfInt nor a PdfReal.
func GetFloat(obj ast.PdfObject) float64 {
	switch v := obj.(type) {
	case ast.PdfReal:
		return float64(v)
	case ast.PdfInt:
		return float64(v)
	}
	return 0
}

// ResolveObject resolves an indirect reference through the Document's Resolve
// method, which provides circular-reference protection and object caching.
// If obj is not an indirect reference, it is returned as-is.
//
// This is the single authorised way to chase indirect refs across all internal
// packages. Callers must never re-implement this logic locally.
func ResolveObject(ctx context.Context, doc *Document, obj ast.PdfObject) (ast.PdfObject, error) {
	if ref, ok := obj.(ast.PdfIndirectRef); ok {
		return doc.Resolve(ctx, ref)
	}
	return obj, nil
}

// ResolveDict resolves obj and returns it as a PdfDict. Returns nil (no error)
// when the resolved object is not a dictionary — callers that require a dict
// should check for nil.
func ResolveDict(ctx context.Context, doc *Document, obj ast.PdfObject) (ast.PdfDict, error) {
	resolved, err := ResolveObject(ctx, doc, obj)
	if err != nil {
		return nil, err
	}
	dict, _ := resolved.(ast.PdfDict)
	return dict, nil
}

// ResolveDictRequired resolves obj and returns it as a PdfDict, returning an
// error if the resolved object is not a dictionary.
func ResolveDictRequired(ctx context.Context, doc *Document, obj ast.PdfObject, context string) (ast.PdfDict, error) {
	resolved, err := ResolveObject(ctx, doc, obj)
	if err != nil {
		return nil, err
	}
	dict, ok := resolved.(ast.PdfDict)
	if !ok {
		return nil, fmt.Errorf("%s: expected dictionary, got %T", context, resolved)
	}
	return dict, nil
}

// ResolveArray resolves obj and returns it as a PdfArray. Returns nil (no error)
// when the resolved object is not an array.
func ResolveArray(ctx context.Context, doc *Document, obj ast.PdfObject) (ast.PdfArray, error) {
	resolved, err := ResolveObject(ctx, doc, obj)
	if err != nil {
		return nil, err
	}
	arr, _ := resolved.(ast.PdfArray)
	return arr, nil
}

// ResolveName resolves obj and returns it as a PdfName. Returns "" (no error)
// when the resolved object is not a name.
func ResolveName(ctx context.Context, doc *Document, obj ast.PdfObject) (ast.PdfName, error) {
	resolved, err := ResolveObject(ctx, doc, obj)
	if err != nil {
		return "", err
	}
	name, _ := resolved.(ast.PdfName)
	return name, nil
}

// ResolveStream resolves obj and returns it as a PdfStream. Returns the zero
// value (no error) when the resolved object is not a stream.
func ResolveStream(ctx context.Context, doc *Document, obj ast.PdfObject) (ast.PdfStream, error) {
	resolved, err := ResolveObject(ctx, doc, obj)
	if err != nil {
		return ast.PdfStream{}, err
	}
	stream, ok := resolved.(ast.PdfStream)
	if !ok {
		return ast.PdfStream{}, nil
	}
	return stream, nil
}

// DecodePdfString converts a raw PDF string to a Go string, handling the
// UTF-16BE BOM (0xFE 0xFF) encoding used by many PDF producers.
//
// This function operates on already-decrypted string data. Decryption is
// handled transparently by the parser's crypto pipeline during object
// resolution, so by the time a PdfString reaches this function the cleartext
// bytes are guaranteed.
func DecodePdfString(s ast.PdfString) string {
	b := []byte(s)
	if len(b) >= 2 && b[0] == 0xFE && b[1] == 0xFF {
		u16s := make([]uint16, (len(b)-2)/2)
		for i := range u16s {
			u16s[i] = uint16(b[2+i*2])<<8 | uint16(b[2+i*2+1])
		}
		return string(utf16.Decode(u16s))
	}
	return string(b)
}

// resolveAny resolves obj if it is an indirect reference, otherwise returns it directly.
// This is safe to call on any PdfObject (unlike Resolve which requires PdfIndirectRef).
func (d *Document) resolveAny(ctx context.Context, obj ast.PdfObject) ast.PdfObject {
	if ref, ok := obj.(ast.PdfIndirectRef); ok {
		res, err := d.Resolve(ctx, ref)
		if err != nil {
			return nil
		}
		return res
	}
	return obj
}

// extractFromObjectStream extracts an object from an Object Stream.
// visited is the per-call-chain set for circular-reference detection.
func (d *Document) extractFromObjectStream(ctx context.Context, objStmNum int, targetIndex int, visited map[int]bool) (ast.PdfObject, error) {
	// 1. Resolve the Object Stream itself
	objStmRef := ast.PdfIndirectRef{ObjectNum: objStmNum, GenNum: 0}
	obj, err := d.resolveWith(ctx, objStmRef, visited)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve object stream %d: %v", objStmNum, err)
	}

	stream, ok := obj.(ast.PdfStream)
	if !ok {
		return nil, fmt.Errorf("object %d is not a stream", objStmNum)
	}

	// Decompress the stream using the filter package
	decodedData, err := filter.DecodeStream(stream)
	if err != nil {
		return nil, fmt.Errorf("failed to decode object stream %d: %v", objStmNum, err)
	}

	// 2. Extract /N and /First
	nObj, ok := stream.Dict["N"]
	if !ok {
		return nil, fmt.Errorf("object stream missing /N")
	}
	n := int(nObj.(ast.PdfInt))

	firstObj, ok := stream.Dict["First"]
	if !ok {
		return nil, fmt.Errorf("object stream missing /First")
	}
	first := int(firstObj.(ast.PdfInt))

	// 3. Loop N times to read the header integer pairs
	reader := bytes.NewReader(decodedData)
	lex := lexer.NewLexer(ioat.NewReader(reader, int64(len(decodedData)), 0))

	targetOffset := -1
	for i := range n {
		// Read objNum
		_, err := lex.NextToken()
		if err != nil {
			return nil, err
		}
		// Read offset
		tokOffset, err := lex.NextToken()
		if err != nil {
			return nil, err
		}

		if i == targetIndex {
			offset, err := strconv.Atoi(tokOffset.Value)
			if err != nil {
				return nil, fmt.Errorf("invalid offset in object stream %d: %w", objStmNum, err)
			}
			targetOffset = offset
			break // We found our target offset, can stop reading pairs
		}
	}

	if targetOffset == -1 {
		return nil, fmt.Errorf("target index %d out of bounds in object stream %d", targetIndex, objStmNum)
	}

	// 4. Calculate absolute position
	absoluteOffset := int64(first + targetOffset)

	// 5. Seek reader to absoluteOffset and parse
	lex = lexer.NewLexer(ioat.NewReader(reader, int64(len(decodedData)), absoluteOffset))
	p := parser.NewParser(lex)

	return p.ParseObject()
}

// Resolve looks up an indirect reference in the document's cross-reference index
// and parses the object from the underlying stream if it hasn't been cached yet.
//
// Resolve is safe for concurrent use by multiple goroutines.
func (d *Document) Resolve(ctx context.Context, ref ast.PdfIndirectRef) (ast.PdfObject, error) {
	return d.resolveWith(ctx, ref, nil)
}

// resolveWith is the internal implementation of Resolve. The visited map tracks
// object numbers in the current call chain to detect circular references. Each
// goroutine gets its own visited set, so there is no cross-goroutine interference.
func (d *Document) resolveWith(ctx context.Context, ref ast.PdfIndirectRef, visited map[int]bool) (ast.PdfObject, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Fast path: check cache under read lock.
	d.mu.RLock()
	if obj, ok := d.objectCache[ref.ObjectNum]; ok {
		d.mu.RUnlock()
		d.Logger.Debug("resolve object cache hit",
			slog.Int("obj", ref.ObjectNum),
			slog.Int("gen", ref.GenNum),
			slog.String("type", fmt.Sprintf("%T", obj)),
		)
		return obj, nil
	}
	d.mu.RUnlock()

	// Check for circular reference in this call chain.
	if visited == nil {
		visited = make(map[int]bool)
	}
	if visited[ref.ObjectNum] {
		return nil, fmt.Errorf("%w: object %d", ErrCircularRef, ref.ObjectNum)
	}
	visited[ref.ObjectNum] = true
	defer delete(visited, ref.ObjectNum)

	// Look up the object in the xref table (Index is immutable after construction).
	entry, ok := d.Index.Entries[ref.ObjectNum]
	if !ok {
		return nil, fmt.Errorf("%w: object %d", ErrObjectNotFound, ref.ObjectNum)
	}

	if entry.Free {
		return ast.PdfNull{}, nil
	}

	// Parse the object without holding any lock. io.ReaderAt is concurrent-safe.
	var obj ast.PdfObject
	var err error

	switch entry.Type {
	case 1:
		// Type 1: Normal object
		lex := lexer.NewLexer(ioat.NewReader(d.r, d.size, entry.Offset))
		p := parser.NewParser(lex)
		if d.Crypto != nil {
			p.WithDecryption(d.Crypto.GetDecryptFunc(ref.ObjectNum, ref.GenNum))
		}

		obj, err = p.ParseObject()
		if err != nil {
			return nil, fmt.Errorf("failed to parse object %d at offset %d: %v", ref.ObjectNum, entry.Offset, err)
		}

		d.Logger.Debug("resolve object parsed",
			slog.Int("obj", ref.ObjectNum),
			slog.Int("gen", ref.GenNum),
			slog.Int64("offset", entry.Offset),
			slog.String("type", fmt.Sprintf("%T", obj)),
		)

	case 2:
		// Type 2: Compressed object in an Object Stream (PDF 1.5+)
		obj, err = d.extractFromObjectStream(ctx, entry.ObjStmNum, entry.ObjStmIndex, visited)
		if err != nil {
			return nil, fmt.Errorf("failed to extract object %d from stream %d: %v", ref.ObjectNum, entry.ObjStmNum, err)
		}

		d.Logger.Debug("resolve object from object stream",
			slog.Int("obj", ref.ObjectNum),
			slog.Int("stream_obj", entry.ObjStmNum),
			slog.Int("index", entry.ObjStmIndex),
			slog.String("type", fmt.Sprintf("%T", obj)),
		)

	default:
		return nil, fmt.Errorf("unknown xref entry type: %d", entry.Type)
	}

	// Cache the result. If another goroutine cached the same object while we
	// were parsing, use its result for consistency.
	d.mu.Lock()
	if existing, ok := d.objectCache[ref.ObjectNum]; ok {
		d.mu.Unlock()
		return existing, nil
	}
	d.objectCache[ref.ObjectNum] = obj
	d.mu.Unlock()

	return obj, nil
}
