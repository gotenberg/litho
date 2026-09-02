package mutations

import (
	"context"
	"fmt"
	"log/slog"
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
		// A name-tree key may be an indirect reference like any other object.
		// Asserting the type directly skipped those entries, and since the
		// collected set is what gets rewritten, attaching a new file DELETED
		// every attachment whose key was indirect: pdfjs issue18444.pdf lost
		// quitecommands.xml the moment anything else was attached.
		s, err := document.ResolveString(ctx, doc, arr[i])
		if err != nil {
			doc.Logger.Debug("resolving embedded file name", slog.Any("err", err))
			continue
		}
		// Decode: a key written by another producer may be UTF-16BE, and
		// carrying its raw bytes through made the name unusable for matching
		// and put mojibake in front of the caller.
		*out = append(*out, EFEntry{Name: document.DecodePdfString(s), Ref: arr[i+1]})
	}
}

// embeddedFilesArray emits the flattened entries as a single-level
// /Names array, sorted lexically by key as ISO 32000-2 §7.9.6 requires.
// Flattening is spec-valid: a root node carrying only /Names needs no
// /Limits or /Kids.
func embeddedFilesArray(entries []EFEntry) ast.PdfArray {
	// Encode first, then sort on the bytes that reach the file. ISO 32000-2,
	// 7.9.6 orders keys by BYTE VALUE, so ordering the Go strings and encoding
	// afterwards would write a tree whose /Limits do not bound it once a key
	// stops being ASCII. Casting the Go string straight to a PDF string also
	// left raw UTF-8 in the file, which no reader can decode: pikepdf showed
	// an attachment named "Café" as "CafÃ©".
	encoded := make([]struct {
		key ast.PdfString
		ref ast.PdfObject
	}, 0, len(entries))
	for _, e := range entries {
		encoded = append(encoded, struct {
			key ast.PdfString
			ref ast.PdfObject
		}{document.EncodePdfString(e.Name), e.Ref})
	}
	sort.SliceStable(encoded, func(i, j int) bool { return encoded[i].key < encoded[j].key })

	arr := make(ast.PdfArray, 0, len(encoded)*2)
	for _, e := range encoded {
		arr = append(arr, e.key, e.ref)
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
		"F":    fileSpecName(name),
		// /UF is a text string; /F stays a byte-oriented file specification
		// (ISO 32000-1, 7.11.2), which is why only one of the pair is encoded.
		"UF": document.EncodePdfString(name),
		"EF": ast.PdfDict{"F": fileStreamRef},
	}
	if opts.AFRelationship != "" {
		filespecDict["AFRelationship"] = ast.PdfName(opts.AFRelationship)
	}
	filespecRef := alloc.Alloc(filespecDict)

	// Update the catalog's /Names/EmbeddedFiles name tree.
	catalogRef, ok := CatalogRef(ctx, doc)
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

// RemoveAttachment removes an embedded file by its filename.
//
// A file reaches the document from two places. The /Names /EmbeddedFiles name
// tree holds document-level attachments (ISO 32000-1, 7.11.4), and a
// FileAttachment annotation carries its own file specification in /FS
// (12.5.6.15). Removing only the tree entry left the annotation, and its file,
// in the output while reporting success, so both are removed.
func RemoveAttachment(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, name string) error {
	if err := removeAttachmentAnnots(ctx, alloc, doc, name); err != nil {
		return err
	}
	if err := removeAssociatedFiles(ctx, alloc, doc, name); err != nil {
		return err
	}
	return removeAttachmentFromNameTree(ctx, alloc, doc, name)
}

// removeAttachmentAnnots drops every FileAttachment annotation whose file
// specification names the given file.
func removeAttachmentAnnots(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, name string) error {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return nil // No catalog: the name-tree pass reports the real problem.
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		return nil
	}

	for _, page := range pages {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("removing attachment annotations canceled: %w", err)
		}
		pageDict := latestPageDict(alloc, page)
		annotsObj, ok := pageDict["Annots"]
		if !ok {
			continue
		}
		annotsArr, err := document.ResolveArray(ctx, doc, annotsObj)
		if err != nil || annotsArr == nil {
			continue
		}

		kept := make(ast.PdfArray, 0, len(annotsArr))
		for _, annotObj := range annotsArr {
			if attachmentAnnotNames(ctx, doc, annotObj) == name {
				continue
			}
			kept = append(kept, annotObj)
		}
		if len(kept) == len(annotsArr) {
			continue
		}

		newPage := make(ast.PdfDict, len(pageDict))
		maps.Copy(newPage, pageDict)
		if len(kept) == 0 {
			delete(newPage, "Annots")
		} else {
			newPage["Annots"] = kept
		}
		alloc.AllocAt(page.Ref.ObjectNum, newPage)
	}
	return nil
}

