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
	"slices"
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
		fsys:     fsys,
		schemas:  make(map[string]*Schema),
		raw:      make(map[string]*rawSchema),
		loading:  make(map[string]bool),
		own:      make(map[*Schema]*Schema),
		includer: make(map[*Schema]*Schema),
		includes: make(map[*Schema][]*Schema),
	}
	root, _, err := loader.loadFile(ctx, rootPath, "")
	if err != nil {
		return nil, err
	}
	if err := loader.link(); err != nil {
		return nil, err
	}
	for doc := range loader.own {
		doc.globalElements = loader.elementDeclarations(doc)
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
	// own maps the Schema of each schema document to the components the
	// document declares itself, apart from those its xs:include children
	// copy in, so the link pass links each component once, in the scope
	// of the document that wrote it ([refScope]).
	own map[*Schema]*Schema
	// includer maps the Schema of each included schema document to that
	// of the first document that includes it.
	includer map[*Schema]*Schema
	// includes maps the Schema of each schema document to those of the
	// documents its xs:include children name, in source order.
	includes map[*Schema][]*Schema
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
	raw, err := parseRawSchema(data)
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
		if _, ok := l.includer[sub]; !ok {
			l.includer[sub] = schema
		}
		l.includes[schema] = append(l.includes[schema], sub)
		copyComponents(schema, sub)
	}

	own := &Schema{
		TargetNamespace: raw.TargetNamespace,
		Elements:        make(map[QName]*Element),
		Types:           make(map[QName]Type),
		Attributes:      make(map[QName]*AttributeUse),
		AttributeGroups: make(map[QName]*AttributeGroup),
	}
	if err := populateFromRaw(own, raw); err != nil {
		return nil, "", fmt.Errorf("at %s: %w", p, err)
	}
	copyComponents(schema, own)
	l.own[schema] = own
	return schema, schema.TargetNamespace, nil
}

// copyComponents adds the top-level components of src, built-in types
// aside, to those of dst.
func copyComponents(dst, src *Schema) {
	for qn, t := range src.Types {
		if _, isBuiltin := t.(*BuiltinType); isBuiltin {
			continue
		}
		dst.Types[qn] = t
	}
	maps.Copy(dst.Elements, src.Elements)
	maps.Copy(dst.Attributes, src.Attributes)
	maps.Copy(dst.AttributeGroups, src.AttributeGroups)
}

// includeRoot returns the Schema that the components of the schema
// document doc join: that of the outermost document that includes doc,
// directly or through other includes, or doc's own when none does. Its
// maps hold every component of the schema doc belongs to (XML Schema
// Part 1, 4.2.1). The loader rejects an include cycle ([ErrImportLoop]),
// so the chain takes at most one step per loaded document.
func (l *loader) includeRoot(doc *Schema) *Schema {
	for range len(l.schemas) {
		includer, ok := l.includer[doc]
		if !ok {
			break
		}
		doc = includer
	}
	return doc
}

// elementDeclarations returns the {element declarations} of the schema
// corresponding to the schema document doc, keyed by name. Per XML Schema
// Part 1, 4.2.1 (src-include clause 3) and 4.2.3 (src-import) that schema
// holds the components of doc, and all the components of each schema
// corresponding to a document doc includes or imports, so the walk follows
// both, through every document it reaches. doc's own declarations, which
// hold those of the documents it includes ([copyComponents]), come first,
// and a name keeps the first declaration the walk finds, which the order
// of the walk fixes.
//
// # Corpus evidence
//
//   - All corpora: no bundled schema uses xs:include, and none of the 28
//     reaches through an import of an import a top-level element declaration
//     that its own document and its direct imports lack, so each resolves
//     the same document elements as when only those counted. Each loads to
//     the same model, and no verdict of the 159 files that embed XML moves,
//     through the Factur-X and ZUGFeRD validators or under any bundled
//     schema.
func (l *loader) elementDeclarations(doc *Schema) map[QName]*Element {
	out := make(map[QName]*Element)
	seen := make(map[*Schema]bool)
	stack := []*Schema{doc}
	for len(stack) > 0 {
		s := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[s] {
			continue
		}
		seen[s] = true
		for qn, el := range s.Elements {
			if _, ok := out[qn]; !ok {
				out[qn] = el
			}
		}
		// Pushed in reverse, so the walk visits the includes in source order,
		// then the imports by namespace.
		imports := slices.Sorted(maps.Keys(s.Imports))
		for _, ns := range slices.Backward(imports) {
			stack = append(stack, s.Imports[ns])
		}
		for _, inc := range slices.Backward(l.includes[s]) {
			stack = append(stack, inc)
		}
	}
	return out
}

