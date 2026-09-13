package compliance

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// checkAnnotationNesting validates PDF/UA-1 §7.18.x annotation nesting rules:
//   - §7.18.1 t01: Non-exempt annotations must be nested within an Annot tag
//   - §7.18.2 t01: TrapNet annotations are forbidden
//   - §7.18.4 t01: Widget annotations must be nested within a Form tag
//   - §7.18.5 t01: Link annotations must be nested within a Link tag
//   - §7.18.8 t01: PrinterMark annotations must NOT be in the logical structure
func checkAnnotationNesting(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if profile != "PDF/UA-1" {
		return nil
	}

	// The rules judge an annotation by the structure element enclosing it, which
	// ISO 32000-1, 14.7.4.4 records in the structure parent tree under the
	// annotation's /StructParent key. veraPDF's GFPDAnnot.getParentDictionary
	// reads that entry and nothing else, so an annotation that an OBJR references
	// but that carries no /StructParent has no enclosing element at all.
	treeRoot, err := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])
	if err != nil {
		doc.Logger.Debug("compliance.accessibility: cannot resolve structure tree root", "error", err)
	}
	typeState := &walkState{doc: doc, roleMap: buildRoleMapIndex(ctx, doc, treeRoot)}

	pages := collectPages(ctx, doc, catalog)
	var results []Violation

	for _, page := range pages {
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

		for _, annotObj := range annots {
			if ctx.Err() != nil {
				return results
			}

			annot, err := document.ResolveDict(ctx, doc, annotObj)
			if err != nil || annot == nil {
				continue
			}

			subtype, _ := document.ResolveName(ctx, doc, annot["Subtype"])

			// Skip hidden or outside-crop-box annotations (all rules share these exemptions).
			if isAnnotHidden(ctx, doc, annot) || isAnnotOutsideCropBox(ctx, doc, annot, page) {
				continue
			}

			// A Popup is not exempt, with or without a /Parent. ISO 14289-1
			// §7.18.1 t1 excludes only Widget, PrinterMark and Link, and
			// veraPDF follows the clause: measured on annotstruct_ua2, whose
			// two Popups both carry a /Parent and whose unnested one draws the
			// rule. litho used to exempt a parented Popup as auxiliary to its
			// markup annotation, which the clause does not say.

			parentType, nestType := annotStructParentTypes(ctx, typeState, treeRoot, annot)

			switch subtype {
			case "TrapNet":
				// §7.18.2: TrapNet annotations are forbidden.
				results = append(results, fail(Rule_14289_1_7_18_2_t01,
					"TrapNet annotation exists")...)
				// §7.18.1 t01: TrapNet is not exempt from the Annot-nesting
				// requirement; veraPDF exempts only Widget, PrinterMark and Link.
				if nestType != "Annot" {
					results = append(results, failf(Rule_14289_1_7_18_1_t01,
						"%s annotation nested within %s instead of Annot", subtype, nestingDesc(nestType))...)
				}

			case "PrinterMark":
				// §7.18.8: PrinterMark must NOT be in logical structure. veraPDF
				// tests structParentType, the enclosing element's /S as written.
				if parentType != "" {
					results = append(results, fail(Rule_14289_1_7_18_8_t01,
						"PrinterMark annotation is included in logical structure")...)
				}

			case "Widget":
				// §7.18.4: Widget must be nested within Form tag.
				if nestType != "Form" {
					results = append(results, failf(Rule_14289_1_7_18_4_t01,
						"Widget annotation nested within %s instead of Form", nestingDesc(nestType))...)
				}

			case "Link":
				// §7.18.5: Link must be nested within Link tag.
				if nestType != "Link" {
					results = append(results, failf(Rule_14289_1_7_18_5_t01,
						"Link annotation nested within %s instead of Link", nestingDesc(nestType))...)
				}

			default:
				// §7.18.1: All other annotations must be nested within Annot tag.
				if nestType != "Annot" {
					results = append(results, failf(Rule_14289_1_7_18_1_t01,
						"%s annotation nested within %s instead of Annot", subtype, nestingDesc(nestType))...)
				}
			}
		}
	}

	return results
}

// nestingDesc returns a human-readable description for the annotation's
// enclosing structure element standard type, or "Artifact" if not in structure.
func nestingDesc(stdType string) string {
	if stdType == "" {
		return "Artifact"
	}
	return stdType
}

