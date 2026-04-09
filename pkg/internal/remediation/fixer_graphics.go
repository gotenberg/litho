package remediation

import (
	"context"
	"fmt"
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
	catalog, err := document.GetCatalog(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("resolving catalog: %w", err)
	}

	pages, err := document.GetPages(ctx, doc, catalog)
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
	if err != nil || gsDict == nil {
		return nil, nil //nolint:nilerr // missing ExtGState is non-fatal
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
			if document.GetFloat(opmObj) == 1 {
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

		if modified && hasRef {
			alloc.AllocAt(gsRef.ObjectNum, newGS)
		}
	}

	return notes, nil
}

func fixXObjects(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, resources ast.PdfDict, visited map[int]bool) ([]string, error) {
	xobjObj, ok := resources["XObject"]
	if !ok {
		return nil, nil
	}

	xobjDict, err := document.ResolveDict(ctx, doc, xobjObj)
	if err != nil || xobjDict == nil {
		return nil, nil //nolint:nilerr // missing XObject is non-fatal
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
