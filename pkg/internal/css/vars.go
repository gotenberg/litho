package css

import (
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/warn"
)

// maxVarDepth caps the chain of custom properties referring to one
// another on one element: 64. Design systems chain a handful. Anti-DoS
// guard, not spec-mandated: it bounds the resolver's recursion.
const maxVarDepth = 64

// resolveCustom computes the custom properties of an element (CSS
// Variables 1, 2 and 3): the declared ones with their var() references
// substituted, over base, the values the element takes undeclared
// ([computeState.registeredBase]). A cycle, a reference to an undefined
// property without fallback, or a value past maxVarTokens makes the
// property guaranteed-invalid, which removes it. parent holds the
// parent's values, which inherit takes.
//
// A registered property (CSS Properties and Values API 1, 2.4) is invalid
// at computed-value time when its value does not match its syntax, and
// then, like one whose substitution fails, computes as unset: its
// parent's value when it inherits, else its initial value. initial is its
// initial value.
func (cs *computeState) resolveCustom(names []string, declared map[string][]token, base, parent map[string][]token) map[string][]token {
	if !cs.m.charge(len(base) + len(names)) {
		return base
	}
	out := make(map[string][]token, len(base)+len(names))
	maps.Copy(out, base)
	const (
		visiting = 1
		done     = 2
	)
	state := map[string]uint8{}
	set := func(name string, v []token, ok bool) ([]token, bool) {
		if ok {
			out[name] = v
		} else {
			delete(out, name)
		}
		return v, ok
	}
	// unset is the value of a property that computes as unset (CSS
	// Cascade 5, 7.3.3): inherited when it inherits, else its initial
	// value, guaranteed-invalid for an unregistered property.
	unset := func(name string) ([]token, bool) {
		reg := cs.r.registered[name]
		if reg == nil || reg.inherits {
			v, ok := parent[name]
			if !ok && reg != nil && reg.initial != nil {
				return set(name, reg.initial, true)
			}
			return set(name, v, ok)
		}
		return set(name, reg.initial, reg.initial != nil)
	}
	// invalid is the value of a property invalid at computed-value time:
	// the guaranteed-invalid value (CSS Variables 1, 3), or unset for a
	// registered property.
	invalid := func(name string) ([]token, bool) {
		if cs.r.registered[name] == nil {
			return set(name, nil, false)
		}
		return unset(name)
	}
	var resolve func(name string, depth int) ([]token, bool)
	resolve = func(name string, depth int) ([]token, bool) {
		raw, isDeclared := declared[name]
		if !isDeclared {
			v, ok := base[name]
			return v, ok
		}
		switch state[name] {
		case visiting:
			return nil, false
		case done:
			v, ok := out[name]
			return v, ok
		}
		state[name] = visiting
		defer func() { state[name] = done }()
		reg := cs.r.registered[name]
		if w, ok := wideKeyword(significant(raw)); ok {
			switch {
			case w == wideInitial && reg != nil:
				return set(name, reg.initial, reg.initial != nil)
			case w == wideInitial:
				return set(name, nil, false)
			case w == wideInherit:
				v, ok := parent[name]
				return set(name, v, ok)
			}
			return unset(name)
		}
		if depth > maxVarDepth {
			return invalid(name)
		}
		budget := maxVarTokens
		v, ok := cs.substitute(raw, func(ref string) ([]token, bool) { return resolve(ref, depth+1) }, &budget, 0)
		if ok && reg != nil && !reg.universal {
			// A registered value computes on its element, its
			// tree-counting functions with it; an unregistered one keeps
			// them for the element that uses it (CSS Values 5, 9).
			v, ok = cs.countSiblings(v, 0)
		}
		if !ok || reg != nil && !reg.matches(significant(v)) {
			return invalid(name)
		}
		return set(name, v, true)
	}
	for _, name := range names {
		resolve(name, 0)
	}
	return out
}

