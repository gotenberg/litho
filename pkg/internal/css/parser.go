package css

import "strings"

// eqFold reports whether a and b are equal under ASCII case folding, the
// only folding CSS keywords get (CSS Syntax 3, 2.1). strings.EqualFold
// would also fold non-ASCII letters such as the Kelvin sign.
func eqFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		x, y := a[i], b[i]
		if x >= 'A' && x <= 'Z' {
			x += 'a' - 'A'
		}
		if y >= 'A' && y <= 'Z' {
			y += 'a' - 'A'
		}
		if x != y {
			return false
		}
	}
	return true
}

// itemKind says what a parsed [item] is.
type itemKind uint8

const (
	itemQualified itemKind = iota
	itemAtRule
	itemDeclaration
	// itemError records a construct the parser dropped; the CSS
	// parsing tests observe it, the cascade ignores it.
	itemError
)

// item is a rule, a declaration or a parse error (CSS Syntax 3, 5.2).
type item struct {
	kind itemKind
	// name is the at-rule's name or the declaration's name.
	name string
	// prelude is the rule's prelude; value is the declaration's value.
	prelude, value []token
	// block is the contents of the rule's {} block; hasBlock tells an
	// at-rule with an empty block from one with none.
	block     []token
	hasBlock  bool
	important bool
}

// parser consumes tokens from a tokenizer, or component values from a
// list, as CSS Syntax 3, 5.3 asks. Every algorithm is the 2021 Candidate
// Recommendation's, which the css-parsing-tests suite pins.
type parser struct {
	tz   *tokenizer
	list []token
	i    int
	cur  token
	back bool
	// depth is the current block and function nesting; deep records that
	// it went past maxNestingDepth, after which the input reads as ended.
	depth int
	deep  bool
}

// newListParser parses an already consumed list of component values.
func newListParser(list []token) *parser { return &parser{list: list} }

// next consumes the next token or component value.
func (p *parser) next() token {
	if p.back {
		p.back = false
		return p.cur
	}
	switch {
	case p.deep:
		p.cur = token{typ: tEOF}
	case p.tz != nil:
		p.cur = p.tz.next()
	case p.i < len(p.list):
		p.cur = p.list[p.i]
		p.i++
	default:
		p.cur = token{typ: tEOF}
	}
	return p.cur
}

// reconsume pushes the current token back.
func (p *parser) reconsume() { p.back = true }

// componentValue consumes a component value starting at t (CSS Syntax 3,
// 5.4.7). A list parser's values are already whole.
func (p *parser) componentValue(t token) token {
	if p.tz == nil {
		return t
	}
	switch t.typ { //nolint:exhaustive // the other token types are invalid here
	case tLBrace, tLBracket, tLParen:
		return p.block(t)
	case tFunction:
		return p.function(t)
	}
	return t
}

// enter accounts for one more level of nesting.
func (p *parser) enter() bool {
	p.depth++
	if p.depth > maxNestingDepth {
		p.deep = true
		return false
	}
	return true
}

// block consumes a simple block (CSS Syntax 3, 5.4.8).
func (p *parser) block(open token) token {
	closing := tRBrace
	switch open.typ { //nolint:exhaustive // the other token types are invalid here
	case tLBracket:
		closing = tRBracket
	case tLParen:
		closing = tRParen
	}
	defer func() { p.depth-- }()
	if !p.enter() {
		return open
	}
	for {
		t := p.next()
		if t.typ == closing || t.typ == tEOF {
			return open
		}
		open.children = append(open.children, p.componentValue(t))
	}
}

// function consumes a function (CSS Syntax 3, 5.4.9).
func (p *parser) function(fn token) token {
	defer func() { p.depth-- }()
	if !p.enter() {
		return fn
	}
	for {
		t := p.next()
		if t.typ == tRParen || t.typ == tEOF {
			return fn
		}
		fn.children = append(fn.children, p.componentValue(t))
	}
}

// rules consumes a list of rules (CSS Syntax 3, 5.4.1); topLevel skips CDO
// and CDC tokens as a stylesheet does. yield receives each rule and stops
// the loop when it returns false.
func (p *parser) rules(topLevel bool, yield func(item) bool) {
	for {
		t := p.next()
		var it item
		switch t.typ { //nolint:exhaustive // the other token types take the default path
		case tWhitespace:
			continue
		case tEOF:
			return
		case tCDO, tCDC:
			if topLevel {
				continue
			}
			p.reconsume()
			it = p.qualifiedRule()
		case tAtKeyword:
			it = p.atRule(t)
		default:
			p.reconsume()
			it = p.qualifiedRule()
		}
		if !yield(it) {
			return
		}
	}
}

