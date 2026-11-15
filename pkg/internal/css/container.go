package css

import (
	"math"
	"slices"

	"golang.org/x/net/html"
)

// ContainerType is the container-type property (CSS Conditional 5, 5.1):
// the queries an element answers besides style queries, which every
// element answers. Zero is normal.
type ContainerType uint8

// The container-type bits.
const (
	// ContainerSize answers size queries on both axes. It applies size
	// and style containment and makes an independent formatting context.
	ContainerSize ContainerType = 1 << iota
	// ContainerInlineSize answers size queries on the container's own
	// inline axis. It applies inline-size and style containment and makes
	// an independent formatting context.
	ContainerInlineSize
	// ContainerScrollState answers scroll-state queries.
	ContainerScrollState
)

// SizeContainer reports a container for size queries: container-type
// size or inline-size (CSS Conditional 5, 5.1). Such a box establishes an
// independent formatting context.
func (s *Style) SizeContainer() bool {
	return s.ContainerType&(ContainerSize|ContainerInlineSize) != 0
}

// Containment returns the containment the box is under: its contain
// property's, with the size and style containment its container-type
// applies (CSS Conditional 5, 5.1). Style containment is not layout
// containment: a container is no containing block for its positioned
// descendants.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 places an
//     absolutely positioned and a fixed child of a container-type:
//     inline-size box at the page's corner, sizes a floated, an
//     inline-block and a flex item container of that type to zero width,
//     a container-type: size block of auto height to zero height, and
//     keeps a float inside such a container.
func (s *Style) Containment() Containment {
	c := s.Contain
	switch {
	case s.ContainerType&ContainerSize != 0:
		c |= ContainSize | ContainStyle
	case s.ContainerType&ContainerInlineSize != 0:
		if c&ContainSize == 0 {
			c |= ContainInlineSize
		}
		c |= ContainStyle
	}
	return c
}

// containerProps are the properties of CSS Conditional 5, 5.1 and 5.2.
func containerProps() map[propID]propDef {
	return map[propID]propDef{
		pContainerType: {name: "container-type", parse: parseContainerType, compute: func(_ *styler, st *Style, v any) {
			if t, ok := v.(ContainerType); ok {
				st.ContainerType = t
			}
		}, copyFrom: field(func(s *Style) *ContainerType { return &s.ContainerType })},
		pContainerName: {name: "container-name", parse: parseContainerName, compute: func(_ *styler, st *Style, v any) {
			if n, ok := v.([]string); ok {
				st.ContainerNames = n
			}
		}, copyFrom: field(func(s *Style) *[]string { return &s.ContainerNames })},
	}
}

// parseContainerType parses normal | [ [ size | inline-size ] ||
// scroll-state ] (CSS Conditional 5, 5.1).
func parseContainerType(v []token, _ *declCtx) (any, bool) {
	if k, ok := keyword(v); ok && k == "normal" {
		return ContainerType(0), true
	}
	if len(v) == 0 || len(v) > 2 {
		return nil, false
	}
	var t ContainerType
	for _, x := range v {
		if x.typ != tIdent {
			return nil, false
		}
		var bit ContainerType
		switch lower(x.value) {
		case "size":
			bit = ContainerSize
		case "inline-size":
			bit = ContainerInlineSize
		case "scroll-state":
			bit = ContainerScrollState
		default:
			return nil, false
		}
		if t&bit != 0 || bit != ContainerScrollState && t&(ContainerSize|ContainerInlineSize) != 0 {
			return nil, false
		}
		t |= bit
	}
	return t, true
}

// parseContainerName parses none | <custom-ident>+, the keywords none,
// and, not and or excluded (CSS Conditional 5, 5.2). nil is none; the
// names keep their case, as they match case-sensitively.
func parseContainerName(v []token, _ *declCtx) (any, bool) {
	if k, ok := keyword(v); ok && k == "none" {
		return []string(nil), true
	}
	if len(v) == 0 {
		return nil, false
	}
	out := make([]string, 0, len(v))
	for _, x := range v {
		if x.typ != tIdent || !containerName(x.value) {
			return nil, false
		}
		out = append(out, x.value)
	}
	return out, true
}

