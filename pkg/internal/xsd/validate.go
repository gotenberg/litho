package xsd

import (
	"cmp"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
)

// Violation captures a single XSD validation failure found while
// walking an XML document. The Rule field discriminates failure
// categories so callers can group or filter; Path carries the
// dot-separated element path to the offending location for
// diagnostics. Reason is a short human-readable explanation.
//
//nolint:errname // mirrors the compliance.Violation precedent; "ViolationError" would read redundantly.
type Violation struct {
	Rule   string
	Path   string
	Reason string
}

// Error implements the error interface so a Violation can be
// returned from the streaming validator alongside any underlying
// parse error.
func (v Violation) Error() string {
	if v.Path != "" {
		return fmt.Sprintf("xsd %s at %s: %s", v.Rule, v.Path, v.Reason)
	}
	return fmt.Sprintf("xsd %s: %s", v.Rule, v.Reason)
}

// Documented Rule values returned by [Validate].
const (
	RuleRoot        = "root"
	RuleStructure   = "structure"
	RuleCardinality = "cardinality"
	RuleLexical     = "lexical"
	RuleFacet       = "facet"
	RuleAttribute   = "attribute"
	// RuleFixed reports an element whose content differs from the
	// fixed value its declaration imposes (XML Schema Part 1, 3.3.4,
	// cvc-elt clause 5.2.2).
	RuleFixed = "fixed"
)

// Validate streams payload through the XSD engine and returns every
// failure as a [Violation]. An empty slice means the document
// satisfies schema's content model, cardinality, and leaf-type
// constraints. An underlying parse error (malformed XML, an encoding
// [NewDecoder] cannot read, which wraps [ErrEncoding]) is returned as the
// second value distinct from violations so callers can tell
// "ran-but-failed-rules" apart from "could-not-run". The violations found
// before it are returned alongside.
//
// The walk checks element ordering, cardinality, leaf values,
// attributes, xsi:nil, xs:choice content, and unknown elements. An xs:any
// wildcard accepts its child and skips the subtree. An attribute no use
// declares must be admitted by the type's xs:anyAttribute.
//
// The walker honours ctx cancellation at element boundaries: a
// canceled context terminates the walk and surfaces ctx.Err() as
// the parse error.
func Validate(ctx context.Context, schema *Schema, payload []byte) ([]Violation, error) {
	if schema == nil {
		return nil, errors.New("xsd: schema is nil")
	}
	v := &validator{
		schema:  schema,
		decoder: NewDecoder(payload),
	}
	if err := v.run(ctx); err != nil {
		return v.violations, err
	}
	return v.violations, nil
}

// validator carries the streaming validation state across calls into
// the token handler. Stack[0] is always the document-element matcher
// once the first StartElement has been read; the slice grows by one
// per nested complex element.
type validator struct {
	schema     *Schema
	decoder    *xml.Decoder
	stack      []*frame
	violations []Violation
}

// frame is one entry on the matcher stack: the element currently
// being validated plus the matcher state for its content model.
type frame struct {
	// element is the *Element from the schema that matched the
	// current open tag. The complex type's Content lives at
	// element.Type and drives the matcher state.
	element *Element
	// name is the element name including namespace for path
	// rendering when leaf rules fire.
	name xml.Name
	// matcher tracks the position inside the parent type's content
	// model. Nil when the element is a leaf: its type is a simple type,
	// or a complex type with simple content.
	matcher contentMatcher
	// pendingText accumulates CharData while waiting for the
	// matching EndElement; only used on a leaf so its value can be
	// validated on close.
	pendingText strings.Builder
	// nilled reports an xsi:nil of true on a nillable element. The frame
	// is then a leaf whatever its type, since it must stay empty.
	nilled bool
}

// contentMatcher abstracts the per-content-model stepping logic so
// the validator stays agnostic of whether the parent type uses
// xs:sequence or xs:choice. Each call to step records one child
// element and returns the matching particle when one applies, plus
// any structural violation that fires (typically "element not
// expected here" for a non-matching name).
type contentMatcher interface {
	// step proposes a child element. Returns the matching *Element
	// from the schema when accepted, or a structural Violation when
	// rejected.
	step(name xml.Name) (*Element, *Violation)
	// finalize reports cardinality violations for unsatisfied
	// minOccurs requirements when the parent element closes.
	finalize(path string) []Violation
}

