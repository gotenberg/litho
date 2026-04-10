package mutations

import (
	"context"
	"crypto/rand"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// ResolveFromAllocOrDoc checks the allocator for a pending version of the
// object, falling back to the document. Returns nil if not found.
func ResolveFromAllocOrDoc(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, ref ast.PdfIndirectRef) ast.PdfDict {
	if obj, ok := alloc.Pending()[ref.ObjectNum]; ok {
		if d, ok := obj.(ast.PdfDict); ok {
			return d
		}
	}
	d, err := document.ResolveDict(ctx, doc, ref)
	if err != nil {
		return nil
	}
	return d
}

// FormatFloat formats a float with up to 4 decimal places, trimming trailing zeros.
func FormatFloat(f float64) string {
	s := fmt.Sprintf("%.4f", f)
	// Trim trailing zeros.
	for len(s) > 1 && s[len(s)-1] == '0' {
		s = s[:len(s)-1]
	}
	if s[len(s)-1] == '.' {
		s = s[:len(s)-1]
	}
	return s
}

// GenerateDocumentID creates a random 16-byte document ID.
func GenerateDocumentID() []byte {
	id := make([]byte, 16)
	_, _ = rand.Read(id) // crypto/rand never fails on supported platforms
	return id
}
