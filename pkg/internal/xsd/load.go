package xsd

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"path"
	"strconv"
	"strings"
)

// Load reads the XSD at rootPath from fsys, follows every xs:import
// and xs:include, and returns a fully-linked [Schema]. The link pass
// resolves every TypeRef and AttributeUse.TypeRef into a Type pointer
// drawn from the schema set; references that remain unresolved at the
// end of the pass surface as [ErrUnresolvedRef].
//
// fsys must be a forward-slash filesystem (the [io/fs] convention),
// which embed.FS satisfies natively and os.DirFS satisfies on every
// platform. Path resolution for imports/includes is relative to the
// importing schema's location.
//
// The loader checks ctx.Err() at each import boundary so callers
// that load large schema sets can bound provisioning time.
func Load(ctx context.Context, fsys fs.FS, rootPath string) (*Schema, error) {
	loader := &loader{
		fsys:    fsys,
		schemas: make(map[string]*Schema),
		raw:     make(map[string]*rawSchema),
		loading: make(map[string]bool),
	}
	root, _, err := loader.loadFile(ctx, rootPath, "")
	if err != nil {
		return nil, err
	}
	if err := loader.link(); err != nil {
		return nil, err
	}
	return root, nil
}

// loader tracks the work-in-progress state during a multi-file XSD
// load. The schemas map is keyed by absolute file path (forward-slash)
// so re-imports of the same file from different importers reuse one
// Schema instance.
type loader struct {
	fsys    fs.FS
	schemas map[string]*Schema
	raw     map[string]*rawSchema
	loading map[string]bool
}

// loadFile reads a single XSD by path. Returns the populated Schema
// plus the namespace URI so callers can register the import. importer
// is the path of the schema that triggered the load; "" for the root.
func (l *loader) loadFile(ctx context.Context, p, importer string) (*Schema, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	if importer != "" {
		p = path.Join(path.Dir(importer), p)
	}
	if existing, ok := l.schemas[p]; ok {
		return existing, existing.TargetNamespace, nil
	}
	if l.loading[p] {
		return nil, "", fmt.Errorf("%w at %s", ErrImportLoop, p)
	}
	l.loading[p] = true
	defer delete(l.loading, p)

	data, err := fs.ReadFile(l.fsys, p)
	if err != nil {
		return nil, "", fmt.Errorf("%w: reading %s: %w", ErrSchemaParse, p, err)
	}
	raw, prefixes, err := parseRawSchema(data)
	if err != nil {
		return nil, "", fmt.Errorf("%w at %s: %w", ErrSchemaParse, p, err)
	}
	schema := &Schema{
		TargetNamespace: raw.TargetNamespace,
		Elements:        make(map[QName]*Element),
		Types:           newBuiltinTypeRegistry(),
		Attributes:      make(map[QName]*AttributeUse),
		AttributeGroups: make(map[QName]*AttributeGroup),
		Imports:         make(map[string]*Schema),
	}
	l.schemas[p] = schema
	l.raw[p] = raw

	// Process imports and includes first so cross-schema references
	// can resolve during the same call's element/type walk.
	for _, imp := range raw.Imports {
		if imp.SchemaLocation == "" {
			continue
		}
		sub, ns, err := l.loadFile(ctx, imp.SchemaLocation, p)
		if err != nil {
			return nil, "", err
		}
		if imp.Namespace != "" && imp.Namespace != ns {
			return nil, "", fmt.Errorf("%w: imported schema %s declares targetNamespace %q but importer expected %q",
				ErrSchemaParse, imp.SchemaLocation, ns, imp.Namespace)
		}
		schema.Imports[ns] = sub
	}
	for _, inc := range raw.Includes {
		if inc.SchemaLocation == "" {
			continue
		}
		sub, _, err := l.loadFile(ctx, inc.SchemaLocation, p)
		if err != nil {
			return nil, "", err
		}
		for qn, t := range sub.Types {
			if _, isBuiltin := t.(*BuiltinType); isBuiltin {
				continue
			}
			schema.Types[qn] = t
		}
		maps.Copy(schema.Elements, sub.Elements)
		maps.Copy(schema.Attributes, sub.Attributes)
		maps.Copy(schema.AttributeGroups, sub.AttributeGroups)
	}

	if err := populateFromRaw(schema, raw, prefixes); err != nil {
		return nil, "", fmt.Errorf("at %s: %w", p, err)
	}
	return schema, schema.TargetNamespace, nil
}

// link walks every loaded schema and resolves every TypeRef into a
// Type pointer. References to built-in XS types resolve against the
// pre-populated registry; references to user types resolve against
// the owning schema or one of its imports. Once every reference
// resolves, it rejects a circular attribute group reference
// ([checkAttributeGroupCycles]).
func (l *loader) link() error {
	for _, schema := range l.schemas {
		if err := linkSchema(schema); err != nil {
			return err
		}
	}
	state := make(map[*AttributeGroup]groupVisit)
	for _, schema := range l.schemas {
		for _, g := range schema.AttributeGroups {
			if err := checkAttributeGroupCycles(g, state); err != nil {
				return err
			}
		}
	}
	return nil
}

func linkSchema(s *Schema) error {
	for _, t := range s.Types {
		if err := linkType(s, t); err != nil {
			return err
		}
	}
	for _, e := range s.Elements {
		if err := linkElement(s, e); err != nil {
			return err
		}
	}
	for _, a := range s.Attributes {
		if err := linkAttribute(s, a); err != nil {
			return err
		}
	}
	for _, g := range s.AttributeGroups {
		if err := linkAttributeGroup(s, g); err != nil {
			return err
		}
	}
	return nil
}

