package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// A reader of the Python the WeasyPrint draw tests are written in: enough of
// the language's tokens and statement structure to find module and function
// level assignments, test functions, their decorators and parameters, and
// the calls they make. Expressions are left as token runs.

// pyKind is a token's kind.
type pyKind int

const (
	pyName pyKind = iota
	pyNumber
	pyString
	pyOp
	pyNewline
	pyIndent
	pyDedent
)

// pyToken is one token. A string keeps its source text, prefix and quotes
// included; [pyToken.str] decodes it.
type pyToken struct {
	kind pyKind
	text string
}

func (t pyToken) is(op string) bool { return t.kind == pyOp && t.text == op }

// prefix returns a string token's prefix, lower-cased.
func (t pyToken) prefix() string {
	i := strings.IndexAny(t.text, `'"`)
	return strings.ToLower(t.text[:i])
}

// str decodes a string token, reporting false for a bytes literal or an
// f-string, which are not string constants.
func (t pyToken) str() (string, bool) {
	p := t.prefix()
	if strings.ContainsAny(p, "bf") {
		return "", false
	}
	body := t.text[len(p):]
	q := body[:1]
	if strings.HasPrefix(body, q+q+q) && len(body) >= 6 {
		q = q + q + q
	}
	body = body[len(q) : len(body)-len(q)]
	if strings.Contains(p, "r") {
		return body, true
	}
	return pyUnescape(body), true
}

// pyUnescape decodes the escape sequences of a non-raw string literal's body.
// An unknown escape keeps its backslash, as Python does.
func pyUnescape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' || i+1 == len(s) {
			b.WriteByte(c)
			continue
		}
		i++
		switch e := s[i]; e {
		case '\n':
		case '\\', '\'', '"':
			b.WriteByte(e)
		case 'a':
			b.WriteByte('\a')
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case 'v':
			b.WriteByte('\v')
		case 'x', 'u', 'U':
			n := map[byte]int{'x': 2, 'u': 4, 'U': 8}[e]
			if i+1+n <= len(s) {
				if v, err := strconv.ParseUint(s[i+1:i+1+n], 16, 32); err == nil {
					b.WriteRune(rune(v)) //nolint:gosec // at most 0xFFFFFFFF, ParseUint's 32-bit bound; WriteRune writes U+FFFD past U+10FFFF
					i += n
					continue
				}
			}
			b.WriteByte('\\')
			b.WriteByte(e)
		default:
			if e >= '0' && e <= '7' {
				j := i
				for j < len(s) && j < i+3 && s[j] >= '0' && s[j] <= '7' {
					j++
				}
				v, _ := strconv.ParseUint(s[i:j], 8, 32) //nolint:errcheck // octal digits checked above
				b.WriteRune(rune(v))                     //nolint:gosec // three octal digits, at most 0o777
				i = j - 1
				continue
			}
			b.WriteByte('\\')
			b.WriteByte(e)
		}
	}
	return b.String()
}

// pyOps are the operators, longest first.
var pyOps = []string{ //nolint:gochecknoglobals // static table
	"**=", "//=", ">>=", "<<=", "...", "->", ":=",
	"+=", "-=", "*=", "/=", "%=", "&=", "|=", "^=", "@=",
	"==", "!=", "<=", ">=", "**", "//", "<<", ">>",
	"+", "-", "*", "/", "%", "&", "|", "^", "~", "@", "<", ">",
	"(", ")", "[", "]", "{", "}", ",", ":", ";", ".", "=",
}

