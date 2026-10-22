package mutations

import (
	"context"
	"fmt"
	"log/slog"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// Signature flags of the interactive form (ISO 32000-1, 12.7.2, Table 219):
// bit 1 is SignaturesExist and bit 2 AppendOnly.
const (
	sigFlagSignaturesExist = 1 << 0
	sigFlagAppendOnly      = 1 << 1
)

// signaturePermsKeys are the catalog /Perms entries whose value is a signature
// dictionary (ISO 32000-1, 12.8.4, Table 258), in the order they are read.
//
//nolint:gochecknoglobals // static spec table
var signaturePermsKeys = []ast.PdfName{"DocMDP", "UR3"}

// SignedSignature names one signature of a document.
type SignedSignature struct {
	// Field is the fully qualified name of the signed signature field that
	// holds the signature (ISO 32000-1, 12.7.3.2), "" when no field of the
	// form or of a page holds it.
	Field string
	// Perms is the catalog /Perms entry that names the signature, "DocMDP" or
	// "UR3" (ISO 32000-1, 12.8.4, Table 258), and "" when none does.
	Perms string
}

// SignedSignatures lists the signatures of doc: each signed signature field its
// interactive form reaches, in form order, then each one only a page's widget
// presents, in page order, then each signature a catalog /Perms entry names
// that no such field holds. A field /Perms names carries that entry's name.
//
// A signature field is signed when its /V is a signature dictionary (ISO
// 32000-1, 12.7.4.5, Table 232). /FT and /V are inheritable (Table 220), so a
// field states them or takes them from an ancestor. A usage rights signature
// sits in /Perms /UR3 with no field (12.8.2.3).
//
// It reads doc as the file holds it, so it lists the signatures a full rewrite
// of doc drops (see [BakeSignaturesForRewrite]) whatever the edits before it
// removed.
func SignedSignatures(ctx context.Context, doc *document.Document) []SignedSignature {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil || catalog == nil {
		return nil
	}
	found := signatureInventory(ctx, nil, doc, catalog.Dict, readerPages(ctx, doc, catalog))
	return found.list()
}

// BakeSignaturesForRewrite returns the copier overrides, in doc's object
// numbers, under which a full rewrite of doc carries none of its signatures,
// drawing each signed signature widget's appearance into its page. overrides
// are the edits the rewrite applies already: the result holds them too, and is
// overrides itself when the document as they stage it holds no signature.
//
// A signature's digest covers the bytes its /ByteRange names (ISO 32000-1,
// 12.8.1), and a full rewrite writes every object at a new offset, so a
// signature copied into it can never verify: poppler's pdfsig reports it
// invalid, or not verified at all. The rule [bakeSignedSignatures] applies to a
// split, a merge and a page insertion applies here:
//
//   - Each signed signature widget a page presents is drawn into the page as
//     [FlattenForm] draws one, and removed, so the page looks as it did. A
//     widget a viewer does not display, one with no area and one with no
//     appearance leave without drawing. In a tagged document the drawing is
//     tagged under the structure element that enclosed the widget, or marked
//     as an artifact when none did (see [flattenTags]), since the rewrite
//     carries the document's structure.
//   - Each signed signature field leaves the form, and a field left with no
//     kid goes with it. An unsigned signature field stays, to be signed.
//   - The form's /SigFlags no longer claims AppendOnly, since no signature is
//     left for a rewrite to invalidate, nor SignaturesExist when no signature
//     field stays (ISO 32000-1, Table 219).
//   - The catalog /Perms loses its /DocMDP and /UR3 entries, the signature
//     dictionaries of a certification and of usage rights (ISO 32000-1,
//     12.8.4, Table 258): each names a signature the rewrite invalidates.
//   - The catalog /DSS goes (see [dropSecurityStore]), also when the edits
//     before took every signature out: the store validates the signatures
//     the file holds, which the rewrite leaves out.
//
// An incremental update keeps every signature, which it appends after, so
// only a full rewrite calls it.
//
// # Corpus evidence
//
//   - All corpora: 61 of the 5202 files litho opens hold a signature, 37 a
//     usage rights one and 10 a certification. A page deletion or a full
//     rewrite with no edit of 28 of them copied a signed field whose
//     /ByteRange no longer covered the file, and of none now. qpdf
//     reports nothing new on the 244 outputs of those two writes and of the
//     PDF/A-2b and PDF/UA-1 remediations of each.
//   - All corpora: the PDF/A-2b remediation of 12 of the 61 is a full rewrite
//     that failed ISO 19005-2, 6.4.3 test 1, and none does now: 4 more come
//     out conforming, and veraPDF 1.30.2 reports the rules litho reports on
//     all 12 outputs.
//   - pdfjs xfa_filled_imm1344e.pdf, xfa_issue13855.pdf, xfa_issue14071.pdf:
//     each nests its signed field below a field that is not a signature
//     field, which only the inherited /FT and /V reach.
func BakeSignaturesForRewrite(ctx context.Context, doc *document.Document, overrides map[int]ast.PdfObject) (map[int]ast.PdfObject, error) {
	catalogRef, ok := CatalogRef(ctx, doc)
	if !ok {
		return overrides, nil
	}
	sourceCatalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil || sourceCatalog == nil {
		doc.Logger.Debug("rewrite: the catalog does not resolve, leaving its signatures as they are", slog.Any("err", err))
		return overrides, nil
	}
	reader := readerPages(ctx, doc, sourceCatalog)

	// A probe first, so a document with no signature is written exactly as
	// before: numbering the drawings reads the highest object number, which
	// runs the raw-byte augmentation of the cross-reference table.
	probe := writer.NewObjAllocator(0)
	for num, obj := range overrides {
		probe.AllocAt(num, obj)
	}
	probeCatalog, err := LiveCatalogDict(ctx, probe, doc, catalogRef)
	if err != nil {
		doc.Logger.Debug("rewrite: the staged catalog does not resolve, leaving its signatures as they are", slog.Any("err", err))
		return overrides, nil
	}
	if signatureInventory(ctx, probe, doc, probeCatalog, StagedPages(ctx, probe, doc, reader)).empty() {
		// The edits took every signature out, or the file holds none. The
		// store goes in the first case alone: in a file with no signature it
		// validates none the rewrite drops.
		if _, has := probeCatalog["DSS"]; !has || signatureInventory(ctx, nil, doc, sourceCatalog.Dict, reader).empty() {
			return overrides, nil
		}
		updated := maps.Clone(probeCatalog)
		dropSecurityStore(doc, updated)
		probe.AllocAt(catalogRef.ObjectNum, updated)
		return probe.Pending(), nil
	}

	alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))
	// A done ctx can stop the raw-byte augmentation short, and the allocator
	// then starts below objects the file defines.
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("drawing the signatures into their pages: %w", err)
	}
	for num, obj := range overrides {
		alloc.AllocAt(num, obj)
	}
	catalog, err := LiveCatalogDict(ctx, alloc, doc, catalogRef)
	if err != nil {
		return nil, fmt.Errorf("resolving the catalog whose signatures the rewrite drops: %w", err)
	}
	tags := newFlattenTags(ctx, alloc, doc, catalogRef, catalog)
	for _, page := range StagedPages(ctx, alloc, doc, reader) {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("drawing the signatures into their pages: %w", err)
		}
		// An inlined page has no object number to write the drawing at.
		if page.Ref.ObjectNum == 0 {
			continue
		}
		flattenPageWidgets(ctx, doc, alloc, page, tags, func(annotObj ast.PdfObject, annotDict ast.PdfDict) (widgetOutcome, ast.PdfStream) {
			if _, signed := widgetSignedField(ctx, alloc, doc, annotObj, annotDict); !signed {
				return widgetKept, ast.PdfStream{}
			}
			return signedWidgetOutcome(ctx, doc, alloc, annotDict)
		})
	}
	if err := tags.commit(ctx, alloc, doc); err != nil {
		return nil, fmt.Errorf("tagging the drawn signatures: %w", err)
	}

	catalog, err = LiveCatalogDict(ctx, alloc, doc, catalogRef)
	if err != nil {
		return nil, fmt.Errorf("rewriting the catalog's signatures: %w", err)
	}
	updated := maps.Clone(catalog)
	formChanged := dropSignedFields(ctx, alloc, doc, updated)
	permsChanged := dropSignaturePerms(ctx, alloc, doc, updated)
	storeChanged := dropSecurityStore(doc, updated)
	if formChanged || permsChanged || storeChanged {
		alloc.AllocAt(catalogRef.ObjectNum, updated)
	}
	return alloc.Pending(), nil
}