func linkType(s *Schema, t Type) error {
	switch tt := t.(type) {
	case *SimpleType:
		if !tt.BaseRef.IsZero() {
			base, err := lookupType(s, tt.BaseRef)
			if err != nil {
				return err
			}
			tt.Base = base
		}
		if tt.Union {
			return linkUnion(s, tt)
		}
		if tt.List {
			return linkList(s, tt)
		}
	case *ComplexType:
		if !tt.BaseRef.IsZero() {
			base, err := lookupType(s, tt.BaseRef)
			if err != nil {
				return err
			}
			tt.Base = base
		}
		if err := linkContent(s, tt.Content); err != nil {
			return err
		}
		for i := range tt.Attributes {
			if err := linkAttribute(s, &tt.Attributes[i]); err != nil {
				return err
			}
		}
		groups, err := resolveAttributeGroups(s, tt.AttributeGroupRefs)
		if err != nil {
			return err
		}
		tt.AttributeGroups = groups
	}
	return nil
}

// linkAttributeGroup resolves the attribute uses of the attribute group g
// and the groups it references. It rebuilds AttributeGroups from scratch,
// as a group an xs:include copies is linked once per schema that holds it.
//
// # Corpus evidence
//
//   - All corpora: no bundled schema defines or references an attribute
//     group, so no verdict of the 159 files that embed XML moves, through
//     the Factur-X and ZUGFeRD validators or under any bundled schema.
//   - All corpora: moving each of the 263 local attributes of the bundled
//     schemas into an attribute group of its own, referenced from a second
//     group, moves no violation under any bundled schema either, where
//     leaving xs:attributeGroup unmodelled raises 5989 violations of
//     undeclared attributes.
func linkAttributeGroup(s *Schema, g *AttributeGroup) error {
	for i := range g.Attributes {
		if err := linkAttribute(s, &g.Attributes[i]); err != nil {
			return err
		}
	}
	groups, err := resolveAttributeGroups(s, g.AttributeGroupRefs)
	if err != nil {
		return err
	}
	g.AttributeGroups = groups
	return nil
}

// resolveAttributeGroups returns the attribute group definitions refs
// name, per XML Schema Part 1, 3.6.2 the top-level ones their QNames
// resolve to, in s or in the schema s imports for their namespace.
func resolveAttributeGroups(s *Schema, refs []QName) ([]*AttributeGroup, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	out := make([]*AttributeGroup, 0, len(refs))
	for _, ref := range refs {
		g := lookupAttributeGroup(s, ref)
		if g == nil {
			return nil, fmt.Errorf("%w: attribute group %s", ErrUnresolvedRef, ref)
		}
		out = append(out, g)
	}
	return out, nil
}

// lookupAttributeGroup returns the top-level attribute group definition
// named qn, in s or in the schema s imports for qn's namespace, or nil.
func lookupAttributeGroup(s *Schema, qn QName) *AttributeGroup {
	if g, ok := s.AttributeGroups[qn]; ok {
		return g
	}
	if imp, ok := s.Imports[qn.Namespace]; ok {
		return imp.AttributeGroups[qn]
	}
	return nil
}

// groupVisit is the state of an attribute group in the depth-first walk of
// [checkAttributeGroupCycles].
type groupVisit uint8

const (
	groupUnvisited groupVisit = iota
	groupOnPath
	groupDone
)

// checkAttributeGroupCycles rejects a circular attribute group reference
// reachable from g. Per XML Schema Part 1, 3.6.3 (src-attribute_group
// clause 3) no xs:attributeGroup outside xs:redefine references itself,
// directly or at any depth through the groups it references. state carries
// the walk across calls, so each group is explored once and the check stays
// linear in the number of references.
func checkAttributeGroupCycles(g *AttributeGroup, state map[*AttributeGroup]groupVisit) error {
	switch state[g] {
	case groupOnPath:
		return fmt.Errorf("%w: xs:attributeGroup %s references itself", ErrSchemaParse, g.Name)
	case groupDone:
		return nil
	case groupUnvisited:
	}
	state[g] = groupOnPath
	for _, ref := range g.AttributeGroups {
		if err := checkAttributeGroupCycles(ref, state); err != nil {
			return err
		}
	}
	state[g] = groupDone
	return nil
}

// linkUnion resolves the {member type definitions} of the union st, per
// XML Schema Part 1, 3.14.2: the types its memberTypes attribute names, in
// order, then its xs:simpleType children, in order. It rebuilds Members
// from scratch, as a type an xs:include copies is linked once per schema
// that holds it.
func linkUnion(s *Schema, st *SimpleType) error {
	members := make([]Type, 0, len(st.MemberRefs)+len(st.InlineMembers))
	for _, ref := range st.MemberRefs {
		member, err := lookupType(s, ref)
		if err != nil {
			return err
		}
		members = append(members, member)
	}
	for _, inline := range st.InlineMembers {
		if err := linkType(s, inline); err != nil {
			return err
		}
		members = append(members, inline)
	}
	st.Members = members
	return nil
}

// linkList resolves the {item type definition} of the list st, per XML
// Schema Part 1, 3.14.2: the type its itemType attribute names, or else its
// xs:simpleType child.
func linkList(s *Schema, st *SimpleType) error {
	if st.InlineItem != nil {
		if err := linkType(s, st.InlineItem); err != nil {
			return err
		}
		st.ItemType = st.InlineItem
		return nil
	}
	item, err := lookupType(s, st.ItemRef)
	if err != nil {
		return err
	}
	st.ItemType = item
	return nil
}

func linkContent(s *Schema, c ContentModel) error {
	switch cm := c.(type) {
	case Sequence:
		for i := range cm.Particles {
			if err := linkParticle(s, &cm.Particles[i]); err != nil {
				return err
			}
		}
	case Choice:
		for i := range cm.Particles {
			if err := linkParticle(s, &cm.Particles[i]); err != nil {
				return err
			}
		}
	case SimpleContent:
		if cm.Inline != nil {
			return linkType(s, cm.Inline)
		}
	case Empty, nil:
	}
	return nil
}

func linkParticle(s *Schema, p *Particle) error {
	if p.Element != nil {
		return linkElement(s, p.Element)
	}
	return nil
}

