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

// matchAnnotAppearance matches PDF/A appearance-dictionary violations for
// annotations and widget form fields: ISO 19005-1 §6.3.3 and §6.5.3,
// ISO 19005-1 §6.9 form-field appearances, ISO 19005-2/3/4 §6.3.3.
func matchAnnotAppearance(v compliance.Violation) bool {
	if !clausePrefix(v, "6.3.3", "6.5.3", "6.9") {
		return false
	}
	return descriptionContains(v, "appearance") || descriptionContains(v, "/AP") ||
		descriptionContains(v, "N key") || descriptionContains(v, "N entry")
}

// fixAnnotAppearance synthesizes a minimal /AP /N Form XObject for every
// annotation that lacks one. Popup, Link, and zero-area annotations are
// exempt per ISO 19005-1 §6.3.3. The generated XObject is a blank Form
// with the annotation's /Rect as its /BBox, which satisfies the spec's
// requirement that "Every annotation shall have at least one appearance
// dictionary" without altering the visible page content.
func fixAnnotAppearance(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	catalogRef, ok := catalogRefForFixer(ctx, doc)
	if !ok {
		return nil, nil
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil, nil
	}
	pagesObj, ok := catalogDict["Pages"]
	if !ok {
		return nil, nil
	}

	var pages []annotPageEntry
	collectPageRefs(ctx, alloc, doc, pagesObj, &pages, make(map[int]bool), 0)

	var count int
	for _, pe := range pages {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		annotsObj, ok := pe.dict["Annots"]
		if !ok {
			continue
		}
		annots, err := document.ResolveArray(ctx, doc, annotsObj)
		if err != nil || len(annots) == 0 {
			continue
		}

		for _, annotObj := range annots {
			var annotRef ast.PdfIndirectRef
			if r, ok := annotObj.(ast.PdfIndirectRef); ok {
				annotRef = r
			}
			if annotRef.ObjectNum == 0 {
				continue
			}
			annotDict := resolveFromAllocOrDoc(ctx, alloc, doc, annotRef)
			if annotDict == nil {
				resolved, err := document.ResolveDict(ctx, doc, annotObj)
				if err != nil || resolved == nil {
					continue
				}
				annotDict = resolved
			}

			// Best-effort: an unreadable /Subtype yields an empty name,
			// which simply fails the comparisons below.
			subtype, _ := document.ResolveName(ctx, doc, annotDict["Subtype"])
			if subtype == "Popup" || subtype == "Link" {
				continue
			}

			// Best-effort: an unreadable /Rect yields a nil array, which
			// isZeroAreaRect treats as zero-area.
			rect, _ := document.ResolveArray(ctx, doc, annotDict["Rect"])
			if isZeroAreaRect(rect) {
				continue
			}

			if needsAppearance(annotDict) {
				bbox := normalizeBBox(rect)
				apRef := allocateBlankForm(alloc, bbox)

				// Widget/Btn (button) fields require /N to be a state
				// sub-dict rather than a bare stream (ISO 19005-1
				// §6.5.3 test 5 / ISO 19005-2 §6.3.3 test 3).
				// Best-effort: a missing /FT yields an empty name; only
				// Widget/Btn fields need the state sub-dict below.
				ft, _ := document.ResolveName(ctx, doc, annotDict["FT"])
				var nValue ast.PdfObject = apRef
				if subtype == "Widget" && ft == "Btn" {
					nValue = ast.PdfDict{"Off": apRef}
				}

				newAnnot := make(ast.PdfDict, len(annotDict)+1)
				maps.Copy(newAnnot, annotDict)
				newAnnot["AP"] = ast.PdfDict{"N": nValue}
				alloc.AllocAt(annotRef.ObjectNum, newAnnot)
				count++
				continue
			}

			// /AP exists. Strip /D and /R (PDF/A §6.3.3 test 2 allows
			// only /N) and promote /N to a state sub-dict when the annot
			// is a Widget/Btn (test 3).
			if patched, changed := normalizeAP(ctx, doc, annotDict); changed {
				newAnnot := make(ast.PdfDict, len(annotDict)+1)
				maps.Copy(newAnnot, annotDict)
				newAnnot["AP"] = patched
				alloc.AllocAt(annotRef.ObjectNum, newAnnot)
				count++
			}
		}
	}

	if count == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("added /AP appearance to %d annotation(s)", count)}, nil
}

