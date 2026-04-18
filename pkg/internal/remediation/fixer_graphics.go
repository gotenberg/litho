package remediation

import (
	"context"
	"fmt"
	"log/slog"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

func matchGraphics(v compliance.Violation) bool {
	return clausePrefix(v, "6.2.9", "6.2.6") || // rendering intent (ExtGState only)
		clausePrefix(v, "6.2.5") || // Form XObject /Subtype2, /PS
		clausePrefix(v, "6.2.4.2") || // overprint mode OPM
		clausePrefix(v, "6.2.8.1") || // Form XObject /OPI
		clausePrefix(v, "6.2.8.2") || // reference XObject /Ref
		clausePrefix(v, "6.2.10") && descriptionContains(v, "blend mode") || // BM (parts 2+)
		descriptionContains(v, "Alternates") || // image /Alternates
		descriptionContains(v, "Interpolate") // image /Interpolate
}

// validRenderingIntents lists the four standard rendering intents per
// ISO 32000-2, Table 69.
var validRenderingIntents = map[ast.PdfName]bool{
	"AbsoluteColorimetric": true,
	"RelativeColorimetric": true,
	"Saturation":           true,
	"Perceptual":           true,
}

func fixGraphics(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("resolving catalog: %w", err)
	}

	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		return nil, fmt.Errorf("resolving pages: %w", err)
	}

	var notes []string
	visited := make(map[int]bool)

	for i, page := range pages {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("fixing graphics on page %d: %w", i+1, err)
		}

		resDict := page.Resources
		if resDict == nil {
			continue
		}

		pageNotes, err := fixResourcesGraphics(ctx, alloc, doc, resDict, visited)
		if err != nil {
			return nil, fmt.Errorf("fixing graphics on page %d: %w", i+1, err)
		}
		notes = append(notes, pageNotes...)
	}

	return notes, nil
}

func fixResourcesGraphics(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, resources ast.PdfDict, visited map[int]bool) ([]string, error) {
	var notes []string

	gsNotes, err := fixExtGStates(ctx, alloc, doc, resources)
	if err != nil {
		return nil, err
	}
	notes = append(notes, gsNotes...)

	xobjNotes, err := fixXObjects(ctx, alloc, doc, resources, visited)
	if err != nil {
		return nil, err
	}
	notes = append(notes, xobjNotes...)

	return notes, nil
}

func fixExtGStates(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, resources ast.PdfDict) ([]string, error) {
	gsObj, ok := resources["ExtGState"]
	if !ok {
		return nil, nil
	}

	gsDict, err := document.ResolveDict(ctx, doc, gsObj)
	if err != nil {
		doc.Logger.Debug("resolving ExtGState dict", slog.Any("err", err))
		return nil, nil
	}
	if gsDict == nil {
		return nil, nil
	}

	var notes []string

	for name, gsEntryObj := range gsDict {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		var gsRef ast.PdfIndirectRef
		var gsEntry ast.PdfDict
		hasRef := false

		if ref, ok := gsEntryObj.(ast.PdfIndirectRef); ok {
			hasRef = true
			gsRef = ref
			gsEntry = resolveFromAllocOrDoc(ctx, alloc, doc, ref)
		} else {
			resolved, err := document.ResolveDict(ctx, doc, gsEntryObj)
			if err != nil {
				continue
			}
			gsEntry = resolved
		}
		if gsEntry == nil {
			continue
		}

		modified := false
		newGS := make(ast.PdfDict, len(gsEntry))
		maps.Copy(newGS, gsEntry)

		// Fix invalid rendering intent (6.2.9 / 6.2.6).
		if riObj, ok := newGS["RI"]; ok {
			if riName, ok := riObj.(ast.PdfName); ok {
				if !validRenderingIntents[riName] {
					newGS["RI"] = ast.PdfName("RelativeColorimetric")
					modified = true
					notes = append(notes, fmt.Sprintf("replaced invalid rendering intent %q in ExtGState %s", string(riName), string(name)))
				}
			}
		}

		// Fix OPM=1 with ICCBased CMYK (6.2.4.2).
		if opmObj, ok := newGS["OPM"]; ok {
			if document.Float(opmObj) == 1 {
				newGS["OPM"] = ast.PdfInt(0)
				modified = true
				notes = append(notes, fmt.Sprintf("set OPM to 0 in ExtGState %s", string(name)))
			}
		}

		// Fix invalid blend mode (6.2.10).
		if bmObj, ok := newGS["BM"]; ok {
			if bmName, ok := bmObj.(ast.PdfName); ok && bmName != "Normal" && bmName != "Compatible" {
				newGS["BM"] = ast.PdfName("Normal")
				modified = true
				notes = append(notes, fmt.Sprintf("set blend mode to Normal in ExtGState %s", string(name)))
			}
		}

		// Strip forbidden transfer-function entries (ISO 19005-2 §6.2.5
		// forbids /TR; /TR2 is permitted only when set to /Default).
		if _, ok := newGS["TR"]; ok {
			delete(newGS, "TR")
			modified = true
			notes = append(notes, fmt.Sprintf("removed /TR from ExtGState %s", string(name)))
		}
		if tr2, ok := newGS["TR2"]; ok {
			if name, ok := tr2.(ast.PdfName); !ok || name != "Default" {
				newGS["TR2"] = ast.PdfName("Default")
				modified = true
			}
		}
		// §6.2.5 also forbids /HTO and /HTP on ExtGState.
		for _, key := range []ast.PdfName{"HTO", "HTP"} {
			if _, ok := newGS[key]; ok {
				delete(newGS, key)
				modified = true
				notes = append(notes, fmt.Sprintf("removed /%s from ExtGState %s", key, string(name)))
			}
		}
		if htObj, ok := newGS["HT"]; ok {
			if patched, changed := normalizeHalftone(ctx, doc, htObj); changed {
				newGS["HT"] = patched
				modified = true
				notes = append(notes, fmt.Sprintf("normalised /HT in ExtGState %s", string(name)))
			}
		}

		if modified && hasRef {
			alloc.AllocAt(gsRef.ObjectNum, newGS)
		}
	}

	return notes, nil
}

