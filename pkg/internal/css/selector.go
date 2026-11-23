package css

import (
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/intconv"
	"github.com/gotenberg/litho/pkg/internal/numeric"
)

// attrOp is an attribute selector's operator (Selectors 4, 6).
type attrOp uint8

const (
	attrExists attrOp = iota
	attrEquals
	attrIncludes
	attrDash
	attrPrefix
	attrSuffix
	attrSubstring
)

// attrSel is an attribute selector.
type attrSel struct {
	name  string
	op    attrOp
	value string
	// prefix is the namespace prefix of a name written ns|name, whose
	// name is then "": an HTML element's attributes are in no namespace.
	prefix string
	// fold compares the value ASCII case-insensitively, the i flag.
	// Selectors 4, 6.3 adds the s flag, which compares it exactly; litho
	// refuses it, as Chromium does not parse it.
	fold bool
}

// pseudoKind is a pseudo-class litho knows.
type pseudoKind uint8

const (
	psRoot pseudoKind = iota
	psFirstChild
	psLastChild
	psOnlyChild
	psFirstOfType
	psLastOfType
	psOnlyOfType
	psNthChild
	psNthLastChild
	psNthOfType
	psNthLastOfType
	psEmpty
	psNot
	psIs
	psWhere
	psLink
	psLang
	psChecked
	psDisabled
	psEnabled
	psAlways
	// psNever is a user-action or other dynamic pseudo-class: a static
	// paged document is never hovered, focused, active or visited.
	psNever
	psHas
	psDir
	psRequired
	psOptional
	psReadOnly
	psReadWrite
	psPlaceholderShown
	psDefault
	psIndeterminate
	psValid
	psInvalid
	psInRange
	psOutOfRange
	psOpen
	// psHost is :host and :host(), psHostContext :host-context(): they
	// match a shadow host from inside its shadow tree, args holding the
	// compound a functional form tests (CSS Scoping 1, 3.2.1).
	psHost
	psHostContext
	psDefined
)

// pseudoSel is a pseudo-class with its arguments.
type pseudoSel struct {
	kind pseudoKind
	// a and b are the An+B of the :nth-* pseudo-classes; of is their
	// "of S" list.
	a, b int
	of   []*complexSel
	// args is the argument list of :not(), :is() and :where(), and the
	// relative selectors of :has(); a is also the direction of :dir(), 0
	// for ltr, 1 for rtl, 2 for none.
	args []*complexSel
	// langs are the ranges of :lang().
	langs []string
}

// compound is a compound selector (Selectors 4, 3.1).
type compound struct {
	// tag is the lowercased type selector, "" for none or *.
	tag     string
	ids     []string
	classes []string
	attrs   []attrSel
	pseudos []pseudoSel
	// never marks a compound no element matches: |p, or an attribute
	// selector with a namespace prefix.
	never bool
	// ns is the namespace an element must be in, nsAny for any; prefix is
	// the namespace prefix of its type selector as written, "" for none,
	// which a style sheet's @namespace rules resolve (CSS Namespaces 3,
	// 4), and attrPrefixes those of its attribute selectors. anyNS marks
	// a type selector written *|, which no default namespace narrows;
	// typed a compound that writes a type or universal selector.
	ns           elementNS
	prefix       string
	attrPrefixes []string
	anyNS        bool
	typed        bool
	// slotted is the argument of a trailing ::slotted(), one compound
	// selector (CSS Scoping 1, 3.2.2); partNames the names of a trailing
	// ::part() (CSS Shadow Parts 1, 4.1).
	slotted   []*complexSel
	partNames []string
}

// elementNS is the namespace a compound selector asks an element to be in.
type elementNS uint8

// The namespaces of the elements of a parsed HTML document (HTML, 13.2):
// HTML, SVG and MathML; nsUnknown is any other, which no element is in.
const (
	nsAny elementNS = iota
	nsHTML
	nsSVG
	nsMathML
	nsUnknown
)

