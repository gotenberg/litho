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
	}

	if err := populateFromRaw(schema, raw, prefixes); err != nil {
		return nil, "", fmt.Errorf("at %s: %w", p, err)
	}
	return schema, schema.TargetNamespace, nil
}

// link walks every loaded schema and resolves every TypeRef into a
// Type pointer. References to built-in XS types resolve against the
// pre-populated registry; references to user types resolve against
// the owning schema or one of its imports.
func (l *loader) link() error {
	for _, schema := range l.schemas {
		if err := linkSchema(schema); err != nil {
			return err
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
	}
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
		return &BuiltinType{Name: qn}, nil
	}
	return nil, fmt.Errorf("%w: %s", ErrUnresolvedRef, qn)
}

// rawSchema is the encoding/xml decode target for an xs:schema
// document. It mirrors the XSD source structure so the loader can
// build the typed [Schema] in a second pass.
type rawSchema struct {
	XMLName         xml.Name           `xml:"http://www.w3.org/2001/XMLSchema schema"`
	TargetNamespace string             `xml:"targetNamespace,attr"`
	Elements        []rawElement       `xml:"element"`
	Attributes      []rawAttribute     `xml:"attribute"`
	ComplexTypes    []rawComplexType   `xml:"complexType"`
	SimpleTypes     []rawSimpleType    `xml:"simpleType"`
	Imports         []rawImportInclude `xml:"import"`
	Includes        []rawImportInclude `xml:"include"`
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
	ComplexType *rawComplexType `xml:"complexType"`
	SimpleType  *rawSimpleType  `xml:"simpleType"`
	Ref         string          `xml:"ref,attr"`
}

type rawComplexType struct {
	Name           string             `xml:"name,attr"`
	Sequence       *rawSequence       `xml:"sequence"`
	Choice         *rawChoice         `xml:"choice"`
	Attributes     []rawAttribute     `xml:"attribute"`
	ComplexContent *rawComplexContent `xml:"complexContent"`
	SimpleContent  *rawSimpleContent  `xml:"simpleContent"`
	AnyAttribute   *rawAnyAttribute   `xml:"anyAttribute"`
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
	Base         string           `xml:"base,attr"`
	SimpleType   *rawSimpleType   `xml:"simpleType"`
	Attributes   []rawAttribute   `xml:"attribute"`
	AnyAttribute *rawAnyAttribute `xml:"anyAttribute"`
	Facets       []rawFacet       `xml:",any"`
}

type rawComplexContent struct {
	Extension *rawExtension `xml:"extension"`
}

type rawExtension struct {
	Base         string           `xml:"base,attr"`
	Sequence     *rawSequence     `xml:"sequence"`
	Choice       *rawChoice       `xml:"choice"`
	Attributes   []rawAttribute   `xml:"attribute"`
	AnyAttribute *rawAnyAttribute `xml:"anyAttribute"`
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
	SimpleType *rawSimpleType `xml:"simpleType"`
}

