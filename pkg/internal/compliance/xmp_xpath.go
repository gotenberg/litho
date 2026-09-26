package compliance

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf16"
)

// xpathGroupLimit and xpathOpLimit are the JDK's default XPath expression
// limits (jdk.xml.xpathExprGrpLimit and jdk.xml.xpathExprOpLimit), which the
// XPath compiler veraPDF's XPathTypeValidator runs enforces: an expression
// with more grouping parentheses or operators does not compile. Not an
// anti-DoS guard of litho's, a mirror of the oracle's refusal; measured on
// the JDK veraPDF 1.30.2 runs with, 11 groups and 101 operators fail.
const (
	xpathGroupLimit = 10
	xpathOpLimit    = 100
)

// xpathOperators are the keywords the JDK's XPath lexer counts toward
// [xpathOpLimit] when a space ends them (Token.OPERATORS).
//
//nolint:gochecknoglobals // static lookup table.
var xpathOperators = map[string]bool{
	"or": true, "and": true, "div": true, "mod": true, "quo": true,
	"..": true, "::": true, "attribute": true, "child": true,
}

// xpathAxes are the XPath 1.0 axis names (Keywords.getAxisName).
//
//nolint:gochecknoglobals // static lookup table.
var xpathAxes = map[string]bool{
	"ancestor": true, "ancestor-or-self": true, "attribute": true, "child": true,
	"descendant": true, "descendant-or-self": true, "following": true,
	"following-sibling": true, "parent": true, "preceding": true,
	"preceding-sibling": true, "self": true, "namespace": true,
}

// xpathNodeTypes are the node type tests written with parentheses
// (Keywords.getNodeType), "*" among them.
//
//nolint:gochecknoglobals // static lookup table.
var xpathNodeTypes = map[string]bool{
	"comment": true, "text": true, "processing-instruction": true, "node": true, "*": true,
}

// xpathFuncArity bounds the argument count of each function the JDK's XPath
// function table holds (FunctionTable, and the checkNumberArgs of each
// function class): minimum and maximum, a negative maximum for none. key is
// in the table with no implementation, so a call to it never compiles, which
// a minimum above its maximum states.
//
//nolint:gochecknoglobals // static lookup table.
var xpathFuncArity = map[string][2]int{
	"current": {0, 0}, "last": {0, 0}, "position": {0, 0}, "true": {0, 0}, "false": {0, 0}, "here": {0, 0},
	"count": {1, 1}, "id": {1, 1}, "not": {1, 1}, "boolean": {1, 1}, "floor": {1, 1}, "ceiling": {1, 1},
	"round": {1, 1}, "sum": {1, 1}, "lang": {1, 1}, "system-property": {1, 1}, "function-available": {1, 1},
	"element-available": {1, 1}, "unparsed-entity-uri": {1, 1},
	"local-name": {0, 1}, "namespace-uri": {0, 1}, "name": {0, 1}, "generate-id": {0, 1}, "number": {0, 1},
	"string": {0, 1}, "normalize-space": {0, 1}, "string-length": {0, 1}, "document-location": {0, 1},
	"starts-with": {2, 2}, "contains": {2, 2}, "substring-before": {2, 2}, "substring-after": {2, 2},
	"translate": {3, 3}, "substring": {2, 3}, "concat": {2, -1},
	"key": {1, 0},
}

// xpathNumber is the form of a number token Java's Double.valueOf accepts once
// the parser has refused an exponent: decimal digits with at most one point,
// or a hexadecimal significand with its binary exponent, either with an
// optional float or double suffix.
//
//nolint:gochecknoglobals // compiled-once pattern.
var xpathNumber = regexp.MustCompile(`^(?:(?:[0-9]+\.?[0-9]*|\.[0-9]+)|0[xX](?:[0-9a-fA-F]+\.?[0-9a-fA-F]*|\.[0-9a-fA-F]+)[pP][+-]?[0-9]+)[fFdD]?$`)

// xpathCompiles reports whether expr compiles as an XPath 1.0 expression the
// way veraPDF's XPathTypeValidator compiles an XPath-typed XMP value
// (ISO 19005-2, 6.6.2.3.1 t2 and ISO 19005-1, 6.7.9 t3): with the JDK's
// javax.xml.xpath compiler, which is Xalan's, and no namespace context. It
// ports that compiler's lexer and parser and the argument counts its function
// table checks, the quirks that set it apart from the XPath 1.0 grammar
// included: a prefix needs no binding, a bare $ is a variable reference, a
// number takes no exponent, and a call to a function the table does not hold
// fails unless it is prefixed.
func xpathCompiles(expr string) bool {
	tokens, ok := xpathTokenize(utf16.Encode([]rune(expr)))
	if !ok {
		return false
	}
	p := &xpathParser{tokens: tokens}
	p.next()
	p.expr()
	if p.token != "" {
		p.failed = true
	}
	return !p.failed
}