// complexSel is a complex selector (Selectors 4, 3.1): compounds from
// left to right and the combinators between them.
type complexSel struct {
	parts []compound
	// combs[i] joins parts[i] and parts[i+1]: ' ', '>', '+' or '~'.
	combs []byte
	spec  uint32
	// lead is the combinator a relative selector of :has() starts with
	// (Selectors 4, 4.2): ' ', '>', '+' or '~'; 0 for any other selector.
	lead byte
	// pseudoElement names the pseudo-element the selector targets, "" for
	// the element itself.
	pseudoElement string
}

// selectorError says why a selector did not parse; unsupported names a
// valid construct litho does not implement, for a warning.
type selectorError struct {
	unsupported string
}

// specBits is the width of each of the three components of a packed
// specificity, and specMax the value each saturates at.
const (
	specBits = 10
	specMax  = 1<<specBits - 1
)

// packSpec packs a specificity (Selectors 4, 17): a, b and c each saturate
// at specMax.
func packSpec(a, b, c int) uint32 {
	sat := func(v int) uint32 {
		if v > specMax {
			return specMax
		}
		return uint32(v) //nolint:gosec // non-negative and at most specMax, checked above
	}
	return sat(a)<<(2*specBits) | sat(b)<<specBits | sat(c)
}

func unpackSpec(s uint32) (a, b, c int) {
	return int(s >> (2 * specBits) & specMax), int(s >> specBits & specMax), int(s & specMax) // fits int: each masked to specBits bits
}

func addSpec(x, y uint32) uint32 {
	a1, b1, c1 := unpackSpec(x)
	a2, b2, c2 := unpackSpec(y)
	return packSpec(a1+a2, b1+b2, c1+c2)
}

// knownPseudoElements are the pseudo-elements that parse (CSS Pseudo 4).
// Rules targeting ::before, ::after, ::marker, ::details-content and
// ::placeholder give the styles [Computed.Pseudo] returns; those targeting
// the others produce nothing.
// ::column (CSS Multicol 2, 4.8, which Chromium parses) takes the scroll
// snap properties alone, which print nothing.
var knownPseudoElements = map[string]bool{
	"before": true, "after": true, "first-line": true, "first-letter": true, "marker": true,
	"placeholder": true, "selection": true, "backdrop": true, "file-selector-button": true,
	"target-text": true, "spelling-error": true, "grammar-error": true, "cue": true,
	"footnote-call": true, "footnote-marker": true, "note-call": true, "note-marker": true,
	"note-callback": true, "column": true, "details-content": true,
	// The parts of a customizable select (CSS Form Control Styling 1,
	// 4), which Chromium parses: a printed select shows no picker.
	"picker-icon": true, "checkmark": true,
}

// subPseudoElements are the pseudo-elements that may follow another (CSS
// Pseudo 4, 4.3 and 3.2: ::before, ::after and ::marker of ::before,
// ::after and ::details-content), as Chromium parses them. A selector of
// such a chain parses and matches nothing: litho generates no
// pseudo-element of a pseudo-element.
var subPseudoElements = map[string]map[string]bool{
	"before":          {"marker": true},
	"after":           {"marker": true},
	"details-content": {"before": true, "after": true, "marker": true},
}

// simplePseudos maps the argument-less pseudo-classes to their kind.
var simplePseudos = map[string]pseudoKind{
	"root": psRoot, "scope": psRoot, "first-child": psFirstChild, "last-child": psLastChild,
	"only-child": psOnlyChild, "first-of-type": psFirstOfType, "last-of-type": psLastOfType,
	"only-of-type": psOnlyOfType, "empty": psEmpty, "link": psLink, "any-link": psLink,
	"checked": psChecked, "disabled": psDisabled, "enabled": psEnabled, "defined": psDefined,
	"hover": psNever, "focus": psNever, "active": psNever, "visited": psNever,
	"focus-within": psNever, "focus-visible": psNever, "target": psNever,
	"required": psRequired, "optional": psOptional, "read-only": psReadOnly, "read-write": psReadWrite,
	"placeholder-shown": psPlaceholderShown, "default": psDefault, "indeterminate": psIndeterminate,
	"valid": psValid, "invalid": psInvalid, "in-range": psInRange, "out-of-range": psOutOfRange, "open": psOpen,
	// A static document has no user interaction, autofill or top layer.
	"user-valid": psNever, "user-invalid": psNever, "autofill": psNever, "popover-open": psNever,
	"modal": psNever, "fullscreen": psNever, "host": psHost,
}

