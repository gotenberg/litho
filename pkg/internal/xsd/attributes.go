package xsd

import (
	"encoding/xml"
	"fmt"
	"slices"
	"strings"
)

// xmlnsPrefix is the name encoding/xml gives a namespace declaration: the
// space of xmlns:p="...", and the local name of xmlns="...".
const xmlnsPrefix = "xmlns"

// isSchemaAttribute reports whether name is an attribute schema validation
// judges. A namespace declaration is not an attribute of the element in
// the XML Information Set, which files it under [namespace attributes]. Per
// XML Schema Part 1, 3.4.4 (cvc-complex-type clause 3) and 3.1.1 (cvc-type
// clause 3.1.1), xsi:type, xsi:nil, xsi:schemaLocation and
// xsi:noNamespaceSchemaLocation are exempt, on an element of any type.
func isSchemaAttribute(name xml.Name) bool {
	if name.Space == xmlnsPrefix || (name.Space == "" && name.Local == xmlnsPrefix) {
		return false
	}
	if name.Space == xsiNamespace {
		switch name.Local {
		case "type", "nil", "schemaLocation", "noNamespaceSchemaLocation":
			return false
		}
	}
	return true
}

// isAnyType reports whether t is xs:anyType, the ur-type, which admits
// any attribute: per XML Schema Part 1, 3.4.7 its {attribute wildcard}
// is ##any with lax processing, and it declares no attribute to find.
func isAnyType(t Type) bool {
	b, ok := t.(*BuiltinType)
	return ok && b.Name == QName{Namespace: XSNamespace, Local: "anyType"}
}

// simpleTypeAttributes reports every attribute of an element whose type
// is a simple type. Per XML Schema Part 1, 3.3.4 (cvc-type clause 3.1.1)
// such an element carries none but the exempt xsi attributes.
func simpleTypeAttributes(start xml.StartElement, path string) []Violation {
	var out []Violation
	for _, a := range start.Attr {
		if !isSchemaAttribute(a.Name) {
			continue
		}
		out = append(out, Violation{
			Rule:   RuleAttribute,
			Path:   path + "/@" + a.Name.Local,
			Reason: fmt.Sprintf("attribute %s on an element of simple type", nameOf(a.Name)),
		})
	}
	return out
}

// attributeWildcardSet is the {attribute wildcard} of a complex type,
// kept as the complete wildcards whose union (XML Schema Part 1, 3.10.6)
// it is: an attribute's namespace is allowed when any one of them allows
// it. Per 3.10.6 the union of two expressible constraints allows exactly
// what either allows, and a union 3.10.6 calls not expressible (clause 5.3)
// is a schema error, which the set judges the same way rather than blaming
// the instance. processContents is the one the union takes, that of the
// most derived wildcard.
type attributeWildcardSet struct {
	alternatives    []*completeWildcard
	processContents string
}

// completeWildcard is the complete wildcard of a complex type or an
// attribute group, per XML Schema Part 1, 3.4.2 and 3.6.2: its own
// xs:anyAttribute intersected (3.10.6) with the {attribute wildcard} of
// every attribute group it references. It is kept as the wildcards whose
// intersection it is: an attribute's namespace is allowed when every one
// of them allows it. Per 3.10.6 the intersection of two expressible
// constraints allows exactly what both allow, and one 3.10.6 calls not
// expressible (clause 5) is a schema error (3.4.3 clause 4, 3.6.3 clause
// 2), which the set judges the same way rather than blaming the instance.
type completeWildcard struct {
	members         []*AttributeWildcard
	processContents string
}

// allows reports whether every member of c admits namespace ns.
func (c *completeWildcard) allows(ns string) bool {
	for _, m := range c.members {
		if !m.allows(ns) {
			return false
		}
	}
	return true
}

// urTypeWildcard is the {attribute wildcard} of xs:anyType, per XML Schema
// Part 1, 3.4.7: any namespace, lax processing.
//
//nolint:gochecknoglobals // immutable component of the ur-type.
var urTypeWildcard = &AttributeWildcard{Namespace: "##any", ProcessContents: "lax"}