// containerName reports an ident valid as a <container-name>: a
// <custom-ident> other than none, and, not and or (CSS Conditional 5, 5.2
// and 5.4).
func containerName(s string) bool {
	switch lower(s) {
	case "none", "and", "not", "or":
		return false
	}
	return customIdent(s)
}

// expandContainer expands the container shorthand: <'container-name'> [
// / <'container-type'> ]?, an omitted type resetting to normal (CSS
// Conditional 5, 5.3).
func expandContainer(v []token, c *declCtx) ([]entry, bool) {
	name, typ := v, []token(nil)
	if i := slices.IndexFunc(v, func(t token) bool { return t.isDelim('/') }); i >= 0 {
		name, typ = v[:i], v[i+1:]
		if len(typ) == 0 {
			return nil, false
		}
	}
	n, ok := parseContainerName(name, c)
	if !ok {
		return nil, false
	}
	t := any(ContainerType(0))
	if typ != nil {
		if t, ok = parseContainerType(typ, c); !ok {
			return nil, false
		}
	}
	return []entry{{id: pContainerName, v: n}, {id: pContainerType, v: t}}, true
}

// containerUnits are the sizes in points the container query units of
// one element resolve against: the width, height, inline size and block
// size of the nearest container answering each, the small viewport's
// where none does (CSS Conditional 5, 7).
type containerUnits struct {
	w, h, i, b float64
}

// tri is the value of a query in three-valued logic (Media Queries 4,
// 3.2): true, false or unknown.
type tri int8

// The query values.
const (
	triFalse tri = iota - 1
	triUnknown
	triTrue
)

func triOf(b bool) tri {
	if b {
		return triTrue
	}
	return triFalse
}

func (t tri) not() tri { return -t }

// containerCond is the condition of an @container rule, chained to the
// enclosing one: a rule applies when every condition of its chain is true
// for the element (CSS Conditional 5, 5.4).
type containerCond struct {
	// conds are the comma-separated <container-condition>s, true when
	// any is.
	conds  []cqCondition
	parent *containerCond
}

// cqCondition is one <container-condition>: an optional name and an
// optional query, at least one of the two.
type cqCondition struct {
	name  string
	query *cqNode
	// needs are the query types a container must answer for every
	// feature of the query; unsupported marks a query with a feature
	// litho does not know, which selects no container.
	needs       cqNeeds
	unsupported bool
}

// cqNeeds are the axes and query types a query's features ask of the
// container it selects.
type cqNeeds struct {
	width, height, inline, block, scroll bool
}

// cqOp is the kind of a node of a container query.
type cqOp uint8

// The node kinds.
const (
	cqNot cqOp = iota
	cqAnd
	cqOr
	cqSize
	cqStyle
	cqScroll
	cqUnknown
)

// cqNode is a node of a container query: a boolean operation of its
// kids, or one feature.
type cqNode struct {
	op   cqOp
	kids []*cqNode
	// name is the feature's lowercased name, or the custom property a
	// style feature queries.
	name string
	// parts compare the feature with values: name op value each. None
	// evaluates the feature in a boolean context.
	parts []cqPart
	// value is the tokens of a plain style feature, plainValue reporting
	// one.
	value      []token
	plainValue bool
}

// cqPart is one comparison of a feature: feature op value. A style
// range's value may name a custom property, in name.
type cqPart struct {
	op    cqCmp
	value token
	name  string
}

// cqCmp is a comparison operator of a range feature.
type cqCmp uint8

// The comparisons, feature op value.
const (
	cmpEq cqCmp = iota
	cmpLt
	cmpLe
	cmpGt
	cmpGe
)

// flip returns the comparison of value op feature as feature op value.
func (c cqCmp) flip() cqCmp {
	switch c {
	case cmpLt:
		return cmpGt
	case cmpLe:
		return cmpGe
	case cmpGt:
		return cmpLt
	case cmpGe:
		return cmpLe
	case cmpEq:
	}
	return c
}

// test compares a with b.
func (c cqCmp) test(a, b float64) bool {
	switch c {
	case cmpLt:
		return a < b
	case cmpLe:
		return a <= b
	case cmpGt:
		return a > b
	case cmpGe:
		return a >= b
	case cmpEq:
	}
	return a == b
}

// maxContainerQueryNodes caps the nodes of one container query: 256.
// Anti-DoS guard, not spec-mandated: a query is evaluated for every
// element its rules' selectors match, and real queries hold a handful.
const maxContainerQueryNodes = 256