// parseSelectorList parses a <selector-list> (Selectors 4, 4.1). A
// forgiving list (:is(), :where()) drops the selectors that fail instead of
// failing whole (Selectors 4, 4.3).
func parseSelectorList(list []token, depth int, forgiving bool) ([]*complexSel, *selectorError) {
	var out []*complexSel
	start := 0
	for i := 0; i <= len(list); i++ {
		if i < len(list) && list[i].typ != tComma {
			continue
		}
		s, err := parseComplex(trimWS(list[start:i]), depth)
		start = i + 1
		if err != nil {
			if !forgiving {
				return nil, err
			}
			continue
		}
		if len(out) >= maxSelectorsPerList {
			return nil, &selectorError{}
		}
		out = append(out, s)
	}
	if len(out) == 0 && !forgiving {
		return nil, &selectorError{}
	}
	return out, nil
}

// selCursor walks the component values of a selector.
type selCursor struct {
	l []token
	i int
}

func (c *selCursor) peek() *token {
	if c.i < len(c.l) {
		return &c.l[c.i]
	}
	return nil
}

func (c *selCursor) ws() bool {
	seen := false
	for c.i < len(c.l) && c.l[c.i].typ == tWhitespace {
		c.i++
		seen = true
	}
	return seen
}

// parseComplex parses a <complex-selector> (Selectors 4, 4.1) and computes
// its specificity.
func parseComplex(list []token, depth int) (*complexSel, *selectorError) {
	if len(list) == 0 {
		return nil, &selectorError{}
	}
	c := &selCursor{l: list}
	s := &complexSel{}
	for {
		comp, pe, err := parseCompound(c, depth)
		if err != nil {
			return nil, err
		}
		if s.pseudoElement != "" {
			return nil, &selectorError{}
		}
		s.parts = append(s.parts, comp)
		s.pseudoElement = pe
		if len(s.parts) > maxCompounds {
			return nil, &selectorError{}
		}
		hadWS := c.ws()
		t := c.peek()
		if t == nil {
			break
		}
		switch {
		case t.isDelim('>') || t.isDelim('+') || t.isDelim('~'):
			s.combs = append(s.combs, byte(t.delim)) //nolint:gosec // one of >, + and ~, all ASCII
			c.i++
			c.ws()
		case hadWS:
			s.combs = append(s.combs, ' ')
		default:
			return nil, &selectorError{}
		}
	}
	var spec uint32
	for i := range s.parts {
		spec = addSpec(spec, s.parts[i].specificity())
	}
	if s.pseudoElement != "" {
		spec = addSpec(spec, packSpec(0, 0, 1))
		// ::slotted() adds its argument's (CSS Scoping 1, 3.2.2).
		spec = addSpec(spec, maxSpec(s.parts[len(s.parts)-1].slotted))
	}
	s.spec = spec
	return s, nil
}

// specificity is the specificity of a compound (Selectors 4, 17).
func (c *compound) specificity() uint32 {
	a := len(c.ids)
	b := len(c.classes) + len(c.attrs)
	ct := 0
	if c.tag != "" {
		ct = 1
	}
	spec := packSpec(a, b, ct)
	for i := range c.pseudos {
		p := &c.pseudos[i]
		switch p.kind { //nolint:exhaustive // the default covers the other pseudo-classes
		case psWhere:
		case psNot, psIs, psHas:
			spec = addSpec(spec, maxSpec(p.args))
		case psNthChild, psNthLastChild:
			spec = addSpec(spec, addSpec(packSpec(0, 1, 0), maxSpec(p.of)))
		case psHost, psHostContext:
			// A pseudo-class, plus its argument's (CSS Scoping 1, 3.2.1).
			spec = addSpec(spec, addSpec(packSpec(0, 1, 0), maxSpec(p.args)))
		default:
			spec = addSpec(spec, packSpec(0, 1, 0))
		}
	}
	return spec
}

