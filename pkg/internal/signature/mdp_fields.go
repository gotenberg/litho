package signature

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// fillEntries are the entries of a field or widget dictionary that filling in
// the field or signing it rewrites: its value, the /V of ISO 32000-1, 12.7.3.1,
// Table 220, the selected indices of a choice field (12.7.4.4, Table 231) and
// a rich text value (12.7.3.4, Table 222), and what redraws the widget, the
// appearance, appearance state, modification date, flags and border of an
// annotation (12.5.2, Table 164), a widget's border style and appearance
// characteristics (12.5.6.19, Table 188) and the default appearance and style
// of variable text (12.7.3.3, Table 222).
//
// # Corpus evidence
//
//   - pdfjs issue17169.pdf: the AntennaHouse PDF Signature Module rewrites
//     /AP, /BS, /Border, /DA, /F, /MK and /V on each signature widget it signs.
//   - pdfjs issue18801.pdf: 14 form fills Acrobat saved after signing rewrite
//     /AP, /AS, /I and /V and nothing else of 1,300 widgets.
var fillEntries = map[ast.PdfName]bool{
	"V": true, "I": true, "RV": true,
	"AP": true, "AS": true, "M": true, "F": true, "Border": true,
	"BS": true, "MK": true, "DA": true, "DS": true,
}

// formEntries are the entries of the interactive form dictionary (ISO
// 32000-1, 12.7.2, Table 218) that filling in the form and signing change:
// the fields a signature adds to, the signature flags, the default resources,
// appearance and quadding of variable text, and the flag that asks a reader to
// construct appearances. The /XFA entry may change too, and its packets are
// judged by [withoutFormData] and [changedUntyped]. Only the calculation order
// /CO is left, and it orders the calculate actions of fields, which neither
// changes.
var formEntries = map[ast.PdfName]bool{
	"Fields": true, "SigFlags": true, "DR": true, "DA": true, "Q": true,
	"NeedAppearances": true, "XFA": true,
}

// readOnlyFlag is the ReadOnly bit of a field's /Ff (ISO 32000-1, 12.7.3.1,
// Table 221).
const readOnlyFlag = 1

// changedField describes the first change to the interactive form of doc
// that is neither filling in a field nor signing, compared with the signed
// revision rev, or returns "" when there is none. objects are the objects
// appended after signing, and held reports whether the signed revision held a
// number ([signedRevision.held]). It runs at P=2.
//
// ISO 32000-1, 12.8.2.2, Table 254 permits filling in forms, instantiating
// page templates and signing at P=2, and invalidates the signature on any
// other change. The classifier passes every field and widget dictionary, so
// what the form reaches is compared here:
//
//   - the form dictionary may change only in [formEntries];
//   - a field or widget the update rewrote may change only in [fillEntries],
//     a signature field only gains a /V where it had none (12.7.4.5), its
//     /Ff only gains the ReadOnly flag a signature's /Lock sets (Table 233),
//     and its /Kids only gain new signature fields;
//   - a field or widget the update added must be a signature field or a
//     widget of one, and carry no /A or /AA action;
//   - no object a field's or widget's action reaches (12.6.2, 12.6.3) may be
//     written differently from its signed definition.
//
// A field's actions run when a reader opens, fills in or leaves it, and a
// field's type and name define the form, so none of these changes fills in a
// form or signs it.
//
// The fields are those the form's /Fields reaches through /Kids, the widgets
// the pages list, and the widgets the update wrote. A new field that
// instantiating a page template adds (12.7.6) is reported like any other.
func changedField(ctx context.Context, doc, rev *document.Document, held func(num int) bool, objects []appendedObject) (string, error) {
	appended := make(map[int]appendedObject)
	for _, obj := range objects {
		if !obj.IsXRef && obj.Unread == "" {
			appended[obj.ObjNum] = obj
		}
	}
	if len(appended) == 0 {
		return "", nil
	}

	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return "", fmt.Errorf("reading the current catalog: %w", err)
	}
	signedCatalog, err := document.ResolveCatalog(ctx, rev)
	if err != nil {
		return "", fmt.Errorf("reading the signed revision's catalog: %w", err)
	}
	form, err := document.ResolveDict(ctx, doc, catalog.Dict["AcroForm"])
	if err != nil {
		// A form that is no dictionary lists no field, and [changedCatalog]
		// compares its entry as written.
		doc.Logger.Debug("resolving /AcroForm for its fields", slog.Any("err", err))
	}
	signedForm, err := document.ResolveDict(ctx, rev, signedCatalog.Dict["AcroForm"])
	if err != nil {
		rev.Logger.Debug("resolving the signed /AcroForm", slog.Any("err", err))
	}
	if form != nil {
		for _, key := range changedKeys(signedForm, form) {
			if !formEntries[key] {
				return fmt.Sprintf("the interactive form's /%s %s, which P=2 does not permit", string(key), change(signedForm[key], form[key])), nil
			}
		}
	}

	nodes, err := formNodes(ctx, doc, form, objects)
	if err != nil {
		return "", err
	}
	f := &fieldDiff{doc: doc, rev: rev, held: held}
	var roots []reachRoot
	for i, num := range nodes {
		if i%100 == 0 {
			if err := ctx.Err(); err != nil {
				return "", err
			}
		}
		dict, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: num})
		if err != nil || dict == nil {
			continue
		}
		if _, ok := appended[num]; ok {
			if violation := f.judge(ctx, num, dict); violation != "" {
				return violation, nil
			}
		}
		for _, key := range []ast.PdfName{"A", "AA"} {
			if action, ok := dict[key]; ok {
				roots = append(roots, reachRoot{action, fmt.Sprintf("%s's /%s", f.describe(ctx, num, dict), string(key))})
			}
		}
	}

	visited := make(map[int]bool)
	for _, root := range roots {
		var violation string
		err := walkReferences(ctx, doc, []ast.PdfObject{root.obj}, outsideForm, visited, func(num int) {
			obj, ok := appended[num]
			if violation != "" || !ok || sameDefinition(ctx, doc, rev, num) {
				return
			}
			violation = fmt.Sprintf("object %d (%s) is reached through %s, which P=2 does not permit changing", num, objDescription(obj), root.via)
		})
		if err != nil {
			return "", fmt.Errorf("walking %s: %w", root.via, err)
		}
		if violation != "" {
			return violation, nil
		}
	}
	return "", nil
}

