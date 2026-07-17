package mutations

import (
	"context"
	"fmt"
	"io"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/crypto"
)

// FullRewriteConfig holds the parameters for a full document rewrite.
type FullRewriteConfig struct {
	Doc             *document.Document
	EncryptCfg      *crypto.EncryptConfig
	Decrypt         bool
	Version         string
	RequiredVersion string
	XRefStream      bool
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
	catalogRef, hasRef := doc.Index.Trailer["Root"].(ast.PdfIndirectRef)
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

	// Handle encryption.
	if cfg.EncryptCfg != nil {
		docID := GenerateDocumentID()
		encryptor, encDict, err := crypto.NewEncryptor(*cfg.EncryptCfg, docID)
		if err != nil {
			return fmt.Errorf("creating encryptor: %w", err)
		}

		// Encrypt all pending objects.
		if err := EncryptPendingObjects(ctx, alloc, encryptor); err != nil {
			return fmt.Errorf("encrypting objects: %w", err)
		}

		// Add /Encrypt to trailer.
		encRef := alloc.Alloc(encDict)
		trailer["Encrypt"] = encRef
		trailer["ID"] = ast.PdfArray{
			ast.PdfString(string(docID)),
			ast.PdfString(string(docID)),
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
		Version:       version,
		Allocator:     alloc,
		Trailer:       trailer,
		UseXRefStream: cfg.XRefStream,
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