func maxSpec(l []*complexSel) uint32 {
	var m uint32
	for _, s := range l {
		m = max(m, s.spec)
	}
	return m
}

// parseCompound parses a <compound-selector> and an optional trailing
// pseudo-element, which only user-action pseudo-classes may follow
// (Selectors 4, 3.6.3).
func parseCompound(c *selCursor, depth int) (compound, string, *selectorError) {
	var comp compound
	empty := true
	if t := c.peek(); t != nil && (t.typ == tIdent || t.isDelim('*') || t.isDelim('|')) {
		empty = false
		comp.typed = true
		if t.isDelim('|') {
			// |p names an element in no namespace: every element of an
			// HTML document is in one (Selectors 4, 5.2).
			comp.never = true
			c.i++
			if nt := c.peek(); nt == nil || nt.typ != tIdent && !nt.isDelim('*') {
				return comp, "", &selectorError{}
			}
			c.i++
		} else {
			if t.typ == tIdent {
				comp.tag = lower(t.value)
			}
			anyNS := t.isDelim('*')
			c.i++
			if nt := c.peek(); nt != nil && nt.isDelim('|') {
				c.i++
				nn := c.peek()
				if nn == nil || nn.typ != tIdent && !nn.isDelim('*') {
					return comp, "", &selectorError{}
				}
				// *|p names an element in any namespace; a named prefix
				// names the namespace an @namespace rule binds it to,
				// which [resolveNamespaces] sets: until then it matches
				// nothing.
				comp.tag = ""
				if nn.typ == tIdent {
					comp.tag = lower(nn.value)
				}
				if anyNS {
					comp.anyNS = true
				} else {
					comp.prefix, comp.ns = t.value, nsUnknown
				}
				c.i++
			}
		}
	}
	pseudoElement := ""
	for {
		t := c.peek()
		if t == nil {
			break
		}
		if pseudoElement != "" && t.typ != tColon {
			break
		}
		switch {
		case t.typ == tHash:
			if !t.id || pseudoElement != "" {
				return comp, "", &selectorError{}
			}
			comp.ids = append(comp.ids, t.value)
			c.i++
		case t.isDelim('.'):
			c.i++
			n := c.peek()
			if n == nil || n.typ != tIdent || pseudoElement != "" {
				return comp, "", &selectorError{}
			}
			comp.classes = append(comp.classes, n.value)
			c.i++
		case t.typ == tLBracket:
			if pseudoElement != "" {
				return comp, "", &selectorError{}
			}
			a, ok, sensitive := parseAttr(t.children)
			if sensitive {
				return comp, "", &selectorError{unsupported: "[" + a.name + " s]"}
			}
			if !ok {
				return comp, "", &selectorError{}
			}
			if a.name == "" {
				comp.never = true
				comp.attrPrefixes = append(comp.attrPrefixes, a.prefix)
			} else {
				comp.attrs = append(comp.attrs, a)
			}
			c.i++
		case t.typ == tColon:
			c.i++
			n := c.peek()
			if n == nil {
				return comp, "", &selectorError{}
			}
			if n.typ == tColon {
				c.i++
				pe := c.peek()
				if pe != nil && pe.typ == tFunction && pseudoElement == "" && eqFold(pe.value, "slotted") {
					// ::slotted(<compound-selector>) (CSS Scoping 1,
					// 3.2.2).
					arg, err := parseCompoundArg(pe.children, depth)
					if err != nil {
						return comp, "", err
					}
					comp.slotted = arg
					pseudoElement = "slotted"
					c.i++
					empty = false
					continue
				}
				if pe != nil && pe.typ == tFunction && pseudoElement == "" && eqFold(pe.value, "part") {
					// ::part(<ident>+) (CSS Shadow Parts 1, 4.1).
					args := significant(pe.children)
					if len(args) == 0 {
						return comp, "", &selectorError{}
					}
					for _, a := range args {
						if a.typ != tIdent {
							return comp, "", &selectorError{}
						}
						comp.partNames = append(comp.partNames, a.value)
					}
					pseudoElement = "part"
					c.i++
					empty = false
					continue
				}
				if pe != nil && pe.typ == tFunction && pseudoElement == "" && eqFold(pe.value, "picker") {
					// ::picker(select) is the drop-down of a customizable
					// select (CSS Form Control Styling 1, 4), which a
					// printed page never opens: it parses, as Chromium
					// parses it, and matches nothing.
					arg := significant(pe.children)
					if len(arg) != 1 || arg[0].typ != tIdent || !eqFold(arg[0].value, "select") {
						return comp, "", &selectorError{}
					}
					comp.never = true
					pseudoElement = "picker"
					c.i++
					empty = false
					continue
				}
				if pe != nil && pe.typ == tIdent && pseudoElement != "" && subPseudoElements[pseudoElement][lower(pe.value)] {
					comp.never = true
					pseudoElement = lower(pe.value)
					c.i++
					continue
				}
				if pe == nil || pe.typ != tIdent || pseudoElement != "" {
					return comp, "", &selectorError{}
				}
				name := lower(pe.value)
				if name == "-webkit-input-placeholder" {
					// Chromium's name of ::placeholder, which it parses as
					// the same pseudo-element.
					name = "placeholder"
				}
				if !knownPseudoElements[name] {
					return comp, "", &selectorError{unsupported: "::" + name}
				}
				pseudoElement = name
				c.i++
				empty = false
				continue
			}
			if n.typ == tIdent {
				name := lower(n.value)
				switch name {
				case "before", "after", "first-line", "first-letter":
					// The CSS 2.1 single-colon syntax of the four legacy
					// pseudo-elements (Selectors 4, 3.6.1).
					if pseudoElement != "" {
						return comp, "", &selectorError{}
					}
					pseudoElement = name
					c.i++
					empty = false
					continue
				}
			}
			p, err := parsePseudo(*n, depth)
			if err != nil {
				return comp, "", err
			}
			if pseudoElement != "" && p.kind != psNever {
				return comp, "", &selectorError{}
			}
			comp.pseudos = append(comp.pseudos, p)
			c.i++
		default:
			if empty {
				return comp, "", &selectorError{}
			}
			return comp, pseudoElement, nil
		}
		empty = false
	}
	if empty {
		return comp, "", &selectorError{}
	}
	return comp, pseudoElement, nil
}

