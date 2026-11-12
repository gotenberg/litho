package css

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// tokenType is the type of a token (CSS Syntax 3, 4). The block types
// double as the types of simple blocks once the parser has consumed them.
type tokenType uint8

const (
	tEOF tokenType = iota
	tIdent
	tFunction
	tAtKeyword
	tHash
	tString
	tBadString
	tURL
	tBadURL
	tDelim
	tNumber
	tPercentage
	tDimension
	tWhitespace
	tCDO
	tCDC
	tColon
	tSemicolon
	tComma
	tLBracket
	tRBracket
	tLParen
	tRParen
	tLBrace
	tRBrace
)

// token is a token or a component value (CSS Syntax 3, 5.1). A function
// (tFunction) or a simple block (tLBrace, tLBracket, tLParen) produced by
// the parser holds its contents in children.
type token struct {
	typ tokenType
	// value is the name of an ident, function or at-keyword, the value of
	// a hash, string or url, and the unit of a dimension.
	value string
	// repr is the source text of a number, percentage or dimension's
	// numeric part.
	repr string
	num  float64
	// delim is the code point of a delim token.
	delim rune
	// integer is the type flag of a numeric token; id is the type flag of
	// a hash token; eof records a string or url that ran into the end of
	// the input.
	integer, id, eof bool
	children         []token
}

// isDelim reports whether t is the delim token c.
func (t *token) isDelim(c rune) bool { return t.typ == tDelim && t.delim == c }

// isIdent reports whether t is an ident ASCII case-insensitively equal to
// name, which is lowercase.
func (t *token) isIdent(name string) bool {
	return t.typ == tIdent && eqFold(t.value, name)
}

// tokenizer splits CSS text into tokens (CSS Syntax 3, 4.3). Its input is
// already preprocessed (CSS Syntax 3, 3.3).
type tokenizer struct {
	s     string
	pos   int
	count int
	max   int
	// over records that more than max tokens were asked for: the
	// tokenizer then returns EOF.
	over bool
}

// newTokenizer tokenizes src, allowing at most maxTokens tokens.
func newTokenizer(src string, maxTokens int) *tokenizer {
	return &tokenizer{s: preprocess(src), max: maxTokens}
}

// preprocess filters the input code points (CSS Syntax 3, 3.3): CR LF, CR
// and FF become LF, NUL becomes U+FFFD, and invalid UTF-8 becomes U+FFFD.
func preprocess(s string) string {
	clean := utf8.ValidString(s) && !strings.ContainsAny(s, "\r\f\x00")
	if clean {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		c, n := utf8.DecodeRuneInString(s[i:])
		switch c {
		case '\r':
			b.WriteByte('\n')
			if i+1 < len(s) && s[i+1] == '\n' {
				n = 2
			}
		case '\f':
			b.WriteByte('\n')
		case 0, utf8.RuneError:
			b.WriteRune(utf8.RuneError)
		default:
			b.WriteString(s[i : i+n])
		}
		i += n
	}
	return b.String()
}

// at returns the code point at byte offset i and its size, or (-1, 0) at
// the end of the input.
func (t *tokenizer) at(i int) (rune, int) {
	if i >= len(t.s) {
		return -1, 0
	}
	if c := t.s[i]; c < utf8.RuneSelf {
		return rune(c), 1
	}
	return utf8.DecodeRuneInString(t.s[i:])
}

// peek returns the code points at the current position and the two after
// it, -1 past the end.
func (t *tokenizer) peek() (c0, c1, c2 rune) {
	c0, n0 := t.at(t.pos)
	c1, n1 := t.at(t.pos + n0)
	c2, _ = t.at(t.pos + n0 + n1)
	return c0, c1, c2
}