// xpathIsLetter and xpathIsDigit are Java's Character.isLetter and isDigit on
// one UTF-16 code unit: a surrogate is neither.
func xpathIsLetter(c uint16) bool {
	return !utf16.IsSurrogate(rune(c)) && unicode.IsLetter(rune(c))
}

func xpathIsDigit(c uint16) bool {
	return !utf16.IsSurrogate(rune(c)) && unicode.IsDigit(rune(c))
}

// xpathTokenize splits pat into tokens as the JDK's XPath Lexer.tokenize does,
// counting the groups and operators it limits. ok is false where the lexer
// raises an error.
func xpathTokenize(pat []uint16) (tokens []string, ok bool) {
	str := func(a, b int) string { return string(utf16.Decode(pat[a:b])) }
	add := func(s string) { tokens = append(tokens, s) }
	peek := func(i int) uint16 {
		if i >= 0 && i < len(pat)-1 {
			return pat[i+1]
		}
		return 0
	}
	opCount, grpCount := 0, 0
	isLiteral := false
	count := func() {
		opCount++
		isLiteral = false
	}
	start, nsSep := -1, -1
	isNum, isAxis := false, false
	// mapNS emits a pending prefixed name. With no namespace context the
	// prefix stands for its own URI, so only an empty prefix fails.
	mapNS := func(end int) bool {
		prefix := ""
		if start >= 0 && nsSep >= 0 {
			prefix = str(start, nsSep)
		}
		if prefix == "" {
			return false
		}
		add(prefix)
		add(":")
		if nsSep+1 < end {
			add(str(nsSep+1, end))
		}
		nsSep = -1
		return true
	}
	// flush emits the pending name or number ending at end.
	flush := func(end int) bool {
		isNum = false
		if nsSep != -1 {
			return mapNS(end)
		}
		add(str(start, end))
		return true
	}
	n := len(pat)
	for i := 0; i < n; i++ {
		c := pat[i]
		switch c {
		case '"', '\'':
			if start != -1 && !flush(i) {
				return nil, false
			}
			start = i
			i++
			for i < n && pat[i] != c {
				i++
			}
			if i >= n {
				return nil, false
			}
			add(str(start, i+1))
			start = -1
		case '\t', '\n', '\r', ' ':
			if start == -1 {
				break
			}
			if nsSep != -1 {
				isNum = false
				if !mapNS(i) {
					return nil, false
				}
			} else {
				isNum = false
				s := str(start, i)
				if xpathOperators[s] {
					count()
				}
				add(s)
			}
			start = -1
		case '-', '@', '!', '$', '(', ')', '*', '+', ',', '/', '<', '=', '>', '[', '\\', ']', '^', '|':
			if c == '-' {
				if !isNum && start != -1 {
					// A hyphen inside a name belongs to it.
					break
				}
				isNum = false
			}
			if start != -1 {
				if !flush(i) {
					return nil, false
				}
				start = -1
			}
			switch c {
			case ')', ']':
			case '[':
				count()
				isAxis = false
			case '(':
				if isLiteral {
					if !isAxis {
						count()
					}
				} else {
					grpCount++
					count()
				}
				isAxis = false
			}
			switch c {
			case '>', '<', '=', '!':
				if peek(i) != '=' {
					count()
				}
			case '/':
				isAxis = false
				if peek(i) != '/' {
					count()
				}
			case '(', '[', ')', ']', ',':
			default:
				if c != '*' || !isAxis {
					count()
				}
				isAxis = false
			}
			add(string(rune(c)))
		default:
			if c == ':' && i > 0 {
				if nsSep == i-1 {
					if start != -1 && start < i-1 {
						add(str(start, i-1))
					}
					isNum = false
					start, nsSep = -1, -1
					opCount++
					isAxis = true
					add("::")
					break
				}
				nsSep = i
			}
			isLiteral = true
			if !isNum && c == '.' && peek(i) != '.' {
				count()
			}
			if start == -1 {
				start = i
				isNum = xpathIsDigit(c)
				break
			}
			if isNum {
				isNum = xpathIsDigit(c)
			}
		}
		if grpCount > xpathGroupLimit || opCount > xpathOpLimit {
			return nil, false
		}
	}
	if start != -1 && !flush(n) {
		return nil, false
	}
	return tokens, len(tokens) > 0
}

