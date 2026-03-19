package document

import (
	"context"
	"fmt"
	"unicode/utf16"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
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
