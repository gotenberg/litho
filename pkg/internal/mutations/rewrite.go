package mutations

import (
	"context"
	"fmt"
	"io"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/crypto"
)

// CompactOptions selects which output-compaction passes a full rewrite runs.
//
// Grouped rather than passed as separate booleans: [Split] and [Merge] take
// these positionally, and three or more adjacent bools at a call site are
// transposable without the compiler noticing. Later passes (image
// recompression, font subsetting, linearization) extend this struct instead of
// lengthening those signatures again.
//
// Every pass here is refused for an encrypted document, because encryption
// runs over the object set before serialization; see [writer.PackObjectStreams]
// and [writer.DedupStreams] for what each would otherwise corrupt.
type CompactOptions struct {
	// PackObjectStreams packs eligible objects into /Type /ObjStm streams.
	// The write engine forces a cross-reference stream when it packs, since
	// a traditional table cannot name a compressed object.
	PackObjectStreams bool
	// DedupStreams merges byte-identical stream objects, repointing every
	// reference at one survivor.
	DedupStreams bool
}

// FullRewriteConfig holds the parameters for a full document rewrite.
type FullRewriteConfig struct {
	Doc             *document.Document
	EncryptCfg      *crypto.EncryptConfig
	Decrypt         bool
	Version         string
	RequiredVersion string
	XRefStream      bool
	// Compact selects the output-compaction passes the rewrite runs.
	Compact CompactOptions
	// Overrides pre-seeds the copier so edits made against the source
	// document's object numbers (remediation patches and pre-applied
	// user mutations) are used in place of the original objects during
	// the copy pass. Keys are source object numbers; values are the
	// replacement payloads. Mutations must arrive here, never as
	// callbacks against the rewrite's own allocator: that allocator is
	// in destination numbering, so a source-numbered AllocAt would stomp
	// an unrelated copied object and silently drop the edit.
	Overrides map[int]ast.PdfObject
}

// FullRewrite performs a complete rewrite of the document, applying the
// override set and optionally encrypting or decrypting the output.
func FullRewrite(ctx context.Context, dst io.Writer, cfg FullRewriteConfig) error {
	doc := cfg.Doc
	alloc := writer.NewObjAllocator(0)
	copier := writer.NewCopier(doc, alloc)
	if len(cfg.Overrides) > 0 {
		copier.WithOverrides(cfg.Overrides)
	}

	// Copy the catalog (root of the object graph). Prefer the trailer /Root,
	// but only when it resolves to a real catalog. A file whose xref wrongly
	// marks the catalog object free, or points /Root at a non-catalog, resolves
	// the ref to null; copying that ref yields a rootless output with no pages
	// (pdfjs issue18986/issue9418). In that case copy the recovered catalog
	// dict the reader synthesises, so the rewrite reproduces what the reader
	// reads, the same way it recovers a trailer that lost /Root entirely.
	catalogRef, hasRef := CatalogRef(ctx, doc)
	var newCatalogRef ast.PdfIndirectRef
	if hasRef && catalogRefResolves(ctx, doc, catalogRef) {
		copied, err := copier.CopyRef(ctx, catalogRef)
		if err != nil {
			return fmt.Errorf("copying catalog: %w", err)
		}
		newCatalogRef = copied
	} else {
		catalog, err := document.ResolveCatalog(ctx, doc)
		if err != nil {
			return fmt.Errorf("resolving catalog for rewrite: %w", err)
		}
		copied, err := copier.CopyObject(ctx, catalog.Dict)
		if err != nil {
			return fmt.Errorf("copying recovered catalog: %w", err)
		}
		newCatalogRef = alloc.Alloc(copied)
	}

	if err := ensurePageTree(alloc, newCatalogRef, pageTreeWasLost(ctx, doc)); err != nil {
		return err
	}

	// Build trailer.
	trailer := ast.PdfDict{
		"Root": newCatalogRef,
	}

	// Copy /Info if present.
	if infoObj, ok := doc.Index.Trailer["Info"]; ok {
		if infoRef, ok := infoObj.(ast.PdfIndirectRef); ok {
			newInfoRef, err := copier.CopyRef(ctx, infoRef)
			if err == nil {
				trailer["Info"] = newInfoRef
			}
		}
	}

	// Copy /ID if present.
	if idArr, ok := doc.Index.Trailer["ID"].(ast.PdfArray); ok {
		copiedID, err := copier.CopyObject(ctx, idArr)
		if err == nil {
			trailer["ID"] = copiedID
		}
	}

	// Shared with [Split] and [Merge] so the three write paths cannot drift:
	// see [applyEncryption] for the ordering the sweep depends on.
	if cfg.EncryptCfg != nil {
		if err := applyEncryption(ctx, alloc, trailer, cfg.EncryptCfg); err != nil {
			return err
		}
	}

	// Decryption: the reader already decrypted all objects during resolution.
	// The copier copies the decrypted data. We just omit /Encrypt from the
	// trailer (which we already did by not copying it).

	version := cfg.Version
	if version == "" && cfg.RequiredVersion != "" {
		version = cfg.RequiredVersion
	}
	if version == "" {
		version = doc.Version
	}

	return writer.WriteRewrite(ctx, dst, writer.RewriteConfig{
		Version:           version,
		Allocator:         alloc,
		Trailer:           trailer,
		UseXRefStream:     cfg.XRefStream,
		PackObjectStreams: cfg.Compact.PackObjectStreams,
		DedupStreams:      cfg.Compact.DedupStreams,
	})
}