// pyTokenize splits Python source into tokens, with NEWLINE ending each
// logical line and INDENT and DEDENT around each block, as Python's
// tokenizer does. Comments and blank lines produce nothing.
func pyTokenize(src []byte) ([]pyToken, error) {
	s := strings.ReplaceAll(strings.ReplaceAll(string(src), "\r\n", "\n"), "\r", "\n")
	var (
		out     []pyToken
		indents = []int{0}
		depth   int
		bol     = true
	)
	for i := 0; i < len(s); {
		if bol && depth == 0 {
			col, j := 0, i
			for j < len(s) && (s[j] == ' ' || s[j] == '\t') {
				if s[j] == '\t' {
					col = (col/8 + 1) * 8
				} else {
					col++
				}
				j++
			}
			if j == len(s) || s[j] == '\n' || s[j] == '#' {
				for j < len(s) && s[j] != '\n' {
					j++
				}
				i = j + 1
				continue
			}
			switch top := indents[len(indents)-1]; {
			case col > top:
				indents = append(indents, col)
				out = append(out, pyToken{kind: pyIndent})
			case col < top:
				for col < indents[len(indents)-1] {
					indents = indents[:len(indents)-1]
					out = append(out, pyToken{kind: pyDedent})
				}
				if col != indents[len(indents)-1] {
					return nil, fmt.Errorf("line %d: inconsistent dedent", strings.Count(s[:i], "\n")+1)
				}
			}
			i, bol = j, false
		}
		c := s[i]
		switch {
		case c == '\n':
			if depth == 0 {
				if len(out) > 0 && out[len(out)-1].kind != pyNewline {
					out = append(out, pyToken{kind: pyNewline})
				}
				bol = true
			}
			i++
		case c == ' ' || c == '\t' || c == '\f':
			i++
		case c == '#':
			for i < len(s) && s[i] != '\n' {
				i++
			}
		case c == '\\' && i+1 < len(s) && s[i+1] == '\n':
			i += 2
		case c == '"' || c == '\'' || (isPyIdentStart(c) && pyStringAhead(s[i:])):
			n, err := pyStringLen(s[i:])
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", strings.Count(s[:i], "\n")+1, err)
			}
			out = append(out, pyToken{kind: pyString, text: s[i : i+n]})
			i += n
		case isPyIdentStart(c) || c >= utf8.RuneSelf:
			j := i
			for j < len(s) {
				r, n := utf8.DecodeRuneInString(s[j:])
				if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
					break
				}
				j += n
			}
			if j == i {
				return nil, fmt.Errorf("line %d: unexpected character", strings.Count(s[:i], "\n")+1)
			}
			out = append(out, pyToken{kind: pyName, text: s[i:j]})
			i = j
		case c >= '0' && c <= '9' || (c == '.' && i+1 < len(s) && s[i+1] >= '0' && s[i+1] <= '9'):
			j := i
			for j < len(s) && (isPyIdentStart(s[j]) || s[j] >= '0' && s[j] <= '9' || s[j] == '.' ||
				((s[j] == '+' || s[j] == '-') && (s[j-1] == 'e' || s[j-1] == 'E'))) {
				j++
			}
			out = append(out, pyToken{kind: pyNumber, text: s[i:j]})
			i = j
		default:
			op := ""
			for _, o := range pyOps {
				if strings.HasPrefix(s[i:], o) {
					op = o
					break
				}
			}
			if op == "" {
				return nil, fmt.Errorf("line %d: unexpected %q", strings.Count(s[:i], "\n")+1, c)
			}
			switch op {
			case "(", "[", "{":
				depth++
			case ")", "]", "}":
				depth--
			}
			out = append(out, pyToken{kind: pyOp, text: op})
			i += len(op)
		}
	}
	if len(out) > 0 && out[len(out)-1].kind != pyNewline {
		out = append(out, pyToken{kind: pyNewline})
	}
	for len(indents) > 1 {
		indents = indents[:len(indents)-1]
		out = append(out, pyToken{kind: pyDedent})
	}
	return out, nil
}