// checkPDFUA1FormRole validates ISO 14289-1 §7.18.4 t2: a Form structure
// element that omits a PrintField Role attribute (ISO 32000-1 Table 348) shall
// have exactly one child, an object reference identifying the widget annotation.
// veraPDF keys this on the SEForm structure element (test
// "roleAttribute != null || hasOneInteractiveChild == true"), so a Form with a
// PrintField Role attribute, or whose only child is a single widget object
// reference, is conformant and does not fire.
func checkPDFUA1FormRole(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if profile != "PDF/UA-1" {
		return nil
	}
	treeRoot, err := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])
	if err != nil || treeRoot == nil {
		return nil
	}
	var results []Violation
	walkStructureElements(ctx, doc, treeRoot, func(elem ast.PdfDict, path string) {
		typeName, _ := document.ResolveName(ctx, doc, elem["S"])
		if typeName != "Form" {
			return
		}
		if formHasPrintFieldRole(ctx, doc, elem) || formHasOneInteractiveChild(ctx, doc, elem) {
			return
		}
		results = append(results, fail(Rule_14289_1_7_18_4_t02,
			"Form element at "+path+" omits a Role attribute and does not have exactly one widget child")...)
	})
	return results
}

// formHasPrintFieldRole reports whether a Form structure element carries a
// PrintField attribute owner with a /Role entry (ISO 32000-1 Table 348), which
// satisfies veraPDF's roleAttribute != null. The /A entry may be a single
// attribute dictionary or an array mixing attribute dictionaries and revision
// numbers; a /Role under any /O /PrintField owner counts.
func formHasPrintFieldRole(ctx context.Context, doc *document.Document, elem ast.PdfDict) bool {
	aObj, ok := elem["A"]
	if !ok {
		return false
	}
	if arr, err := document.ResolveArray(ctx, doc, aObj); err == nil {
		for _, a := range arr {
			if attrIsPrintFieldRole(ctx, doc, a) {
				return true
			}
		}
		return false
	}
	return attrIsPrintFieldRole(ctx, doc, aObj)
}

func attrIsPrintFieldRole(ctx context.Context, doc *document.Document, obj ast.PdfObject) bool {
	d, err := document.ResolveDict(ctx, doc, obj)
	if err != nil || d == nil {
		return false
	}
	if owner, _ := document.ResolveName(ctx, doc, d["O"]); owner != "PrintField" {
		return false
	}
	_, hasRole := d["Role"]
	return hasRole
}

// formHasOneInteractiveChild reports whether a Form structure element's /K
// resolves to exactly one child that is an object reference identifying a widget
// annotation (veraPDF hasOneInteractiveChild). A Form with multiple children, or
// a single non-widget child, does not satisfy it.
func formHasOneInteractiveChild(ctx context.Context, doc *document.Document, elem ast.PdfDict) bool {
	kObj, ok := elem["K"]
	if !ok {
		return false
	}
	if arr, err := document.ResolveArray(ctx, doc, kObj); err == nil {
		return len(arr) == 1 && isWidgetOBJR(ctx, doc, arr[0])
	}
	return isWidgetOBJR(ctx, doc, kObj)
}

// annotStructParentTypes returns the /S of the structure element enclosing an
// annotation and the standard type that element stands for, both "" when the
// annotation has no enclosing element.
//
// The element is the one [annotStructParent] finds. Its standard type is
// resolved as veraPDF's PDStructElem.getStructureElementStandardType resolves
// it for ISO 32000-1: the role map applies before the standard-name test, so a
// standard type the role map remaps no longer stands for itself.
func annotStructParentTypes(ctx context.Context, state *walkState, treeRoot, annot ast.PdfDict) (ast.PdfName, string) {
	elem := annotStructParent(ctx, state.doc, treeRoot, annot)
	if elem == nil {
		return "", ""
	}
	typeName, _ := document.ResolveName(ctx, state.doc, elem["S"])
	if typeName == "" {
		return "", ""
	}
	return typeName, string(iso32005StandardName(ctx, state, elem, typeName))
}