// parseContainerPrelude parses the prelude of an @container rule,
// <container-condition>#, where <container-condition> = [
// <container-name>? <container-query>? ]! (CSS Conditional 5, 5.4). It
// returns false for a prelude that does not match, which drops the rule.
func parseContainerPrelude(prelude []token) ([]cqCondition, bool) {
	parts := splitCommas(trimWS(prelude))
	if len(parts) == 0 {
		return nil, false
	}
	out := make([]cqCondition, 0, len(parts))
	count := 0
	for _, part := range parts {
		l := significant(part)
		if len(l) == 0 {
			return nil, false
		}
		var c cqCondition
		if l[0].typ == tIdent && !l[0].isIdent("not") {
			if !containerName(l[0].value) {
				return nil, false
			}
			c.name = l[0].value
			l = l[1:]
		}
		if len(l) > 0 {
			q, ok := parseCQ(l, &c, &count)
			if !ok {
				return nil, false
			}
			c.query = q
		}
		out = append(out, c)
	}
	return out, true
}

// parseCQ parses a <container-query>: not <query-in-parens>, or
// <query-in-parens> joined by and or by or.
func parseCQ(l []token, c *cqCondition, count *int) (*cqNode, bool) {
	return parseBoolean(l, count, func(t token) (*cqNode, bool) { return parseQueryInParens(t, c, count) })
}

// parseBoolean parses the boolean shape container, style and
// scroll-state queries share: not <in>, or <in> [and <in>]*, or <in> [or
// <in>]*, each <in> parsed by in.
func parseBoolean(l []token, count *int, in func(token) (*cqNode, bool)) (*cqNode, bool) {
	*count++
	if len(l) == 0 || *count > maxContainerQueryNodes {
		return nil, false
	}
	if l[0].isIdent("not") {
		if len(l) != 2 {
			return nil, false
		}
		k, ok := in(l[1])
		if !ok {
			return nil, false
		}
		return &cqNode{op: cqNot, kids: []*cqNode{k}}, true
	}
	first, ok := in(l[0])
	if !ok {
		return nil, false
	}
	if len(l) == 1 {
		return first, true
	}
	n := &cqNode{kids: []*cqNode{first}}
	for i := 1; i < len(l); i += 2 {
		if l[i].typ != tIdent || i+1 >= len(l) {
			return nil, false
		}
		var op cqOp
		switch lower(l[i].value) {
		case "and":
			op = cqAnd
		case "or":
			op = cqOr
		default:
			return nil, false
		}
		if i > 1 && op != n.op {
			return nil, false
		}
		n.op = op
		k, ok := in(l[i+1])
		if !ok {
			return nil, false
		}
		n.kids = append(n.kids, k)
	}
	return n, true
}

// parseQueryInParens parses a <query-in-parens>: a parenthesised
// container query or size feature, style(), scroll-state(), or any other
// function or parenthesised block, <general-enclosed>, which is unknown
// and selects no container.
func parseQueryInParens(t token, c *cqCondition, count *int) (*cqNode, bool) {
	switch t.typ { //nolint:exhaustive // the other token types are invalid here
	case tLParen:
		inner := significant(t.children)
		if len(inner) > 0 && (inner[0].typ == tLParen || inner[0].typ == tFunction || inner[0].isIdent("not")) {
			if n, ok := parseCQ(inner, c, count); ok {
				return n, true
			}
		}
		if n, ok := parseSizeFeature(inner, c); ok {
			return n, true
		}
	case tFunction:
		switch lower(t.value) {
		case "style":
			if n, ok := parseStyleQuery(significant(t.children), count); ok {
				return n, true
			}
		case "scroll-state":
			if n, ok := parseScrollStateQuery(significant(t.children), c, count); ok {
				return n, true
			}
		}
	default:
		return nil, false
	}
	c.unsupported = true
	return &cqNode{op: cqUnknown}, true
}

// sizeFeatures are the size container features (CSS Conditional 5,
// 6.1), with whether each is a range one.
var sizeFeatures = map[string]bool{
	"width": true, "height": true, "inline-size": true, "block-size": true, "aspect-ratio": true, "orientation": false,
}

