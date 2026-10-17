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
// - t1: all structure elements must belong to or be role-mapped to a standard namespace
// - t3: within a namespace, types shall not be role-mapped to types in the same namespace
// - t4: a standard type shall not be remapped to a non-standard one ([checkRemappedStandardTypes])
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
		}
	}

	// The legacy /RoleMap lets a non-standard type name (in any namespace) be
	// mapped to a standard type, which veraPDF honours for §8.2.4 t1.
	classicRoleMap := LoadRoleMap(ctx, doc, treeRoot)

	results = append(results, checkRemappedStandardTypes(ctx, doc, treeRoot, classicRoleMap)...)

	// t1: walk structure tree, check each element resolves to a standard namespace.
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

	walkNSCheck(ctx, doc, treeRoot, defaultNSURI, nsByURI, classicRoleMap, &results, make(map[int]bool), 0)
	return results
}

// checkRemappedStandardTypes validates ISO 14289-2 §8.2.4 t4: a structure
// element whose type is standard in the namespace it belongs to shall not be
// role mapped to a type that is not. The test judges each element, not each
// role-map entry, and judges where its mapping ENDS: per ISO 32000-2:2020,
// 14.7.4 and 14.8.6 a mapping is followed until it reaches a type a standard namespace
// defines, so a chain such as Div -> Section -> Sect leaves the element a
// standard Sect, while Sect -> Book with nothing mapping Book leaves it on no
// standard type at all. Each element that fires is reported once.
//
// It mirrors veraPDF 1.30.2, whose rule object is SENonStandard with test
// remappedStandardType == null: an element is SENonStandard when following
// its mapping reaches no standard type, and remappedStandardType names the
// element's own type when that type is standard. A type with no mapping, or
// whose first hop is already visited, keeps its own type, which is why a
// standard type mapped to itself does not fire here (that is t2).
//
// roleMap is the structure tree root's legacy /RoleMap.
func checkRemappedStandardTypes(ctx context.Context, doc *document.Document, treeRoot ast.PdfDict, roleMap map[ast.PdfName]ast.PdfName) []Violation {
	var results []Violation
	memo := make(map[string]bool)
	walkStructureElements(ctx, doc, treeRoot, func(elem ast.PdfDict, _ string) {
		sType, _ := document.ResolveName(ctx, doc, elem["S"])
		if sType == "" {
			return
		}
		t := newStructTypeRef(ctx, doc, sType, elem["NS"])
		if !t.isStandard(ctx, doc) {
			return
		}
		key := t.key()
		remapped, seen := memo[key]
		if !seen {
			remapped = reachesNoStandardType(ctx, doc, t, roleMap)
			memo[key] = remapped
		}
		if remapped {
			uri, _ := t.uri(ctx, doc)
			results = append(results, failf(Rule_14289_2_8_2_4_t04,
				"standard type %s in namespace %q is remapped to a non-standard type",
				sType, uri)...)
		}
	})
	return results
}

// structTypeRef is one structure type as ISO 32000-2:2020, 14.8.6 names it: a
// type name and the namespace dictionary it belongs to, nil for none. nsKey
// identifies the namespace, by object number when it is an indirect object
// and by URI otherwise, and is empty when ns is nil.
type structTypeRef struct {
	name  ast.PdfName
	ns    ast.PdfDict
	nsKey string
}

// newStructTypeRef builds the structure type naming name in the namespace
// nsObj. A namespace that does not resolve to a dictionary is no namespace, as
// veraPDF reads it.
func newStructTypeRef(ctx context.Context, doc *document.Document, name ast.PdfName, nsObj ast.PdfObject) structTypeRef {
	t := structTypeRef{name: name}
	if nsObj == nil {
		return t
	}
	ns, err := document.ResolveDict(ctx, doc, nsObj)
	if err != nil || ns == nil {
		return t
	}
	t.ns = ns
	if ref, ok := nsObj.(ast.PdfIndirectRef); ok {
		t.nsKey = fmt.Sprintf("obj %d", ref.ObjectNum)
	} else {
		uri, _ := document.ResolveString(ctx, doc, ns["NS"])
		t.nsKey = "uri " + string(uri)
	}
	return t
}

// key identifies the type for a visited set.
func (t structTypeRef) key() string {
	return string(t.name) + "\x00" + t.nsKey
}

// uri returns the namespace URI, and false when the type has no namespace or
// its namespace carries no /NS string.
func (t structTypeRef) uri(ctx context.Context, doc *document.Document) (string, bool) {
	if t.ns == nil {
		return "", false
	}
	uri, err := document.ResolveString(ctx, doc, t.ns["NS"])
	if err != nil {
		return "", false
	}
	return string(uri), true
}