func isPyIdentStart(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// pyStringAhead reports whether s starts with a string prefix and a quote.
func pyStringAhead(s string) bool {
	for i := 0; i < len(s) && i < 3; i++ {
		switch c := s[i]; c {
		case '\'', '"':
			return i > 0
		case 'r', 'R', 'b', 'B', 'u', 'U', 'f', 'F':
		default:
			return false
		}
	}
	return false
}

// pyStringLen returns the length of the string literal s starts with.
func pyStringLen(s string) (int, error) {
	i := strings.IndexAny(s, `'"`)
	q := s[i : i+1]
	if strings.HasPrefix(s[i:], q+q+q) {
		q = q + q + q
	}
	for j := i + len(q); j < len(s); j++ {
		switch {
		case s[j] == '\\':
			j++
		case strings.HasPrefix(s[j:], q):
			return j + len(q), nil
		case s[j] == '\n' && len(q) == 1:
			return 0, errors.New("unterminated string")
		}
	}
	return 0, errors.New("unterminated string")
}

// pyStmt is one statement: the tokens of its logical line, without the
// NEWLINE, its block when it is a compound statement, and the decorator
// lines before it.
type pyStmt struct {
	decorators [][]pyToken
	tokens     []pyToken
	body       []pyStmt
}

// pyParse groups Python source into its top-level statements.
func pyParse(src []byte) ([]pyStmt, error) {
	toks, err := pyTokenize(src)
	if err != nil {
		return nil, err
	}
	stmts, rest := pyBlock(toks)
	if len(rest) > 0 {
		return nil, errors.New("unbalanced indentation")
	}
	return stmts, nil
}

// pyBlock reads statements up to the DEDENT ending the block, and returns
// them with the tokens after that DEDENT.
func pyBlock(toks []pyToken) ([]pyStmt, []pyToken) {
	var (
		stmts      []pyStmt
		decorators [][]pyToken
	)
	for len(toks) > 0 {
		if toks[0].kind == pyDedent {
			return stmts, toks[1:]
		}
		end := 0
		for end < len(toks) && toks[end].kind != pyNewline {
			end++
		}
		line := toks[:end]
		toks = toks[min(end+1, len(toks)):]
		if len(line) > 0 && line[0].is("@") {
			decorators = append(decorators, line[1:])
			continue
		}
		st := pyStmt{decorators: decorators, tokens: line}
		decorators = nil
		if len(toks) > 0 && toks[0].kind == pyIndent {
			st.body, toks = pyBlock(toks[1:])
		}
		stmts = append(stmts, st)
	}
	return stmts, nil
}

// isDef reports whether the statement defines a function or a class.
func (s pyStmt) isDef() bool {
	return len(s.tokens) > 0 && s.tokens[0].kind == pyName && (s.tokens[0].text == "def" || s.tokens[0].text == "class")
}

// testFunction returns the name and the parameters of a module-level test
// function: a plain def whose name starts with test_.
func (s pyStmt) testFunction() (string, []string, bool) {
	t := s.tokens
	if len(t) < 3 || t[0].kind != pyName || t[0].text != "def" || !strings.HasPrefix(t[1].text, "test_") || !t[2].is("(") {
		return "", nil, false
	}
	var params []string
	depth, start := 0, true
	for _, tok := range t[3:] {
		switch {
		case tok.is("(") || tok.is("[") || tok.is("{"):
			depth++
		case tok.is(")") || tok.is("]") || tok.is("}"):
			if depth == 0 {
				return t[1].text, params, true
			}
			depth--
		case depth == 0 && tok.is(","):
			start = true
			continue
		}
		if depth == 0 && start {
			start = false
			// *args, **kwargs and the * and / markers are not parameters
			// pytest fills.
			if tok.kind == pyName {
				params = append(params, tok.text)
			}
		}
	}
	return t[1].text, params, true
}

// xfail reports whether a decorator of the statement is an attribute named
// xfail, called or not: pytest.mark.xfail.
func (s pyStmt) xfail() bool {
	for _, d := range s.decorators {
		target := d
		if n := len(d); n > 0 && d[n-1].is(")") {
			depth := 0
			for i := n - 1; i >= 0; i-- {
				if d[i].is(")") {
					depth++
				} else if d[i].is("(") {
					if depth--; depth == 0 {
						target = d[:i]
						break
					}
				}
			}
		}
		if n := len(target); n >= 3 && target[n-1].kind == pyName && target[n-1].text == "xfail" && target[n-2].is(".") {
			return true
		}
	}
	return false
}

// pyCall is the tokens between the parentheses of one call.
type pyCall []pyToken

// calls returns, in source order, the calls the statement, its decorators
// and its block make to the plain name fn.
func (s pyStmt) calls(fn string) []pyCall {
	var out []pyCall
	scan := func(t []pyToken) {
		for i := 0; i+1 < len(t); i++ {
			if t[i].kind != pyName || t[i].text != fn || !t[i+1].is("(") {
				continue
			}
			if i > 0 && (t[i-1].is(".") || (t[i-1].kind == pyName && t[i-1].text == "def")) {
				continue
			}
			depth := 0
			for j := i + 1; j < len(t); j++ {
				if t[j].is("(") || t[j].is("[") || t[j].is("{") {
					depth++
				} else if t[j].is(")") || t[j].is("]") || t[j].is("}") {
					if depth--; depth == 0 {
						out = append(out, pyCall(t[i+2:j]))
						break
					}
				}
			}
		}
	}
	for _, d := range s.decorators {
		scan(d)
	}
	scan(s.tokens)
	for _, b := range s.body {
		out = append(out, b.calls(fn)...)
	}
	return out
}

// args splits a call's arguments, and reports whether it passes a keyword
// argument or **mapping.
func (c pyCall) args() ([][]pyToken, bool) {
	var (
		args     [][]pyToken
		keywords bool
	)
	for _, a := range pySplitTop(c, ",") {
		if len(a) == 0 {
			continue
		}
		if a[0].is("**") || (len(a) > 1 && a[0].kind == pyName && a[1].is("=")) {
			keywords = true
			continue
		}
		args = append(args, a)
	}
	return args, keywords
}

// pySplitTop splits tokens at the operator sep outside brackets.
func pySplitTop(t []pyToken, sep string) [][]pyToken {
	var (
		out   [][]pyToken
		depth int
		start int
	)
	for i, tok := range t {
		switch {
		case tok.is("(") || tok.is("[") || tok.is("{"):
			depth++
		case tok.is(")") || tok.is("]") || tok.is("}"):
			depth--
		case depth == 0 && tok.is(sep):
			out = append(out, t[start:i])
			start = i + 1
		}
	}
	return append(out, t[start:])
}

// pyStripParens removes the parentheses enclosing a whole expression.
func pyStripParens(t []pyToken) []pyToken {
	for len(t) >= 2 && t[0].is("(") && t[len(t)-1].is(")") {
		depth := 0
		whole := true
		for i, tok := range t {
			if tok.is("(") || tok.is("[") || tok.is("{") {
				depth++
			} else if tok.is(")") || tok.is("]") || tok.is("}") {
				depth--
				if depth == 0 && i < len(t)-1 {
					whole = false
					break
				}
			}
		}
		if !whole {
			break
		}
		t = t[1 : len(t)-1]
	}
	return t
}

// pyConstant returns the string an expression is when it is a string
// constant: string literals, implicitly concatenated, none bytes or an
// f-string.
func pyConstant(t []pyToken) (string, bool) {
	t = pyStripParens(t)
	if len(t) == 0 {
		return "", false
	}
	var b strings.Builder
	for _, tok := range t {
		if tok.kind != pyString {
			return "", false
		}
		s, ok := tok.str()
		if !ok {
			return "", false
		}
		b.WriteString(s)
	}
	return b.String(), true
}

// pyLiteral returns the string an argument evaluates to: a string constant,
// or a name bound to one.
func pyLiteral(t []pyToken, names map[string]string) (string, bool) {
	if s, ok := pyConstant(t); ok {
		return s, true
	}
	t = pyStripParens(t)
	if len(t) == 1 && t[0].kind == pyName {
		s, ok := names[t[0].text]
		return s, ok
	}
	return "", false
}

// pyStringBindings maps each name a block binds exactly once, to a string
// constant, to that string. Every assignment in the block counts, nested
// blocks included, except those inside a def or class of the block itself.
func pyStringBindings(stmts []pyStmt) map[string]string {
	counts, values := map[string]int{}, map[string]string{}
	var walk func(s pyStmt)
	walk = func(s pyStmt) {
		for _, simple := range pySimpleStatements(s.tokens) {
			pyAssignments(simple, counts, values)
		}
		for _, b := range s.body {
			walk(b)
		}
	}
	for _, s := range stmts {
		if !s.isDef() {
			walk(s)
		}
	}
	out := map[string]string{}
	for k, v := range values {
		if counts[k] == 1 {
			out[k] = v
		}
	}
	return out
}

// pyCompound are the keywords opening a compound statement.
var pyCompound = map[string]bool{ //nolint:gochecknoglobals // static set
	"if": true, "elif": true, "else": true, "for": true, "while": true, "with": true,
	"try": true, "except": true, "finally": true, "def": true, "class": true, "async": true,
}

// pySimpleStatements returns the simple statements of a logical line: the
// statements after the header of a compound statement written on one line,
// each separated by ";".
func pySimpleStatements(t []pyToken) [][]pyToken {
	if len(t) > 0 && t[0].kind == pyName && pyCompound[t[0].text] {
		depth := 0
		rest := []pyToken(nil)
	scan:
		for i, tok := range t {
			switch {
			case tok.is("(") || tok.is("[") || tok.is("{"):
				depth++
			case tok.is(")") || tok.is("]") || tok.is("}"):
				depth--
			case depth == 0 && tok.is(":"):
				rest = t[i+1:]
				break scan
			}
		}
		t = rest
	}
	return pySplitTop(t, ";")
}

// pyAssignments counts the names a simple statement binds by an
// assignment, an augmented assignment or an annotated one, and records the
// string constant a plain assignment binds.
func pyAssignments(t []pyToken, counts map[string]int, values map[string]string) {
	parts := pySplitTop(t, "=")
	if len(parts) > 1 {
		value, isStr := pyConstant(parts[len(parts)-1])
		for _, target := range parts[:len(parts)-1] {
			if len(target) == 1 && target[0].kind == pyName {
				counts[target[0].text]++
				if isStr {
					values[target[0].text] = value
				}
			} else if len(target) >= 2 && target[0].kind == pyName && target[1].is(":") {
				counts[target[0].text]++
			}
		}
		return
	}
	if len(t) >= 2 && t[0].kind == pyName {
		if (t[1].kind == pyOp && strings.HasSuffix(t[1].text, "=") && t[1].text != "==" && t[1].text != "!=" && t[1].text != "<=" && t[1].text != ">=") || t[1].is(":") {
			counts[t[0].text]++
		}
	}
}