// parseCompoundArg parses the argument of :host(), :host-context() and
// ::slotted(): one compound selector, without a pseudo-element.
func parseCompoundArg(list []token, depth int) ([]*complexSel, *selectorError) {
	if depth >= maxSelectorNesting {
		return nil, &selectorError{}
	}
	arg, err := parseComplex(trimWS(list), depth+1)
	if err != nil {
		return nil, err
	}
	if len(arg.parts) != 1 || arg.pseudoElement != "" {
		return nil, &selectorError{}
	}
	return []*complexSel{arg}, nil
}

// parseAttr parses the contents of an attribute selector (Selectors 4, 6).
// A namespace prefix yields an empty name: no element matches it.
// sensitive reports the s flag, which Chromium does not parse.
func parseAttr(list []token) (a attrSel, ok, sensitive bool) {
	c := &selCursor{l: list}
	c.ws()
	t := c.peek()
	if t == nil {
		return a, false, false
	}
	switch {
	case t.typ == tIdent:
		a.name = lower(t.value)
		c.i++
		if n := c.peek(); n != nil && n.isDelim('|') && c.i+1 < len(list) && list[c.i+1].typ == tIdent {
			c.i += 2
			a.name, a.prefix = "", t.value
		}
	case t.isDelim('*') || t.isDelim('|'):
		c.i++
		if t.isDelim('*') {
			if n := c.peek(); n == nil || !n.isDelim('|') {
				return a, false, false
			}
			c.i++
		}
		n := c.peek()
		if n == nil || n.typ != tIdent {
			return a, false, false
		}
		c.i++
		// |a names an attribute in no namespace and *|a one in any
		// (Selectors 4, 6.5): an HTML element's attributes are in none.
		a.name = lower(n.value)
	default:
		return a, false, false
	}
	c.ws()
	t = c.peek()
	if t == nil {
		return a, true, false
	}
	if t.typ != tDelim {
		return a, false, false
	}
	switch t.delim {
	case '=':
		a.op = attrEquals
		c.i++
	case '~', '|', '^', '$', '*':
		c.i++
		if n := c.peek(); n == nil || !n.isDelim('=') {
			return a, false, false
		}
		c.i++
		a.op = map[rune]attrOp{'~': attrIncludes, '|': attrDash, '^': attrPrefix, '$': attrSuffix, '*': attrSubstring}[t.delim]
	default:
		return a, false, false
	}
	c.ws()
	v := c.peek()
	if v == nil || (v.typ != tIdent && v.typ != tString) {
		return a, false, false
	}
	a.value = v.value
	c.i++
	c.ws()
	if f := c.peek(); f != nil {
		switch {
		case f.isIdent("i"):
			a.fold = true
		case f.isIdent("s"):
			return a, false, true
		default:
			return a, false, false
		}
		c.i++
		c.ws()
	}
	return a, c.peek() == nil, false
}

