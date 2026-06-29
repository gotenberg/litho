package compliance

import (
	"context"
	"fmt"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

func checkFigureAltText(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	rule := figureAltRule(profile)

	treeRoot, err := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])
	if err != nil || treeRoot == nil {
		return nil
	}

	roleMap := LoadRoleMap(ctx, doc, treeRoot)

	var results []Violation
	walkStructureElements(ctx, doc, treeRoot, func(elem ast.PdfDict, path string) {
		if FigureNeedsAltText(ctx, doc, elem, roleMap, profile) {
			results = append(results, Violation{Rule: rule, Context: path})
		}
	})

	return results
}

// FigureNeedsAltText reports whether elem is a Figure structure element, directly
// or through the role map, that lacks the alternate text PDF/UA requires. A
// remediation fixer calls it so it repairs exactly the figures this validator
// flags, with no drift. roleMap comes from [LoadRoleMap]; profile selects the
// PDF/UA-1 §7.3 or PDF/UA-2 §8.2.5.28.2 requirement.
func FigureNeedsAltText(ctx context.Context, doc *document.Document, elem ast.PdfDict, roleMap map[ast.PdfName]ast.PdfName, profile string) bool {
	typeName, _ := document.ResolveName(ctx, doc, elem["S"])
	// Resolve through RoleMap to catch non-standard types mapped to Figure.
	if resolveStandardType(typeName, roleMap, profile) != "Figure" {
		return false
	}
	return !figureSatisfiesAltText(ctx, doc, elem, profile)
}

// figureSatisfiesAltText reports whether a Figure already carries the alternate
// text its profile requires. PDF/UA-1 §7.3 needs a non-empty /Alt or a present
// /ActualText; PDF/UA-2 §8.2.5.28.2 accepts a present /Alt, a present
// /ActualText, or a Caption child (presence alone suffices, even when empty).
func figureSatisfiesAltText(ctx context.Context, doc *document.Document, elem ast.PdfDict, profile string) bool {
	if profile == "PDF/UA-1" {
		return hasNonEmptyStringKey(ctx, doc, elem, "Alt") ||
			hasStringKeyPresent(ctx, doc, elem, "ActualText")
	}
	return hasStringKeyPresent(ctx, doc, elem, "Alt") ||
		hasStringKeyPresent(ctx, doc, elem, "ActualText") ||
		figureHasCaptionChild(ctx, doc, elem)
}

func checkArtifactTagging(ctx context.Context, _ *document.Document, _ ast.PdfDict, _ string) []Violation {
	// Artifact tagging checks require deep content stream analysis to determine
	// if headers/footers/page numbers are properly marked as Artifact. This is
	// a heuristic check that cannot be fully validated without rendering, so
	// no violations are emitted. A future enhancement may add heuristic
	// detection of repeated header/footer content.
	return nil
}

// markupAnnotationSubtypes lists the markup annotation types per ISO
// 32000-2, 12.5.6.2 (annotations that carry a /T entry and convey user
// commentary). RichMedia, Screen, 3D, Movie, and the other non-markup types
// are deliberately absent. Projection is also absent: ISO 32000-2 §12.5.6.2
// does not classify it as a markup annotation, and veraPDF's markup model
// (PDMarkupAnnot) omits it, so §8.9.2.3 t1 (markup-in-Annot nesting) does not
// apply to a Projection annotation.
//
//nolint:gochecknoglobals // static spec table.
var markupAnnotationSubtypes = map[ast.PdfName]bool{
	"Text": true, "FreeText": true, "Line": true, "Square": true,
	"Circle": true, "Polygon": true, "PolyLine": true, "Highlight": true,
	"Underline": true, "Squiggly": true, "StrikeOut": true, "Caret": true,
	"Stamp": true, "Ink": true, "FileAttachment": true, "Sound": true,
	"Redact": true,
}