// needsAppearance reports whether an annotation dict is missing an /AP /N
// appearance stream. Dicts that already carry /AP with a /N entry are left
// alone, even if the /N payload itself is malformed: inspecting the /N
// target would require resolving and validating a stream, and overwriting
// a user-supplied appearance risks losing intent.
func needsAppearance(annotDict ast.PdfDict) bool {
	apObj, hasAP := annotDict["AP"]
	if !hasAP {
		return true
	}
	apDict, ok := apObj.(ast.PdfDict)
	if !ok {
		return true
	}
	_, hasN := apDict["N"]
	return !hasN
}

// isZeroAreaRect implements the PDF/A §6.3.3 exception for annotations whose
// /Rect has rect[0]==rect[2] and rect[1]==rect[3] (zero width and height).
func isZeroAreaRect(rect ast.PdfArray) bool {
	if len(rect) != 4 {
		return false
	}
	v := make([]float64, 4)
	for i, o := range rect {
		v[i] = document.Float(o)
	}
	return v[0] == v[2] && v[1] == v[3]
}

// normalizeBBox converts an annotation /Rect into a BBox array [0 0 w h].
// A zero-sized rect yields a 1x1 fallback so the Form XObject remains valid.
func normalizeBBox(rect ast.PdfArray) ast.PdfArray {
	w, h := 1.0, 1.0
	if len(rect) == 4 {
		dw := document.Float(rect[2]) - document.Float(rect[0])
		dh := document.Float(rect[3]) - document.Float(rect[1])
		if dw < 0 {
			dw = -dw
		}
		if dh < 0 {
			dh = -dh
		}
		if dw > 0 {
			w = dw
		}
		if dh > 0 {
			h = dh
		}
	}
	return ast.PdfArray{ast.PdfReal(0), ast.PdfReal(0), ast.PdfReal(w), ast.PdfReal(h)}
}

// normalizeAP strips /D and /R from the annotation's /AP dict (PDF/A
// §6.3.3 test 2) and, for Widget/Btn annotations whose /N is a stream
// rather than a state sub-dict, wraps /N into a two-state dict named
// /Off → /N-stream (§6.3.3 test 3). Returns (nil, false) when no change
// is needed.
func normalizeAP(ctx context.Context, doc *document.Document, annotDict ast.PdfDict) (ast.PdfDict, bool) {
	apDict, ok := annotDict["AP"].(ast.PdfDict)
	if !ok {
		return nil, false
	}
	newAP := make(ast.PdfDict, len(apDict))
	changed := false
	for k, v := range apDict {
		if k == "N" {
			newAP[k] = v
			continue
		}
		changed = true
	}

	// Best-effort: unreadable /Subtype or /FT yield empty names; only the
	// Widget/Btn branch below acts on them.
	subtype, _ := document.ResolveName(ctx, doc, annotDict["Subtype"])
	ft, _ := document.ResolveName(ctx, doc, annotDict["FT"])
	if subtype == "Widget" && ft == "Btn" {
		if ref, isRef := newAP["N"].(ast.PdfIndirectRef); isRef {
			if stream, err := doc.Resolve(ctx, ref); err == nil {
				if _, isStream := stream.(ast.PdfStream); isStream {
					newAP["N"] = ast.PdfDict{"Off": ref}
					changed = true
				}
			}
		}
	}
	if !changed {
		return nil, false
	}
	return newAP, true
}

// allocateBlankForm emits an empty Form XObject stream and returns its
// indirect reference. Form XObjects must declare /Type, /Subtype, /BBox,
// and /FormType per ISO 32000-1 Table 93. An empty content stream is the
// minimum valid body.
func allocateBlankForm(alloc *writer.ObjAllocator, bbox ast.PdfArray) ast.PdfIndirectRef {
	formDict := ast.PdfDict{
		"Type":     ast.PdfName("XObject"),
		"Subtype":  ast.PdfName("Form"),
		"FormType": ast.PdfInt(1),
		"BBox":     bbox,
	}
	return alloc.Alloc(ast.NewStream(formDict, nil))
}