// parsePseudo parses a pseudo-class after its colon.
func parsePseudo(t token, depth int) (pseudoSel, *selectorError) {
	switch t.typ { //nolint:exhaustive // the other token types are invalid here
	case tIdent:
		name := lower(t.value)
		if k, ok := simplePseudos[name]; ok {
			return pseudoSel{kind: k}, nil
		}
		return pseudoSel{}, &selectorError{unsupported: ":" + name}
	case tFunction:
		name := lower(t.value)
		if depth >= maxSelectorNesting {
			return pseudoSel{}, &selectorError{}
		}
		switch name {
		case "not", "is", "where", "-webkit-any":
			kind := psIs
			switch name {
			case "not":
				kind = psNot
			case "where":
				kind = psWhere
			}
			list, err := parseSelectorList(t.children, depth+1, kind != psNot)
			if err != nil {
				return pseudoSel{}, err
			}
			// A pseudo-element is no selector of an element: a
			// forgiving list drops it (Selectors 4, 4.3), :not() fails.
			kept := list[:0]
			for _, s := range list {
				if s.pseudoElement == "" {
					kept = append(kept, s)
				} else if kind == psNot {
					return pseudoSel{}, &selectorError{}
				}
			}
			return pseudoSel{kind: kind, args: kept}, nil
		case "has":
			list, err := parseRelativeList(t.children, depth+1)
			if err != nil {
				return pseudoSel{}, err
			}
			return pseudoSel{kind: psHas, args: list}, nil
		case "dir":
			// :dir() takes an identifier; one other than ltr and rtl
			// is valid and matches nothing (Selectors 4, 7.1).
			args := significant(t.children)
			if len(args) != 1 || args[0].typ != tIdent {
				return pseudoSel{}, &selectorError{}
			}
			dir := 2
			switch lower(args[0].value) {
			case "ltr":
				dir = 0
			case "rtl":
				dir = 1
			}
			return pseudoSel{kind: psDir, a: dir}, nil
		case "state":
			// Custom element states: litho runs no script that sets one.
			if len(significant(t.children)) == 0 {
				return pseudoSel{}, &selectorError{}
			}
			return pseudoSel{kind: psNever}, nil
		case "host", "host-context":
			// :host(<compound-selector>), :host-context(<compound-selector>)
			// (CSS Scoping 1, 3.2.1).
			arg, err := parseCompoundArg(t.children, depth)
			if err != nil {
				return pseudoSel{}, err
			}
			kind := psHost
			if name == "host-context" {
				kind = psHostContext
			}
			return pseudoSel{kind: kind, args: arg}, nil
		case "nth-child", "nth-last-child", "nth-of-type", "nth-last-of-type":
			kind := map[string]pseudoKind{
				"nth-child": psNthChild, "nth-last-child": psNthLastChild,
				"nth-of-type": psNthOfType, "nth-last-of-type": psNthLastOfType,
			}[name]
			args := t.children
			var of []*complexSel
			if kind == psNthChild || kind == psNthLastChild {
				for i := range args {
					if args[i].isIdent("of") {
						list, err := parseSelectorList(args[i+1:], depth+1, false)
						if err != nil {
							return pseudoSel{}, err
						}
						of = list
						args = args[:i]
						break
					}
				}
			}
			a, b, ok := parseAnB(args)
			if !ok {
				return pseudoSel{}, &selectorError{}
			}
			return pseudoSel{kind: kind, a: a, b: b, of: of}, nil
		case "lang":
			// Selectors 4, 7.2 takes a comma-separated list of
			// language ranges, identifiers or strings; litho follows
			// Chromium, which parses one identifier alone.
			args := significant(t.children)
			if len(args) != 1 || args[0].typ != tIdent {
				if len(args) > 0 {
					return pseudoSel{}, &selectorError{unsupported: ":lang() of a string or a list"}
				}
				return pseudoSel{}, &selectorError{}
			}
			return pseudoSel{kind: psLang, langs: []string{lower(args[0].value)}}, nil
		}
		return pseudoSel{}, &selectorError{unsupported: ":" + name + "()"}
	}
	return pseudoSel{}, &selectorError{}
}

