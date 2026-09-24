package compliance

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// standardNamespaceURIs lists the namespace URIs that are "standard" per
// ISO 32000-2:2020, 14.8.6.
//
//nolint:gochecknoglobals // static lookup table
var standardNamespaceURIs = map[string]bool{
	pdf1NamespaceURI:   true,
	pdf2NamespaceURI:   true,
	mathMLNamespaceURI: true,
}

// defaultStandardNamespaceURI is the namespace a structure element without an
// explicit /NS entry belongs to when the document declares no /Namespaces array.
// Per ISO 32000-2:2020, 14.8.6, such an element is in the default standard
// structure namespace, so a non-standard type name there is still SENonStandard
// (ISO 14289-2 §8.2.4 t1), matching veraPDF's isNotMappedToStandardType.
const defaultStandardNamespaceURI = "http://iso.org/pdf2/ssn"

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
		for _, typeName := range sortedNames(rmns) {
			mappingObj := rmns[typeName]
			// A type mapped to ITSELF within the same namespace is a circular
			// mapping (ISO 14289-2 §8.2.4 t2), which veraPDF reports distinctly
			// from a same-namespace remap to a DIFFERENT type (t3). Detect the
			// direct self-map first and skip the t3/t4 paths for it.
			if targetArr, _ := document.ResolveArray(ctx, doc, mappingObj); len(targetArr) >= 2 {
				targetType, _ := document.ResolveName(ctx, doc, targetArr[0])
				targetNSDict, _ := document.ResolveDict(ctx, doc, targetArr[1])
				targetURI, _ := document.ResolveString(ctx, doc, targetNSDict["NS"])
				if string(targetType) == string(typeName) && string(targetURI) == string(srcURI) {
					results = append(results, failf(Rule_14289_2_8_2_4_t02,
						"namespace %q: type %s maps to itself", string(srcURI), typeName)...)
					continue
				}
			}
			// t03: same-namespace circular mapping (chain returns through a
			// different type).
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
	// remapped to non-standard types. veraPDF keys §8.2.4 t4 on the SENonStandard
	// structure element (test remappedStandardType == null), not the /RoleMap
	// entry: it fires only when a USED element of the standard type actually
	// resolves to a non-standard type through the legacy /RoleMap. An element
	// resolves through the legacy /RoleMap only when it carries no explicit /NS; an
	// element of the same type with an explicit /NS resolves through that
	// namespace, leaving the /RoleMap entry inert. Gating on a used no-/NS element
	// of the source type matches veraPDF exactly and avoids a false positive when
	// remediation adds /Namespaces and an explicit /NS to every element while
	// leaving the now-inert legacy /RoleMap in place.
	roleMap, _ := document.ResolveDict(ctx, doc, treeRoot["RoleMap"])
	if len(roleMap) > 0 {
		usedNoNS := usedTypesWithoutNS(ctx, doc, treeRoot)
		for _, src := range sortedNames(roleMap) {
			if !usedNoNS[src] || !isStandardStructType(src, "PDF/UA-2") {
				continue
			}
			dstName, _ := document.ResolveName(ctx, doc, roleMap[src])
			if dstName != "" && !isStandardStructType(dstName, "PDF/UA-2") {
				results = append(results, failf(Rule_14289_2_8_2_4_t04,
					"standard type %s remapped to non-standard type %s in StructTreeRoot /RoleMap",
					src, dstName)...)
			}
		}
	}

	// t1/t4: walk structure tree, check each element resolves to a standard namespace.
	// Elements with no explicit /NS use the first namespace in the array. When the
	// document declares no /Namespaces array, an NS-less element belongs to the
	// default standard structure namespace (ISO 32000-2 §14.8.6), so a non-standard
	// type name there is still SENonStandard (§8.2.4 t1). Defaulting to "" instead
	// would skip the check entirely, under-detecting relative to veraPDF.
	defaultNSURI := defaultStandardNamespaceURI
	if len(nsArr) > 0 {
		nsDict, _ := document.ResolveDict(ctx, doc, nsArr[0])
		if nsDict != nil {
			ns, _ := document.ResolveString(ctx, doc, nsDict["NS"])
			if len(ns) > 0 {
				defaultNSURI = string(ns)
			}
		}
	}

	// The legacy /RoleMap lets a non-standard type name (in any namespace) be
	// mapped to a standard type, which veraPDF honours for §8.2.4 t1.
	classicRoleMap := LoadRoleMap(ctx, doc, treeRoot)

	walkNSCheck(ctx, doc, treeRoot, defaultNSURI, nsByURI, classicRoleMap, &results, make(map[int]bool), 0)
	return results
}

