package compliance

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// --- 11. Namespaces (PDF/UA-2) ---

func checkNamespaces(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if profile != "PDF/UA-2" {
		return nil
	}

	slog.Debug("compliance.accessibility: checking /Namespaces in StructTreeRoot")

	treeRoot, err := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])
	if err != nil || treeRoot == nil {
		slog.Debug("compliance.accessibility: no StructTreeRoot for namespace check")
		return nil
	}

	nsObj, ok := treeRoot["Namespaces"]
	if !ok {
		slog.Debug("compliance.accessibility: StructTreeRoot missing /Namespaces")
		return fail(Rule_14289_2_8_2_4_t01, "Namespaces missing from StructTreeRoot")
	}

	nsArray, err := document.ResolveArray(ctx, doc, nsObj)
	if err != nil || len(nsArray) == 0 {
		slog.Debug("compliance.accessibility: /Namespaces is empty or unresolvable", "error", err)
		return fail(Rule_14289_2_8_2_4_t01, "Namespaces empty")
	}

	var results []Violation
	for i, nsEntryObj := range nsArray {
		if ctx.Err() != nil {
			return results
		}

		nsDict, err := document.ResolveDict(ctx, doc, nsEntryObj)
		if err != nil || nsDict == nil {
			slog.Debug("compliance.accessibility: cannot resolve namespace entry", "index", i, "error", err)
			continue
		}

		// Each namespace dict must have /NS (URI).
		nsURIObj, ok := nsDict["NS"]
		if !ok {
			slog.Debug("compliance.accessibility: namespace entry missing /NS", "index", i)
			results = append(results, Violation{
				Rule:    Rule_14289_2_8_2_4_t01,
				Context: fmt.Sprintf("namespace %d missing /NS", i),
			})
			continue
		}

		nsURI, err := document.ResolveString(ctx, doc, nsURIObj)
		if err != nil || string(nsURI) == "" {
			slog.Debug("compliance.accessibility: namespace /NS is empty", "index", i)
			results = append(results, Violation{
				Rule:    Rule_14289_2_8_2_4_t01,
				Context: fmt.Sprintf("namespace %d has empty /NS", i),
			})
		} else {
			slog.Debug("compliance.accessibility: namespace entry valid", "index", i, "ns", string(nsURI))
		}
	}

	return results
}

// --- 11b. Namespace role mapping (PDF/UA-2 §8.2.4) ---

// standardNamespaceURIs lists the namespace URIs that are "standard" per
// ISO 32000-2:2020, 14.8.6.
//
//nolint:gochecknoglobals // static lookup table
var standardNamespaceURIs = map[string]bool{
	"http://iso.org/pdf/ssn":             true, // PDF 1.7
	"http://iso.org/pdf2/ssn":            true, // PDF 2.0
	"http://www.w3.org/1998/Math/MathML": true, // MathML
}

