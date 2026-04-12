package mutations

import (
	"context"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// Metadata holds document information dictionary fields.
type Metadata struct {
	Title        string
	Author       string
	Subject      string
	Keywords     string
	Creator      string
	Producer     string
	CreationDate string
	ModDate      string
}

// SetMetadata creates or replaces the /Info dictionary.
func SetMetadata(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, meta Metadata) error {
	// Resolve the existing Info dict if any.
	var infoDict ast.PdfDict
	if infoObj, ok := doc.Index.Trailer["Info"]; ok {
		existing, err := document.ResolveDict(ctx, doc, infoObj)
		if err == nil && existing != nil {
			infoDict = make(ast.PdfDict, len(existing))
			maps.Copy(infoDict, existing)
		}
	}
	if infoDict == nil {
		infoDict = make(ast.PdfDict)
	}

	// Apply non-empty fields.
	setInfoField(infoDict, "Title", meta.Title)
	setInfoField(infoDict, "Author", meta.Author)
	setInfoField(infoDict, "Subject", meta.Subject)
	setInfoField(infoDict, "Keywords", meta.Keywords)
	setInfoField(infoDict, "Creator", meta.Creator)
	setInfoField(infoDict, "Producer", meta.Producer)
	setInfoField(infoDict, "CreationDate", meta.CreationDate)
	setInfoField(infoDict, "ModDate", meta.ModDate)

	// Allocate as a new or replacement object.
	if infoRef, ok := doc.Index.Trailer["Info"].(ast.PdfIndirectRef); ok {
		alloc.AllocAt(infoRef.ObjectNum, infoDict)
	} else {
		ref := alloc.Alloc(infoDict)
		doc.Index.Trailer["Info"] = ref
	}

	return nil
}

func setInfoField(d ast.PdfDict, key, value string) {
	if value != "" {
		d[ast.PdfName(key)] = ast.PdfString(value)
	}
}

// ClearMetadata removes all document metadata. It empties all /Info dictionary
// fields and removes the catalog /Metadata stream (XMP).
func ClearMetadata(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) error {
	// Clear /Info dictionary: replace with an empty dict.
	if infoRef, ok := doc.Index.Trailer["Info"].(ast.PdfIndirectRef); ok {
		alloc.AllocAt(infoRef.ObjectNum, ast.PdfDict{})
	} else {
		ref := alloc.Alloc(ast.PdfDict{})
		doc.Index.Trailer["Info"] = ref
	}

	// Remove /Metadata from catalog.
	rootObj, ok := doc.Index.Trailer["Root"]
	if !ok {
		return nil
	}
	catalogRef, ok := rootObj.(ast.PdfIndirectRef)
	if !ok {
		return nil
	}

	catalogDict := ResolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil
	}

	if _, hasMetadata := catalogDict["Metadata"]; hasMetadata {
		newCatalog := make(ast.PdfDict, len(catalogDict))
		maps.Copy(newCatalog, catalogDict)
		delete(newCatalog, ast.PdfName("Metadata"))
		alloc.AllocAt(catalogRef.ObjectNum, newCatalog)
	}

	return nil
}