// usedTypesWithoutNS returns the structure types that have at least one used
// element with no explicit /NS entry. Such an element resolves its type through
// the legacy /RoleMap rather than a namespace's /RoleMapNS (ISO 32000-2 §14.8.6),
// which is what makes a standard type remapped to a non-standard one in the
// legacy /RoleMap an SENonStandard element under ISO 14289-2 §8.2.4 t4. An
// element of the same type carrying an explicit /NS resolves through that
// namespace instead, so the legacy /RoleMap entry does not apply to it.
func usedTypesWithoutNS(ctx context.Context, doc *document.Document, treeRoot ast.PdfDict) map[ast.PdfName]bool {
	out := make(map[ast.PdfName]bool)
	walkStructureElements(ctx, doc, treeRoot, func(elem ast.PdfDict, _ string) {
		if _, hasNS := elem["NS"]; hasNS {
			return
		}
		if sType, _ := document.ResolveName(ctx, doc, elem["S"]); sType != "" {
			out[sType] = true
		}
	})
	return out
}

// typeInLegacyRoleMap reports whether typeName is a key in the structure tree
// root's legacy /RoleMap. Such a type IS mapped per ISO 32000-2 §14.8.4, even
// when the mapping is circular or same-namespace; those defects are reported by
// §8.2.4 t02/t03, so the type must not also trip the unmapped-type rule t01.
// roleMapChainReachesStandard alone does not cover circular chains, which never
// reach a standard type yet are still "mapped".
func typeInLegacyRoleMap(roleMap map[ast.PdfName]ast.PdfName, typeName ast.PdfName) bool {
	_, ok := roleMap[typeName]
	return ok
}

// nsChainOutcome classifies where a namespace role-map (/RoleMapNS) chain ends.
type nsChainOutcome int

const (
	// nsChainUnmapped: the chain dead-ends without ever entering a standard
	// namespace, so the type is not mapped to a standard structure type.
	nsChainUnmapped nsChainOutcome = iota
	// nsChainStandard: the chain reaches a type in a standard namespace.
	nsChainStandard
	// nsChainCircular: the chain revisits a (namespace, type) pair.
	nsChainCircular
)

// roleMapNSChainOutcome follows the /RoleMapNS chain starting at typeName in
// nsURI and reports where it ends. Per ISO 32000-2 §14.8.6 a namespaced
// structure type resolves through its own namespace's /RoleMapNS, one hop at a
// time, so a chain that only ever names types in non-standard namespaces never
// reaches a standard structure type: that is ISO 14289-2 §8.2.4 t1
// (veraPDF SENonStandard, isNotMappedToStandardType). Merely appearing as a
// /RoleMapNS key does not make a type mapped. A chain that loops is reported by
// §8.2.4 t2 instead, so it is classified separately and must not also trip t1.
//
// nsRoleMaps holds only namespaces that declare a /RoleMapNS, so a namespace
// absent from it ends the chain. Exhausting [maxRoleMapNSDepth] counts as
// circular: a chain that long is pathological, and treating it as unmapped
// would report t1 on input the depth guard, not the document, cut short.
func roleMapNSChainOutcome(ctx context.Context, doc *document.Document, typeName ast.PdfName, nsURI string, nsRoleMaps map[string]ast.PdfDict) nsChainOutcome {
	curType, curNS := typeName, nsURI
	visited := map[string]bool{curNS + "\x00" + string(curType): true}

	for range maxRoleMapNSDepth {
		rmns, ok := nsRoleMaps[curNS]
		if !ok {
			return nsChainUnmapped
		}
		mappingObj, ok := rmns[curType]
		if !ok {
			return nsChainUnmapped
		}
		arr, _ := document.ResolveArray(ctx, doc, mappingObj)
		if len(arr) < 2 {
			return nsChainUnmapped
		}
		targetType, _ := document.ResolveName(ctx, doc, arr[0])
		targetNSDict, _ := document.ResolveDict(ctx, doc, arr[1])
		if targetType == "" || targetNSDict == nil {
			return nsChainUnmapped
		}
		targetURI, _ := document.ResolveString(ctx, doc, targetNSDict["NS"])
		if standardNamespaceURIs[string(targetURI)] {
			return nsChainStandard
		}

		key := string(targetURI) + "\x00" + string(targetType)
		if visited[key] {
			return nsChainCircular
		}
		visited[key] = true
		curType, curNS = targetType, string(targetURI)
	}
	return nsChainCircular
}

