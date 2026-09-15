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
	// veraPDF builds SEForm from the standard type the role map resolves to
	// (GFSEFactory.createTypedStructElem), so a name mapped to Form is a Form and
	// a Form the role map remaps is not. Measured against veraPDF with both.
	typeState := &walkState{doc: doc, roleMap: buildRoleMapIndex(ctx, doc, treeRoot), classMap: structClassMap(ctx, doc, treeRoot)}
	walkStructureElements(ctx, doc, treeRoot, func(elem ast.PdfDict, path string) {
		typeName, _ := document.ResolveName(ctx, doc, elem["S"])
		if iso32005Key(ctx, typeState, elem, typeName) != "Form" {
			return
		}
		if formHasPrintFieldRole(ctx, typeState, elem) || formHasOneInteractiveChild(ctx, doc, elem) {
			return
		}
		results = append(results, fail(Rule_14289_1_7_18_4_t02,
			"Form element at "+path+" omits a Role attribute and does not have exactly one widget child")...)
	})
	return results
}

// formHasPrintFieldRole reports whether a Form structure element carries a
// PrintField /Role attribute (ISO 32000-1, Table 348), which satisfies veraPDF's
// roleAttribute != null. veraPDF reads it with AttributeHelper.getRole, a
// PrintField-owned name taken from /A and then from the classes /C names in the
// /ClassMap, without inheritance: a /Role written as null or as a string is
// absent, and one a class supplies counts. Measured against veraPDF on each.
func formHasPrintFieldRole(ctx context.Context, state *walkState, elem ast.PdfDict) bool {
	return ownedAttributeName(ctx, state.doc, elem["A"], "PrintField", "Role") != "" ||
		classAttributeName(ctx, state, elem["C"], "PrintField", "Role") != ""
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

// checkPDFUA2AnnotStructNesting validates PDF/UA-2 annotation-in-structure
// nesting rules that depend on the enclosing element's namespace-resolved type:
//   - §8.9.2.3 t1: a markup annotation shall be enclosed by an Annot element.
//   - §8.10.1 t1: a field widget annotation shall be enclosed by a Form element.
//   - §8.9.2.4.16 t1: a Watermark annotation used as real content shall be
//     enclosed by an Annot element.
//   - §8.2.5.20 t1: a Link annotation shall be enclosed by a Link or Reference
//     element.
//
// Each of these rules is gated on veraPDF's structParentType,
// structParentStandardType and isArtifact, all read from the element the
// annotation's /StructParent names in the structure parent tree
// ([annotStructParent]). Clause 8.9 exempts artifacts from the structure
// requirements, so an annotation with no such element, or whose element or one
// of its ancestors is an Artifact, is out of scope. An OBJR that references the
// annotation does not bring it into scope.
//
// It also judges §8.2.5.20 t2 (Link annotations that target different
// locations shall be in separate structure elements), which the validation
// profile tests on every Link annotation without those gates
// ([linkTargetConflict]).
func checkPDFUA2AnnotStructNesting(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if PDFUAPart(profile) != 2 {
		return nil
	}
	treeRoot, err := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])
	if err != nil || treeRoot == nil {
		return nil
	}
	r := newNSRoleResolver(ctx, doc, treeRoot)

	var results []Violation
	linkTargets := make(map[int]map[string]bool)
	for _, page := range collectPages(ctx, doc, catalog) {
		if ctx.Err() != nil {
			return results
		}
		annots, _ := document.ResolveArray(ctx, doc, page["Annots"])
		for _, annotObj := range annots {
			annot, _ := document.ResolveDict(ctx, doc, annotObj)
			if annot == nil {
				continue
			}
			subtype, _ := document.ResolveName(ctx, doc, annot["Subtype"])
			if subtype == "Link" && linkTargetConflict(ctx, doc, treeRoot, annot, linkTargets) {
				results = append(results, fail(Rule_14289_2_8_2_5_20_t02,
					"Link annotation shares its structure element with a Link annotation targeting a different structure destination")...)
			}
			parent, underArtifact := annotStructScope(ctx, doc, treeRoot, r, annot)
			if parent == nil || underArtifact {
				continue
			}
			parentType, _ := document.ResolveName(ctx, doc, parent["S"])
			// structParentStandardType is null for a type that resolves to no
			// standard type, which matches none of the required types.
			stdType, _, _ := r.elementStandardType(ctx, doc, parent)
			encl := stdType
			if encl == "" {
				encl = string(parentType)
			}
			if markupAnnotationSubtypes[subtype] {
				if stdType != "Annot" {
					results = append(results, failf(Rule_14289_2_8_9_2_3_t01,
						"%s markup annotation enclosed by %s instead of Annot", subtype, encl)...)
				}
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
						"field Widget annotation enclosed by %s instead of Form", encl)...)
				}
			case "Watermark":
				if stdType != "Annot" {
					results = append(results, failf(Rule_14289_2_8_9_2_4_16_t01,
						"Watermark annotation enclosed by %s instead of Annot", encl)...)
				}
			case "Link":
				if stdType != "Link" && stdType != "Reference" {
					results = append(results, failf(Rule_14289_2_8_2_5_20_t01,
						"Link annotation enclosed by %s instead of Link or Reference", encl)...)
				}
			}
		}
	}
	return results
}

// annotStructScope returns the structure element enclosing an annotation and
// whether that element sits under an Artifact, the two facts ISO 14289-2 gates
// its annotation rules on. The element is nil when veraPDF's structParentType
// is null: the annotation has no /StructParent, the structure parent tree has
// no entry for it, or the entry's element carries no /S. underArtifact is
// veraPDF's isArtifact for that element, see [structElemUnderArtifact].
func annotStructScope(ctx context.Context, doc *document.Document, treeRoot ast.PdfDict, r *nsRoleResolver, annot ast.PdfDict) (parent ast.PdfDict, underArtifact bool) {
	parent = annotStructParent(ctx, doc, treeRoot, annot)
	if parent == nil {
		return nil, false
	}
	if sType, _ := document.ResolveName(ctx, doc, parent["S"]); sType == "" {
		return nil, false
	}
	return parent, structElemUnderArtifact(ctx, doc, r, parent)
}

// structElemUnderArtifact reports whether a structure element, or any ancestor
// its /P chain reaches, stands for the standard type Artifact. It mirrors
// veraPDF's GFPDAnnot.getisArtifact, which climbs from the element enclosing an
// annotation; ISO 14289-2, 8.9 exempts artifacts from the structure
// requirements.
func structElemUnderArtifact(ctx context.Context, doc *document.Document, r *nsRoleResolver, elem ast.PdfDict) bool {
	for range maxStructTreeWalkDepth {
		if elem == nil || ctx.Err() != nil {
			return false
		}
		if stdType, _, ok := r.elementStandardType(ctx, doc, elem); ok && stdType == "Artifact" {
			return true
		}
		parent, err := document.ResolveDict(ctx, doc, elem["P"])
		if err != nil {
			return false
		}
		elem = parent
	}
	return false
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