// parseSizeFeature parses a <size-feature> as Media Queries 4, 2.4 has
// them: a name alone, name: value with an optional min- or max- prefix
// on a range feature, or the range forms.
func parseSizeFeature(in []token, c *cqCondition) (*cqNode, bool) {
	if len(in) == 0 {
		return nil, false
	}
	if in[0].typ == tIdent && (len(in) == 1 || in[1].typ == tColon) {
		name := lower(in[0].value)
		op := cmpEq
		base := name
		switch {
		case len(name) > 4 && name[:4] == "min-":
			op, base = cmpGe, name[4:]
		case len(name) > 4 && name[:4] == "max-":
			op, base = cmpLe, name[4:]
		}
		isRange, known := sizeFeatures[base]
		if !known || base != name && !isRange {
			return nil, false
		}
		if len(in) == 1 {
			if base != name {
				return nil, false
			}
			c.needs.add(base)
			return &cqNode{op: cqSize, name: base}, true
		}
		v, ok := featureValue(in[2:])
		if !ok {
			return nil, false
		}
		c.needs.add(base)
		return &cqNode{op: cqSize, name: base, parts: []cqPart{{op: op, value: v}}}, true
	}
	return parseSizeRange(in, c)
}

// featureValue returns the one value of a feature: a ratio becomes one
// number token.
func featureValue(l []token) (token, bool) {
	if len(l) == 1 {
		return l[0], true
	}
	if r, ok := mediaRatio(l); ok {
		return token{typ: tNumber, num: r}, true
	}
	return token{}, false
}

// readCmp reads a comparison operator at l[i]: <, <=, >, >= or =.
func readCmp(l []token, i int) (cqCmp, int, bool) {
	if i >= len(l) || l[i].typ != tDelim {
		return 0, i, false
	}
	var op cqCmp
	switch l[i].delim {
	case '=':
		return cmpEq, i + 1, true
	case '<':
		op = cmpLt
	case '>':
		op = cmpGt
	default:
		return 0, i, false
	}
	if i+1 < len(l) && l[i+1].isDelim('=') {
		op++
		return op, i + 2, true
	}
	return op, i + 1, true
}

// rangeOperands splits the range form of a feature into its operands and
// operators: a op b, or a op b op c, each operand one value or a ratio.
func rangeOperands(in []token) ([][]token, []cqCmp, bool) {
	var operands [][]token
	var ops []cqCmp
	start := 0
	for i := 0; i < len(in); {
		if in[i].typ != tDelim || !in[i].isDelim('<') && !in[i].isDelim('>') && !in[i].isDelim('=') {
			i++
			continue
		}
		op, j, ok := readCmp(in, i)
		if !ok || i == start {
			return nil, nil, false
		}
		operands = append(operands, in[start:i])
		ops = append(ops, op)
		start, i = j, j
	}
	if start >= len(in) || len(ops) == 0 || len(ops) > 2 {
		return nil, nil, false
	}
	operands = append(operands, in[start:])
	if len(ops) == 2 {
		lt := ops[0] == cmpLt || ops[0] == cmpLe
		if ops[0] == cmpEq || ops[1] == cmpEq || lt != (ops[1] == cmpLt || ops[1] == cmpLe) {
			return nil, nil, false
		}
	}
	return operands, ops, true
}

// parseSizeRange parses the range form of a size feature: name op value,
// value op name, or value op name op value.
func parseSizeRange(in []token, c *cqCondition) (*cqNode, bool) {
	operands, ops, ok := rangeOperands(in)
	if !ok {
		return nil, false
	}
	nameAt := -1
	for i, o := range operands {
		if len(o) == 1 && o[0].typ == tIdent {
			if _, known := sizeFeatures[lower(o[0].value)]; known {
				nameAt = i
			}
		}
	}
	if nameAt < 0 || len(operands) == 3 && nameAt != 1 {
		return nil, false
	}
	name := lower(operands[nameAt][0].value)
	if !sizeFeatures[name] {
		return nil, false
	}
	n := &cqNode{op: cqSize, name: name}
	for i, o := range operands {
		if i == nameAt {
			continue
		}
		v, ok := featureValue(o)
		if !ok {
			return nil, false
		}
		if i < nameAt {
			n.parts = append(n.parts, cqPart{op: ops[i].flip(), value: v})
		} else {
			n.parts = append(n.parts, cqPart{op: ops[i-1], value: v})
		}
	}
	c.needs.add(name)
	return n, true
}

