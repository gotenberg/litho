package mutations

import (
	"context"
	"crypto/rand"
	"fmt"
	"math"
	"sort"

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

// CatalogDict returns the catalog dictionary a mutation should edit, given the
// reference [CatalogRef] chose.
//
// Recovering the reference is not enough when the cross-reference table
// misdirects it. On pdfjs issue18986.pdf the catalog's entry addresses the
// offset of an unrelated empty dictionary, so resolving the reference yields
// "<<>>" with no error. `litho set-bookmarks` copied those zero entries, wrote
// "<< /Outlines 9 0 R >>" over the catalog, and reported success: the output
// lost /Type, /Pages and every other key, leaving a document with no page tree
// that pikepdf rejects and litho's own `bookmarks` reads as empty.
//
// The catalog the reader recovers is the fallback, so the update keeps the
// entries the document actually has. An empty result is treated as a failure
// rather than an empty catalog: a catalog with no entries is not a catalog
// (ISO 32000-1, 7.7.2 requires /Type and /Pages), and writing one back destroys
// the document.
func CatalogDict(ctx context.Context, doc *document.Document, ref ast.PdfIndirectRef) (ast.PdfDict, error) {
	dict, err := document.ResolveDict(ctx, doc, ref)
	if err == nil && len(dict) > 0 {
		return dict, nil
	}
	if resolved, rerr := document.ResolveCatalog(ctx, doc); rerr == nil && resolved != nil && len(resolved.Dict) > 0 {
		return resolved.Dict, nil
	}
	if err != nil {
		return nil, fmt.Errorf("resolving catalog: %w", err)
	}
	return nil, ErrCatalogUnreadable
}

// LiveCatalogDict returns the catalog a mutation should edit, preferring the
// version an earlier mutation left pending in the allocator.
//
// Mutations queued on one writer share a single allocator and are applied in
// order with no flush between them, so a mutation that rewrites the whole
// catalog must start from the pending copy or it discards what the mutations
// before it wrote there. Attaching a file and then flattening the form lost the
// attachment's /Names and /AF exactly that way, and nothing put them back:
// unlike the remediation registry, mutations have no fix-to-fixpoint loop to
// reapply a discarded write, so the loss reaches the output file.
//
// [CatalogDict] stays the fallback, so the reference recovery and the
// empty-catalog rule documented there still apply. A pending value that is not
// a non-empty dictionary is ignored for that same reason.
func LiveCatalogDict(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, ref ast.PdfIndirectRef) (ast.PdfDict, error) {
	if pending, ok := alloc.Pending()[ref.ObjectNum]; ok {
		if d, isDict := pending.(ast.PdfDict); isDict && len(d) > 0 {
			return d, nil
		}
	}
	return CatalogDict(ctx, doc, ref)
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

// finiteValues reports the first option that is not a finite number.
//
// A PDF number is an optional sign, digits and an optional decimal point (ISO
// 32000-1, 7.3.3); there is no spelling for NaN or infinity. Go's flag package
// parses "NaN" and "Inf" into a float64 perfectly happily, and %f then wrote
// them into the content stream verbatim: `litho stamp-text -x NaN` produced a
// page poppler rejects with "Syntax Error: Unknown operator 'NaN'" followed by
// "Too few (1) args to 'Td'", while litho exited 0. Callers are told instead.
func finiteValues(named map[string]float64) error {
	names := make([]string, 0, len(named))
	for name := range named {
		names = append(names, name)
	}
	// Sorted so the same bad input always names the same option first.
	sort.Strings(names)
	for _, name := range names {
		if v := named[name]; math.IsNaN(v) || math.IsInf(v, 0) {
			return fmt.Errorf("%s must be a finite number, got %v", name, v)
		}
	}
	return nil
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