// formNodes returns, each once and in a fixed order, the fields the form
// dictionary form reaches from /Fields through /Kids, the widgets the pages of
// doc list, and the widgets among objects. The field walk is bounded by
// [maxFieldTreeDepth].
func formNodes(ctx context.Context, doc *document.Document, form ast.PdfDict, objects []appendedObject) ([]int, error) {
	var nodes []int
	seen := make(map[int]bool)
	add := func(num int) bool {
		if seen[num] {
			return false
		}
		seen[num] = true
		nodes = append(nodes, num)
		return true
	}

	var walk func(fields ast.PdfObject, depth int) error
	walk = func(fields ast.PdfObject, depth int) error {
		if depth > maxFieldTreeDepth {
			return nil
		}
		arr, err := document.ResolveArray(ctx, doc, fields)
		if err != nil {
			doc.Logger.Debug("resolving a field's /Kids", slog.Any("err", err))
			return ctx.Err()
		}
		for _, entry := range arr {
			ref, ok := entry.(ast.PdfIndirectRef)
			if !ok || !add(ref.ObjectNum) {
				continue
			}
			if len(seen)%100 == 0 {
				if err := ctx.Err(); err != nil {
					return err
				}
			}
			dict, err := document.ResolveDict(ctx, doc, ref)
			if err != nil || dict == nil {
				continue
			}
			if err := walk(dict["Kids"], depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if form != nil {
		if err := walk(form["Fields"], 0); err != nil {
			return nil, err
		}
	}

	pages, err := pagesOf(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("reading the current pages: %w", err)
	}
	for i, page := range pages {
		if i%100 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		annots, err := document.ResolveArray(ctx, doc, page.Dict["Annots"])
		if err != nil {
			doc.Logger.Debug("resolving /Annots for widgets", slog.Any("err", err))
			continue
		}
		for _, entry := range annots {
			ref, ok := entry.(ast.PdfIndirectRef)
			if !ok || seen[ref.ObjectNum] {
				continue
			}
			dict, err := document.ResolveDict(ctx, doc, ref)
			if err == nil && dict != nil && dictName(dict, "Subtype") == "Widget" {
				add(ref.ObjectNum)
			}
		}
	}

	for _, obj := range objects {
		if obj.IsAnnot && obj.Subtype == "Widget" {
			add(obj.ObjNum)
		}
	}
	return nodes, nil
}

// fieldDiff compares the fields and widgets of the current document doc with
// the signed revision rev for [changedField].
type fieldDiff struct {
	doc, rev *document.Document
	held     func(num int) bool
}

// judge describes how the field or widget num, which the update wrote as dict,
// changed beyond filling in and signing, or returns "" when it did not.
func (f *fieldDiff) judge(ctx context.Context, num int, dict ast.PdfDict) string {
	desc := f.describe(ctx, num, dict)
	if !f.held(num) {
		for _, key := range []ast.PdfName{"A", "AA"} {
			if _, ok := dict[key]; ok {
				return fmt.Sprintf("%s was added with an /%s action, which P=2 does not permit", desc, string(key))
			}
		}
		if f.fieldType(ctx, dict) != "Sig" {
			return fmt.Sprintf("%s was added, which P=2 permits only for a signature field", desc)
		}
		return ""
	}

	before, err := document.ResolveDict(ctx, f.rev, ast.PdfIndirectRef{ObjectNum: num})
	if err != nil {
		// A number the signed revision refers to without a dictionary to
		// show for it gains every entry it now has.
		f.rev.Logger.Debug("resolving a signed field", slog.Int("object", num), slog.Any("err", err))
	}
	isSig := f.fieldType(ctx, dict) == "Sig"
	for _, key := range changedKeys(before, dict) {
		if !f.filled(ctx, key, before, dict, isSig) {
			return fmt.Sprintf("%s %s its /%s, which P=2 does not permit", desc, changeVerb(before[key], dict[key]), string(key))
		}
	}
	return ""
}

// filled reports whether the entry key, which differs between the signed
// field or widget before and the current one after, changed as filling in or
// signing changes it.
func (f *fieldDiff) filled(ctx context.Context, key ast.PdfName, before, after ast.PdfDict, isSig bool) bool {
	switch key {
	case "V":
		// ISO 32000-1, 12.7.4.5: signing sets a signature field's value. A
		// field already signed keeps its signature.
		return !isSig || isNullOrAbsent(before["V"])
	case "Ff":
		b, bok := fieldFlags(before["Ff"])
		a, aok := fieldFlags(after["Ff"])
		return bok && aok && a == b|readOnlyFlag
	case "Kids":
		return f.kidsAdded(ctx, before["Kids"], after["Kids"])
	}
	return fillEntries[key]
}

// kidsAdded reports whether the current /Kids after is the signed /Kids
// before with only new objects added. The added kids are judged as fields
// themselves.
func (f *fieldDiff) kidsAdded(ctx context.Context, before, after ast.PdfObject) bool {
	b, err := document.ResolveArray(ctx, f.rev, before)
	if err != nil {
		return false
	}
	a, err := document.ResolveArray(ctx, f.doc, after)
	if err != nil {
		return false
	}
	rest := make(ast.PdfArray, 0, len(a))
	for _, kid := range a {
		if ref, ok := kid.(ast.PdfIndirectRef); ok && !f.held(ref.ObjectNum) {
			continue
		}
		rest = append(rest, kid)
	}
	return sameObject(b, rest)
}

// fieldType returns the field type of a field or widget of the current
// document, stated or inherited through /Parent (ISO 32000-1, 12.7.3.1, Table
// 220), or "" when it has none. The walk is bounded by [maxFieldTreeDepth].
func (f *fieldDiff) fieldType(ctx context.Context, dict ast.PdfDict) string {
	seen := make(map[int]bool)
	for depth := 0; dict != nil && depth <= maxFieldTreeDepth; depth++ {
		if ft, err := document.ResolveName(ctx, f.doc, dict["FT"]); err == nil && ft != "" {
			return string(ft)
		}
		var ok bool
		if _, dict, ok = document.ResolveFieldParent(ctx, f.doc, dict, seen); !ok {
			break
		}
	}
	return ""
}

// describe names the field or widget num for MDPInfo.
func (f *fieldDiff) describe(ctx context.Context, num int, dict ast.PdfDict) string {
	kind := "field"
	_, ft := dict["FT"]
	_, title := dict["T"]
	if !ft && !title && dictName(dict, "Subtype") == "Widget" {
		kind = "widget"
	}
	if typ := f.fieldType(ctx, dict); typ != "" {
		return fmt.Sprintf("%s %d (%s)", kind, num, typ)
	}
	return fmt.Sprintf("%s %d", kind, num)
}

// changeVerb describes how an entry went from before to after, where nil is
// an absent entry, as a verb.
func changeVerb(before, after ast.PdfObject) string {
	switch {
	case before == nil:
		return "added"
	case after == nil:
		return "removed"
	}
	return "changed"
}

// isNullOrAbsent reports whether an entry is absent or the null object, which
// ISO 32000-1, 7.3.9 makes the same.
func isNullOrAbsent(obj ast.PdfObject) bool {
	if obj == nil {
		return true
	}
	_, null := obj.(ast.PdfNull)
	return null
}

// fieldFlags reads a field's /Ff as written, an absent one as 0 (ISO
// 32000-1, 12.7.3.1, Table 220). It reports false for a value that is no
// integer.
func fieldFlags(obj ast.PdfObject) (int64, bool) {
	if obj == nil {
		return 0, true
	}
	v, ok := obj.(ast.PdfInt)
	return int64(v), ok
}
