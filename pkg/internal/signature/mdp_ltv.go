package signature

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// ltvObjects holds the object numbers a document spends on long-term
// validation: its document security store and its document timestamp fields.
type ltvObjects struct {
	all     map[int]bool // every object below
	fields  map[int]bool // document timestamp signature fields
	widgets map[int]bool // the widget annotations of those fields
}

// collectLTV returns the objects doc holds for its document security store
// and its document timestamps.
//
// ISO 32000-2, 12.8.2.2, Table 257 says of /P that "changes to a PDF that are
// incremental updates which include only the data necessary to add DSS's
// 12.8.4.3, "Document Security Store (DSS)" and/or document timestamps 12.8.5,
// "Document timestamp (DTS) dictionary" to the document shall not be
// considered as changes to the document", whatever the permission. ETSI EN
// 319 142-1 states the same of the DSS in 5.4.2.3 and of the document
// timestamp in 5.4.3. Only the objects those structures consist of are
// collected, so nothing else an update writes can pass as validation data.
func collectLTV(ctx context.Context, doc *document.Document) (*ltvObjects, error) {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return nil, err
	}
	l := &ltvObjects{all: make(map[int]bool), fields: make(map[int]bool), widgets: make(map[int]bool)}
	l.addSecurityStore(ctx, doc, catalog.Dict["DSS"])
	l.addTimestampFields(ctx, doc, catalog)
	return l, ctx.Err()
}

// addRef records obj when it is a reference.
func (l *ltvObjects) addRef(obj ast.PdfObject) {
	if ref, ok := obj.(ast.PdfIndirectRef); ok {
		l.all[ref.ObjectNum] = true
	}
}

// addSecurityStore records the DSS dictionary and the objects its entries
// name, and nothing further: the /Certs, /OCSPs and /CRLs arrays and their
// streams, the /VRI dictionary, each signature VRI dictionary, its /Cert, /CRL
// and /OCSP arrays and streams, and its /TS stream (ETSI EN 319 142-1, 5.4.2.2
// and 5.4.2.3; ISO 32000-2, 12.8.4.3).
func (l *ltvObjects) addSecurityStore(ctx context.Context, doc *document.Document, dssObj ast.PdfObject) {
	if dssObj == nil {
		return
	}
	l.addRef(dssObj)
	dss, err := document.ResolveDict(ctx, doc, dssObj)
	if err != nil || dss == nil {
		// An unreadable DSS holds nothing to record, and whatever an update
		// wrote for it is then classified as any other object.
		doc.Logger.Debug("resolving /DSS", slog.Any("err", err))
		return
	}
	for _, key := range []ast.PdfName{"Certs", "OCSPs", "CRLs"} {
		l.addStreamArray(ctx, doc, dss[key])
	}
	vriObj, ok := dss["VRI"]
	if !ok {
		return
	}
	l.addRef(vriObj)
	vri, err := document.ResolveDict(ctx, doc, vriObj)
	if err != nil {
		doc.Logger.Debug("resolving /DSS /VRI", slog.Any("err", err))
		return
	}
	i := 0
	for _, entryObj := range vri {
		if i%100 == 0 && ctx.Err() != nil {
			return
		}
		i++
		l.addRef(entryObj)
		entry, err := document.ResolveDict(ctx, doc, entryObj)
		if err != nil || entry == nil {
			continue
		}
		for _, key := range []ast.PdfName{"Cert", "CRL", "OCSP"} {
			l.addStreamArray(ctx, doc, entry[key])
		}
		l.addRef(entry["TS"])
	}
}

// addStreamArray records an array of stream references and the array itself.
func (l *ltvObjects) addStreamArray(ctx context.Context, doc *document.Document, obj ast.PdfObject) {
	if obj == nil {
		return
	}
	l.addRef(obj)
	arr, err := document.ResolveArray(ctx, doc, obj)
	if err != nil {
		doc.Logger.Debug("resolving a DSS stream array", slog.Any("err", err))
		return
	}
	for _, entry := range arr {
		l.addRef(entry)
	}
}

