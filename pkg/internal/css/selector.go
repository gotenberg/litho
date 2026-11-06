package css

import (
	"strconv"
	"strings"
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
	// fold compares the value ASCII case-insensitively, the i flag;
	// sensitive compares it exactly, the s flag, even for the HTML
	// attributes whose values compare case-insensitively by default.
	fold, sensitive bool
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
)

// pseudoSel is a pseudo-class with its arguments.
type pseudoSel struct {
	kind pseudoKind
	// a and b are the An+B of the :nth-* pseudo-classes; of is their
	// "of S" list.
	a, b int
	of   []*complexSel
	// args is the argument list of :not(), :is() and :where().
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
	// never marks a compound no element matches: a namespace prefix,
	// which litho does not support.
	never bool
}

// complexSel is a complex selector (Selectors 4, 3.1): compounds from
// left to right and the combinators between them.
type complexSel struct {
	parts []compound
	// combs[i] joins parts[i] and parts[i+1]: ' ', '>', '+' or '~'.
	combs []byte
	spec  uint32
	// pseudoElement names the pseudo-element the selector targets, "" for
	// the element itself.
	pseudoElement string
}

// selectorError says why a selector did not parse; unsupported names a
// valid construct litho does not implement, for a warning.
type selectorError struct {
	unsupported string
}

// spec packs a specificity (Selectors 4, 17): a, b and c each saturate at
// 1023.
func packSpec(a, b, c int) uint32 {
	sat := func(v int) uint32 {
		if v > 1023 {
			return 1023
		}
		return uint32(v) //nolint:gosec // non-negative and at most 1023, checked above
	}
	return sat(a)<<20 | sat(b)<<10 | sat(c)
}

func unpackSpec(s uint32) (a, b, c int) {
	return int(s >> 20 & 1023), int(s >> 10 & 1023), int(s & 1023) // fits int: each masked to 10 bits
}

func addSpec(x, y uint32) uint32 {
	a1, b1, c1 := unpackSpec(x)
	a2, b2, c2 := unpackSpec(y)
	return packSpec(a1+a2, b1+b2, c1+c2)
}

// knownPseudoElements are the pseudo-elements that parse (CSS Pseudo 4).
// Rules targeting ::before, ::after and ::marker give the styles
// [Computed.Pseudo] returns; those targeting the others produce nothing.
var knownPseudoElements = map[string]bool{
	"before": true, "after": true, "first-line": true, "first-letter": true, "marker": true,
	"placeholder": true, "selection": true, "backdrop": true, "file-selector-button": true,
	"target-text": true, "spelling-error": true, "grammar-error": true, "cue": true,
}

// simplePseudos maps the argument-less pseudo-classes to their kind.
var simplePseudos = map[string]pseudoKind{
	"root": psRoot, "scope": psRoot, "first-child": psFirstChild, "last-child": psLastChild,
	"only-child": psOnlyChild, "first-of-type": psFirstOfType, "last-of-type": psLastOfType,
	"only-of-type": psOnlyOfType, "empty": psEmpty, "link": psLink, "any-link": psLink,
	"checked": psChecked, "disabled": psDisabled, "enabled": psEnabled, "defined": psAlways,
	"hover": psNever, "focus": psNever, "active": psNever, "visited": psNever,
	"focus-within": psNever, "focus-visible": psNever, "target": psNever, "target-within": psNever,
}