// dropSecurityStore removes the document security store from catalog, as
// [BakeSignaturesForRewrite] describes, and reports whether catalog held one.
//
// Its certificates, OCSP responses and CRLs, and the /VRI entry each keyed by
// the uppercase hex SHA-1 of one signature's /Contents, are the validation data
// of the document's signatures (ISO 32000-2, 12.8.4.3; ETSI EN 319 142-1,
// 5.4.2). A rewrite keeps none of its source's signatures, so the store
// validates nothing the output holds, and goes whole: no signature stays for
// its shared arrays to serve, nor a /VRI entry to keep.
//
// # Corpus evidence
//
//   - pdfjs issue14269.pdf, issue8047.pdf: the only 2 of the 5217 files that
//     carry a /DSS, each with 2 signatures and 1 /VRI entry. A full rewrite
//     copied the store, and drops it now.
func dropSecurityStore(doc *document.Document, catalog ast.PdfDict) bool {
	if _, has := catalog["DSS"]; !has {
		return false
	}
	delete(catalog, "DSS")
	doc.Logger.Debug("rewrite: removing /DSS, the validation data of the signatures the rewrite drops")
	return true
}

// readerPages returns doc's pages as the file holds them, none when its page
// tree does not resolve.
func readerPages(ctx context.Context, doc *document.Document, catalog *document.Catalog) []*document.Page {
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		doc.Logger.Debug("signatures: resolving the pages failed", slog.Any("err", err))
		return nil
	}
	return pages
}