// catalogRefResolves reports whether ref resolves to a catalog dictionary with
// a resolvable /Pages tree. A false result means the xref misrepresents the
// catalog (a wrongly-freed entry, or /Root pointing at a non-catalog), so the
// rewrite must recover the catalog rather than copy a null reference.
func catalogRefResolves(ctx context.Context, doc *document.Document, ref ast.PdfIndirectRef) bool {
	dict, err := document.ResolveDict(ctx, doc, ref)
	if err != nil || dict == nil {
		return false
	}
	pages, err := document.ResolveDict(ctx, doc, dict["Pages"])
	return err == nil && pages != nil
}

// ensurePageTree gives the output catalog an empty page tree when the copied
// one has no usable /Pages.
//
// ISO 32000-1, 7.7.3.1 makes /Pages the page tree root DICTIONARY, so a catalog
// pointing at a null object is not a document: pikepdf refuses the file with
// "unable to find page tree". A damaged source produces exactly that, and
// faithfully so, because ISO 32000-1, 7.3.9 makes a reference to a missing
// object equivalent to null. pdfjs poppler-937-0-fuzzed.pdf, whose /Kids array
// opens with a NUL byte instead of "[", rewrote 1666 bytes into 344 whose whole
// body was "1 0 obj null" under a catalog naming it.
//
// An empty tree rather than a refusal, because the reader already reports the
// document as zero pages: the rewrite then says the same thing in a form every
// reader can open, instead of one none can. [catalogRefResolves] applies the
// same reasoning to a /Root that resolves to null.
//
// That reasoning holds only while there are no pages to lose. When pageTreeLost
// says the file does define the page tree and litho merely failed to load it,
// an empty tree would answer a question the source never asked, so report
// [ErrPageTreeNotReproduced] instead. See [pageTreeWasLost].
func ensurePageTree(alloc *writer.ObjAllocator, catalogRef ast.PdfIndirectRef, pageTreeLost bool) error {
	catalog, ok := alloc.Pending()[catalogRef.ObjectNum].(ast.PdfDict)
	if !ok || pageTreeResolves(alloc, catalog["Pages"]) {
		return nil
	}

	// A source that named a page tree has pages to lose. Substituting an empty
	// one would hand back a valid but emptied document and report success.
	if pageTreeLost {
		return ErrPageTreeNotReproduced
	}

	updated := make(ast.PdfDict, len(catalog)+1)
	maps.Copy(updated, catalog)
	updated["Pages"] = alloc.Alloc(ast.PdfDict{
		"Type":  ast.PdfName("Pages"),
		"Kids":  ast.PdfArray{},
		"Count": ast.PdfInt(0),
	})
	// The catalog holds a positive object number, the one case AllocAt
	// refuses, so the replacement always lands.
	alloc.AllocAt(catalogRef.ObjectNum, updated)
	return nil
}

// pageTreeWasLost reports whether the source catalog names a page tree the file
// actually defines, so failing to reproduce it discards real pages.
//
// A catalog naming an object the file never defines is the dangling case the
// empty page tree exists for: ISO 32000-1, 7.3.9 makes that reference null, the
// document has no pages to begin with, and pikepdf rejects it outright. An
// object the cross-reference table does list is different. litho merely failed
// to load it, as on an encrypted pdfjs file whose page tree sits in an object
// stream that would not decode, and substituting an empty tree there hands back
// a document stripped of every page.
func pageTreeWasLost(ctx context.Context, doc *document.Document) bool {
	resolved, err := document.ResolveCatalog(ctx, doc)
	if err != nil || resolved == nil || resolved.Dict == nil {
		return false
	}
	ref, ok := resolved.Dict["Pages"].(ast.PdfIndirectRef)
	if !ok {
		// A direct page tree travels with the catalog, so it cannot go
		// missing on its own.
		return false
	}
	return doc.DefinesObject(ref.ObjectNum)
}

// pageTreeResolves reports whether obj names a dictionary the output will
// actually contain.
func pageTreeResolves(alloc *writer.ObjAllocator, obj ast.PdfObject) bool {
	switch v := obj.(type) {
	case ast.PdfDict:
		return true
	case ast.PdfIndirectRef:
		_, ok := alloc.Pending()[v.ObjectNum].(ast.PdfDict)
		return ok
	}
	return false
}
