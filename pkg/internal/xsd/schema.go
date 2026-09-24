package xsd

// XSNamespace is the canonical XML Schema namespace URI.
const XSNamespace = "http://www.w3.org/2001/XMLSchema"

// MaxUnbounded is the sentinel [Element.MaxOccurs] value for an
// unbounded upper bound. Chosen rather than math.MaxInt so the model
// JSON-marshals cleanly for diagnostic dumps and so loops that
// terminate on count >= MaxUnbounded keep predictable bounds.
const MaxUnbounded = -1

// QName is an XML qualified name: a namespace URI plus a local part.
// Two QName values are equal when both fields match exactly. The zero
// value is the empty-namespace empty-local name used to flag
// "anonymous" or "no name" inside the loader; do not rely on it as a
// valid lookup key.
type QName struct {
	Namespace string
	Local     string
}

// String returns the conventional "{namespace}local" notation, useful
// for diagnostics. Tests should not lean on this format for assertions
// because it may evolve.
func (q QName) String() string {
	if q.Namespace == "" {
		return q.Local
	}
	return "{" + q.Namespace + "}" + q.Local
}

// IsZero reports whether q is the zero-value QName.
func (q QName) IsZero() bool { return q.Namespace == "" && q.Local == "" }

// Schema is the loaded representation of one xs:schema document plus
// every schema reachable through xs:import or xs:include during the
// load. Values must be treated as immutable after [Load] returns.
type Schema struct {
	// TargetNamespace is the namespace URI declared on the root
	// xs:schema element. Empty when the schema omits the attribute.
	TargetNamespace string
	// Elements indexes top-level xs:element declarations by their
	// fully qualified name.
	Elements map[QName]*Element
	// Types indexes top-level xs:complexType and xs:simpleType
	// declarations by their fully qualified name. Built-in types from
	// [builtinNames] are pre-populated so references to xs:string and
	// the like resolve without a second lookup table.
	Types map[QName]Type
	// Imports indexes loaded imported schemas by their target
	// namespace. The current schema is not present in this map; it
	// reaches its own elements/types via Elements and Types directly.
	Imports map[string]*Schema
}

// Type is the closed sum of XSD type kinds the loader recognises.
// SimpleType, ComplexType, and BuiltinType implement it. The interface
// is intentionally minimal so callers must type-switch to consume the
// concrete fields; this avoids accreting accessor noise on the model
// while the engine is small.
type Type interface {
	// QName returns the fully qualified name of the type. Anonymous
	// inline types return the zero-value QName.
	QName() QName

	isType()
}

// maxTypeDerivationDepth bounds every walk up a type's base chain: the
// simpleType restriction chain in facets.go and the complexType
// extension chains in validate.go. It is an anti-DoS guard, not a limit
// XSD imposes.
//
// Nothing rejects a schema whose base chain returns to itself. The
// loader resolves each base through [Schema.Types] and checks only that
// the name resolves, so a simpleType restricting itself (directly, or
// through a second type) links without complaint. Unbounded, the facet
// walk recursed until the goroutine stack passed its 1 GB limit, which
// Go reports as a fatal error and no recover can catch.
//
// 64 sits far above real schemas: the bundled CII schemas nest at most
// a few restrictions deep, and a chain longer than this describes no
// document a validator could usefully judge.
const maxTypeDerivationDepth = 64

// BuiltinType is the marker representation of an XS-namespace
// primitive (xs:string, xs:int, xs:date, ...). The loader populates a
// stub instance for each known built-in in [builtinNames]. The type
// carries no behaviour of its own: [validateLexical] dispatches on its
// name, and [ValidateValue] applies the facets a restriction adds.
type BuiltinType struct {
	Name QName
}

// QName implements [Type].
func (t *BuiltinType) QName() QName { return t.Name }
func (t *BuiltinType) isType()      {}

// SimpleType models an xs:simpleType declaration. Only restriction-
// based derivations are supported; xs:union and xs:list are out of
// scope per the CII subset.
type SimpleType struct {
	// Name is the qualified name of the type. Zero-value when the
	// type is anonymous (inlined on an element or attribute).
	Name QName
	// Base is the type this simpleType restricts. Resolved during
	// the link pass; nil before then on a freshly loaded model.
	Base Type
	// BaseRef holds the unresolved QName parsed off the xs:restriction
	// base attribute. The loader uses it to drive the link pass and
	// callers can fall back on it when [Base] is nil (e.g. when the
	// base is itself anonymous, which the CII subset does not exercise).
	BaseRef QName
	// Facets carry the restriction constraints (pattern, enumeration,
	// length variants, value range, fractionDigits, totalDigits,
	// whiteSpace). The order matches the XSD source so multi-value
	// constraints like enumeration sets remain ordered.
	Facets []Facet
}

