package mutations

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"maps"
	"math"
	"strconv"
	"strings"

	"golang.org/x/text/encoding/charmap"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/ioat"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// maxFieldTreeDepth bounds the walk up a widget's /Parent chain. It is an
// anti-DoS guard, not spec-mandated: ISO 32000-1, 12.7.3 describes a shallow
// field hierarchy, and a crafted file whose /Parent points back at a descendant
// would otherwise loop.
//
// The name says tree; the bound is on a /Parent ASCENT. This is the same
// concept as maxFieldParentDepth in acroform_split_merge.go, which carries the
// measured depth and the note on cycles. Renaming it would collide with that
// constant in this same package, so the two are cross-referenced instead.
// Beware that compliance and signature each define a maxFieldTreeDepth that
// really is a /Kids descent, a different walk entirely.
const maxFieldTreeDepth = 64

// SetFormField finds a form field by name and updates its /V value.
//
// The field is looked for in the interactive form's /Fields array first
// (ISO 32000-1, 12.7.2) and then among the widget annotations the pages carry
// (12.5.6.19). A producer may write the widget without registering the field,
// leaving it reachable only from the page. Setting one of those registers it,
// creating the interactive form when the document states none, so a viewer
// that reads the form finds the value.
//
// The form, its fields and the widgets are read from alloc first, as earlier
// stages staged them: each is rewritten from what is read, so reading the
// source's copy of one a remediation had repaired wrote the repair away.
//
// # Corpus evidence
//
//   - pdfjs issue12963.pdf: has 27 fields reachable only from the page and no
//     /AcroForm at all.
//   - pdfjs issue18561.pdf and litho syntax_1b.pdf: a field set after a
//     PDF/A-2b remediation was rewritten from its source copy, and the output
//     failed ISO 19005-2, 6.4.1 again.
func SetFormField(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, name, value string) error {
	catalogRef, ok := CatalogRef(ctx, doc)
	if !ok {
		return fmt.Errorf("trailer missing /Root")
	}
	catalogDict, err := LiveCatalogDict(ctx, alloc, doc, catalogRef)
	if err != nil {
		return err
	}

	var acroFormDict ast.PdfDict
	acroFormObj, hasAcroForm := catalogDict["AcroForm"]
	if hasAcroForm {
		acroFormDict, err = resolveDictAllocFirst(ctx, alloc, doc, acroFormObj)
		if err != nil {
			return fmt.Errorf("resolving AcroForm: %w", err)
		}
	}

	var fieldsArr ast.PdfArray
	if acroFormDict != nil {
		if fieldsObj, ok := acroFormDict["Fields"]; ok {
			fieldsArr, err = resolveArrayAllocFirst(ctx, alloc, doc, fieldsObj)
			if err != nil {
				return fmt.Errorf("resolving Fields: %w", err)
			}
		}
	}

	found := false
	if len(fieldsArr) > 0 {
		found, err = findAndSetField(ctx, doc, alloc, fieldsArr, "", "", name, value)
		if err != nil {
			return err
		}
	}

	// Not in the form: the field may exist only as a page widget.
	var registered ast.PdfIndirectRef
	if !found {
		ref, ok, err := setPageWidgetField(ctx, alloc, doc, name, value)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("form field %q not found", name)
		}
		registered = ref
	}

	newAcroForm := make(ast.PdfDict, len(acroFormDict)+2)
	maps.Copy(newAcroForm, acroFormDict)
	// Set /NeedAppearances on AcroForm so viewers regenerate appearance.
	newAcroForm["NeedAppearances"] = ast.PdfBool(true)

	if registered.ObjectNum > 0 {
		// Register the previously unlisted field so the form describes it.
		newFields := make(ast.PdfArray, 0, len(fieldsArr)+1)
		newFields = append(newFields, fieldsArr...)
		newFields = append(newFields, registered)
		newAcroForm["Fields"] = newFields
	} else if _, ok := newAcroForm["Fields"]; !ok {
		newAcroForm["Fields"] = fieldsArr
	}

	acroFormRef, isRef := acroFormObj.(ast.PdfIndirectRef)
	if hasAcroForm && isRef {
		alloc.AllocAt(acroFormRef.ObjectNum, newAcroForm)
		return nil
	}

	// The document states no interactive form, or states it inline. Allocate one
	// and point the catalog at it.
	newRef := alloc.Alloc(newAcroForm)
	newCatalog := make(ast.PdfDict, len(catalogDict)+1)
	maps.Copy(newCatalog, catalogDict)
	newCatalog["AcroForm"] = newRef
	alloc.AllocAt(catalogRef.ObjectNum, newCatalog)
	return nil
}

// setPageWidgetField finds the field a page widget presents by its fully
// qualified name and writes the value on it, returning the field's reference so
// the caller can register it.
//
// A widget either is the field, carrying /T and /FT itself, or presents one
// through /Parent (ISO 32000-1, 12.7.3.1). The name is built from the /T chain,
// and the value is written on the node that supplies the last component, which
// is where a field's /V belongs.
func setPageWidgetField(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	name, value string,
) (ast.PdfIndirectRef, bool, error) {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return ast.PdfIndirectRef{}, false, nil
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		return ast.PdfIndirectRef{}, false, nil
	}

	for _, page := range pages {
		if err := ctx.Err(); err != nil {
			return ast.PdfIndirectRef{}, false, fmt.Errorf("setting form field canceled: %w", err)
		}
		annotsObj, ok := latestPageDict(alloc, page)["Annots"]
		if !ok {
			continue
		}
		annotsArr, err := resolveArrayAllocFirst(ctx, alloc, doc, annotsObj)
		if err != nil {
			continue
		}
		for _, annotObj := range annotsArr {
			fieldObj, fieldDict, fieldFT, qualified, ok := widgetFieldIdentity(ctx, doc, alloc, annotObj)
			if !ok || qualified != name {
				continue
			}
			ref, isRef := fieldObj.(ast.PdfIndirectRef)
			if !isRef {
				// An inline field has no object number to address, so the value
				// would be dropped rather than written.
				continue
			}

			newDict := make(ast.PdfDict, len(fieldDict)+1)
			maps.Copy(newDict, fieldDict)
			setFieldValue(newDict, fieldFT, value)
			alloc.AllocAt(ref.ObjectNum, newDict)
			setKidAppearanceStates(ctx, doc, alloc, newDict, fieldFT, value)

			// ISO 32000-1, 12.7.2: /Fields lists the document's ROOT fields.
			// Registering the named node instead would rename a nested field,
			// since a viewer qualifies names from the root down.
			rootRef, ok := rootFieldRef(ctx, doc, annotObj)
			if !ok {
				rootRef = ref
			}
			return rootRef, true, nil
		}
	}
	return ast.PdfIndirectRef{}, false, nil
}