func (v *validator) run(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		tok, err := v.decoder.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				if len(v.stack) > 0 {
					v.violations = append(v.violations, Violation{
						Rule:   RuleStructure,
						Path:   v.path(),
						Reason: "unexpected EOF before closing tag",
					})
				}
				return nil
			}
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			v.onStart(t)
		case xml.EndElement:
			v.onEnd(t)
		case xml.CharData:
			v.onCharData(t)
		}
	}
}

func (v *validator) onStart(start xml.StartElement) {
	if len(v.stack) == 0 {
		v.openRoot(start)
		return
	}
	parent := v.stack[len(v.stack)-1]
	if parent.matcher == nil {
		// Two cases with no matcher: a simple-type leaf (children
		// are illegal, surface a structure violation) or a
		// placeholder absorbing a wildcard-matched or rejected
		// subtree (children silently propagate as more placeholders
		// so cascading false violations do not pile up).
		if parent.element != nil {
			reason := fmt.Sprintf("element %s appears inside simple-type content", nameOf(start.Name))
			if parent.nilled {
				reason = fmt.Sprintf("element %s appears inside an element whose xsi:nil is true", nameOf(start.Name))
			}
			v.violations = append(v.violations, Violation{
				Rule:   RuleStructure,
				Path:   v.path() + "/" + nameOf(start.Name),
				Reason: reason,
			})
		}
		v.pushPlaceholder(start)
		return
	}
	el, vio := parent.matcher.step(start.Name)
	if vio != nil {
		vio.Path = v.path() + "/" + nameOf(start.Name)
		v.violations = append(v.violations, *vio)
		v.pushPlaceholder(start)
		return
	}
	if el == nil {
		// Wildcard match: accepted, but no schema element to drive
		// validation on the subtree. Treat the child as opaque.
		v.pushPlaceholder(start)
		return
	}
	v.pushFrame(start, el)
}

// The element is unused: the validator pops its own stack. The parameter
// stays to mirror onStart, which the SAX-style dispatch calls the same way.
func (v *validator) onEnd(_ xml.EndElement) {
	if len(v.stack) == 0 {
		return
	}
	top := v.stack[len(v.stack)-1]
	if top.matcher != nil {
		v.violations = append(v.violations, top.matcher.finalize(v.path())...)
	} else if top.element != nil {
		v.violations = append(v.violations, leafViolations(top, v.path())...)
	}
	v.stack = v.stack[:len(v.stack)-1]
}

// leafViolations validates the text of a leaf frame against the lexical
// space and facets of its type.
//
// The text is handed over as the document holds it: [ValidateValue]
// normalizes it under the type's own whiteSpace (XML Schema Part 2, 4.3.6),
// so an xs:string keeps its spaces and an xs:token loses them. Empty text
// is validated too, which an xs:string or xs:token admits and an xs:decimal
// does not. Two cases change the value, both from XML Schema Part 1, 3.3.4
// (cvc-elt): an element with no content takes the default or fixed value of
// its declaration (clause 5.1), and an element whose xsi:nil is true must
// have no character content (clause 3.2.1) and is not validated against its
// type (cvc-type clause 3.1.3).
//
// An element with content and a fixed value constraint must hold that value
// (clause 5.2.2.2.2): its actual value must equal the fixed one, compared as
// [fixedValueMatches] compares them. A mismatch is reported under
// [RuleFixed] alone, as the fixed value is itself valid for the type (3.3.6,
// e-props-correct clause 2).
//
// # Corpus evidence
//
//   - All corpora: validating empty text changes no XSD violation on the 159
//     files that embed XML. Through the Factur-X and ZUGFeRD validators they
//     hold 53 empty xs:token values in 23 files, 64 xs:string in 21 and 3
//     xs:base64Binary in 3, types that all admit the empty string, and no
//     empty xs:decimal.
//   - All corpora: keeping the white space of an xs:string changes none
//     either. 244 xs:string values in 83 files are padded and 88 more hold a
//     line break or a run of spaces, but no string type of the bundled
//     schemas declares a facet.
//   - All corpora: none of those files carries xsi:nil.
//   - All corpora: no schema those files validate against gives an element
//     a fixed value, so the clause 5.2.2 check moves no verdict.
func leafViolations(top *frame, path string) []Violation {
	text := top.pendingText.String()
	if top.nilled {
		if text == "" {
			return nil
		}
		return []Violation{{
			Rule:   RuleStructure,
			Path:   path,
			Reason: "element whose xsi:nil is true has character content",
		}}
	}
	if text == "" {
		text = cmp.Or(top.element.Fixed, top.element.Default)
	} else if top.element.Fixed != "" && !fixedValueMatches(text, top.element.Fixed, top.element.Type) {
		return []Violation{{
			Rule:   RuleFixed,
			Path:   path,
			Reason: fmt.Sprintf("value %q does not match the fixed value %q", text, top.element.Fixed),
		}}
	}
	var out []Violation
	for _, fv := range ValidateValue(text, top.element.Type) {
		rule := RuleFacet
		if fv.Facet == nil {
			rule = RuleLexical
		}
		out = append(out, Violation{
			Rule:   rule,
			Path:   path,
			Reason: fv.Reason,
		})
	}
	return out
}