// link resolves the references of every loaded schema document: each
// TypeRef into a Type pointer, and each attribute and attribute group
// reference into the declaration it names. References to built-in XS
// types resolve against the pre-populated registry. Each component links
// once, in the scope of the document that wrote it ([refScope]). Once
// every reference resolves, it rejects a circular attribute group
// reference ([checkAttributeGroupCycles]).
func (l *loader) link() error {
	scopes := make(map[*Schema]refScope, len(l.own))
	for doc := range l.own {
		scopes[doc] = refScope{schema: l.includeRoot(doc), doc: doc}
	}
	// An attribute use that references a top-level attribute declaration
	// takes the declaration's type ([linkAttributeRef]), so every
	// declaration links first.
	for doc, own := range l.own {
		for _, a := range own.Attributes {
			if err := linkAttribute(scopes[doc], a); err != nil {
				return err
			}
		}
	}
	for doc, own := range l.own {
		if err := linkComponents(scopes[doc], own); err != nil {
			return err
		}
	}
	state := make(map[*AttributeGroup]groupVisit)
	for _, own := range l.own {
		for _, g := range own.AttributeGroups {
			if err := checkAttributeGroupCycles(g, state); err != nil {
				return err
			}
		}
	}
	return nil
}

// refScope is where a QName a schema document writes resolves. Per XML
// Schema Part 1, 4.2.1 an xs:include brings the components of the
// included document into the including schema, so a QName in the target
// namespace resolves against every component of that schema, whichever
// document declares it. Per 3.15.3 (src-resolve clause 4) a QName in
// another namespace resolves only when the schema document that carries
// it imports that namespace: an included document resolves through its
// own xs:import children, and its namespace bindings stay its own
// ([qnameScope]).
//
// # Corpus evidence
//
//   - All corpora: no bundled schema uses xs:include, so each component
//     resolves in the document that declares it alone. Each of the 28
//     loads to the same model as when an included component also linked
//     in the including document, and no verdict of the 159 files that
//     embed XML moves, through the Factur-X and ZUGFeRD validators or
//     under any bundled schema.
type refScope struct {
	// schema holds the components of the schema the document belongs to
	// ([loader.includeRoot]).
	schema *Schema
	// doc is the Schema of the document that wrote the QName, whose
	// Imports resolve the other namespaces.
	doc *Schema
}

// lookupComponent returns the component named qn in the map components
// picks: that of sc.schema, then that of the schema sc.doc imports for
// qn's namespace.
func lookupComponent[T any](sc refScope, qn QName, components func(*Schema) map[QName]T) (T, bool) {
	if c, ok := components(sc.schema)[qn]; ok {
		return c, true
	}
	if imp, ok := sc.doc.Imports[qn.Namespace]; ok {
		c, ok := components(imp)[qn]
		return c, ok
	}
	var zero T
	return zero, false
}

// linkComponents links the types, element declarations and attribute
// groups own holds, the components one schema document declares, in the
// scope sc of that document. [loader.link] links its attribute
// declarations beforehand.
func linkComponents(sc refScope, own *Schema) error {
	for _, t := range own.Types {
		if err := linkType(sc, t); err != nil {
			return err
		}
	}
	for _, e := range own.Elements {
		if err := linkElement(sc, e); err != nil {
			return err
		}
	}
	for _, g := range own.AttributeGroups {
		if err := linkAttributeGroup(sc, g); err != nil {
			return err
		}
	}
	return nil
}