// QName implements [Type].
func (t *SimpleType) QName() QName { return t.Name }
func (t *SimpleType) isType()      {}

// ComplexType models an xs:complexType declaration. Content is the
// content model; attributes are the type's attribute uses.
//
// A type with xs:complexContent derives by xs:extension only; complex
// content derivation by restriction is out of scope per the CII subset. A
// type with xs:simpleContent derives by extension or by restriction, and
// its Content is then a [SimpleContent].
type ComplexType struct {
	// Name is the qualified name of the type. Zero-value when the
	// type is anonymous (inlined on an element).
	Name QName
	// Content is the content model: a Sequence, Choice, Empty or
	// SimpleContent. Mixed content is not yet modelled because the CII
	// schemas do not use it on validated invoice elements.
	Content ContentModel
	// Attributes is the list of attribute uses the type declares
	// itself. Those it inherits stay on [ComplexType.Base].
	Attributes []AttributeUse
	// Base is the type this complexType derives from, per XML Schema
	// Part 1, 3.4.2 its {base type definition}: the base of the
	// xs:complexContent extension, or of the xs:simpleContent extension
	// or restriction. Nil when the type has no base. Resolved during the
	// link pass.
	Base Type
	// BaseRef holds the unresolved QName parsed off the base attribute.
	// Zero-value when the type does not derive.
	BaseRef QName
}

// QName implements [Type].
func (t *ComplexType) QName() QName { return t.Name }
func (t *ComplexType) isType()      {}

// Element models a top-level or inline xs:element declaration. Inline
// types are carried in [Inline]; top-level type references are carried
// in [TypeRef] and resolved into [Type] during the link pass.
type Element struct {
	Name QName
	// Type is the resolved element type. Set during the link pass for
	// elements that point at a top-level type via [TypeRef], or copied
	// from [Inline] when the type is anonymous.
	Type Type
	// TypeRef is the unresolved reference parsed off the type
	// attribute. Zero-value when the element carries an inline type.
	TypeRef QName
	// Inline is the anonymous type when the xs:element nests an
	// xs:complexType or xs:simpleType child. Nil when the element
	// uses [TypeRef] instead.
	Inline Type
	// MinOccurs defaults to 1 when absent on the XSD source.
	MinOccurs int
	// MaxOccurs is 1 by default, [MaxUnbounded] when xs:maxOccurs="unbounded".
	MaxOccurs int
	// Nillable reports nillable="true". Per XML Schema Part 1, 3.3.4 only
	// a nillable element may carry xsi:nil, and one whose xsi:nil is true
	// has empty content that is not validated against its type.
	Nillable bool
	// Default and Fixed carry the element's value constraint, empty when
	// it has none. Per XML Schema Part 1, 3.3.4 (cvc-elt clause 5.1) an
	// element with no content takes it as its value. A fixed element's
	// content is not compared with Fixed.
	Default string
	Fixed   string
}

// AttributeUse models an xs:attribute occurrence on a complex type.
// Top-level xs:attribute declarations are not yet modelled because
// CII uses inline attribute uses exclusively.
type AttributeUse struct {
	Name     QName
	Type     Type
	TypeRef  QName
	Required bool
	// Prohibited reports use="prohibited". Per XML Schema Part 1, 3.2.2
	// such an xs:attribute is no attribute use at all: in a derivation by
	// restriction it removes the base type's use of the same name
	// (3.4.2), and elsewhere it has no effect.
	Prohibited bool
	Default    string
	Fixed      string
}

// ContentModel is the closed sum of content models. [Sequence],
// [Choice], [Empty] and [SimpleContent] implement it.
type ContentModel interface{ isContent() }

// Sequence models xs:sequence: an ordered list of particles each of
// which is an element or wildcard with its own occurrence bounds.
type Sequence struct {
	Particles []Particle
}

func (Sequence) isContent() {}

