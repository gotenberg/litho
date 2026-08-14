package mutations

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"math"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// SetFormField finds a form field by name and updates its /V value.
func SetFormField(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, name, value string) error {
	catalogRef, ok := CatalogRef(ctx, doc)
	if !ok {
		return fmt.Errorf("trailer missing /Root")
	}
	catalogDict, err := CatalogDict(ctx, doc, catalogRef)
	if err != nil {
		return err
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
	found, err := findAndSetField(ctx, doc, alloc, fieldsArr, "", "", name, value)
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

// findAndSetField recursively traverses the field tree to find and update a
// field. parentFT carries the field type down the tree, which a kid inherits
// when it does not state its own (ISO 32000-1, 12.7.3.1).
func findAndSetField(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, fields ast.PdfArray, parentName, parentFT, targetName, value string) (bool, error) {
	for _, fieldObj := range fields {
		var fieldRef ast.PdfIndirectRef
		if ref, ok := fieldObj.(ast.PdfIndirectRef); ok {
			fieldRef = ref
		}

		fieldDict, err := document.ResolveDict(ctx, doc, fieldObj)
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

		// Recurse into /Kids.
		if kidsObj, ok := fieldDict["Kids"]; ok {
			kidsArr, err := document.ResolveArray(ctx, doc, kidsObj)
			if err == nil && kidsArr != nil {
				found, err := findAndSetField(ctx, doc, alloc, kidsArr, fullName, fieldFT, targetName, value)
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
// bad assignment hid the whole form. pdfjs checkbox-bad-appearance.pdf shows it
// on entirely well-formed input.
//
// The caller supplies the state name; an unknown one leaves the button off in
// every viewer, which is the spec's own answer to a value that names no
// appearance.
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

	kidsArr, err := document.ResolveArray(ctx, doc, fieldDict["Kids"])
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
		kidDict, err := document.ResolveDict(ctx, doc, kidObj)
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
func FlattenForm(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) error {
	catalogRef, ok := CatalogRef(ctx, doc)
	if !ok {
		return fmt.Errorf("trailer missing /Root")
	}
	catalogDict, err := CatalogDict(ctx, doc, catalogRef)
	if err != nil {
		return err
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
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return fmt.Errorf("getting catalog: %w", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		return fmt.Errorf("getting pages: %w", err)
	}

	for _, page := range pages {
		flattenPageAnnotations(ctx, doc, alloc, page)
	}

	return nil
}

// flattenPageAnnotations processes widget annotations on a page,
// stamping their appearance streams into the page content and removing
// them from /Annots. All page-dict mutations (XObject resource
// additions, /Contents append, /Annots trim) are accumulated into a
// single draft and written with exactly one [writer.ObjAllocator.AllocAt]
// call per page, the same invariant [applyStampToPages] follows, so
// concurrent mutations cannot clobber each other.
func flattenPageAnnotations(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, page *document.Page) {
	annotsObj, ok := page.Dict["Annots"]
	if !ok {
		return
	}
	annots, err := document.ResolveArray(ctx, doc, annotsObj)
	if err != nil || annots == nil {
		return
	}

	// Start from the latest pending version of the page dict so we
	// compose correctly with any prior mutation that has already
	// rewritten this page.
	baseDict := latestPageDict(alloc, page)

	var (
		remainingAnnots  ast.PdfArray
		stampOps         []string
		xobjectAdditions []stampResource
	)

	for _, annotObj := range annots {
		annotDict, err := document.ResolveDict(ctx, doc, annotObj)
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

		// A widget litho cannot bake into the page keeps its annotation. There is
		// nothing to draw in its place, so dropping it deletes whatever it showed:
		// pdfjs annotation-tx.pdf holds a text field with a value and no /AP at
		// all, which viewers render by generating an appearance from /V and /DA
		// (ISO 32000-1, 12.7.3.3), and flattening removed the widget and with it
		// every character on the page.
		apStream, ok := resolveWidgetAppearance(ctx, doc, annotDict)
		if !ok {
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

		// string(): PdfName.String already adds the slash, and "//Name Do"
		// names no XObject, so the flattened appearance never drew.
		stampOps = append(stampOps, fmt.Sprintf("\nq 1 0 0 1 %s %s cm /%s Do Q",
			FormatFloat(x1), FormatFloat(y1), string(xobjName)))
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
		newDict["Contents"] = appendPageContents(ctx, doc, baseDict, newContentBrackets(alloc), stampRef)
	}

	// Update /Annots: keep non-widget annotations, drop the rest.
	if len(remainingAnnots) > 0 {
		newDict["Annots"] = remainingAnnots
	} else {
		delete(newDict, "Annots")
	}

	alloc.AllocAt(page.Ref.ObjectNum, newDict)
}

// resolveWidgetAppearance extracts the /N appearance stream from a
// widget annotation dict. Handles both the direct-stream form and the
// state-dict form where /N is a dict keyed by /AS name. Returns the
// resolved stream and ok=true on success; ok=false if any step of the
// resolution failed (with a debug log explaining why).
func resolveWidgetAppearance(ctx context.Context, doc *document.Document, annotDict ast.PdfDict) (ast.PdfStream, bool) {
	apObj, ok := annotDict["AP"]
	if !ok {
		doc.Logger.Debug("widget has no /AP, skipping flatten")
		return ast.PdfStream{}, false
	}
	apDict, err := document.ResolveDict(ctx, doc, apObj)
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
		resolved, err := document.ResolveObject(ctx, doc, nObj)
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
// /Subtype /Form and /BBox required there. Copying the stream unchanged left
// pdfjs checkbox-bad-appearance.pdf with a "/FlatAP20 Do" that every reader
// skipped, so the tick of a checked box vanished from a flattened page that
// still reported success.
//
// The box is only synthesised when absent: a stream that declares one is placed
// by it, and overriding that would move the appearance. Width and height come
// from /Rect, matching the translation the caller emits, and negative extents
// are normalised because a rectangle's corners may be given in either order
// (7.9.5).
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
