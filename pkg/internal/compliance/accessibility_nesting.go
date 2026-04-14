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

	// Build map: annotation objNum -> enclosing structure element standard type.
	structParentMap := buildAnnotStructTypeMap(ctx, doc, catalog, profile)

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

			annotObjNum := 0
			if ref, ok := annotObj.(ast.PdfIndirectRef); ok {
				annotObjNum = ref.ObjectNum
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

			// Skip Popup annotations. They are auxiliary and don't have structure requirements.
			if subtype == "Popup" {
				continue
			}

			stdType := structParentMap[annotObjNum] // "" if not in structure tree

			switch subtype {
			case "TrapNet":
				// §7.18.2: TrapNet annotations are forbidden.
				results = append(results, fail(Rule_14289_1_7_18_2_t01,
					"TrapNet annotation exists")...)

			case "PrinterMark":
				// §7.18.8: PrinterMark must NOT be in logical structure.
				if stdType != "" {
					results = append(results, fail(Rule_14289_1_7_18_8_t01,
						"PrinterMark annotation is included in logical structure")...)
				}

			case "Widget":
				// §7.18.4: Widget must be nested within Form tag.
				if stdType != "Form" {
					results = append(results, failf(Rule_14289_1_7_18_4_t01,
						"Widget annotation nested within %s instead of Form", nestingDesc(stdType))...)
				}

			case "Link":
				// §7.18.5: Link must be nested within Link tag.
				if stdType != "Link" {
					results = append(results, failf(Rule_14289_1_7_18_5_t01,
						"Link annotation nested within %s instead of Link", nestingDesc(stdType))...)
				}

			default:
				// §7.18.1: All other annotations must be nested within Annot tag.
				if stdType != "Annot" {
					results = append(results, failf(Rule_14289_1_7_18_1_t01,
						"%s annotation nested within %s instead of Annot", subtype, nestingDesc(stdType))...)
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

// buildAnnotStructTypeMap walks the structure tree and returns a map from
// annotation object number to the standard type name of the enclosing structure
// element. Annotations not referenced by any structure element are absent.
func buildAnnotStructTypeMap(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) map[int]string {
	treeRoot, err := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])
	if err != nil || treeRoot == nil {
		return nil
	}

	// Load role map for resolving non-standard types to standard types.
	roleMap := loadRoleMap(ctx, doc, treeRoot)

	result := make(map[int]string)
	walkStructureElements(ctx, doc, treeRoot, func(elem ast.PdfDict, _ string) {
		typeName, _ := document.ResolveName(ctx, doc, elem["S"])
		if typeName == "" {
			return
		}

		stdType := resolveStandardType(typeName, roleMap, profile)

		kObj, ok := elem["K"]
		if !ok {
			return
		}
		collectOBJRWithType(ctx, doc, kObj, string(stdType), result)
	})

	return result
}

// loadRoleMap loads the /RoleMap dictionary from the structure tree root.
func loadRoleMap(ctx context.Context, doc *document.Document, structTreeRoot ast.PdfDict) map[ast.PdfName]ast.PdfName {
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