// checkNamespaceRoleMapping validates PDF/UA-2 §8.2.4:
// - t1/t4: all structure elements must belong to or be role-mapped to a standard namespace
// - t3: within a namespace, types shall not be role-mapped to types in the same namespace
func checkNamespaceRoleMapping(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if profile != "PDF/UA-2" {
		return nil
	}

	treeRoot, err := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])
	if err != nil || treeRoot == nil {
		return nil
	}

	// Collect namespace dicts and their URIs.
	nsArr, _ := document.ResolveArray(ctx, doc, treeRoot["Namespaces"])

	// Build a map of namespace URI → RoleMapNS dict for chain following.
	nsByURI := make(map[string]ast.PdfDict)
	for _, nsObj := range nsArr {
		nsDict, _ := document.ResolveDict(ctx, doc, nsObj)
		if nsDict == nil {
			continue
		}
		uri, _ := document.ResolveString(ctx, doc, nsDict["NS"])
		if len(uri) > 0 {
			rmns, _ := document.ResolveDict(ctx, doc, nsDict["RoleMapNS"])
			if rmns != nil {
				nsByURI[string(uri)] = rmns
			}
		}
	}

	// t3: check each namespace's RoleMapNS. Follow chains through intermediate
	// namespaces — if the final target resolves back to the source namespace
	// (directly or indirectly), it's a same-namespace mapping violation.
	var results []Violation
	for _, nsObj := range nsArr {
		nsDict, _ := document.ResolveDict(ctx, doc, nsObj)
		if nsDict == nil {
			continue
		}
		srcURI, _ := document.ResolveString(ctx, doc, nsDict["NS"])
		rmns, _ := document.ResolveDict(ctx, doc, nsDict["RoleMapNS"])
		if rmns == nil {
			continue
		}
		for typeName, mappingObj := range rmns {
			// t03: same-namespace circular mapping.
			if roleMapChainReturnsSameNS(ctx, doc, string(srcURI), string(typeName), nsByURI) {
				results = append(results, failf(Rule_14289_2_8_2_4_t03,
					"namespace %q: type %s maps back to same namespace through chain",
					string(srcURI), typeName)...)
			}
			// t04: standard type remapped to non-standard.
			if standardNamespaceURIs[string(srcURI)] && isStandardStructType(typeName, "PDF/UA-2") {
				// Check if the target is also in a standard namespace type.
				targetArr, _ := document.ResolveArray(ctx, doc, mappingObj)
				if len(targetArr) >= 2 { //nolint:mnd
					targetNSDict, _ := document.ResolveDict(ctx, doc, targetArr[1])
					if targetNSDict != nil {
						targetURI, _ := document.ResolveString(ctx, doc, targetNSDict["NS"])
						targetType, _ := document.ResolveName(ctx, doc, targetArr[0])
						if !standardNamespaceURIs[string(targetURI)] || !isStandardStructType(targetType, "PDF/UA-2") {
							results = append(results, failf(Rule_14289_2_8_2_4_t04,
								"standard type %s remapped to %s in namespace %q",
								typeName, targetType, string(targetURI))...)
						}
					}
				}
			}
		}
	}

	// t04: also check the StructTreeRoot's old-style /RoleMap for standard types
	// remapped to non-standard types. This applies when the PDF uses legacy RoleMap
	// entries alongside namespace-based structure.
	roleMap, _ := document.ResolveDict(ctx, doc, treeRoot["RoleMap"])
	for src, dstObj := range roleMap {
		if isStandardStructType(src, "PDF/UA-2") {
			dstName, _ := document.ResolveName(ctx, doc, dstObj)
			if dstName != "" && !isStandardStructType(dstName, "PDF/UA-2") {
				results = append(results, failf(Rule_14289_2_8_2_4_t04,
					"standard type %s remapped to non-standard type %s in StructTreeRoot /RoleMap",
					src, dstName)...)
			}
		}
	}

	// t1/t4: walk structure tree, check each element resolves to a standard namespace.
	// Elements with no explicit /NS use the first namespace in the array.
	defaultNSURI := ""
	if len(nsArr) > 0 {
		nsDict, _ := document.ResolveDict(ctx, doc, nsArr[0])
		if nsDict != nil {
			ns, _ := document.ResolveString(ctx, doc, nsDict["NS"])
			defaultNSURI = string(ns)
		}
	}

	walkNSCheck(ctx, doc, treeRoot, defaultNSURI, nsArr, &results)
	return results
}

// walkNSCheck recursively walks structure elements and checks namespace membership.
func walkNSCheck(ctx context.Context, doc *document.Document, node ast.PdfDict, defaultNSURI string, nsArr ast.PdfArray, results *[]Violation) {
	if ctx.Err() != nil {
		return
	}

	sType, _ := document.ResolveName(ctx, doc, node["S"])
	if sType != "" {
		// Determine the element's namespace URI.
		elemNSURI := defaultNSURI
		if nsObj, ok := node["NS"]; ok {
			nsDict, _ := document.ResolveDict(ctx, doc, nsObj)
			if nsDict != nil {
				ns, _ := document.ResolveString(ctx, doc, nsDict["NS"])
				if len(ns) > 0 {
					elemNSURI = string(ns)
				}
			}
		}

		// Check if the element's namespace (or role-mapped target) is standard.
		if !standardNamespaceURIs[elemNSURI] {
			// Check if the namespace has a RoleMapNS that maps to a standard NS.
			if !typeMapsToStandardNS(ctx, doc, string(sType), elemNSURI, nsArr) {
				*results = append(*results, failf(Rule_14289_2_8_2_4_t01,
					"structure type %s in namespace %q is not mapped to a standard namespace",
					sType, elemNSURI)...)
				return // One violation is enough per document.
			}
		}
	}

	kObj := node["K"]
	if kObj == nil {
		return
	}
	if arr, _ := document.ResolveArray(ctx, doc, kObj); len(arr) > 0 {
		for _, kid := range arr {
			d, _ := document.ResolveDict(ctx, doc, kid)
			if d != nil {
				walkNSCheck(ctx, doc, d, defaultNSURI, nsArr, results)
			}
		}
	} else if d, _ := document.ResolveDict(ctx, doc, kObj); d != nil {
		walkNSCheck(ctx, doc, d, defaultNSURI, nsArr, results)
	}
}

