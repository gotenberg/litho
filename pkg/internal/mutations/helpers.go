package mutations

import (
	"context"
	"crypto/rand"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// CatalogRef returns the object the reader, the validator and the writer all
// treat as the catalog: [document.Document.CatalogRef], which honours the
// trailer /Root when it resolves to a catalog and recovers by scanning the
// object table when it does not.
//
// Per ISO 32000-1 §7.5.5 Table 15 /Root shall be the catalog, so a file whose
// damaged incremental update points it elsewhere is malformed, and the reader
// already recovers from it. Taking the raw /Root at face value made every
// catalog-editing mutation address whatever object it named: on pdfjs
// issue9418.pdf, whose final trailer points /Root at the /Info dictionary,
// `litho attach` reported success and wrote /Names into /Info, leaving the real
// catalog untouched, so `litho attachments` on that same output reported no
// attachments. The mutation was lost, and the /Info dictionary corrupted.
//
// The raw trailer ref remains the fallback: when recovery finds no catalog at
// all, a reference the file at least names beats no reference. ok is false only
// when neither yields one.
func CatalogRef(ctx context.Context, doc *document.Document) (ast.PdfIndirectRef, bool) {
	if ref, ok := doc.CatalogRef(ctx); ok {
		return ref, true
	}
	if ref, ok := doc.Index.Trailer["Root"].(ast.PdfIndirectRef); ok {
		return ref, true
	}
	return ast.PdfIndirectRef{}, false
}

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