// addTimestampFields records every document timestamp field the form lists,
// and those only the page carries, as [ExtractSignatures] finds signatures in
// both places.
func (l *ltvObjects) addTimestampFields(ctx context.Context, doc *document.Document, catalog *document.Catalog) {
	visited := make(map[int]bool)
	acroForm, err := document.ResolveDict(ctx, doc, catalog.Dict["AcroForm"])
	if err != nil {
		doc.Logger.Debug("resolving /AcroForm for document timestamps", slog.Any("err", err))
	}
	if acroForm != nil {
		fields, err := document.ResolveArray(ctx, doc, acroForm["Fields"])
		if err != nil {
			doc.Logger.Debug("resolving /Fields for document timestamps", slog.Any("err", err))
		}
		l.addTimestampFieldTree(ctx, doc, fields, visited, 0)
	}

	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		doc.Logger.Debug("resolving pages for document timestamps", slog.Any("err", err))
		return
	}
	for _, page := range pages {
		if ctx.Err() != nil {
			return
		}
		annots, err := document.ResolveArray(ctx, doc, page.Dict["Annots"])
		if err != nil {
			doc.Logger.Debug("resolving /Annots for document timestamps", slog.Any("err", err))
			continue
		}
		l.addTimestampFieldTree(ctx, doc, annots, visited, 0)
	}
}

// addTimestampFieldTree walks fields and their /Kids, recording each document
// timestamp field it meets. visited keeps a node listed twice from being
// walked twice, and the depth is bounded by [maxFieldTreeDepth].
func (l *ltvObjects) addTimestampFieldTree(ctx context.Context, doc *document.Document, fields ast.PdfArray, visited map[int]bool, depth int) {
	if depth > maxFieldTreeDepth {
		return
	}
	for i, fieldObj := range fields {
		if i%100 == 0 && ctx.Err() != nil {
			return
		}
		ref, ok := fieldObj.(ast.PdfIndirectRef)
		if !ok || visited[ref.ObjectNum] {
			continue
		}
		visited[ref.ObjectNum] = true
		field, err := document.ResolveDict(ctx, doc, ref)
		if err != nil || field == nil {
			continue
		}
		if isTimestampField(ctx, doc, field) {
			l.addTimestampField(ctx, doc, ref.ObjectNum, field)
			continue
		}
		kids, err := document.ResolveArray(ctx, doc, field["Kids"])
		if err == nil && len(kids) > 0 {
			l.addTimestampFieldTree(ctx, doc, kids, visited, depth+1)
		}
	}
}

// isTimestampField reports whether field is a signature field whose value is
// a document timestamp dictionary, which ETSI EN 319 142-1, 5.4.3 and ISO
// 32000-2, 12.8.5 type /DocTimeStamp.
func isTimestampField(ctx context.Context, doc *document.Document, field ast.PdfDict) bool {
	ft, err := document.ResolveName(ctx, doc, field["FT"])
	if err != nil || ft != "Sig" {
		return false
	}
	value, err := document.ResolveDict(ctx, doc, field["V"])
	return err == nil && value != nil && dictName(value, "Type") == "DocTimeStamp"
}

// addTimestampField records a document timestamp field, its timestamp
// dictionary and its widgets: the field itself when field and widget are
// merged (ISO 32000-1, 12.5.6.19), and its /Kids otherwise. A field that
// carries actions is not recorded (see [hasActions]).
func (l *ltvObjects) addTimestampField(ctx context.Context, doc *document.Document, num int, field ast.PdfDict) {
	if hasActions(field) {
		return
	}
	l.fields[num] = true
	l.all[num] = true
	l.addRef(field["V"])
	if dictName(field, "Subtype") == "Widget" {
		l.addWidget(ctx, doc, num, field)
	}
	kids, err := document.ResolveArray(ctx, doc, field["Kids"])
	if err != nil {
		doc.Logger.Debug("resolving a document timestamp field's /Kids", slog.Any("err", err))
	}
	for _, kidObj := range kids {
		ref, ok := kidObj.(ast.PdfIndirectRef)
		if !ok {
			continue
		}
		kid, err := document.ResolveDict(ctx, doc, ref)
		if err != nil || kid == nil {
			continue
		}
		l.addWidget(ctx, doc, ref.ObjectNum, kid)
	}
}

