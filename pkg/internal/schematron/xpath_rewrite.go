package schematron

import (
	"regexp"
	"strings"
)

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
	out = numericCastRewrite.ReplaceAllString(out, "number($1)")
	out = stringCastRewrite.ReplaceAllString(out, "string($1)")
	out = rewriteCaseFunction(out, "upper-case", upperCaseLo, upperCaseUp)
	out = rewriteCaseFunction(out, "lower-case", lowerCaseUp, lowerCaseLo)
	out = rewriteFnRename(out, "exists", "boolean")
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

//nolint:gochecknoglobals // immutable compiled patterns shared across calls.
var (
	// numericCastRewrite matches xs:decimal(EXPR), xs:integer(EXPR),
	// xs:double(EXPR), xs:float(EXPR). EXPR is the bracketed body,
	// captured greedily up to the matching close paren. The naive
	// pattern below cannot count parens, so a nested xs:decimal call
	// will only catch the inner one; outer calls get caught on a
	// second pass because the rewriter runs once per compile request
	// and the inner result no longer carries the xs: prefix.
	numericCastRewrite = regexp.MustCompile(`xs:(?:decimal|integer|double|float|long|int|short|byte|positiveInteger|nonNegativeInteger)\(([^()]*)\)`)
	stringCastRewrite  = regexp.MustCompile(`xs:(?:string|date|dateTime|time|anyURI|normalizedString|token|language)\(([^()]*)\)`)
)

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