// Choice models xs:choice: alternative particles where exactly one
// matches per occurrence. The CII schemas use choice sparingly,
// mostly inside extended trade transaction sections.
type Choice struct {
	Particles []Particle
}

func (Choice) isContent() {}

// Empty is the content model of a complex type that declares no
// element children (attributes-only types).
type Empty struct{}

func (Empty) isContent() {}

// SimpleContent is the content model of a complex type declared with
// xs:simpleContent: the element holds character data and no child
// element. Per XML Schema Part 1, 3.4.2 the type's {content type} is then
// a simple type definition, built from [ComplexType.Base]:
//
//   - By extension, it is the base's own: the base simple type, or the
//     content type of a base complex type with simple content. The
//     extension only adds attribute uses.
//   - By restriction, it restricts the base's content type, or Inline when
//     the xs:restriction carries an xs:simpleType, with Facets.
//
// [ValidateValue] validates a value against the content type when handed
// the complex type itself.
type SimpleContent struct {
	// Restriction reports derivation by xs:restriction. False means
	// derivation by xs:extension.
	Restriction bool
	// Facets are the facets the xs:restriction declares, in source
	// order. They form one derivation step. Empty for an extension.
	Facets []Facet
	// Inline is the xs:simpleType child of the xs:restriction, nil when
	// it has none.
	Inline *SimpleType
}

func (SimpleContent) isContent() {}

// Particle is one occurrence-slot inside a content model. Exactly
// one of [Element] or [Wildcard] is populated.
type Particle struct {
	Element  *Element
	Wildcard *Wildcard
}

// Wildcard models xs:any. The loader treats every wildcard as
// process-contents="skip" during validation because CII embeds them
// almost exclusively as escape hatches for extensions; capturing the
// processContents attribute keeps the door open for stricter handling
// later without re-parsing.
type Wildcard struct {
	Namespace       string
	ProcessContents string
	MinOccurs       int
	MaxOccurs       int
}

// Facet is the closed sum of xs:restriction facet kinds the loader
// recognises. [ValidateValue] applies each kind to a value.
type Facet interface{ isFacet() }

// Pattern matches xs:pattern.
type Pattern struct{ Value string }

// Enumeration matches one xs:enumeration entry. SimpleType.Facets
// carries the full enumeration set as multiple Enumeration entries
// in source order so callers can render allow-lists in error context.
type Enumeration struct{ Value string }

// The length facets match xs:length, xs:minLength and xs:maxLength.
type (
	// Length matches xs:length.
	Length struct{ Value int }
	// MinLength matches xs:minLength.
	MinLength struct{ Value int }
	// MaxLength matches xs:maxLength.
	MaxLength struct{ Value int }
)

// The range facets match the xs:*Inclusive / xs:*Exclusive numeric and
// date range facets. Each value is kept as the raw lexical form parsed
// off the XSD, and [ValidateValue] compares the value and the bound in
// the value space of the built-in the simpleType's restriction chain
// bottoms out on, so a date range compares as dates (XML Schema Part 2,
// 4.3.7 to 4.3.10).
type (
	// MinInclusive matches xs:minInclusive.
	MinInclusive struct{ Value string }
	// MaxInclusive matches xs:maxInclusive.
	MaxInclusive struct{ Value string }
	// MinExclusive matches xs:minExclusive.
	MinExclusive struct{ Value string }
	// MaxExclusive matches xs:maxExclusive.
	MaxExclusive struct{ Value string }
)

// FractionDigits matches xs:fractionDigits.
type FractionDigits struct{ Value int }

// TotalDigits matches xs:totalDigits.
type TotalDigits struct{ Value int }

// WhiteSpace matches xs:whiteSpace. Allowed values are
// "preserve", "replace", and "collapse".
type WhiteSpace struct{ Value string }

func (Pattern) isFacet()        {}
func (Enumeration) isFacet()    {}
func (Length) isFacet()         {}
func (MinLength) isFacet()      {}
func (MaxLength) isFacet()      {}
func (MinInclusive) isFacet()   {}
func (MaxInclusive) isFacet()   {}
func (MinExclusive) isFacet()   {}
func (MaxExclusive) isFacet()   {}
func (FractionDigits) isFacet() {}
func (TotalDigits) isFacet()    {}
func (WhiteSpace) isFacet()     {}