// isStandard reports whether the type is one its namespace defines. A type in
// no namespace, or in a namespace dictionary with no /NS, is judged against
// the PDF 1.7 vocabulary (ISO 32000-1:2008, 14.8.4); a custom namespace
// defines no standard type (ISO 32000-2:2020, 14.8.6).
func (t structTypeRef) isStandard(ctx context.Context, doc *document.Document) bool {
	uri, ok := t.uri(ctx, doc)
	if !ok {
		return standardStructTypesPDF17[t.name]
	}
	if uri == "" {
		return false
	}
	return isStandardStructTypeInNS(t.name, uri)
}

// equivalent returns the type t is role mapped to, or false when nothing maps
// it. A namespace with a /RoleMapNS maps its types through that dictionary
// alone (ISO 32000-2:2020, 14.7.4); the legacy /RoleMap maps a type in no
// namespace, or in a PDF 1.7 namespace without a /RoleMapNS, and its targets
// belong to no namespace.
func (t structTypeRef) equivalent(ctx context.Context, doc *document.Document, roleMap map[ast.PdfName]ast.PdfName) (structTypeRef, bool) {
	if t.ns != nil {
		if rmns, _ := document.ResolveDict(ctx, doc, t.ns["RoleMapNS"]); rmns != nil {
			value, ok := rmns[t.name]
			if !ok {
				return structTypeRef{}, false
			}
			return roleMapNSTargetType(ctx, doc, value)
		}
		if uri, ok := t.uri(ctx, doc); !ok || uri != pdf1NamespaceURI {
			return structTypeRef{}, false
		}
	}
	target, ok := roleMap[t.name]
	if !ok {
		return structTypeRef{}, false
	}
	return structTypeRef{name: target}, true
}

// roleMapNSTargetType reads one /RoleMapNS value, an array of a name and the
// namespace it belongs to (ISO 32000-2:2020, Table 353). A bare name is read
// too, as a type in no namespace: veraPDF 1.30.2 reads it so
// (StructureType.createStructureType), and t4 follows veraPDF's chain.
func roleMapNSTargetType(ctx context.Context, doc *document.Document, value ast.PdfObject) (structTypeRef, bool) {
	if name, err := document.ResolveName(ctx, doc, value); err == nil && name != "" {
		return structTypeRef{name: name}, true
	}
	arr, _ := document.ResolveArray(ctx, doc, value)
	if len(arr) < 2 {
		return structTypeRef{}, false
	}
	name, err := document.ResolveName(ctx, doc, arr[0])
	if err != nil || name == "" {
		return structTypeRef{}, false
	}
	return newStructTypeRef(ctx, doc, name, arr[1]), true
}