func (v *validator) onCharData(t xml.CharData) {
	if len(v.stack) == 0 {
		return
	}
	top := v.stack[len(v.stack)-1]
	if top.element == nil || top.matcher != nil {
		return
	}
	top.pendingText.Write(t)
}

// openRoot consumes the document element and pushes the first frame.
// The element must be declared at the top level of the schema
// ([validator.findRootElement]) for validation to proceed.
func (v *validator) openRoot(start xml.StartElement) {
	qn := QName{Namespace: start.Name.Space, Local: start.Name.Local}
	el, ok := v.findRootElement(qn)
	if !ok {
		v.violations = append(v.violations, Violation{
			Rule:   RuleRoot,
			Path:   nameOf(start.Name),
			Reason: fmt.Sprintf("element %s is not declared as a top-level element in the schema", qn),
		})
		v.pushPlaceholder(start)
		return
	}
	v.pushFrame(start, el)
}

// findRootElement returns the element declaration the document element
// named qn validates against. Per XML Schema Part 1, 3.3.4 (cvc-assess-elt
// clause 1.1.1.3.2) and 5.2 the validation root takes the declaration its
// name resolves to by QName resolution (Instance), 3.15.4: an entry of the
// {element declarations} of the schema, which holds those of every schema
// document the validated one includes or imports, however deep
// ([loader.elementDeclarations]).
func (v *validator) findRootElement(qn QName) (*Element, bool) {
	el, ok := v.schema.globalElements[qn]
	return el, ok
}

// pushFrame opens a frame for an accepted element. The matcher is
// installed when the type carries an element content model; simple
// types, built-ins and complex types with simple content leave matcher
// nil so CharData is collected as leaf text instead. Attribute
// validation runs at frame open since every attribute is bound to its
// element by the time the StartElement event arrives.
func (v *validator) pushFrame(start xml.StartElement, el *Element) {
	path := v.pathWith(start.Name)
	nilled, vio := nilState(start, el, path)
	if vio != nil {
		v.violations = append(v.violations, *vio)
	}
	f := &frame{element: el, name: start.Name, nilled: nilled}
	if ct, ok := el.Type.(*ComplexType); ok {
		// Per XML Schema Part 1, 3.4.4 (cvc-complex-type clause 2) the
		// content model is not checked on a nilled element, only its
		// attributes are.
		if _, simple := ct.Content.(SimpleContent); !simple && !nilled {
			f.matcher = newMatcher(ct, v.schema)
		}
		v.violations = append(v.violations, v.validateAttributes(start, ct, path)...)
	} else if !isAnyType(el.Type) {
		v.violations = append(v.violations, simpleTypeAttributes(start, path)...)
	}
	v.stack = append(v.stack, f)
}

// xsiNamespace is the XML Schema instance namespace, which xsi:nil lives
// in (XML Schema Part 1, 2.6).
const xsiNamespace = "http://www.w3.org/2001/XMLSchema-instance"

// nilState reads the xsi:nil attribute of start, per XML Schema Part 1,
// 3.3.4 (cvc-elt clause 3): it may appear on a nillable element only, and
// its value is an xs:boolean (2.6.2). It reports whether the element is
// nilled, and the violation the attribute raises, if any.
func nilState(start xml.StartElement, el *Element, path string) (bool, *Violation) {
	for _, a := range start.Attr {
		if a.Name.Space != xsiNamespace || a.Name.Local != "nil" {
			continue
		}
		if !el.Nillable {
			return false, &Violation{
				Rule:   RuleAttribute,
				Path:   path + "/@xsi:nil",
				Reason: "xsi:nil on an element that is not nillable",
			}
		}
		if fvs := ValidateValue(a.Value, &BuiltinType{Name: QName{Namespace: XSNamespace, Local: "boolean"}}); len(fvs) > 0 {
			return false, &Violation{Rule: RuleAttribute, Path: path + "/@xsi:nil", Reason: fvs[0].Reason}
		}
		return isTrueBoolean(a.Value), nil
	}
	return false, nil
}