// addWidget records a document timestamp widget and, when the widget shows
// nothing, its appearance streams.
//
// A widget whose /Rect has no area draws nothing, so its appearance is part of
// the field and nothing more. An appearance with an area draws on the page,
// which is more than the "data necessary to add" a timestamp that Table 257
// exempts, so it stays a change like any other. A widget that carries actions
// is not recorded (see [hasActions]).
func (l *ltvObjects) addWidget(ctx context.Context, doc *document.Document, num int, widget ast.PdfDict) {
	if hasActions(widget) {
		return
	}
	l.widgets[num] = true
	l.all[num] = true
	if !hasNoArea(ctx, doc, widget["Rect"]) {
		return
	}
	l.addRef(widget["AP"])
	ap, err := document.ResolveDict(ctx, doc, widget["AP"])
	if err != nil || ap == nil {
		return
	}
	for _, key := range []ast.PdfName{"N", "R", "D"} {
		appearance, ok := ap[key]
		if !ok {
			continue
		}
		l.addRef(appearance)
		resolved, err := document.ResolveObject(ctx, doc, appearance)
		if err != nil {
			continue
		}
		// ISO 32000-1, 12.5.5, Table 168: each entry is a stream or a
		// dictionary of streams keyed by appearance state.
		if states, ok := resolved.(ast.PdfDict); ok {
			for _, state := range states {
				l.addRef(state)
			}
		}
	}
}

// hasActions reports whether a field or widget dictionary carries an action
// (/A, ISO 32000-1, Table 188) or additional actions (/AA, Tables 188 and
// 220). A timestamp needs neither, and either runs when a reader opens or
// touches the page, which is more than the data Table 257 exempts.
func hasActions(dict ast.PdfDict) bool {
	_, a := dict["A"]
	_, aa := dict["AA"]
	return a || aa
}

// hasNoArea reports whether a /Rect is a rectangle of zero width or height.
// A missing or malformed rectangle reports false.
func hasNoArea(ctx context.Context, doc *document.Document, rectObj ast.PdfObject) bool {
	rect, err := document.ResolveArray(ctx, doc, rectObj)
	if err != nil || len(rect) != 4 {
		return false
	}
	var v [4]float64
	for i, n := range rect {
		f, ok := document.Number(n)
		if !ok {
			return false
		}
		v[i] = f
	}
	return v[0] == v[2] || v[1] == v[3]
}

// ltvExemptions returns the object numbers among objects that add validation
// data or document timestamps, and so are not changes (ISO 32000-2, 12.8.2.2,
// Table 257).
//
// An object of the current DSS or of a document timestamp field is exempt when
// it is new, or when it already served the same purpose in the signed
// revision rev. Anything else the signed revision held is not: listing the
// page's content stream in /Certs does not make rewriting it validation data.
// held reports whether the signed revision held a number, defining it or
// referring to it ([signedRevision.held]), so an update that defines a number
// the signed document refers to cannot pass it as new validation data.
//
// At P=1 the objects that must change to hold the new data are exempt too, and
// only when that is all that changed. Each is compared with its signed version:
// the catalog may gain /DSS, the /AcroForm updates below and the /ESIC or /ADBE
// extension that ETSI EN 319 142-1, 5.6 asks a writer to declare; the form may
// gain timestamp fields in /Fields and change /SigFlags; a page may gain
// timestamp widgets in /Annots. P=2 and P=3 already permit those rewrites.
func ltvExemptions(ctx context.Context, doc, rev *document.Document, held func(num int) bool, current *ltvObjects, objects []appendedObject, policy int) (map[int]bool, error) {
	exempt := make(map[int]bool)
	var signed *ltvObjects
	var diff *ltvDiff
	for _, obj := range objects {
		num := obj.ObjNum
		if num <= 0 || obj.IsXRef || obj.Unread != "" || exempt[num] {
			continue
		}
		if current.all[num] {
			if !held(num) {
				exempt[num] = true
				continue
			}
			if signed == nil {
				var err error
				if signed, err = collectLTV(ctx, rev); err != nil {
					return nil, err
				}
			}
			exempt[num] = signed.all[num]
			continue
		}
		if policy != 1 {
			continue
		}
		if diff == nil {
			var err error
			if diff, err = newLTVDiff(ctx, doc, rev, current); err != nil {
				return nil, err
			}
		}
		if diff.confined(ctx, num) {
			exempt[num] = true
		}
	}
	return exempt, nil
}

