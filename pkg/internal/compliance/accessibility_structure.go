package compliance

import (
	"context"
	"fmt"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

func checkNamespaces(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if profile != "PDF/UA-2" {
		return nil
	}

	treeRoot, err := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])
	if err != nil || treeRoot == nil {
		return nil
	}

	nsObj, ok := treeRoot["Namespaces"]
	if !ok {
		return fail(Rule_14289_2_8_2_4_t01, "Namespaces missing from StructTreeRoot")
	}

	nsArray, err := document.ResolveArray(ctx, doc, nsObj)
	if err != nil || len(nsArray) == 0 {
		doc.Logger.Debug("compliance.accessibility: /Namespaces is empty or unresolvable", "error", err)
		return fail(Rule_14289_2_8_2_4_t01, "Namespaces empty")
	}

	var results []Violation
	for i, nsEntryObj := range nsArray {
		if ctx.Err() != nil {
			return results
		}

		nsDict, err := document.ResolveDict(ctx, doc, nsEntryObj)
		if err != nil || nsDict == nil {
			doc.Logger.Debug("compliance.accessibility: cannot resolve namespace entry", "index", i, "error", err)
			continue
		}

		// Each namespace dict must have /NS (URI).
		nsURIObj, ok := nsDict["NS"]
		if !ok {
			results = append(results, Violation{
				Rule:    Rule_14289_2_8_2_4_t01,
				Context: fmt.Sprintf("namespace %d missing /NS", i),
			})
			continue
		}

		nsURI, err := document.ResolveString(ctx, doc, nsURIObj)
		if err != nil || string(nsURI) == "" {
			results = append(results, Violation{
				Rule:    Rule_14289_2_8_2_4_t01,
				Context: fmt.Sprintf("namespace %d has empty /NS", i),
			})
		}
	}

	return results
}

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
	// namespaces. If the final target resolves back to the source namespace
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
				// Verify the target is also in a standard namespace type.
				targetArr, _ := document.ResolveArray(ctx, doc, mappingObj)
				if len(targetArr) >= 2 {
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

		// Verify the element's namespace (or role-mapped target) is standard.
		if !standardNamespaceURIs[elemNSURI] {
			// Namespace may have a RoleMapNS that maps to a standard NS.
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
		if len(arr) < 2 {
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

// maxRoleMapNSDepth bounds the namespace role-map (/RoleMapNS) and legacy
// /RoleMap chain length during type resolution. It is an anti-DoS guard against
// a crafted cyclic or pathologically long mapping chain, not a spec limit; real
// documents resolve in one or two hops. Resolution also tracks visited
// (namespace, type) pairs, so this is a belt-and-suspenders cap.
const maxRoleMapNSDepth = 32

// nsRoleResolver resolves a structure element's type to its effective standard
// structure type, following PDF 2.0 namespace role mapping (ISO 32000-2 §14.7.4
// /RoleMapNS) and the legacy /RoleMap. Build it once per document from the
// structure tree root with [newNSRoleResolver]; it performs no mutation and is
// safe to reuse across a single validation run.
type nsRoleResolver struct {
	// defaultNSURI is the namespace inherited by elements without an explicit
	// /NS, taken from the first entry of the tree root /Namespaces array.
	defaultNSURI string
	// rmnsByURI maps a namespace URI to that namespace's /RoleMapNS dict.
	rmnsByURI map[string]ast.PdfDict
	// classicRole is the tree root legacy /RoleMap (non-namespaced mapping).
	classicRole map[ast.PdfName]ast.PdfName
}

// newNSRoleResolver builds a resolver from a structure tree root.
func newNSRoleResolver(ctx context.Context, doc *document.Document, treeRoot ast.PdfDict) *nsRoleResolver {
	r := &nsRoleResolver{rmnsByURI: make(map[string]ast.PdfDict)}

	nsArr, _ := document.ResolveArray(ctx, doc, treeRoot["Namespaces"])
	for _, nsObj := range nsArr {
		nsDict, _ := document.ResolveDict(ctx, doc, nsObj)
		if nsDict == nil {
			continue
		}
		uri, _ := document.ResolveString(ctx, doc, nsDict["NS"])
		if len(uri) == 0 {
			continue
		}
		if rmns, _ := document.ResolveDict(ctx, doc, nsDict["RoleMapNS"]); rmns != nil {
			r.rmnsByURI[string(uri)] = rmns
		}
	}
	if len(nsArr) > 0 {
		if nsDict, _ := document.ResolveDict(ctx, doc, nsArr[0]); nsDict != nil {
			if uri, _ := document.ResolveString(ctx, doc, nsDict["NS"]); len(uri) > 0 {
				r.defaultNSURI = string(uri)
			}
		}
	}

	r.classicRole = LoadRoleMap(ctx, doc, treeRoot)
	return r
}

// elementNSURI returns the namespace URI an element belongs to: its own /NS, or
// the document default namespace when /NS is absent.
func (r *nsRoleResolver) elementNSURI(ctx context.Context, doc *document.Document, elem ast.PdfDict) string {
	if nsObj, ok := elem["NS"]; ok {
		if nsDict, _ := document.ResolveDict(ctx, doc, nsObj); nsDict != nil {
			if uri, _ := document.ResolveString(ctx, doc, nsDict["NS"]); len(uri) > 0 {
				return string(uri)
			}
		}
	}
	return r.defaultNSURI
}

// elementStandardType resolves an element to its effective standard structure
// type and the standard namespace URI it lands in. ok is false when the element
// has no /S or cannot be resolved to a type in a standard namespace.
func (r *nsRoleResolver) elementStandardType(ctx context.Context, doc *document.Document, elem ast.PdfDict) (stdType, stdNS string, ok bool) {
	sType, _ := document.ResolveName(ctx, doc, elem["S"])
	if sType == "" {
		return "", "", false
	}
	return r.resolveStandardType(ctx, doc, string(sType), r.elementNSURI(ctx, doc, elem))
}

// resolveStandardType resolves (typeName, nsURI) to its effective standard type
// and standard namespace by following /RoleMapNS (namespaced) or the legacy
// /RoleMap (when nsURI is empty). ok is false if no standard namespace is
// reached within [maxRoleMapNSDepth] hops or a cycle is detected.
func (r *nsRoleResolver) resolveStandardType(ctx context.Context, doc *document.Document, typeName, nsURI string) (stdType, stdNS string, ok bool) {
	visited := make(map[string]bool)
	for range maxRoleMapNSDepth {
		key := nsURI + "\x00" + typeName
		if visited[key] {
			return "", "", false
		}
		visited[key] = true

		if standardNamespaceURIs[nsURI] {
			return typeName, nsURI, true
		}

		if nsURI == "" {
			// No namespace mechanism: the legacy /RoleMap targets standard type
			// names with no namespace.
			if isStandardStructType(ast.PdfName(typeName), "PDF/UA-2") {
				return typeName, "", true
			}
			next, exists := r.classicRole[ast.PdfName(typeName)]
			if !exists {
				return "", "", false
			}
			typeName = string(next)
			continue
		}

		// Namespaced type: follow /RoleMapNS[type] -> [targetType, targetNS].
		rmns, exists := r.rmnsByURI[nsURI]
		if !exists {
			return "", "", false
		}
		mappingObj, exists := rmns[ast.PdfName(typeName)]
		if !exists {
			return "", "", false
		}
		arr, _ := document.ResolveArray(ctx, doc, mappingObj)
		if len(arr) < 2 {
			return "", "", false
		}
		targetType, _ := document.ResolveName(ctx, doc, arr[0])
		targetNSDict, _ := document.ResolveDict(ctx, doc, arr[1])
		if targetType == "" || targetNSDict == nil {
			return "", "", false
		}
		targetURI, _ := document.ResolveString(ctx, doc, targetNSDict["NS"])
		typeName, nsURI = string(targetType), string(targetURI)
	}
	return "", "", false
}

// childStandardTypes returns the effective standard structure types of an
// element's structural children (those with an /S entry), in order. complete is
// false when any structural child cannot be resolved to a standard type, in
// which case callers should not draw conclusions from the partial sequence (the
// unresolved child is itself a §8.2.4 violation).
func (r *nsRoleResolver) childStandardTypes(ctx context.Context, doc *document.Document, elem ast.PdfDict) (types []string, complete bool) {
	kObj, ok := elem["K"]
	if !ok {
		return nil, true
	}

	collect := func(item ast.PdfObject) bool {
		d, _ := document.ResolveDict(ctx, doc, item)
		if d == nil {
			return true
		}
		if _, has := d["S"]; !has {
			return true // content item (MCR/OBJR/MCID), not a structure element.
		}
		st, _, ok := r.elementStandardType(ctx, doc, d)
		if !ok {
			return false
		}
		types = append(types, st)
		return true
	}

	arr, err := document.ResolveArray(ctx, doc, kObj)
	if err != nil {
		if !collect(kObj) {
			return types, false
		}
		return types, true
	}
	for _, item := range arr {
		if !collect(item) {
			return types, false
		}
	}
	return types, true
}

// pdf1NamespaceURI is the legacy PDF 1.7 standard structure namespace.
const pdf1NamespaceURI = "http://iso.org/pdf/ssn"

// checkPDFUA2NamespaceStructTypes validates PDF/UA-2 structure-type constraints
// that depend on namespace role-map resolution:
//   - §8.2.5.14 t1: the standard Note type shall not be present (use FENote in
//     the PDF 2.0 namespace).
//   - §8.2.5.23 t1: a Ruby element's children shall be RB,RT or RB,RP,RT,RP.
//   - §8.2.5.24 t1: a Warichu element's children shall be WP,WT,WP.
//
// Element and child types are resolved through [nsRoleResolver], so a custom
// type role-mapped to a standard type is recognised and not false-rejected.
func checkPDFUA2NamespaceStructTypes(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if profile != "PDF/UA-2" {
		return nil
	}
	treeRoot, err := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])
	if err != nil || treeRoot == nil {
		return nil
	}
	r := newNSRoleResolver(ctx, doc, treeRoot)

	var results []Violation
	walkStructureElements(ctx, doc, treeRoot, func(elem ast.PdfDict, _ string) {
		stdType, stdNS, ok := r.elementStandardType(ctx, doc, elem)
		if !ok {
			return
		}
		switch stdType {
		case "Note":
			// The standard Note belongs to the PDF 1.7 namespace; a Note
			// role-mapped into the PDF 2.0 namespace resolves elsewhere and is
			// exempt.
			if stdNS == pdf1NamespaceURI {
				results = append(results, failf(Rule_14289_2_8_2_5_14_t01,
					"standard Note structure type is present (use FENote in the PDF 2.0 namespace)")...)
			}
		case "Ruby":
			if seq, complete := r.childStandardTypes(ctx, doc, elem); complete {
				joined := strings.Join(seq, "&")
				if joined != "RB&RT" && joined != "RB&RP&RT&RP" {
					results = append(results, failf(Rule_14289_2_8_2_5_23_t01,
						"Ruby element children are [%s], not RB,RT or RB,RP,RT,RP", joined)...)
				}
			}
		case "Warichu":
			if seq, complete := r.childStandardTypes(ctx, doc, elem); complete {
				if joined := strings.Join(seq, "&"); joined != "WP&WT&WP" {
					results = append(results, failf(Rule_14289_2_8_2_5_24_t01,
						"Warichu element children are [%s], not WP,WT,WP", joined)...)
				}
			}
		}
	})
	return results
}

// roleMapChainReturnsSameNS follows a RoleMapNS chain starting from typeName
// in srcNSURI and returns true if the chain eventually maps back to the
// source namespace (directly or through intermediates).
func roleMapChainReturnsSameNS(ctx context.Context, doc *document.Document, srcNSURI string, typeName string, nsByURI map[string]ast.PdfDict) bool {
	currentType := typeName
	currentNS := srcNSURI
	visited := map[string]bool{currentNS + ":" + currentType: true}

	for range 10 {
		rmns, ok := nsByURI[currentNS]
		if !ok {
			return false
		}
		mappingObj, ok := rmns[ast.PdfName(currentType)]
		if !ok {
			return false
		}
		arr, _ := document.ResolveArray(ctx, doc, mappingObj)
		if len(arr) < 2 {
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

func checkLinkNesting(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if profile != "PDF/UA-2" {
		return nil
	}

	treeRoot, err := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])
	if err != nil || treeRoot == nil {
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
			doc.Logger.Debug("compliance.accessibility: cannot resolve annots array", "error", err)
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

	if linkStructCount == 0 && linkAnnotCount > 0 {
		return fail(Rule_14289_2_8_2_5_20_t01, "link annotations exist but no Link/Reference structure elements found")
	}

	return nil
}

func checkFormulaMathML(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if profile != "PDF/UA-2" {
		return nil
	}

	treeRoot, err := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])
	if err != nil || treeRoot == nil {
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
				return
			}
		}
		if atObj, hasAT := elem["ActualText"]; hasAT {
			atStr, err := document.ResolveString(ctx, doc, atObj)
			if err == nil && strings.TrimSpace(string(atStr)) != "" {
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

		// Check for child structure elements. Formula elements that contain
		// structural children (e.g., math content elements) provide the
		// formula content directly and do not require /AF or /Alt.
		if kObj, hasK := elem["K"]; hasK {
			if hasStructuralChildren(ctx, doc, kObj) {
				return
			}
		}

		results = append(results, Violation{Rule: Rule_14289_2_8_2_5_29_t01, Context: path})
	})

	return results
}