// rootFieldRef returns the outermost field object a widget belongs to, which is
// the one an interactive form lists in /Fields.
func rootFieldRef(ctx context.Context, doc *document.Document, annotObj ast.PdfObject) (ast.PdfIndirectRef, bool) {
	node, err := document.ResolveDict(ctx, doc, annotObj)
	if err != nil || node == nil {
		return ast.PdfIndirectRef{}, false
	}
	current := annotObj
	seen := make(map[int]bool)
	for range maxFieldTreeDepth {
		parentObj, parentDict, ok := document.ResolveFieldParent(ctx, doc, node, seen)
		if !ok {
			break
		}
		current, node = parentObj, parentDict
	}
	ref, ok := current.(ast.PdfIndirectRef)
	return ref, ok
}

// widgetFieldIdentity returns the field object a widget presents, its
// dictionary, its field type, and its fully qualified name. The widget is read
// from alloc first.
func widgetFieldIdentity(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, annotObj ast.PdfObject) (ast.PdfObject, ast.PdfDict, string, string, bool) {
	annotDict, err := resolveDictAllocFirst(ctx, alloc, doc, annotObj)
	if err != nil || annotDict == nil {
		return nil, nil, "", "", false
	}
	if subtype, err := document.ResolveName(ctx, doc, annotDict["Subtype"]); err != nil || subtype != "Widget" {
		return nil, nil, "", "", false
	}

	var (
		names      []string
		fieldObj   ast.PdfObject
		fieldDict  ast.PdfDict
		fieldFT    string
		node       = annotDict
		nodeObj    = annotObj
		haveField  bool
		haveFTNode bool
	)
	seen := make(map[int]bool)
	for range maxFieldTreeDepth {
		partial := ""
		if tObj, ok := node["T"]; ok {
			if tStr, err := document.ResolveString(ctx, doc, tObj); err == nil && tStr != "" {
				partial = document.DecodePdfString(tStr)
			}
		}
		if partial != "" {
			names = append(names, partial)
			if !haveField {
				fieldObj, fieldDict, haveField = nodeObj, node, true
			}
		}
		if !haveFTNode {
			if ftObj, ok := node["FT"]; ok {
				if ft, err := document.ResolveName(ctx, doc, ftObj); err == nil && ft != "" {
					fieldFT, haveFTNode = string(ft), true
				}
			}
		}

		parentObj, parentDict, ok := document.ResolveFieldParent(ctx, doc, node, seen)
		if !ok {
			break
		}
		node, nodeObj = parentDict, parentObj
	}

	if !haveField || !haveFTNode {
		return nil, nil, "", "", false
	}

	// The chain was walked from the widget upwards, so the names are innermost
	// first and the qualified name reads the other way.
	for i, j := 0, len(names)-1; i < j; i, j = i+1, j-1 {
		names[i], names[j] = names[j], names[i]
	}
	return fieldObj, fieldDict, fieldFT, strings.Join(names, "."), true
}

// findAndSetField recursively traverses the field tree to find and update a
// field. parentFT carries the field type down the tree, which a kid inherits
// when it does not state its own (ISO 32000-1, 12.7.3.1).
func findAndSetField(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, fields ast.PdfArray, parentName, parentFT, targetName, value string) (bool, error) {
	return findAndSetFieldAt(ctx, doc, alloc, fields, parentName, parentFT, targetName, value, make(map[int]bool))
}