// xpathParser is the JDK's XPathParser over a token queue, reduced to whether
// the expression parses and compiles. Every production returns once failed is
// set, as the Java one returns by throwing.
type xpathParser struct {
	tokens []string
	pos    int
	token  string // "" past the last token
	failed bool
	// isLocationPath, lPathRequired and nextTokenRead carry the parser's check
	// that each operand of a union is a node set.
	isLocationPath, lPathRequired, nextTokenRead bool
}

func (p *xpathParser) next() {
	if p.lPathRequired {
		if p.nextTokenRead {
			if !p.isLocationPath {
				p.failed = true
			}
			p.lPathRequired, p.nextTokenRead = false, false
		} else {
			p.nextTokenRead = true
		}
	}
	if p.pos < len(p.tokens) {
		p.token = p.tokens[p.pos]
		p.pos++
		return
	}
	p.token = ""
}

// is reports whether the current token starts with c, as tokenIs(char) does.
func (p *xpathParser) is(c byte) bool {
	return p.token != "" && p.token[0] == c
}

// ahead reports whether the token n places ahead is the one-character c.
func (p *xpathParser) ahead(c string, n int) bool {
	i := p.pos + n - 1
	return i >= 0 && i < len(p.tokens) && p.tokens[i] == c
}

func (p *xpathParser) expect(c byte) {
	if !p.is(c) {
		p.failed = true
		return
	}
	p.next()
}

func (p *xpathParser) firstUnit() uint16 {
	if p.token == "" {
		return 0
	}
	return utf16.Encode([]rune(p.token))[0]
}

func (p *xpathParser) expr() {
	p.and()
	for !p.failed && p.token == "or" {
		p.next()
		p.and()
	}
}

func (p *xpathParser) and() {
	p.equality()
	for !p.failed && p.token == "and" {
		p.next()
		p.equality()
	}
}

func (p *xpathParser) equality() {
	p.relational()
	for !p.failed {
		switch {
		case p.is('!') && p.ahead("=", 1):
			p.next()
			p.next()
		case p.is('='):
			p.next()
		default:
			return
		}
		p.relational()
	}
}

func (p *xpathParser) relational() {
	p.additive()
	for !p.failed && (p.is('<') || p.is('>')) {
		p.next()
		if p.is('=') {
			p.next()
		}
		p.additive()
	}
}

func (p *xpathParser) additive() {
	p.multiplicative()
	for !p.failed && (p.is('+') || p.is('-')) {
		p.next()
		p.multiplicative()
	}
}

func (p *xpathParser) multiplicative() {
	p.unary()
	for !p.failed {
		switch {
		case p.is('*'), p.token == "div", p.token == "mod":
		case p.token == "quo":
			// The parser takes quo, and the compiler refuses its opcode.
			p.failed = true
			return
		default:
			return
		}
		p.next()
		p.unary()
	}
}

func (p *xpathParser) unary() {
	if p.is('-') {
		p.next()
	}
	p.union()
}

func (p *xpathParser) union() {
	for !p.failed {
		p.path()
		if p.failed || !p.is('|') {
			return
		}
		if !p.isLocationPath {
			p.failed = true
			return
		}
		p.isLocationPath = false
		p.lPathRequired = true
		p.next()
	}
}

func (p *xpathParser) path() {
	match, constant := p.filter()
	if p.failed {
		return
	}
	if match == 0 {
		p.locationPath()
		return
	}
	if p.is('/') {
		p.next()
		if !p.relativeLocationPath() {
			p.failed = true
		}
		match = 2
	}
	// A filter expression with predicates or a path after it compiles to a
	// location path whose first step is the primary expression, and the
	// compiler takes a variable, a group or a function call there but refuses
	// a literal or a number (OpMap.getNextStepPos).
	if match == 2 && constant {
		p.failed = true
	}
}

// filter returns 0 when no primary expression starts here, 1 for one, and 2
// for one followed by predicates, and whether that primary expression is a
// literal or a number.
func (p *xpathParser) filter() (int, bool) {
	match, constant := p.primary()
	if match == 0 || !p.is('[') {
		return match, constant
	}
	for !p.failed && p.is('[') {
		p.predicate()
	}
	return 2, constant
}