// dropSignedFields removes the signed signature fields from the form catalog
// names, and settles its /SigFlags, as [BakeSignaturesForRewrite] describes.
// A form written directly into catalog is updated there; it reports whether
// catalog changed.
func dropSignedFields(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, catalog ast.PdfDict) bool {
	formObj, has := catalog["AcroForm"]
	if !has {
		return false
	}
	form, err := resolveDictAllocFirst(ctx, alloc, doc, formObj)
	if err != nil || form == nil {
		return false
	}
	walk := newSignedFieldWalk(ctx, alloc, doc, true)
	fields, err := resolveArrayAllocFirst(ctx, alloc, doc, form["Fields"])
	if err != nil {
		fields = nil
	}
	kept, fieldsChanged := walk.list(fields, "", "", nil, 0)

	updated := maps.Clone(form)
	if fieldsChanged {
		updated["Fields"] = kept
	}
	flags, hasFlags := intEntry(ctx, doc, form["SigFlags"])
	newFlags := flags &^ sigFlagAppendOnly
	if !walk.keptSignatureField {
		newFlags &^= sigFlagSignaturesExist
	}
	flagsChanged := hasFlags && newFlags != flags
	switch {
	case flagsChanged && newFlags == 0:
		delete(updated, "SigFlags")
	case flagsChanged:
		updated["SigFlags"] = ast.PdfInt(newFlags)
	}
	if !fieldsChanged && !flagsChanged {
		return false
	}
	if ref, isRef := formObj.(ast.PdfIndirectRef); isRef && alloc.AllocAt(ref.ObjectNum, updated) {
		return false
	}
	catalog["AcroForm"] = updated
	return true
}

