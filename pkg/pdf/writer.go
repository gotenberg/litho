package pdf

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"maps"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/crypto"
	"github.com/gotenberg/litho/pkg/models"
)

// Metadata holds document information dictionary fields for [Writer.SetMetadata].
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

// Writer applies content-preserving modifications to a PDF document.
// Mutations are batched and written atomically via [Writer.WriteTo].
type Writer struct {
	reader       *Reader
	mutations    []mutation
	encryptCfg   *crypto.EncryptConfig
	decrypt      bool
	forceRewrite bool
	signer       Signer
}

// mutation is a deferred modification applied during WriteTo.
type mutation struct {
	apply func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) error
}

// NewWriter creates a Writer that modifies the document behind the given Reader.
func NewWriter(r *Reader) *Writer {
	return &Writer{reader: r}
}

// SetMetadata sets or replaces document Info dictionary fields. Empty fields
// in meta are skipped (the existing value is preserved).
func (w *Writer) SetMetadata(_ context.Context, meta Metadata) {
	w.mutations = append(w.mutations, mutation{
		apply: func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) error {
			return applySetMetadata(ctx, alloc, doc, meta)
		},
	})
}

// RotatePages sets the /Rotate value for the given 1-based page numbers.
// Rotation must be a multiple of 90 (0, 90, 180, 270).
func (w *Writer) RotatePages(ctx context.Context, rotation int, pages ...int) error {
	if rotation%90 != 0 {
		return fmt.Errorf("rotation must be a multiple of 90, got %d", rotation)
	}
	w.mutations = append(w.mutations, mutation{
		apply: func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) error {
			return applyRotatePages(ctx, alloc, w.reader, rotation, pages)
		},
	})
	return nil
}

// SetBookmarks replaces the document outline with the given bookmarks.
func (w *Writer) SetBookmarks(_ context.Context, bookmarks []models.Bookmark) {
	w.mutations = append(w.mutations, mutation{
		apply: func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) error {
			return applySetBookmarks(ctx, alloc, doc, w.reader, bookmarks)
		},
	})
}

// Encrypt applies password-based encryption to the output document. Forces a
// full rewrite since every string and stream must be encrypted.
func (w *Writer) Encrypt(cfg crypto.EncryptConfig) {
	w.encryptCfg = &cfg
	w.forceRewrite = true
}

// Decrypt removes encryption from the output document. The Reader must have
// been opened with the correct password. Forces a full rewrite.
func (w *Writer) Decrypt() {
	w.decrypt = true
	w.forceRewrite = true
}

// SetFormField sets the value of an AcroForm field by its fully qualified name.
// Uses incremental update. Sets /NeedAppearances on the AcroForm so viewers
// regenerate the visual appearance.
func (w *Writer) SetFormField(_ context.Context, name, value string) {
	w.mutations = append(w.mutations, mutation{
		apply: func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) error {
			return applySetFormField(ctx, alloc, doc, name, value)
		},
	})
}

// FlattenForm bakes form field appearances into page content streams and
// removes the AcroForm dictionary. Forces a full rewrite.
func (w *Writer) FlattenForm(_ context.Context) {
	w.forceRewrite = true
	w.mutations = append(w.mutations, mutation{
		apply: applyFlattenForm,
	})
}

// TextStamp defines a text overlay to stamp on pages.
type TextStamp struct {
	Text     string  // Text content
	X, Y     float64 // Position in points from bottom-left
	FontSize float64 // Font size in points
	Opacity  float64 // 0.0–1.0 (1.0 = fully opaque)
}

// StampText overlays text on the specified 1-based pages. If no pages are
// specified, the stamp is applied to all pages. Uses incremental update.
func (w *Writer) StampText(ctx context.Context, stamp TextStamp, pages ...int) {
	w.mutations = append(w.mutations, mutation{
		apply: func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) error {
			return applyStampText(ctx, alloc, doc, w.reader, stamp, pages)
		},
	})
}