// parseRelativeList parses the <relative-selector-list> of :has()
// (Selectors 4, 4.2 and 4.5): each selector may start with a combinator,
// a descendant one when it has none. As Chromium parses it, the list is
// not forgiving, and holds no :has() and no pseudo-element.
func parseRelativeList(list []token, depth int) ([]*complexSel, *selectorError) {
	var out []*complexSel
	for _, part := range splitCommas(list) {
		part = trimWS(part)
		lead := byte(' ')
		if len(part) > 0 && (part[0].isDelim('>') || part[0].isDelim('+') || part[0].isDelim('~')) {
			lead = byte(part[0].delim) //nolint:gosec // one of >, + and ~, all ASCII
			part = trimWS(part[1:])
		}
		s, err := parseComplex(part, depth)
		if err != nil {
			return nil, err
		}
		if s.pseudoElement != "" || hasHas(s) {
			return nil, &selectorError{}
		}
		if len(out) >= maxSelectorsPerList {
			return nil, &selectorError{}
		}
		s.lead = lead
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil, &selectorError{}
	}
	return out, nil
}

// hasHas reports a :has() anywhere in s, which a relative selector may
// not hold.
func hasHas(s *complexSel) bool {
	for i := range s.parts {
		for _, p := range s.parts[i].pseudos {
			if p.kind == psHas || slices.ContainsFunc(p.args, hasHas) || slices.ContainsFunc(p.of, hasHas) {
				return true
			}
		}
	}
	return false
}

// splitCommas splits a list at its top-level commas.
func splitCommas(l []token) [][]token {
	var out [][]token
	start := 0
	for i := 0; i <= len(l); i++ {
		if i == len(l) || l[i].typ == tComma {
			if i > start || i < len(l) {
				out = append(out, l[start:i])
			}
			start = i + 1
		}
	}
	return out
}