// typeMapsToStandardNS checks if a structure type in a given namespace has a
// RoleMapNS entry that ultimately resolves to a standard namespace.
func typeMapsToStandardNS(ctx context.Context, doc *document.Document, typeName, nsURI string, nsArr ast.PdfArray) bool {
	// Find the namespace dict for nsURI.
	for _, nsObj := range nsArr {
		nsDict, _ := document.ResolveDict(ctx, doc, nsObj)
		if nsDict == nil {
			continue
		}
		ns, _ := document.ResolveString(ctx, doc, nsDict["NS"])
		if string(ns) != nsURI {
			continue
		}
		rmns, _ := document.ResolveDict(ctx, doc, nsDict["RoleMapNS"])
		if rmns == nil {
			return false
		}
		mappingObj, ok := rmns[ast.PdfName(typeName)]
		if !ok {
			return false
		}
		arr, _ := document.ResolveArray(ctx, doc, mappingObj)
		if len(arr) < 2 { //nolint:mnd
			return false
		}
		targetNSDict, _ := document.ResolveDict(ctx, doc, arr[1])
		if targetNSDict == nil {
			return false
		}
		targetURI, _ := document.ResolveString(ctx, doc, targetNSDict["NS"])
		return standardNamespaceURIs[string(targetURI)]
	}
	return false
}

// roleMapChainReturnsSameNS follows a RoleMapNS chain starting from typeName
// in srcNSURI and returns true if the chain eventually maps back to the
// source namespace (directly or through intermediates).
func roleMapChainReturnsSameNS(ctx context.Context, doc *document.Document, srcNSURI string, typeName string, nsByURI map[string]ast.PdfDict) bool {
	currentType := typeName
	currentNS := srcNSURI
	visited := map[string]bool{currentNS + ":" + currentType: true}

	for range 10 { //nolint:mnd // max chain depth
		rmns, ok := nsByURI[currentNS]
		if !ok {
			return false
		}
		mappingObj, ok := rmns[ast.PdfName(currentType)]
		if !ok {
			return false
		}
		arr, _ := document.ResolveArray(ctx, doc, mappingObj)
		if len(arr) < 2 { //nolint:mnd
			return false
		}
		targetType, _ := document.ResolveName(ctx, doc, arr[0])
		targetNSDict, _ := document.ResolveDict(ctx, doc, arr[1])
		if targetNSDict == nil {
			return false
		}
		targetURI, _ := document.ResolveString(ctx, doc, targetNSDict["NS"])
		targetNS := string(targetURI)

		if targetNS == srcNSURI {
			return true // Chain resolves back to source namespace.
		}

		key := targetNS + ":" + string(targetType)
		if visited[key] {
			return false // Cycle without returning to source NS.
		}
		visited[key] = true
		currentType = string(targetType)
		currentNS = targetNS
	}
	return false
}

// --- 12. Link nesting (PDF/UA-2) ---

func checkLinkNesting(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if profile != "PDF/UA-2" {
		return nil
	}

	slog.Debug("compliance.accessibility: checking link nesting in structure tree")

	treeRoot, err := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])
	if err != nil || treeRoot == nil {
		slog.Debug("compliance.accessibility: no StructTreeRoot for link nesting check")
		return nil
	}

	// Collect all Link annotations from pages.
	pages := collectPages(ctx, doc, catalog)
	linkAnnotCount := 0
	for _, page := range pages {
		annotsObj, ok := page["Annots"]
		if !ok {
			continue
		}
		annots, err := document.ResolveArray(ctx, doc, annotsObj)
		if err != nil {
			slog.Debug("compliance.accessibility: cannot resolve annots array", "error", err)
			continue
		}
		for _, annotObj := range annots {
			annot, err := document.ResolveDict(ctx, doc, annotObj)
			if err != nil || annot == nil {
				continue
			}
			subtype, _ := document.ResolveName(ctx, doc, annot["Subtype"])
			if subtype == "Link" {
				linkAnnotCount++
			}
		}
	}

	if linkAnnotCount == 0 {
		slog.Debug("compliance.accessibility: no link annotations found")
		return nil
	}

	// Check that Link structure elements exist in the tree.
	var linkStructCount int
	walkStructureElements(ctx, doc, treeRoot, func(elem ast.PdfDict, path string) {
		typeName, _ := document.ResolveName(ctx, doc, elem["S"])
		if typeName == "Link" || typeName == "Reference" {
			linkStructCount++
		}
	})

	slog.Debug("compliance.accessibility: link annotation/structure counts",
		"annotations", linkAnnotCount, "structElements", linkStructCount)

	if linkStructCount == 0 && linkAnnotCount > 0 {
		return fail(Rule_14289_2_8_2_5_20_t01, "link annotations exist but no Link/Reference structure elements found")
	}

	return nil
}

// --- 13. Formula MathML (PDF/UA-2) ---