// attributeWildcard derives the {attribute wildcard} of ct along its
// derivation chain, per XML Schema Part 1, 3.4.2. A type that derives by
// restriction, explicitly under xs:simpleContent or implicitly from the
// ur-type when it names no base, keeps its own complete wildcard alone
// ([wildcardOf]). A type that derives by extension keeps its base's
// wildcard, united with its own complete wildcard when it has one. A base
// that is a simple type has none, and an explicit extension of xs:anyType
// inherits [urTypeWildcard]. Returns nil when the type admits no
// undeclared attribute.
func attributeWildcard(ct *ComplexType) *attributeWildcardSet {
	var w *attributeWildcardSet
	memo := make(map[*AttributeGroup]*completeWildcard)
	for _, link := range derivationChain(ct) {
		local := wildcardOf(link.AnyAttribute, link.AttributeGroups, memo)
		content, simple := link.Content.(SimpleContent)
		switch {
		case link.Base == nil, simple && content.Restriction:
			w = uniteWildcard(nil, local)
		case isAnyType(link.Base) && !simple:
			ur := &completeWildcard{members: []*AttributeWildcard{urTypeWildcard}, processContents: urTypeWildcard.ProcessContents}
			w = uniteWildcard(uniteWildcard(nil, ur), local)
		default:
			w = uniteWildcard(w, local)
		}
	}
	return w
}

// wildcardOf returns the complete wildcard of a complex type or an
// attribute group whose xs:anyAttribute is local and whose attribute group
// references resolve to groups, per XML Schema Part 1, 3.4.2 clause 2, or
// nil when it is absent. With no group wildcard it is local alone.
// Otherwise its namespace constraint intersects local's with that of every
// group wildcard, and its process contents is local's, or with no local
// wildcard that of the first group wildcard (clause 2.2.2).
//
// memo holds the {attribute wildcard} of every group already derived, so a
// group referenced along several paths is derived once and the members
// stay distinct, rather than doubling at each level of a lattice of
// references. A group met again while its own wildcard is being derived, a
// reference cycle [Load] rejects, contributes none.
func wildcardOf(local *AttributeWildcard, groups []*AttributeGroup, memo map[*AttributeGroup]*completeWildcard) *completeWildcard {
	var w *completeWildcard
	seen := make(map[*AttributeWildcard]bool)
	if local != nil {
		w = &completeWildcard{members: []*AttributeWildcard{local}, processContents: local.ProcessContents}
		seen[local] = true
	}
	for _, g := range groups {
		gw, derived := memo[g]
		if !derived {
			memo[g] = nil
			gw = wildcardOf(g.AnyAttribute, g.AttributeGroups, memo)
			memo[g] = gw
		}
		if gw == nil {
			continue
		}
		if w == nil {
			w = &completeWildcard{processContents: gw.processContents}
		}
		for _, m := range gw.members {
			if !seen[m] {
				seen[m] = true
				w.members = append(w.members, m)
			}
		}
	}
	return w
}

// uniteWildcard returns the union of base and local, per XML Schema Part
// 1, 3.4.2: base alone when local is nil, local alone when base is nil,
// otherwise both, with the processContents of local.
func uniteWildcard(base *attributeWildcardSet, local *completeWildcard) *attributeWildcardSet {
	if local == nil {
		return base
	}
	united := &attributeWildcardSet{processContents: local.processContents}
	if base != nil {
		united.alternatives = slices.Clone(base.alternatives)
	}
	united.alternatives = append(united.alternatives, local)
	return united
}

// ownAttributeUses returns the attribute uses ct contributes itself, per
// XML Schema Part 1, 3.4.2 clauses 1 and 2: the ones it declares, then the
// {attribute uses} of the attribute groups it references
// ([appendGroupAttributeUses]). Those of its base are not included.
func ownAttributeUses(ct *ComplexType) []AttributeUse {
	if len(ct.AttributeGroups) == 0 {
		return ct.Attributes
	}
	out := slices.Clone(ct.Attributes)
	return appendGroupAttributeUses(out, ct.AttributeGroups, make(map[*AttributeGroup]bool))
}