// add records what the size feature name asks of a container.
func (n *cqNeeds) add(name string) {
	switch name {
	case "width":
		n.width = true
	case "height":
		n.height = true
	case "inline-size":
		n.inline = true
	case "block-size":
		n.block = true
	default:
		n.width, n.height = true, true
	}
}

// parseStyleQuery parses a <style-query> (CSS Conditional 5, 5.4): one
// feature, or parenthesised style queries and features under not, joined
// by and, or joined by or.
func parseStyleQuery(l []token, count *int) (*cqNode, bool) {
	if len(l) == 0 {
		return nil, false
	}
	if n, ok := parseStyleFeature(l); ok {
		return n, true
	}
	return parseBoolean(l, count, func(t token) (*cqNode, bool) {
		if t.typ != tLParen {
			if t.typ == tFunction {
				return &cqNode{op: cqUnknown}, true
			}
			return nil, false
		}
		inner := significant(t.children)
		if n, ok := parseStyleFeature(inner); ok {
			return n, true
		}
		if n, ok := parseStyleQuery(inner, count); ok {
			return n, true
		}
		return &cqNode{op: cqUnknown}, true
	})
}

// parseStyleFeature parses a <style-feature>: a declaration, a property
// name alone, or a range comparing values and custom property names. A
// feature naming a standard property is unknown: Chromium answers style
// queries of custom properties alone.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 applies none of
//     @container style(color: rgb(255,0,0)) and style(display: block) on
//     elements whose container matches either, and applies style(--n >
//     3) where --n is 5.
func parseStyleFeature(l []token) (*cqNode, bool) {
	if len(l) == 0 {
		return nil, false
	}
	if l[0].typ == tIdent && (len(l) == 1 || l[1].typ == tColon) {
		name := l[0].value
		if len(name) < 3 || name[:2] != "--" {
			if len(l) == 1 || knownProperty(lower(name)) {
				return &cqNode{op: cqUnknown}, true
			}
			return nil, false
		}
		if len(l) == 1 {
			return &cqNode{op: cqStyle, name: name}, true
		}
		v := trimWS(l[2:])
		for _, t := range v {
			if t.typ == tDelim && (t.delim == '<' || t.delim == '>' || t.delim == '=') {
				return nil, false
			}
		}
		if !declarationValue(v) {
			return nil, false
		}
		return &cqNode{op: cqStyle, name: name, value: v, plainValue: true}, true
	}
	operands, ops, ok := rangeOperands(l)
	if !ok {
		return nil, false
	}
	n := &cqNode{op: cqStyle}
	for i, o := range operands {
		if len(o) != 1 {
			return nil, false
		}
		p := cqPart{value: o[0]}
		if o[0].typ == tIdent && len(o[0].value) > 2 && o[0].value[:2] == "--" {
			p.name = o[0].value
		}
		if i > 0 {
			p.op = ops[i-1]
		}
		n.parts = append(n.parts, p)
	}
	return n, true
}

// scrollStateFeatures are the scroll-state container features (CSS
// Conditional 5, 6.3) with the value each has on a printed page, where
// nothing scrolls, sticks or snaps: none.
var scrollStateFeatures = map[string]bool{"stuck": true, "snapped": true, "scrollable": true, "scrolled": true}

// parseScrollStateQuery parses a <scroll-state-query>.
func parseScrollStateQuery(l []token, c *cqCondition, count *int) (*cqNode, bool) {
	feature := func(in []token) (*cqNode, bool) {
		if len(in) == 0 || in[0].typ != tIdent || !scrollStateFeatures[lower(in[0].value)] {
			return nil, false
		}
		switch {
		case len(in) == 1:
			c.needs.scroll = true
			return &cqNode{op: cqScroll, name: lower(in[0].value)}, true
		case len(in) == 3 && in[1].typ == tColon && in[2].typ == tIdent:
			c.needs.scroll = true
			return &cqNode{op: cqScroll, name: lower(in[0].value), value: in[2:], plainValue: true}, true
		}
		return nil, false
	}
	if n, ok := feature(l); ok {
		return n, true
	}
	return parseBoolean(l, count, func(t token) (*cqNode, bool) {
		if t.typ != tLParen {
			if t.typ == tFunction {
				return &cqNode{op: cqUnknown}, true
			}
			return nil, false
		}
		inner := significant(t.children)
		if n, ok := feature(inner); ok {
			return n, true
		}
		if n, ok := parseScrollStateQuery(inner, c, count); ok {
			return n, true
		}
		return &cqNode{op: cqUnknown}, true
	})
}