// maxEarlierRevisions bounds how many revisions between the signed one and the
// current one [exemptDroppedValidationData] reads. It is an anti-DoS guard, not
// spec-mandated: each revision is read through its own /Prev chain, so a
// crafted file of many small updates would otherwise cost the square of their
// number. A document gains a revision each time validation data or a document
// timestamp is added to it, and 64 covers decades of yearly renewals.
const maxEarlierRevisions = 64

// exemptDroppedValidationData adds to exempt each object number that an
// earlier update wrote as validation data and a later update dropped.
//
// ISO 32000-2, 12.8.2.2, Table 257 exempts each incremental update that only
// adds a DSS or a document timestamp, and a later update of the same kind may
// rewrite the DSS without a stream an earlier one added. That stream is then
// validation data in the revision its update made, and in none after, so
// [ltvExemptions], which reads the current revision, classifies it as any
// other object. A number is exempt when every appended definition of it lies
// in an update whose revision holds it as validation data, and when it is new
// since the signed revision rev or already served as validation data there,
// the rule [ltvExemptions] applies, with signedHeld telling what the signed
// revision held. A definition in the last update is judged
// by the current revision, so a stream an update rewrites while dropping it
// stays a change.
//
// Exempting a dropped object passes nothing that reaches the document: an
// object new since signing is named only by objects written after signing,
// and each of those is classified itself.
func exemptDroppedValidationData(ctx context.Context, doc, rev *document.Document, signedHeld func(num int) bool, objects []appendedObject, exempt map[int]bool) error {
	pending := make(map[int][]int64)
	for _, obj := range objects {
		num := obj.ObjNum
		if num <= 0 || obj.IsXRef || obj.Unread != "" || exempt[num] {
			continue
		}
		pending[num] = append(pending[num], obj.UpdateEnd)
	}
	distinct := make(map[int64]bool)
	for _, defs := range pending {
		for _, end := range defs {
			if end > 0 {
				distinct[end] = true
			}
		}
	}
	if len(distinct) > maxEarlierRevisions {
		return fmt.Errorf("%d updates hold objects to check, more than the %d compared", len(distinct), maxEarlierRevisions)
	}
	ends := slices.Sorted(maps.Keys(distinct))

	held := make(map[int64]map[int]bool, len(ends))
	for _, end := range ends {
		if err := ctx.Err(); err != nil {
			return err
		}
		revision, err := doc.Revision(ctx, end)
		if err != nil {
			// A revision that cannot be read holds nothing, so what its
			// update wrote stays a change.
			doc.Logger.Debug("reading an earlier revision for validation data", slog.Int64("end", end), slog.Any("err", err))
			continue
		}
		ltv, err := collectLTV(ctx, revision)
		if err != nil {
			doc.Logger.Debug("collecting an earlier revision's validation data", slog.Int64("end", end), slog.Any("err", err))
			continue
		}
		held[end] = ltv.all
	}

	var signed *ltvObjects
	for num, defs := range pending {
		dropped := true
		for _, end := range defs {
			dropped = dropped && held[end][num]
		}
		if !dropped {
			continue
		}
		if signedHeld(num) {
			if signed == nil {
				var err error
				if signed, err = collectLTV(ctx, rev); err != nil {
					return err
				}
			}
			if !signed.all[num] {
				continue
			}
		}
		exempt[num] = true
	}
	return nil
}