// pathWith renders the path that the new frame will own once pushed.
// Used by attribute validation so violation paths point at the
// owning element, not at its parent.
func (v *validator) pathWith(name xml.Name) string {
	if len(v.stack) == 0 {
		return "/" + name.Local
	}
	return v.path() + "/" + name.Local
}

// pushPlaceholder opens a frame without an attached schema element.
// Used to keep the depth counter in sync when an element is rejected
// so subsequent EndElement events do not under-pop the stack and
// trigger cascading false violations.
func (v *validator) pushPlaceholder(start xml.StartElement) {
	v.stack = append(v.stack, &frame{name: start.Name})
}

// path renders the current stack as a "/foo/bar/baz" string. Used to
// annotate Violation.Path so callers can locate the offending
// element in their source document. Namespace prefixes are dropped
// for readability; local names are unambiguous in CII documents.
func (v *validator) path() string {
	if len(v.stack) == 0 {
		return ""
	}
	var b strings.Builder
	for i := range v.stack {
		b.WriteByte('/')
		b.WriteString(v.stack[i].name.Local)
	}
	return b.String()
}

func nameOf(n xml.Name) string {
	if n.Space != "" {
		return "{" + n.Space + "}" + n.Local
	}
	return n.Local
}

// newMatcher constructs the matcher for a ComplexType's content
// model. Walks xs:extension chains so inherited particles from the
// base type sit ahead of the derived type's own particles in the
// matcher's view, matching XSD's "base content first, then extension"
// semantics for derivation by extension.
//
// xs:choice content models route through [choiceMatcher], which
// enforces "exactly one alternative" semantics rather than the
// per-particle minOccurs that [sequenceMatcher] uses for xs:sequence.
// CII leans on choice for DateTimeType and a handful of other
// switchable scalar containers, so accurate choice handling matters.
func newMatcher(ct *ComplexType, _ *Schema) contentMatcher {
	if _, isChoice := ct.Content.(Choice); isChoice {
		particles := collectExtendedParticles(ct)
		if len(particles) == 0 {
			return &emptyMatcher{}
		}
		return &choiceMatcher{particles: particles}
	}
	particles := collectExtendedParticles(ct)
	if len(particles) == 0 {
		return &emptyMatcher{}
	}
	return &sequenceMatcher{seq: Sequence{Particles: particles}, particles: particles}
}

// derivationChain returns ct and the complex types it derives from, the
// most basic first: the base chain up to the first base that is not a
// complex type.
//
// The loader rejects xs:include cycles, but it does NOT reject a type
// whose base chain returns to itself, so the bound is load bearing
// rather than merely defensive: see [maxTypeDerivationDepth].
func derivationChain(ct *ComplexType) []*ComplexType {
	var chain []*ComplexType
	cur := ct
	for depth := 0; cur != nil && depth < maxTypeDerivationDepth; depth++ {
		chain = append([]*ComplexType{cur}, chain...)
		if base, ok := cur.Base.(*ComplexType); ok {
			cur = base
			continue
		}
		break
	}
	return chain
}

// collectExtendedParticles flattens the xs:extension chain rooted at
// ct into a single particle list. Base particles appear before
// derived particles, matching the way an extending complex type
// appends to the base's content model.
func collectExtendedParticles(ct *ComplexType) []Particle {
	chain := derivationChain(ct)
	var out []Particle
	for _, link := range chain {
		switch m := link.Content.(type) {
		case Sequence:
			out = append(out, m.Particles...)
		case Choice:
			out = append(out, m.Particles...)
		}
	}
	return out
}

