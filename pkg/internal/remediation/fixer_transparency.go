package remediation

import (
	"context"
	"fmt"
	"maps"
	"math"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

func matchTransparency(v compliance.Violation) bool {
	return v.Rule.Specification == "ISO_19005_1" &&
		strings.HasPrefix(v.Rule.Clause, "6.4")
}

func fixTransparency(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	catalog, err := document.GetCatalog(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("getting catalog: %w", err)
	}
	pages, err := document.GetPages(ctx, doc, catalog)
	if err != nil {
		return nil, fmt.Errorf("getting pages: %w", err)
	}

	var totalFixed int

	for _, page := range pages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		pageModified := false

		// Fix ExtGState entries in page Resources.
		if page.Resources != nil {
			gsFixed, err := fixExtGStateDict(ctx, doc, alloc, page.Resources)
			if err != nil {
				return nil, err
			}
			totalFixed += gsFixed
			if gsFixed > 0 {
				pageModified = true
			}
		}

		// Fix XObjects in page Resources (SMask on images, Group on forms).
		xoFixed, err := fixXObjectTransparency(ctx, doc, alloc, page.Resources)
		if err != nil {
			return nil, err
		}
		totalFixed += xoFixed
		if xoFixed > 0 {
			pageModified = true
		}

		// Remove page-level transparency group.
		if groupObj, ok := page.Dict["Group"]; ok {
			if isTransparencyGroup(ctx, doc, groupObj) {
				newDict := make(ast.PdfDict, len(page.Dict))
				maps.Copy(newDict, page.Dict)
				delete(newDict, "Group")
				alloc.AllocAt(page.Ref.ObjectNum, newDict)
				pageModified = true
				totalFixed++
			}
		}

		// Fix annotation appearance stream resources.
		if annotsObj, ok := page.Dict["Annots"]; ok {
			annFixed, err := fixAnnotationTransparency(ctx, doc, alloc, annotsObj)
			if err != nil {
				return nil, err
			}
			totalFixed += annFixed
			if annFixed > 0 {
				pageModified = true
			}
		}

		_ = pageModified
	}

	if totalFixed == 0 {
		return nil, nil
	}

	return []string{fmt.Sprintf("normalized %d transparency properties to opaque (CA/ca=1.0, BM=Normal, SMask=None)", totalFixed)}, nil
}

// fixExtGStateDict finds and fixes transparency properties in a Resources/ExtGState dict.
func fixExtGStateDict(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, resources ast.PdfDict) (int, error) {
	gsObj, ok := resources["ExtGState"]
	if !ok {
		return 0, nil
	}

	gsDict, err := document.ResolveDict(ctx, doc, gsObj)
	if err != nil || gsDict == nil {
		return 0, nil
	}

	fixed := 0
	for gsName, gsEntry := range gsDict {
		var gsRef ast.PdfIndirectRef
		if ref, ok := gsEntry.(ast.PdfIndirectRef); ok {
			gsRef = ref
		}

		gs, err := document.ResolveDict(ctx, doc, gsEntry)
		if err != nil || gs == nil {
			continue
		}

		modified := false
		newGS := make(ast.PdfDict, len(gs))
		maps.Copy(newGS, gs)

		// CA (stroke alpha): must be 1.0
		if caObj, ok := gs["CA"]; ok {
			if ca := document.GetFloat(caObj); !floatEquals1(ca) {
				newGS["CA"] = ast.PdfReal(1.0)
				modified = true
				fixed++
			}
		}

		// ca (fill alpha): must be 1.0
		if caObj, ok := gs["ca"]; ok {
			if ca := document.GetFloat(caObj); !floatEquals1(ca) {
				newGS["ca"] = ast.PdfReal(1.0)
				modified = true
				fixed++
			}
		}

		// BM (blend mode): must be Normal or Compatible
		if bmObj, ok := gs["BM"]; ok {
			if name, ok := bmObj.(ast.PdfName); ok {
				bm := string(name)
				if bm != "Normal" && bm != "Compatible" {
					newGS["BM"] = ast.PdfName("Normal")
					modified = true
					fixed++
				}
			}
		}

		// SMask: must be None or absent
		if smObj, ok := gs["SMask"]; ok {
			if name, ok := smObj.(ast.PdfName); ok && string(name) == "None" {
				// Already None, fine.
			} else {
				newGS["SMask"] = ast.PdfName("None")
				modified = true
				fixed++
			}
		}

		if modified {
			if gsRef.ObjectNum > 0 {
				alloc.AllocAt(gsRef.ObjectNum, newGS)
			} else {
				// Inline dict — need to update the parent ExtGState dict.
				newGSDict := make(ast.PdfDict, len(gsDict))
				maps.Copy(newGSDict, gsDict)
				newGSDict[gsName] = newGS
				// Update resources ExtGState.
				if gsRef, ok := gsObj.(ast.PdfIndirectRef); ok {
					alloc.AllocAt(gsRef.ObjectNum, newGSDict)
				}
			}
		}
	}

	return fixed, nil
}