func linkElement(s *Schema, e *Element) error {
	if e.Inline != nil {
		// Anonymous types live inside the element; link them too so
		// nested complex/simple types resolve their own refs.
		if err := linkType(s, e.Inline); err != nil {
			return err
		}
		e.Type = e.Inline
		return nil
	}
	if e.TypeRef.IsZero() {
		// xs:element without an explicit type defaults to xs:anyType.
		e.Type = s.Types[QName{Namespace: XSNamespace, Local: "anyType"}]
		return nil
	}
	t, err := lookupType(s, e.TypeRef)
	if err != nil {
		return err
	}
	e.Type = t
	return nil
}

// linkAttribute resolves the {type definition} of a, per XML Schema Part
// 1, 3.2.2: the xs:simpleType child when there is one, else the type the
// type attribute names, else the simple ur-type definition. The loader
// holds the xs:simpleType child in [AttributeUse.Type] already, with no
// TypeRef, so only its own references are left to resolve.
//
// # Corpus evidence
//
//   - All corpora: no schema the 159 files that embed XML validate against
//     gives an attribute an xs:simpleType child, so none of their verdicts
//     moves.
func linkAttribute(s *Schema, a *AttributeUse) error {
	if !a.Ref.IsZero() {
		return linkAttributeRef(s, a)
	}
	if a.TypeRef.IsZero() {
		if a.Type != nil {
			return linkType(s, a.Type)
		}
		a.Type = s.Types[QName{Namespace: XSNamespace, Local: "anySimpleType"}]
		return nil
	}
	t, err := lookupType(s, a.TypeRef)
	if err != nil {
		return err
	}
	a.Type = t
	return nil
}

// linkAttributeRef resolves the attribute use a, a reference to a top-level
// attribute declaration, per XML Schema Part 1, 3.2.2: its {attribute
// declaration} is the one a.Ref names, whose {type definition} a takes.
//
// A fixed value binds from either side: per 3.5.4 (cvc-au) the use's own,
// and per 3.2.4 (cvc-attribute clause 4) the declaration's. Per 3.5.6
// (au-props-correct clause 2) a use may only repeat the fixed value of its
// declaration, so a takes the declaration's fixed value when it states
// none.
//
// The declaration is linked first, in the schema that holds it, since the
// link pass visits schemas in no fixed order. It cannot recurse: a
// top-level declaration carries no ref ([populateFromRaw]).
//
// # Corpus evidence
//
//   - All corpora: no bundled schema references an attribute declaration,
//     so no verdict of the 159 files that embed XML moves.
func linkAttributeRef(s *Schema, a *AttributeUse) error {
	owner, decl := lookupAttribute(s, a.Ref)
	if decl == nil {
		return fmt.Errorf("%w: attribute %s", ErrUnresolvedRef, a.Ref)
	}
	if err := linkAttribute(owner, decl); err != nil {
		return err
	}
	a.Type = decl.Type
	if a.Fixed == "" {
		a.Fixed = decl.Fixed
	}
	return nil
}

// lookupAttribute returns the top-level attribute declaration named qn, in
// s or in the schema s imports for qn's namespace, together with the schema
// that holds it. Both are nil when neither declares it.
func lookupAttribute(s *Schema, qn QName) (*Schema, *AttributeUse) {
	if a, ok := s.Attributes[qn]; ok {
		return s, a
	}
	if imp, ok := s.Imports[qn.Namespace]; ok {
		if a, ok := imp.Attributes[qn]; ok {
			return imp, a
		}
	}
	return nil, nil
}

// lookupType resolves qn against the schema's own Types map, then
// against the imports indexed by qn.Namespace.
func lookupType(s *Schema, qn QName) (Type, error) {
	if t, ok := s.Types[qn]; ok {
		return t, nil
	}
	if imp, ok := s.Imports[qn.Namespace]; ok {
		if t, ok := imp.Types[qn]; ok {
			return t, nil
		}
	}
	if IsBuiltin(qn) {
		return newBuiltinTypeRegistry()[qn], nil
	}
	return nil, fmt.Errorf("%w: %s", ErrUnresolvedRef, qn)
}

// rawSchema is the encoding/xml decode target for an xs:schema
// document. It mirrors the XSD source structure so the loader can
// build the typed [Schema] in a second pass.
type rawSchema struct {
	XMLName              xml.Name            `xml:"http://www.w3.org/2001/XMLSchema schema"`
	TargetNamespace      string              `xml:"targetNamespace,attr"`
	AttributeFormDefault string              `xml:"attributeFormDefault,attr"`
	ElementFormDefault   string              `xml:"elementFormDefault,attr"`
	Elements             []rawElement        `xml:"element"`
	Attributes           []rawAttribute      `xml:"attribute"`
	AttributeGroups      []rawAttributeGroup `xml:"attributeGroup"`
	ComplexTypes         []rawComplexType    `xml:"complexType"`
	SimpleTypes          []rawSimpleType     `xml:"simpleType"`
	Imports              []rawImportInclude  `xml:"import"`
	Includes             []rawImportInclude  `xml:"include"`
}

type rawImportInclude struct {
	Namespace      string `xml:"namespace,attr"`
	SchemaLocation string `xml:"schemaLocation,attr"`
}

type rawElement struct {
	Name        string          `xml:"name,attr"`
	Type        string          `xml:"type,attr"`
	MinOccurs   string          `xml:"minOccurs,attr"`
	MaxOccurs   string          `xml:"maxOccurs,attr"`
	Nillable    string          `xml:"nillable,attr"`
	Default     string          `xml:"default,attr"`
	Fixed       string          `xml:"fixed,attr"`
	Form        string          `xml:"form,attr"`
	ComplexType *rawComplexType `xml:"complexType"`
	SimpleType  *rawSimpleType  `xml:"simpleType"`
	Ref         string          `xml:"ref,attr"`
}

