package xsd

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

// TestPatternFor_AppendixF pins the XML Schema Part 2, Appendix F meaning of
// each construct where Go's regexp dialect reads it differently. Each case
// lists literals the pattern must match and literals it must not.
func TestPatternFor_AppendixF(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pattern string
		match   []string
		reject  []string
	}{
		{
			// Production [16]: Go read this as [a-z-[aeiou] then "]",
			// so it matched "a]" and rejected "b".
			"class subtraction", `[a-z-[aeiou]]`,
			[]string{"b", "z", "y"},
			[]string{"a", "e", "a]", "B", ""},
		},
		{
			"nested subtraction", `[a-z-[aeiou-[e]]]`,
			[]string{"b", "e"},
			[]string{"a", "u"},
		},
		{
			"subtraction from a negative group", `[^a-z-[0-9]]`,
			[]string{"A", "-"},
			[]string{"a", "5"},
		},
		{
			"subtraction of an escape", `[\w-[\d]]+`,
			[]string{"abc", "é"},
			[]string{"ab1", "a٣"},
		},
		{
			"subtraction of a category", `[\p{Lu}-[A-Z]]`,
			[]string{"É", "Ω"},
			[]string{"E", "é"},
		},
		{
			"subtraction leaving nothing", `[a-[a]]`,
			nil,
			[]string{"a", "", "b"},
		},
		{
			"subtraction under a quantifier", `[A-Z-[IO]]{2}`,
			[]string{"DE", "FR"},
			[]string{"IO", "DI", "D"},
		},
		{
			// Production [10]: "^" and "$" are normal characters.
			"caret and dollar are literals", `^A$`,
			[]string{"^A$"},
			[]string{"A"},
		},
		{
			"wildcard excludes CR and LF", `a.c`,
			[]string{"abc", "a c", "aéc"},
			[]string{"a\nc", "a\rc", "ac"},
		},
		{
			"\\d is Unicode Nd", `\d+`,
			[]string{"42", "١٢٣"},
			[]string{"a", "4a"},
		},
		{
			// \w excludes P, so the connector "_" is not a word
			// character, unlike Go's \w; "é" is, unlike Go's.
			"\\w excludes punctuation", `\w+`,
			[]string{"élan", "a1", "€"},
			[]string{"a_b", "a b", "a-b"},
		},
		{
			"\\s is the four XML spaces", `a\sb`,
			[]string{"a b", "a\tb", "a\nb", "a\rb"},
			[]string{"a\fb", "ab"},
		},
		{
			"\\S complements \\s", `\S`,
			[]string{"a", "\f"},
			[]string{" ", "\t"},
		},
		{
			"\\W complements \\w", `\W`,
			[]string{"_", " ", "."},
			[]string{"a", "1"},
		},
		{
			"category C includes unassigned", `\p{C}`,
			[]string{"\u200b", "\u0378"},
			[]string{"a", " "},
		},
		{
			"category Cn is unassigned", `\p{Cn}`,
			[]string{"\u0378"},
			[]string{"a", "\u200b"},
		},
		{
			"complement category escape", `[\P{L}]`,
			[]string{"1", " "},
			[]string{"a"},
		},
		{
			"negative group", `[^\p{L}]`,
			[]string{"1", "\n"},
			[]string{"a"},
		},
		{
			// Go reads a count with a leading zero as literal text.
			"quantity with a leading zero", `a{02}`,
			[]string{"aa"},
			[]string{"a", "a{02}"},
		},
		{
			"open quantity", `a{2,}`,
			[]string{"aa", "aaaa"},
			[]string{"a"},
		},
		{
			"bounded quantity", `a{1,2}`,
			[]string{"a", "aa"},
			[]string{"", "aaa"},
		},
		{
			"groups and branches", `(ab|cd)?e`,
			[]string{"e", "abe", "cde"},
			[]string{"abcde", "ab"},
		},
		{
			"empty branch", `a|`,
			[]string{"a", ""},
			[]string{"b"},
		},
		{
			"empty pattern", ``,
			[]string{""},
			[]string{"a"},
		},
		{
			"dash at the start and end of a group", `[-a][a-]`,
			[]string{"-a", "a-", "--", "aa"},
			[]string{"b-"},
		},
		{
			"single character escapes", `[\-\[\]\^\\]\.\{\}\(\)\|\?\*\+`,
			[]string{"[.{}()|?*+", "].{}()|?*+", "^.{}()|?*+", "\\.{}()|?*+", "-.{}()|?*+"},
			[]string{"a.{}()|?*+"},
		},
		{
			"escaped dash as a range start", `[\--/]`,
			[]string{"-", ".", "/"},
			[]string{","},
		},
		{
			"caret inside a group is literal", `[a^]`,
			[]string{"a", "^"},
			[]string{"b"},
		},
		{
			"implicit anchoring", `[A-Z]{3}`,
			[]string{"EUR"},
			[]string{"EURO", "xEUR"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			re := patternFor(tc.pattern)
			if re == nil {
				_, err := translatePattern(tc.pattern)
				t.Fatalf("patternFor(%q) = nil (translation: %v), want a compiled pattern", tc.pattern, err)
			}
			for _, s := range tc.match {
				if !re.MatchString(s) {
					t.Errorf("%q does not match %q, want a match", tc.pattern, s)
				}
			}
			for _, s := range tc.reject {
				if re.MatchString(s) {
					t.Errorf("%q matches %q, want no match", tc.pattern, s)
				}
			}
		})
	}
}