// roleMapChainReachesStandard reports whether typeName, declared in namespace
// nsURI, resolves to a standard structure type by following the legacy /RoleMap
// chain. Used as the §8.2.4 t1 escape for a non-standard type name role-mapped
// to a standard one.
//
// nsURI selects the vocabulary the first link is judged against, because a type
// is standard only in the namespace that defines it (ISO 32000-2:2020, 14.8.6).
// A legacy /RoleMap value is a bare name that belongs to no namespace, so every
// later link is judged against the PDF 1.7 vocabulary.
func roleMapChainReachesStandard(roleMap map[ast.PdfName]ast.PdfName, typeName ast.PdfName, nsURI string) bool {
	seen := make(map[ast.PdfName]bool)
	cur, curNS := typeName, nsURI
	for range maxStructTreeDepth {
		if isStandardStructTypeInNS(cur, curNS) {
			return true
		}
		if seen[cur] {
			return false
		}
		seen[cur] = true
		next, ok := roleMap[cur]
		if !ok {
			return false
		}
		cur, curNS = next, ""
	}
	return false
}

// walkNSCheck recursively walks structure elements and checks namespace
// membership. visited tracks the object numbers of /K children already
// walked: a crafted /K cycle would otherwise recurse forever, and stack
// exhaustion is not recoverable in Go. The depth cap bounds inline-dict
// nesting, which carries no object numbers for the visited set.
func walkNSCheck(ctx context.Context, doc *document.Document, node ast.PdfDict, defaultNSURI string, nsRoleMaps map[string]ast.PdfDict, roleMap map[ast.PdfName]ast.PdfName, results *[]Violation, visited map[int]bool, depth int) {
	if ctx.Err() != nil || depth > maxStructTreeDepth {
		return
	}

	sType, _ := document.ResolveName(ctx, doc, node["S"])
	if sType != "" {
		// Determine the element's namespace URI. declaredNSURI holds only what
		// the element itself declares through /NS and stays empty otherwise: the
		// standard structure vocabulary an element is judged against follows the
		// namespace it declares, never the document default, because
		// ISO 32000-1:2008, 14.8.4 governs an element that belongs to no
		// namespace.
		elemNSURI := defaultNSURI
		declaredNSURI := ""
		if nsObj, ok := node["NS"]; ok {
			nsDict, _ := document.ResolveDict(ctx, doc, nsObj)
			if nsDict != nil {
				ns, _ := document.ResolveString(ctx, doc, nsDict["NS"])
				if len(ns) > 0 {
					elemNSURI = string(ns)
					declaredNSURI = string(ns)
				}
			}
		}

		// Verify the element's namespace (or role-mapped target) is
		// standard. Elements without any namespace stay out of this walk:
		// they belong to the default standard structure namespace
		// (ISO 32000-2 §14.8.6), and non-standard NS-less types resolve
		// through the legacy /RoleMap, which the structure-type walk
		// already validates against 8.2.4 t1.
		if elemNSURI != "" && !standardNamespaceURIs[elemNSURI] {
			// t01 (isNotMappedToStandardType) fires when the type does not reach
			// a STANDARD type, not merely when it carries no mapping: a
			// /RoleMapNS chain that stays inside non-standard namespaces leaves
			// the type unmapped for ISO 14289-2 §8.2.4 t1. A circular chain, by
			// contrast, is reported by t02/t03, so it is excluded here to avoid a
			// double-fire. The standard-namespace branch below reads the same way.
			if roleMapNSChainOutcome(ctx, doc, sType, elemNSURI, nsRoleMaps) == nsChainUnmapped &&
				!roleMapChainReachesStandard(roleMap, sType, declaredNSURI) &&
				!typeInLegacyRoleMap(roleMap, sType) {
				*results = append(*results, failf(Rule_14289_2_8_2_4_t01,
					"structure type %s in namespace %q is not mapped to a standard namespace",
					sType, elemNSURI)...)
				return // One violation is enough per document.
			}
		} else if elemNSURI != "" && standardNamespaceURIs[elemNSURI] && !isStandardStructTypeInNS(sType, declaredNSURI) {
			// A type declared in a standard namespace but whose name is not a
			// standard structure type is still SENonStandard (veraPDF
			// isNotMappedToStandardType): the standard namespaces define a fixed
			// vocabulary, so e.g. /PDFDocument in the PDF 1.7 namespace fails
			// §8.2.4 t1 unless it is role-mapped to a standard type, via the
			// namespace's RoleMapNS or the legacy /RoleMap (e.g. /Standard /P).
			// Each namespace carries its own vocabulary (ISO 32000-2:2020,
			// 14.8.6), so BlockQuote fails here in the PDF 2.0 namespace and
			// FENote fails in the PDF 1.7 one.
			// t01 fires when the type does not reach a standard type; a circular
			// chain is reported by t02/t03, not t01, so exclude it here.
			if roleMapNSChainOutcome(ctx, doc, sType, elemNSURI, nsRoleMaps) == nsChainUnmapped &&
				!roleMapChainReachesStandard(roleMap, sType, declaredNSURI) &&
				!typeInLegacyRoleMap(roleMap, sType) {
				*results = append(*results, failf(Rule_14289_2_8_2_4_t01,
					"structure type %s in standard namespace %q is not a standard type and is not role-mapped",
					sType, elemNSURI)...)
				return // One violation is enough per document.
			}
		}
	} else if depth > 0 && isStructElemDict(ctx, doc, node) {
		// An element whose /S is missing, null or no name has no structure type
		// (ISO 32000-1, Table 323 makes /S required), so it maps to no standard
		// type: veraPDF's SENonStandard reports ISO 14289-2, 8.2.4 t1 for each.
		// Measured.
		*results = append(*results, failf(Rule_14289_2_8_2_4_t01,
			"structure element has no structure type")...)
	}

	kObj := node["K"]
	if kObj == nil {
		return
	}
	if arr, _ := document.ResolveArray(ctx, doc, kObj); len(arr) > 0 {
		for _, kid := range arr {
			walkNSCheckChild(ctx, doc, kid, defaultNSURI, nsRoleMaps, roleMap, results, visited, depth)
		}
	} else {
		walkNSCheckChild(ctx, doc, kObj, defaultNSURI, nsRoleMaps, roleMap, results, visited, depth)
	}
}