func isDigit(c rune) bool    { return c >= '0' && c <= '9' }
func isHexDigit(c rune) bool { return isDigit(c) || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') }
func isWS(c rune) bool       { return c == ' ' || c == '\t' || c == '\n' }

// isIdentStart is an ident-start code point (CSS Syntax 3, 4.2).
func isIdentStart(c rune) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_' || c >= 0x80
}

// isIdentChar is an ident code point (CSS Syntax 3, 4.2).
func isIdentChar(c rune) bool { return isIdentStart(c) || isDigit(c) || c == '-' }

// isNonPrintable is a non-printable code point (CSS Syntax 3, 4.2).
func isNonPrintable(c rune) bool {
	return (c >= 0 && c <= 8) || c == 0x0B || (c >= 0x0E && c <= 0x1F) || c == 0x7F
}

// validEscape checks whether two code points are a valid escape (CSS
// Syntax 3, 4.3.8).
func validEscape(c0, c1 rune) bool { return c0 == '\\' && c1 != '\n' }

// startsIdent checks whether three code points would start an ident
// sequence (CSS Syntax 3, 4.3.9).
func startsIdent(c0, c1, c2 rune) bool {
	switch {
	case c0 == '-':
		return isIdentStart(c1) || c1 == '-' || validEscape(c1, c2)
	case isIdentStart(c0):
		return true
	case c0 == '\\':
		return validEscape(c0, c1)
	}
	return false
}

// startsNumber checks whether three code points would start a number (CSS
// Syntax 3, 4.3.10).
func startsNumber(c0, c1, c2 rune) bool {
	switch c0 {
	case '+', '-':
		return isDigit(c1) || (c1 == '.' && isDigit(c2))
	case '.':
		return isDigit(c1)
	}
	return isDigit(c0)
}

// next consumes a token (CSS Syntax 3, 4.3.1).
func (t *tokenizer) next() token {
	t.consumeComments()
	if t.pos >= len(t.s) || t.over {
		return token{typ: tEOF}
	}
	t.count++
	if t.count > t.max {
		t.over = true
		return token{typ: tEOF}
	}
	c0, c1, c2 := t.peek()
	switch {
	case isWS(c0):
		for {
			c, n := t.at(t.pos)
			if !isWS(c) {
				break
			}
			t.pos += n
		}
		return token{typ: tWhitespace}
	case c0 == '"' || c0 == '\'':
		t.pos++
		return t.consumeString(c0)
	case c0 == '#':
		if isIdentChar(c1) || validEscape(c1, c2) {
			t.pos++
			a, b, c := t.peek()
			id := startsIdent(a, b, c)
			return token{typ: tHash, value: t.consumeIdent(), id: id}
		}
	case c0 == '(':
		t.pos++
		return token{typ: tLParen}
	case c0 == ')':
		t.pos++
		return token{typ: tRParen}
	case c0 == '[':
		t.pos++
		return token{typ: tLBracket}
	case c0 == ']':
		t.pos++
		return token{typ: tRBracket}
	case c0 == '{':
		t.pos++
		return token{typ: tLBrace}
	case c0 == '}':
		t.pos++
		return token{typ: tRBrace}
	case c0 == ',':
		t.pos++
		return token{typ: tComma}
	case c0 == ':':
		t.pos++
		return token{typ: tColon}
	case c0 == ';':
		t.pos++
		return token{typ: tSemicolon}
	case c0 == '+' || c0 == '.':
		if startsNumber(c0, c1, c2) {
			return t.consumeNumeric()
		}
	case c0 == '-':
		if startsNumber(c0, c1, c2) {
			return t.consumeNumeric()
		}
		if c1 == '-' && c2 == '>' {
			t.pos += 3
			return token{typ: tCDC}
		}
		if startsIdent(c0, c1, c2) {
			return t.consumeIdentLike()
		}
	case c0 == '<':
		if strings.HasPrefix(t.s[t.pos:], "<!--") {
			t.pos += 4
			return token{typ: tCDO}
		}
	case c0 == '@':
		_, n := t.at(t.pos)
		save := t.pos
		t.pos += n
		a, b, c := t.peek()
		if startsIdent(a, b, c) {
			return token{typ: tAtKeyword, value: t.consumeIdent()}
		}
		t.pos = save
	case c0 == '\\':
		if validEscape(c0, c1) {
			return t.consumeIdentLike()
		}
	case isDigit(c0):
		return t.consumeNumeric()
	case isIdentStart(c0):
		return t.consumeIdentLike()
	}
	_, n := t.at(t.pos)
	t.pos += n
	return token{typ: tDelim, delim: c0}
}