// dropSignaturePerms removes from the catalog's /Perms each entry naming a
// signature dictionary, as [BakeSignaturesForRewrite] describes, and /Perms
// itself when nothing is left in it. It reports whether catalog changed.
func dropSignaturePerms(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, catalog ast.PdfDict) bool {
	permsObj, has := catalog["Perms"]
	if !has {
		return false
	}
	perms, err := resolveDictAllocFirst(ctx, alloc, doc, permsObj)
	if err != nil || perms == nil {
		return false
	}
	updated := maps.Clone(perms)
	for _, key := range signaturePermsKeys {
		if sig, err := resolveDictAllocFirst(ctx, alloc, doc, perms[key]); err == nil && sig != nil {
			delete(updated, key)
		}
	}
	if len(updated) == len(perms) {
		return false
	}
	if len(updated) == 0 {
		delete(catalog, "Perms")
		return true
	}
	if ref, isRef := permsObj.(ast.PdfIndirectRef); isRef && alloc.AllocAt(ref.ObjectNum, updated) {
		return false
	}
	catalog["Perms"] = updated
	return true
}

// signedField is a signed signature field: the terminal field holding the
// signature, its fully qualified name, and its /V.
type signedField struct {
	name string
	// ref is the field's object, zero for a field written directly.
	ref ast.PdfIndirectRef
	// value is the signature dictionary's object, zero for one written
	// directly.
	value ast.PdfIndirectRef
}

// signatures is what [signatureInventory] finds.
type signatures struct {
	// fields are the signed signature fields the form reaches, then those
	// only a page's widget presents.
	fields []signedField
	// perms maps each /Perms entry naming a signature dictionary to that
	// dictionary's object, zero for one written directly.
	perms map[ast.PdfName]ast.PdfIndirectRef
}

// empty reports whether nothing was found.
func (s signatures) empty() bool {
	return len(s.fields) == 0 && len(s.perms) == 0
}

// list returns what was found as [SignedSignature] values, in the order
// [SignedSignatures] documents.
func (s signatures) list() []SignedSignature {
	out := make([]SignedSignature, 0, len(s.fields)+len(s.perms))
	for _, f := range s.fields {
		out = append(out, SignedSignature{Field: f.name})
	}
	for _, key := range signaturePermsKeys {
		value, named := s.perms[key]
		if !named {
			continue
		}
		held := false
		for i, f := range s.fields {
			if value.ObjectNum != 0 && f.value.ObjectNum == value.ObjectNum && out[i].Perms == "" {
				out[i].Perms = string(key)
				held = true
				break
			}
		}
		if !held {
			out = append(out, SignedSignature{Perms: string(key)})
		}
	}
	return out
}

// signatureInventory finds the signatures of the document catalog heads, the
// objects read from alloc first when it is not nil: the signed signature
// fields its form reaches, those the widgets on pages present, and the
// signature dictionaries its /Perms names.
func signatureInventory(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, catalog ast.PdfDict, pages []*document.Page) signatures {
	var found signatures
	seen := make(map[int]bool)
	if form, err := resolveDictAllocFirst(ctx, alloc, doc, catalog["AcroForm"]); err == nil && form != nil {
		walk := newSignedFieldWalk(ctx, alloc, doc, false)
		if fields, err := resolveArrayAllocFirst(ctx, alloc, doc, form["Fields"]); err == nil {
			walk.list(fields, "", "", nil, 0)
		}
		for _, f := range walk.found {
			if f.ref.ObjectNum != 0 {
				seen[f.ref.ObjectNum] = true
			}
		}
		found.fields = walk.found
	}
	for _, page := range pages {
		if ctx.Err() != nil {
			break
		}
		pageDict := page.Dict
		if alloc != nil {
			pageDict = latestPageDict(alloc, page)
		}
		annots, err := resolveArrayAllocFirst(ctx, alloc, doc, pageDict["Annots"])
		if err != nil {
			continue
		}
		for _, annotObj := range annots {
			annotDict, err := resolveDictAllocFirst(ctx, alloc, doc, annotObj)
			if err != nil || annotDict == nil {
				continue
			}
			f, signed := widgetSignedField(ctx, alloc, doc, annotObj, annotDict)
			if !signed || (f.ref.ObjectNum != 0 && seen[f.ref.ObjectNum]) {
				continue
			}
			if f.ref.ObjectNum != 0 {
				seen[f.ref.ObjectNum] = true
			}
			found.fields = append(found.fields, f)
		}
	}
	if perms, err := resolveDictAllocFirst(ctx, alloc, doc, catalog["Perms"]); err == nil && perms != nil {
		for _, key := range signaturePermsKeys {
			sig, err := resolveDictAllocFirst(ctx, alloc, doc, perms[key])
			if err != nil || sig == nil {
				continue
			}
			if found.perms == nil {
				found.perms = make(map[ast.PdfName]ast.PdfIndirectRef)
			}
			ref, _ := perms[key].(ast.PdfIndirectRef)
			found.perms[key] = ref
		}
	}
	return found
}