// containerHost is a query container an element's query selected: the
// element, its computed style and custom properties, and its size when
// the layout measured it.
type containerHost struct {
	node *html.Node
	st   *Style
	vars map[string][]token
}

// matchesContainers reports whether every condition of the chain c is
// true for the element n, or for a pseudo-element of n when pseudo is
// set: a pseudo-element queries the inclusive ancestors of its
// originating element, an element its ancestors (CSS Conditional 5, 5).
func (cs *computeState) matchesContainers(c *containerCond, n *html.Node, pseudo bool) bool {
	// The rules of one @container block share its condition: it is
	// evaluated once an element.
	if cs.cqMemoAt != cs.elem || cs.cqMemo == nil {
		if cs.cqMemo == nil {
			cs.cqMemo = map[*containerCond]bool{}
		}
		clear(cs.cqMemo)
		cs.cqMemoAt = cs.elem
	}
	if v, ok := cs.cqMemo[c]; ok {
		return v
	}
	v := cs.chainTrue(c, n, pseudo)
	cs.cqMemo[c] = v
	return v
}

// chainTrue reports whether every condition of the chain c is true for n.
func (cs *computeState) chainTrue(c *containerCond, n *html.Node, pseudo bool) bool {
	for ; c != nil; c = c.parent {
		v := triFalse
		for i := range c.conds {
			if r := cs.evalCondition(&c.conds[i], n, pseudo); r > v {
				v = r
			}
		}
		if v != triTrue {
			return false
		}
	}
	return true
}

// evalCondition evaluates one <container-condition> for n: it selects the
// nearest ancestor whose name matches and that answers every feature of
// the query, and evaluates the query against it; unknown when there is
// none (CSS Conditional 5, 5.4).
func (cs *computeState) evalCondition(c *cqCondition, n *html.Node, pseudo bool) tri {
	if c.unsupported {
		return triUnknown
	}
	a := n
	if !pseudo {
		a = parentElement(n)
	}
	for ; a != nil; a = parentElement(a) {
		st := cs.out[a]
		if st == nil || !cs.m.charge(1) {
			return triUnknown
		}
		if c.name != "" && !slices.Contains(st.ContainerNames, c.name) {
			continue
		}
		if !answers(st, c.needs) {
			continue
		}
		if c.query == nil {
			return triTrue
		}
		h := containerHost{node: a, st: st, vars: cs.vars[a]}
		return cs.evalQuery(c.query, &h)
	}
	return triUnknown
}

// answers reports whether a container of style st answers the features
// of needs: a size container in the axes it contains, its own inline axis
// for inline-size (CSS Conditional 5, 5.1 and 6.1), a scroll-state
// container for scroll-state features. Every element answers style
// features.
func answers(st *Style, needs cqNeeds) bool {
	if needs.scroll && st.ContainerType&ContainerScrollState == 0 {
		return false
	}
	if !needs.width && !needs.height && !needs.inline && !needs.block {
		return true
	}
	if st.ContainerType&ContainerSize != 0 {
		return true
	}
	if st.ContainerType&ContainerInlineSize == 0 || needs.block {
		return false
	}
	vertical := st.WritingMode != HorizontalTB
	return (!needs.width || !vertical) && (!needs.height || vertical)
}

// evalQuery evaluates the query node q against the container h.
func (cs *computeState) evalQuery(q *cqNode, h *containerHost) tri {
	switch q.op {
	case cqNot:
		return cs.evalQuery(q.kids[0], h).not()
	case cqAnd:
		v := triTrue
		for _, k := range q.kids {
			v = min(v, cs.evalQuery(k, h))
		}
		return v
	case cqOr:
		v := triFalse
		for _, k := range q.kids {
			v = max(v, cs.evalQuery(k, h))
		}
		return v
	case cqSize:
		return cs.evalSize(q, h)
	case cqStyle:
		return cs.evalStyle(q, h)
	case cqScroll:
		// Nothing scrolls, sticks or snaps on a printed page: each
		// feature is none.
		if !q.plainValue {
			return triFalse
		}
		return triOf(q.value[0].isIdent("none"))
	case cqUnknown:
	}
	return triUnknown
}