// AddAttachment embeds a file in the document's /EmbeddedFiles name tree.
// Uses incremental update.
func (w *Writer) AddAttachment(_ context.Context, name string, data []byte, mimeType string) {
	w.mutations = append(w.mutations, mutation{
		apply: func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) error {
			return applyAddAttachment(ctx, alloc, doc, name, data, mimeType)
		},
	})
}

// WriteTo serializes the modified document to dst. By default, it uses an
// incremental update (appending to the original file). Pass [WithFullRewrite]
// to force a clean rewrite.
func (w *Writer) WriteTo(ctx context.Context, dst io.Writer, opts ...WriteOption) error {
	cfg := writeOpts{}
	for _, o := range opts {
		o(&cfg)
	}

	// Signing requires its own two-pass incremental flow.
	if w.signer != nil {
		return w.writeWithSignature(ctx, dst, cfg)
	}

	if cfg.fullRewrite || w.forceRewrite {
		return w.writeFullRewrite(ctx, dst, cfg)
	}

	doc := w.reader.doc

	// Find max object number for the allocator.
	maxObjNum := 0
	for objNum := range doc.Index.Entries {
		if objNum > maxObjNum {
			maxObjNum = objNum
		}
	}

	alloc := writer.NewObjAllocator(maxObjNum)

	// Apply all batched mutations.
	for _, m := range w.mutations {
		if err := m.apply(ctx, alloc, doc); err != nil {
			return fmt.Errorf("applying mutation: %w", err)
		}
	}

	// If no mutations produced pending objects, nothing to write.
	if len(alloc.Pending()) == 0 {
		// Just copy the original file as-is.
		src := io.NewSectionReader(doc.RawReader(), 0, doc.FileSize())
		_, err := io.Copy(dst, src)
		return err
	}

	// Find the original startxref offset.
	startXRef, err := document.FindStartXRef(doc.RawReader(), doc.FileSize())
	if err != nil {
		return fmt.Errorf("finding startxref: %w", err)
	}

	// Build the trailer from the existing one.
	trailer := make(ast.PdfDict, len(doc.Index.Trailer))
	for k, v := range doc.Index.Trailer {
		if k == "Prev" || k == "Size" {
			continue // managed by the incremental engine
		}
		trailer[k] = v
	}

	// Detect xref format: use xref streams if the source used them or if requested.
	useXRefStream := cfg.xrefStream
	if _, ok := doc.Index.Trailer["Type"]; ok {
		// If trailer has /Type, it was a xref stream.
		useXRefStream = true
	}

	return writer.WriteIncremental(ctx, dst, writer.IncrementalConfig{
		Source:          doc.RawReader(),
		SourceSize:      doc.FileSize(),
		StartXRefOffset: startXRef,
		Allocator:       alloc,
		Trailer:         trailer,
		UseXRefStream:   useXRefStream,
	})
}