// signedFieldWalk descends an interactive form's field tree (ISO 32000-1,
// 12.7.3.1), reading objects from alloc first when it is not nil, and finds
// the signed signature fields in it. With rewrite set it also takes them out:
// each non-terminal field that loses a kid is written back to alloc, or handed
// to its parent when it is written directly, and one left with no kid goes as
// well.
//
// Each indirect field is visited once, which ends a cycle and bounds the walk
// by the objects the file holds. A /Kids array written as an object of its own
// is walked once too, and its result reused wherever it is reached again: the
// direct fields it holds can each name it again, and walked anew wherever it
// is reached it would fan out 2^depth. The descent is bounded by
// [maxFieldParentDepth], borrowed from the /Parent ascent as pruneFieldKidsAt
// borrows it, for the stack's sake.
type signedFieldWalk struct {
	ctx     context.Context
	alloc   *writer.ObjAllocator
	doc     *document.Document
	rewrite bool
	visited map[int]bool
	// kids holds, per /Kids array written as an object of its own, the fields
	// its walk kept and whether any went, and a nil entry while that walk is
	// under way (see [signedFieldWalk.kidsList]).
	kids map[int]*walkedKids
	// found lists each signed signature field reached, in walk order.
	found []signedField
	// keptSignatureField is set when a signature field that is not signed
	// stays in the form.
	keptSignatureField bool
}

func newSignedFieldWalk(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, rewrite bool) *signedFieldWalk {
	return &signedFieldWalk{ctx: ctx, alloc: alloc, doc: doc, rewrite: rewrite, visited: make(map[int]bool), kids: make(map[int]*walkedKids)}
}

// walkedKids is the result of walking one /Kids array: the fields it keeps,
// and whether any went.
type walkedKids struct {
	kept    ast.PdfArray
	changed bool
}

// kidsList walks kids, the /Kids value kidsObj, as [signedFieldWalk.list]
// does. An array written as an object of its own is walked once, and a parent
// that reaches it again takes the same result, so the field it lists goes from
// every parent that names it. One reached again while its walk is under way, a
// cycle back to it, is left as it is.
func (w *signedFieldWalk) kidsList(kidsObj ast.PdfObject, kids ast.PdfArray, parentName string, ft ast.PdfName, v ast.PdfObject, depth int) (ast.PdfArray, bool) {
	ref, isRef := kidsObj.(ast.PdfIndirectRef)
	if !isRef {
		return w.list(kids, parentName, ft, v, depth)
	}
	if done, ok := w.kids[ref.ObjectNum]; ok {
		if done == nil {
			return kids, false
		}
		return done.kept, done.changed
	}
	w.kids[ref.ObjectNum] = nil
	kept, changed := w.list(kids, parentName, ft, v, depth)
	w.kids[ref.ObjectNum] = &walkedKids{kept: kept, changed: changed}
	return kept, changed
}