func checkAnnotationAltText(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	// PDF/UA-2 has no generic "markup annotation needs Contents/Alt" rule: it
	// keys alternate descriptions per annotation subtype (§8.9.2.4.x, handled by
	// checkPDFUA2AnnotationConstraints), and §8.9.2.3 t1 is a nesting rule
	// (markup must be enclosed in Annot, handled by checkPDFUA2AnnotStructNesting),
	// not an alt-text rule. This UA-1 §7.18.1 t2 model does not apply under UA-2.
	if profile == "PDF/UA-2" {
		return nil
	}

	rule := annotAltRule(profile)

	// Build a map from annotation object number to whether the enclosing
	// structure element has a non-empty /Alt entry.
	structAltMap := buildAnnotStructAltMap(ctx, doc, catalog)

	pages := collectPages(ctx, doc, catalog)
	var results []Violation

	for pageIdx, page := range pages {
		if ctx.Err() != nil {
			return results
		}

		annotsObj, ok := page["Annots"]
		if !ok {
			continue
		}

		annots, err := document.ResolveArray(ctx, doc, annotsObj)
		if err != nil {
			doc.Logger.Debug("compliance.accessibility: cannot resolve annots array", "error", err)
			continue
		}

		for annotIdx, annotObj := range annots {
			if ctx.Err() != nil {
				return results
			}

			// Grab the object number before resolving, for structure tree lookup.
			annotObjNum := 0
			if ref, ok := annotObj.(ast.PdfIndirectRef); ok {
				annotObjNum = ref.ObjectNum
			}

			annot, err := document.ResolveDict(ctx, doc, annotObj)
			if err != nil || annot == nil {
				continue
			}

			subtype, _ := document.ResolveName(ctx, doc, annot["Subtype"])

			// Widget annotations are excluded from this check. Link is NOT
			// excluded under PDF/UA-1: ISO 14289-1 §7.18.1 t2 exempts only
			// Widget/hidden/out-of-cropbox, so a Link lacking both /Contents
			// and an enclosing-element /Alt fails (verified live on the Link in
			// 6-3-2-t01-fail-b remediated to PDF/UA-1). Under PDF/UA-2 the
			// markup-subtype filter below still exempts Link (not a markup type).
			if subtype == "Widget" {
				continue
			}
			// A Popup inherits content from its parent markup annotation, so a
			// Popup WITH a /Parent is exempt. A parentless Popup has no source
			// of content and must itself satisfy the Contents-or-Alt
			// requirement: ISO 14289-1 §7.18.1 t2 exempts only Widget, hidden,
			// and out-of-cropbox annotations, and a parentless Popup is none of
			// those. Under PDF/UA-2 the markup-subtype filter below still exempts
			// every Popup (Popup is not a markup type).
			if subtype == "Popup" {
				if _, hasParent := annot["Parent"]; hasParent {
					continue
				}
			}

			// ISO 14289-2 §8.9.2.3 covers markup annotations only, so a
			// non-markup subtype is out of its scope. A /Contents-less
			// RichMedia annotation fails its per-subtype description rule
			// (§8.9.2.4.19), never §8.9.2.3. PDF/UA-1 §7.18.1 keeps the
			// wider scope.
			if profile == "PDF/UA-2" && !markupAnnotationSubtypes[subtype] {
				continue
			}

			// Skip hidden annotations or those outside the crop box.
			if isAnnotHidden(ctx, doc, annot) || isAnnotOutsideCropBox(ctx, doc, annot, page) {
				continue
			}

			// Check /Contents key: must be present and non-empty.
			if contentsObj, hasContents := annot["Contents"]; hasContents {
				contentsStr, err := document.ResolveString(ctx, doc, contentsObj)
				if err == nil && strings.TrimSpace(string(contentsStr)) != "" {
					continue
				}
			}

			// Annotation lacks /Contents. Check the enclosing structure element for /Alt.
			if annotObjNum > 0 && structAltMap[annotObjNum] {
				continue
			}

			loc := fmt.Sprintf("page %d, annot %d (/%s)", pageIdx, annotIdx, subtype)
			results = append(results, Violation{Rule: rule, Context: loc})
		}
	}

	return results
}

// checkAnnotContentsAltMatch validates PDF/UA-2 §8.9.4.2 t1: when an annotation
// has a /Contents entry and its directly enclosing structure element has an /Alt
// entry, the two decoded values shall be identical.
func checkAnnotContentsAltMatch(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if profile != "PDF/UA-2" {
		return nil
	}
	altValues := buildAnnotStructAltValueMap(ctx, doc, catalog)
	if len(altValues) == 0 {
		return nil
	}

	var results []Violation
	for pageIdx, page := range collectPages(ctx, doc, catalog) {
		if ctx.Err() != nil {
			return results
		}
		annots, _ := document.ResolveArray(ctx, doc, page["Annots"])
		for annotIdx, annotObj := range annots {
			objNum := 0
			if ref, ok := annotObj.(ast.PdfIndirectRef); ok {
				objNum = ref.ObjectNum
			}
			alt, hasAlt := altValues[objNum]
			if !hasAlt {
				continue
			}
			annot, err := document.ResolveDict(ctx, doc, annotObj)
			if err != nil || annot == nil {
				continue
			}
			contentsObj, hasContents := annot["Contents"]
			if !hasContents {
				continue
			}
			contentsStr, err := document.ResolveString(ctx, doc, contentsObj)
			if err != nil {
				continue
			}
			if document.DecodePdfString(contentsStr) != alt {
				results = append(results, failf(Rule_14289_2_8_9_4_2_t01,
					"page %d, annot %d: /Contents differs from enclosing structure element /Alt", pageIdx, annotIdx)...)
			}
		}
	}
	return results
}

