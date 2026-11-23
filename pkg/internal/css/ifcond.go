package css

import "strings"

// ifValue returns what the if() function t stands for (CSS Values 5, 12):
// the value of its first branch whose condition is true, its var(),
// attr() and nested if() substituted, and false when no branch is true
// or the function does not parse, which makes the declaration invalid at
// computed-value time. A condition is else, or a boolean expression of
// media(), supports() and style() tests:
//
//   - media() holds a <media-feature> or a <media-condition>, which the
//     printed medium answers; a media type is neither, and so unknown.
//   - supports() holds a declaration or a <supports-condition>, answered
//     as @supports answers it.
//   - style() tests the element's own custom properties, as a container
//     style query tests its container's: --name alone holds when it has
//     a value, --name: value when its value is the same tokens.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 (gotenberg/gotenberg:9.4.1) prints
//     widths of if() as the tests here say: media(width > 100px) true on a
//     600px page, media(print) and media(screen) unknown, not
//     media(screen) false; supports(display: grid) and
//     supports(selector(:has(a))) true; style(--x: 1) by the element's
//     own value, whitespace aside, style(--y) false while --y is unset;
//     a custom property holding if() resolved where it is declared; no
//     true branch, or an empty value, invalid.
func (cs *computeState) ifValue(t *token, lookup func(string) ([]token, bool), budget *int, depth int) ([]token, bool) {
	for _, branch := range splitSemicolons(t.children) {
		branch = trimWS(branch)
		if len(branch) == 0 {
			continue
		}
		colon := -1
		for i, x := range branch {
			if x.typ == tColon {
				colon = i
				break
			}
		}
		if colon < 0 {
			return nil, false
		}
		cond := significant(branch[:colon])
		var v tri
		if len(cond) == 1 && cond[0].isIdent("else") {
			v = triTrue
		} else {
			var ok bool
			v, ok = evalCondition(cond, func(x token) (tri, bool) { return cs.ifTest(x, lookup, budget, depth) })
			if !ok {
				return nil, false
			}
		}
		if v != triTrue {
			continue
		}
		value := trimWS(branch[colon+1:])
		if len(value) == 0 {
			return nil, false
		}
		return cs.substitute(value, lookup, budget, depth+1)
	}
	return nil, false
}

// ifTest evaluates one test of an if() condition, a parenthesised
// condition included.
func (cs *computeState) ifTest(t token, lookup func(string) ([]token, bool), budget *int, depth int) (tri, bool) {
	args := significant(t.children)
	switch {
	case t.typ == tLParen:
		return evalCondition(args, func(x token) (tri, bool) { return cs.ifTest(x, lookup, budget, depth) })
	case t.typ != tFunction:
		return triFalse, false
	case eqFold(t.value, "media"):
		if v, ok := evalCondition(args, func(x token) (tri, bool) { return evalFeature(x, cs.r.media) }); ok {
			return v, true
		}
		if v, ok := evalFeature(token{typ: tLParen, children: t.children}, cs.r.media); ok {
			return v, true
		}
		return triUnknown, true
	case eqFold(t.value, "supports"):
		if len(args) > 1 && args[0].typ == tIdent && args[1].typ == tColon {
			v, ok := supportsLeaf(token{typ: tLParen, children: t.children}, nil, false)
			return triOf(v && ok), true
		}
		return triOf(evalSupports(t.children, nil, false)), true
	case eqFold(t.value, "style"):
		return cs.styleTest(args, lookup, budget, depth), true
	}
	return triUnknown, true
}

// styleTest evaluates the argument of style() in an if() condition: a
// custom property, its value or a condition of them.
func (cs *computeState) styleTest(args []token, lookup func(string) ([]token, bool), budget *int, depth int) tri {
	if len(args) == 0 {
		return triFalse
	}
	if args[0].typ != tIdent || !strings.HasPrefix(args[0].value, "--") {
		v, ok := evalCondition(args, func(x token) (tri, bool) {
			if x.typ != tLParen {
				return triFalse, false
			}
			return cs.styleTest(significant(x.children), lookup, budget, depth), true
		})
		if !ok {
			return triFalse
		}
		return v
	}
	got, set := lookup(args[0].value)
	if len(args) == 1 {
		return triOf(set)
	}
	if args[1].typ != tColon {
		return triFalse
	}
	want, ok := cs.substitute(trimWS(args[2:]), lookup, budget, depth+1)
	return triOf(ok && set && sameTokens(significant(got), significant(want)))
}

// splitSemicolons splits l at its top-level semicolons.
func splitSemicolons(l []token) [][]token {
	var out [][]token
	start := 0
	for i, t := range l {
		if t.typ == tSemicolon {
			out = append(out, l[start:i])
			start = i + 1
		}
	}
	return append(out, l[start:])
}