// parseAnB parses the An+B microsyntax (CSS Syntax 3, 6.2). Escapes are
// already resolved in the token values, as the grammar asks.
func parseAnB(list []token) (a, b int, ok bool) {
	c := &selCursor{l: list}
	c.ws()
	t := c.peek()
	if t == nil {
		return 0, 0, false
	}
	c.i++
	done := func(a, b int) (int, int, bool) {
		c.ws()
		return a, b, c.peek() == nil
	}
	switch t.typ { //nolint:exhaustive // the other token types are invalid here
	case tNumber:
		if !t.integer {
			return 0, 0, false
		}
		n, ok := intOf(t.num)
		if !ok {
			return 0, 0, false
		}
		return done(0, n)
	case tDimension:
		if !t.integer {
			return 0, 0, false
		}
		n, ok := intOf(t.num)
		if !ok {
			return 0, 0, false
		}
		unit := lower(t.value)
		switch {
		case unit == "n":
			return anbTail(c, n)
		case unit == "n-":
			return anbSignless(c, n, -1)
		case strings.HasPrefix(unit, "n-") && allDigits(unit[2:]):
			d, ok := atoiCapped(unit[2:])
			if !ok {
				return 0, 0, false
			}
			return done(n, -d)
		}
		return 0, 0, false
	case tIdent:
		v := lower(t.value)
		switch v {
		case "even":
			return done(2, 0)
		case "odd":
			return done(2, 1)
		}
		return anbIdent(c, v, false)
	case tDelim:
		if t.delim != '+' {
			return 0, 0, false
		}
		n := c.peek()
		if n == nil || n.typ != tIdent {
			return 0, 0, false
		}
		c.i++
		return anbIdent(c, lower(n.value), true)
	}
	return 0, 0, false
}

// anbIdent handles the ident forms of An+B: n, -n, n-, -n-, n-<digits> and
// -n-<digits>. afterPlus rejects the forms with a leading minus.
func anbIdent(c *selCursor, v string, afterPlus bool) (int, int, bool) {
	a := 1
	if strings.HasPrefix(v, "-") {
		if afterPlus {
			return 0, 0, false
		}
		a = -1
		v = v[1:]
	}
	switch {
	case v == "n":
		return anbTail(c, a)
	case v == "n-":
		return anbSignless(c, a, -1)
	case strings.HasPrefix(v, "n-") && allDigits(v[2:]):
		d, ok := atoiCapped(v[2:])
		if !ok {
			return 0, 0, false
		}
		c.ws()
		return a, -d, c.peek() == nil
	}
	return 0, 0, false
}

// anbTail parses what may follow "An": nothing, a signed integer, or a
// sign and a signless integer.
func anbTail(c *selCursor, a int) (int, int, bool) {
	c.ws()
	t := c.peek()
	if t == nil {
		return a, 0, true
	}
	switch {
	case t.isDelim('+'):
		c.i++
		return anbSignless(c, a, 1)
	case t.isDelim('-'):
		c.i++
		return anbSignless(c, a, -1)
	case t.typ == tNumber && t.integer && (strings.HasPrefix(t.repr, "+") || strings.HasPrefix(t.repr, "-")):
		c.i++
		b, ok := intOf(t.num)
		if !ok {
			return 0, 0, false
		}
		c.ws()
		return a, b, c.peek() == nil
	}
	return 0, 0, false
}

// anbSignless parses a signless integer and applies sign to it.
func anbSignless(c *selCursor, a, sign int) (int, int, bool) {
	c.ws()
	t := c.peek()
	if t == nil || t.typ != tNumber || !t.integer || strings.HasPrefix(t.repr, "+") || strings.HasPrefix(t.repr, "-") {
		return 0, 0, false
	}
	c.i++
	b, ok := intOf(t.num)
	if !ok {
		return 0, 0, false
	}
	c.ws()
	return a, sign * b, c.peek() == nil
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// maxAnB caps the magnitude of A and B: 2^30. Anti-DoS guard, not
// spec-mandated: it keeps the An+B arithmetic from overflowing a 32-bit
// int; no document has a billion siblings.
const maxAnB = 1 << 30

func intOf(v float64) (int, bool) {
	if !numeric.Finite(v) || v > maxAnB || v < -maxAnB {
		return 0, false
	}
	return int(v), true
}

func atoiCapped(s string) (int, bool) {
	n, err := intconv.Atoi(s)
	if err != nil || n > maxAnB {
		return 0, false
	}
	return n, true
}

// nthMatches reports whether the 1-based index i is An+B for some n >= 0.
func nthMatches(a, b, i int) bool {
	if a == 0 {
		return i == b
	}
	d := i - b
	return d%a == 0 && d/a >= 0
}