// containerSize returns the content box size in points the layout
// measured for the container h, and records that a style depends on it.
func (cs *computeState) containerSize(h *containerHost) (float64, float64, bool) {
	cs.queried = true
	if cs.opts.ContainerSize == nil || h.st.Display == DisplayNone || h.st.Display == Contents {
		return 0, 0, false
	}
	return cs.opts.ContainerSize(h.node)
}

// layoutUnit returns v points in whole 64ths of a CSS pixel, the unit
// Chromium lays boxes out and compares container sizes in.
func layoutUnit(v float64) float64 {
	return math.Round(v / Pt * 64)
}

// evalSize evaluates a size feature against the container h (CSS
// Conditional 5, 6.1): its content box, unknown before the layout
// measured it. Lengths in the query resolve against the container's
// font (6.1).
func (cs *computeState) evalSize(q *cqNode, h *containerHost) tri {
	w, ht, ok := cs.containerSize(h)
	if !ok {
		return triUnknown
	}
	inline, block := w, ht
	if h.st.WritingMode != HorizontalTB {
		inline, block = ht, w
	}
	var got float64
	switch q.name {
	case "width":
		got = w
	case "height":
		got = ht
	case "inline-size":
		got = inline
	case "block-size":
		got = block
	case "aspect-ratio":
		if ht <= 0 {
			return triUnknown
		}
		r := w / ht
		if len(q.parts) == 0 {
			return triOf(r != 0)
		}
		for _, p := range q.parts {
			want, ok := ratioValue(p.value)
			if !ok {
				return triUnknown
			}
			if !p.op.test(r, want) {
				return triFalse
			}
		}
		return triTrue
	case "orientation":
		if len(q.parts) == 0 {
			return triTrue
		}
		portrait := ht >= w
		v := q.parts[0].value
		switch {
		case v.isIdent("portrait"):
			return triOf(portrait)
		case v.isIdent("landscape"):
			return triOf(!portrait)
		}
		return triUnknown
	}
	if len(q.parts) == 0 {
		return triOf(layoutUnit(got) != 0)
	}
	lc := cs.queryLenCtx(h.st)
	for _, p := range q.parts {
		l, ok := parseLength(p.value, 0)
		if !ok {
			return triUnknown
		}
		if !p.op.test(layoutUnit(got), layoutUnit(l.points(lc))) {
			return triFalse
		}
	}
	return triTrue
}

// ratioValue reads a <ratio> feature value, already one number.
func ratioValue(t token) (float64, bool) {
	if t.typ != tNumber || t.num < 0 {
		return 0, false
	}
	return t.num, true
}

// queryLenCtx returns the context the lengths of a query against the
// container of style st resolve in: its font, the root's, the viewport.
func (cs *computeState) queryLenCtx(st *Style) lenCtx {
	vw, vh := cs.r.media.viewport()
	lc := lenCtx{fontSize: st.FontSize, rootFontSize: mediumFontSize, width: vw, height: vh}
	if cs.root != nil {
		lc.rootFontSize = cs.root.FontSize
	}
	lc.lineHeight, lc.rootLineHeight = usedLineHeight(st), lc.lineHeight
	return lc
}

// evalStyle evaluates a style feature against the container h (CSS
// Conditional 5, 6.2): a custom property's computed value against the
// value given, as tokens; a name alone is true when the property has a
// value other than its initial, the guaranteed-invalid value; a range
// compares numbers, percentages or dimensions of one type.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 applies
//     style(--n: 5) and not style(--n: 5.0) where --n is 5, style(--x:
//     initial) where --x is not set, and style(--theme: dark) to an
//     element whose parent inherits --theme: dark.
func (cs *computeState) evalStyle(q *cqNode, h *containerHost) tri {
	if len(q.parts) > 0 {
		return cs.evalStyleRange(q, h)
	}
	got, set := h.vars[q.name]
	if !q.plainValue {
		return triOf(set)
	}
	if len(q.value) == 1 && q.value[0].typ == tIdent {
		switch lower(q.value[0].value) {
		case "initial":
			return triOf(!set)
		case "revert", "revert-layer":
			return triFalse
		case "inherit", "unset":
			p := parentElement(h.node)
			want, pset := cs.vars[p][q.name]
			if p == nil {
				pset = false
			}
			return triOf(set == pset && sameTokens(trimWS(got), trimWS(want)))
		}
	}
	return triOf(set && sameTokens(trimWS(got), q.value))
}