// collectExtendedAttributes flattens the derivation chain into a
// single attribute-use list, base-first then derived. Used by
// attribute validation so a derived complex type honours every
// attribute declared on its base alongside the local additions.
//
// Per XML Schema Part 1, 3.4.2 a derivation by restriction keeps the
// base's attribute uses except those it redeclares or prohibits, so a
// declaration replaces an earlier one of the same name, and a prohibited
// one removes it. An extension may not redeclare a base attribute (3.4.6,
// ct-props-correct.4), so the rule changes nothing there on a valid
// schema. Each link contributes the uses it declares and those of the
// attribute groups it references ([ownAttributeUses]).
func collectExtendedAttributes(ct *ComplexType) []AttributeUse {
	chain := derivationChain(ct)
	var out []AttributeUse
	for _, link := range chain {
		for _, use := range ownAttributeUses(link) {
			out = slices.DeleteFunc(out, func(u AttributeUse) bool { return u.Name == use.Name })
			if !use.Prohibited {
				out = append(out, use)
			}
		}
	}
	return out
}

// validateAttributes walks the declared attribute uses on ct and
// matches each one against the StartElement's attribute list, then
// judges every attribute no use declares against the type's attribute
// wildcard ([undeclaredAttribute]). The path argument names the owning
// element so violations point at the right place when several elements
// have attribute issues.
//
// Four failure modes surface as RuleAttribute:
//   - required attribute absent
//   - attribute value does not satisfy its declared type
//   - attribute has a fixed value and the document supplied a
//     different one
//   - attribute no use declares and no wildcard admits
//
// # Corpus evidence
//
//   - All corpora: none of the 159 files that embed XML carries an
//     attribute no use declares, through the Factur-X and ZUGFeRD
//     validators or under any bundled schema, and no bundled schema
//     declares an xs:anyAttribute. Counting every attribute as undeclared
//     instead raises 5888 violations, so the check reaches them.
func (v *validator) validateAttributes(start xml.StartElement, ct *ComplexType, path string) []Violation {
	declared := collectExtendedAttributes(ct)
	var out []Violation
	for _, use := range declared {
		value, present := findAttr(start.Attr, use.Name)
		if !present {
			if use.Required {
				out = append(out, Violation{
					Rule:   RuleAttribute,
					Path:   path + "/@" + use.Name.Local,
					Reason: "required attribute missing",
				})
			}
			continue
		}
		out = append(out, attributeValueViolations(value, use, path)...)
	}
	var wildcard *attributeWildcardSet
	for _, a := range start.Attr {
		if !isSchemaAttribute(a.Name) {
			continue
		}
		if slices.ContainsFunc(declared, func(u AttributeUse) bool { return attrMatches(a.Name, u.Name) }) {
			continue
		}
		if wildcard == nil {
			wildcard = attributeWildcard(ct)
		}
		out = append(out, v.undeclaredAttribute(a, wildcard, path)...)
	}
	return out
}

// attributeValueViolations validates value, the value of the attribute
// use declares, against the use's fixed value and type.
func attributeValueViolations(value string, use AttributeUse, path string) []Violation {
	if use.Fixed != "" && !fixedValueMatches(value, use.Fixed, use.Type) {
		return []Violation{{
			Rule:   RuleAttribute,
			Path:   path + "/@" + use.Name.Local,
			Reason: fmt.Sprintf("fixed attribute value %q does not match required %q", value, use.Fixed),
		}}
	}
	var out []Violation
	for _, fv := range ValidateValue(value, use.Type) {
		out = append(out, Violation{
			Rule:   RuleAttribute,
			Path:   path + "/@" + use.Name.Local,
			Reason: fv.Reason,
		})
	}
	return out
}

// fixedValueMatches reports whether value, of type t, satisfies the fixed
// value constraint fixed. Per XML Schema Part 1, 3.2.4 (cvc-au) for an
// attribute and 3.3.4 (cvc-elt clause 5.2.2.2.2) for an element, the
// actual value must equal the constraint's value, so both sides are
// white-space normalized under the type's mode first: " EUR " matches a
// fixed "EUR" on an xs:token attribute. Normalized literals that differ
// can still denote one value: the ordered primitives compare in their
// value space ([compareRange]), so "1.0" matches a fixed decimal "1", and
// the boolean literals "1" and "true" match (XML Schema Part 2, 3.2.2.1).
// The other types compare literally, which is value equality for the
// string-derived types CII fixes. On a type derived from a union, each side
// is normalized and compared as the member type that validates it
// ([resolveUnionValue]).
func fixedValueMatches(value, fixed string, t Type) bool {
	budget := maxUnionMemberChecks
	got, want := resolveValue(value, t, 0, &budget), resolveValue(fixed, t, 0, &budget)
	value, fixed, base := got.normalized, want.normalized, got.base
	if value == fixed {
		return true
	}
	if o, status := compareRange(value, fixed, base); status == rangeCompared {
		return o == orderEqual
	}
	if base != nil && base.Name == (QName{Namespace: XSNamespace, Local: "boolean"}) {
		return checkBoolean(value, base) == nil && checkBoolean(fixed, base) == nil &&
			isTrueBoolean(value) == isTrueBoolean(fixed)
	}
	return false
}