// consumeComments skips comments (CSS Syntax 3, 4.3.2).
func (t *tokenizer) consumeComments() {
	for strings.HasPrefix(t.s[t.pos:], "/*") {
		end := strings.Index(t.s[t.pos+2:], "*/")
		if end < 0 {
			t.pos = len(t.s)
			return
		}
		t.pos += end + 4
	}
}

// consumeEscape consumes an escaped code point after the backslash (CSS
// Syntax 3, 4.3.7).
func (t *tokenizer) consumeEscape() rune {
	c, n := t.at(t.pos)
	if c < 0 {
		return utf8.RuneError
	}
	if !isHexDigit(c) {
		t.pos += n
		return c
	}
	v := rune(0)
	for range 6 {
		c, _ := t.at(t.pos)
		if !isHexDigit(c) {
			break
		}
		t.pos++
		switch {
		case c >= 'a':
			v = v*16 + c - 'a' + 10
		case c >= 'A':
			v = v*16 + c - 'A' + 10
		default:
			v = v*16 + c - '0'
		}
	}
	if c, n := t.at(t.pos); isWS(c) {
		t.pos += n
	}
	if v == 0 || (v >= 0xD800 && v <= 0xDFFF) || v > utf8.MaxRune {
		return utf8.RuneError
	}
	return v
}

// consumeIdent consumes an ident sequence (CSS Syntax 3, 4.3.11).
func (t *tokenizer) consumeIdent() string {
	start := t.pos
	var b *strings.Builder
	for {
		c0, c1, _ := t.peek()
		switch {
		case isIdentChar(c0):
			_, n := t.at(t.pos)
			if b != nil {
				b.WriteString(t.s[t.pos : t.pos+n])
			}
			t.pos += n
		case validEscape(c0, c1):
			if b == nil {
				b = &strings.Builder{}
				b.WriteString(t.s[start:t.pos])
			}
			t.pos++
			b.WriteRune(t.consumeEscape())
		default:
			if b != nil {
				return b.String()
			}
			return t.s[start:t.pos]
		}
	}
}

// consumeNumber consumes a number (CSS Syntax 3, 4.3.12).
func (t *tokenizer) consumeNumber() (repr string, value float64, integer bool) {
	start := t.pos
	integer = true
	if c, _ := t.at(t.pos); c == '+' || c == '-' {
		t.pos++
	}
	t.digits()
	if c0, c1, _ := t.peek(); c0 == '.' && isDigit(c1) {
		t.pos++
		t.digits()
		integer = false
	}
	if c0, c1, c2 := t.peek(); (c0 == 'e' || c0 == 'E') && (isDigit(c1) || ((c1 == '+' || c1 == '-') && isDigit(c2))) {
		t.pos += 2
		t.digits()
		integer = false
	}
	repr = t.s[start:t.pos]
	// ParseFloat fails only on range: it then returns the infinity of the
	// right sign, which the value parsers reject as non-finite.
	value, _ = strconv.ParseFloat(repr, 64)
	return repr, value, integer
}

func (t *tokenizer) digits() {
	for t.pos < len(t.s) && isDigit(rune(t.s[t.pos])) {
		t.pos++
	}
}