// list walks the fields of list, the form's /Fields or a field's /Kids, under
// a parent whose fully qualified name is parentName and whose inheritable /FT
// and /V are ft and v. It returns list without the signed signature fields, and
// whether any went.
func (w *signedFieldWalk) list(list ast.PdfArray, parentName string, ft ast.PdfName, v ast.PdfObject, depth int) (ast.PdfArray, bool) {
	out := make(ast.PdfArray, 0, len(list))
	changed := false
	for _, obj := range list {
		keep, replacement := w.field(obj, parentName, ft, v, depth)
		switch {
		case !keep:
			changed = true
		case replacement != nil:
			out = append(out, replacement)
			changed = true
		default:
			out = append(out, obj)
		}
	}
	return out, changed
}

// field walks one field. It returns whether the field stays and, for a field
// written directly whose kids changed, the dictionary that replaces it.
func (w *signedFieldWalk) field(obj ast.PdfObject, parentName string, ft ast.PdfName, v ast.PdfObject, depth int) (bool, ast.PdfObject) {
	if depth > maxFieldParentDepth || w.ctx.Err() != nil {
		return true, nil
	}
	ref, isRef := obj.(ast.PdfIndirectRef)
	if isRef {
		if w.visited[ref.ObjectNum] {
			return true, nil
		}
		w.visited[ref.ObjectNum] = true
	}
	dict, err := resolveDictAllocFirst(w.ctx, w.alloc, w.doc, obj)
	if err != nil || dict == nil {
		return true, nil
	}
	if own, err := document.ResolveName(w.ctx, w.doc, dict["FT"]); err == nil && own != "" {
		ft = own
	}
	if own, has := dict["V"]; has {
		v = own
	}
	name := qualifiedFieldName(w.ctx, w.doc, parentName, dict)

	kids, err := resolveArrayAllocFirst(w.ctx, w.alloc, w.doc, dict["Kids"])
	if err != nil || !w.hasFieldKid(kids) {
		// A terminal field: its kids, if any, are its widgets (ISO 32000-1,
		// 12.7.3.1).
		if ft != "Sig" {
			return true, nil
		}
		if !w.isDict(v) {
			w.keptSignatureField = true
			return true, nil
		}
		f := signedField{name: name, ref: ref}
		f.value, _ = v.(ast.PdfIndirectRef)
		w.found = append(w.found, f)
		return false, nil
	}

	remaining, changed := w.kidsList(dict["Kids"], kids, name, ft, v, depth+1)
	if !changed || !w.rewrite {
		return true, nil
	}
	if len(remaining) == 0 {
		return false, nil
	}
	updated := maps.Clone(dict)
	updated["Kids"] = remaining
	if isRef && w.alloc.AllocAt(ref.ObjectNum, updated) {
		return true, nil
	}
	return true, updated
}

// hasFieldKid reports whether kids holds a field rather than widgets alone. A
// widget annotation is /Subtype /Widget with no partial name of its own; a kid
// with one is a field, merged with its widget or not (ISO 32000-1, 12.7.3.1
// and 12.5.6.19).
func (w *signedFieldWalk) hasFieldKid(kids ast.PdfArray) bool {
	for _, kid := range kids {
		dict, err := resolveDictAllocFirst(w.ctx, w.alloc, w.doc, kid)
		if err != nil || dict == nil {
			continue
		}
		subtype, _ := document.ResolveName(w.ctx, w.doc, dict["Subtype"])
		_, named := dict["T"]
		if subtype != "Widget" || named {
			return true
		}
	}
	return false
}

// isDict reports whether v resolves to a dictionary, the signature
// dictionary of a signed field.
func (w *signedFieldWalk) isDict(v ast.PdfObject) bool {
	if v == nil {
		return false
	}
	dict, err := resolveDictAllocFirst(w.ctx, w.alloc, w.doc, v)
	return err == nil && dict != nil
}

// qualifiedFieldName returns the fully qualified name of field under a parent
// named parentName: the two partial names joined by a period, or the one there
// is when the other is absent (ISO 32000-1, 12.7.3.2).
func qualifiedFieldName(ctx context.Context, doc *document.Document, parentName string, field ast.PdfDict) string {
	partial := ""
	if s, err := document.ResolveString(ctx, doc, field["T"]); err == nil && s != "" {
		partial = document.DecodePdfString(s)
	}
	switch {
	case partial == "":
		return parentName
	case parentName == "":
		return partial
	}
	return parentName + "." + partial
}