// findAttr returns the value of the attribute of attrs that the
// declaration named want matches ([attrMatches]), plus whether one was
// found.
func findAttr(attrs []xml.Attr, want QName) (string, bool) {
	for _, a := range attrs {
		if attrMatches(a.Name, want) {
			return a.Value, true
		}
	}
	return "", false
}

// attrMatches reports whether the attribute name matches the declared
// name want. Per XML Schema Part 1, 3.4.4 (cvc-complex-type clause 3.1)
// the local names must match and the namespaces be identical, a
// declaration in no namespace matching an unprefixed attribute alone. A
// local declaration is in no namespace unless its form makes it qualified
// ([schemaDoc.localAttributeNamespace]), so a prefixed ram:currencyID is
// not the currencyID the CII schemas declare.
//
// # Corpus evidence
//
//   - All corpora: the 159 files that embed XML carry 2526 unprefixed
//     attributes and no prefixed one but xmlns and xsi, and no bundled
//     schema sets attributeFormDefault or form, so the exact match moves no
//     violation, through the Factur-X and ZUGFeRD validators or under any
//     bundled schema.
func attrMatches(name xml.Name, want QName) bool {
	return name.Local == want.Local && name.Space == want.Namespace
}

// sequenceMatcher walks an xs:sequence content model. It accepts
// child elements in declaration order, enforces minOccurs/maxOccurs
// per particle, and emits cardinality violations on finalize when
// any required particle was not fully consumed.
type sequenceMatcher struct {
	seq       Sequence
	particles []Particle
	// pos is the current particle index. The matcher advances when
	// the active particle reaches its maxOccurs ceiling or when an
	// incoming element name does not match the current particle and
	// the next particle accepts it.
	pos int
	// occurrence counts how many times the active particle has
	// matched. Reset to zero when the matcher advances past pos.
	occurrence int
	// satisfied[i] is true when particle i has reached its
	// minOccurs threshold. Carried across pos advances so finalize
	// can report which particles fell short.
	satisfied map[int]bool
}

func (m *sequenceMatcher) step(name xml.Name) (*Element, *Violation) {
	for m.pos < len(m.particles) {
		p := m.particles[m.pos]
		if p.Wildcard != nil {
			// Wildcards accept any qualified name and treat the
			// child as opaque (process-contents="skip" semantics).
			// Tracking occurrence keeps a finite-maxOccurs wildcard
			// from accepting more than the spec allows.
			m.occurrence++
			if m.occurrence >= maxOccursOrUnlimited(p.Wildcard.MaxOccurs) {
				m.advance()
			}
			return nil, nil
		}
		if p.Element == nil {
			m.advance()
			continue
		}
		want := p.Element.Name
		if want.Namespace == name.Space && want.Local == name.Local {
			m.occurrence++
			if m.occurrence >= maxOccursOrUnlimited(p.Element.MaxOccurs) {
				m.satisfy(m.pos, p.Element.MinOccurs)
				m.advance()
			}
			return p.Element, nil
		}
		// Name does not match the active particle. If the active
		// particle has reached its minOccurs the matcher can advance
		// and re-test; otherwise this is a structural violation.
		if m.occurrence >= p.Element.MinOccurs {
			m.satisfy(m.pos, p.Element.MinOccurs)
			m.advance()
			continue
		}
		return nil, &Violation{
			Rule:   RuleStructure,
			Reason: fmt.Sprintf("expected %s, got %s", nameOf(xml.Name{Space: want.Namespace, Local: want.Local}), nameOf(name)),
		}
	}
	return nil, &Violation{
		Rule:   RuleStructure,
		Reason: fmt.Sprintf("unexpected element %s after sequence exhausted", nameOf(name)),
	}
}

