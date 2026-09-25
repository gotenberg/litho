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

// catalogEntry says how an entry of the catalog may change from P=2 up.
type catalogEntry struct {
	// kept is set on an entry that may change but not be removed.
	kept bool
	// entries, when set, names the entries of the dictionary the catalog
	// entry holds that may change. Its other entries keep their signed values,
	// and [changedUntyped] walks them.
	entries map[ast.PdfName]bool
}

// signingCatalogEntries are the catalog entries (ISO 32000-1, 7.7.2, Table 28)
// that filling in forms, instantiating page templates and signing change,
// which 12.8.2.2, Table 254 permits from P=2 up, and those that the validation
// data ISO 32000-2, 12.8.2.2, Table 257 exempts at every P changes. Every other
// entry of the catalog keeps its signed value:
//
//   - /AcroForm holds the form (12.7.2) that filling in and signing change, and
//     what it reaches is classified by the P=2 rules. Neither removes it.
//   - /Pages is the page tree a template instantiation grows (12.7.6). Its
//     pages are compared by [changedPage], [removedPage] and [removedAnnotation].
//   - /DSS holds validation data (ISO 32000-2, 12.8.4.3).
//   - /Extensions may change in the /ESIC and /ADBE entries that ETSI EN 319
//     142-1, 5.6 has a writer declare for validation data, and in the /ISO_
//     entry (ISO 32000-2, 7.12.2) that a signature using the algorithms of
//     ISO/TS 32001 or ISO/TS 32002 declares.
//   - /Perms may change in its /UR3 entry, a usage rights signature (12.8.4,
//     Table 258). Its /DocMDP entry names the certification signature.
//   - /Names may change in its /Pages and /Templates entries, which name the
//     pages and templates a template instantiation shows or hides (12.7.6).
//   - /Version declares a version later than the header's, which an
//     incremental update can declare only there (7.7.2, Table 28; 7.5.6), and a
//     signature using a later feature needs.
//
// /Metadata is not among them. No clause of 12.8 permits updating the XMP
// metadata on signing, and the classifier already reports the /Type /Metadata
// stream (14.3.2, Table 315) an update rewrites.
var signingCatalogEntries = map[ast.PdfName]catalogEntry{
	"AcroForm":   {kept: true},
	"Pages":      {},
	"DSS":        {},
	"Extensions": {entries: map[ast.PdfName]bool{"ESIC": true, "ADBE": true, "ISO_": true}},
	"Perms":      {entries: map[ast.PdfName]bool{"UR3": true}},
	"Names":      {entries: map[ast.PdfName]bool{"Pages": true, "Templates": true}},
	"Version":    {},
}

// changedCatalog compares the catalog of the signed revision rev with the
// current catalog of doc, entry by entry, and describes the first entry that
// changed beyond what [signingCatalogEntries] permits, or returns "" when none
// did. policy is 2 or 3: at P=1, [ltvExemptions] compares a rewritten catalog.
//
// ISO 32000-1, 12.8.2.2, Table 254 permits filling in forms, instantiating page
// templates and signing from P=2 up, adds annotation changes at P=3, and
// invalidates the signature on any other change. 12.8.2.2.2 lets a reader
// compare the signed and current versions, which is what this does. The
// classifier permits a rewritten catalog, since adding a field or a signature
// rewrites it, so an update that adds or replaces an /OpenAction or /AA action
// (12.6.3), document JavaScript or an embedded file in /Names (7.7.4), the
// optional content configuration (8.11.4) or the outline appends nothing the
// classifier refuses.
//
// Values are compared as written, with references by object number, and the
// dictionaries [signingCatalogEntries] opens are compared one level deep,
// whether or not the reference to them changed. What any other reference
// resolves to is the classifier's to judge when it carries a /Type, and
// [changedUntyped]'s when it does not. Both catalogs are read from
// their trailers, so an update that points /Root at another catalog is
// compared too.
func changedCatalog(ctx context.Context, doc, rev *document.Document, policy int) (string, error) {
	signed, err := document.ResolveCatalog(ctx, rev)
	if err != nil {
		return "", fmt.Errorf("reading the signed revision's catalog: %w", err)
	}
	current, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return "", fmt.Errorf("reading the current catalog: %w", err)
	}
	for _, key := range allKeys(signed.Dict, current.Dict) {
		before, after := signed.Dict[key], current.Dict[key]
		entry, permitted := signingCatalogEntries[key]
		if permitted && entry.entries != nil {
			// The dictionary is compared even when the reference to it is
			// unchanged: an update may rewrite the dictionary itself.
			sub, b, a, ok := changedSubentry(ctx, doc, rev, before, after, entry.entries)
			if err := ctx.Err(); err != nil {
				return "", err
			}
			switch {
			case ok && sub != "":
				return fmt.Sprintf("the catalog's /%s /%s %s, which P=%d does not permit", string(key), string(sub), change(b, a), policy), nil
			case ok, sameObject(before, after):
				continue
			}
		} else if sameObject(before, after) || (permitted && (!entry.kept || after != nil)) {
			continue
		}
		return fmt.Sprintf("the catalog's /%s %s, which P=%d does not permit", string(key), change(before, after), policy), nil
	}
	return "", nil
}