// numberTreeLookupObj resolves a key in a number tree to its raw value object.
// It mirrors [numberTreeLookup] but returns the value as-is rather than forcing
// an array, because an annotation /StructParent entry references a single
// structure element. Depth is capped by [maxEFNameTreeDepth].
func numberTreeLookupObj(ctx context.Context, doc *document.Document, node ast.PdfDict, key, depth int, seen map[int]bool) ast.PdfObject {
	if depth > maxEFNameTreeDepth || ctx.Err() != nil {
		return nil
	}
	if numsObj, ok := node["Nums"]; ok {
		nums, err := document.ResolveArray(ctx, doc, numsObj)
		if err == nil {
			for i := 0; i+1 < len(nums); i += 2 {
				k, kerr := document.ResolveInt(ctx, doc, nums[i])
				if kerr != nil || int(k) != key {
					continue
				}
				return nums[i+1]
			}
		}
	}
	if kidsObj, ok := node["Kids"]; ok {
		kids, err := document.ResolveArray(ctx, doc, kidsObj)
		if err == nil {
			for _, kid := range kids {
				if ref, ok := kid.(ast.PdfIndirectRef); ok {
					if seen[ref.ObjectNum] {
						continue
					}
					seen[ref.ObjectNum] = true
				}
				kd, kerr := document.ResolveDict(ctx, doc, kid)
				if kerr != nil || kd == nil {
					continue
				}
				if v := numberTreeLookupObj(ctx, doc, kd, key, depth+1, seen); v != nil {
					return v
				}
			}
		}
	}
	return nil
}

// buildAnnotStructTypeMapNS walks the structure tree and returns a map from
// annotation object number to the PDF 2.0 namespace-resolved standard type of
// the enclosing structure element, resolving element types through
// [nsRoleResolver] so a custom type role-mapped to a standard type is
// recognised.
func buildAnnotStructTypeMapNS(ctx context.Context, doc *document.Document, treeRoot ast.PdfDict, r *nsRoleResolver) map[int]string {
	result := make(map[int]string)
	walkStructureElements(ctx, doc, treeRoot, func(elem ast.PdfDict, _ string) {
		stdType, _, ok := r.elementStandardType(ctx, doc, elem)
		if !ok {
			return
		}
		if kObj, has := elem["K"]; has {
			collectOBJRWithType(ctx, doc, kObj, stdType, result)
		}
	})
	return result
}

// checkPDFUA2AnnotStructNesting validates PDF/UA-2 annotation-in-structure
// nesting rules that depend on the enclosing element's namespace-resolved type:
//   - §8.10.1 t1: a field widget annotation shall be enclosed by a Form element.
//   - §8.9.2.4.16 t1: a Watermark annotation used as real content shall be
//     enclosed by an Annot element.
//   - §8.2.5.20 t1: a Link annotation shall be enclosed by a Link or Reference
//     element.
//
// Annotations that are artifacts (absent from the structure tree) are exempt
// from both, matching the rules' structParentType/isArtifact conditions.
func checkPDFUA2AnnotStructNesting(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if PDFUAPart(profile) != 2 {
		return nil
	}
	treeRoot, err := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])
	if err != nil || treeRoot == nil {
		return nil
	}
	structMap := buildAnnotStructTypeMapNS(ctx, doc, treeRoot, newNSRoleResolver(ctx, doc, treeRoot))

	var results []Violation
	for _, page := range collectPages(ctx, doc, catalog) {
		if ctx.Err() != nil {
			return results
		}
		annots, _ := document.ResolveArray(ctx, doc, page["Annots"])
		for _, annotObj := range annots {
			objNum := 0
			if ref, ok := annotObj.(ast.PdfIndirectRef); ok {
				objNum = ref.ObjectNum
			}
			annot, _ := document.ResolveDict(ctx, doc, annotObj)
			if annot == nil {
				continue
			}
			subtype, _ := document.ResolveName(ctx, doc, annot["Subtype"])

			// §8.9.2.3 t1: a markup annotation shall be enclosed within an Annot
			// structure element. ISO 14289-2 §8.9.2.3 constrains the enclosing
			// element, so it has nothing to say about an annotation that no
			// element encloses: such an annotation is an artifact, and clause 8.9
			// exempts artifacts from the structure requirements. An annotation
			// absent from the structure tree therefore passes, and only one
			// enclosed by an element other than Annot fails.
			if markupAnnotationSubtypes[subtype] {
				stdType, inTree := structMap[objNum]
				if inTree && stdType != "Annot" {
					results = append(results, failf(Rule_14289_2_8_9_2_3_t01,
						"%s markup annotation enclosed by %s instead of Annot",
						subtype, nestingDesc(stdType))...)
				}
				continue
			}

			// structParentType == null (not in the tree, i.e. an artifact) is
			// exempt for the remaining rules.
			stdType, inTree := structMap[objNum]
			if !inTree {
				continue
			}

			switch subtype {
			case "Widget":
				// isFieldWidget: a widget associated with an AcroForm field
				// carries /FT (merged field+widget) or /Parent (a child of a field).
				_, hasFT := annot["FT"]
				_, hasParent := annot["Parent"]
				if (hasFT || hasParent) && stdType != "Form" {
					results = append(results, failf(Rule_14289_2_8_10_1_t01,
						"field Widget annotation enclosed by %s instead of Form", nestingDesc(stdType))...)
				}
			case "Watermark":
				if stdType != "Annot" {
					results = append(results, failf(Rule_14289_2_8_9_2_4_16_t01,
						"Watermark annotation enclosed by %s instead of Annot", nestingDesc(stdType))...)
				}
			case "Link":
				// §8.2.5.20 t1: a Link annotation shall be enclosed by a Link or
				// Reference element. Artifact is excluded explicitly because it is
				// a PDF 2.0 standard type and so resolves here, where veraPDF
				// exempts it through its separate isArtifact condition.
				if stdType != "Link" && stdType != "Reference" && stdType != "Artifact" {
					results = append(results, failf(Rule_14289_2_8_2_5_20_t01,
						"Link annotation enclosed by %s instead of Link or Reference",
						nestingDesc(stdType))...)
				}
			}
		}
	}
	return results
}