type rawSimpleType struct {
	Name        string          `xml:"name,attr"`
	Restriction *rawRestriction `xml:"restriction"`
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

func populateFromRaw(s *Schema, raw *rawSchema, prefixes map[string]string) error {
	tns := raw.TargetNamespace
	for i := range raw.ComplexTypes {
		ct, err := buildComplexType(tns, prefixes, &raw.ComplexTypes[i])
		if err != nil {
			return err
		}
		s.Types[ct.QName()] = ct
	}
	for i := range raw.SimpleTypes {
		st, err := buildSimpleType(tns, prefixes, &raw.SimpleTypes[i])
		if err != nil {
			return err
		}
		s.Types[st.QName()] = st
	}
	for i := range raw.Elements {
		el, err := buildElement(tns, prefixes, &raw.Elements[i])
		if err != nil {
			return err
		}
		s.Elements[el.Name] = el
	}
	attrs, err := buildAttributes(tns, prefixes, raw.Attributes)
	if err != nil {
		return err
	}
	for i := range attrs {
		s.Attributes[attrs[i].Name] = &attrs[i]
	}
	return nil
}

func buildElement(tns string, prefixes map[string]string, raw *rawElement) (*Element, error) {
	if raw.Ref != "" {
		return nil, fmt.Errorf("%w: top-level xs:element with ref attribute (CII does not use this)",
			ErrUnsupportedConstruct)
	}
	el := &Element{
		Name:      QName{Namespace: tns, Local: raw.Name},
		MinOccurs: occursOrDefault(raw.MinOccurs, 1),
		MaxOccurs: occursOrDefault(raw.MaxOccurs, 1),
		Nillable:  isTrueBoolean(raw.Nillable),
		Default:   raw.Default,
		Fixed:     raw.Fixed,
	}
	switch {
	case raw.Type != "":
		ref, err := parseQName(raw.Type, tns, prefixes)
		if err != nil {
			return nil, err
		}
		el.TypeRef = ref
	case raw.ComplexType != nil:
		ct, err := buildComplexType(tns, prefixes, raw.ComplexType)
		if err != nil {
			return nil, err
		}
		el.Inline = ct
	case raw.SimpleType != nil:
		st, err := buildSimpleType(tns, prefixes, raw.SimpleType)
		if err != nil {
			return nil, err
		}
		el.Inline = st
	}
	return el, nil
}

func buildComplexType(tns string, prefixes map[string]string, raw *rawComplexType) (*ComplexType, error) {
	ct := &ComplexType{
		Name: QName{Namespace: tns, Local: raw.Name},
	}
	switch {
	case raw.Sequence != nil:
		seq, err := buildSequence(tns, prefixes, raw.Sequence)
		if err != nil {
			return nil, err
		}
		ct.Content = seq
	case raw.Choice != nil:
		ch, err := buildChoice(tns, prefixes, raw.Choice)
		if err != nil {
			return nil, err
		}
		ct.Content = ch
	case raw.ComplexContent != nil && raw.ComplexContent.Extension != nil:
		ext := raw.ComplexContent.Extension
		baseRef, err := parseQName(ext.Base, tns, prefixes)
		if err != nil {
			return nil, err
		}
		ct.BaseRef = baseRef
		switch {
		case ext.Sequence != nil:
			seq, err := buildSequence(tns, prefixes, ext.Sequence)
			if err != nil {
				return nil, err
			}
			ct.Content = seq
		case ext.Choice != nil:
			ch, err := buildChoice(tns, prefixes, ext.Choice)
			if err != nil {
				return nil, err
			}
			ct.Content = ch
		default:
			ct.Content = Empty{}
		}
		attrs, err := buildAttributes(tns, prefixes, ext.Attributes)
		if err != nil {
			return nil, err
		}
		ct.Attributes = append(ct.Attributes, attrs...)
		ct.AnyAttribute = buildAnyAttribute(tns, ext.AnyAttribute)
	case raw.SimpleContent != nil:
		if err := buildSimpleContent(tns, prefixes, raw.SimpleContent, ct); err != nil {
			return nil, err
		}
	default:
		ct.Content = Empty{}
	}
	attrs, err := buildAttributes(tns, prefixes, raw.Attributes)
	if err != nil {
		return nil, err
	}
	ct.Attributes = append(ct.Attributes, attrs...)
	if raw.AnyAttribute != nil {
		ct.AnyAttribute = buildAnyAttribute(tns, raw.AnyAttribute)
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
func buildSimpleContent(tns string, prefixes map[string]string, raw *rawSimpleContent, ct *ComplexType) error {
	var base string
	var rawAttrs []rawAttribute
	switch {
	case raw.Extension != nil:
		base, rawAttrs = raw.Extension.Base, raw.Extension.Attributes
		ct.AnyAttribute = buildAnyAttribute(tns, raw.Extension.AnyAttribute)
		ct.Content = SimpleContent{}
	case raw.Restriction != nil:
		r := raw.Restriction
		base, rawAttrs = r.Base, r.Attributes
		ct.AnyAttribute = buildAnyAttribute(tns, r.AnyAttribute)
		content := SimpleContent{Restriction: true, Facets: collectFacets(r.Facets)}
		if r.SimpleType != nil {
			inline, err := buildSimpleType(tns, prefixes, r.SimpleType)
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
	baseRef, err := parseQName(base, tns, prefixes)
	if err != nil {
		return err
	}
	ct.BaseRef = baseRef
	attrs, err := buildAttributes(tns, prefixes, rawAttrs)
	if err != nil {
		return err
	}
	ct.Attributes = append(ct.Attributes, attrs...)
	return nil
}

func buildSimpleType(tns string, prefixes map[string]string, raw *rawSimpleType) (*SimpleType, error) {
	st := &SimpleType{
		Name: QName{Namespace: tns, Local: raw.Name},
	}
	if raw.Restriction == nil {
		return st, nil
	}
	if raw.Restriction.Base != "" {
		base, err := parseQName(raw.Restriction.Base, tns, prefixes)
		if err != nil {
			return nil, err
		}
		st.BaseRef = base
	}
	st.Facets = collectFacets(raw.Restriction.Facets)
	return st, nil
}

func buildSequence(tns string, prefixes map[string]string, raw *rawSequence) (Sequence, error) {
	particles, err := buildParticles(tns, prefixes, raw.Elements, raw.Wildcards, raw.Choices)
	if err != nil {
		return Sequence{}, err
	}
	return Sequence{Particles: particles}, nil
}

func buildChoice(tns string, prefixes map[string]string, raw *rawChoice) (Choice, error) {
	particles, err := buildParticles(tns, prefixes, raw.Elements, raw.Wildcards, nil)
	if err != nil {
		return Choice{}, err
	}
	return Choice{Particles: particles}, nil
}

func buildParticles(tns string, prefixes map[string]string, els []rawElement, wcs []rawWildcard, nested []rawChoice) ([]Particle, error) {
	out := make([]Particle, 0, len(els)+len(wcs))
	for i := range els {
		el, err := buildElement(tns, prefixes, &els[i])
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

func buildAttributes(tns string, prefixes map[string]string, raws []rawAttribute) ([]AttributeUse, error) {
	out := make([]AttributeUse, 0, len(raws))
	for i := range raws {
		a := raws[i]
		au := AttributeUse{
			Name:       QName{Namespace: tns, Local: a.Name},
			Default:    a.Default,
			Fixed:      a.Fixed,
			Required:   a.Use == "required",
			Prohibited: a.Use == "prohibited",
		}
		if a.Type != "" {
			ref, err := parseQName(a.Type, tns, prefixes)
			if err != nil {
				return nil, err
			}
			au.TypeRef = ref
		} else if a.SimpleType != nil {
			st, err := buildSimpleType(tns, prefixes, a.SimpleType)
			if err != nil {
				return nil, err
			}
			au.Type = st
		}
		out = append(out, au)
	}
	return out, nil
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
func parseQName(raw string, tns string, prefixes map[string]string) (QName, error) {
	for i := 0; i < len(raw); i++ {
		if raw[i] == ':' {
			prefix := raw[:i]
			local := raw[i+1:]
			ns, ok := prefixes[prefix]
			if !ok {
				return QName{}, fmt.Errorf("%w: unknown prefix %q in %q", ErrSchemaParse, prefix, raw)
			}
			return QName{Namespace: ns, Local: local}, nil
		}
	}
	return QName{Namespace: tns, Local: raw}, nil
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