// buildAnnotStructAltValueMap maps an annotation object number to the decoded
// /Alt value of its directly enclosing structure element. Mirror of
// [buildAnnotStructAltMap] that captures the value for the §8.9.4.2 comparison.
func buildAnnotStructAltValueMap(ctx context.Context, doc *document.Document, catalog ast.PdfDict) map[int]string {
	treeRoot, err := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])
	if err != nil || treeRoot == nil {
		return nil
	}
	result := make(map[int]string)
	walkStructureElements(ctx, doc, treeRoot, func(elem ast.PdfDict, _ string) {
		altObj, ok := elem["Alt"]
		if !ok {
			return
		}
		altStr, err := document.ResolveString(ctx, doc, altObj)
		if err != nil {
			return
		}
		kObj, ok := elem["K"]
		if !ok {
			return
		}
		objNums := make(map[int]bool)
		collectOBJRAnnotObjNums(ctx, doc, kObj, objNums)
		alt := document.DecodePdfString(altStr)
		for n := range objNums {
			result[n] = alt
		}
	})
	return result
}

// buildAnnotStructAltMap walks the structure tree and returns a set of annotation
// object numbers whose enclosing structure element has a non-empty /Alt entry.
// PDF structure elements reference annotations via OBJR (object reference) dicts
// in their /K children: { /Type /OBJR /Obj <indirect ref to annotation> }.
func buildAnnotStructAltMap(ctx context.Context, doc *document.Document, catalog ast.PdfDict) map[int]bool {
	treeRoot, err := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])
	if err != nil || treeRoot == nil {
		return nil
	}

	result := make(map[int]bool)
	walkStructureElements(ctx, doc, treeRoot, func(elem ast.PdfDict, _ string) {
		// Only process elements with non-empty /Alt.
		if !hasNonEmptyStringKey(ctx, doc, elem, "Alt") {
			return
		}

		// Scan /K children for OBJR dicts pointing to annotations.
		kObj, ok := elem["K"]
		if !ok {
			return
		}
		collectOBJRAnnotObjNums(ctx, doc, kObj, result)
	})

	return result
}

// collectOBJRAnnotObjNums finds OBJR dicts in the /K children of a structure element
// and records the object numbers of their /Obj references.
func collectOBJRAnnotObjNums(ctx context.Context, doc *document.Document, kObj ast.PdfObject, dest map[int]bool) {
	// /K can be a single item or an array.
	arr, err := document.ResolveArray(ctx, doc, kObj)
	if err != nil {
		// Try as a single dict.
		checkOBJR(ctx, doc, kObj, dest)
		return
	}
	for _, item := range arr {
		checkOBJR(ctx, doc, item, dest)
	}
}

// checkOBJR checks if obj is an OBJR dict and records the /Obj object number.
func checkOBJR(ctx context.Context, doc *document.Document, obj ast.PdfObject, dest map[int]bool) {
	dict, err := document.ResolveDict(ctx, doc, obj)
	if err != nil || dict == nil {
		return
	}
	typeName, _ := document.ResolveName(ctx, doc, dict["Type"])
	if typeName != "OBJR" {
		return
	}
	// /Obj is an indirect reference to the annotation.
	if ref, ok := dict["Obj"].(ast.PdfIndirectRef); ok {
		dest[ref.ObjectNum] = true
	}
}

// checkWidgetAA validates that Widget annotations do not have /AA (additional actions)
// entries. For PDF/UA-1, the presence of /AA on a Widget is forbidden per §7.18.
// For PDF/UA-2, if /AA is present, the Widget must have a /Contents entry (§8.10.2.3 t02).
func checkWidgetAA(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	pages := collectPages(ctx, doc, catalog)
	var results []Violation

	for pageIdx, page := range pages {
		if ctx.Err() != nil {
			return results
		}

		annotsObj, ok := page["Annots"]
		if !ok {
			continue
		}

		annots, err := document.ResolveArray(ctx, doc, annotsObj)
		if err != nil {
			doc.Logger.Debug("compliance.accessibility: cannot resolve annots array", "error", err)
			continue
		}

		for annotIdx, annotObj := range annots {
			if ctx.Err() != nil {
				return results
			}

			annot, err := document.ResolveDict(ctx, doc, annotObj)
			if err != nil || annot == nil {
				continue
			}

			subtype, _ := document.ResolveName(ctx, doc, annot["Subtype"])
			if subtype != "Widget" {
				continue
			}

			_, hasAA := annot["AA"]
			if !hasAA {
				continue
			}

			// Only PDF/UA-2 restricts a Widget that carries /AA (§8.10.2.3 t02:
			// it must also carry /Contents). PDF/UA-1 has no Widget-/AA rule:
			// its 7.18.1 t3 is the TU-or-Alt requirement (checked elsewhere),
			// not an /AA prohibition, so firing it here was a false positive.
			if profile != "PDF/UA-2" {
				continue
			}
			// ISO 14289-2 §8.10.2.3 t02 applies to a widget linked into the
			// structure tree. A widget with no /StructParent is not linked, so
			// it has no structure-element alternate description to govern and is
			// exempt.
			if _, inTree := annot["StructParent"]; !inTree {
				continue
			}
			loc := fmt.Sprintf("page %d, widget annot %d", pageIdx+1, annotIdx)
			if _, hasContents := annot["Contents"]; !hasContents {
				results = append(results, Violation{
					Rule:    Rule_14289_2_8_10_2_3_t02,
					Context: loc,
				})
			}
		}
	}

	return results
}