type rawComplexType struct {
	Name            string              `xml:"name,attr"`
	Sequence        *rawSequence        `xml:"sequence"`
	Choice          *rawChoice          `xml:"choice"`
	Attributes      []rawAttribute      `xml:"attribute"`
	AttributeGroups []rawAttributeGroup `xml:"attributeGroup"`
	ComplexContent  *rawComplexContent  `xml:"complexContent"`
	SimpleContent   *rawSimpleContent   `xml:"simpleContent"`
	AnyAttribute    *rawAnyAttribute    `xml:"anyAttribute"`
}

// rawAttributeGroup captures an xs:attributeGroup, XML Schema Part 1,
// 3.6.2: the defining form, a child of xs:schema with a name and the
// attribute declarations, group references and wildcard it groups, or the
// referring form, with a ref and no content.
type rawAttributeGroup struct {
	Name            string              `xml:"name,attr"`
	Ref             string              `xml:"ref,attr"`
	Attributes      []rawAttribute      `xml:"attribute"`
	AttributeGroups []rawAttributeGroup `xml:"attributeGroup"`
	AnyAttribute    *rawAnyAttribute    `xml:"anyAttribute"`
}

// rawAnyAttribute captures an xs:anyAttribute, XML Schema Part 1, 3.10.2.
type rawAnyAttribute struct {
	Namespace       string `xml:"namespace,attr"`
	ProcessContents string `xml:"processContents,attr"`
}

type rawSimpleContent struct {
	Extension   *rawExtension                `xml:"extension"`
	Restriction *rawSimpleContentRestriction `xml:"restriction"`
}

// rawSimpleContentRestriction captures an xs:restriction under
// xs:simpleContent: an optional xs:simpleType, the facets, then the
// attribute uses. Every child the named fields do not claim lands in
// Facets, which [collectFacets] filters.
type rawSimpleContentRestriction struct {
	Base            string              `xml:"base,attr"`
	SimpleType      *rawSimpleType      `xml:"simpleType"`
	Attributes      []rawAttribute      `xml:"attribute"`
	AttributeGroups []rawAttributeGroup `xml:"attributeGroup"`
	AnyAttribute    *rawAnyAttribute    `xml:"anyAttribute"`
	Facets          []rawFacet          `xml:",any"`
}

type rawComplexContent struct {
	Extension *rawExtension `xml:"extension"`
}

type rawExtension struct {
	Base            string              `xml:"base,attr"`
	Sequence        *rawSequence        `xml:"sequence"`
	Choice          *rawChoice          `xml:"choice"`
	Attributes      []rawAttribute      `xml:"attribute"`
	AttributeGroups []rawAttributeGroup `xml:"attributeGroup"`
	AnyAttribute    *rawAnyAttribute    `xml:"anyAttribute"`
}

type rawSequence struct {
	Elements  []rawElement  `xml:"element"`
	Choices   []rawChoice   `xml:"choice"`
	Wildcards []rawWildcard `xml:"any"`
}

type rawChoice struct {
	Elements  []rawElement  `xml:"element"`
	Wildcards []rawWildcard `xml:"any"`
	MinOccurs string        `xml:"minOccurs,attr"`
	MaxOccurs string        `xml:"maxOccurs,attr"`
}

type rawWildcard struct {
	Namespace       string `xml:"namespace,attr"`
	ProcessContents string `xml:"processContents,attr"`
	MinOccurs       string `xml:"minOccurs,attr"`
	MaxOccurs       string `xml:"maxOccurs,attr"`
}

type rawAttribute struct {
	Name       string         `xml:"name,attr"`
	Type       string         `xml:"type,attr"`
	Use        string         `xml:"use,attr"`
	Default    string         `xml:"default,attr"`
	Fixed      string         `xml:"fixed,attr"`
	Form       string         `xml:"form,attr"`
	Ref        string         `xml:"ref,attr"`
	SimpleType *rawSimpleType `xml:"simpleType"`
}

type rawSimpleType struct {
	Name        string          `xml:"name,attr"`
	Restriction *rawRestriction `xml:"restriction"`
	List        *rawList        `xml:"list"`
	Union       *rawUnion       `xml:"union"`
}

// rawList captures an xs:list, XML Schema Part 1, 3.14.2: an itemType QName
// or an xs:simpleType child.
type rawList struct {
	ItemType   string         `xml:"itemType,attr"`
	SimpleType *rawSimpleType `xml:"simpleType"`
}

// rawUnion captures an xs:union, XML Schema Part 1, 3.14.2: a memberTypes
// list of QNames and xs:simpleType children.
type rawUnion struct {
	MemberTypes string          `xml:"memberTypes,attr"`
	SimpleTypes []rawSimpleType `xml:"simpleType"`
}

type rawRestriction struct {
	Base   string     `xml:"base,attr"`
	Facets []rawFacet `xml:",any"`
}

// rawFacet captures every child of xs:restriction in source order so
// SimpleType.Facets preserves the order producers wrote the
// constraints, which keeps multi-value enumeration lists stable.
type rawFacet struct {
	XMLName xml.Name
	Value   string `xml:"value,attr"`
}

// parseRawSchema decodes data into a rawSchema and side-loads the
// prefix → namespace bindings declared on the xs:schema start tag.
// encoding/xml resolves namespaces on element names but leaves
// attribute values as raw strings, so the loader needs the prefix
// map separately to translate type="lib:Foo" references.
func parseRawSchema(data []byte) (*rawSchema, map[string]string, error) {
	prefixes, err := extractSchemaPrefixes(data)
	if err != nil {
		return nil, nil, err
	}
	var raw rawSchema
	if err := xml.Unmarshal(data, &raw); err != nil {
		return nil, nil, err
	}
	if raw.XMLName.Local != "schema" || raw.XMLName.Space != XSNamespace {
		return nil, nil, errors.New("root element is not xs:schema")
	}
	return &raw, prefixes, nil
}