// normalizeHalftone coerces a halftone dict so it complies with
// ISO 19005-2 §6.2.5: HalftoneType must be 1 or 5, HalftoneName is
// forbidden, and any non-primary component of a Type-5 halftone needs
// a TransferFunction (defaulted to /Identity when absent).
func normalizeHalftone(ctx context.Context, doc *document.Document, htObj ast.PdfObject) (ast.PdfDict, bool) {
	htDict, err := document.ResolveDict(ctx, doc, htObj)
	if err != nil || htDict == nil {
		return nil, false
	}
	newDict := make(ast.PdfDict, len(htDict))
	maps.Copy(newDict, htDict)

	changed := false
	if _, ok := newDict["HalftoneName"]; ok {
		delete(newDict, "HalftoneName")
		changed = true
	}
	htType := 1
	if htTypeObj, ok := newDict["HalftoneType"]; ok {
		switch t := htTypeObj.(type) {
		case ast.PdfInt:
			htType = int(t)
		case ast.PdfReal:
			htType = int(t)
		}
	}
	if htType != 1 && htType != 5 {
		newDict["HalftoneType"] = ast.PdfInt(1)
		changed = true
		htType = 1
	}

	if htType == 5 {
		// Halftone Type 5 component rules (ISO 32000-2 §10.6.5.4):
		// primary CMYK components must not carry a TransferFunction,
		// non-primary (spot) components must carry one. Normalise
		// both directions.
		primaryCMYK := map[ast.PdfName]bool{
			"Cyan": true, "Magenta": true, "Yellow": true, "Black": true,
		}
		for k, v := range newDict {
			if k == "Type" || k == "HalftoneType" || k == "HalftoneName" {
				continue
			}
			compDict, err := document.ResolveDict(ctx, doc, v)
			if err != nil || compDict == nil {
				continue
			}
			_, hasTF := compDict["TransferFunction"]
			if primaryCMYK[k] {
				if !hasTF {
					continue
				}
				newComp := make(ast.PdfDict, len(compDict))
				maps.Copy(newComp, compDict)
				delete(newComp, "TransferFunction")
				newDict[k] = newComp
				changed = true
				continue
			}
			if hasTF {
				continue
			}
			newComp := make(ast.PdfDict, len(compDict)+1)
			maps.Copy(newComp, compDict)
			newComp["TransferFunction"] = ast.PdfName("Identity")
			newDict[k] = newComp
			changed = true
		}
	}

	if !changed {
		return nil, false
	}
	return newDict, true
}