// ltvDiff compares the objects a P=1 update may rewrite to hold validation
// data with their signed versions.
type ltvDiff struct {
	doc, rev *document.Document
	ltv      *ltvObjects

	catalogNum    int // current catalog, 0 when /Root is no reference
	catalog       ast.PdfDict
	signedCatalog ast.PdfDict
	acroFormNum   int // current form dictionary, 0 when written directly
	fieldsNum     int // current /Fields array, 0 when written directly

	pages       map[int]ast.PdfDict // current pages by object number
	signedPages map[int]ast.PdfDict // signed pages by object number
	annotsOf    map[int]int         // current /Annots array object number to its page
}

// newLTVDiff reads what [ltvDiff.confined] compares from both revisions.
func newLTVDiff(ctx context.Context, doc, rev *document.Document, ltv *ltvObjects) (*ltvDiff, error) {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return nil, err
	}
	signedCatalog, err := document.ResolveCatalog(ctx, rev)
	if err != nil {
		return nil, err
	}
	d := &ltvDiff{
		doc: doc, rev: rev, ltv: ltv,
		catalog: catalog.Dict, signedCatalog: signedCatalog.Dict,
		pages: make(map[int]ast.PdfDict), signedPages: make(map[int]ast.PdfDict), annotsOf: make(map[int]int),
	}
	if root, ok := doc.Index.Trailer["Root"].(ast.PdfIndirectRef); ok {
		d.catalogNum = root.ObjectNum
	}
	if ref, ok := catalog.Dict["AcroForm"].(ast.PdfIndirectRef); ok {
		d.acroFormNum = ref.ObjectNum
	}
	if form, err := document.ResolveDict(ctx, doc, catalog.Dict["AcroForm"]); err == nil && form != nil {
		if ref, ok := form["Fields"].(ast.PdfIndirectRef); ok {
			d.fieldsNum = ref.ObjectNum
		}
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		return nil, err
	}
	for _, p := range pages {
		if p.Ref.ObjectNum > 0 {
			d.pages[p.Ref.ObjectNum] = p.Dict
			if ref, ok := p.Dict["Annots"].(ast.PdfIndirectRef); ok {
				d.annotsOf[ref.ObjectNum] = p.Ref.ObjectNum
			}
		}
	}
	signedPages, err := document.ResolvePages(ctx, rev, signedCatalog)
	if err != nil {
		return nil, err
	}
	for _, p := range signedPages {
		if p.Ref.ObjectNum > 0 {
			d.signedPages[p.Ref.ObjectNum] = p.Dict
		}
	}
	return d, nil
}

// confined reports whether rewriting object num changed only what adding the
// current validation data requires. An object with no such role is not. num
// is positive, so it never matches a role [newLTVDiff] left at 0.
func (d *ltvDiff) confined(ctx context.Context, num int) bool {
	switch num {
	case d.catalogNum:
		return dictConfined(d.signedCatalog, d.catalog, func(key ast.PdfName, before, after ast.PdfObject) bool {
			switch key {
			case "DSS":
				return true
			case "AcroForm":
				return d.formConfined(ctx, before, after)
			case "Extensions":
				return d.extensionsConfined(ctx, before, after)
			}
			return false
		})
	case d.acroFormNum:
		return d.formConfined(ctx, d.signedCatalog["AcroForm"], d.catalog["AcroForm"])
	case d.fieldsNum:
		before, err := document.ResolveDict(ctx, d.rev, d.signedCatalog["AcroForm"])
		if err != nil {
			return false
		}
		after, err := document.ResolveDict(ctx, d.doc, d.catalog["AcroForm"])
		if err != nil || after == nil {
			return false
		}
		return d.arrayConfined(ctx, before["Fields"], after["Fields"], d.ltv.fields)
	}
	if page, ok := d.pages[num]; ok {
		signed, ok := d.signedPages[num]
		if !ok {
			return false
		}
		return dictConfined(signed, page, func(key ast.PdfName, before, after ast.PdfObject) bool {
			return key == "Annots" && d.arrayConfined(ctx, before, after, d.ltv.widgets)
		})
	}
	if pageNum, ok := d.annotsOf[num]; ok {
		signed, ok := d.signedPages[pageNum]
		if !ok {
			return false
		}
		return d.arrayConfined(ctx, signed["Annots"], d.pages[pageNum]["Annots"], d.ltv.widgets)
	}
	return false
}