// extractSchemaPrefixes walks the token stream just far enough to
// reach the xs:schema StartElement, then returns prefix → URI for
// every xmlns: attribute on that element. The wellKnownPrefixes map
// is folded in so a schema that omits the conventional ram/qdt/udt
// declarations still resolves CII references.
func extractSchemaPrefixes(data []byte) (map[string]string, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, errors.New("no xs:schema start element found")
			}
			return nil, err
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if se.Name.Space != XSNamespace || se.Name.Local != "schema" {
			return nil, fmt.Errorf("first start element is %s, want xs:schema", se.Name.Local)
		}
		out := make(map[string]string, len(se.Attr)+len(wellKnownPrefixes))
		maps.Copy(out, wellKnownPrefixes)
		for _, a := range se.Attr {
			if a.Name.Space == "xmlns" {
				out[a.Name.Local] = a.Value
			}
		}
		return out, nil
	}
}

// schemaDoc is what the builders read off the xs:schema element of the
// schema document a declaration sits in.
type schemaDoc struct {
	// tns is the targetNamespace attribute, empty when absent.
	tns string
	// prefixes maps each namespace prefix declared on xs:schema, and
	// [wellKnownPrefixes], to its namespace.
	prefixes map[string]string
	// attributeFormDefault and elementFormDefault are the attributes of
	// those names, whose absence means unqualified (XML Schema Part 1,
	// 3.15.2).
	attributeFormDefault string
	elementFormDefault   string
}

// localNamespace returns the {target namespace} of a local declaration
// whose form attribute is form, per XML Schema Part 1, 3.2.2 and 3.3.2:
// the target namespace when form is qualified, or when form is absent and
// formDefault, the attributeFormDefault or elementFormDefault of its schema
// document, is qualified, and no namespace otherwise.
func (d *schemaDoc) localNamespace(form, formDefault string) string {
	form = strings.TrimSpace(form)
	if form == "" {
		form = strings.TrimSpace(formDefault)
	}
	if form == "qualified" {
		return d.tns
	}
	return ""
}

// localAttributeNamespace returns the {target namespace} of a local
// attribute declaration whose form attribute is form, per XML Schema Part
// 1, 3.2.2: the target namespace of the schema when form is qualified, or
// when form is absent and attributeFormDefault is qualified, and no
// namespace otherwise. A top-level declaration always takes the target
// namespace.
//
// # Corpus evidence
//
//   - All corpora: no bundled schema sets attributeFormDefault or form, so
//     their local attributes are all unqualified. Qualifying them instead
//     turns 91 of the 159 files that embed XML invalid through the Factur-X
//     and ZUGFeRD validators, as their unprefixed attributes no longer
//     match.
func (d *schemaDoc) localAttributeNamespace(form string) string {
	return d.localNamespace(form, d.attributeFormDefault)
}

// localElementNamespace returns the {target namespace} of a local element
// declaration, an xs:element with a name inside a complex type, whose form
// attribute is form, per XML Schema Part 1, 3.3.2: the target namespace of
// the schema when form is qualified, or when form is absent and
// elementFormDefault is qualified, and no namespace otherwise. A top-level
// declaration always takes the target namespace: the topLevelElement type
// of the schema for schemas gives it no form.
//
// # Corpus evidence
//
//   - All corpora: every bundled schema document sets elementFormDefault
//     to qualified and none sets form, so their local elements all stay in
//     the target namespace, and no verdict of the 159 files that embed XML
//     moves, through the Factur-X and ZUGFeRD validators or under any
//     bundled schema. Leaving them unqualified instead turns 91 of those
//     files invalid through the validators, as their child elements, all in
//     the target namespace, no longer match.
func (d *schemaDoc) localElementNamespace(form string) string {
	return d.localNamespace(form, d.elementFormDefault)
}

func populateFromRaw(s *Schema, raw *rawSchema, prefixes map[string]string) error {
	doc := &schemaDoc{
		tns:                  raw.TargetNamespace,
		prefixes:             prefixes,
		attributeFormDefault: raw.AttributeFormDefault,
		elementFormDefault:   raw.ElementFormDefault,
	}
	for i := range raw.ComplexTypes {
		ct, err := buildComplexType(doc, &raw.ComplexTypes[i])
		if err != nil {
			return err
		}
		s.Types[ct.QName()] = ct
	}
	for i := range raw.SimpleTypes {
		st, err := buildSimpleType(doc, &raw.SimpleTypes[i])
		if err != nil {
			return err
		}
		s.Types[st.QName()] = st
	}
	for i := range raw.Elements {
		el, err := buildElement(doc, &raw.Elements[i], doc.tns)
		if err != nil {
			return err
		}
		s.Elements[el.Name] = el
	}
	for i := range raw.Attributes {
		if raw.Attributes[i].Ref != "" || raw.Attributes[i].Name == "" {
			// Per XML Schema Part 1, 3.2.2 a top-level xs:attribute declares
			// the attribute its name names; the schema for schemas gives it
			// no ref attribute.
			return fmt.Errorf("%w: top-level xs:attribute needs a name and no ref", ErrSchemaParse)
		}
		a, err := buildAttribute(doc, &raw.Attributes[i], doc.tns)
		if err != nil {
			return err
		}
		s.Attributes[a.Name] = &a
	}
	for i := range raw.AttributeGroups {
		g, err := buildAttributeGroup(doc, &raw.AttributeGroups[i])
		if err != nil {
			return err
		}
		s.AttributeGroups[g.Name] = g
	}
	return nil
}