func fixXObjects(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, resources ast.PdfDict, visited map[int]bool) ([]string, error) {
	xobjObj, ok := resources["XObject"]
	if !ok {
		return nil, nil
	}

	xobjDict, err := document.ResolveDict(ctx, doc, xobjObj)
	if err != nil {
		doc.Logger.Debug("resolving XObject dict", slog.Any("err", err))
		return nil, nil
	}
	if xobjDict == nil {
		return nil, nil
	}

	var notes []string

	for name, entryObj := range xobjDict {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		ref, ok := entryObj.(ast.PdfIndirectRef)
		if !ok {
			continue
		}

		if visited[ref.ObjectNum] {
			continue
		}
		visited[ref.ObjectNum] = true

		// Resolve from allocator first, then from document.
		var xobjStream ast.PdfStream
		if obj, ok := alloc.Pending()[ref.ObjectNum]; ok {
			if s, ok := obj.(ast.PdfStream); ok {
				xobjStream = s
			}
		}
		if xobjStream.Dict == nil {
			resolved, err := document.ResolveStream(ctx, doc, ref)
			if err != nil {
				continue
			}
			xobjStream = resolved
		}
		if xobjStream.Dict == nil {
			continue
		}

		subtype, _ := xobjStream.Dict["Subtype"].(ast.PdfName)
		modified := false
		newDict := make(ast.PdfDict, len(xobjStream.Dict))
		maps.Copy(newDict, xobjStream.Dict)

		if subtype == "Image" {
			// Remove /Alternates (6.2.8 / 6.2.7.1).
			if _, ok := newDict["Alternates"]; ok {
				delete(newDict, "Alternates")
				modified = true
				notes = append(notes, fmt.Sprintf("removed /Alternates from image %s", string(name)))
			}

			// Set /Interpolate to false (6.2.4).
			if interpObj, ok := newDict["Interpolate"]; ok {
				if b, ok := interpObj.(ast.PdfBool); ok && bool(b) {
					newDict["Interpolate"] = ast.PdfBool(false)
					modified = true
					notes = append(notes, fmt.Sprintf("set /Interpolate to false in image %s", string(name)))
				}
			}
		}

		if subtype == "PS" {
			// PDF/A-2+ §6.2.9.3 forbids PostScript XObjects. Convert to
			// an empty Form XObject so existing Do references still
			// resolve to a valid no-op.
			newDict["Subtype"] = ast.PdfName("Form")
			newDict["FormType"] = ast.PdfInt(1)
			if _, hasBBox := newDict["BBox"]; !hasBBox {
				newDict["BBox"] = ast.PdfArray{ast.PdfReal(0), ast.PdfReal(0), ast.PdfReal(1), ast.PdfReal(1)}
			}
			for _, k := range []ast.PdfName{"Level1", "PS", "OPI", "Ref"} {
				delete(newDict, k)
			}
			alloc.AllocAt(ref.ObjectNum, ast.NewStream(newDict, nil))
			notes = append(notes, fmt.Sprintf("replaced PostScript XObject %s with empty Form", string(name)))
			continue
		}

		if subtype == "Form" {
			// Remove /Subtype2 and /PS (6.2.5).
			if _, ok := newDict["Subtype2"]; ok {
				delete(newDict, "Subtype2")
				modified = true
				notes = append(notes, fmt.Sprintf("removed /Subtype2 from form XObject %s", string(name)))
			}
			if _, ok := newDict["PS"]; ok {
				delete(newDict, "PS")
				modified = true
				notes = append(notes, fmt.Sprintf("removed /PS from form XObject %s", string(name)))
			}

			// Remove /OPI (6.2.8.1).
			if _, ok := newDict["OPI"]; ok {
				delete(newDict, "OPI")
				modified = true
				notes = append(notes, fmt.Sprintf("removed /OPI from form XObject %s", string(name)))
			}

			// Remove /Ref (6.2.8.2).
			if _, ok := newDict["Ref"]; ok {
				delete(newDict, "Ref")
				modified = true
				notes = append(notes, fmt.Sprintf("removed /Ref from form XObject %s", string(name)))
			}

			// Recurse into form XObject resources.
			if resObj, ok := newDict["Resources"]; ok {
				formRes, err := document.ResolveDict(ctx, doc, resObj)
				if err == nil && formRes != nil {
					subNotes, err := fixResourcesGraphics(ctx, alloc, doc, formRes, visited)
					if err != nil {
						return nil, err
					}
					notes = append(notes, subNotes...)
				}
			}
		}

		if modified {
			alloc.AllocAt(ref.ObjectNum, ast.NewStream(newDict, nil))
		}
	}

	return notes, nil
}