// walkNSCheckChild resolves one /K child and recurses into it, skipping
// references already walked.
func walkNSCheckChild(ctx context.Context, doc *document.Document, kid ast.PdfObject, defaultNSURI string, nsRoleMaps map[string]ast.PdfDict, roleMap map[ast.PdfName]ast.PdfName, results *[]Violation, visited map[int]bool, depth int) {
	if ref, ok := kid.(ast.PdfIndirectRef); ok {
		if visited[ref.ObjectNum] {
			return
		}
		visited[ref.ObjectNum] = true
	}
	d, err := document.ResolveDict(ctx, doc, kid)
	if err != nil || d == nil {
		return
	}
	walkNSCheck(ctx, doc, d, defaultNSURI, nsRoleMaps, roleMap, results, visited, depth+1)
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

// declaredNSURI returns the namespace URI an element declares through /NS, or
// "" when it declares none. An element that declares no namespace belongs to
// none, which is what selects the standard structure vocabulary it is judged
// against (ISO 32000-1:2008, 14.8.4).
func (r *nsRoleResolver) declaredNSURI(ctx context.Context, doc *document.Document, elem ast.PdfDict) string {
	nsObj, ok := elem["NS"]
	if !ok {
		return ""
	}
	nsDict, _ := document.ResolveDict(ctx, doc, nsObj)
	if nsDict == nil {
		return ""
	}
	uri, _ := document.ResolveString(ctx, doc, nsDict["NS"])
	return string(uri)
}

// elementStandardType resolves an element to its effective standard structure
// type and the standard namespace URI it lands in. ok is false when the element
// has no /S or cannot be resolved to a type in a standard namespace.
//
// Resolution starts from the namespace the element declares, never from the
// document default: the vocabulary a type is standard in follows its own
// namespace (ISO 32000-2:2020, 14.8.6), so an /S of FENote without an /NS is
// judged against ISO 32000-1:2008, 14.8.4 and is not a standard type.
//
// stdNS is empty when the type belongs to no namespace, and the document
// default is NOT substituted. An element that declares no /NS belongs to no
// namespace, which is what ISO 32000-2:2020, 14.8.6 makes the vocabulary
// question turn on: reporting the default instead let a Document without /NS
// pass ISO 14289-2, 8.2.5.2 t2 whenever the default happened to be the PDF 2.0
// namespace, and hid a standard Note from 8.2.5.14 t1.
func (r *nsRoleResolver) elementStandardType(ctx context.Context, doc *document.Document, elem ast.PdfDict) (stdType, stdNS string, ok bool) {
	sType, _ := document.ResolveName(ctx, doc, elem["S"])
	if sType == "" {
		return "", "", false
	}
	return r.resolveStandardType(ctx, doc, string(sType), r.declaredNSURI(ctx, doc, elem))
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

		// A type is resolved through the mapping that governs it BEFORE it is
		// accepted as standard: a standard type that IS remapped no longer denotes
		// that standard type (ISO 32000-2:2020, 14.8.6). Accepting the standard
		// name first reported ISO 14289-2 8.2.5.2 t2 (a Document in the wrong
		// namespace) where veraPDF reports t1 (the root holds no Document at all),
		// and disagreed with iso32005Resolve, which already resolves in this
		// order for the ISO/TS 32005 matrix.
		//
		// A type that maps to itself has no applicable mapping and stands for
		// itself; 8.2.4 t2 reports the circular entry separately.
		if next, nextNS, mapped := r.equivalentType(ctx, doc, typeName, nsURI); mapped &&
			(next != typeName || nextNS != nsURI) {
			typeName, nsURI = next, nextNS
			continue
		}

		// Landing in a standard namespace is not enough: each standard namespace
		// defines its own vocabulary (ISO 32000-2:2020, 14.8.6), so a name that
		// namespace does not define resolves to no standard type at all.
		if standardNamespaceURIs[nsURI] && isStandardStructTypeInNS(ast.PdfName(typeName), nsURI) {
			return typeName, nsURI, true
		}
		if nsURI == "" && isStandardStructTypeInNS(ast.PdfName(typeName), "") {
			return typeName, "", true
		}
		return "", "", false
	}
	return "", "", false
}

