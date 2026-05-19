package schematron

import "strings"

// rewriteXPath2 transforms a subset of XPath 2.0 expressions into
// XPath 1.0 equivalents that antchfx/xpath can compile. The
// transformations are textual and intentionally narrow: cover the
// constructs EN 16931 and XRechnung lean on, leave everything else
// untouched.
//
// Specifically:
//
//   - xs:decimal(x) / xs:integer(x) / xs:double(x) → number(x).
//     Loses the exact-arithmetic semantics of xs:decimal but produces
//     the right verdict for the existence / equality / range checks
//     that dominate EN 16931's arithmetic rules.
//   - xs:string(x) / xs:date(x) / xs:dateTime(x) / xs:anyURI(x) →
//     string(x). Date comparisons fall back to lexicographic string
//     comparison; CCYY-MM-DD ordering happens to coincide with that.
//   - upper-case(x) / lower-case(x) → translate(x, lo, up) / mirror.
//   - The lossy transformations above are documented at the call
//     site so callers know what to expect when comparing verdicts
//     against Saxon.
//
// Functions outside this list (tokenize, string-join, current-date,
// xsl:function helpers) are left alone; rules that call them fail
// to compile and the evaluator skips them via the per-rule isolation
// in [evaluateRule].
func rewriteXPath2(expr string) string {
	out := expr
	for _, cast := range numericCasts {
		out = rewriteCast(out, "xs:"+cast, "number")
	}
	for _, cast := range stringCasts {
		out = rewriteCast(out, "xs:"+cast, "string")
	}
	out = rewriteCaseFunction(out, "upper-case", upperCaseLo, upperCaseUp)
	out = rewriteCaseFunction(out, "lower-case", lowerCaseUp, lowerCaseLo)
	out = rewriteFnRename(out, "exists", "boolean")
	out = rewriteSequenceEquality(out)
	return out
}

//nolint:gochecknoglobals // immutable cast tables shared across calls.
var (
	numericCasts = []string{
		"decimal", "integer", "double", "float", "long", "int",
		"short", "byte", "positiveInteger", "nonNegativeInteger",
	}
	stringCasts = []string{
		"string", "date", "dateTime", "time", "anyURI",
		"normalizedString", "token", "language",
	}
)

// rewriteCast walks the expression and replaces every call of the
// form "fullName(EXPR)" with "replacement(EXPR)". Paren-aware so
// nested casts like xs:decimal(xs:decimal(sum(x))) collapse to
// number(number(sum(x))) in a single pass.
func rewriteCast(expr, fullName, replacement string) string {
	prefix := fullName + "("
	if !strings.Contains(expr, prefix) {
		return expr
	}
	var b strings.Builder
	b.Grow(len(expr))
	for i := 0; i < len(expr); {
		if strings.HasPrefix(expr[i:], prefix) && (i == 0 || !isXPathNameChar(expr[i-1])) {
			start := i + len(prefix)
			end := matchCloseParen(expr, start)
			if end < 0 {
				b.WriteByte(expr[i])
				i++
				continue
			}
			b.WriteString(replacement)
			b.WriteByte('(')
			b.WriteString(expr[start:end])
			b.WriteByte(')')
			i = end + 1
			continue
		}
		b.WriteByte(expr[i])
		i++
	}
	return b.String()
}

// rewriteSequenceEquality converts XPath 2.0 sequence-literal
// comparisons of the form
//
//	EXPR = ('a', 'b', 'c')
//	EXPR != ('a', 'b', 'c')
//
// into the XPath 1.0 disjunction
//
//	(EXPR = 'a' or EXPR = 'b' or EXPR = 'c')
//	(EXPR != 'a' and EXPR != 'b' and EXPR != 'c')
//
// The XRechnung Schematron leans on this pattern for code-list
// containment checks (BR-DE-17, BR-DE-19, BR-DE-23, BR-DE-24) which
// would otherwise skip wholesale on the trailing parenthesis-comma
// triggering antchfx/xpath's "has an invalid token" error.
//
// EXPR is conservatively captured backwards from the operator until
// a balancing whitespace or operator boundary; the rewriter walks
// each match individually so adjacent sequence literals do not get
// over-matched.
func rewriteSequenceEquality(expr string) string {
	out := expr
	for {
		next := rewriteOneSequenceEquality(out)
		if next == out {
			return out
		}
		out = next
	}
}

func rewriteOneSequenceEquality(expr string) string {
	for i := 0; i < len(expr); i++ {
		op, opLen := matchEqualityOperator(expr, i)
		if op == "" {
			continue
		}
		// Find the opening paren of the sequence literal that
		// follows the operator (whitespace allowed in between).
		k := i + opLen
		for k < len(expr) && (expr[k] == ' ' || expr[k] == '\n' || expr[k] == '\t') {
			k++
		}
		if k >= len(expr) || expr[k] != '(' {
			continue
		}
		closeIdx := matchCloseParen(expr, k+1)
		if closeIdx < 0 {
			continue
		}
		items := splitSequenceLiteral(expr[k+1 : closeIdx])
		if len(items) < 2 {
			// One-item sequence or empty literal: probably not the
			// XPath 2.0 sequence comparison we are targeting.
			continue
		}
		exprStart, exprEnd := captureLHS(expr, i)
		if exprStart < 0 {
			continue
		}
		lhs := strings.TrimSpace(expr[exprStart:exprEnd])
		joinOp := " or "
		perItemOp := " = "
		if op == "!=" {
			joinOp = " and "
			perItemOp = " != "
		}
		var parts []string
		for _, item := range items {
			parts = append(parts, lhs+perItemOp+strings.TrimSpace(item))
		}
		replacement := "(" + strings.Join(parts, joinOp) + ")"
		return expr[:exprStart] + replacement + expr[closeIdx+1:]
	}
	return expr
}