// formConfined reports whether the interactive form changed only by gaining
// document timestamp fields and a new /SigFlags (ISO 32000-1, 12.7.2, Table
// 218), which a writer adding a signature field sets.
func (d *ltvDiff) formConfined(ctx context.Context, beforeObj, afterObj ast.PdfObject) bool {
	before, err := document.ResolveDict(ctx, d.rev, beforeObj)
	if err != nil {
		return false
	}
	after, err := document.ResolveDict(ctx, d.doc, afterObj)
	if err != nil {
		return false
	}
	return dictConfined(before, after, func(key ast.PdfName, b, a ast.PdfObject) bool {
		switch key {
		case "Fields":
			return d.arrayConfined(ctx, b, a, d.ltv.fields)
		case "SigFlags":
			return true
		}
		return false
	})
}

// extensionsConfined reports whether the extensions dictionary changed only
// in the /ESIC entry ETSI EN 319 142-1, 5.6 has a writer declare for the DSS
// and document timestamps, or the /ADBE entry its note allows instead.
func (d *ltvDiff) extensionsConfined(ctx context.Context, beforeObj, afterObj ast.PdfObject) bool {
	before, err := document.ResolveDict(ctx, d.rev, beforeObj)
	if err != nil {
		return false
	}
	after, err := document.ResolveDict(ctx, d.doc, afterObj)
	if err != nil {
		return false
	}
	return dictConfined(before, after, func(key ast.PdfName, _, _ ast.PdfObject) bool {
		return key == "ESIC" || key == "ADBE"
	})
}

// arrayConfined reports whether the array afterObj of the current document is
// the signed revision's beforeObj with references from added appended, and
// nothing else changed.
func (d *ltvDiff) arrayConfined(ctx context.Context, beforeObj, afterObj ast.PdfObject, added map[int]bool) bool {
	before, err := document.ResolveArray(ctx, d.rev, beforeObj)
	if err != nil {
		return false
	}
	after, err := document.ResolveArray(ctx, d.doc, afterObj)
	if err != nil {
		return false
	}
	had := make(map[int]bool, len(before))
	for _, e := range before {
		if ref, ok := e.(ast.PdfIndirectRef); ok {
			had[ref.ObjectNum] = true
		}
	}
	kept := make(ast.PdfArray, 0, len(before))
	for _, e := range after {
		if ref, ok := e.(ast.PdfIndirectRef); ok && added[ref.ObjectNum] && !had[ref.ObjectNum] {
			continue
		}
		kept = append(kept, e)
	}
	return sameObject(before, kept)
}

// dictConfined reports whether every entry that differs between before and
// after is one allowed accepts.
func dictConfined(before, after ast.PdfDict, allowed func(key ast.PdfName, before, after ast.PdfObject) bool) bool {
	for key, a := range after {
		if b, ok := before[key]; ok && sameObject(b, a) {
			continue
		}
		if !allowed(key, before[key], a) {
			return false
		}
	}
	for key, b := range before {
		if _, ok := after[key]; !ok && !allowed(key, b, nil) {
			return false
		}
	}
	return true
}

// sameObject reports whether a and b are the same direct object, comparing
// references by object number, as litho resolves them.
func sameObject(a, b ast.PdfObject) bool {
	switch av := a.(type) {
	case ast.PdfDict:
		bv, ok := b.(ast.PdfDict)
		if !ok || len(av) != len(bv) {
			return false
		}
		for key, v := range av {
			w, ok := bv[key]
			if !ok || !sameObject(v, w) {
				return false
			}
		}
		return true
	case ast.PdfArray:
		bv, ok := b.(ast.PdfArray)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !sameObject(av[i], bv[i]) {
				return false
			}
		}
		return true
	case ast.PdfIndirectRef:
		bv, ok := b.(ast.PdfIndirectRef)
		return ok && av.ObjectNum == bv.ObjectNum
	case ast.PdfStream:
		// A stream is always indirect (ISO 32000-1, 7.3.8.1), so none sits
		// inside the dictionaries and arrays compared here.
		return false
	case nil:
		return b == nil
	}
	return a == b
}