func (m *sequenceMatcher) advance() {
	m.pos++
	m.occurrence = 0
}

func (m *sequenceMatcher) satisfy(pos, minOccurs int) {
	if m.satisfied == nil {
		m.satisfied = make(map[int]bool)
	}
	if m.occurrence >= minOccurs {
		m.satisfied[pos] = true
	}
}

func (m *sequenceMatcher) finalize(path string) []Violation {
	var out []Violation
	// Capture the in-flight particle's count too: the parent
	// EndElement may arrive while the active particle has accrued
	// some occurrences but not been advanced past.
	if m.pos < len(m.particles) && m.particles[m.pos].Element != nil {
		if m.occurrence >= m.particles[m.pos].Element.MinOccurs {
			m.satisfy(m.pos, m.particles[m.pos].Element.MinOccurs)
		}
	}
	for i, p := range m.particles {
		if p.Element == nil {
			continue
		}
		if p.Element.MinOccurs == 0 {
			continue
		}
		if m.satisfied != nil && m.satisfied[i] {
			continue
		}
		if i == m.pos && m.occurrence >= p.Element.MinOccurs {
			continue
		}
		out = append(out, Violation{
			Rule: RuleCardinality,
			Path: path + "/" + p.Element.Name.Local,
			Reason: fmt.Sprintf("element %s requires minOccurs=%d, got %d",
				p.Element.Name.Local, p.Element.MinOccurs, occurrenceCount(m, i)),
		})
	}
	return out
}

func occurrenceCount(m *sequenceMatcher, i int) int {
	if i == m.pos {
		return m.occurrence
	}
	if m.satisfied != nil && m.satisfied[i] {
		// Lost the exact count once satisfied; reporting "at least
		// minOccurs" without the exact tally is fine for the
		// finalize path because only unsatisfied particles are reported.
		return m.particles[i].Element.MinOccurs
	}
	return 0
}

func maxOccursOrUnlimited(max int) int {
	if max == MaxUnbounded {
		// Use a large sentinel so the comparison m.occurrence >= max
		// never trips. Pure-Go validators tend to bound documents by
		// element count elsewhere; the comparison here is just for
		// matcher advance.
		return 1 << 30
	}
	return max
}

// choiceMatcher implements xs:choice content semantics: any one of
// the listed particles accepts a child. Cardinality is on the choice
// itself, not on the alternatives, so an unmatched alternative does
// not trip RuleCardinality on finalize. The current model carries
// choice cardinality only at the particle level, so a top-level
// choice content model is treated as min=1 max=1 by default; nested
// choice particles inherit their bounds from the surrounding sequence.
type choiceMatcher struct {
	particles []Particle
	// matched counts how many alternatives have been accepted so
	// far. For a min=1 max=1 choice, finalize requires matched >= 1
	// and step refuses additional matches.
	matched int
}

func (m *choiceMatcher) step(name xml.Name) (*Element, *Violation) {
	if m.matched >= 1 {
		return nil, &Violation{
			Rule:   RuleStructure,
			Reason: fmt.Sprintf("unexpected element %s: choice already satisfied", nameOf(name)),
		}
	}
	for _, p := range m.particles {
		if p.Wildcard != nil {
			m.matched++
			return nil, nil
		}
		if p.Element == nil {
			continue
		}
		want := p.Element.Name
		if want.Namespace == name.Space && want.Local == name.Local {
			m.matched++
			return p.Element, nil
		}
	}
	return nil, &Violation{
		Rule:   RuleStructure,
		Reason: fmt.Sprintf("element %s matches no alternative in choice", nameOf(name)),
	}
}

func (m *choiceMatcher) finalize(path string) []Violation {
	if m.matched >= 1 {
		return nil
	}
	return []Violation{{
		Rule:   RuleCardinality,
		Path:   path,
		Reason: "choice requires at least one alternative to match",
	}}
}

// emptyMatcher accepts no children and surfaces every encountered
// child as a structure violation. Used for complex types whose
// Content is the [Empty] sentinel (attribute-only types).
type emptyMatcher struct{}

func (emptyMatcher) step(name xml.Name) (*Element, *Violation) {
	return nil, &Violation{
		Rule:   RuleStructure,
		Reason: fmt.Sprintf("unexpected element %s inside empty content model", nameOf(name)),
	}
}

func (emptyMatcher) finalize(string) []Violation { return nil }