// TestTranslatePattern_Refusals pins what translatePattern refuses and why:
// a string that is not an Appendix F regular expression, a construct it
// does not support, and a pattern past its anti-DoS guards. A refused
// pattern checks nothing (see validatePatterns), so a refusal must never
// stand in for a wrong match set.
func TestTranslatePattern_Refusals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pattern string
		want    error
	}{
		{"name start escape", `\i\c*`, errPatternUnsupported},
		{"name escape complement", `[\C]`, errPatternUnsupported},
		{"block escape", `\p{IsBasicLatin}+`, errPatternUnsupported},
		{"block escape in a class", `[\P{IsGreek}]`, errPatternUnsupported},
		{"unknown category", `\p{Xx}`, errPatternSyntax},
		{"surrogate category", `\p{Cs}`, errPatternSyntax},
		{"unterminated category", `\p{L`, errPatternSyntax},
		{"unknown escape", `\b`, errPatternSyntax},
		{"trailing backslash", `a\`, errPatternSyntax},
		{"two quantifiers", `a**`, errPatternSyntax},
		{"lazy quantifier", `a*?`, errPatternSyntax},
		{"quantifier without an atom", `*a`, errPatternSyntax},
		{"out of order quantity", `a{3,2}`, errPatternSyntax},
		{"unterminated quantity", `a{3`, errPatternSyntax},
		{"empty quantity", `a{}`, errPatternSyntax},
		{"stray close brace", `a}`, errPatternSyntax},
		{"unclosed group", `(a`, errPatternSyntax},
		{"unmatched close paren", `a)`, errPatternSyntax},
		{"Perl group syntax", `(?:a)`, errPatternSyntax},
		{"empty class", `[]`, errPatternSyntax},
		{"unterminated class", `[a-z`, errPatternSyntax},
		{"unescaped open bracket in a class", `[a[b]`, errPatternSyntax},
		{"dash inside a group", `[a-c-e]`, errPatternSyntax},
		{"out of order range", `[z-a]`, errPatternSyntax},
		{"range ending on a dash", `[+--]`, errPatternSyntax},
		{"range ending on a multi-character escape", `[a-\d]`, errPatternSyntax},
		{"subtraction not ending its group", `[a-z-[aeiou]b]`, errPatternSyntax},
		{"stray close bracket", `a]`, errPatternSyntax},
		{"overflowing count", `a{99999999999999999999}`, errPatternTooLarge},
		{"groups past the nesting guard", strings.Repeat("(", maxPatternNesting+1) + strings.Repeat(")", maxPatternNesting+1), errPatternTooLarge},
		{"classes past the nesting guard", strings.Repeat("[a-", maxPatternNesting+1) + "b" + strings.Repeat("]", maxPatternNesting+1), errPatternTooLarge},
		{"translation past the size guard", strings.Repeat(`[\w]`, 200), errPatternTooLarge},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := translatePattern(tc.pattern)
			if !errors.Is(err, tc.want) {
				t.Fatalf("translatePattern(%.40q) = %.60q, %v; want an error wrapping %v", tc.pattern, got, err, tc.want)
			}
			if patternFor(tc.pattern) != nil {
				t.Errorf("patternFor(%.40q) compiled a refused pattern", tc.pattern)
			}
		})
	}
}

// TestTranslatePattern_AtTheGuards is the control for the nesting guards: a
// pattern exactly at the bound still translates, so the guard refuses only
// what lies past it.
func TestTranslatePattern_AtTheGuards(t *testing.T) {
	t.Parallel()

	groups := strings.Repeat("(", maxPatternNesting) + "a" + strings.Repeat(")", maxPatternNesting)
	if _, err := translatePattern(groups); err != nil {
		t.Errorf("groups at the bound: %v", err)
	}
	classes := strings.Repeat("[a-z-", maxPatternNesting-1) + "[a]" + strings.Repeat("]", maxPatternNesting-1)
	if _, err := translatePattern(classes); err != nil {
		t.Errorf("classes at the bound: %v", err)
	}
}

// TestEscapeTables_Sets checks every entry of the Appendix F escape tables
// reads back as a non-empty set, and that each complement escape denotes
// exactly the code points its escape leaves out.
func TestEscapeTables_Sets(t *testing.T) {
	t.Parallel()

	pairs := map[string][2]goClassEscape{
		`\s`: {multiCharEscapes['s'], multiCharEscapes['S']},
		`\d`: {multiCharEscapes['d'], multiCharEscapes['D']},
		`\w`: {multiCharEscapes['w'], multiCharEscapes['W']},
	}
	for name, classes := range categoryEscapes {
		pairs[`\p{`+name+`}`] = classes
	}
	for name, pair := range pairs {
		in, err := pair[0].set()
		if err != nil || len(in) == 0 {
			t.Errorf("%s: set = %v, %v; want a non-empty set", name, in, err)
			continue
		}
		out, err := pair[1].set()
		if err != nil {
			t.Errorf("%s complement: %v", name, err)
			continue
		}
		if union := runeSet(slices.Concat(in, out)).normalize(); len(union) != 1 || union[0] != (runeRange{0, 0x10FFFF}) {
			t.Errorf("%s: escape and complement do not cover every code point: %v", name, union[:min(len(union), 4)])
		}
		if overlap := in.minus(out.complement()); len(overlap) != 0 {
			t.Errorf("%s: escape and complement share %v", name, overlap[:min(len(overlap), 4)])
		}
	}
}

// TestValidateValue_PatternSubtraction runs a subtraction pattern end to end
// through the facet engine: a two-letter code that leaves out I and O.
func TestValidateValue_PatternSubtraction(t *testing.T) {
	t.Parallel()

	st := &SimpleType{Base: builtin("token"), Facets: []Facet{Pattern{Value: `[A-Z-[IO]]{2}`}}}
	if got := ValidateValue("DE", st); len(got) != 0 {
		t.Errorf("DE: got %v, want no violation", got)
	}
	if got := ValidateValue("IO", st); len(got) != 1 {
		t.Errorf("IO: got %v, want one pattern violation", got)
	}
}