// widgetSignedField returns the signed signature field a widget presents, and
// false when the annotation is not a widget of a signed signature field.
//
// The widget's terminal field is the widget itself when it carries a partial
// name or has no /Parent, merged with its field, and its /Parent otherwise
// (ISO 32000-1, 12.5.6.19). /FT and /V are read from the widget up the /Parent
// chain, the nearest statement of each winning (12.7.3.1), and the field is
// signed when /FT is /Sig and /V a dictionary. Objects are read from alloc
// first when it is not nil.
func widgetSignedField(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, annotObj ast.PdfObject, annotDict ast.PdfDict) (signedField, bool) {
	if subtype, _ := document.ResolveName(ctx, doc, annotDict["Subtype"]); subtype != "Widget" {
		return signedField{}, false
	}
	chain := []ast.PdfDict{annotDict}
	refs := []ast.PdfObject{annotObj}
	visited := make(map[int]bool)
	if ref, isRef := annotObj.(ast.PdfIndirectRef); isRef {
		visited[ref.ObjectNum] = true
	}
	for depth := 0; depth < maxFieldParentDepth; depth++ {
		parentObj, has := chain[len(chain)-1]["Parent"]
		if !has {
			break
		}
		if ref, isRef := parentObj.(ast.PdfIndirectRef); isRef {
			if visited[ref.ObjectNum] {
				break
			}
			visited[ref.ObjectNum] = true
		}
		parent, err := resolveDictAllocFirst(ctx, alloc, doc, parentObj)
		if err != nil || parent == nil {
			break
		}
		chain = append(chain, parent)
		refs = append(refs, parentObj)
	}

	var (
		ft ast.PdfName
		v  ast.PdfObject
	)
	for _, dict := range chain {
		if ft == "" {
			if own, err := document.ResolveName(ctx, doc, dict["FT"]); err == nil && own != "" {
				ft = own
			}
		}
		if v == nil {
			v = dict["V"]
		}
	}
	if ft != "Sig" || v == nil {
		return signedField{}, false
	}
	if sig, err := resolveDictAllocFirst(ctx, alloc, doc, v); err != nil || sig == nil {
		return signedField{}, false
	}

	terminal := 0
	if _, named := annotDict["T"]; !named && len(chain) > 1 {
		terminal = 1
	}
	name := ""
	for i := len(chain) - 1; i >= terminal; i-- {
		name = qualifiedFieldName(ctx, doc, name, chain[i])
	}
	f := signedField{name: name}
	f.ref, _ = refs[terminal].(ast.PdfIndirectRef)
	f.value, _ = v.(ast.PdfIndirectRef)
	return f, true
}

// signedWidgetOutcome decides what becomes of a signed signature widget whose
// field leaves the document: its appearance is drawn into the page, or it
// leaves without drawing when a viewer shows nothing of it.
//
// A widget the viewer does not display (Hidden or NoView, ISO 32000-1, Table
// 165), one with no area, as an invisible signature has, and one with no
// appearance stream draw nothing. A viewer scales an appearance onto a /Rect
// with no area to nothing (ISO 32000-1, 12.5.5, Algorithm 8.1), so an
// invisible signature shows nothing.
func signedWidgetOutcome(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, annotDict ast.PdfDict) (widgetOutcome, ast.PdfStream) {
	if annotationHiddenOrNoView(ctx, doc, annotDict) {
		return widgetDropped, ast.PdfStream{}
	}
	if rect, ok := widgetRect(ctx, doc, annotDict); !ok || rect[0] == rect[2] || rect[1] == rect[3] {
		return widgetDropped, ast.PdfStream{}
	}
	appearance, ok := resolveWidgetAppearance(ctx, doc, alloc, annotDict)
	if !ok {
		return widgetDropped, ast.PdfStream{}
	}
	return widgetBaked, appearance
}