// registeredBase returns the custom properties an element takes before
// its own declarations: its parent's, with each registered property that
// does not inherit at its initial value, and each registered property its
// parent lacks at its initial value too (CSS Properties and Values API 1,
// 2.4). It returns parent itself when no value changes.
func (cs *computeState) registeredBase(parent map[string][]token) map[string][]token {
	if len(cs.r.registered) == 0 {
		return parent
	}
	var out map[string][]token
	for _, name := range cs.r.registeredNames {
		reg := cs.r.registered[name]
		v, has := parent[name]
		want, keep := reg.initial, reg.initial != nil
		if reg.inherits && has {
			continue
		}
		if has == keep && sharedTokens(v, want) {
			continue
		}
		if out == nil {
			if !cs.m.charge(len(parent) + len(cs.r.registeredNames)) {
				return parent
			}
			out = make(map[string][]token, len(parent)+len(cs.r.registeredNames))
			maps.Copy(out, parent)
		}
		if keep {
			out[name] = want
		} else {
			delete(out, name)
		}
	}
	if out == nil {
		return parent
	}
	return out
}

// sharedTokens reports whether a and b are the same slice: a registered
// property's initial value is one slice every element shares.
func sharedTokens(a, b []token) bool {
	return len(a) == len(b) && (len(a) == 0 || &a[0] == &b[0])
}

// substitute replaces every var() in l (CSS Variables 1, 3): with the
// referenced property's value, else with the fallback, else the whole
// value fails; and every attr() and env() (substituteFunc). budget counts
// the tokens produced.
func (cs *computeState) substitute(l []token, lookup func(string) ([]token, bool), budget *int, depth int) ([]token, bool) {
	if depth > maxNestingDepth {
		return nil, false
	}
	out := make([]token, 0, len(l))
	for _, t := range l {
		if t.typ == tFunction && (eqFold(t.value, "attr") || eqFold(t.value, "env")) {
			v, fallback, ok := cs.substituteFunc(&t)
			if !ok {
				return nil, false
			}
			if fallback {
				if v, ok = cs.substitute(v, lookup, budget, depth+1); !ok {
					return nil, false
				}
			}
			*budget -= len(v)
			if *budget < 0 || !cs.m.charge(len(v)) {
				cs.warns.add(Warning{Code: warn.LimitReached, Effect: warn.Approximated, Message: "a var() substitution grew past the size limit and was dropped"})
				return nil, false
			}
			out = append(out, v...)
			continue
		}
		if t.typ == tFunction && urlFunction(t.value) && containsSubstitution(t.children) {
			// CSS Values 5, 7.7.2: a value an attr() gives never becomes
			// a URL, so a document's attributes cannot make litho fetch.
			return nil, false
		}
		if t.typ == tFunction && eqFold(t.value, "var") {
			args := t.children
			i := 0
			for i < len(args) && args[i].typ == tWhitespace {
				i++
			}
			if i >= len(args) || args[i].typ != tIdent || !strings.HasPrefix(args[i].value, "--") {
				return nil, false
			}
			name := args[i].value
			i++
			for i < len(args) && args[i].typ == tWhitespace {
				i++
			}
			var fallback []token
			hasFallback := false
			if i < len(args) {
				if args[i].typ != tComma {
					return nil, false
				}
				fallback, hasFallback = args[i+1:], true
			}
			v, ok := lookup(name)
			if !ok {
				if !hasFallback {
					return nil, false
				}
				if v, ok = cs.substitute(fallback, lookup, budget, depth+1); !ok {
					return nil, false
				}
			}
			*budget -= len(v)
			if *budget < 0 || !cs.m.charge(len(v)) {
				cs.warns.add(Warning{Code: warn.LimitReached, Effect: warn.Approximated, Message: "a var() substitution grew past the size limit and was dropped"})
				return nil, false
			}
			out = append(out, v...)
			continue
		}
		if len(t.children) > 0 {
			kids, ok := cs.substitute(t.children, lookup, budget, depth+1)
			if !ok {
				return nil, false
			}
			t.children = kids
		}
		*budget--
		if *budget < 0 {
			cs.warns.add(Warning{Code: warn.LimitReached, Effect: warn.Approximated, Message: "a var() substitution grew past the size limit and was dropped"})
			return nil, false
		}
		out = append(out, t)
	}
	return out, true
}