// primary parses a primary expression. It returns 0 when none starts here and
// 1 otherwise, and whether it is a literal or a number.
func (p *xpathParser) primary() (int, bool) {
	switch {
	case p.is('\'') || p.is('"'):
		p.next()
		return 1, true
	case p.is('$'):
		p.next()
		p.qname()
	case p.is('('):
		p.next()
		p.expr()
		p.expect(')')
	case p.token != "" && (p.is('.') && len(p.token) > 1 && xpathIsDigit(utf16.Encode([]rune(p.token))[1]) || xpathIsDigit(p.firstUnit())):
		p.number()
		return 1, true
	case p.ahead("(", 1) || p.ahead(":", 1) && p.ahead("(", 3):
		if !p.functionCall() {
			return 0, false
		}
	default:
		return 0, false
	}
	return 1, false
}

func (p *xpathParser) qname() {
	if p.ahead(":", 1) {
		p.next()
		p.expect(':')
	}
	p.next()
}

func (p *xpathParser) number() {
	t := strings.TrimFunc(p.token, func(r rune) bool { return r <= ' ' })
	if strings.ContainsAny(p.token, "eE") || !xpathNumber.MatchString(t) {
		p.failed = true
		return
	}
	p.next()
}

func (p *xpathParser) functionCall() bool {
	arity := [2]int{0, -1}
	if p.ahead(":", 1) {
		p.next()
		p.expect(':')
		p.next()
	} else {
		if xpathNodeTypes[p.token] && p.token != "*" {
			return false
		}
		a, known := xpathFuncArity[p.token]
		if !known {
			p.failed = true
			return true
		}
		arity = a
		switch p.token {
		case "current", "here", "id":
			p.isLocationPath = true
		}
		p.next()
	}
	p.expect('(')
	args := 0
	for !p.failed && !p.is(')') && p.token != "" {
		if p.is(',') {
			p.failed = true
			return true
		}
		p.expr()
		args++
		if p.failed || p.is(')') {
			continue
		}
		p.expect(',')
		if p.is(')') {
			p.failed = true
		}
	}
	p.expect(')')
	if args < arity[0] || arity[1] >= 0 && args > arity[1] {
		p.failed = true
	}
	return true
}

func (p *xpathParser) locationPath() {
	p.isLocationPath = true
	slash := p.is('/')
	if slash {
		p.next()
	} else if p.token == "" {
		p.failed = true
		return
	}
	if p.token != "" && !p.relativeLocationPath() && !slash {
		p.failed = true
	}
}

func (p *xpathParser) relativeLocationPath() bool {
	if !p.step() {
		return false
	}
	for !p.failed && p.is('/') {
		p.next()
		if !p.step() {
			p.failed = true
		}
	}
	return true
}

func (p *xpathParser) step() bool {
	if p.failed {
		return true
	}
	doubleSlash := p.is('/')
	if doubleSlash {
		p.next()
	}
	switch {
	case p.token == ".":
		p.next()
		if p.is('[') {
			p.failed = true
		}
	case p.token == "..":
		p.next()
	case p.is('*') || p.is('@') || p.is('_') || p.token != "" && xpathIsLetter(p.firstUnit()):
		p.basis()
		for !p.failed && p.is('[') {
			p.predicate()
		}
	default:
		if doubleSlash {
			p.failed = true
		}
		return false
	}
	return true
}

func (p *xpathParser) basis() {
	switch {
	case p.ahead("::", 1):
		if !xpathAxes[p.token] {
			p.failed = true
			return
		}
		p.next()
		p.next()
	case p.is('@'):
		p.next()
	}
	p.nodeTest()
}

func (p *xpathParser) nodeTest() {
	if p.failed {
		return
	}
	if p.ahead("(", 1) {
		if !xpathNodeTypes[p.token] {
			p.failed = true
			return
		}
		pi := p.token == "processing-instruction"
		p.next()
		p.expect('(')
		if pi && !p.failed && !p.is(')') {
			if !p.is('\'') && !p.is('"') {
				p.failed = true
				return
			}
			p.next()
		}
		p.expect(')')
		return
	}
	nameTest := func() {
		if !p.is('*') && !xpathIsLetter(p.firstUnit()) && !p.is('_') {
			p.failed = true
		}
	}
	if p.ahead(":", 1) {
		nameTest()
		p.next()
		p.expect(':')
	}
	nameTest()
	p.next()
}

func (p *xpathParser) predicate() {
	p.next()
	p.expr()
	p.expect(']')
}