// fixXObjectTransparency removes SMask from images and Group/Transparency from forms.
func fixXObjectTransparency(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, resources ast.PdfDict) (int, error) {
	if resources == nil {
		return 0, nil
	}

	xoObj, ok := resources["XObject"]
	if !ok {
		return 0, nil
	}

	xoDict, err := document.ResolveDict(ctx, doc, xoObj)
	if err != nil || xoDict == nil {
		return 0, nil
	}

	fixed := 0
	for _, xoEntry := range xoDict {
		var xoRef ast.PdfIndirectRef
		if ref, ok := xoEntry.(ast.PdfIndirectRef); ok {
			xoRef = ref
		}

		resolved, err := document.ResolveObject(ctx, doc, xoEntry)
		if err != nil {
			continue
		}

		stream, ok := resolved.(ast.PdfStream)
		if !ok {
			continue
		}

		modified := false
		newDict := make(ast.PdfDict, len(stream.Dict))
		maps.Copy(newDict, stream.Dict)

		subtype, _ := stream.Dict["Subtype"].(ast.PdfName)

		switch string(subtype) {
		case "Image":
			if _, ok := stream.Dict["SMask"]; ok {
				delete(newDict, "SMask")
				modified = true
				fixed++
			}
		case "Form":
			if groupObj, ok := stream.Dict["Group"]; ok {
				if isTransparencyGroup(ctx, doc, groupObj) {
					delete(newDict, "Group")
					modified = true
					fixed++
				}
			}
			// Recurse into Form XObject Resources.
			if formRes, err := document.ResolveDict(ctx, doc, stream.Dict["Resources"]); err == nil && formRes != nil {
				gsFixed, err := fixExtGStateDict(ctx, doc, alloc, formRes)
				if err != nil {
					return fixed, err
				}
				fixed += gsFixed
			}
		}

		if modified && xoRef.ObjectNum > 0 {
			alloc.AllocAt(xoRef.ObjectNum, ast.NewLazyStream(newDict, stream.Bytes))
		}
	}

	return fixed, nil
}

// fixAnnotationTransparency walks page annotations and fixes transparency in
// their appearance stream resources.
func fixAnnotationTransparency(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, annotsObj ast.PdfObject) (int, error) {
	annots, err := document.ResolveArray(ctx, doc, annotsObj)
	if err != nil || annots == nil {
		return 0, nil
	}

	fixed := 0
	for _, annotObj := range annots {
		annotDict, err := document.ResolveDict(ctx, doc, annotObj)
		if err != nil || annotDict == nil {
			continue
		}

		apObj, ok := annotDict["AP"]
		if !ok {
			continue
		}

		apDict, err := document.ResolveDict(ctx, doc, apObj)
		if err != nil || apDict == nil {
			continue
		}

		// Check /N (normal appearance).
		if nObj, ok := apDict["N"]; ok {
			resolved, err := document.ResolveObject(ctx, doc, nObj)
			if err == nil {
				if stream, ok := resolved.(ast.PdfStream); ok {
					if res, err := document.ResolveDict(ctx, doc, stream.Dict["Resources"]); err == nil && res != nil {
						gsFixed, err := fixExtGStateDict(ctx, doc, alloc, res)
						if err != nil {
							return fixed, err
						}
						fixed += gsFixed
					}
				}
			}
		}
	}

	return fixed, nil
}

func isTransparencyGroup(ctx context.Context, doc *document.Document, groupObj ast.PdfObject) bool {
	d, err := document.ResolveDict(ctx, doc, groupObj)
	if err != nil || d == nil {
		return false
	}
	if s, ok := d["S"].(ast.PdfName); ok {
		return string(s) == "Transparency"
	}
	return false
}

func floatEquals1(f float64) bool {
	return math.Abs(f-1.0) < 1e-6
}