// buildElement builds the element declaration raw, named in namespace ns:
// the target namespace for a top-level declaration, and the one its form
// gives a local one ([schemaDoc.localElementNamespace]).
func buildElement(doc *schemaDoc, raw *rawElement, ns string) (*Element, error) {
	if raw.Ref != "" {
		return nil, fmt.Errorf("%w: top-level xs:element with ref attribute (CII does not use this)",
			ErrUnsupportedConstruct)
	}
	el := &Element{
		Name:      QName{Namespace: ns, Local: raw.Name},
		MinOccurs: occursOrDefault(raw.MinOccurs, 1),
		MaxOccurs: occursOrDefault(raw.MaxOccurs, 1),
		Nillable:  isTrueBoolean(raw.Nillable),
		Default:   raw.Default,
		Fixed:     raw.Fixed,
	}
	switch {
	case raw.Type != "":
		ref, err := doc.parseQName(raw.Type)
		if err != nil {
			return nil, err
		}
		el.TypeRef = ref
	case raw.ComplexType != nil:
		ct, err := buildComplexType(doc, raw.ComplexType)
		if err != nil {
			return nil, err
		}
		el.Inline = ct
	case raw.SimpleType != nil:
		st, err := buildSimpleType(doc, raw.SimpleType)
		if err != nil {
			return nil, err
		}
		el.Inline = st
	}
	return el, nil
}

func buildComplexType(doc *schemaDoc, raw *rawComplexType) (*ComplexType, error) {
	ct := &ComplexType{
		Name: QName{Namespace: doc.tns, Local: raw.Name},
	}
	switch {
	case raw.Sequence != nil:
		seq, err := buildSequence(doc, raw.Sequence)
		if err != nil {
			return nil, err
		}
		ct.Content = seq
	case raw.Choice != nil:
		ch, err := buildChoice(doc, raw.Choice)
		if err != nil {
			return nil, err
		}
		ct.Content = ch
	case raw.ComplexContent != nil && raw.ComplexContent.Extension != nil:
		ext := raw.ComplexContent.Extension
		baseRef, err := doc.parseQName(ext.Base)
		if err != nil {
			return nil, err
		}
		ct.BaseRef = baseRef
		switch {
		case ext.Sequence != nil:
			seq, err := buildSequence(doc, ext.Sequence)
			if err != nil {
				return nil, err
			}
			ct.Content = seq
		case ext.Choice != nil:
			ch, err := buildChoice(doc, ext.Choice)
			if err != nil {
				return nil, err
			}
			ct.Content = ch
		default:
			ct.Content = Empty{}
		}
		attrs, err := buildAttributes(doc, ext.Attributes)
		if err != nil {
			return nil, err
		}
		ct.Attributes = append(ct.Attributes, attrs...)
		groups, err := buildAttributeGroupRefs(doc, ext.AttributeGroups)
		if err != nil {
			return nil, err
		}
		ct.AttributeGroupRefs = append(ct.AttributeGroupRefs, groups...)
		ct.AnyAttribute = buildAnyAttribute(doc.tns, ext.AnyAttribute)
	case raw.SimpleContent != nil:
		if err := buildSimpleContent(doc, raw.SimpleContent, ct); err != nil {
			return nil, err
		}
	default:
		ct.Content = Empty{}
	}
	attrs, err := buildAttributes(doc, raw.Attributes)
	if err != nil {
		return nil, err
	}
	ct.Attributes = append(ct.Attributes, attrs...)
	groups, err := buildAttributeGroupRefs(doc, raw.AttributeGroups)
	if err != nil {
		return nil, err
	}
	ct.AttributeGroupRefs = append(ct.AttributeGroupRefs, groups...)
	if raw.AnyAttribute != nil {
		ct.AnyAttribute = buildAnyAttribute(doc.tns, raw.AnyAttribute)
	}
	return ct, nil
}

// buildAnyAttribute models an xs:anyAttribute declared in a schema whose
// target namespace is tns. Nil in, nil out.
func buildAnyAttribute(tns string, raw *rawAnyAttribute) *AttributeWildcard {
	if raw == nil {
		return nil
	}
	return &AttributeWildcard{
		Namespace:       raw.Namespace,
		TargetNamespace: tns,
		ProcessContents: raw.ProcessContents,
	}
}

// buildSimpleContent fills ct from an xs:simpleContent child, per XML
// Schema Part 1, 3.4.2: the base attribute names the {base type
// definition}, and the derivation's own attributes join the type's
// attribute uses. Per the same clause simpleContent holds exactly one
// xs:extension or xs:restriction, and each requires base.
func buildSimpleContent(doc *schemaDoc, raw *rawSimpleContent, ct *ComplexType) error {
	var base string
	var rawAttrs []rawAttribute
	var rawGroups []rawAttributeGroup
	switch {
	case raw.Extension != nil:
		base, rawAttrs, rawGroups = raw.Extension.Base, raw.Extension.Attributes, raw.Extension.AttributeGroups
		ct.AnyAttribute = buildAnyAttribute(doc.tns, raw.Extension.AnyAttribute)
		ct.Content = SimpleContent{}
	case raw.Restriction != nil:
		r := raw.Restriction
		base, rawAttrs, rawGroups = r.Base, r.Attributes, r.AttributeGroups
		ct.AnyAttribute = buildAnyAttribute(doc.tns, r.AnyAttribute)
		content := SimpleContent{Restriction: true, Facets: collectFacets(r.Facets)}
		if r.SimpleType != nil {
			inline, err := buildSimpleType(doc, r.SimpleType)
			if err != nil {
				return err
			}
			content.Inline = inline
		}
		ct.Content = content
	default:
		return fmt.Errorf("%w: xs:simpleContent of %q holds neither xs:extension nor xs:restriction",
			ErrSchemaParse, ct.Name.Local)
	}
	if base == "" {
		return fmt.Errorf("%w: xs:simpleContent of %q derives without a base", ErrSchemaParse, ct.Name.Local)
	}
	baseRef, err := doc.parseQName(base)
	if err != nil {
		return err
	}
	ct.BaseRef = baseRef
	attrs, err := buildAttributes(doc, rawAttrs)
	if err != nil {
		return err
	}
	ct.Attributes = append(ct.Attributes, attrs...)
	groups, err := buildAttributeGroupRefs(doc, rawGroups)
	if err != nil {
		return err
	}
	ct.AttributeGroupRefs = append(ct.AttributeGroupRefs, groups...)
	return nil
}

