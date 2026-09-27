package mutations

import (
	"context"
	"fmt"
	"io"
	"log/slog"
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
	// PageSources are the documents the overrides hold pages copied from (see
	// [InsertPages]). The header declares at least the version each of them
	// declares, as a merge's header declares its latest source's (see
	// [Merge]), whichever version the fields above name.
	PageSources []*document.Document
	XRefStream  bool
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
//
// # Corpus evidence
//
//   - pdfjs issue18986.pdf, issue9418.pdf: the xref wrongly marks the catalog
//     object free, or points /Root at a non-catalog, so copying the trailer
//     /Root yielded a rootless output with no pages.
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
	// the ref to null; copying that ref yields a rootless output with no pages.
	// In that case copy the recovered catalog dict the reader synthesises, so
	// the rewrite reproduces what the reader reads, the same way it recovers a
	// trailer that lost /Root entirely.
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

	if infoRef, ok := copyTrailerInfo(ctx, doc, copier, alloc); ok {
		trailer["Info"] = infoRef
	}

	// Copy /ID if present.
	if idArr, ok := doc.Index.Trailer["ID"].(ast.PdfArray); ok {
		copiedID, err := copier.CopyObject(ctx, idArr)
		if err == nil {
			trailer["ID"] = copiedID
		}
	}

	version := cfg.Version
	if version == "" && cfg.RequiredVersion != "" {
		version = cfg.RequiredVersion
	}
	if version == "" {
		version = sourceVersion(doc)
	}
	for _, src := range cfg.PageSources {
		version = writer.AtLeastVersion(version, sourceVersion(src))
	}

	// Shared with Split and Merge so the three write paths cannot drift: see
	// applyEncryption for the ordering the sweep depends on.
	if cfg.EncryptCfg != nil {
		if err := applyEncryption(ctx, alloc, trailer, cfg.EncryptCfg, version); err != nil {
			return err
		}
	}

	// Decryption: the reader already decrypted all objects during resolution,
	// and the copier copies the decrypted data. All that remains is to omit
	// /Encrypt from the trailer, which the trailer built above never copies.

	return writer.WriteRewrite(ctx, dst, writer.RewriteConfig{
		Version:           version,
		Allocator:         alloc,
		Trailer:           trailer,
		UseXRefStream:     cfg.XRefStream,
		PackObjectStreams: cfg.Compact.PackObjectStreams,
		DedupStreams:      cfg.Compact.DedupStreams,
	})
}

// sourceVersion returns the version a rewrite of doc declares in its header
// when neither the caller nor a remediation names one: the later of what the
// source's header declares and what its catalog /Version declares.
//
// [document.Document.Version] alone is not that. It follows the catalog entry
// in both directions, as pdf.js does (see document.initCatalogVersion), so a
// %PDF-2.0 file whose catalog says /1.7 reads as 1.7. The rewrite copies the
// catalog and its /Version verbatim, so writing that answer into the header
// too left no trace of 2.0 in the output. ISO 32000-1, 7.7.2 (Table 28) makes
// a document conform to the later of the two, and ISO 19005-4, 6.1.2 requires
// %PDF-2.n of a PDF/A-4 file whatever its catalog says. With the later of the
// two in the header and the source's own entry in the catalog, the output
// reads as its source does under either rule. A catalog entry later than the
// header still raises the header, as it always has.
//
// A headerless source falls back to its catalog's version, and one with
// neither yields "", which [writer.WriteRewrite] writes as 1.4.
//
// # Corpus evidence
//
//   - litho a4syntax_4.pdf: its header is %PDF-2.0 and its catalog /Version
//     /1.7, and page insertion and deletion, flattening and encryption each
//     wrote %PDF-1.7.
//   - All corpora: 27 of the 5196 files litho opens declare a later version in
//     the header than the reader reports (13 pdfjs, 13 verapdf, 1 litho), and a
//     full rewrite wrote each one's header lower.
func sourceVersion(doc *document.Document) string {
	return writer.AtLeastVersion(doc.HeaderVersion, doc.Version)
}

// copyTrailerInfo copies doc's document information dictionary into a
// rewrite and returns the reference its new trailer names it by, or false
// when the source has none or it cannot be copied.
//
// ISO 32000-1, 7.5.5 (Table 15) makes the trailer /Info an indirect
// reference, but a producer can write the dictionary directly, as qpdf does
// when its trailer holds one, and litho reads it. Copying only a reference
// dropped a direct /Info, and every title, author and date in it. A direct
// dictionary is written as an object of its own: a new file has no reason to
// repeat the source's departure from Table 15, and an encrypted output then
// encrypts it like every other object, where a direct dictionary has no
// object number for 7.6.2, Algorithm 1 to key its strings with.
//
// It allocates, so it runs before applyEncryption, which writes whatever is
// allocated after it in the clear.
//
// # Corpus evidence
//
//   - litho xrefstm_2b.pdf: its cross-reference stream holds a direct /Info,
//     and page insertion and deletion, flattening and encryption each wrote
//     the file without one.
//   - All corpora: 2 of the 5196 files litho opens hold a direct /Info, litho
//     xrefstm_2b.pdf and pdfjs issue9540.pdf.
func copyTrailerInfo(ctx context.Context, doc *document.Document, copier *writer.Copier, alloc *writer.ObjAllocator) (ast.PdfIndirectRef, bool) {
	switch info := doc.Index.Trailer["Info"].(type) {
	case ast.PdfIndirectRef:
		ref, err := copier.CopyRef(ctx, info)
		if err != nil {
			doc.Logger.Debug("copying the trailer /Info failed", slog.Any("err", err))
			return ast.PdfIndirectRef{}, false
		}
		return ref, true
	case ast.PdfDict:
		copied, err := copier.CopyObject(ctx, info)
		if err != nil {
			doc.Logger.Debug("copying the direct trailer /Info failed", slog.Any("err", err))
			return ast.PdfIndirectRef{}, false
		}
		return alloc.Alloc(copied), true
	}
	return ast.PdfIndirectRef{}, false
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
// object equivalent to null.
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
//
// # Corpus evidence
//
//   - pdfjs poppler-937-0-fuzzed.pdf: its /Kids array opens with a NUL byte
//     instead of "[", and the rewrite turned its 1666 bytes into 344 whose
//     whole body was "1 0 obj null" under a catalog naming it.
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
// to load it, as when an encrypted file's page tree sits in an object stream
// that does not decode, and substituting an empty tree there hands back a
// document stripped of every page.
//
// # Corpus evidence
//
//   - pdfjs: an encrypted file's page tree sits in an object stream that would
//     not decode, while its cross-reference table lists the page tree as live.
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
