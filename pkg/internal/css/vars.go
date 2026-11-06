package css

import "strings"

// maxVarDepth caps the chain of custom properties referring to one
// another on one element: 64. Design systems chain a handful. Anti-DoS
// guard, not spec-mandated: it bounds the resolver's recursion.
const maxVarDepth = 64

// resolveCustom computes the custom properties of an element (CSS
// Variables 1, 2 and 3): the declared ones with their var() references
// substituted, over the inherited ones. A cycle, a reference to an
// undefined property without fallback, or a value past maxVarTokens makes
// the property guaranteed-invalid, which removes it.
func (cs *computeState) resolveCustom(names []string, declared map[string][]token, parent map[string][]token) map[string][]token {
	if !cs.m.charge(len(parent) + len(names)) {
		return parent
	}
	out := make(map[string][]token, len(parent)+len(names))
	for k, v := range parent {
		out[k] = v
	}
	const (
		visiting = 1
		done     = 2
	)
	state := map[string]uint8{}
	var resolve func(name string, depth int) ([]token, bool)
	resolve = func(name string, depth int) ([]token, bool) {
		raw, isDeclared := declared[name]
		if !isDeclared {
			v, ok := parent[name]
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
		if w, ok := wideKeyword(significant(raw)); ok {
			if w == wideInitial {
				delete(out, name)
				return nil, false
			}
			v, ok := parent[name]
			if ok {
				out[name] = v
			} else {
				delete(out, name)
			}
			return v, ok
		}
		if depth > maxVarDepth {
			delete(out, name)
			return nil, false
		}
		budget := maxVarTokens
		v, ok := cs.substitute(raw, func(ref string) ([]token, bool) { return resolve(ref, depth+1) }, &budget, 0)
		if !ok {
			delete(out, name)
			return nil, false
		}
		out[name] = v
		return v, true
	}
	for _, name := range names {
		resolve(name, 0)
	}
	return out
}

// substitute replaces every var() in l (CSS Variables 1, 3): with the
// referenced property's value, else with the fallback, else the whole
// value fails. budget counts the tokens produced.
func (cs *computeState) substitute(l []token, lookup func(string) ([]token, bool), budget *int, depth int) ([]token, bool) {
	if depth > maxNestingDepth {
		return nil, false
	}
	out := make([]token, 0, len(l))
	for _, t := range l {
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
				cs.warns.add(Warning{Code: "css-limit", Message: "a var() substitution grew past the size limit and was dropped"})
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
			cs.warns.add(Warning{Code: "css-limit", Message: "a var() substitution grew past the size limit and was dropped"})
			return nil, false
		}
		out = append(out, t)
	}
	return out, true
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
	if !ok {
		return nil
	}
	c := &declCtx{baseURL: d.base}
	entries, ok := c.expand(d.Property, v)
	for _, w := range c.warns {
		cs.warns.add(w)
	}
	if !ok {
		cs.warns.add(Warning{Code: "invalid-css-value", Message: "a var() value for \"" + d.Property + "\" is invalid after substitution; the property computes as unset"})
		return nil
	}
	return entries
}