// evalStyleRange evaluates a style range: each value, a custom property
// substituted, must be one number, percentage or dimension, all of one
// type, else the range is false (CSS Conditional 5, 6.2).
func (cs *computeState) evalStyleRange(q *cqNode, h *containerHost) tri {
	vals := make([]float64, len(q.parts))
	kind := ""
	lc := cs.queryLenCtx(h.st)
	for i, p := range q.parts {
		t := p.value
		if p.name != "" {
			v := significant(h.vars[p.name])
			if len(v) != 1 {
				return triFalse
			}
			t = v[0]
		}
		k, v, ok := rangeNumber(t, lc)
		if !ok || kind != "" && k != kind {
			return triFalse
		}
		kind, vals[i] = k, v
	}
	for i := 1; i < len(vals); i++ {
		if !q.parts[i].op.test(vals[i-1], vals[i]) {
			return triFalse
		}
	}
	return triTrue
}

// rangeNumber reads one value of a style range: its type and its value in
// the type's canonical unit, a length in points.
func rangeNumber(t token, lc lenCtx) (string, float64, bool) {
	switch t.typ { //nolint:exhaustive // the other token types are no range value
	case tNumber:
		return "number", t.num, true
	case tPercentage:
		return "percentage", t.num, true
	case tDimension:
		if l, ok := parseLength(t, 0); ok {
			return "length", l.points(lc), true
		}
		if o, ok := otherUnits[lower(t.value)]; ok {
			k := "angle"
			switch o.t {
			case typeTime:
				k = "time"
			case typeFreq:
				k = "frequency"
			case typeRes:
				k = "resolution"
			}
			return k, t.num * o.k, true
		}
	}
	return "", 0, false
}

// sameTokens reports whether two token lists are the same values, a run
// of whitespace equal to any other.
func sameTokens(a, b []token) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		x, y := &a[i], &b[i]
		if x.typ != y.typ {
			return false
		}
		if x.typ == tWhitespace {
			continue
		}
		if x.value != y.value || x.repr != y.repr || x.delim != y.delim || x.typ == tNumber && x.repr == "" && x.num != y.num || !sameTokens(x.children, y.children) {
			return false
		}
	}
	return true
}

// containerUnitsOf returns the sizes the container query units of the
// element n, or of a pseudo-element of it, resolve against: for each axis
// the nearest container answering size queries on it, the small viewport
// where none does or the layout has not measured it (CSS Conditional 5,
// 7). It returns nil when no ancestor is a size container.
func (cs *computeState) containerUnitsOf(n *html.Node, pseudo bool) *containerUnits {
	if cs.sizeContainers == 0 {
		return nil
	}
	vw, vh := cs.r.media.viewport()
	vi, vb := vw, vh
	if cs.root != nil && cs.root.WritingMode != HorizontalTB {
		vi, vb = vh, vw
	}
	u := containerUnits{w: vw, h: vh, i: vi, b: vb}
	var haveW, haveH, haveI, haveB bool
	found := false
	a := n
	if !pseudo {
		a = parentElement(n)
	}
	for ; a != nil && (!haveW || !haveH || !haveI || !haveB) && cs.m.charge(1); a = parentElement(a) {
		st := cs.out[a]
		if st == nil || !st.SizeContainer() {
			continue
		}
		found = true
		h := containerHost{node: a, st: st}
		w, ht, ok := cs.containerSize(&h)
		vertical := st.WritingMode != HorizontalTB
		both := st.ContainerType&ContainerSize != 0
		inline, block := w, ht
		if vertical {
			inline, block = ht, w
		}
		if !haveW && (both || !vertical) {
			haveW = true
			if ok {
				u.w = w
			}
		}
		if !haveH && (both || vertical) {
			haveH = true
			if ok {
				u.h = ht
			}
		}
		if !haveI {
			haveI = true
			if ok {
				u.i = inline
			}
		}
		if !haveB && both {
			haveB = true
			if ok {
				u.b = block
			}
		}
	}
	if !found {
		return nil
	}
	return &u
}
