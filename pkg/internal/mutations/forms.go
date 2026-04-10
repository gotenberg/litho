package mutations

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// SetFormField finds a form field by name and updates its /V value.
func SetFormField(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, name, value string) error {
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

// FlattenForm bakes form field appearances into page content and removes
// the AcroForm dictionary from the catalog.
func FlattenForm(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) error {
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
			doc.Logger.Debug("resolving annotation dict failed during flatten",
				slog.Any("err", err),
			)
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
			doc.Logger.Debug("widget has no /AP, skipping flatten")
			continue
		}
		apDict, err := document.ResolveDict(ctx, doc, apObj)
		if err != nil || apDict == nil {
			doc.Logger.Debug("resolving widget /AP failed", slog.Any("err", err))
			continue
		}

		// Get /N (normal appearance). Could be a stream or a dict of states.
		nObj, ok := apDict["N"]
		if !ok {
			doc.Logger.Debug("widget /AP has no /N, skipping flatten")
			continue
		}

		var apStream ast.PdfStream
		switch v := nObj.(type) {
		case ast.PdfStream:
			apStream = v
		default:
			// Could be a dict of states: try the /AS key.
			resolved, err := document.ResolveObject(ctx, doc, nObj)
			if err != nil {
				doc.Logger.Debug("resolving widget /N failed", slog.Any("err", err))
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
			doc.Logger.Debug("widget appearance stream unresolved, skipping")
			continue
		}

		// Get the annotation /Rect for positioning.
		rectArr, err := document.ResolveArray(ctx, doc, annotDict["Rect"])
		if err != nil || len(rectArr) < 4 {
			doc.Logger.Debug("widget /Rect invalid, skipping",
				slog.Int("len", len(rectArr)),
				slog.Any("err", err),
			)
			continue
		}
		x1 := document.GetFloat(rectArr[0])
		y1 := document.GetFloat(rectArr[1])

		// Allocate the appearance stream as a Form XObject.
		apRef := alloc.Alloc(apStream)
		xobjName := fmt.Sprintf("FlatAP%d", apRef.ObjectNum)

		// Generate content stream commands to stamp the appearance.
		stampOps = append(stampOps, fmt.Sprintf("\nq 1 0 0 1 %s %s cm /%s Do Q",
			FormatFloat(x1), FormatFloat(y1), xobjName))

		// Add the XObject to page resources.
		addXObjectToPage(ctx, doc, alloc, page, xobjName, apRef)
	}

	if len(stampOps) > 0 {
		// Append stamp operations to page contents.
		appendToPageContents(doc, alloc, page, stampOps)
	}

	// Update /Annots (remove flattened widgets).
	if len(remainingAnnots) > 0 {
		newDict := make(ast.PdfDict, len(page.Dict))
		maps.Copy(newDict, page.Dict)
		newDict["Annots"] = remainingAnnots
		alloc.AllocAt(page.Ref.ObjectNum, newDict)
	} else if len(annots) > 0 {
		// All annotations were flattened: remove /Annots.
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
func appendToPageContents(_ *document.Document, alloc *writer.ObjAllocator, page *document.Page, ops []string) {
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