func linkType(sc refScope, t Type) error {
	switch tt := t.(type) {
	case *SimpleType:
		if !tt.BaseRef.IsZero() {
			base, err := lookupType(sc, tt.BaseRef)
			if err != nil {
				return err
			}
			tt.Base = base
		}
		if tt.Union {
			return linkUnion(sc, tt)
		}
		if tt.List {
			return linkList(sc, tt)
		}
	case *ComplexType:
		if !tt.BaseRef.IsZero() {
			base, err := lookupType(sc, tt.BaseRef)
			if err != nil {
				return err
			}
			tt.Base = base
		}
		if err := linkContent(sc, tt.Content); err != nil {
			return err
		}
		for i := range tt.Attributes {
			if err := linkAttribute(sc, &tt.Attributes[i]); err != nil {
				return err
			}
		}
		groups, err := resolveAttributeGroups(sc, tt.AttributeGroupRefs)
		if err != nil {
			return err
		}
		tt.AttributeGroups = groups
	}
	return nil
}

// linkAttributeGroup resolves the attribute uses of the attribute group g
// and the groups it references.
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
func linkAttributeGroup(sc refScope, g *AttributeGroup) error {
	for i := range g.Attributes {
		if err := linkAttribute(sc, &g.Attributes[i]); err != nil {
			return err
		}
	}
	groups, err := resolveAttributeGroups(sc, g.AttributeGroupRefs)
	if err != nil {
		return err
	}
	g.AttributeGroups = groups
	return nil
}