// appendGroupAttributeUses appends to out the {attribute uses} of groups,
// per XML Schema Part 1, 3.6.2: the ones each group declares, united with
// those of the groups it references. Per 3.2.2 an xs:attribute whose use
// is prohibited corresponds to nothing at all inside a group, so it is
// left out: only one among the children of an xs:restriction removes a
// base use (3.4.2 clause 3.2). visited holds the groups already appended,
// as {attribute uses} is a set: a group referenced along several paths
// contributes once, and a reference cycle, which [Load] rejects, ends.
func appendGroupAttributeUses(out []AttributeUse, groups []*AttributeGroup, visited map[*AttributeGroup]bool) []AttributeUse {
	for _, g := range groups {
		if visited[g] {
			continue
		}
		visited[g] = true
		for _, use := range g.Attributes {
			if !use.Prohibited {
				out = append(out, use)
			}
		}
		out = appendGroupAttributeUses(out, g.AttributeGroups, visited)
	}
	return out
}

// allows reports whether the wildcard admits an attribute in namespace ns,
// "" for none, per XML Schema Part 1, 3.10.4 (cvc-wildcard-namespace):
// ##any admits every namespace, ##other every one but the target
// namespace and none (clause 2), and a list the namespaces it names
// (clause 3), ##targetNamespace and ##local standing for the target
// namespace and none (3.10.2).
func (w *AttributeWildcard) allows(ns string) bool {
	switch constraint := strings.TrimSpace(w.Namespace); constraint {
	case "", "##any":
		return true
	case "##other":
		return ns != "" && ns != w.TargetNamespace
	}
	for _, token := range strings.Fields(w.Namespace) {
		switch token {
		case "##targetNamespace":
			if ns == w.TargetNamespace {
				return true
			}
		case "##local":
			if ns == "" {
				return true
			}
		default:
			if ns == token {
				return true
			}
		}
	}
	return false
}

// undeclaredAttribute judges an attribute that no attribute use of its
// element's type declares, per XML Schema Part 1, 3.4.4 (cvc-complex-type
// clause 3.2): the type's attribute wildcard must admit its namespace.
// Once admitted, the wildcard's {process contents} decides, per 3.10.1:
// skip checks nothing more; lax validates the attribute against the
// top-level declaration of its name when one exists; strict requires that
// declaration.
func (v *validator) undeclaredAttribute(a xml.Attr, w *attributeWildcardSet, path string) []Violation {
	at := path + "/@" + a.Name.Local
	if w == nil || !slices.ContainsFunc(w.alternatives, func(alt *completeWildcard) bool { return alt.allows(a.Name.Space) }) {
		return []Violation{{
			Rule:   RuleAttribute,
			Path:   at,
			Reason: fmt.Sprintf("attribute %s is not declared and no attribute wildcard admits it", nameOf(a.Name)),
		}}
	}
	if w.processContents == "skip" {
		return nil
	}
	decl := v.globalAttribute(QName{Namespace: a.Name.Space, Local: a.Name.Local})
	if decl == nil {
		if w.processContents == "lax" {
			return nil
		}
		return []Violation{{
			Rule:   RuleAttribute,
			Path:   at,
			Reason: fmt.Sprintf("attribute %s matches a strict wildcard but has no top-level declaration", nameOf(a.Name)),
		}}
	}
	return attributeValueViolations(a.Value, *decl, path)
}

// globalAttribute returns the top-level attribute declaration named qn, or
// nil. Per XML Schema Part 1, 3.4.4 (cvc-complex-type clause 3.2) and
// 3.10.4 (cvc-wildcard) an attribute a lax or strict wildcard admits is
// assessed against the declaration its name resolves to by QName resolution
// (Instance), 3.15.4: an entry of the {attribute declarations} of the
// schema, which holds those of every schema document the validated one
// includes or imports, however deep ([reachableDeclarations]). Searching
// the validated document and its direct imports alone missed a declaration
// an imported document reaches through an import of its own, so a strict
// wildcard rejected an attribute the schema declares and a lax one let its
// value through unchecked.
func (v *validator) globalAttribute(qn QName) *AttributeUse {
	return v.schema.globalAttributes[qn]
}