// hasStructuralChildren reports whether /K contains at least one child dict with an /S key
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

// checkAnnotArtifactFlags validates ISO 14289-2 §8.9.2.2: an annotation that is
// part of the logical structure (has /StructParent) must not be effectively
// hidden. An Invisible annotation violates t1; a NoView annotation without
// ToggleNoView violates t2. Conforming files instead mark such annotations as
// artifacts, which keeps them out of the structure tree. /StructParent presence
// is the in-structure (non-artifact) signal, matching the t1 proxy.
func checkAnnotArtifactFlags(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if profile != "PDF/UA-2" {
		return nil
	}

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

			// Both tests apply only to annotations in the logical structure.
			if _, hasStructParent := annot["StructParent"]; !hasStructParent {
				continue
			}

			fObj, ok := annot["F"]
			if !ok {
				continue
			}
			flags, err := document.ResolveInt(ctx, doc, fObj)
			if err != nil {
				doc.Logger.Debug("compliance.accessibility: cannot resolve annotation flags", "error", err)
				continue
			}
			f := int(flags)

			subtype, _ := document.ResolveName(ctx, doc, annot["Subtype"])
			loc := fmt.Sprintf("page %d, annot %d (%s)", pageIdx+1, annotIdx, subtype)

			if f&annotFlagInvisible != 0 {
				results = append(results, Violation{Rule: Rule_14289_2_8_9_2_2_t01, Context: loc})
			}
			if f&annotFlagNoView != 0 && f&annotFlagToggleNoView == 0 {
				results = append(results, Violation{Rule: Rule_14289_2_8_9_2_2_t02, Context: loc})
			}
		}
	}

	return results
}
