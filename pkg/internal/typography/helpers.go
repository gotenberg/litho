package typography

import (
	"unicode/utf16"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// getFloat is a helper to safely extract a float64 from a PdfObject.
func getFloat(obj ast.PdfObject) float64 {
	switch v := obj.(type) {
	case ast.PdfReal:
		return float64(v)
	case ast.PdfInt:
		return float64(v)
	}
	return 0
}

// resolveObject is a helper to resolve indirect references if needed.
func resolveObject(doc *document.Document, obj ast.PdfObject) (ast.PdfObject, error) {
	if ref, ok := obj.(ast.PdfIndirectRef); ok {
		return doc.Resolve(ref)
	}
	return obj, nil
}

// decodePdfString converts a PDF string to a Go string, handling UTF-16BE if present.
func decodePdfString(s ast.PdfString) string {
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