// allKeys returns the keys of before and after, in byte order, each once.
func allKeys(before, after ast.PdfDict) []ast.PdfName {
	keys := slices.Collect(maps.Keys(before))
	for key := range after {
		if _, ok := before[key]; !ok {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	return keys
}

// changedKeys returns the keys, in byte order, whose values differ between the
// dictionaries before and after. Values are compared as written, with
// references by object number ([sameObject]).
func changedKeys(before, after ast.PdfDict) []ast.PdfName {
	keys := allKeys(before, after)
	changed := keys[:0]
	for _, key := range keys {
		if !sameObject(before[key], after[key]) {
			changed = append(changed, key)
		}
	}
	return changed
}

// changedSubentry returns the first key, in byte order, whose value differs
// between the dictionary before names in rev and the one after names in doc,
// other than those in permitted, with its values in both, or "" when none
// does. ok is false when either value is neither a dictionary nor absent,
// which changes the entry as a whole.
func changedSubentry(ctx context.Context, doc, rev *document.Document, before, after ast.PdfObject, permitted map[ast.PdfName]bool) (key ast.PdfName, b, a ast.PdfObject, ok bool) {
	beforeDict, err := document.ResolveDict(ctx, rev, before)
	if err != nil {
		return "", nil, nil, false
	}
	afterDict, err := document.ResolveDict(ctx, doc, after)
	if err != nil {
		return "", nil, nil, false
	}
	for _, k := range changedKeys(beforeDict, afterDict) {
		if !permitted[k] {
			return k, beforeDict[k], afterDict[k], true
		}
	}
	return "", nil, nil, true
}

// change describes how an entry went from before to after, where nil is an
// absent entry.
func change(before, after ast.PdfObject) string {
	switch {
	case before == nil:
		return "was added"
	case after == nil:
		return "was removed"
	}
	return "changed"
}

// reachRoot is where a walk of [changedUntyped] starts, and via names it.
type reachRoot struct {
	obj ast.PdfObject
	via string
}

// fieldTypes are the field types of ISO 32000-1, 12.7.3.1, Table 220.
var fieldTypes = map[string]bool{"Btn": true, "Tx": true, "Ch": true, "Sig": true}

// changedUntyped describes the first of objects that carries no /Type, is
// written differently from its definition in the signed revision rev, and that
// doc reaches through what filling in forms, instantiating page templates and
// signing do not change. It returns "" when there is none. objects are the
// appended objects the exemptions for validation data and form data left, and
// policy is 2 or 3.
//
// The classifier permits an object without /Type, since an appearance stream,
// a font file or a field's value need not carry one ([isAllowedP2]).
// [changedPage] reports one that a signed page reaches, and this one that the
// current document reaches from:
//
//   - an entry of the catalog outside [signingCatalogEntries], and each entry
//     of the dictionaries it opens other than those it permits, such as the
//     target of /OpenAction or a name tree of document JavaScript or embedded
//     files;
//   - each XFA packet other than the datasets packet ([xfaPackets]), which
//     defines the form rather than fills it in (12.7.8);
//   - at P=2, each annotation a page lists other than a widget, which ISO
//     32000-1, 12.8.2.2, Table 254 lets change only at P=3.
//
// Reach is judged in the current revision, so an update that defines a number
// the signed document refers to but does not define (7.3.10), such as a
// dangling /OpenAction target, is judged by what refers to it. Each walk ends
// where [outsideForm] ends it. An object nothing of the above reaches is left
// alone: the form, a page, or nothing reaches it.
//
// Deliberate leniency: the trailer's /Info dictionary (14.3.3) is not a root,
// although Table 254 permits no metadata change. It draws and runs nothing,
// and iText rewrites its /ModDate and /Producer on every incremental save, so
// an approval signature it adds to a certified document would otherwise be
// reported.
func changedUntyped(ctx context.Context, doc, rev *document.Document, objects []appendedObject, policy int) (string, error) {
	var candidates []appendedObject
	for _, obj := range objects {
		if obj.IsXRef || obj.Unread != "" || obj.IsAnnot || obj.Type != "" || fieldTypes[obj.FT] {
			continue
		}
		candidates = append(candidates, obj)
	}
	if len(candidates) == 0 {
		return "", nil
	}

	roots, err := untypedRoots(ctx, doc, policy)
	if err != nil {
		return "", err
	}
	reach := make(map[int]string)
	visited := make(map[int]bool)
	for _, root := range roots {
		err := walkReferences(ctx, doc, []ast.PdfObject{root.obj}, outsideForm, visited, func(num int) {
			if _, ok := reach[num]; !ok {
				reach[num] = root.via
			}
		})
		if err != nil {
			return "", fmt.Errorf("walking %s: %w", root.via, err)
		}
	}

	for _, obj := range candidates {
		via, ok := reach[obj.ObjNum]
		if !ok || sameDefinition(ctx, doc, rev, obj.ObjNum) {
			continue
		}
		return fmt.Sprintf("object %d (%s) is reached through %s, which P=%d does not permit changing", obj.ObjNum, objDescription(obj), via, policy), nil
	}
	return "", nil
}

// untypedRoots returns where the walks of [changedUntyped] over doc start at
// policy.
func untypedRoots(ctx context.Context, doc *document.Document, policy int) ([]reachRoot, error) {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("reading the current catalog: %w", err)
	}
	var roots []reachRoot
	for _, key := range slices.Sorted(maps.Keys(catalog.Dict)) {
		value := catalog.Dict[key]
		entry, permitted := signingCatalogEntries[key]
		if !permitted {
			roots = append(roots, reachRoot{value, fmt.Sprintf("the catalog's /%s", string(key))})
			continue
		}
		if entry.entries == nil {
			continue
		}
		dict, err := document.ResolveDict(ctx, doc, value)
		if err != nil {
			// A value that is no dictionary opens nothing, and a reader
			// reads nothing from it. [changedCatalog] compares it as written.
			doc.Logger.Debug("resolving a catalog entry for its entries", slog.String("key", string(key)), slog.Any("err", err))
			continue
		}
		for _, sub := range slices.Sorted(maps.Keys(dict)) {
			if !entry.entries[sub] {
				roots = append(roots, reachRoot{dict[sub], fmt.Sprintf("the catalog's /%s /%s", string(key), string(sub))})
			}
		}
	}

	packets, err := xfaPackets(ctx, doc, catalog)
	if err != nil {
		return nil, err
	}
	for _, p := range packets {
		if p.name != "datasets" {
			roots = append(roots, reachRoot{p.value, fmt.Sprintf("the XFA %q packet", p.name)})
		}
	}

	if policy != 2 {
		return roots, nil
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		return nil, fmt.Errorf("reading the current pages: %w", err)
	}
	seen := make(map[int]bool)
	for i, page := range pages {
		if i%100 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		annots, err := document.ResolveArray(ctx, doc, page.Dict["Annots"])
		if err != nil {
			// An /Annots that cannot be read lists nothing a reader draws.
			doc.Logger.Debug("resolving /Annots for annotations P=2 keeps", slog.Any("err", err))
			continue
		}
		for _, entry := range annots {
			ref, ok := entry.(ast.PdfIndirectRef)
			if !ok || seen[ref.ObjectNum] {
				continue
			}
			seen[ref.ObjectNum] = true
			dict, err := document.ResolveDict(ctx, doc, ref)
			if err != nil || dict == nil || !isAnnotationDict(dict) {
				continue
			}
			if subtype := dictName(dict, "Subtype"); subtype != "Widget" {
				desc := objDescription(appendedObject{IsAnnot: true, Subtype: subtype})
				roots = append(roots, reachRoot{dict, fmt.Sprintf("annotation %d (%s)", ref.ObjectNum, desc)})
			}
		}
	}
	return roots, nil
}

// outsideForm reports whether a resolved object belongs to a walk of
// [changedUntyped]: [pageReach] accepts it, and it is no form field. A field
// is the form's, which the P=2 rules govern, and it carries /FT, or /T with
// /Kids or /Parent as a node of the field tree (ISO 32000-1, 12.7.3.1, Table
// 220).
func outsideForm(obj ast.PdfObject) bool {
	if !pageReach(obj) {
		return false
	}
	dict, ok := obj.(ast.PdfDict)
	if !ok {
		return true
	}
	if _, ft := dict["FT"]; ft {
		return false
	}
	_, title := dict["T"]
	_, kids := dict["Kids"]
	_, parent := dict["Parent"]
	return !title || (!kids && !parent)
}