// matchEqualityOperator detects "=" or "!=" at position i in expr.
// Returns the operator string and its byte length, or ("",0) when
// no operator starts at i.
func matchEqualityOperator(expr string, i int) (string, int) {
	if i+1 < len(expr) && expr[i] == '!' && expr[i+1] == '=' {
		return "!=", 2
	}
	if expr[i] == '=' {
		// Skip "==" which is not an XPath operator but might be a
		// user typo; also skip "<=" / ">=" by checking the prior byte.
		if i > 0 && (expr[i-1] == '<' || expr[i-1] == '>' || expr[i-1] == '!' || expr[i-1] == '=') {
			return "", 0
		}
		if i+1 < len(expr) && expr[i+1] == '=' {
			return "", 0
		}
		return "=", 1
	}
	return "", 0
}

// captureLHS walks backwards from the operator at opIdx to find the
// start of the EXPR being compared. The capture honours XPath name
// characters, predicate brackets, and balanced parens so an
// expression like normalize-space(x/y) becomes the LHS atomically.
func captureLHS(expr string, opIdx int) (start, end int) {
	end = opIdx
	for end > 0 && (expr[end-1] == ' ' || expr[end-1] == '\t' || expr[end-1] == '\n') {
		end--
	}
	start = end
	depth := 0
	for start > 0 {
		c := expr[start-1]
		switch c {
		case ')', ']':
			depth++
		case '(', '[':
			depth--
			if depth < 0 {
				return start, end
			}
		case ' ', '\t', '\n':
			if depth == 0 {
				return start, end
			}
		}
		if depth == 0 {
			// Stop on operators (=, !=, <, >, and, or, +, -, *, /, etc.)
			// when we are at the top level. Recognising every operator
			// would be its own parser; conservatively break on common
			// XPath delimiters.
			if c == '|' || c == ',' {
				return start, end
			}
		}
		start--
	}
	return start, end
}

// splitSequenceLiteral splits the inside of a sequence literal on
// commas at depth zero, respecting quoted string literals.
func splitSequenceLiteral(body string) []string {
	var out []string
	depth := 0
	start := 0
	inStr := byte(0)
	for i := 0; i < len(body); i++ {
		c := body[i]
		if inStr != 0 {
			if c == inStr {
				inStr = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			inStr = c
		case '(', '[':
			depth++
		case ')', ']':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, body[start:i])
				start = i + 1
			}
		}
	}
	out = append(out, body[start:])
	return out
}

// rewriteFnRename replaces every fn(EXPR) call with replacement(EXPR).
// Used to redirect XPath 2.0 function names to their XPath 1.0
// equivalents when the semantics match exactly. For example,
// exists(x) and boolean(x) return the same value for any node set
// or sequence: empty maps to false, non-empty to true.
func rewriteFnRename(expr, fn, replacement string) string {
	prefix := fn + "("
	if !strings.Contains(expr, prefix) {
		return expr
	}
	// Replace only at function-call boundaries: the byte preceding
	// "fn(" must not be a name character, otherwise we would rewrite
	// "myExists(" or similar.
	var b strings.Builder
	b.Grow(len(expr))
	for i := 0; i < len(expr); {
		if strings.HasPrefix(expr[i:], prefix) && (i == 0 || !isXPathNameChar(expr[i-1])) {
			b.WriteString(replacement)
			b.WriteByte('(')
			i += len(prefix)
			continue
		}
		b.WriteByte(expr[i])
		i++
	}
	return b.String()
}

const (
	upperCaseLo = "abcdefghijklmnopqrstuvwxyz"
	upperCaseUp = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	lowerCaseLo = "abcdefghijklmnopqrstuvwxyz"
	lowerCaseUp = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
)

// rewriteCaseFunction rewrites every fn(EXPR) call into
// translate(EXPR, from, to). Walking byte-by-byte to handle nested
// parens correctly: regex with a single capture group misses cases
// where the argument itself contains parens, which EN 16931 uses
// often (e.g. upper-case(normalize-space(x))).
func rewriteCaseFunction(expr, fn, from, to string) string {
	prefix := fn + "("
	if !strings.Contains(expr, prefix) {
		return expr
	}
	var b strings.Builder
	b.Grow(len(expr))
	for i := 0; i < len(expr); {
		if strings.HasPrefix(expr[i:], prefix) {
			start := i + len(prefix)
			end := matchCloseParen(expr, start)
			if end < 0 {
				b.WriteByte(expr[i])
				i++
				continue
			}
			arg := expr[start:end]
			b.WriteString("translate(")
			b.WriteString(arg)
			b.WriteString(",'")
			b.WriteString(from)
			b.WriteString("','")
			b.WriteString(to)
			b.WriteString("')")
			i = end + 1
			continue
		}
		b.WriteByte(expr[i])
		i++
	}
	return b.String()
}

// matchCloseParen returns the index of the close paren that matches
// the open paren conceptually preceding start. The function counts
// parens and respects single/double-quoted XPath string literals so
// a paren inside a string does not throw off the depth counter.
// Returns -1 when no match exists.
func matchCloseParen(s string, start int) int {
	depth := 1
	i := start
	for i < len(s) {
		c := s[i]
		switch c {
		case '\'', '"':
			// Skip the entire string literal.
			j := i + 1
			for j < len(s) && s[j] != c {
				j++
			}
			if j >= len(s) {
				return -1
			}
			i = j + 1
			continue
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
		i++
	}
	return -1
}