// resolveAttributeGroups returns the attribute group definitions refs
// name, per XML Schema Part 1, 3.6.2 the top-level ones their QNames
// resolve to in sc.
func resolveAttributeGroups(sc refScope, refs []QName) ([]*AttributeGroup, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	out := make([]*AttributeGroup, 0, len(refs))
	for _, ref := range refs {
		g, ok := lookupComponent(sc, ref, func(s *Schema) map[QName]*AttributeGroup { return s.AttributeGroups })
		if !ok {
			return nil, fmt.Errorf("%w: attribute group %s", ErrUnresolvedRef, ref)
		}
		out = append(out, g)
	}
	return out, nil
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
// order, then its xs:simpleType children, in order.
func linkUnion(sc refScope, st *SimpleType) error {
	members := make([]Type, 0, len(st.MemberRefs)+len(st.InlineMembers))
	for _, ref := range st.MemberRefs {
		member, err := lookupType(sc, ref)
		if err != nil {
			return err
		}
		members = append(members, member)
	}
	for _, inline := range st.InlineMembers {
		if err := linkType(sc, inline); err != nil {
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
func linkList(sc refScope, st *SimpleType) error {
	if st.InlineItem != nil {
		if err := linkType(sc, st.InlineItem); err != nil {
			return err
		}
		st.ItemType = st.InlineItem
		return nil
	}
	item, err := lookupType(sc, st.ItemRef)
	if err != nil {
		return err
	}
	st.ItemType = item
	return nil
}

func linkContent(sc refScope, c ContentModel) error {
	switch cm := c.(type) {
	case Sequence:
		for i := range cm.Particles {
			if err := linkParticle(sc, &cm.Particles[i]); err != nil {
				return err
			}
		}
	case Choice:
		for i := range cm.Particles {
			if err := linkParticle(sc, &cm.Particles[i]); err != nil {
				return err
			}
		}
	case SimpleContent:
		if cm.Inline != nil {
			return linkType(sc, cm.Inline)
		}
	case Empty, nil:
	}
	return nil
}

func linkParticle(sc refScope, p *Particle) error {
	if p.Element != nil {
		return linkElement(sc, p.Element)
	}
	return nil
}

func linkElement(sc refScope, e *Element) error {
	if e.Inline != nil {
		// Anonymous types live inside the element; link them too so
		// nested complex/simple types resolve their own refs.
		if err := linkType(sc, e.Inline); err != nil {
			return err
		}
		e.Type = e.Inline
		return nil
	}
	if e.TypeRef.IsZero() {
		// xs:element without an explicit type defaults to xs:anyType.
		e.Type = sc.schema.Types[QName{Namespace: XSNamespace, Local: "anyType"}]
		return nil
	}
	t, err := lookupType(sc, e.TypeRef)
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
func linkAttribute(sc refScope, a *AttributeUse) error {
	if !a.Ref.IsZero() {
		return linkAttributeRef(sc, a)
	}
	if a.TypeRef.IsZero() {
		if a.Type != nil {
			return linkType(sc, a.Type)
		}
		a.Type = sc.schema.Types[QName{Namespace: XSNamespace, Local: "anySimpleType"}]
		return nil
	}
	t, err := lookupType(sc, a.TypeRef)
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
// [loader.link] links every top-level declaration before any reference to
// one, in the scope of the document that declares it. A top-level
// declaration carries no ref ([populateFromRaw]), so none of them reaches
// here.
//
// # Corpus evidence
//
//   - All corpora: no bundled schema references an attribute declaration,
//     so no verdict of the 159 files that embed XML moves.
func linkAttributeRef(sc refScope, a *AttributeUse) error {
	decl := lookupAttribute(sc, a.Ref)
	if decl == nil {
		return fmt.Errorf("%w: attribute %s", ErrUnresolvedRef, a.Ref)
	}
	a.Type = decl.Type
	if a.Fixed == "" {
		a.Fixed = decl.Fixed
	}
	return nil
}

// lookupAttribute returns the top-level attribute declaration named qn in
// sc, or nil when none is.
func lookupAttribute(sc refScope, qn QName) *AttributeUse {
	a, _ := lookupComponent(sc, qn, func(s *Schema) map[QName]*AttributeUse { return s.Attributes })
	return a
}

// lookupType resolves qn in sc, then against the built-in types.
func lookupType(sc refScope, qn QName) (Type, error) {
	if t, ok := lookupComponent(sc, qn, func(s *Schema) map[QName]Type { return s.Types }); ok {
		return t, nil
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

// parseRawSchema decodes data into a rawSchema. It reads the tokens
// through a [qnameScope], so each QName-valued attribute the rawSchema
// holds already names the expanded name it denotes ([parseQName]).
func parseRawSchema(data []byte) (*rawSchema, error) {
	var raw rawSchema
	if err := xml.NewTokenDecoder(newQNameScope(data)).Decode(&raw); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("no xs:schema start element found")
		}
		return nil, err
	}
	if raw.XMLName.Local != "schema" || raw.XMLName.Space != XSNamespace {
		return nil, errors.New("root element is not xs:schema")
	}
	return &raw, nil
}

// qnameScope is an [xml.TokenReader] over the raw tokens of a schema
// document. It tracks the namespace declarations in scope at each
// element and rewrites each QName-valued attribute of an XML Schema
// element into the expanded name it denotes, written {namespace}local.
// encoding/xml resolves the names of elements and attributes but leaves
// attribute values as written, and the declarations an element makes go
// out of scope at its end tag, so the loader resolves the values here.
//
// Per XML Schema Part 1, 3.15.3 (QName Interpretation), a prefixed QName
// takes the namespace its prefix is bound to in the in-scope namespaces
// of the element that carries it, and an unprefixed one the default
// namespace in scope there, or no namespace when none is. The target
// namespace plays no part: a schema document that names its own types
// unprefixed binds the default namespace to its target namespace.
//
// The content of xs:annotation stays as written: its xs:appinfo and
// xs:documentation hold markup no component is built from.
//
// # Corpus evidence
//
//   - All corpora: no bundled schema writes an unprefixed QName or declares
//     a namespace below xs:schema, so each of the 28 loads to the same model
//     as when an unprefixed QName took the target namespace, and no verdict
//     of the 159 files that embed XML moves, through the Factur-X and
//     ZUGFeRD validators or under any bundled schema.
type qnameScope struct {
	dec *xml.Decoder
	// bindings holds the namespace declarations of each open element,
	// innermost last, keyed by prefix. The empty prefix is the default
	// namespace, which xmlns="" binds to no namespace.
	bindings []map[string]string
	// annotation is the depth of the open xs:annotation, 0 outside one.
	annotation int
}

func newQNameScope(data []byte) *qnameScope {
	return &qnameScope{dec: xml.NewDecoder(bytes.NewReader(data))}
}

// Token implements [xml.TokenReader].
func (q *qnameScope) Token() (xml.Token, error) {
	tok, err := q.dec.RawToken()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case xml.StartElement:
		t = t.Copy()
		q.bindings = append(q.bindings, namespaceDeclarations(t.Attr))
		if q.annotation != 0 || !q.isSchemaElement(t.Name) {
			return t, nil
		}
		if t.Name.Local == "annotation" {
			q.annotation = len(q.bindings)
			return t, nil
		}
		if err := q.expandAttributes(t.Attr); err != nil {
			return nil, err
		}
		return t, nil
	case xml.EndElement:
		if q.annotation == len(q.bindings) {
			q.annotation = 0
		}
		if len(q.bindings) > 0 {
			q.bindings = q.bindings[:len(q.bindings)-1]
		}
	}
	return xml.CopyToken(tok), nil
}

// namespaceDeclarations returns the prefix → namespace bindings the
// xmlns and xmlns:prefix attributes among attrs, as a raw token carries
// them, declare. The default namespace is keyed by the empty prefix.
// Nil when attrs declares none.
func namespaceDeclarations(attrs []xml.Attr) map[string]string {
	var out map[string]string
	for _, a := range attrs {
		prefix, ok := "", false
		switch {
		case a.Name.Space == "xmlns":
			prefix, ok = a.Name.Local, true
		case a.Name.Space == "" && a.Name.Local == "xmlns":
			ok = true
		}
		if !ok {
			continue
		}
		if out == nil {
			out = make(map[string]string)
		}
		out[prefix] = a.Value
	}
	return out
}

// isSchemaElement reports whether the raw element name name, whose Space
// holds its prefix, is in the XML Schema namespace.
func (q *qnameScope) isSchemaElement(name xml.Name) bool {
	var ns string
	if name.Space == "" {
		ns = q.defaultNamespace()
	} else {
		var ok bool
		if ns, ok = q.prefixNamespace(name.Space); !ok {
			return false
		}
	}
	return ns == XSNamespace
}

// expandAttributes rewrites, in place, the value of each attribute of an
// XML Schema element that the schema for schemas (XML Schema Part 1,
// Appendix A) types as a QName, or for memberTypes as a list of QNames,
// into the expanded names it denotes. No XML Schema element carries an
// unprefixed attribute of one of these names under another type.
func (q *qnameScope) expandAttributes(attrs []xml.Attr) error {
	for i := range attrs {
		a := &attrs[i]
		if a.Name.Space != "" || a.Value == "" {
			continue
		}
		switch a.Name.Local {
		case "type", "ref", "base", "itemType", "substitutionGroup", "refer":
			expanded, err := q.expand(a.Value)
			if err != nil {
				return err
			}
			a.Value = expanded
		case "memberTypes":
			names := strings.Fields(a.Value)
			for j, name := range names {
				expanded, err := q.expand(name)
				if err != nil {
					return err
				}
				names[j] = expanded
			}
			a.Value = strings.Join(names, " ")
		}
	}
	return nil
}

// expand returns the expanded name {namespace}local the QName raw
// denotes in the current scope ([qnameScope]).
func (q *qnameScope) expand(raw string) (string, error) {
	ns := q.defaultNamespace()
	local := raw
	if prefix, rest, prefixed := strings.Cut(raw, ":"); prefixed {
		var ok bool
		if ns, ok = q.prefixNamespace(prefix); !ok {
			return "", fmt.Errorf("%w: unknown prefix %q in %q", ErrSchemaParse, prefix, raw)
		}
		local = rest
	}
	return "{" + ns + "}" + local, nil
}

// defaultNamespace returns the default namespace in scope, "" when none
// is.
func (q *qnameScope) defaultNamespace() string {
	for i := len(q.bindings) - 1; i >= 0; i-- {
		if ns, ok := q.bindings[i][""]; ok {
			return ns
		}
	}
	return ""
}

// prefixNamespace returns the namespace prefix is bound to in scope.
// Per Namespaces in XML 1.0, 3 the xml prefix is bound by definition.
// A prefix no element declares falls back to [wellKnownPrefixes].
func (q *qnameScope) prefixNamespace(prefix string) (string, bool) {
	for i := len(q.bindings) - 1; i >= 0; i-- {
		if ns, ok := q.bindings[i][prefix]; ok {
			return ns, true
		}
	}
	if prefix == "xml" {
		return xmlNamespace, true
	}
	ns, ok := wellKnownPrefixes[prefix]
	return ns, ok
}

// xmlNamespace is the namespace the xml prefix is bound to by definition
// (Namespaces in XML 1.0, 3).
const xmlNamespace = "http://www.w3.org/XML/1998/namespace"

// schemaDoc is what the builders read off the xs:schema element of the
// schema document a declaration sits in.
type schemaDoc struct {
	// tns is the targetNamespace attribute, empty when absent.
	tns string
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
// namespace, and [populateFromRaw] rejects one that carries form.
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
// of the schema for schemas prohibits form, and [populateFromRaw] rejects
// one that carries it.
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

// populateFromRaw builds the top-level components the schema document raw
// declares into s.
//
// # Corpus evidence
//
//   - All corpora: no bundled schema sets form on a top-level xs:element or
//     xs:attribute, so each of the 28 still loads to the same model, and no
//     verdict of the 159 files that embed XML moves, through the Factur-X
//     and ZUGFeRD validators or under any bundled schema.
func populateFromRaw(s *Schema, raw *rawSchema) error {
	doc := &schemaDoc{
		tns:                  raw.TargetNamespace,
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
		if raw.Elements[i].Form != "" {
			// Per XML Schema Part 1, 3.3.3 an xs:element meets the schema
			// for schemas, whose topLevelElement type (Appendix A)
			// prohibits form: a top-level declaration always takes the
			// target namespace.
			return fmt.Errorf("%w: top-level xs:element %q carries form", ErrSchemaParse, raw.Elements[i].Name)
		}
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
		if raw.Attributes[i].Form != "" {
			// Per XML Schema Part 1, 3.2.3 an xs:attribute meets the schema
			// for schemas, whose topLevelAttribute type (Appendix A)
			// prohibits form: a top-level declaration always takes the
			// target namespace.
			return fmt.Errorf("%w: top-level xs:attribute %q carries form", ErrSchemaParse, raw.Attributes[i].Name)
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
		ref, err := parseQName(raw.Type)
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
		baseRef, err := parseQName(ext.Base)
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
	baseRef, err := parseQName(base)
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
		base, err := parseQName(raw.Restriction.Base)
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
		ref, err := parseQName(name)
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
	ref, err := parseQName(raw.ItemType)
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
		ref, err := parseQName(r.Ref)
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
	ref, err := parseQName(a.Ref)
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
		ref, err := parseQName(a.Type)
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

// parseQName reads the expanded name {namespace}local that [qnameScope]
// wrote in place of a QName-valued attribute. A value in any other form
// sits on an element outside the XML Schema namespace, which the schema
// for schemas does not allow where the loader reads one.
func parseQName(raw string) (QName, error) {
	end := strings.LastIndexByte(raw, '}')
	if !strings.HasPrefix(raw, "{") || end < 0 {
		return QName{}, fmt.Errorf("%w: QName %q is not on an XML Schema element", ErrSchemaParse, raw)
	}
	return QName{Namespace: raw[1:end], Local: raw[end+1:]}, nil
}

// wellKnownPrefixes maps XSD prefix aliases to their namespace URIs.
// The CII building blocks use a stable set of prefixes across all
// shipped schema generations.
//
// Deliberate leniency: per XML Schema Part 1, 3.15.3 (QName
// Interpretation) a prefix a QName carries must be bound in scope, and
// [qnameScope] falls back to this table for a prefix no element binds,
// so a schema document that omits the conventional ram, qdt or udt
// declarations still resolves its CII references.
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
