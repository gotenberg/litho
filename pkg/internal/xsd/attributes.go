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
// kept as the wildcards whose union (XML Schema Part 1, 3.10.6) it is: an
// attribute's namespace is allowed when any one of them allows it. Per
// 3.10.6 the union of two expressible constraints allows exactly what
// either allows, and a union 3.10.6 calls not expressible (clause 5.3) is
// a schema error, which the set judges the same way rather than blaming
// the instance. processContents is the one the union takes, that of the
// most derived wildcard.
type attributeWildcardSet struct {
	alternatives    []*AttributeWildcard
	processContents string
}

// urTypeWildcard is the {attribute wildcard} of xs:anyType, per XML Schema
// Part 1, 3.4.7: any namespace, lax processing.
//
//nolint:gochecknoglobals // immutable component of the ur-type.
var urTypeWildcard = &AttributeWildcard{Namespace: "##any", ProcessContents: "lax"}

// attributeWildcard derives the {attribute wildcard} of ct along its
// derivation chain, per XML Schema Part 1, 3.4.2. A type that derives by
// restriction, explicitly under xs:simpleContent or implicitly from the
// ur-type when it names no base, keeps its own xs:anyAttribute alone. A
// type that derives by extension keeps its base's wildcard, united with
// its own when it declares one. A base that is a simple type has none,
// and an explicit extension of xs:anyType inherits [urTypeWildcard].
// Returns nil when the type admits no undeclared attribute.
func attributeWildcard(ct *ComplexType) *attributeWildcardSet {
	var w *attributeWildcardSet
	for _, link := range derivationChain(ct) {
		content, simple := link.Content.(SimpleContent)
		switch {
		case link.Base == nil, simple && content.Restriction:
			w = uniteWildcard(nil, link.AnyAttribute)
		case isAnyType(link.Base) && !simple:
			w = uniteWildcard(uniteWildcard(nil, urTypeWildcard), link.AnyAttribute)
		default:
			w = uniteWildcard(w, link.AnyAttribute)
		}
	}
	return w
}

// uniteWildcard returns the union of base and local, per XML Schema Part
// 1, 3.4.2: base alone when local is nil, local alone when base is nil,
// otherwise both, with the processContents of local.
func uniteWildcard(base *attributeWildcardSet, local *AttributeWildcard) *attributeWildcardSet {
	if local == nil {
		return base
	}
	united := &attributeWildcardSet{processContents: local.ProcessContents}
	if base != nil {
		united.alternatives = slices.Clone(base.alternatives)
	}
	united.alternatives = append(united.alternatives, local)
	return united
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
	if w == nil || !slices.ContainsFunc(w.alternatives, func(alt *AttributeWildcard) bool { return alt.allows(a.Name.Space) }) {
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

// globalAttribute returns the top-level attribute declaration named qn, in
// the validated schema or the one it imports for qn's namespace, or nil.
func (v *validator) globalAttribute(qn QName) *AttributeUse {
	if a, ok := v.schema.Attributes[qn]; ok {
		return a
	}
	if imp, ok := v.schema.Imports[qn.Namespace]; ok {
		if a, ok := imp.Attributes[qn]; ok {
			return a
		}
	}
	return nil
}