// atRule consumes an at-rule (CSS Syntax 3, 5.4.2).
func (p *parser) atRule(kw token) item {
	it := item{kind: itemAtRule, name: kw.value, prelude: []token{}}
	for {
		t := p.next()
		switch t.typ { //nolint:exhaustive // the other token types take the default path
		case tSemicolon, tEOF:
			return it
		case tLBrace:
			if p.tz == nil {
				it.block, it.hasBlock = orEmpty(t.children), true
				return it
			}
			b := p.block(t)
			it.block, it.hasBlock = orEmpty(b.children), true
			return it
		}
		it.prelude = append(it.prelude, p.componentValue(t))
	}
}

// qualifiedRule consumes a qualified rule (CSS Syntax 3, 5.4.3). A rule cut
// short by the end of the input is an error item.
func (p *parser) qualifiedRule() item {
	it := item{kind: itemQualified, prelude: []token{}}
	for {
		t := p.next()
		switch t.typ { //nolint:exhaustive // the other token types take the default path
		case tEOF:
			return item{kind: itemError, name: "invalid"}
		case tLBrace:
			if p.tz == nil {
				it.block = orEmpty(t.children)
				return it
			}
			it.block = orEmpty(p.block(t).children)
			return it
		}
		it.prelude = append(it.prelude, p.componentValue(t))
	}
}

func orEmpty(l []token) []token {
	if l == nil {
		return []token{}
	}
	return l
}

// declarations consumes a list of declarations (CSS Syntax 3, 5.4.5):
// declarations and at-rules, with errors recovering at the next semicolon.
func (p *parser) declarations(yield func(item) bool) {
	for {
		t := p.next()
		switch t.typ { //nolint:exhaustive // the other token types take the default path
		case tWhitespace, tSemicolon:
			continue
		case tEOF:
			return
		case tAtKeyword:
			if !yield(p.atRule(t)) {
				return
			}
		case tIdent:
			tmp := []token{t}
			for {
				n := p.next()
				if n.typ == tSemicolon || n.typ == tEOF {
					break
				}
				tmp = append(tmp, p.componentValue(n))
			}
			it, ok := declaration(tmp)
			if !ok {
				it = item{kind: itemError, name: "invalid"}
			}
			if !yield(it) {
				return
			}
		default:
			p.reconsume()
			for {
				n := p.next()
				if n.typ == tSemicolon || n.typ == tEOF {
					break
				}
				p.componentValue(n)
			}
			if !yield(item{kind: itemError, name: "invalid"}) {
				return
			}
		}
	}
}

// declaration consumes a declaration from a list of component values that
// starts with its name (CSS Syntax 3, 5.4.6).
func declaration(list []token) (item, bool) {
	if len(list) == 0 || list[0].typ != tIdent {
		return item{}, false
	}
	it := item{kind: itemDeclaration, name: list[0].value}
	i := 1
	for i < len(list) && list[i].typ == tWhitespace {
		i++
	}
	if i >= len(list) || list[i].typ != tColon {
		return item{}, false
	}
	value := list[i+1:]
	// The last two non-whitespace tokens "!" "important" make the
	// declaration important; the suite keeps whitespace before the "!".
	last := len(value) - 1
	for last >= 0 && value[last].typ == tWhitespace {
		last--
	}
	if last >= 0 && value[last].isIdent("important") {
		bang := last - 1
		for bang >= 0 && value[bang].typ == tWhitespace {
			bang--
		}
		if bang >= 0 && value[bang].isDelim('!') {
			it.important = true
			value = value[:bang]
		}
	}
	it.value = append([]token{}, value...)
	return it, true
}

// parseComponentValues parses a list of component values (CSS Syntax 3,
// 5.3.10).
func parseComponentValues(src string, maxTok int) ([]token, *parser) {
	p := &parser{tz: newTokenizer(src, maxTok)}
	var out []token
	for {
		t := p.next()
		if t.typ == tEOF {
			return out, p
		}
		out = append(out, p.componentValue(t))
	}
}

// trimWS drops leading and trailing whitespace tokens.
func trimWS(l []token) []token {
	for len(l) > 0 && l[0].typ == tWhitespace {
		l = l[1:]
	}
	for len(l) > 0 && l[len(l)-1].typ == tWhitespace {
		l = l[:len(l)-1]
	}
	return l
}

// significant drops whitespace tokens from the top level of a value.
func significant(l []token) []token {
	out := make([]token, 0, len(l))
	for _, t := range l {
		if t.typ != tWhitespace {
			out = append(out, t)
		}
	}
	return out
}

// lower lowercases ASCII letters only, as CSS's ASCII case-insensitive
// matching asks (CSS Syntax 3, 2.1 references Infra's ASCII lowercase).
func lower(s string) string {
	for i := 0; i < len(s); i++ {
		if c := s[i]; c >= 'A' && c <= 'Z' {
			return strings.Map(func(r rune) rune {
				if r >= 'A' && r <= 'Z' {
					return r + 'a' - 'A'
				}
				return r
			}, s)
		}
	}
	return s
}
