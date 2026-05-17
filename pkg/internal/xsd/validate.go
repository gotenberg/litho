package xsd

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
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
)

// Validate streams payload through the XSD engine and returns every
// failure as a [Violation]. An empty slice means the document
// satisfies schema's content model, cardinality, and leaf-type
// constraints. An underlying parse error (malformed XML, unexpected
// EOF) is returned as the second value distinct from violations so
// callers can tell "ran-but-failed-rules" apart from "could-not-run".
//
// PR3 covers element ordering, cardinality, leaf-type checks, and
// unknown-element detection. Attribute validation, xs:choice, and
// xs:any wildcards land in PR4; CII bundling lands in PR5.
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
		decoder: xml.NewDecoder(bytes.NewReader(payload)),
	}
	v.decoder.Strict = true
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
	// model. Nil when element.Type is a simple type (leaf).
	matcher contentMatcher
	// pendingText accumulates CharData while waiting for the
	// matching EndElement; only used when the type is a simple
	// type so the leaf value can be validated on close.
	pendingText strings.Builder
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
		v.violations = append(v.violations, Violation{
			Rule:   RuleStructure,
			Path:   v.path() + "/" + nameOf(start.Name),
			Reason: fmt.Sprintf("element %s appears inside simple-type content", nameOf(start.Name)),
		})
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
	v.pushFrame(start, el)
}

func (v *validator) onEnd(end xml.EndElement) {
	if len(v.stack) == 0 {
		return
	}
	top := v.stack[len(v.stack)-1]
	if top.matcher != nil {
		v.violations = append(v.violations, top.matcher.finalize(v.path())...)
	} else if top.element != nil {
		// Simple-type leaf: validate the accumulated text against
		// the type's lexical space and facets.
		text := strings.TrimSpace(top.pendingText.String())
		if text != "" {
			for _, fv := range ValidateValue(text, top.element.Type) {
				rule := RuleFacet
				if fv.Facet == nil {
					rule = RuleLexical
				}
				v.violations = append(v.violations, Violation{
					Rule:   rule,
					Path:   v.path(),
					Reason: fv.Reason,
				})
			}
		}
	}
	v.stack = v.stack[:len(v.stack)-1]
}

func (v *validator) onCharData(t xml.CharData) {
	if len(v.stack) == 0 {
		return
	}
	top := v.stack[len(v.stack)-1]
	if top.element == nil {
		return
	}
	if _, simple := top.element.Type.(*SimpleType); simple {
		top.pendingText.Write(t)
		return
	}
	if _, builtin := top.element.Type.(*BuiltinType); builtin {
		top.pendingText.Write(t)
	}
}

// openRoot consumes the document element and pushes the first frame.
// The element must be declared at the schema top level (or one of
// its imports) for validation to proceed.
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

func (v *validator) findRootElement(qn QName) (*Element, bool) {
	if el, ok := v.schema.Elements[qn]; ok {
		return el, true
	}
	for _, imp := range v.schema.Imports {
		if el, ok := imp.Elements[qn]; ok {
			return el, true
		}
	}
	return nil, false
}

// pushFrame opens a frame for an accepted element. The matcher is
// installed when the type carries an element content model; simple
// types and built-ins leave matcher nil so CharData is collected as
// leaf text instead.
func (v *validator) pushFrame(start xml.StartElement, el *Element) {
	f := &frame{element: el, name: start.Name}
	if ct, ok := el.Type.(*ComplexType); ok {
		f.matcher = newMatcher(ct, v.schema)
	}
	v.stack = append(v.stack, f)
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
// model. Resolves the model by walking xs:extension chains so the
// content seen by the matcher includes inherited particles from
// the base type. Base-merging is a PR4 concern; PR3 honours the
// type's own Content only.
func newMatcher(ct *ComplexType, schema *Schema) contentMatcher {
	switch m := ct.Content.(type) {
	case Sequence:
		return &sequenceMatcher{seq: m, particles: m.Particles}
	case Choice:
		// Treat choice as a tolerant sequence in PR3: any particle
		// is accepted, cardinality enforced. PR4 promotes this to a
		// true choice matcher.
		seq := Sequence(m)
		return &sequenceMatcher{seq: seq, particles: m.Particles}
	}
	return &emptyMatcher{}
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
			// Wildcards are skipped permissively in PR3: any name
			// matches, no cardinality counting.
			m.advance()
			continue
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
		// particle has reached its minOccurs we can advance and
		// re-test; otherwise this is a structural violation.
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
		// finalize path because we only report unsatisfied particles.
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
