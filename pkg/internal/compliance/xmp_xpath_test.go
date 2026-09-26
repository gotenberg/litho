package compliance

import (
	"strings"
	"testing"
)

// TestXPathCompiles pins [xpathCompiles] against the JDK's javax.xml.xpath
// compiler, the one veraPDF 1.30.2's XPathTypeValidator runs, each verdict
// measured with XPathFactory.newInstance().newXPath().compile on that JDK.
// Beyond this table, 150,145 generated expressions, from grammar productions
// and from random characters, got the same verdict from both.
func TestXPathCompiles(t *testing.T) {
	t.Parallel()

	cases := []struct {
		expr string
		want bool
	}{
		{"/a/b[1]", true},
		{"///", false},
		{"count(", false},
		{"foo(1)", false},
		{"a:b", true},
		{"$x", true},
		{"", false},
		{"a[", false},
		{"1 + 2", true},
		{"foo::bar", false},
		{"concat('a', 'b')", true},
		{"a:foo(1)", true},
		{"//", false},
		{"/", true},
		{".", true},
		{"..", true},
		{"@a", true},
		{"*", true},
		{"a | b", true},
		{"a/b/", false},
		{"count()", false},
		{"count(1,2)", false},
		{"substring('a')", false},
		{"true()", true},
		{"lang()", false},
		{"id('a')", true},
		{"a[1][", false},
		{"1 +", false},
		{"'unterminated", false},
		{"a b", false},
		{"processing-instruction('x')", true},
		{"node()", true},
		{"text()", true},
		{"comment(1)", false},
		{"a!=b", true},
		{"a!b", false},
		{"-1", true},
		{"--1", false},
		{"1 div 0", true},
		{"a mod", false},
		{"position()", true},
		{"last()", true},
		{"local-name()", true},
		{"namespace-uri(a)", true},
		{"name()", true},
		{"normalize-space()", true},
		{"string-length()", true},
		{"translate('a','b')", false},
		{"boolean(1)", true},
		{"number()", true},
		{"sum(a)", true},
		{"floor(1)", true},
		{"ceiling(1)", true},
		{"round(1)", true},
		{"not(1)", true},
		{"false()", true},
		{"contains('a','b')", true},
		{"starts-with('a','b')", true},
		{"substring-before('a','b')", true},
		{"substring-after('a','b')", true},
		{"string()", true},
		{"ends-with('a','b')", false},
		{"matches('a','b')", false},
		{"lower-case('A')", false},
		{"child::a", true},
		{"descendant-or-self::node()", true},
		{"following-sibling::*", true},
		{"namespace::*", true},
		{"a[@b='c']", true},
		{"a[position()=1]", true},
		{"/a//b", true},
		{"$a:b", true},
		{"a:*", true},
		{"*:a", false},
		{"1.5", true},
		{".5", true},
		{"5.", true},
		{"a = 1", true},
		{"a<b", true},
		{"a<=b", true},
		{"a >= b", true},
		{"a and b", true},
		{"a or b", true},
		{"(a)", true},
		{"(a)[1]", true},
		{"key('a','b')", false},
		{"document('a')", false},
		{"current()", true},
		{"format-number(1,'#')", false},
		{"unparsed-entity-uri('a')", true},
		{"system-property('a')", true},
		{"generate-id()", true},
		{"element-available('a')", true},
		{"function-available('a')", true},
		{"x:y()", true},
		{"\"dq\"", true},
		{"'sq'", true},
		{"]", false},
		{")", false},
		{"a][", false},
		{"@", false},
		{"@*", true},
		{"@a:b", true},
		{"text", true},
		{"div", true},
		{"div div div", true},
		{"and", true},
		{"or", true},
		{"mod", true},
		{"a/..", true},
		{"../a", true},
		{"./a", true},
		{"a//.", true},
		{"//@a", true},
		{"/..", true},
		{"/*", true},
		{"a/@*", true},
		{"$", true},
		{"$1", true},
		{"1e5", false},
		{"1/a", false},
		{"'x'/a", false},
		{"(1)/a", true},
		{"$x/a", true},
		{"count(a)/b", true},
		{"concat('a','b')/a", true},
		{"(a)/b", true},
		{"id(a)/b", true},
		{"1[1]/a", false},
		{"1//a", false},
		{"(1)//a", true},
		{"x:f()/a", true},
		{"current()/a", true},
		{"(1)[1]/a", true},
		{"'x'[1]", false},
		{"1[1]", false},
		{"true()/a", true},
		{"last()/a", true},
	}
	// The JDK refuses an expression with more than 10 grouping parentheses or
	// more than 100 operators.
	for n := 9; n <= 12; n++ {
		cases = append(cases, struct {
			expr string
			want bool
		}{strings.Repeat("(", n) + "1" + strings.Repeat(")", n), n <= 10})
	}
	for n := 98; n <= 102; n++ {
		cases = append(cases, struct {
			expr string
			want bool
		}{strings.Repeat("1+", n) + "1", n <= 100})
		cases = append(cases, struct {
			expr string
			want bool
		}{"a" + strings.Repeat("[1]", n), n <= 100})
	}
	for _, tc := range cases {
		if got := xpathCompiles(tc.expr); got != tc.want {
			t.Errorf("xpathCompiles(%q) = %v, the JDK compiler answers %v", tc.expr, got, tc.want)
		}
	}
}

// TestXMPExtensionXPathValues pins the value of an extension property declared
// XPath, measured on veraPDF 1.30.2 with a crafted file at PDF/A-1b and
// PDF/A-2b: veraPDF compiles it (XPathTypeValidator) and fails 6.7.9 t3 and
// 6.6.2.3.1 t2 when it does not compile.
func TestXMPExtensionXPathValues(t *testing.T) {
	t.Parallel()

	one := func(value string, fails bool) extScopeCase {
		tc := extScopeCase{
			name:    "XPath " + value,
			entries: extScopeEntry(extScopeEx, "ex", [][2]string{{"pa", "XPath"}}),
			props:   `<ex:pa>` + value + `</ex:pa>`,
		}
		if fails {
			tc.want1b, tc.want2b = "6.7.9-t03:1", "6.6.2.3.1-t02:1"
		}
		return tc
	}
	runExtScopeCases(t, []extScopeCase{
		one("/a/b[1]", false),
		one("///", true),
		one("count(", true),
		one("foo(1)", true),
		one("a:b", false),
		one("$x", false),
		one("", true),
		one("a[", true),
		one(" ", true),
		one("1 + 2", false),
		one("foo::bar", true),
		one("concat('a', 'b')", false),
	})
}
