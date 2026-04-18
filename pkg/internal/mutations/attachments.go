package mutations

import (
	"context"
	"fmt"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// AttachmentOptions configures an embedded file attachment.
// AFRelationship and adding to the catalog /AF array are required for
// PDF/A-3+ and Factur-X / ZUGFeRD compliance.
type AttachmentOptions struct {
	// MimeType is the embedded file /Subtype (e.g., "application/xml").
	// Empty means no /Subtype is set.
	MimeType string
	// AFRelationship is the /AFRelationship value on the file specification
	// dictionary. Valid values per ISO 32000-2 §7.11.3: Source, Data,
	// Alternative, Supplement, EncryptedPayload, FormData, Schema,
	// Unspecified. Empty means no /AFRelationship is set.
	AFRelationship string
	// AddToCatalogAF adds the filespec reference to the catalog /AF array,
	// required for PDF/A-3+ associated files.
	AddToCatalogAF bool
}

// AddAttachment embeds a file in the document's /Names/EmbeddedFiles tree.
// Backward-compatible signature; for /AFRelationship and catalog /AF support
// use [AddAttachmentWithOptions].
func AddAttachment(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, name string, data []byte, mimeType string) error {
	return AddAttachmentWithOptions(ctx, alloc, doc, name, data, AttachmentOptions{MimeType: mimeType})
}

// AddAttachmentWithOptions embeds a file with full control over PDF/A-3+
// associated-file metadata. Sets /Subtype, /AFRelationship, and optionally
// adds the filespec to the catalog /AF array.
func AddAttachmentWithOptions(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, name string, data []byte, opts AttachmentOptions) error {
	// Create the embedded file stream.
	streamDict := ast.PdfDict{
		"Type":   ast.PdfName("EmbeddedFile"),
		"Params": ast.PdfDict{"Size": ast.PdfInt(len(data))},
	}
	if opts.MimeType != "" {
		streamDict["Subtype"] = ast.PdfName(opts.MimeType)
	}
	fileStream := ast.NewStream(streamDict, data)
	fileStreamRef := alloc.Alloc(fileStream)

	// Create the Filespec dictionary.
	filespecDict := ast.PdfDict{
		"Type": ast.PdfName("Filespec"),
		"F":    ast.PdfString(name),
		"UF":   ast.PdfString(name),
		"EF":   ast.PdfDict{"F": fileStreamRef},
	}
	if opts.AFRelationship != "" {
		filespecDict["AFRelationship"] = ast.PdfName(opts.AFRelationship)
	}
	filespecRef := alloc.Alloc(filespecDict)

	// Update the catalog's /Names/EmbeddedFiles name tree.
	catalogRef, ok := doc.Index.Trailer["Root"].(ast.PdfIndirectRef)
	if !ok {
		return fmt.Errorf("trailer missing /Root")
	}

	// Check allocator first (another mutation may have already modified the catalog).
	catalogDict := ResolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return fmt.Errorf("resolving catalog")
	}

	// Get or create /Names dict. Check allocator for previously modified versions.
	var namesDict ast.PdfDict
	if namesObj, ok := catalogDict["Names"]; ok {
		if namesRef, ok := namesObj.(ast.PdfIndirectRef); ok {
			if d := ResolveFromAllocOrDoc(ctx, alloc, doc, namesRef); d != nil {
				namesDict = make(ast.PdfDict, len(d)+1)
				maps.Copy(namesDict, d)
			}
		}
		if namesDict == nil {
			if d, err := document.ResolveDict(ctx, doc, namesObj); err == nil && d != nil {
				namesDict = make(ast.PdfDict, len(d)+1)
				maps.Copy(namesDict, d)
			}
		}
	}
	if namesDict == nil {
		namesDict = ast.PdfDict{}
	}

	// Get or create /EmbeddedFiles name tree.
	var efNames ast.PdfArray
	if efObj, ok := namesDict["EmbeddedFiles"]; ok {
		var efDict ast.PdfDict
		if efRef, ok := efObj.(ast.PdfIndirectRef); ok {
			efDict = ResolveFromAllocOrDoc(ctx, alloc, doc, efRef)
		}
		if efDict == nil {
			efDict, _ = document.ResolveDict(ctx, doc, efObj) // Best-effort: nil efDict is handled below.
		}
		if efDict != nil {
			if namesArr, ok := efDict["Names"]; ok {
				if arr, ok := namesArr.(ast.PdfArray); ok {
					efNames = arr
				} else if arr, err := document.ResolveArray(ctx, doc, namesArr); err == nil {
					efNames = arr
				}
			}
		}
	}

	// Add the new entry: [name, filespecRef, ...existing...]
	efNames = append(ast.PdfArray{ast.PdfString(name), filespecRef}, efNames...)

	efDict := ast.PdfDict{"Names": efNames}
	efRef := alloc.Alloc(efDict)
	namesDict["EmbeddedFiles"] = efRef

	namesRef := alloc.Alloc(namesDict)

	// Update catalog.
	newCatalog := make(ast.PdfDict, len(catalogDict)+2)
	maps.Copy(newCatalog, catalogDict)
	newCatalog["Names"] = namesRef

	// Add to catalog /AF array if requested (PDF/A-3+ associated files).
	if opts.AddToCatalogAF {
		var afArr ast.PdfArray
		if existing, ok := catalogDict["AF"]; ok {
			if arr, err := document.ResolveArray(ctx, doc, existing); err == nil {
				afArr = arr
			}
		}
		afArr = append(afArr, filespecRef)
		newCatalog["AF"] = afArr
	}

	alloc.AllocAt(catalogRef.ObjectNum, newCatalog)

	return nil
}

