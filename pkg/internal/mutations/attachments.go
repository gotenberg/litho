package mutations

import (
	"context"
	"fmt"
	"maps"
	"sort"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// maxAttachmentTreeDepth caps EmbeddedFiles name-tree recursion in
// [CollectEmbeddedFileEntries]. Anti-DoS guard, not spec-mandated: real
// trees nest two or three levels; 64 mirrors maxNameTreeDepth in
// internal/core/document.
const maxAttachmentTreeDepth = 64

// EFEntry is one key-value pair of the EmbeddedFiles name tree.
type EFEntry struct {
	Name string
	Ref  ast.PdfObject
}

// CollectEmbeddedFileEntries flattens the EmbeddedFiles name tree rooted
// at nodeObj into key-value pairs, following /Kids intermediate nodes.
// Pending allocator versions take precedence so repeated mutations see
// each other's edits.
func CollectEmbeddedFileEntries(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, nodeObj ast.PdfObject, depth int, out *[]EFEntry) {
	if depth > maxAttachmentTreeDepth {
		return
	}
	var node ast.PdfDict
	if ref, ok := nodeObj.(ast.PdfIndirectRef); ok {
		node = ResolveFromAllocOrDoc(ctx, alloc, doc, ref)
	}
	if node == nil {
		node, _ = document.ResolveDict(ctx, doc, nodeObj) // Best-effort: nil node ends the branch.
	}
	if node == nil {
		return
	}
	if kidsObj, ok := node["Kids"]; ok {
		if kids, err := document.ResolveArray(ctx, doc, kidsObj); err == nil {
			for _, kid := range kids {
				CollectEmbeddedFileEntries(ctx, alloc, doc, kid, depth+1, out)
			}
			return
		}
	}
	namesObj, ok := node["Names"]
	if !ok {
		return
	}
	arr, ok := namesObj.(ast.PdfArray)
	if !ok {
		var err error
		if arr, err = document.ResolveArray(ctx, doc, namesObj); err != nil {
			return
		}
	}
	for i := 0; i+1 < len(arr); i += 2 {
		if s, ok := arr[i].(ast.PdfString); ok {
			*out = append(*out, EFEntry{Name: string(s), Ref: arr[i+1]})
		}
	}
}

// embeddedFilesArray emits the flattened entries as a single-level
// /Names array, sorted lexically by key as ISO 32000-2 §7.9.6 requires.
// Flattening is spec-valid: a root node carrying only /Names needs no
// /Limits or /Kids.
func embeddedFilesArray(entries []EFEntry) ast.PdfArray {
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	arr := make(ast.PdfArray, 0, len(entries)*2)
	for _, e := range entries {
		arr = append(arr, ast.PdfString(e.Name), e.Ref)
	}
	return arr
}

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

	// Flatten the existing tree (including /Kids nodes, which the old
	// single-leaf read silently dropped) and replace any entry that
	// already carries this name so re-running the mutation stays
	// idempotent: name-tree keys must be unique per ISO 32000-2 §7.9.6.
	var entries []EFEntry
	if efObj, ok := namesDict["EmbeddedFiles"]; ok {
		CollectEmbeddedFileEntries(ctx, alloc, doc, efObj, 0, &entries)
	}
	var replacedRef ast.PdfObject
	for i := range entries {
		if entries[i].Name == name {
			replacedRef = entries[i].Ref
			entries[i].Ref = filespecRef
		}
	}
	if replacedRef == nil {
		entries = append(entries, EFEntry{Name: name, Ref: filespecRef})
	}

	efDict := ast.PdfDict{"Names": embeddedFilesArray(entries)}
	efRef := alloc.Alloc(efDict)
	namesDict["EmbeddedFiles"] = efRef

	namesRef := alloc.Alloc(namesDict)

	// Update catalog.
	newCatalog := make(ast.PdfDict, len(catalogDict)+2)
	maps.Copy(newCatalog, catalogDict)
	newCatalog["Names"] = namesRef

	// Add to catalog /AF array if requested (PDF/A-3+ associated files).
	// A replaced attachment swaps its old filespec in place instead of
	// appending a duplicate associated-file entry.
	if opts.AddToCatalogAF {
		var afArr ast.PdfArray
		if existing, ok := catalogDict["AF"]; ok {
			if arr, err := document.ResolveArray(ctx, doc, existing); err == nil {
				afArr = arr
			}
		}
		swapped := false
		if oldRef, ok := replacedRef.(ast.PdfIndirectRef); ok {
			for i, item := range afArr {
				if cur, ok := item.(ast.PdfIndirectRef); ok && cur == oldRef {
					afArr[i] = filespecRef
					swapped = true
				}
			}
		}
		if !swapped {
			afArr = append(afArr, filespecRef)
		}
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

	// Flatten the tree (including /Kids nodes) and drop the matching
	// entries, remembering their filespec refs so the catalog /AF array
	// loses the now-dangling associated-file pointers too.
	var entries []EFEntry
	CollectEmbeddedFileEntries(ctx, alloc, doc, efObj, 0, &entries)
	var kept []EFEntry
	removedRefs := make(map[ast.PdfIndirectRef]bool)
	for _, e := range entries {
		if e.Name == name {
			if ref, ok := e.Ref.(ast.PdfIndirectRef); ok {
				removedRefs[ref] = true
			}
			continue
		}
		kept = append(kept, e)
	}
	if len(kept) == len(entries) {
		return nil // Nothing matched; leave the document untouched.
	}

	newEFDict := ast.PdfDict{"Names": embeddedFilesArray(kept)}
	efRef := alloc.Alloc(newEFDict)

	newNamesDict := make(ast.PdfDict, len(namesDict))
	maps.Copy(newNamesDict, namesDict)
	newNamesDict["EmbeddedFiles"] = efRef

	namesRef := alloc.Alloc(newNamesDict)

	newCatalog := make(ast.PdfDict, len(catalogDict))
	maps.Copy(newCatalog, catalogDict)
	newCatalog["Names"] = namesRef

	if afObj, ok := catalogDict["AF"]; ok && len(removedRefs) > 0 {
		if afArr, err := document.ResolveArray(ctx, doc, afObj); err == nil {
			pruned := make(ast.PdfArray, 0, len(afArr))
			for _, item := range afArr {
				if ref, ok := item.(ast.PdfIndirectRef); ok && removedRefs[ref] {
					continue
				}
				pruned = append(pruned, item)
			}
			if len(pruned) == 0 {
				delete(newCatalog, "AF")
			} else {
				newCatalog["AF"] = pruned
			}
		}
	}

	alloc.AllocAt(catalogRef.ObjectNum, newCatalog)

	return nil
}