// urlFunction reports the functions whose argument is fetched.
func urlFunction(name string) bool {
	switch lower(name) {
	case "url", "src", "image", "image-set", "-webkit-image-set":
		return true
	}
	return false
}

// substituteFunc returns what the attr() or env() t substitutes for, or
// its fallback, which the caller substitutes in turn, false when neither
// is valid: the declaration is then invalid at computed-value time.
func (cs *computeState) substituteFunc(t *token) (v []token, fallback, ok bool) {
	var args, fb []token
	hasFallback := false
	for i, a := range t.children {
		if a.typ == tComma {
			args, fb, hasFallback = t.children[:i], t.children[i+1:], true
			break
		}
	}
	if !hasFallback {
		args = t.children
	}
	args = significant(args)
	var got []token
	var found bool
	if eqFold(t.value, "env") {
		got, found = envValue(args)
	} else {
		got, found = cs.attrValue(args)
	}
	switch {
	case found:
		return got, false, true
	case hasFallback:
		return fb, true, true
	case got != nil:
		return got, false, true
	}
	return nil, false, false
}

// envValue returns the value of an environment variable (CSS Environment
// Variables 1, 3): the safe area insets are zero on paper, as Chromium
// prints them; every other name is unknown and takes the fallback.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 prints env(safe-area-inset-left, 77px)
//     as 0px and env(nonexistent, 66px) as 66px.
func envValue(args []token) ([]token, bool) {
	if len(args) == 0 || args[0].typ != tIdent {
		return nil, false
	}
	switch lower(args[0].value) {
	case "safe-area-inset-top", "safe-area-inset-right", "safe-area-inset-bottom", "safe-area-inset-left",
		"safe-area-max-inset-top", "safe-area-max-inset-right", "safe-area-max-inset-bottom", "safe-area-max-inset-left":
		return []token{{typ: tDimension, num: 0, repr: "0", value: "px"}}, len(args) == 1
	}
	return nil, false
}

// attrValue returns what attr() substitutes for (CSS Values 5, 7.7): the
// attribute of the element as a string without a type, as a number of
// the unit given, or parsed as the type(<syntax>) given. found is false
// for a missing attribute or a value that does not parse as the type;
// without a type a missing attribute still gives the empty string.
func (cs *computeState) attrValue(args []token) (v []token, found bool) {
	if len(args) == 0 || len(args) > 2 || args[0].typ != tIdent || cs.node == nil {
		return nil, false
	}
	val, has := attr(cs.node, args[0].value)
	if len(args) == 1 || args[1].isIdent("raw-string") {
		str := []token{{typ: tString, value: val}}
		if !has {
			return str, false
		}
		return str, true
	}
	if !has {
		return nil, false
	}
	toks, _ := parseComponentValues(val)
	toks = significant(toks)
	typ := args[1]
	if typ.typ == tIdent || typ.typ == tDelim && typ.delim == '%' {
		// A unit: the attribute is a number of it.
		if len(toks) != 1 || toks[0].typ != tNumber {
			return nil, false
		}
		n := toks[0]
		if typ.typ == tDelim {
			return []token{{typ: tPercentage, num: n.num, repr: n.repr}}, true
		}
		unit := lower(typ.value)
		if _, ok := absUnits[unit]; !ok {
			if _, ok := lengthUnits[unit]; !ok {
				if _, ok := otherUnits[unit]; !ok {
					return nil, false
				}
			}
		}
		return []token{{typ: tDimension, num: n.num, repr: n.repr, value: unit}}, true
	}
	if typ.typ != tFunction || !eqFold(typ.value, "type") || len(toks) != 1 {
		return nil, false
	}
	for _, name := range syntaxTypes(typ.children) {
		if matchesType(toks[0], name) {
			return toks, true
		}
	}
	return nil, false
}