// consumeNumeric consumes a numeric token (CSS Syntax 3, 4.3.3).
func (t *tokenizer) consumeNumeric() token {
	repr, v, integer := t.consumeNumber()
	c0, c1, c2 := t.peek()
	if startsIdent(c0, c1, c2) {
		return token{typ: tDimension, repr: repr, num: v, integer: integer, value: t.consumeIdent()}
	}
	if c0 == '%' {
		t.pos++
		return token{typ: tPercentage, repr: repr, num: v, integer: integer}
	}
	return token{typ: tNumber, repr: repr, num: v, integer: integer}
}

// consumeIdentLike consumes an ident-like token (CSS Syntax 3, 4.3.4).
func (t *tokenizer) consumeIdentLike() token {
	name := t.consumeIdent()
	c, _ := t.at(t.pos)
	if c != '(' {
		return token{typ: tIdent, value: name}
	}
	t.pos++
	if !eqFold(name, "url") {
		return token{typ: tFunction, value: name}
	}
	for {
		c0, c1, _ := t.peek()
		if isWS(c0) && isWS(c1) {
			t.pos++
			continue
		}
		if c0 == '"' || c0 == '\'' || (isWS(c0) && (c1 == '"' || c1 == '\'')) {
			return token{typ: tFunction, value: name}
		}
		return t.consumeURL()
	}
}

// consumeString consumes a string token after its opening quote (CSS
// Syntax 3, 4.3.5).
func (t *tokenizer) consumeString(quote rune) token {
	var b strings.Builder
	for {
		c, n := t.at(t.pos)
		switch {
		case c < 0:
			return token{typ: tString, value: b.String(), eof: true}
		case c == quote:
			t.pos += n
			return token{typ: tString, value: b.String()}
		case c == '\n':
			return token{typ: tBadString}
		case c == '\\':
			t.pos++
			next, m := t.at(t.pos)
			switch {
			case next < 0:
			case next == '\n':
				t.pos += m
			default:
				b.WriteRune(t.consumeEscape())
			}
		default:
			b.WriteString(t.s[t.pos : t.pos+n])
			t.pos += n
		}
	}
}

// consumeURL consumes a url token after "url(" (CSS Syntax 3, 4.3.6).
func (t *tokenizer) consumeURL() token {
	var b strings.Builder
	t.skipWS()
	for {
		c0, c1, _ := t.peek()
		_, n := t.at(t.pos)
		switch {
		case c0 == ')':
			t.pos++
			return token{typ: tURL, value: b.String()}
		case c0 < 0:
			return token{typ: tURL, value: b.String(), eof: true}
		case isWS(c0):
			t.skipWS()
			c, _ := t.at(t.pos)
			if c == ')' {
				t.pos++
				return token{typ: tURL, value: b.String()}
			}
			if c < 0 {
				return token{typ: tURL, value: b.String(), eof: true}
			}
			t.consumeBadURL()
			return token{typ: tBadURL}
		case c0 == '"' || c0 == '\'' || c0 == '(' || isNonPrintable(c0):
			t.consumeBadURL()
			return token{typ: tBadURL}
		case c0 == '\\':
			if !validEscape(c0, c1) {
				t.consumeBadURL()
				return token{typ: tBadURL}
			}
			t.pos++
			b.WriteRune(t.consumeEscape())
		default:
			b.WriteString(t.s[t.pos : t.pos+n])
			t.pos += n
		}
	}
}

func (t *tokenizer) skipWS() {
	for {
		c, n := t.at(t.pos)
		if !isWS(c) {
			return
		}
		t.pos += n
	}
}

// consumeBadURL consumes the remnants of a bad url (CSS Syntax 3, 4.3.14).
func (t *tokenizer) consumeBadURL() {
	for {
		c0, c1, _ := t.peek()
		_, n := t.at(t.pos)
		switch {
		case c0 < 0:
			return
		case c0 == ')':
			t.pos++
			return
		case validEscape(c0, c1):
			t.pos++
			t.consumeEscape()
		default:
			t.pos += n
		}
	}
}