func buildSimpleType(doc *schemaDoc, raw *rawSimpleType) (*SimpleType, error) {
	st := &SimpleType{
		Name: QName{Namespace: doc.tns, Local: raw.Name},
	}
	if raw.Union != nil {
		return st, buildUnion(doc, raw.Union, st)
	}
	if raw.List != nil {
		return st, buildList(doc, raw.List, st)
	}
	if raw.Restriction == nil {
		return st, nil
	}
	if raw.Restriction.Base != "" {
		base, err := doc.parseQName(raw.Restriction.Base)
		if err != nil {
			return nil, err
		}
		st.BaseRef = base
	}
	st.Facets = collectFacets(raw.Restriction.Facets)
	return st, nil
}

// buildUnion fills st from an xs:union child. Per XML Schema Part 1,
// 3.14.2 the union's members are the memberTypes QNames, a
// white-space-separated list, then the xs:simpleType children.
func buildUnion(doc *schemaDoc, raw *rawUnion, st *SimpleType) error {
	st.Union = true
	for _, name := range strings.Fields(raw.MemberTypes) {
		ref, err := doc.parseQName(name)
		if err != nil {
			return err
		}
		st.MemberRefs = append(st.MemberRefs, ref)
	}
	for i := range raw.SimpleTypes {
		member, err := buildSimpleType(doc, &raw.SimpleTypes[i])
		if err != nil {
			return err
		}
		st.InlineMembers = append(st.InlineMembers, member)
	}
	return nil
}

// buildList fills st from an xs:list child. Per XML Schema Part 1, 3.14.3
// (src-list-itemType-or-simpleType) the xs:list carries an itemType
// attribute or an xs:simpleType child, never both nor neither.
func buildList(doc *schemaDoc, raw *rawList, st *SimpleType) error {
	st.List = true
	if (raw.ItemType == "") == (raw.SimpleType == nil) {
		return fmt.Errorf("%w: xs:list of %q needs exactly one of itemType and an xs:simpleType child",
			ErrSchemaParse, st.Name.Local)
	}
	if raw.SimpleType != nil {
		item, err := buildSimpleType(doc, raw.SimpleType)
		if err != nil {
			return err
		}
		st.InlineItem = item
		return nil
	}
	ref, err := doc.parseQName(raw.ItemType)
	if err != nil {
		return err
	}
	st.ItemRef = ref
	return nil
}

func buildSequence(doc *schemaDoc, raw *rawSequence) (Sequence, error) {
	particles, err := buildParticles(doc, raw.Elements, raw.Wildcards, raw.Choices)
	if err != nil {
		return Sequence{}, err
	}
	return Sequence{Particles: particles}, nil
}

func buildChoice(doc *schemaDoc, raw *rawChoice) (Choice, error) {
	particles, err := buildParticles(doc, raw.Elements, raw.Wildcards, nil)
	if err != nil {
		return Choice{}, err
	}
	return Choice{Particles: particles}, nil
}

func buildParticles(doc *schemaDoc, els []rawElement, wcs []rawWildcard, nested []rawChoice) ([]Particle, error) {
	out := make([]Particle, 0, len(els)+len(wcs))
	for i := range els {
		el, err := buildElement(doc, &els[i], doc.localElementNamespace(els[i].Form))
		if err != nil {
			return nil, err
		}
		out = append(out, Particle{Element: el})
	}
	for i := range wcs {
		out = append(out, Particle{Wildcard: &Wildcard{
			Namespace:       wcs[i].Namespace,
			ProcessContents: wcs[i].ProcessContents,
			MinOccurs:       occursOrDefault(wcs[i].MinOccurs, 1),
			MaxOccurs:       occursOrDefault(wcs[i].MaxOccurs, 1),
		}})
	}
	if len(nested) > 0 {
		// Nested xs:choice inside xs:sequence is technically supported
		// by XSD but CII does not exercise it. Reject early so a future
		// schema update that uses it surfaces as a load error rather
		// than as a silent validation gap.
		return nil, fmt.Errorf("%w: xs:choice nested inside xs:sequence", ErrUnsupportedConstruct)
	}
	return out, nil
}

// buildAttributes builds the attribute uses raws of a complex type: each
// local declaration in the namespace its form gives it
// ([schemaDoc.localAttributeNamespace]), and each reference to a top-level
// declaration ([buildAttributeRef]).
func buildAttributes(doc *schemaDoc, raws []rawAttribute) ([]AttributeUse, error) {
	out := make([]AttributeUse, 0, len(raws))
	for i := range raws {
		var au AttributeUse
		var err error
		if raws[i].Ref != "" {
			au, err = buildAttributeRef(doc, &raws[i])
		} else {
			au, err = buildAttribute(doc, &raws[i], doc.localAttributeNamespace(raws[i].Form))
		}
		if err != nil {
			return nil, err
		}
		out = append(out, au)
	}
	return out, nil
}

// buildAttributeGroup builds the top-level xs:attributeGroup raw. Per XML
// Schema Part 1, 3.6.2 the defining form carries a name and no ref, its
// name is in the target namespace, and it holds attribute declarations,
// attribute group references and an xs:anyAttribute.
func buildAttributeGroup(doc *schemaDoc, raw *rawAttributeGroup) (*AttributeGroup, error) {
	if raw.Name == "" || raw.Ref != "" {
		return nil, fmt.Errorf("%w: top-level xs:attributeGroup needs a name and no ref", ErrSchemaParse)
	}
	attrs, err := buildAttributes(doc, raw.Attributes)
	if err != nil {
		return nil, err
	}
	refs, err := buildAttributeGroupRefs(doc, raw.AttributeGroups)
	if err != nil {
		return nil, err
	}
	return &AttributeGroup{
		Name:               QName{Namespace: doc.tns, Local: raw.Name},
		Attributes:         attrs,
		AttributeGroupRefs: refs,
		AnyAttribute:       buildAnyAttribute(doc.tns, raw.AnyAttribute),
	}, nil
}