// reachesNoStandardType reports whether following the role mapping of the
// standard type t ends on no standard type: every hop lands on a type no
// standard namespace defines until the chain stops or closes on a type
// already visited. A type with no mapping, or whose first hop is already
// visited, keeps its own standard type and does not count. A chain longer than
// maxRoleMapNSDepth gets no verdict and does not count either, so the cap can
// hide a remap but never invent one.
func reachesNoStandardType(ctx context.Context, doc *document.Document, t structTypeRef, roleMap map[ast.PdfName]ast.PdfName) bool {
	visited := map[string]bool{t.key(): true}
	cur, ok := t.equivalent(ctx, doc, roleMap)
	if !ok || visited[cur.key()] {
		return false
	}
	for range maxRoleMapNSDepth {
		if cur.isStandard(ctx, doc) {
			return false
		}
		visited[cur.key()] = true
		cur, ok = cur.equivalent(ctx, doc, roleMap)
		if !ok || visited[cur.key()] {
			return true
		}
	}
	return false
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

// legacyRoleMapGoverns reports whether the structure tree root's legacy /RoleMap
// maps a type declared in namespace nsURI, "" for a type in no namespace. The
// legacy map governs a type in no namespace, or in the PDF 1.7 namespace when
// that namespace declares no /RoleMapNS of its own; a type in any other
// namespace resolves through that namespace's /RoleMapNS alone
// (ISO 32000-2:2020, 14.8.6). veraPDF 1.30.2 resolves the same way: once an
// element declares the PDF 2.0 namespace, a custom one, or a PDF 1.7 namespace
// carrying a /RoleMapNS, a legacy entry sending its type to a standard one
// leaves it unmapped for ISO 14289-2 §8.2.4 t1. Measured.
//
// nsRoleMaps holds the /RoleMapNS of every namespace that declares one.
//
// # Corpus evidence
//
//   - All corpora: no tagged file declares, on an element whose type only the
//     legacy map maps, a namespace that map does not govern.
//   - litho rolemapscope_ua2: declares the PDF 2.0 namespace on such an
//     element, beside the two placements the map governs.
//   - All corpora: PDF/UA-2 remediation of 6 tagged files declared the PDF 2.0
//     namespace on a root element the map sends to Document, and veraPDF
//     reported 8.2.4 t1 on each output where litho reported nothing.
func legacyRoleMapGoverns(nsURI string, nsRoleMaps map[string]ast.PdfDict) bool {
	if nsURI == "" {
		return true
	}
	_, declares := nsRoleMaps[nsURI]
	return nsURI == pdf1NamespaceURI && !declares
}

// legacyRoleMapMaps reports whether the legacy /RoleMap maps typeName, declared
// in namespace nsURI, for ISO 14289-2 §8.2.4 t1: the map has to govern the type
// ([legacyRoleMapGoverns]), and then either lead it to a standard type or name
// it at all, since a circular entry is still a mapping that t2 reports
// ([typeInLegacyRoleMap]).
func legacyRoleMapMaps(roleMap map[ast.PdfName]ast.PdfName, typeName ast.PdfName, nsURI string, nsRoleMaps map[string]ast.PdfDict) bool {
	if !legacyRoleMapGoverns(nsURI, nsRoleMaps) {
		return false
	}
	return roleMapChainReachesStandard(roleMap, typeName, nsURI) || typeInLegacyRoleMap(roleMap, typeName)
}

// UngovernedRoleReader reads the structure types that the structure tree root's
// legacy /RoleMap maps but does not govern, as ISO 14289-2 §8.2.4 t1 judges
// them, so a remediation fixer repairs exactly the elements this validator
// reports. Build it with [NewUngovernedRoleReader] from one structure tree
// root. It performs no mutation after construction, so it is safe for
// concurrent use.
type UngovernedRoleReader struct {
	r *nsRoleResolver
}

// NewUngovernedRoleReader returns a reader over treeRoot's legacy /RoleMap and
// the /RoleMapNS of every namespace its /Namespaces array lists.
func NewUngovernedRoleReader(ctx context.Context, doc *document.Document, treeRoot ast.PdfDict) *UngovernedRoleReader {
	return &UngovernedRoleReader{r: newNSRoleResolver(ctx, doc, treeRoot)}
}

// Target reports the standard structure type the legacy /RoleMap gives elem's
// type, for an element ISO 14289-2 §8.2.4 t1 reports only because that map does
// not govern the namespace the element declares. nsURI is that namespace.
//
// The legacy /RoleMap governs a type in no namespace, or in the PDF 1.7
// namespace when that namespace declares no /RoleMapNS ([legacyRoleMapGoverns],
// ISO 32000-2:2020, 14.8.6). A type declared in any other namespace resolves
// through that namespace's /RoleMapNS alone, so a type the legacy map sends to
// a standard one is still unmapped there, unless the namespace defines the type
// itself or its /RoleMapNS maps it. target is where the legacy map leads the
// type read in no namespace, which is where it leads every element of that type
// the map does govern: the reading the document's producer declared.
//
// ok is false for an element the map governs, one whose type the namespace
// already resolves, one the namespace's own /RoleMapNS names (the namespace
// states that type's mapping itself, so the legacy map is not the producer's
// word for it), and one the legacy map leads to no standard type or back to the
// type it names.
func (u *UngovernedRoleReader) Target(ctx context.Context, doc *document.Document, elem ast.PdfDict) (target ast.PdfName, nsURI string, ok bool) {
	sType, _ := document.ResolveName(ctx, doc, elem["S"])
	if sType == "" {
		return "", "", false
	}
	nsURI = u.r.declaredNSURI(ctx, doc, elem)
	if nsURI == "" || legacyRoleMapGoverns(nsURI, u.r.rmnsByURI) {
		return "", "", false
	}
	if standardNamespaceURIs[nsURI] && isStandardStructTypeInNS(sType, nsURI) {
		return "", "", false
	}
	// A namespace role map that does not name the type leaves it unmapped
	// ([roleMapNSChainOutcome] stops at its first hop), which is the §8.2.4 t1
	// the validator reports.
	if rmns, declares := u.r.rmnsByURI[nsURI]; declares {
		if _, named := rmns[sType]; named {
			return "", "", false
		}
	}
	std, _, mapped := u.r.resolveStandardType(ctx, doc, string(sType), "")
	if !mapped || ast.PdfName(std) == sType {
		return "", "", false
	}
	return ast.PdfName(std), nsURI, true
}

// IsStandardStructTypeInNS reports whether name is a standard structure type of
// the standard structure namespace nsURI, "" for a type in no namespace. Each
// standard namespace defines its own vocabulary (ISO 32000-2:2020, 14.8.6). It
// is exported so a remediation fixer names a type in a namespace exactly as the
// validator judges it there.
func IsStandardStructTypeInNS(name ast.PdfName, nsURI string) bool {
	return isStandardStructTypeInNS(name, nsURI)
}

// IsStandardNamespace reports whether nsURI is one of the standard structure
// namespaces of ISO 32000-2:2020, 14.8.6.
func IsStandardNamespace(nsURI string) bool {
	return standardNamespaceURIs[nsURI]
}

// nsTypeKey identifies a structure type within its namespace, for the cycle
// detection of role-map chains. A struct keeps the pair apart where a joined
// string would not: a name holds any byte a "#hh" escape decodes to, NUL
// included (ISO 32000-1, 7.3.5), and a namespace URI is a string that can hold
// one too, so two different pairs joined on NUL could collide.
type nsTypeKey struct {
	ns  string
	typ ast.PdfName
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
	visited := map[nsTypeKey]bool{{ns: curNS, typ: curType}: true}

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

		key := nsTypeKey{ns: string(targetURI), typ: targetType}
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
				!legacyRoleMapMaps(roleMap, sType, declaredNSURI, nsRoleMaps) {
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
			// namespace's RoleMapNS or, where it governs, the legacy /RoleMap
			// (e.g. /Standard /P). The legacy map never reaches a type the
			// element declares in the PDF 2.0 namespace, so /PDFDocument there
			// fails even when the map sends it to Document.
			// Each namespace carries its own vocabulary (ISO 32000-2:2020,
			// 14.8.6), so BlockQuote fails here in the PDF 2.0 namespace and
			// FENote fails in the PDF 1.7 one.
			// t01 fires when the type does not reach a standard type; a circular
			// chain is reported by t02/t03, not t01, so exclude it here.
			if roleMapNSChainOutcome(ctx, doc, sType, elemNSURI, nsRoleMaps) == nsChainUnmapped &&
				!legacyRoleMapMaps(roleMap, sType, declaredNSURI, nsRoleMaps) {
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
		// A /K array written as an object of its own is walked once, as an
		// element is: its direct elements can each name it again as their /K.
		if document.VisitedBefore(kObj, visited) {
			return
		}
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
	visited := make(map[nsTypeKey]bool)
	for range maxRoleMapNSDepth {
		key := nsTypeKey{ns: nsURI, typ: ast.PdfName(typeName)}
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
// in no namespace or in the PDF 1.7 one ([legacyRoleMapGoverns]). A legacy
// target names no namespace of its own. mapped is false when no mapping governs
// the type, which is what makes it stand for itself.
func (r *nsRoleResolver) equivalentType(ctx context.Context, doc *document.Document, typeName, nsURI string) (target, targetNS string, mapped bool) {
	if rmns, declares := r.rmnsByURI[nsURI]; declares && nsURI != "" {
		return roleMapNSTarget(ctx, doc, rmns[ast.PdfName(typeName)])
	}
	if !legacyRoleMapGoverns(nsURI, r.rmnsByURI) {
		return "", "", false
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
// by the visited set, which holds each wrapper and each /K array written as an
// object of its own, so a cyclic or fan-out-heavy wrapper chain cannot make
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
// object numbers of the wrappers and /K arrays already expanded.
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
	} else if document.VisitedBefore(kObj, visited) {
		// A /K array written as an object of its own is expanded once: the
		// direct wrappers it holds can each name it again as their /K.
		return nil
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
			// An array written as an object of its own is walked once: one
			// that names itself, or whose direct kids name it, would fan out
			// 2^depth.
			if document.VisitedBefore(obj, seen) {
				return
			}
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

	var results []Violation

	// Each annotation is judged in the unit veraPDF judges it in
	// ([judgedAnnotations]): an annotation object two pages list fails once.
	for _, pa := range judgedAnnotations(ctx, doc, collectPageObjects(ctx, doc, catalog)) {
		if ctx.Err() != nil {
			return results
		}
		annot := pa.annot

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
		loc := fmt.Sprintf("page %d, annot %d (%s)", pa.pageIdx+1, pa.annotIdx, subtype)

		if f&annotFlagInvisible != 0 {
			results = append(results, Violation{Rule: Rule_14289_2_8_9_2_2_t01, Context: loc})
		}
		if f&annotFlagNoView != 0 && f&annotFlagToggleNoView == 0 {
			results = append(results, Violation{Rule: Rule_14289_2_8_9_2_2_t02, Context: loc})
		}
	}

	return results
}