// RemoveAttachment removes a file from the document's /Names/EmbeddedFiles
// name tree by its filename.
func RemoveAttachment(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, name string) error {
	catalogRef, ok := doc.Index.Trailer["Root"].(ast.PdfIndirectRef)
	if !ok {
		return fmt.Errorf("trailer missing /Root")
	}

	catalogDict := ResolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return fmt.Errorf("resolving catalog")
	}

	namesObj, ok := catalogDict["Names"]
	if !ok {
		return nil // No /Names dict, nothing to remove.
	}

	var namesDict ast.PdfDict
	if namesRef, ok := namesObj.(ast.PdfIndirectRef); ok {
		if d := ResolveFromAllocOrDoc(ctx, alloc, doc, namesRef); d != nil {
			namesDict = make(ast.PdfDict, len(d))
			maps.Copy(namesDict, d)
		}
	}
	if namesDict == nil {
		if d, err := document.ResolveDict(ctx, doc, namesObj); err == nil && d != nil {
			namesDict = make(ast.PdfDict, len(d))
			maps.Copy(namesDict, d)
		}
	}
	if namesDict == nil {
		return nil
	}

	efObj, ok := namesDict["EmbeddedFiles"]
	if !ok {
		return nil
	}

	var efDict ast.PdfDict
	if efRef, ok := efObj.(ast.PdfIndirectRef); ok {
		efDict = ResolveFromAllocOrDoc(ctx, alloc, doc, efRef)
	}
	if efDict == nil {
		efDict, _ = document.ResolveDict(ctx, doc, efObj) // Best-effort: nil efDict is handled below.
	}
	if efDict == nil {
		return nil
	}

	namesArr, ok := efDict["Names"]
	if !ok {
		return nil
	}

	var arr ast.PdfArray
	if a, ok := namesArr.(ast.PdfArray); ok {
		arr = a
	} else if a, err := document.ResolveArray(ctx, doc, namesArr); err == nil {
		arr = a
	}
	if arr == nil {
		return nil
	}

	// Name tree entries are [name1, ref1, name2, ref2, ...].
	// Find and remove the matching pair.
	var filtered ast.PdfArray
	for i := 0; i+1 < len(arr); i += 2 {
		entryName := ""
		if s, ok := arr[i].(ast.PdfString); ok {
			entryName = string(s)
		}
		if entryName == name {
			continue // Skip this pair.
		}
		filtered = append(filtered, arr[i], arr[i+1])
	}

	// Rebuild the name tree.
	newEFDict := ast.PdfDict{"Names": filtered}
	efRef := alloc.Alloc(newEFDict)

	newNamesDict := make(ast.PdfDict, len(namesDict))
	maps.Copy(newNamesDict, namesDict)
	newNamesDict["EmbeddedFiles"] = efRef

	namesRef := alloc.Alloc(newNamesDict)

	newCatalog := make(ast.PdfDict, len(catalogDict))
	maps.Copy(newCatalog, catalogDict)
	newCatalog["Names"] = namesRef
	alloc.AllocAt(catalogRef.ObjectNum, newCatalog)

	return nil
}