// attachmentAnnotNames returns the filename a FileAttachment annotation
// carries, or "" when the annotation is not one or names no file.
func attachmentAnnotNames(ctx context.Context, doc *document.Document, annotObj ast.PdfObject) string {
	annotDict, err := document.ResolveDict(ctx, doc, annotObj)
	if err != nil || annotDict == nil {
		return ""
	}
	if subtype, err := document.ResolveName(ctx, doc, annotDict["Subtype"]); err != nil || subtype != "FileAttachment" {
		return ""
	}
	return fileSpecFilename(ctx, doc, annotDict["FS"])
}

// fileSpecFilename returns the filename a file specification states, preferring
// the Unicode /UF exactly as the listing does, so `detach` matches what
// `attachments` printed.
func fileSpecFilename(ctx context.Context, doc *document.Document, fsObj ast.PdfObject) string {
	fsDict, err := document.ResolveDict(ctx, doc, fsObj)
	if err != nil || fsDict == nil {
		return ""
	}
	for _, key := range []ast.PdfName{"UF", "F"} {
		if s, err := document.ResolveString(ctx, doc, fsDict[key]); err == nil && s != "" {
			return document.DecodePdfString(s)
		}
	}
	return ""
}

// removeAssociatedFiles drops the /AF entries naming the given file from the
// catalog and from every page.
//
// ISO 32000-2, 14.13 associates a file with the document or a page through /AF,
// and that array may be the only route to an embedded file. Leaving it behind
// kept the file in the output while `detach` reported success.
func removeAssociatedFiles(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, name string) error {
	catalogRef, ok := CatalogRef(ctx, doc)
	if !ok {
		return nil // The name-tree pass reports a missing catalog.
	}
	catalogDict := ResolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict != nil {
		if newDict, changed := strippedAF(ctx, doc, catalogDict, name); changed {
			alloc.AllocAt(catalogRef.ObjectNum, newDict)
		}
	}

	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return nil
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		return nil
	}
	for _, page := range pages {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("removing associated files canceled: %w", err)
		}
		pageDict := latestPageDict(alloc, page)
		if newDict, changed := strippedAF(ctx, doc, pageDict, name); changed {
			alloc.AllocAt(page.Ref.ObjectNum, newDict)
		}
	}
	return nil
}

// strippedAF returns a copy of dict whose /AF array no longer names the file,
// and whether anything was dropped. An /AF left with nothing in it is removed
// rather than written as an empty array.
func strippedAF(ctx context.Context, doc *document.Document, dict ast.PdfDict, name string) (ast.PdfDict, bool) {
	afObj, ok := dict["AF"]
	if !ok {
		return nil, false
	}
	specs, err := document.ResolveArray(ctx, doc, afObj)
	if err != nil {
		// ISO 32000-2 allows a single file specification where an array is
		// expected; treat it as a one-entry array.
		specs = ast.PdfArray{afObj}
	}

	kept := make(ast.PdfArray, 0, len(specs))
	for _, specObj := range specs {
		if fileSpecFilename(ctx, doc, specObj) == name {
			continue
		}
		kept = append(kept, specObj)
	}
	if len(kept) == len(specs) {
		return nil, false
	}

	newDict := make(ast.PdfDict, len(dict))
	maps.Copy(newDict, dict)
	if len(kept) == 0 {
		delete(newDict, "AF")
	} else {
		newDict["AF"] = kept
	}
	return newDict, true
}

// removeAttachmentFromNameTree removes a file from the document's
// /Names/EmbeddedFiles name tree by its filename.
func removeAttachmentFromNameTree(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, name string) error {
	catalogRef, ok := CatalogRef(ctx, doc)
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

// fileSpecName renders a file specification's /F entry. Unlike /UF it is not a
// text string, so it may not take the UTF-16BE form (ISO 32000-1, 7.11.2): a
// name PDFDocEncoding cannot spell keeps its existing bytes and the Unicode
// spelling travels in /UF, which every reader since PDF 1.7 prefers.
func fileSpecName(name string) ast.PdfString {
	if encoded, ok := document.EncodePdfDocString(name); ok {
		return encoded
	}
	return ast.PdfString(name)
}