// parseSelectorList parses a <selector-list> (Selectors 4, 4.1). A
// forgiving list (:is(), :where()) drops the selectors that fail instead of
// failing whole (Selectors 4, 4.3).
func parseSelectorList(list []token, depth int, forgiving bool) ([]*complexSel, *selectorError) {
	var out []*complexSel
	var firstErr *selectorError
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
			if firstErr == nil {
				firstErr = err
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
		case psNot, psIs:
			spec = addSpec(spec, maxSpec(p.args))
		case psNthChild, psNthLastChild:
			spec = addSpec(spec, addSpec(packSpec(0, 1, 0), maxSpec(p.of)))
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
		if t.isDelim('|') {
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
			c.i++
			if nt := c.peek(); nt != nil && nt.isDelim('|') {
				c.i++
				nn := c.peek()
				if nn == nil || nn.typ != tIdent && !nn.isDelim('*') {
					return comp, "", &selectorError{}
				}
				comp.never = true
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
		if pseudoElement != "" && !t.isDelim(':') && t.typ != tColon {
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
			a, ok := parseAttr(t.children)
			if !ok {
				return comp, "", &selectorError{}
			}
			if a.name == "" {
				comp.never = true
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
				if pe == nil || pe.typ != tIdent || pseudoElement != "" {
					return comp, "", &selectorError{}
				}
				name := lower(pe.value)
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

// parseAttr parses the contents of an attribute selector (Selectors 4, 6).
// A namespace prefix yields an empty name: no element matches it.
func parseAttr(list []token) (attrSel, bool) {
	c := &selCursor{l: list}
	c.ws()
	var a attrSel
	t := c.peek()
	if t == nil {
		return a, false
	}
	switch {
	case t.typ == tIdent:
		a.name = lower(t.value)
		c.i++
		if n := c.peek(); n != nil && n.isDelim('|') && c.i+1 < len(list) && list[c.i+1].typ == tIdent {
			c.i += 2
			a.name = ""
		}
	case t.isDelim('*') || t.isDelim('|'):
		c.i++
		if t.isDelim('*') {
			if n := c.peek(); n == nil || !n.isDelim('|') {
				return a, false
			}
			c.i++
		}
		if n := c.peek(); n == nil || n.typ != tIdent {
			return a, false
		}
		c.i++
		a.name = ""
	default:
		return a, false
	}
	c.ws()
	t = c.peek()
	if t == nil {
		return a, true
	}
	if t.typ != tDelim {
		return a, false
	}
	switch t.delim {
	case '=':
		a.op = attrEquals
		c.i++
	case '~', '|', '^', '$', '*':
		c.i++
		if n := c.peek(); n == nil || !n.isDelim('=') {
			return a, false
		}
		c.i++
		a.op = map[rune]attrOp{'~': attrIncludes, '|': attrDash, '^': attrPrefix, '$': attrSuffix, '*': attrSubstring}[t.delim]
	default:
		return a, false
	}
	c.ws()
	v := c.peek()
	if v == nil || (v.typ != tIdent && v.typ != tString) {
		return a, false
	}
	a.value = v.value
	c.i++
	c.ws()
	if f := c.peek(); f != nil {
		switch {
		case f.isIdent("i"):
			a.fold = true
		case f.isIdent("s"):
			a.sensitive = true
		default:
			return a, false
		}
		c.i++
		c.ws()
	}
	return a, c.peek() == nil
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
		case "not", "is", "where", "matches", "-webkit-any", "-moz-any":
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
			for _, s := range list {
				if s.pseudoElement != "" {
					return pseudoSel{}, &selectorError{}
				}
			}
			return pseudoSel{kind: kind, args: list}, nil
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
			var langs []string
			for _, part := range splitCommas(significant(t.children)) {
				if len(part) != 1 || (part[0].typ != tIdent && part[0].typ != tString) {
					return pseudoSel{}, &selectorError{}
				}
				langs = append(langs, lower(part[0].value))
			}
			if len(langs) == 0 {
				return pseudoSel{}, &selectorError{}
			}
			return pseudoSel{kind: psLang, langs: langs}, nil
		}
		return pseudoSel{}, &selectorError{unsupported: ":" + name + "()"}
	}
	return pseudoSel{}, &selectorError{}
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
	if !finite(v) || v > maxAnB || v < -maxAnB {
		return 0, false
	}
	return int(v), true
}

func atoiCapped(s string) (int, bool) {
	n, err := strconv.Atoi(s)
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
