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
// annotation that lacks one. Popup and Link annotations are exempt, as are
// zero-area ones under ISO 19005-2/3/4 §6.3.3 t1, whose test opens with
// "width == 0 && height == 0". The generated XObject is a blank Form
// with the annotation's /Rect as its /BBox, which satisfies the spec's
// requirement that "Every annotation shall have at least one appearance
// dictionary" without altering the visible page content.
//
// PDF/A-1 states the requirement on the widget instead, and states it
// unconditionally: ISO 19005-1 §6.9 t2 is keyed to veraPDF's PDWidgetAnnot
// with the test "AP != null" and grants no zero-area exemption, which is why
// compliance.checkFieldAppearance deliberately applies none either. An
// invisible signature field is conventionally /Rect [0 0 0 0], so exempting it
// here left part 1 reporting a violation this fixer could never answer.
//
// # Corpus evidence
//
//   - pdfjs annotation-tx.pdf: a blank form given to a valued text field lost
//     the 12 characters that were the whole page.
//   - pdfjs bug1847733.pdf: a blank form given to a valued text field lost the
//     field reading "In order to submit this form, you should open it with
//     Adobe Acrobat Reader."
func fixAnnotAppearance(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, profile string) ([]string, error) {
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
			// Measure the coordinates, not the references naming them
			// (ISO 32000-1, 7.3.9). Left unresolved they read as zero, so a real
			// rectangle looked zero-area: the annotation was skipped and kept its
			// missing /AP, and any appearance built for it would have carried a
			// 1 by 1 /BBox instead of the annotation's own size.
			rect = document.ResolveRectangle(ctx, doc, rect)
			// A zero-area annotation is exempt everywhere except a PDF/A-1
			// widget: ISO 19005-2/3/4 §6.3.3 t1 opens its test with
			// "width == 0 && height == 0", while ISO 19005-1 §6.9 t2 asks only
			// "AP != null" of a PDWidgetAnnot. Invisible signature fields carry
			// /Rect [0 0 0 0], so skipping them left a part-1 violation
			// reported and unfixable. Part 1 states nothing about a zero-area
			// annotation that is not a widget: its §6.5.3 tests 4 to 6 all
			// begin "AP != N", constraining an appearance that exists rather
			// than requiring one, so those stay exempt.
			if isZeroAreaRect(rect) && (compliance.PDFAPart(profile) != 1 || subtype != "Widget") {
				continue
			}

			if needsAppearance(ctx, doc, annotDict) {
				// A variable-text field with a value has no appearance because
				// the viewer builds one from /V and /DA (ISO 32000-1, 12.7.3.3).
				// A blank form satisfies ISO 19005-2, 6.3.3 by replacing that
				// with nothing, erasing the field's visible text. Drawing the
				// value needs the /DA font out of /DR and the field's own
				// layout, so until that exists the violation stays as a
				// residual the caller can see, which is worth more than a
				// conformant document that lost its content.
				if rendersOwnValue(ctx, doc, annotDict, subtype) {
					continue
				}
				bbox := normalizeBBox(rect)
				apRef := allocateBlankForm(alloc, bbox)

				// Widget/Btn (button) fields require /N to be a state
				// sub-dict rather than a bare stream (ISO 19005-1
				// §6.5.3 test 5 / ISO 19005-2 §6.3.3 test 3).
				var nValue ast.PdfObject = apRef
				var state ast.PdfName
				if subtype == "Widget" && inheritedFieldType(ctx, doc, annotDict) == "Btn" {
					state = appearanceState(ctx, doc, annotDict)
					nValue = ast.PdfDict{state: apRef}
				}

				newAnnot := make(ast.PdfDict, len(annotDict)+2)
				maps.Copy(newAnnot, annotDict)
				newAnnot["AP"] = ast.PdfDict{"N": nValue}
				if state != "" {
					newAnnot["AS"] = state
				}
				alloc.AllocAt(annotRef.ObjectNum, newAnnot)
				count++
				continue
			}

			// /AP exists. Strip /D and /R (PDF/A §6.3.3 test 2 allows
			// only /N) and promote /N to a state sub-dict when the annot
			// is a Widget/Btn (test 3).
			if patched, state, changed := normalizeAP(ctx, doc, annotDict); changed {
				newAnnot := make(ast.PdfDict, len(annotDict)+2)
				maps.Copy(newAnnot, annotDict)
				newAnnot["AP"] = patched
				// ISO 32000-1, Table 168 requires /AS once /AP holds
				// subdictionaries: without it a viewer has no way to pick
				// which of the states to draw.
				if state != "" {
					newAnnot["AS"] = state
				}
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
//
// The /AP entry is resolved rather than type-asserted: any object may be given
// as an indirect reference (ISO 32000-1, 7.3.10), and asserting the dictionary
// directly made every such annotation look as if it had no appearance at all.
// The fixer then replaced a real appearance with a blank form.
//
// # Corpus evidence
//
//   - pdfjs bug1778692.pdf: 36 of 63 widgets write /AP indirectly, and
//     remediation blanked them. The appearance streams that drew text fell
//     from 11 to 2, and 147 characters of filled-in form values disappeared
//     from a document veraPDF then called conformant.
func needsAppearance(ctx context.Context, doc *document.Document, annotDict ast.PdfDict) bool {
	if _, hasAP := annotDict["AP"]; !hasAP {
		return true
	}
	apDict, err := document.ResolveDict(ctx, doc, annotDict["AP"])
	if err != nil || apDict == nil {
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
// rather than a state sub-dict, wraps /N into a state dict (§6.3.3 test 3).
// Returns (nil, "", false) when no change is needed, otherwise the new /AP
// and the appearance state the caller must record in /AS, empty when the
// wrap did not happen.
func normalizeAP(ctx context.Context, doc *document.Document, annotDict ast.PdfDict) (ast.PdfDict, ast.PdfName, bool) {
	// Resolved, not type-asserted: an indirect /AP (ISO 32000-1, 7.3.10) used to
	// skip this normalisation entirely, so /D and /R survived where 6.3.3 test 2
	// allows only /N.
	apDict, err := document.ResolveDict(ctx, doc, annotDict["AP"])
	if err != nil || apDict == nil {
		return nil, "", false
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

	// Best-effort: an unreadable /Subtype yields an empty name; only the
	// Widget/Btn branch below acts on it.
	subtype, _ := document.ResolveName(ctx, doc, annotDict["Subtype"])
	var state ast.PdfName
	if subtype == "Widget" && inheritedFieldType(ctx, doc, annotDict) == "Btn" {
		if ref, isRef := newAP["N"].(ast.PdfIndirectRef); isRef {
			if stream, err := doc.Resolve(ctx, ref); err == nil {
				if _, isStream := stream.(ast.PdfStream); isStream {
					state = appearanceState(ctx, doc, annotDict)
					newAP["N"] = ast.PdfDict{state: ref}
					changed = true
				}
			}
		}
	}
	if !changed {
		return nil, "", false
	}
	return newAP, state, true
}

// inheritedFieldType returns a widget's form field type, following the
// /Parent chain.
//
// /FT is inheritable (ISO 32000-1, 12.7.3.1), and a widget that is a child of
// its field carries none of its own. Reading it off the annotation alone left
// every such button with an /AP /N stream where ISO 19005-2, 6.3.3 test 3
// requires a subdictionary.
//
// # Corpus evidence
//
//   - govdocs thread_400_400580: reading /FT off the annotation alone left
//     every button with an /AP /N stream where ISO 19005-2, 6.3.3 test 3
//     requires a subdictionary.
func inheritedFieldType(ctx context.Context, doc *document.Document, annotDict ast.PdfDict) ast.PdfName {
	dict := annotDict
	seen := make(map[int]bool)
	for depth := 0; depth < maxFieldParentDepth && dict != nil; depth++ {
		if ft, err := document.ResolveName(ctx, doc, dict["FT"]); err == nil && ft != "" {
			return ft
		}
		_, parent, ok := document.ResolveFieldParent(ctx, doc, dict, seen)
		if !ok {
			return ""
		}
		dict = parent
	}
	return ""
}

// appearanceState names the sub-dictionary key the wrapped stream takes.
// A state the annotation already selects is kept so the viewer keeps drawing
// what it drew before; otherwise the off state is the neutral choice
// (ISO 32000-1, 12.7.4.2.3).
func appearanceState(ctx context.Context, doc *document.Document, annotDict ast.PdfDict) ast.PdfName {
	if as, err := document.ResolveName(ctx, doc, annotDict["AS"]); err == nil && as != "" {
		return as
	}
	return "Off"
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

// maxFieldParentDepth bounds the walk up a form field's /Parent chain. This is
// an anti-DoS guard, not a spec limit: ISO 32000-1, 12.7.3.1 puts no depth on
// the field tree, but a crafted file can chain parents indefinitely and a real
// form nests a handful at most.
//
// The same ascent is bounded in mutations (three constants), in
// annotation/acroform.go (64) and in compliance/annotations.go (100). See
// mutations/acroform_split_merge.go for the measurement that makes the
// difference unreachable, and [document.ResolveFieldParent] for the visited set
// that ends a cyclic chain.
const maxFieldParentDepth = 32

// rendersOwnValue reports whether a viewer would draw this annotation from text
// it already carries rather than from a stored appearance.
//
// Two kinds do. Text and choice fields are the variable-text fields of ISO
// 32000-1, 12.7.3.3: their appearance is generated from /V and /DA when none is
// stored, and both /FT and /V are inheritable (12.7.3.1), so the answer can live
// on an ancestor. A free text annotation displays the text in /Contents through
// the same /DA machinery (12.5.6.6), so a blank appearance erases it too.
//
// # Corpus evidence
//
//   - pdfjs issue16021.pdf: its whole page is a /FreeText reading "Hello
//     World", and it came out of remediation blank.
func rendersOwnValue(ctx context.Context, doc *document.Document, annotDict ast.PdfDict, subtype ast.PdfName) bool {
	if subtype == "FreeText" {
		return hasRenderableValue(ctx, doc, annotDict["Contents"])
	}
	// A line annotation replicates /Contents as a caption on the line when /Cap
	// is true (ISO 32000-1, 12.5.6.7, Table 175). Without it, /Contents is only
	// an alternate description and nothing is drawn from it.
	if subtype == "Line" {
		if !annotationCaptionEnabled(ctx, doc, annotDict) {
			return false
		}
		return hasRenderableValue(ctx, doc, annotDict["Contents"])
	}
	if subtype != "Widget" {
		return false
	}

	dict := annotDict
	var fieldType ast.PdfName
	var value ast.PdfObject
	seen := make(map[int]bool)
	for depth := 0; depth < maxFieldParentDepth && dict != nil; depth++ {
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
	return hasRenderableValue(ctx, doc, value)
}

// hasRenderableValue reports whether a field value would put glyphs on the page.
// An absent, null or empty value draws nothing, so a blank appearance loses
// nothing and the fixer may supply one.
func hasRenderableValue(ctx context.Context, doc *document.Document, value ast.PdfObject) bool {
	if value == nil {
		return false
	}
	switch v := value.(type) {
	case ast.PdfNull:
		return false
	case ast.PdfString:
		return len(document.DecodePdfString(v)) > 0
	case ast.PdfArray:
		// A multi-select choice field holds its selection as an array.
		return len(v) > 0
	case ast.PdfIndirectRef:
		resolved, err := doc.Resolve(ctx, v)
		if err != nil {
			return false
		}
		return hasRenderableValue(ctx, doc, resolved)
	default:
		return false
	}
}

// annotationCaptionEnabled reports whether a line annotation asks for its
// /Contents to be drawn as a caption (ISO 32000-1, Table 175 /Cap).
func annotationCaptionEnabled(ctx context.Context, doc *document.Document, annotDict ast.PdfDict) bool {
	resolved, err := document.ResolveObject(ctx, doc, annotDict["Cap"])
	if err != nil {
		return false
	}
	enabled, ok := resolved.(ast.PdfBool)
	return ok && bool(enabled)
}