// LoadRoleMap loads the /RoleMap dictionary from the structure tree root. It is
// exported so a remediation fixer resolves a structure element's standard type
// exactly as the validator does, rather than re-deriving the mapping and risking
// drift.
func LoadRoleMap(ctx context.Context, doc *document.Document, structTreeRoot ast.PdfDict) map[ast.PdfName]ast.PdfName {
	rmObj, ok := structTreeRoot["RoleMap"]
	if !ok {
		return nil
	}
	rm, err := document.ResolveDict(ctx, doc, rmObj)
	if err != nil || rm == nil {
		return nil
	}
	mapping := make(map[ast.PdfName]ast.PdfName, len(rm))
	for key, val := range rm {
		targetName, err := document.ResolveName(ctx, doc, val)
		if err == nil && targetName != "" {
			mapping[key] = targetName
		}
	}
	return mapping
}

// resolveStandardType follows the role map chain to find the standard type.
// Returns the original name if already standard or if no mapping exists.
func resolveStandardType(name ast.PdfName, roleMap map[ast.PdfName]ast.PdfName, profile string) ast.PdfName {
	if isStandardStructType(name, profile) {
		return name
	}
	visited := map[ast.PdfName]bool{name: true}
	current := name
	for range 50 {
		target, ok := roleMap[current]
		if !ok {
			return current
		}
		if isStandardStructType(target, profile) {
			return target
		}
		if visited[target] {
			return current // circular
		}
		visited[target] = true
		current = target
	}
	return current
}

// collectOBJRWithType finds OBJR dicts in /K children and records the annotation
// object numbers with their enclosing element's standard type.
func collectOBJRWithType(ctx context.Context, doc *document.Document, kObj ast.PdfObject, stdType string, dest map[int]string) {
	arr, err := document.ResolveArray(ctx, doc, kObj)
	if err != nil {
		recordOBJRType(ctx, doc, kObj, stdType, dest)
		return
	}
	for _, item := range arr {
		recordOBJRType(ctx, doc, item, stdType, dest)
	}
}

// recordOBJRType checks if obj is an OBJR dict and records the annotation
// object number with the given standard type.
func recordOBJRType(ctx context.Context, doc *document.Document, obj ast.PdfObject, stdType string, dest map[int]string) {
	dict, err := document.ResolveDict(ctx, doc, obj)
	if err != nil || dict == nil {
		return
	}
	typeName, _ := document.ResolveName(ctx, doc, dict["Type"])
	if typeName != "OBJR" {
		return
	}
	if ref, ok := dict["Obj"].(ast.PdfIndirectRef); ok {
		dest[ref.ObjectNum] = stdType
	}
}