// buildAttributeGroupRefs parses the xs:attributeGroup children raws of a
// complex type or an attribute group. Per XML Schema Part 1, 3.6.2 each is
// the referring form: a ref, no name, and no content.
func buildAttributeGroupRefs(doc *schemaDoc, raws []rawAttributeGroup) ([]QName, error) {
	if len(raws) == 0 {
		return nil, nil
	}
	out := make([]QName, 0, len(raws))
	for i := range raws {
		r := &raws[i]
		if r.Ref == "" || r.Name != "" || len(r.Attributes) > 0 || len(r.AttributeGroups) > 0 || r.AnyAttribute != nil {
			return nil, fmt.Errorf("%w: a nested xs:attributeGroup needs a ref, and no name or content", ErrSchemaParse)
		}
		ref, err := doc.parseQName(r.Ref)
		if err != nil {
			return nil, err
		}
		out = append(out, ref)
	}
	return out, nil
}

// buildAttributeRef builds the attribute use a, an xs:attribute ref. Per
// XML Schema Part 1, 3.2.2 its {attribute declaration} is the top-level one
// ref names, which the link pass resolves ([linkAttributeRef]), and its
// required and value constraint are its own. Per 3.2.3 (src-attribute
// clauses 3.1 and 3.2) it carries no name, type, form or xs:simpleType.
func buildAttributeRef(doc *schemaDoc, a *rawAttribute) (AttributeUse, error) {
	if a.Name != "" || a.Type != "" || a.Form != "" || a.SimpleType != nil {
		return AttributeUse{}, fmt.Errorf("%w: xs:attribute ref=%q also carries a name, type, form or xs:simpleType",
			ErrSchemaParse, a.Ref)
	}
	ref, err := doc.parseQName(a.Ref)
	if err != nil {
		return AttributeUse{}, err
	}
	return AttributeUse{
		Name:       ref,
		Ref:        ref,
		Default:    a.Default,
		Fixed:      a.Fixed,
		Required:   a.Use == "required",
		Prohibited: a.Use == "prohibited",
	}, nil
}

// buildAttribute builds the attribute declaration a, named in namespace ns.
func buildAttribute(doc *schemaDoc, a *rawAttribute, ns string) (AttributeUse, error) {
	au := AttributeUse{
		Name:       QName{Namespace: ns, Local: a.Name},
		Default:    a.Default,
		Fixed:      a.Fixed,
		Required:   a.Use == "required",
		Prohibited: a.Use == "prohibited",
	}
	if a.Type != "" {
		ref, err := doc.parseQName(a.Type)
		if err != nil {
			return AttributeUse{}, err
		}
		au.TypeRef = ref
	} else if a.SimpleType != nil {
		st, err := buildSimpleType(doc, a.SimpleType)
		if err != nil {
			return AttributeUse{}, err
		}
		au.Type = st
	}
	return au, nil
}

func collectFacets(raws []rawFacet) []Facet {
	out := make([]Facet, 0, len(raws))
	for _, f := range raws {
		if f.XMLName.Space != XSNamespace {
			continue
		}
		switch f.XMLName.Local {
		case "pattern":
			out = append(out, Pattern{Value: f.Value})
		case "enumeration":
			out = append(out, Enumeration{Value: f.Value})
		case "length":
			out = append(out, Length{Value: atoiSafe(f.Value)})
		case "minLength":
			out = append(out, MinLength{Value: atoiSafe(f.Value)})
		case "maxLength":
			out = append(out, MaxLength{Value: atoiSafe(f.Value)})
		case "minInclusive":
			out = append(out, MinInclusive{Value: f.Value})
		case "maxInclusive":
			out = append(out, MaxInclusive{Value: f.Value})
		case "minExclusive":
			out = append(out, MinExclusive{Value: f.Value})
		case "maxExclusive":
			out = append(out, MaxExclusive{Value: f.Value})
		case "fractionDigits":
			out = append(out, FractionDigits{Value: atoiSafe(f.Value)})
		case "totalDigits":
			out = append(out, TotalDigits{Value: atoiSafe(f.Value)})
		case "whiteSpace":
			out = append(out, WhiteSpace{Value: f.Value})
		}
	}
	return out
}

// parseQName turns a prefixed name from an XSD attribute (e.g.
// "xs:string", "ram:IDType") into a QName by consulting the per-schema
// prefix map captured at load time. Unprefixed names assume the
// schema's target namespace.
func (d *schemaDoc) parseQName(raw string) (QName, error) {
	for i := 0; i < len(raw); i++ {
		if raw[i] == ':' {
			prefix := raw[:i]
			local := raw[i+1:]
			ns, ok := d.prefixes[prefix]
			if !ok {
				return QName{}, fmt.Errorf("%w: unknown prefix %q in %q", ErrSchemaParse, prefix, raw)
			}
			return QName{Namespace: ns, Local: local}, nil
		}
	}
	return QName{Namespace: d.tns, Local: raw}, nil
}

// wellKnownPrefixes maps XSD prefix aliases to their namespace URIs.
// The CII building blocks use a stable set of prefixes across all
// shipped schema generations.
//
//nolint:gochecknoglobals // immutable lookup table.
var wellKnownPrefixes = map[string]string{
	"xs":   XSNamespace,
	"xsd":  XSNamespace,
	"rsm":  "urn:un:unece:uncefact:data:standard:CrossIndustryInvoice:100",
	"ram":  "urn:un:unece:uncefact:data:standard:ReusableAggregateBusinessInformationEntity:100",
	"qdt":  "urn:un:unece:uncefact:data:standard:QualifiedDataType:100",
	"udt":  "urn:un:unece:uncefact:data:standard:UnqualifiedDataType:100",
	"ccts": "urn:un:unece:uncefact:documentation:standard:CoreComponentsTechnicalSpecification:2",
}

func occursOrDefault(raw string, def int) int {
	if raw == "" {
		return def
	}
	if raw == "unbounded" {
		return MaxUnbounded
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return n
}

func atoiSafe(raw string) int {
	n, _ := strconv.Atoi(raw)
	return n
}