// findAndSetFieldAt is [findAndSetField] carrying the field objects on the CURRENT
// path. A /Kids entry naming an ancestor is a cycle: unguarded, a field whose
// /Kids named itself recursed until the goroutine stack was exhausted, which is
// a fatal runtime error no recover() converts, and every level appended another
// segment to the qualified name it was building.
//
// onPath is cleared on the way back out rather than kept, because this walk
// SEARCHES and matches on the fully qualified name. The same field reached under
// a different parent has a different name, so a mark that outlived its path
// would not merely save work: it would make a legitimate match unfindable.
func findAndSetFieldAt(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, fields ast.PdfArray, parentName, parentFT, targetName, value string, onPath map[int]bool) (bool, error) {
	for _, fieldObj := range fields {
		var fieldRef ast.PdfIndirectRef
		if ref, ok := fieldObj.(ast.PdfIndirectRef); ok {
			fieldRef = ref
		}

		fieldDict, err := resolveDictAllocFirst(ctx, alloc, doc, fieldObj)
		if err != nil || fieldDict == nil {
			doc.Logger.Debug("resolving form field dict failed",
				slog.String("parent", parentName),
				slog.Any("err", err),
			)
			continue
		}

		// Build fully qualified name.
		partialName := ""
		if tObj, ok := fieldDict["T"]; ok {
			if tStr, err := document.ResolveString(ctx, doc, tObj); err == nil && tStr != "" {
				partialName = document.DecodePdfString(tStr)
			}
		}
		fieldFT := parentFT
		if ftObj, ok := fieldDict["FT"]; ok {
			if ft, err := document.ResolveName(ctx, doc, ftObj); err == nil && ft != "" {
				fieldFT = string(ft)
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
			// Found it: update /V.
			newDict := make(ast.PdfDict, len(fieldDict)+1)
			maps.Copy(newDict, fieldDict)
			setFieldValue(newDict, fieldFT, value)
			if fieldRef.ObjectNum > 0 {
				alloc.AllocAt(fieldRef.ObjectNum, newDict)
			}
			setKidAppearanceStates(ctx, doc, alloc, newDict, fieldFT, value)
			return true, nil
		}

		// Recurse into /Kids, unless this field already sits on the path.
		if kidsObj, ok := fieldDict["Kids"]; ok {
			num := fieldRef.ObjectNum
			if num != 0 && onPath[num] {
				continue
			}
			kidsArr, err := resolveArrayAllocFirst(ctx, alloc, doc, kidsObj)
			if err == nil && kidsArr != nil {
				// A direct dictionary carries no object number and cannot name
				// itself, so it closes no cycle and needs no mark.
				if num != 0 {
					onPath[num] = true
				}
				found, err := findAndSetFieldAt(ctx, doc, alloc, kidsArr, fullName, fieldFT, targetName, value, onPath)
				if num != 0 {
					delete(onPath, num)
				}
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

// setFieldValue writes /V in the representation the field's type requires.
//
// A button field's value is a NAME: the on-state chosen from its appearance
// subdictionary, or /Off (ISO 32000-1, 12.7.4.2.3). Only a text or choice
// field takes a text string (Table 228). Writing a string for every type, which
// this used to do, produced a checkbox whose /V was `(LITHOPROBE)` instead of
// `/LITHOPROBE`, and litho's own form reader then listed NO fields at all: one
// bad assignment hid the whole form, even on entirely well-formed input.
//
// The caller supplies the state name; an unknown one leaves the button off in
// every viewer, which is the spec's own answer to a value that names no
// appearance.
//
// # Corpus evidence
//
//   - pdfjs checkbox-bad-appearance.pdf: shows the hidden form on entirely
//     well-formed input.
func setFieldValue(dict ast.PdfDict, fieldFT, value string) {
	if fieldFT == "Btn" {
		dict["V"] = ast.PdfName(value)
		return
	}
	dict["V"] = document.EncodePdfString(value)
}

// setKidAppearanceStates points every widget of a button field at the appearance
// matching the new value.
//
// /AS selects which appearance in /AP is drawn (ISO 32000-1, 12.5.5). Leaving it
// on the previous state renders the OLD state, so a checkbox set to /Off still
// shows a tick until a viewer regenerates appearances. /V alone is not enough.
func setKidAppearanceStates(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, fieldDict ast.PdfDict, fieldFT, value string) {
	if fieldFT != "Btn" {
		return
	}

	kidsArr, err := resolveArrayAllocFirst(ctx, alloc, doc, fieldDict["Kids"])
	if err != nil || kidsArr == nil {
		// A field merged with its widget carries /AP itself (12.5.6.19).
		if _, hasAP := fieldDict["AP"]; hasAP {
			fieldDict["AS"] = ast.PdfName(value)
		}
		return
	}

	for _, kidObj := range kidsArr {
		kidRef, ok := kidObj.(ast.PdfIndirectRef)
		if !ok {
			continue
		}
		kidDict, err := resolveDictAllocFirst(ctx, alloc, doc, kidObj)
		if err != nil || kidDict == nil {
			continue
		}
		if _, hasAP := kidDict["AP"]; !hasAP {
			continue
		}
		newKid := make(ast.PdfDict, len(kidDict)+1)
		maps.Copy(newKid, kidDict)
		newKid["AS"] = ast.PdfName(value)
		alloc.AllocAt(kidRef.ObjectNum, newKid)
	}
}

// FlattenForm bakes form field appearances into page content and removes
// the AcroForm dictionary from the catalog.
//
// The form, the page annotations and their appearances are read from alloc
// first, as earlier stages staged them. A remediation that keeps one round
// queues its repairs there, and reading the source's copies undid them: the
// form still asked for appearance regeneration, so a widget was kept rather
// than flattened, and written back with the action the remediation removed.
//
// # Corpus evidence
//
//   - pdfjs 160F-2019.pdf: flattened after a PDF/A-2b remediation, it
//     validated to 42 violations, against 3 when the remediated file is
//     flattened on its own.
func FlattenForm(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) error {
	catalogRef, ok := CatalogRef(ctx, doc)
	if !ok {
		return fmt.Errorf("trailer missing /Root")
	}
	catalogDict, err := LiveCatalogDict(ctx, alloc, doc, catalogRef)
	if err != nil {
		return err
	}

	// Read the flag before the form goes: it decides whether the stored
	// appearances are authoritative (see [flattenPageAnnotations]).
	regenerated := formNeedsAppearances(ctx, doc, alloc, catalogDict)
	// The default appearance string a kept widget inherits from the form, which
	// the form's removal would otherwise take with it.
	inheritedDA := formDefaultAppearance(ctx, doc, alloc, catalogDict)
	// A tagged document's structure tree has to follow the widgets out.
	tags := newFlattenTags(ctx, alloc, doc, catalogRef, catalogDict)

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
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return fmt.Errorf("getting catalog: %w", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		return fmt.Errorf("getting pages: %w", err)
	}

	for _, page := range pages {
		flattenPageAnnotations(ctx, doc, alloc, page, regenerated, inheritedDA, catalogDict, tags)
	}

	return tags.commit(ctx, alloc, doc)
}

// flattenPageAnnotations processes widget annotations on a page,
// stamping their appearance streams into the page content and removing
// them from /Annots. All page-dict mutations (XObject resource
// additions, /Contents append, /Annots trim) are accumulated into a
// single draft and written with exactly one [writer.ObjAllocator.AllocAt]
// call per page, the same invariant [applyStampToPages] follows, so
// concurrent mutations cannot clobber each other.
//
// # Corpus evidence
//
//   - pdfjs annotation_hidden_noview.pdf: flattening drew both its hidden and
//     its no-view text field, and poppler renders neither in the original.
//   - pdfjs annotation-tx.pdf: holds a text field with a value and no /AP at
//     all, and flattening removed the widget and with it every character on
//     the page.
//   - pdfjs issue12233.pdf: keeps 12-to-45 byte appearances that paint nothing
//     behind the values "Man", "150" and "Red". Flattening wrote those empty
//     streams into the page and then deleted the widgets, so the values left
//     the document entirely.
func flattenPageAnnotations(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, page *document.Page, regenerated bool, inheritedDA ast.PdfObject, catalogDict ast.PdfDict, tags *flattenTags) {
	// Start from the latest pending version of the page dict so the edit
	// composes correctly with any prior mutation that has already
	// rewritten this page, its /Annots included.
	baseDict := latestPageDict(alloc, page)

	annotsObj, ok := baseDict["Annots"]
	if !ok {
		return
	}
	annots, err := resolveArrayAllocFirst(ctx, alloc, doc, annotsObj)
	if err != nil || annots == nil {
		return
	}

	var (
		remainingAnnots  ast.PdfArray
		stampOps         []string
		xobjectAdditions []stampResource
	)

	for _, annotObj := range annots {
		annotDict, err := resolveDictAllocFirst(ctx, alloc, doc, annotObj)
		if err != nil || annotDict == nil {
			doc.Logger.Debug("resolving annotation dict failed during flatten",
				slog.Any("err", err),
			)
			remainingAnnots = append(remainingAnnots, annotObj)
			continue
		}

		// Only flatten Widget annotations (form fields). Non-widget
		// annotations (Link, Text, Highlight, ...) are preserved.
		subtypeObj, _ := document.ResolveName(ctx, doc, annotDict["Subtype"])
		if subtypeObj != "Widget" {
			remainingAnnots = append(remainingAnnots, annotObj)
			continue
		}

		// A widget the viewer does not display must not be painted into the
		// page. Per ISO 32000-1, Table 165 the Hidden flag (bit 2) means the
		// annotation is neither displayed nor printed, and NoView (bit 6) means
		// it is not displayed but still printed. Baking either one in makes it
		// visible for good, which for a hidden field puts a value on the page
		// that nobody could see before.
		//
		// The annotation is kept rather than dropped. Hidden shows nothing
		// either way, but NoView still prints, and dropping it would lose that.
		if annotationHiddenOrNoView(ctx, doc, annotDict) {
			remainingAnnots = append(remainingAnnots, annotObj)
			continue
		}

		// A widget litho cannot bake into the page keeps its annotation. There is
		// nothing to draw in its place, so dropping it deletes whatever it showed.
		// A text field with a value and no /AP at all is rendered by generating
		// an appearance from /V and /DA (ISO 32000-1, 12.7.3.3), and flattening
		// it removed the widget and with it every character on the page.
		apStream, ok := resolveWidgetAppearance(ctx, doc, alloc, annotDict)
		if !ok {
			// No stored appearance. Draw the value the field holds, the way a
			// viewer would (ISO 32000-1, 12.7.3.3), so the flatten can finish
			// rather than leaving the widget behind. `set-form` produces exactly
			// this shape: it writes /V and asks for regeneration without
			// building an /AP.
			generated, built := generatedWidgetAppearance(ctx, doc, alloc, catalogDict, annotDict, inheritedDA)
			if !built && widgetDrawsNothing(ctx, doc, annotDict) {
				// Nothing to draw and nothing lost: the widget goes with the
				// form rather than outliving it as a field.
				tags.drop(ctx, alloc, doc, annotObj, annotDict)
				continue
			}
			if !built {
				keepWidget(alloc, annotObj, annotDict, inheritedDA)
				remainingAnnots = append(remainingAnnots, annotObj)
				continue
			}
			apStream = generated
		}

		// A form that asks the viewer to build its appearances stores
		// placeholders, and baking one in loses the value it stands for:
		// flattening wrote the empty streams into the page and then deleted the
		// widgets, so the values left the document entirely. Keeping the widget
		// is not enough on its own, because the placeholder it carries is what a
		// viewer would draw once /NeedAppearances goes with the form; the
		// placeholder has to go too.
		if regenerated && valueOutlivesAppearance(ctx, doc, annotDict, apStream) {
			keepWidget(alloc, annotObj, annotDict, inheritedDA)
			remainingAnnots = append(remainingAnnots, annotObj)
			continue
		}

		rectArr, err := document.ResolveArray(ctx, doc, annotDict["Rect"])
		if err != nil || len(rectArr) < 4 {
			doc.Logger.Debug("widget /Rect invalid, keeping the annotation",
				slog.Int("len", len(rectArr)),
				slog.Any("err", err),
			)
			remainingAnnots = append(remainingAnnots, annotObj)
			continue
		}
		x1 := document.Float(rectArr[0])
		y1 := document.Float(rectArr[1])
		x2 := document.Float(rectArr[2])
		y2 := document.Float(rectArr[3])

		apRef := alloc.Alloc(asFormXObject(apStream, x2-x1, y2-y1))
		xobjName := ast.PdfName(fmt.Sprintf("FlatAP%d", apRef.ObjectNum))

		// On a tagged page the appearance is tagged under the widget's
		// structure element, or marked as an artifact when it had none (see
		// flattenTags).
		open, closing := tags.bake(ctx, alloc, doc, page, annotObj, annotDict)

		// string(): PdfName.String already adds the slash, and "//Name Do"
		// names no XObject, so the flattened appearance never drew.
		stampOps = append(stampOps, fmt.Sprintf("\n%sq 1 0 0 1 %s %s cm /%s Do Q%s",
			open, FormatFloat(x1), FormatFloat(y1), string(xobjName), closing))
		xobjectAdditions = append(xobjectAdditions, stampResource{
			category: "XObject", name: xobjName, ref: apRef,
		})
	}

	// No widgets were flattenable, nothing to write.
	if len(stampOps) == 0 && len(remainingAnnots) == len(annots) {
		return
	}

	newDict := make(ast.PdfDict, len(baseDict)+1)
	maps.Copy(newDict, baseDict)

	// Merge XObject additions into /Resources.
	if len(xobjectAdditions) > 0 {
		newResources := mergeStampResources(ctx, doc, alloc, baseDict, page, xobjectAdditions)
		newDict["Resources"] = newResources
	}

	// Append the stamp operations to /Contents as a single new stream.
	if len(stampOps) > 0 {
		var content strings.Builder
		for _, op := range stampOps {
			content.WriteString(op)
		}
		stampStream := ast.NewStream(ast.PdfDict{}, []byte(content.String()))
		stampRef := alloc.Alloc(stampStream)
		// A flattened appearance is placed with the widget's absolute /Rect
		// coordinates, so it must start from the default graphics state rather
		// than whatever the page left in force (ISO 32000-1, 8.4.4).
		newDict["Contents"] = appendPageContents(ctx, doc, alloc, baseDict, newContentBrackets(alloc), stampRef)
	}
	// Marked content on a page that had none needs the key its /ParentTree
	// entry is filed under (ISO 32000-1, 14.7.4.4).
	if key, assigned := tags.structParents(page); assigned {
		newDict["StructParents"] = key
	}

	// Update /Annots: keep non-widget annotations, drop the rest.
	if len(remainingAnnots) > 0 {
		newDict["Annots"] = remainingAnnots
	} else {
		delete(newDict, "Annots")
	}

	alloc.AllocAt(page.Ref.ObjectNum, newDict)
}

// Annotation flags that keep a viewer from showing the annotation on screen
// (ISO 32000-1, Table 165): bit 2 is Hidden and bit 6 is NoView.
const (
	annotFlagHidden = 1 << 1
	annotFlagNoView = 1 << 5
)

// annotationHiddenOrNoView reports whether the annotation's /F flags keep it
// off the displayed page.
func annotationHiddenOrNoView(ctx context.Context, doc *document.Document, annotDict ast.PdfDict) bool {
	fObj, ok := annotDict["F"]
	if !ok {
		return false
	}
	resolved, err := document.ResolveObject(ctx, doc, fObj)
	if err != nil {
		doc.Logger.Debug("resolving annotation flags during flatten", slog.Any("err", err))
		return false
	}
	flags, isNum := document.Number(resolved)
	if !isNum {
		return false
	}
	return int(flags)&(annotFlagHidden|annotFlagNoView) != 0
}

// resolveWidgetAppearance extracts the /N appearance stream from a
// widget annotation dict. Handles both the direct-stream form and the
// state-dict form where /N is a dict keyed by /AS name. Returns the
// resolved stream and ok=true on success; ok=false if any step of the
// resolution failed (with a debug log explaining why). Each step reads alloc
// first, where an appearance an earlier stage built lives.
func resolveWidgetAppearance(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, annotDict ast.PdfDict) (ast.PdfStream, bool) {
	apObj, ok := annotDict["AP"]
	if !ok {
		doc.Logger.Debug("widget has no /AP, skipping flatten")
		return ast.PdfStream{}, false
	}
	apDict, err := resolveDictAllocFirst(ctx, alloc, doc, apObj)
	if err != nil || apDict == nil {
		doc.Logger.Debug("resolving widget /AP failed", slog.Any("err", err))
		return ast.PdfStream{}, false
	}
	nObj, ok := apDict["N"]
	if !ok {
		doc.Logger.Debug("widget /AP has no /N, skipping flatten")
		return ast.PdfStream{}, false
	}

	var apStream ast.PdfStream
	switch v := nObj.(type) {
	case ast.PdfStream:
		apStream = v
	default:
		// /N is a dict of states, or an indirect reference to one.
		resolved, err := resolveAllocFirst(ctx, alloc, doc, nObj)
		if err != nil {
			doc.Logger.Debug("resolving widget /N failed", slog.Any("err", err))
			return ast.PdfStream{}, false
		}
		if s, ok := resolved.(ast.PdfStream); ok {
			apStream = s
		} else if stateDict, ok := resolved.(ast.PdfDict); ok {
			asName := ast.PdfName("Off")
			if as, ok := annotDict["AS"].(ast.PdfName); ok {
				asName = as
			}
			if stateObj, ok := stateDict[asName]; ok {
				resolved2, err := resolveAllocFirst(ctx, alloc, doc, stateObj)
				if err == nil {
					if s, ok := resolved2.(ast.PdfStream); ok {
						apStream = s
					}
				}
			}
		}
	}

	if apStream.Dict == nil {
		doc.Logger.Debug("widget appearance stream unresolved, skipping")
		return ast.PdfStream{}, false
	}
	return apStream, true
}

// asFormXObject returns the appearance stream with the entries a form XObject
// needs once it is invoked from page content, sized to the widget's rectangle
// when it declares no box of its own.
//
// An appearance stream is a form XObject by virtue of where it sits (ISO
// 32000-1, 12.5.5), so a producer can omit /Subtype and readers still draw it
// through the annotation. Flattening moves it into the page's /XObject
// resources, where Do resolves it by dictionary instead, and Table 95 makes
// /Subtype /Form and /BBox required there. Copying the stream unchanged left a
// Do that every reader skipped, so the tick of a checked box vanished from a
// flattened page that still reported success.
//
// The box is only synthesised when absent: a stream that declares one is placed
// by it, and overriding that would move the appearance. Width and height come
// from /Rect, matching the translation the caller emits, and negative extents
// are normalised because a rectangle's corners may be given in either order
// (7.9.5).
//
// # Corpus evidence
//
//   - pdfjs checkbox-bad-appearance.pdf: the unchanged copy left a
//     "/FlatAP20 Do" that every reader skipped.
func asFormXObject(stream ast.PdfStream, width, height float64) ast.PdfStream {
	_, hasSubtype := stream.Dict["Subtype"]
	_, hasBBox := stream.Dict["BBox"]
	if hasSubtype && hasBBox {
		return stream
	}

	dict := make(ast.PdfDict, len(stream.Dict)+3)
	maps.Copy(dict, stream.Dict)
	dict["Type"] = ast.PdfName("XObject")
	if !hasSubtype {
		dict["Subtype"] = ast.PdfName("Form")
	}
	if !hasBBox {
		dict["BBox"] = ast.PdfArray{
			ast.PdfInt(0), ast.PdfInt(0),
			ast.PdfReal(math.Abs(width)), ast.PdfReal(math.Abs(height)),
		}
	}
	// Through the original loader, so the bytes stay raw and lazy: the copied
	// dictionary still names the /Filter they are encoded with.
	return ast.NewLazyStream(dict, stream.Bytes)
}

// formNeedsAppearances reports whether the interactive form asks viewers to
// build field appearances from the field values (ISO 32000-1, 12.7.3.3). The
// form is read from alloc first.
func formNeedsAppearances(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, catalogDict ast.PdfDict) bool {
	acroForm, err := resolveDictAllocFirst(ctx, alloc, doc, catalogDict["AcroForm"])
	if err != nil || acroForm == nil {
		return false
	}
	resolved, err := document.ResolveObject(ctx, doc, acroForm["NeedAppearances"])
	if err != nil {
		return false
	}
	flag, ok := resolved.(ast.PdfBool)
	return ok && bool(flag)
}

// valueOutlivesAppearance reports whether baking this widget's stored appearance
// would drop text the field still carries.
//
// True only when the field holds a value and the appearance paints no glyphs, so
// a widget whose appearance already draws its value flattens as before.
func valueOutlivesAppearance(ctx context.Context, doc *document.Document, annotDict ast.PdfDict, apStream ast.PdfStream) bool {
	if !fieldHoldsText(ctx, doc, annotDict) {
		return false
	}
	data, err := doc.DecodeStream(ctx, apStream)
	if err != nil {
		// Unreadable: assume it draws nothing rather than bake an unknown.
		return true
	}
	return !bytes.Contains(data, []byte("Tj")) && !bytes.Contains(data, []byte("TJ"))
}

// fieldHoldsText reports whether the widget's field is a variable-text field
// carrying a non-empty value. /FT and /V are inheritable (ISO 32000-1, 12.7.3.1),
// so both may live on an ancestor.
func fieldHoldsText(ctx context.Context, doc *document.Document, annotDict ast.PdfDict) bool {
	dict := annotDict
	var fieldType ast.PdfName
	var value ast.PdfObject
	seen := make(map[int]bool)
	for depth := 0; depth < maxFieldAncestorDepth && dict != nil; depth++ {
		if fieldType == "" {
			if ft, err := document.ResolveName(ctx, doc, dict["FT"]); err == nil && ft != "" {
				fieldType = ft
			}
		}
		if value == nil {
			if v, ok := dict["V"]; ok {
				value = v
			}
		}
		if fieldType != "" && value != nil {
			break
		}
		_, parent, ok := document.ResolveFieldParent(ctx, doc, dict, seen)
		if !ok {
			break
		}
		dict = parent
	}
	if fieldType != "Tx" && fieldType != "Ch" {
		return false
	}
	text, ok := value.(ast.PdfString)
	return ok && len(document.DecodePdfString(text)) > 0
}

// Sizing for a generated field appearance. None of these are spec values: ISO
// 32000-1, 12.7.3.3 leaves an auto-sized field's size to the viewer, so these
// are the proportions that keep one line legible inside a widget's box.
const (
	autoFontSizeRatio      = 0.66
	autoFontSizeMax        = 12.0
	autoFontSizeMin        = 4.0
	baselineDescenderRatio = 0.25
	textInsetX             = 2.0
	checkMarkInset         = 0.25
	checkMarkStroke        = 0.12
	fallbackFontName       = "LithoHelv"
)

// maxFieldAncestorDepth bounds the walk up a field's /Parent chain. This is an
// anti-DoS guard, not a spec limit: ISO 32000-1, 12.7.3.1 puts no depth on the
// field tree, while a crafted file can chain parents indefinitely.
//
// Third name in this package for one walk, beside maxFieldParentDepth (32) in
// acroform_split_merge.go, which carries the measurement, and maxFieldTreeDepth
// (64) above. Cross-referenced rather than merged: real chains stay far
// shallower than either value, so no document tells them apart.
//
// # Corpus evidence
//
//   - All corpora: the deepest real /Parent chain is 7.
const maxFieldAncestorDepth = 32

// keepWidget rewrites a widget the flatten is leaving in place so it can still
// be drawn once the interactive form is gone.
//
// Two things go with /AcroForm. A placeholder appearance that paints nothing is
// what a viewer draws instead of building one from the field value, so it is
// removed: ISO 32000-1, 12.7.3.3 has the viewer generate an appearance when the
// widget stores none. And the default-appearance string is inherited from the
// form (same clause), so it is copied onto the widget, which is otherwise left
// with no font or colour to draw with.
//
// Only an indirect widget can be rewritten: one inlined in /Annots has no object
// number to replace.
//
// The form's /DR is not carried across, because a resource dictionary has no
// place on an annotation. A /DA naming one of the standard 14 fonts, which is
// what these placeholders use, resolves without it.
//
// # Corpus evidence
//
//   - litho sigcms_2b.pdf: shows the inherited /DA case. Its text field carries
//     a value and no /DA of its own, and deleting the form alone dropped
//     "not a signature" from the page.
func keepWidget(alloc *writer.ObjAllocator, annotObj ast.PdfObject, annotDict ast.PdfDict, inheritedDA ast.PdfObject) {
	ref, ok := annotObj.(ast.PdfIndirectRef)
	if !ok {
		return
	}
	_, hasAP := annotDict["AP"]
	_, hasDA := annotDict["DA"]
	needsDA := !hasDA && inheritedDA != nil
	if !hasAP && !needsDA {
		return
	}

	updated := make(ast.PdfDict, len(annotDict)+1)
	for key, value := range annotDict {
		if key == "AP" {
			continue
		}
		updated[key] = value
	}
	if needsDA {
		updated["DA"] = inheritedDA
	}
	alloc.AllocAt(ref.ObjectNum, updated)
}

// widgetDrawsNothing reports whether a widget that stores no appearance, and
// that litho could not draw one for, would show nothing on the page anyway.
//
// A viewer builds a missing appearance from the field's value (ISO 32000-1,
// 12.7.3.3), over the colours and captions the widget's /MK states (12.5.6.19,
// Table 189). A text or choice field with no value and a button that is off
// therefore draw nothing once /MK states nothing to paint either, and neither
// does a signature field, whose appearance comes from its stream alone. Such a
// widget can leave with the form it belonged to: keeping it left a form field in
// a document the flatten promised had none. A widget of any other kind, or one
// holding a value litho could not draw, is not known to be blank and stays.
//
// # Corpus evidence
//
//   - litho filestruct_1b, sigcms_2b, typedreads_1b: each kept an empty text
//     field or an unsigned signature field after flattening, and litho still
//     read it as a form field.
//   - All corpora, 617 files that mention a form: 32 flattened outputs that
//     kept a form field keep none.
func widgetDrawsNothing(ctx context.Context, doc *document.Document, annotDict ast.PdfDict) bool {
	if appearanceCharacteristicsPaint(ctx, doc, annotDict) {
		return false
	}
	fieldType, value := inheritedFieldTypeAndValue(ctx, doc, annotDict)
	switch fieldType {
	case "Tx", "Ch":
		switch v := value.(type) {
		case nil, ast.PdfNull:
			return true
		case ast.PdfString:
			return strings.TrimSpace(document.DecodePdfString(v)) == ""
		case ast.PdfArray:
			// A multiple-selection choice field lists its values (Table 231).
			return len(v) == 0
		}
		return false
	case "Btn":
		switch v := value.(type) {
		case nil, ast.PdfNull:
			return true
		case ast.PdfName:
			return v == "" || v == "Off"
		}
		return false
	case "Sig":
		return true
	}
	return false
}

// appearanceCharacteristicsPaint reports whether the widget's /MK states
// something a viewer paints when it builds the widget's appearance: a background
// or border colour, a caption or an icon (ISO 32000-1, Table 189).
func appearanceCharacteristicsPaint(ctx context.Context, doc *document.Document, annotDict ast.PdfDict) bool {
	mk, err := document.ResolveDict(ctx, doc, annotDict["MK"])
	if err != nil || mk == nil {
		return false
	}
	for _, key := range []ast.PdfName{"BG", "BC"} {
		if colour, err := document.ResolveArray(ctx, doc, mk[key]); err == nil && len(colour) > 0 {
			return true
		}
	}
	for _, key := range []ast.PdfName{"CA", "RC", "AC"} {
		if caption, err := document.ResolveString(ctx, doc, mk[key]); err == nil && len(caption) > 0 {
			return true
		}
	}
	for _, key := range []ast.PdfName{"I", "RI", "IX"} {
		if _, has := mk[key]; has {
			return true
		}
	}
	return false
}

// formDefaultAppearance returns the interactive form's /DA string, the default
// appearance a field inherits when it declares none (ISO 32000-1, 12.7.3.3).
// The form is read from alloc first.
func formDefaultAppearance(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, catalogDict ast.PdfDict) ast.PdfObject {
	acroForm, err := resolveDictAllocFirst(ctx, alloc, doc, catalogDict["AcroForm"])
	if err != nil || acroForm == nil {
		return nil
	}
	resolved, err := document.ResolveObject(ctx, doc, acroForm["DA"])
	if err != nil {
		return nil
	}
	if _, ok := resolved.(ast.PdfString); !ok {
		return nil
	}
	return resolved
}

// generatedAppearance builds an appearance stream for a widget that stores
// none, from the value the field holds and the default-appearance string that
// says how to draw it.
//
// ISO 32000-1, 12.7.3.3 has a viewer generate the appearance of a variable-text
// field from /V and /DA when the widget carries no /AP. Flattening drops the
// widget, so a document whose fields rely on that would lose every value it
// shows, and litho used to keep such widgets rather than lose them. Drawing the
// value instead lets the flatten finish the job.
//
// The value is drawn on a single line. A text or choice field draws its string;
// a button draws a check when its value names an on-state. A field of any other
// type, or one holding nothing to show, reports false so the caller keeps the
// widget as before.
//
// # Corpus evidence
//
//   - pdfjs annotation-tx.pdf: its fields rely on a generated appearance.
func generatedAppearance(
	ctx context.Context,
	doc *document.Document,
	alloc *writer.ObjAllocator,
	catalogDict ast.PdfDict,
	annotDict ast.PdfDict,
	inheritedDA ast.PdfObject,
	width, height float64,
) (ast.PdfStream, bool) {
	if width <= 0 || height <= 0 {
		return ast.PdfStream{}, false
	}

	fieldType, value := inheritedFieldTypeAndValue(ctx, doc, annotDict)
	switch fieldType {
	case "Tx", "Ch":
		text := ""
		if s, ok := value.(ast.PdfString); ok {
			text = document.DecodePdfString(s)
		}
		if strings.TrimSpace(text) == "" {
			return ast.PdfStream{}, false
		}
		return textFieldAppearance(ctx, doc, alloc, catalogDict, annotDict, inheritedDA, text, width, height)
	case "Btn":
		state := ""
		if n, ok := value.(ast.PdfName); ok {
			state = string(n)
		}
		if state == "" || state == "Off" {
			return ast.PdfStream{}, false
		}
		return checkMarkAppearance(width, height), true
	}
	return ast.PdfStream{}, false
}

// inheritedFieldTypeAndValue reads /FT and /V from the widget or the nearest
// ancestor that states them (ISO 32000-1, 12.7.3.1).
func inheritedFieldTypeAndValue(ctx context.Context, doc *document.Document, annotDict ast.PdfDict) (ast.PdfName, ast.PdfObject) {
	var (
		fieldType ast.PdfName
		value     ast.PdfObject
	)
	dict := annotDict
	seen := make(map[int]bool)
	for depth := 0; depth < maxFieldAncestorDepth && dict != nil; depth++ {
		if fieldType == "" {
			if ft, err := document.ResolveName(ctx, doc, dict["FT"]); err == nil && ft != "" {
				fieldType = ft
			}
		}
		if value == nil {
			if v, ok := dict["V"]; ok {
				if resolved, err := document.ResolveObject(ctx, doc, v); err == nil {
					value = resolved
				}
			}
		}
		if fieldType != "" && value != nil {
			break
		}
		_, parent, ok := document.ResolveFieldParent(ctx, doc, dict, seen)
		if !ok {
			break
		}
		dict = parent
	}
	return fieldType, value
}

// textFieldAppearance draws a single line of text inside the widget's box.
//
// # Corpus evidence
//
//   - pdfjs issue12963.pdf: carries a Cyrillic value WinAnsi cannot express.
func textFieldAppearance(
	ctx context.Context,
	doc *document.Document,
	alloc *writer.ObjAllocator,
	catalogDict ast.PdfDict,
	annotDict ast.PdfDict,
	inheritedDA ast.PdfObject,
	text string,
	width, height float64,
) (ast.PdfStream, bool) {
	da := ""
	if s, err := document.ResolveString(ctx, doc, annotDict["DA"]); err == nil && s != "" {
		da = document.DecodePdfString(s)
	} else if s, ok := inheritedDA.(ast.PdfString); ok {
		da = document.DecodePdfString(s)
	}

	fontName, size, colourOps := parseDefaultAppearance(da)
	if size <= 0 {
		// An auto-sized field (0 Tf) leaves the size to the viewer. Fit the line
		// to the box, which is what a viewer does, and keep it readable.
		size = height * autoFontSizeRatio
		if size > autoFontSizeMax {
			size = autoFontSizeMax
		}
	}
	if size < autoFontSizeMin {
		size = autoFontSizeMin
	}
	if colourOps == "" {
		colourOps = "0 g"
	}

	// The generated appearance draws through a WinAnsi-encoded font, so the
	// value has to be expressible in it. A value that is not, such as a
	// Cyrillic one, would otherwise reach a viewer as raw UTF-8 bytes decoded
	// one by one through CP1252, i.e. mojibake.
	// Declining leaves the widget in place, which is what litho did for every
	// such field before it could draw any of them.
	encoded, ok := winAnsiEncode(text)
	if !ok {
		return ast.PdfStream{}, false
	}

	fontRes, resourceName, ok := appearanceFont(ctx, doc, alloc, catalogDict, fontName)
	if !ok {
		return ast.PdfStream{}, false
	}

	// Sit the baseline so the line is vertically centred in the box, with a
	// small left inset so the glyphs do not touch the border.
	baseline := (height - size) / 2
	if baseline < 0 {
		baseline = 0
	}
	baseline += size * baselineDescenderRatio

	// The font name comes from the document's /DA, so it is spelled by the
	// writer: written raw, a name holding white space, a delimiter or a "#"
	// reads back as another name (ISO 32000-1, 7.3.5), and the Tf selects no
	// font. One holding a NUL has no spelling at all, and the widget stays.
	var fontOperand bytes.Buffer
	if _, err := writer.SerializeName(&fontOperand, ast.PdfName(resourceName)); err != nil {
		doc.Logger.Debug("widget /DA names a font with no PDF spelling, keeping the annotation",
			slog.Any("err", err))
		return ast.PdfStream{}, false
	}

	body := fmt.Sprintf("/Tx BMC\nq\nBT\n%s\n%s %s Tf\n%s %s Td\n%s Tj\nET\nQ\nEMC\n",
		colourOps,
		fontOperand.String(),
		formatFloat(size),
		formatFloat(textInsetX),
		formatFloat(baseline),
		escapePdfString(encoded),
	)

	dict := ast.PdfDict{
		"Type":     ast.PdfName("XObject"),
		"Subtype":  ast.PdfName("Form"),
		"FormType": ast.PdfInt(1),
		"BBox": ast.PdfArray{
			ast.PdfInt(0), ast.PdfInt(0),
			ast.PdfReal(width), ast.PdfReal(height),
		},
		"Resources": ast.PdfDict{
			"Font": ast.PdfDict{ast.PdfName(resourceName): fontRes},
		},
	}
	return ast.NewStream(dict, []byte(body)), true
}

// checkMarkAppearance draws the tick a checked button shows, in strokes rather
// than glyphs so it needs no font.
func checkMarkAppearance(width, height float64) ast.PdfStream {
	inset := math.Min(width, height) * checkMarkInset
	body := fmt.Sprintf("q\n0 g\n%s w\n1 J\n1 j\n%s %s m\n%s %s l\n%s %s l\nS\nQ\n",
		formatFloat(math.Max(math.Min(width, height)*checkMarkStroke, 0.5)),
		formatFloat(inset), formatFloat(height/2),
		formatFloat(width/2-inset/2), formatFloat(inset),
		formatFloat(width-inset), formatFloat(height-inset),
	)
	dict := ast.PdfDict{
		"Type":     ast.PdfName("XObject"),
		"Subtype":  ast.PdfName("Form"),
		"FormType": ast.PdfInt(1),
		"BBox": ast.PdfArray{
			ast.PdfInt(0), ast.PdfInt(0),
			ast.PdfReal(width), ast.PdfReal(height),
		},
	}
	return ast.NewStream(dict, []byte(body))
}

// appearanceFont returns the font the generated appearance draws with and the
// name it is reached by inside the stream's own resources.
//
// The form's /DR holds the fonts a /DA names (ISO 32000-1, 12.7.2). When the
// document does not supply one, a standard Helvetica is written: the alternative
// is drawing nothing, and Helvetica is one of the standard 14 a viewer knows
// without embedding. That leaves the output short of a PDF/A font-embedding
// requirement, which remediation fixes separately.
func appearanceFont(
	ctx context.Context,
	doc *document.Document,
	alloc *writer.ObjAllocator,
	catalogDict ast.PdfDict,
	fontName string,
) (ast.PdfObject, string, bool) {
	if fontName != "" {
		if acroForm, err := resolveDictAllocFirst(ctx, alloc, doc, catalogDict["AcroForm"]); err == nil && acroForm != nil {
			if dr, err := resolveDictAllocFirst(ctx, alloc, doc, acroForm["DR"]); err == nil && dr != nil {
				if fonts, err := resolveDictAllocFirst(ctx, alloc, doc, dr["Font"]); err == nil && fonts != nil {
					if fontObj, ok := fonts[ast.PdfName(fontName)]; ok {
						return fontObj, fontName, true
					}
				}
			}
		}
	}

	ref := alloc.Alloc(ast.PdfDict{
		"Type":     ast.PdfName("Font"),
		"Subtype":  ast.PdfName("Type1"),
		"BaseFont": ast.PdfName("Helvetica"),
		"Encoding": ast.PdfName("WinAnsiEncoding"),
	})
	return ref, fallbackFontName, true
}

// parseDefaultAppearance reads the font name, size and colour operators out of
// a /DA string such as "/Helv 9 Tf 0 g" (ISO 32000-1, 12.7.3.3).
//
// The colour is whatever precedes the g, rg or k operator, so a /DA stating a
// grey, RGB or CMYK fill is carried across unchanged.
//
// The font name is returned decoded ([daFontName]), as the /DR key it names is.
func parseDefaultAppearance(da string) (fontName string, size float64, colourOps string) {
	tokens := strings.Fields(da)
	for i, tok := range tokens {
		switch tok {
		case "Tf":
			if i >= 2 {
				fontName = daFontName(tokens[i-2])
				if v, err := strconv.ParseFloat(tokens[i-1], 64); err == nil {
					size = v
				}
			}
		case "g", "rg", "k":
			operands := colourOperandCount(tok)
			if i >= operands {
				colourOps = strings.Join(tokens[i-operands:i+1], " ")
			}
		}
	}
	return fontName, size, colourOps
}

// daFontName decodes the name a /DA writes before its Tf. A /DA holds
// content-stream operators (ISO 32000-1, 12.7.3.3), so its font name is a name
// object whose "#" escapes the lexer decodes (ISO 32000-1, 7.3.5), and the /DR
// font dictionary it selects from is keyed by the decoded name: "/Helv#20Bold"
// names the key "Helv Bold".
//
// The token is split at white space only, so it can hold more than one name:
// "//Helvetica" is the empty name and the name Helvetica, and Tf takes the one
// next to its size. The last name in the token is therefore the font. A token
// holding no name keeps its spelling, less a leading solidus.
//
// # Corpus evidence
//
//   - All corpora: 13 files write a /DA font token this changes, each a
//     "//Helvetica" style run on an annotation flattening does not draw, so
//     flattening them writes the same content.
func daFontName(tok string) string {
	lex := lexer.NewLexer(ioat.NewReader(strings.NewReader(tok), int64(len(tok)), 0))
	name, found := "", false
	for range len(tok) {
		t, err := lex.NextToken()
		if err != nil || t.Type == lexer.TokenEOF {
			break
		}
		if t.Type == lexer.TokenName {
			name, found = strings.TrimPrefix(t.Value, "/"), true
		}
	}
	if !found {
		return strings.TrimPrefix(tok, "/")
	}
	return name
}

// colourOperandCount returns how many numbers a fill-colour operator takes.
func colourOperandCount(op string) int {
	switch op {
	case "g":
		return 1
	case "rg":
		return 3
	case "k":
		return 4
	}
	return 0
}

// winAnsiEncode renders s in the encoding the generated appearance's font
// declares, and reports whether every character could be expressed.
//
// ISO 32000-1, Annex D.2 defines WinAnsiEncoding, which covers Latin text and
// nothing else. A caller that cannot express its value must not draw it: the
// bytes would be decoded one per character and shown as mojibake.
func winAnsiEncode(s string) ([]byte, bool) {
	out, err := charmap.Windows1252.NewEncoder().Bytes([]byte(s))
	if err != nil {
		return nil, false
	}
	return out, true
}

// escapePdfString renders s as a PDF literal string, escaping the three
// characters that would otherwise end it or start an escape (ISO 32000-1,
// 7.3.4.2).
func escapePdfString(s []byte) string {
	var b strings.Builder
	b.WriteByte('(')
	for i := range s {
		switch s[i] {
		case '\\', '(', ')':
			b.WriteByte('\\')
			b.WriteByte(s[i])
		case '\n', '\r':
			// A generated appearance draws one line; a newline inside the value
			// would otherwise break the string across the content stream.
			b.WriteByte(' ')
		default:
			b.WriteByte(s[i])
		}
	}
	b.WriteByte(')')
	return b.String()
}

// formatFloat writes a number the way a content stream expects: no exponent,
// no trailing zeros, and no floating-point noise. Rounding to four decimals is
// far finer than a PDF unit (1/72 inch) can show and keeps the output
// byte-identical between runs.
func formatFloat(v float64) string {
	s := strconv.FormatFloat(v, 'f', 4, 64)
	s = strings.TrimRight(s, "0")
	return strings.TrimSuffix(s, ".")
}

// generatedWidgetAppearance measures the widget and draws its value into a form
// XObject sized to match.
func generatedWidgetAppearance(
	ctx context.Context,
	doc *document.Document,
	alloc *writer.ObjAllocator,
	catalogDict ast.PdfDict,
	annotDict ast.PdfDict,
	inheritedDA ast.PdfObject,
) (ast.PdfStream, bool) {
	rect, err := document.ResolveArray(ctx, doc, annotDict["Rect"])
	if err != nil || len(rect) < 4 {
		return ast.PdfStream{}, false
	}
	rect = document.ResolveRectangle(ctx, doc, rect)
	width := math.Abs(document.Float(rect[2]) - document.Float(rect[0]))
	height := math.Abs(document.Float(rect[3]) - document.Float(rect[1]))
	return generatedAppearance(ctx, doc, alloc, catalogDict, annotDict, inheritedDA, width, height)
}