func checkFormulaMathML(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if profile != "PDF/UA-2" {
		return nil
	}

	slog.Debug("compliance.accessibility: checking Formula structure elements for /AF (MathML)")

	treeRoot, err := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])
	if err != nil || treeRoot == nil {
		slog.Debug("compliance.accessibility: no StructTreeRoot for Formula check")
		return nil
	}

	var results []Violation
	walkStructureElements(ctx, doc, treeRoot, func(elem ast.PdfDict, path string) {
		typeName, _ := document.ResolveName(ctx, doc, elem["S"])
		if typeName != "Formula" {
			return
		}

		// Formula elements should have one of:
		// (a) /AF (associated files) with MathML content
		// (b) Non-empty /Alt text
		// (c) Non-empty /ActualText
		// (d) Child structure elements providing the formula content
		if altObj, hasAlt := elem["Alt"]; hasAlt {
			altStr, err := document.ResolveString(ctx, doc, altObj)
			if err == nil && strings.TrimSpace(string(altStr)) != "" {
				slog.Debug("compliance.accessibility: Formula has /Alt text", "path", path)
				return
			}
		}
		if atObj, hasAT := elem["ActualText"]; hasAT {
			atStr, err := document.ResolveString(ctx, doc, atObj)
			if err == nil && strings.TrimSpace(string(atStr)) != "" {
				slog.Debug("compliance.accessibility: Formula has /ActualText", "path", path)
				return
			}
		}

		// Check for /AF.
		if afObj, hasAF := elem["AF"]; hasAF {
			afArr, err := document.ResolveArray(ctx, doc, afObj)
			if err != nil {
				// Might be a single dict, which is acceptable.
				afDict, err := document.ResolveDict(ctx, doc, afObj)
				if err == nil && afDict != nil {
					return
				}
			} else if len(afArr) > 0 {
				return
			}
		}

		// Check for child structure elements — Formula elements that contain
		// structural children (e.g., math content elements) provide the
		// formula content directly and do not require /AF or /Alt.
		if kObj, hasK := elem["K"]; hasK {
			if hasStructuralChildren(ctx, doc, kObj) {
				slog.Debug("compliance.accessibility: Formula has structural children", "path", path)
				return
			}
		}

		slog.Debug("compliance.accessibility: Formula missing /AF, /Alt, /ActualText, and structural children", "path", path)
		results = append(results, Violation{Rule: Rule_14289_2_8_2_5_29_t01, Context: path})
	})

	return results
}

// --- 14. Invisible annotations in structure (PDF/UA-2) ---

// checkInvisibleAnnotArtifact validates that annotations with the Invisible flag
// set are not included in the logical structure tree (they must be artifacts).
// Rule: ISO 14289-2, 8.9.2.2, test 1.
// hasStructuralChildren checks if /K contains at least one child dict with an /S key
// (a structure element), as opposed to only MCR/OBJR/integer references.
func hasStructuralChildren(ctx context.Context, doc *document.Document, kObj ast.PdfObject) bool {
	// Try as array first.
	arr, err := document.ResolveArray(ctx, doc, kObj)
	if err == nil {
		for _, item := range arr {
			d, err := document.ResolveDict(ctx, doc, item)
			if err == nil && d != nil {
				if _, hasS := d["S"]; hasS {
					return true
				}
			}
		}
		return false
	}
	// Try as single dict.
	d, err := document.ResolveDict(ctx, doc, kObj)
	if err == nil && d != nil {
		if _, hasS := d["S"]; hasS {
			return true
		}
	}
	return false
}

func checkInvisibleAnnotArtifact(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if profile != "PDF/UA-2" {
		return nil
	}

	slog.Debug("compliance.accessibility: checking invisible annotation artifact requirement")

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
			slog.Debug("compliance.accessibility: cannot resolve annots array", "error", err)
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

			// Check if annotation has the Invisible flag set.
			fObj, ok := annot["F"]
			if !ok {
				continue
			}
			flags, err := document.ResolveInt(ctx, doc, fObj)
			if err != nil {
				slog.Debug("compliance.accessibility: cannot resolve annotation flags", "error", err)
				continue
			}

			invisible := int(flags)&annotFlagInvisible != 0
			if !invisible {
				continue
			}

			// Invisible annotation — it must NOT be in the logical structure
			// (i.e., it must not have /StructParent).
			if _, hasStructParent := annot["StructParent"]; hasStructParent {
				subtype, _ := document.ResolveName(ctx, doc, annot["Subtype"])
				loc := fmt.Sprintf("page %d, annot %d (%s)", pageIdx+1, annotIdx, subtype)
				slog.Debug("compliance.accessibility: invisible annotation in structure tree", "loc", loc)
				results = append(results, Violation{Rule: Rule_14289_2_8_9_2_2_t01, Context: loc})
			}
		}
	}

	return results
}