// syntaxTypes reads the <syntax> of type(): data type names joined by |,
// each "<name>". It returns nil for any other syntax, which litho does
// not match: a keyword, a multiplier, or the universal syntax, whose
// tokens could reach a URL.
func syntaxTypes(l []token) []string {
	l = significant(l)
	var out []string
	for i := 0; i < len(l); {
		if i+2 >= len(l) || !l[i].isDelim('<') || l[i+1].typ != tIdent || !l[i+2].isDelim('>') {
			return nil
		}
		out = append(out, lower(l[i+1].value))
		i += 3
		if i < len(l) {
			if !l[i].isDelim('|') {
				return nil
			}
			i++
		}
	}
	return out
}

// matchesType reports whether t is a value of the CSS data type name.
func matchesType(t token, name string) bool {
	switch name {
	case "length":
		_, ok := parseLength(t, 0)
		return ok && t.typ != tPercentage
	case "length-percentage":
		_, ok := parseLength(t, allowPct|allowMixed)
		return ok
	case "percentage":
		return t.typ == tPercentage
	case "number":
		_, ok := parseNumber(t)
		return ok
	case "integer":
		_, ok := parseInteger(t)
		return ok
	case "angle":
		_, ok := parseAngle(t)
		return ok && t.typ != tNumber
	case "color":
		_, ok := parseColor(t)
		return ok
	case "custom-ident":
		_, wide := wideKeyword([]token{t})
		return t.typ == tIdent && !wide && !t.isIdent("default")
	case "string":
		return t.typ == tString
	}
	return false
}

// substituteDecl substitutes the var() references of a declaration and
// parses the result (CSS Variables 1, 3.1). It returns nil when the value
// is invalid at computed-value time: its longhands then compute as unset.
func (cs *computeState) substituteDecl(d *Declaration, vars map[string][]token) []entry {
	budget := maxVarTokens
	v, ok := cs.substitute(d.pending, func(name string) ([]token, bool) {
		x, ok := vars[name]
		return x, ok
	}, &budget, 0)
	if ok {
		v, ok = cs.countSiblings(v, 0)
	}
	if !ok {
		return nil
	}
	c := &declCtx{baseURL: d.base, quirks: d.quirks}
	entries, ok := c.expand(d.Property, v)
	for _, w := range c.warns {
		cs.warns.add(w)
	}
	if !ok {
		cs.warns.add(Warning{Code: warn.CSSInvalid, Effect: warn.Approximated, Message: "a var() value for \"" + d.Property + "\" is invalid after substitution; the property computes as unset"})
		return nil
	}
	return entries
}

// treeCounting reports a tree-counting function by its name: sibling-index()
// and sibling-count() (CSS Values 5, 9).
func treeCounting(name string) bool {
	return eqFold(name, "sibling-index") || eqFold(name, "sibling-count")
}

// countSiblings replaces each sibling-index() and sibling-count() in l by
// the integer it stands for on the element being styled (CSS Values 5,
// 9): its 1-based position among the element children of its parent, and
// their number, a pseudo-element's those of its originating element. A
// tree-counting function with arguments makes the value invalid.
func (cs *computeState) countSiblings(l []token, depth int) ([]token, bool) {
	if depth > maxNestingDepth {
		return nil, false
	}
	out := l
	for i := range l {
		t := l[i]
		switch {
		case t.typ == tFunction && treeCounting(t.value):
			if len(significant(t.children)) > 0 || cs.node == nil {
				return nil, false
			}
			info := cs.m.infoOf(cs.node)
			n := info.index
			if eqFold(t.value, "sibling-count") {
				n = info.count
			}
			t = token{typ: tNumber, num: float64(n), repr: strconv.Itoa(n), integer: true}
		case len(t.children) > 0:
			kids, ok := cs.countSiblings(t.children, depth+1)
			if !ok {
				return nil, false
			}
			if &kids[0] == &t.children[0] {
				continue
			}
			t.children = kids
		default:
			continue
		}
		if &out[0] == &l[0] {
			out = slices.Clone(l)
		}
		out[i] = t
	}
	return out, true
}