// equivalentType returns the role-map target of one structure type, mirroring
// [iso32005Equivalent]: a type whose namespace declares a /RoleMapNS is remapped
// through it, while the structure tree root's legacy /RoleMap governs only a type
// in no namespace or in the PDF 1.7 one (ISO 32000-2:2020, 14.8.6). A legacy
// target names no namespace of its own. mapped is false when no mapping governs
// the type, which is what makes it stand for itself.
func (r *nsRoleResolver) equivalentType(ctx context.Context, doc *document.Document, typeName, nsURI string) (target, targetNS string, mapped bool) {
	if nsURI != "" {
		if rmns, declares := r.rmnsByURI[nsURI]; declares {
			return roleMapNSTarget(ctx, doc, rmns[ast.PdfName(typeName)])
		}
		if nsURI != pdf1NamespaceURI {
			return "", "", false
		}
	}
	next, exists := r.classicRole[ast.PdfName(typeName)]
	if !exists {
		return "", "", false
	}
	return string(next), "", true
}

// roleMapNSTarget reads one /RoleMapNS value as a [name namespace] pair
// (ISO 32000-2:2020, Table 353). mapped is false for a malformed entry, so the
// type stands for itself rather than resolving through a mapping that is not
// there.
func roleMapNSTarget(ctx context.Context, doc *document.Document, entry ast.PdfObject) (target, targetNS string, mapped bool) {
	if entry == nil {
		return "", "", false
	}
	arr, err := document.ResolveArray(ctx, doc, entry)
	if err != nil || len(arr) < 2 {
		return "", "", false
	}
	targetType, _ := document.ResolveName(ctx, doc, arr[0])
	targetNSDict, _ := document.ResolveDict(ctx, doc, arr[1])
	if targetType == "" || targetNSDict == nil {
		return "", "", false
	}
	uri, _ := document.ResolveString(ctx, doc, targetNSDict["NS"])
	return string(targetType), string(uri), true
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

// significantChild is one structurally significant child of a structure tree
// node, reduced to the standard structure type it resolves to and the standard
// namespace that type lands in. Both fields are empty when the child resolves
// to no standard type at all.
type significantChild struct {
	stdType string
	stdNS   string
}

// significantChildren returns a node's structurally significant children in
// order, looking through the pass-through grouping wrappers listed in
// [iso32005PassThroughTypes]: a Part, Div, or NonStruct contributes its own
// children instead of itself, recursively, because ISO 32000-2:2020, 14.8.4.4
// defines the three as pure grouping wrappers whose descendants carry the
// structural meaning. Nesting is bounded by [maxISO32005PassThroughDepth] and
// by the visited set, so a cyclic or fan-out-heavy wrapper chain cannot make
// the walk diverge.
//
// Content items are skipped: per ISO 32000-2:2020, Table 355 a structure
// element dictionary either omits /Type or sets it to StructElem, while the
// marked-content and object references of Tables 356 and 357 carry /MCR and
// /OBJR.
func (r *nsRoleResolver) significantChildren(ctx context.Context, doc *document.Document, node ast.PdfDict, depth int) []significantChild {
	return r.significantChildrenAt(ctx, doc, node, depth, make(map[int]bool))
}

// significantChildrenAt is [nsRoleResolver.significantChildren] carrying the
// object numbers already expanded on the current pass-through spine.
func (r *nsRoleResolver) significantChildrenAt(ctx context.Context, doc *document.Document, node ast.PdfDict, depth int, visited map[int]bool) []significantChild {
	if ctx.Err() != nil || depth > maxISO32005PassThroughDepth {
		return nil
	}
	kObj, ok := node["K"]
	if !ok {
		return nil
	}
	items, err := document.ResolveArray(ctx, doc, kObj)
	if err != nil {
		items = ast.PdfArray{kObj}
	}

	var out []significantChild
	for _, item := range items {
		d, derr := document.ResolveDict(ctx, doc, item)
		if derr != nil || d == nil {
			continue
		}
		if typeName, _ := document.ResolveName(ctx, doc, d["Type"]); typeName != "" && typeName != "StructElem" {
			continue
		}
		stdType, stdNS, _ := r.elementStandardType(ctx, doc, d)
		if !iso32005PassThroughTypes[stdType] {
			out = append(out, significantChild{stdType: stdType, stdNS: stdNS})
			continue
		}
		if ref, isRef := item.(ast.PdfIndirectRef); isRef {
			if visited[ref.ObjectNum] {
				continue
			}
			visited[ref.ObjectNum] = true
		}
		out = append(out, r.significantChildrenAt(ctx, doc, d, depth+1, visited)...)
	}
	return out
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
			// exempt. An element declaring no /NS belongs to no namespace and is
			// judged against the PDF 1.7 vocabulary per ISO 32000-1:2008, 14.8.4,
			// so it is a standard Note too.
			if stdNS == pdf1NamespaceURI || stdNS == "" {
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

// sortedIntKeys returns the keys of an int-keyed set in ascending order, so a
// check that reports the first matching object is deterministic.
func sortedIntKeys[V any](m map[int]V) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// feNoteRefGraph records, per structure element object number, the object
// numbers its /Ref entry points at, and which of those elements are FENotes.
type feNoteRefGraph struct {
	refs    map[int]map[int]bool
	feNotes []int
}

// collectFENoteRefGraph walks the structure tree recording each element's /Ref
// targets by object number. Object identity is what the two-way association
// test needs, and [walkStructureElements] surfaces only the resolved
// dictionaries, so this walk keeps the numbers.
func collectFENoteRefGraph(ctx context.Context, doc *document.Document, r *nsRoleResolver, treeRoot ast.PdfDict) feNoteRefGraph {
	graph := feNoteRefGraph{refs: make(map[int]map[int]bool)}
	seen := make(map[int]bool)

	var visit func(obj ast.PdfObject, depth int)
	visit = func(obj ast.PdfObject, depth int) {
		if ctx.Err() != nil || depth > maxStructTreeWalkDepth {
			return
		}
		if arr, err := document.ResolveArray(ctx, doc, obj); err == nil {
			for _, kid := range arr {
				visit(kid, depth+1)
			}
			return
		}
		ref, isRef := obj.(ast.PdfIndirectRef)
		if !isRef || seen[ref.ObjectNum] {
			return
		}
		seen[ref.ObjectNum] = true
		elem, err := document.ResolveDict(ctx, doc, obj)
		if err != nil || elem == nil {
			return
		}
		targets := make(map[int]bool)
		if refArr, err := document.ResolveArray(ctx, doc, elem["Ref"]); err == nil {
			for _, target := range refArr {
				if t, ok := target.(ast.PdfIndirectRef); ok {
					targets[t.ObjectNum] = true
				}
			}
		}
		graph.refs[ref.ObjectNum] = targets
		if stdType, _, ok := r.elementStandardType(ctx, doc, elem); ok && stdType == "FENote" {
			graph.feNotes = append(graph.feNotes, ref.ObjectNum)
		}
		if kids, hasKids := elem["K"]; hasKids {
			visit(kids, depth+1)
		}
	}

	visit(treeRoot["K"], 0)
	return graph
}

// checkPDFUA2FENoteRefs validates the two-way /Ref association between a FENote
// and the content that refers to it (ISO 14289-2, 8.2.5.14 t2 and t3). Content
// referring to a note points at it with /Ref, and the note points back the same
// way. veraPDF names the two halves separately, which the tests mirror:
//
//   - t2, orphanRefs: an element points at the FENote and the FENote does not
//     point back.
//   - t3, ghostRefs:  the FENote points at an element that does not point back.
//
// Both were measured against veraPDF on isolated fixtures rather than inferred
// from the clause text, which names neither direction explicitly.
func checkPDFUA2FENoteRefs(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if profile != "PDF/UA-2" {
		return nil
	}
	treeRoot, err := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])
	if err != nil || treeRoot == nil {
		return nil
	}
	r := newNSRoleResolver(ctx, doc, treeRoot)
	graph := collectFENoteRefGraph(ctx, doc, r, treeRoot)

	var results []Violation
	for _, note := range graph.feNotes {
		if ctx.Err() != nil {
			return results
		}
		noteRefs := graph.refs[note]

		// t2: someone points at this note without the note pointing back.
		for _, source := range sortedIntKeys(graph.refs) {
			if source == note || !graph.refs[source][note] || noteRefs[source] {
				continue
			}
			results = append(results, failf(Rule_14289_2_8_2_5_14_t02,
				"FENote object %d is referenced by object %d but does not reference it back", note, source)...)
			break
		}

		// t3: this note points at something that does not point back.
		for _, target := range sortedIntKeys(noteRefs) {
			if graph.refs[target][note] {
				continue
			}
			results = append(results, failf(Rule_14289_2_8_2_5_14_t03,
				"FENote object %d references object %d, which does not reference it back", note, target)...)
			break
		}
	}
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

	treeRoot, err := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])
	if err != nil {
		doc.Logger.Debug("compliance.accessibility: cannot resolve structure tree root", "error", err)
	}
	r := newNSRoleResolver(ctx, doc, treeRoot)

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

			// Both tests apply only to an annotation in the logical structure
			// that no Artifact encloses: veraPDF gates them on
			// structParentType == null || isArtifact, see annotStructScope.
			if parent, underArtifact := annotStructScope(ctx, doc, treeRoot, r, annot); parent == nil || underArtifact {
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