// applySetMetadata creates or replaces the /Info dictionary.
func applySetMetadata(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, meta Metadata) error {
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

// applyRotatePages modifies /Rotate on the specified page dictionaries.
func applyRotatePages(ctx context.Context, alloc *writer.ObjAllocator, r *Reader, rotation int, pages []int) error {
	for _, pageNum := range pages {
		p, err := r.resolvePage(ctx, pageNum)
		if err != nil {
			return fmt.Errorf("resolving page %d: %w", pageNum, err)
		}

		// Clone the page dict and set /Rotate.
		newDict := make(ast.PdfDict, len(p.Dict)+1)
		maps.Copy(newDict, p.Dict)
		newDict["Rotate"] = ast.PdfInt(rotation)

		// Write the modified page at its existing object number.
		alloc.AllocAt(p.Ref.ObjectNum, newDict)
	}
	return nil
}

// applySetBookmarks replaces the document outline tree.
func applySetBookmarks(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, r *Reader, bookmarks []models.Bookmark) error {
	if len(bookmarks) == 0 {
		return nil
	}

	// Resolve pages to build page number → indirect ref mapping.
	resolvedPages, err := r.resolvePages(ctx)
	if err != nil {
		return fmt.Errorf("resolving pages for bookmarks: %w", err)
	}
	pageRefs := make(map[int]ast.PdfIndirectRef, len(resolvedPages))
	for i, p := range resolvedPages {
		pageRefs[i+1] = p.Ref // 1-based
	}

	// Build outline items as a linked list (each item has /Next, /Prev, /Parent).
	outlineRef := alloc.Alloc(ast.PdfNull{}) // placeholder, will be replaced
	items := buildOutlineItems(alloc, bookmarks, outlineRef, pageRefs)

	if len(items) == 0 {
		return nil
	}

	// Build the root /Outlines dict.
	outlineDict := ast.PdfDict{
		"Type":  ast.PdfName("Outlines"),
		"First": items[0],
		"Last":  items[len(items)-1],
		"Count": ast.PdfInt(len(bookmarks)),
	}
	alloc.AllocAt(outlineRef.ObjectNum, outlineDict)

	// Update the catalog to reference the new /Outlines.
	catalogRef, ok := doc.Index.Trailer["Root"].(ast.PdfIndirectRef)
	if !ok {
		return fmt.Errorf("trailer missing /Root indirect reference")
	}
	catalogDict, err := document.ResolveDict(ctx, doc, catalogRef)
	if err != nil {
		return fmt.Errorf("resolving catalog: %w", err)
	}

	newCatalog := make(ast.PdfDict, len(catalogDict)+1)
	maps.Copy(newCatalog, catalogDict)
	newCatalog["Outlines"] = outlineRef

	alloc.AllocAt(catalogRef.ObjectNum, newCatalog)

	return nil
}

// buildOutlineItems creates outline item objects and returns their refs.
func buildOutlineItems(alloc *writer.ObjAllocator, bookmarks []models.Bookmark, parent ast.PdfIndirectRef, pageRefs map[int]ast.PdfIndirectRef) []ast.PdfIndirectRef {
	refs := make([]ast.PdfIndirectRef, len(bookmarks))

	// First pass: allocate refs.
	for i := range bookmarks {
		refs[i] = alloc.Alloc(ast.PdfNull{}) // placeholder
	}

	// Second pass: build dicts with links.
	for i, bm := range bookmarks {
		d := ast.PdfDict{
			"Title":  ast.PdfString(bm.Title),
			"Parent": parent,
		}

		// Destination: [pageRef /Fit]
		if pageRef, ok := pageRefs[bm.PageNumber]; ok {
			d["Dest"] = ast.PdfArray{pageRef, ast.PdfName("Fit")}
		}

		if i > 0 {
			d["Prev"] = refs[i-1]
		}
		if i < len(bookmarks)-1 {
			d["Next"] = refs[i+1]
		}

		// Recurse for children.
		if len(bm.Children) > 0 {
			childRefs := buildOutlineItems(alloc, bm.Children, refs[i], pageRefs)
			if len(childRefs) > 0 {
				d["First"] = childRefs[0]
				d["Last"] = childRefs[len(childRefs)-1]
				d["Count"] = ast.PdfInt(len(bm.Children))
			}
		}

		alloc.AllocAt(refs[i].ObjectNum, d)
	}

	return refs
}

// writeFullRewrite performs a complete rewrite of the document, applying all
// mutations and optionally encrypting or decrypting the output.
func (w *Writer) writeFullRewrite(ctx context.Context, dst io.Writer, cfg writeOpts) error {
	doc := w.reader.doc
	alloc := writer.NewObjAllocator(0)
	copier := writer.NewCopier(doc, alloc)

	// Copy the catalog (root of the object graph).
	catalogRef, ok := doc.Index.Trailer["Root"].(ast.PdfIndirectRef)
	if !ok {
		return fmt.Errorf("trailer missing /Root indirect reference")
	}
	newCatalogRef, err := copier.CopyRef(ctx, catalogRef)
	if err != nil {
		return fmt.Errorf("copying catalog: %w", err)
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

	// Apply batched mutations on the rewritten objects.
	// Mutations modify objects in the allocator's pending map.
	for _, m := range w.mutations {
		if err := m.apply(ctx, alloc, doc); err != nil {
			return fmt.Errorf("applying mutation: %w", err)
		}
	}

	// Handle encryption.
	if w.encryptCfg != nil {
		docID := generateDocumentID()
		encryptor, encDict, err := crypto.NewEncryptor(*w.encryptCfg, docID)
		if err != nil {
			return fmt.Errorf("creating encryptor: %w", err)
		}

		// Encrypt all pending objects.
		if err := encryptPendingObjects(alloc, encryptor); err != nil {
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

	version := cfg.version
	if version == "" {
		version = doc.Version
	}

	return writer.WriteRewrite(ctx, dst, writer.RewriteConfig{
		Version:       version,
		Allocator:     alloc,
		Trailer:       trailer,
		UseXRefStream: cfg.xrefStream,
	})
}

// encryptPendingObjects encrypts all string and stream objects in the allocator.
func encryptPendingObjects(alloc *writer.ObjAllocator, enc *crypto.Encryptor) error {
	for objNum, obj := range alloc.Pending() {
		encrypted, err := encryptObject(obj, enc, objNum, 0)
		if err != nil {
			return fmt.Errorf("encrypting object %d: %w", objNum, err)
		}
		alloc.AllocAt(objNum, encrypted)
	}
	return nil
}

// encryptObject recursively encrypts strings and streams within an object.
func encryptObject(obj ast.PdfObject, enc *crypto.Encryptor, objNum, genNum int) (ast.PdfObject, error) {
	encFn := enc.GetEncryptFunc(objNum, genNum)

	switch v := obj.(type) {
	case ast.PdfString:
		encrypted, err := encFn([]byte(v))
		if err != nil {
			return nil, err
		}
		return ast.PdfString(string(encrypted)), nil
	case ast.PdfStream:
		// Encrypt stream data.
		data, err := v.Bytes()
		if err != nil {
			return nil, err
		}
		encrypted, err := encFn(data)
		if err != nil {
			return nil, err
		}
		// Encrypt strings in the dict (but not /Length, /Filter, /DecodeParms).
		newDict, err := encryptDict(v.Dict, enc, objNum, genNum)
		if err != nil {
			return nil, err
		}
		return ast.NewStream(newDict, encrypted), nil
	case ast.PdfDict:
		return encryptDict(v, enc, objNum, genNum)
	case ast.PdfArray:
		result := make(ast.PdfArray, len(v))
		for i, elem := range v {
			encrypted, err := encryptObject(elem, enc, objNum, genNum)
			if err != nil {
				return nil, err
			}
			result[i] = encrypted
		}
		return result, nil
	default:
		return obj, nil // scalars, refs, null — not encrypted
	}
}

// encryptDict encrypts string values within a dictionary.
func encryptDict(d ast.PdfDict, enc *crypto.Encryptor, objNum, genNum int) (ast.PdfDict, error) {
	result := make(ast.PdfDict, len(d))
	for k, v := range d {
		encrypted, err := encryptObject(v, enc, objNum, genNum)
		if err != nil {
			return nil, err
		}
		result[k] = encrypted
	}
	return result, nil
}

// generateDocumentID creates a random 16-byte document ID.
func generateDocumentID() []byte {
	id := make([]byte, 16)
	_, _ = rand.Read(id) // crypto/rand never fails on supported platforms
	return id
}

// applySetFormField finds a form field by name and updates its /V value.
func applySetFormField(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, name, value string) error {
	catalogRef, ok := doc.Index.Trailer["Root"].(ast.PdfIndirectRef)
	if !ok {
		return fmt.Errorf("trailer missing /Root")
	}
	catalogDict, err := document.ResolveDict(ctx, doc, catalogRef)
	if err != nil {
		return fmt.Errorf("resolving catalog: %w", err)
	}
	acroFormObj, ok := catalogDict["AcroForm"]
	if !ok {
		return fmt.Errorf("no AcroForm in catalog")
	}

	// Resolve AcroForm dict and its ref for modification.
	acroFormDict, err := document.ResolveDict(ctx, doc, acroFormObj)
	if err != nil {
		return fmt.Errorf("resolving AcroForm: %w", err)
	}

	fieldsObj, ok := acroFormDict["Fields"]
	if !ok {
		return fmt.Errorf("AcroForm has no Fields")
	}
	fieldsArr, err := document.ResolveArray(ctx, doc, fieldsObj)
	if err != nil {
		return fmt.Errorf("resolving Fields: %w", err)
	}

	// Find and update the field.
	found, err := findAndSetField(ctx, doc, alloc, fieldsArr, "", name, value)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("form field %q not found", name)
	}

	// Set /NeedAppearances on AcroForm so viewers regenerate appearance.
	newAcroForm := make(ast.PdfDict, len(acroFormDict)+1)
	maps.Copy(newAcroForm, acroFormDict)
	newAcroForm["NeedAppearances"] = ast.PdfBool(true)

	if acroFormRef, ok := acroFormObj.(ast.PdfIndirectRef); ok {
		alloc.AllocAt(acroFormRef.ObjectNum, newAcroForm)
	}

	return nil
}

// findAndSetField recursively traverses the field tree to find and update a field.
func findAndSetField(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, fields ast.PdfArray, parentName, targetName, value string) (bool, error) {
	for _, fieldObj := range fields {
		var fieldRef ast.PdfIndirectRef
		if ref, ok := fieldObj.(ast.PdfIndirectRef); ok {
			fieldRef = ref
		}

		fieldDict, err := document.ResolveDict(ctx, doc, fieldObj)
		if err != nil || fieldDict == nil {
			continue
		}

		// Build fully qualified name.
		partialName := ""
		if tObj, ok := fieldDict["T"]; ok {
			if tStr, err := document.ResolveString(ctx, doc, tObj); err == nil && tStr != "" {
				partialName = document.DecodePdfString(tStr)
			}
		}
		fullName := parentName
		if partialName != "" {
			if fullName != "" {
				fullName += "." + partialName
			} else {
				fullName = partialName
			}
		}

		if fullName == targetName {
			// Found it — update /V.
			newDict := make(ast.PdfDict, len(fieldDict)+1)
			maps.Copy(newDict, fieldDict)
			newDict["V"] = ast.PdfString(value)
			if fieldRef.ObjectNum > 0 {
				alloc.AllocAt(fieldRef.ObjectNum, newDict)
			}
			return true, nil
		}

		// Recurse into /Kids.
		if kidsObj, ok := fieldDict["Kids"]; ok {
			kidsArr, err := document.ResolveArray(ctx, doc, kidsObj)
			if err == nil && kidsArr != nil {
				found, err := findAndSetField(ctx, doc, alloc, kidsArr, fullName, targetName, value)
				if err != nil {
					return false, err
				}
				if found {
					return true, nil
				}
			}
		}
	}
	return false, nil
}

// applyFlattenForm bakes form field appearances into page content and removes
// the AcroForm dictionary from the catalog.
func applyFlattenForm(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) error {
	catalogRef, ok := doc.Index.Trailer["Root"].(ast.PdfIndirectRef)
	if !ok {
		return fmt.Errorf("trailer missing /Root")
	}
	catalogDict, err := document.ResolveDict(ctx, doc, catalogRef)
	if err != nil {
		return fmt.Errorf("resolving catalog: %w", err)
	}

	// Remove /AcroForm from the catalog.
	newCatalog := make(ast.PdfDict, len(catalogDict))
	for k, v := range catalogDict {
		if k == "AcroForm" {
			continue
		}
		newCatalog[k] = v
	}
	alloc.AllocAt(catalogRef.ObjectNum, newCatalog)

	// For each page, find widget annotations with /AP and stamp them into
	// the content stream, then remove them from /Annots.
	catalog, err := document.GetCatalog(ctx, doc)
	if err != nil {
		return fmt.Errorf("getting catalog: %w", err)
	}
	pages, err := document.GetPages(ctx, doc, catalog)
	if err != nil {
		return fmt.Errorf("getting pages: %w", err)
	}

	for _, page := range pages {
		if err := flattenPageAnnotations(ctx, doc, alloc, page); err != nil {
			return fmt.Errorf("flattening page annotations: %w", err)
		}
	}

	return nil
}

// flattenPageAnnotations processes widget annotations on a page, stamping
// their appearance streams into the page content and removing them from /Annots.
func flattenPageAnnotations(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, page *document.Page) error {
	annotsObj, ok := page.Dict["Annots"]
	if !ok {
		return nil
	}
	annots, err := document.ResolveArray(ctx, doc, annotsObj)
	if err != nil || annots == nil {
		return nil
	}

	var remainingAnnots ast.PdfArray
	var stampOps []string

	for _, annotObj := range annots {
		annotDict, err := document.ResolveDict(ctx, doc, annotObj)
		if err != nil || annotDict == nil {
			remainingAnnots = append(remainingAnnots, annotObj)
			continue
		}

		// Only flatten Widget annotations (form fields).
		subtypeObj, _ := document.ResolveName(ctx, doc, annotDict["Subtype"])
		if subtypeObj != "Widget" {
			remainingAnnots = append(remainingAnnots, annotObj)
			continue
		}

		// Get the appearance stream.
		apObj, ok := annotDict["AP"]
		if !ok {
			continue // No appearance, skip
		}
		apDict, err := document.ResolveDict(ctx, doc, apObj)
		if err != nil || apDict == nil {
			continue
		}

		// Get /N (normal appearance). Could be a stream or a dict of states.
		nObj, ok := apDict["N"]
		if !ok {
			continue
		}

		var apStream ast.PdfStream
		switch v := nObj.(type) {
		case ast.PdfStream:
			apStream = v
		default:
			// Could be a dict of states — try the /AS key.
			resolved, err := document.ResolveObject(ctx, doc, nObj)
			if err != nil {
				continue
			}
			if s, ok := resolved.(ast.PdfStream); ok {
				apStream = s
			} else if stateDict, ok := resolved.(ast.PdfDict); ok {
				// Look up the current appearance state.
				asName := ast.PdfName("Off")
				if as, ok := annotDict["AS"].(ast.PdfName); ok {
					asName = as
				}
				if stateObj, ok := stateDict[asName]; ok {
					resolved2, err := document.ResolveObject(ctx, doc, stateObj)
					if err == nil {
						if s, ok := resolved2.(ast.PdfStream); ok {
							apStream = s
						}
					}
				}
			}
		}

		if apStream.Dict == nil {
			continue
		}

		// Get the annotation /Rect for positioning.
		rectArr, err := document.ResolveArray(ctx, doc, annotDict["Rect"])
		if err != nil || len(rectArr) < 4 {
			continue
		}
		x1 := document.GetFloat(rectArr[0])
		y1 := document.GetFloat(rectArr[1])

		// Allocate the appearance stream as a Form XObject.
		apRef := alloc.Alloc(apStream)
		xobjName := fmt.Sprintf("FlatAP%d", apRef.ObjectNum)

		// Generate content stream commands to stamp the appearance.
		stampOps = append(stampOps, fmt.Sprintf("\nq 1 0 0 1 %s %s cm /%s Do Q",
			formatFloat(x1), formatFloat(y1), xobjName))

		// Add the XObject to page resources.
		addXObjectToPage(ctx, doc, alloc, page, xobjName, apRef)
	}

	if len(stampOps) > 0 {
		// Append stamp operations to page contents.
		appendToPageContents(ctx, doc, alloc, page, stampOps)
	}

	// Update /Annots (remove flattened widgets).
	if len(remainingAnnots) > 0 {
		newDict := make(ast.PdfDict, len(page.Dict))
		maps.Copy(newDict, page.Dict)
		newDict["Annots"] = remainingAnnots
		alloc.AllocAt(page.Ref.ObjectNum, newDict)
	} else if len(annots) > 0 {
		// All annotations were flattened — remove /Annots.
		newDict := make(ast.PdfDict, len(page.Dict))
		for k, v := range page.Dict {
			if k == "Annots" {
				continue
			}
			newDict[k] = v
		}
		alloc.AllocAt(page.Ref.ObjectNum, newDict)
	}

	return nil
}

// addXObjectToPage adds a Form XObject reference to a page's /Resources/XObject dict.
func addXObjectToPage(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, page *document.Page, name string, ref ast.PdfIndirectRef) {
	xobjectDict := ast.PdfDict{}
	if resObj, ok := page.Resources["XObject"]; ok {
		if existing, err := document.ResolveDict(ctx, doc, resObj); err == nil && existing != nil {
			maps.Copy(xobjectDict, existing)
		}
	}
	xobjectDict[ast.PdfName(name)] = ref

	newResources := make(ast.PdfDict, len(page.Resources)+1)
	maps.Copy(newResources, page.Resources)
	newResources["XObject"] = xobjectDict

	newDict := make(ast.PdfDict, len(page.Dict)+1)
	maps.Copy(newDict, page.Dict)
	newDict["Resources"] = newResources
	alloc.AllocAt(page.Ref.ObjectNum, newDict)
}

// appendToPageContents appends content stream operations to a page.
func appendToPageContents(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, page *document.Page, ops []string) {
	// Build new content stream with the stamp operations.
	var content strings.Builder
	for _, op := range ops {
		content.WriteString(op)
	}
	stampStream := ast.NewStream(ast.PdfDict{}, []byte(content.String()))
	stampRef := alloc.Alloc(stampStream)

	// Wrap existing /Contents + new stream in an array.
	newDict := make(ast.PdfDict, len(page.Dict)+1)
	maps.Copy(newDict, page.Dict)

	if existing, ok := page.Dict["Contents"]; ok {
		newDict["Contents"] = ast.PdfArray{existing, stampRef}
	} else {
		newDict["Contents"] = stampRef
	}
	alloc.AllocAt(page.Ref.ObjectNum, newDict)
}

// resolveFromAllocOrDoc checks the allocator for a pending version of the
// object, falling back to the document. Returns nil if not found.
func resolveFromAllocOrDoc(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, ref ast.PdfIndirectRef) ast.PdfDict {
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

func formatFloat(f float64) string {
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

// applyStampText overlays text on the specified pages using a standard font.
func applyStampText(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, r *Reader, stamp TextStamp, pages []int) error {
	resolvedPages, err := r.resolvePages(ctx)
	if err != nil {
		return fmt.Errorf("resolving pages: %w", err)
	}

	// If no pages specified, stamp all.
	if len(pages) == 0 {
		pages = make([]int, len(resolvedPages))
		for i := range resolvedPages {
			pages[i] = i + 1
		}
	}

	// Create a Helvetica font resource (standard Type1, always available).
	fontDict := ast.PdfDict{
		"Type":     ast.PdfName("Font"),
		"Subtype":  ast.PdfName("Type1"),
		"BaseFont": ast.PdfName("Helvetica"),
	}
	fontRef := alloc.Alloc(fontDict)

	// Escape parentheses in the text for PDF string literal.
	escaped := strings.ReplaceAll(stamp.Text, "\\", "\\\\")
	escaped = strings.ReplaceAll(escaped, "(", "\\(")
	escaped = strings.ReplaceAll(escaped, ")", "\\)")

	fontSize := stamp.FontSize
	if fontSize == 0 {
		fontSize = 12
	}

	// Build the content stream for the stamp.
	var ops strings.Builder
	ops.WriteString("\nq ")
	fmt.Fprintf(&ops, "BT /StampF1 %s Tf %s %s Td (%s) Tj ET ",
		formatFloat(fontSize), formatFloat(stamp.X), formatFloat(stamp.Y), escaped)
	ops.WriteString("Q")

	stampStream := ast.NewStream(ast.PdfDict{}, []byte(ops.String()))
	stampRef := alloc.Alloc(stampStream)

	for _, pageNum := range pages {
		if pageNum < 1 || pageNum > len(resolvedPages) {
			continue
		}
		page := resolvedPages[pageNum-1]

		// Add font to page resources.
		resources := make(ast.PdfDict, len(page.Resources)+1)
		maps.Copy(resources, page.Resources)
		fontResDict := ast.PdfDict{}
		if existingFonts, ok := page.Resources["Font"]; ok {
			if fd, err := document.ResolveDict(ctx, doc, existingFonts); err == nil && fd != nil {
				maps.Copy(fontResDict, fd)
			}
		}
		fontResDict["StampF1"] = fontRef
		resources["Font"] = fontResDict

		// Append stamp stream to page contents.
		newDict := make(ast.PdfDict, len(page.Dict)+1)
		maps.Copy(newDict, page.Dict)
		newDict["Resources"] = resources
		if existing, ok := page.Dict["Contents"]; ok {
			newDict["Contents"] = ast.PdfArray{existing, stampRef}
		} else {
			newDict["Contents"] = stampRef
		}
		alloc.AllocAt(page.Ref.ObjectNum, newDict)
	}

	return nil
}

// applyAddAttachment embeds a file in the document's /Names/EmbeddedFiles tree.
func applyAddAttachment(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, name string, data []byte, mimeType string) error {
	// Create the embedded file stream.
	streamDict := ast.PdfDict{
		"Type":   ast.PdfName("EmbeddedFile"),
		"Params": ast.PdfDict{"Size": ast.PdfInt(len(data))},
	}
	if mimeType != "" {
		streamDict["Subtype"] = ast.PdfName(mimeType)
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
	filespecRef := alloc.Alloc(filespecDict)

	// Update the catalog's /Names/EmbeddedFiles name tree.
	catalogRef, ok := doc.Index.Trailer["Root"].(ast.PdfIndirectRef)
	if !ok {
		return fmt.Errorf("trailer missing /Root")
	}

	// Check allocator first (another mutation may have already modified the catalog).
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return fmt.Errorf("resolving catalog")
	}

	// Get or create /Names dict. Check allocator for previously modified versions.
	var namesDict ast.PdfDict
	if namesObj, ok := catalogDict["Names"]; ok {
		if namesRef, ok := namesObj.(ast.PdfIndirectRef); ok {
			if d := resolveFromAllocOrDoc(ctx, alloc, doc, namesRef); d != nil {
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
			efDict = resolveFromAllocOrDoc(ctx, alloc, doc, efRef)
		}
		if efDict == nil {
			efDict, _ = document.ResolveDict(ctx, doc, efObj)
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
	newCatalog := make(ast.PdfDict, len(catalogDict)+1)
	maps.Copy(newCatalog, catalogDict)
	newCatalog["Names"] = namesRef
	alloc.AllocAt(catalogRef.ObjectNum, newCatalog)

	return nil
}
