package xsd

import (
	"cmp"
	"errors"
	"fmt"
	"regexp"
	"regexp/syntax"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// maxPatternNesting bounds how deeply [translatePattern] descends into
// parenthesized groups and subtracted character classes. It is an anti-DoS
// guard, not an Appendix F limit: the grammar nests without bound, and a
// pattern is read from a schema the caller may not control. 1000 is the
// nesting depth Go's regexp/syntax accepts itself, so the guard refuses no
// pattern Go could compile, and real schema patterns nest a few levels.
const maxPatternNesting = 1000

// maxPatternTranslationBytes caps the Go expression [translatePattern]
// emits. It is an anti-DoS guard, not spec-mandated: every character class
// expression is spelled out as explicit ranges, so a short pattern can
// translate to a long one, and a crafted pattern would otherwise make the
// translation, and the compile after it, arbitrarily large. The widest
// class a real pattern holds, the complement of a general category, is
// under 20 KiB, so 1 MiB leaves room for dozens of them.
const maxPatternTranslationBytes = 1 << 20

// Errors [translatePattern] reports. None escapes the package: a pattern
// that does not translate is not checked (see [validatePatterns]).
var (
	errPatternSyntax      = errors.New("not an XML Schema regular expression")
	errPatternUnsupported = errors.New("unsupported XML Schema regular expression construct")
	errPatternTooLarge    = errors.New("regular expression too large")
)

// translatePattern rewrites an XML Schema regular expression (XML Schema
// Part 2, Appendix F) as a Go regexp/syntax expression that denotes the
// same set of strings. The result is not anchored: a pattern matches a
// whole literal, so the caller wraps it.
//
// The two dialects differ in more than spelling. Appendix F has no anchors,
// so "^" and "$" are normal characters (production [10]); "." excludes the
// carriage return as well as the line feed (F.1.1); \d, \s and \w name
// Unicode sets where Go's name ASCII ones; and a character class may
// subtract another, [a-z-[aeiou]] (production [16]), which Go has no syntax
// for. Every character class expression is therefore computed as a set of
// ranges and written out explicitly.
//
// A block escape \p{IsX} whose name the Appendix F.1.1 block table does not
// list reports errPatternUnsupported: F.1.1 encourages blocks of later
// Unicode versions, which litho does not know.
//
// # Corpus evidence
//
//   - All corpora: no bundled schema declares an xs:pattern, so the name and
//     block escapes change no XSD verdict on the 159 files that embed XML.
func translatePattern(pattern string) (string, error) {
	t := &patternTranslator{src: []rune(pattern)}
	if err := t.regExp(); err != nil {
		return "", err
	}
	if t.pos < len(t.src) {
		// Only an unmatched ")" stops the top-level regExp early.
		return "", fmt.Errorf("%w: unmatched %q at offset %d", errPatternSyntax, t.src[t.pos], t.pos)
	}
	return t.out.String(), nil
}

// patternTranslator walks one pattern, writing the Go expression to out as
// it recognizes each production of Appendix F.
type patternTranslator struct {
	src   []rune
	pos   int
	depth int
	out   strings.Builder
}

func (t *patternTranslator) peek(offset int) (rune, bool) {
	if i := t.pos + offset; i < len(t.src) {
		return t.src[i], true
	}
	return 0, false
}

func (t *patternTranslator) peekIs(offset int, want rune) bool {
	c, ok := t.peek(offset)
	return ok && c == want
}

func (t *patternTranslator) accept(want rune) bool {
	if t.peekIs(0, want) {
		t.pos++
		return true
	}
	return false
}

// regExp reads productions [1] regExp ::= branch ( '|' branch )*.
func (t *patternTranslator) regExp() error {
	for {
		if err := t.branch(); err != nil {
			return err
		}
		if !t.accept('|') {
			return nil
		}
		t.out.WriteByte('|')
	}
}

// branch reads [2] branch ::= piece* and [3] piece ::= atom quantifier?.
func (t *patternTranslator) branch() error {
	for {
		c, ok := t.peek(0)
		if !ok || c == '|' || c == ')' {
			return nil
		}
		if err := t.atom(); err != nil {
			return err
		}
		if err := t.quantifier(); err != nil {
			return err
		}
		if t.out.Len() > maxPatternTranslationBytes {
			return fmt.Errorf("%w: translation exceeds %d bytes", errPatternTooLarge, maxPatternTranslationBytes)
		}
	}
}

// atom reads [9] atom ::= Char | charClass | '(' regExp ')'.
func (t *patternTranslator) atom() error {
	c := t.src[t.pos]
	t.pos++
	switch c {
	case '(':
		if t.depth >= maxPatternNesting {
			return fmt.Errorf("%w: groups nest deeper than %d", errPatternTooLarge, maxPatternNesting)
		}
		t.depth++
		t.out.WriteString("(?:")
		if err := t.regExp(); err != nil {
			return err
		}
		if !t.accept(')') {
			return fmt.Errorf("%w: unclosed group", errPatternSyntax)
		}
		t.out.WriteByte(')')
		t.depth--
		return nil
	case '[':
		set, err := t.charClassExpr()
		if err != nil {
			return err
		}
		t.out.WriteString(set.goClass())
		return nil
	case '\\':
		single, esc, err := t.escape()
		if err != nil {
			return err
		}
		if esc != "" {
			t.out.WriteString(string(esc))
			return nil
		}
		t.out.WriteString(regexp.QuoteMeta(string(single)))
		return nil
	case '.':
		// Per F.1.1 the wildcard escape is [^\n\r].
		t.out.WriteString(string(wildcardEscape))
		return nil
	case '?', '*', '+', '{', '}', ']':
		// Per Appendix F these are metacharacters, and none begins an
		// atom.
		return fmt.Errorf("%w: metacharacter %q at offset %d", errPatternSyntax, c, t.pos-1)
	}
	t.out.WriteString(regexp.QuoteMeta(string(c)))
	return nil
}

// quantifier reads [4] quantifier ::= [?*+] | ( '{' quantity '}' ), with
// quantity one of {n,m}, {n,} and {n} ([5] to [8]). A piece takes one
// quantifier at most, so "a**" and the Perl lazy "a*?" fail on the second.
func (t *patternTranslator) quantifier() error {
	c, ok := t.peek(0)
	if !ok {
		return nil
	}
	switch c {
	case '?', '*', '+':
		t.pos++
		t.out.WriteRune(c)
		return nil
	case '{':
	default:
		return nil
	}
	t.pos++
	lo, err := t.quantExact()
	if err != nil {
		return err
	}
	var spelled string
	switch {
	case !t.accept(','):
		spelled = fmt.Sprintf("{%d}", lo)
	case t.peekIs(0, '}'):
		spelled = fmt.Sprintf("{%d,}", lo)
	default:
		hi, err := t.quantExact()
		if err != nil {
			return err
		}
		if hi < lo {
			return fmt.Errorf("%w: quantifier {%d,%d} is out of order", errPatternSyntax, lo, hi)
		}
		spelled = fmt.Sprintf("{%d,%d}", lo, hi)
	}
	if !t.accept('}') {
		return fmt.Errorf("%w: unterminated quantifier", errPatternSyntax)
	}
	t.out.WriteString(spelled)
	return nil
}

// quantExact reads [8] QuantExact ::= [0-9]+. The value is re-spelled on
// output because Go reads a leading zero, legal here, as a literal brace.
func (t *patternTranslator) quantExact() (int, error) {
	start := t.pos
	for c, ok := t.peek(0); ok && c >= '0' && c <= '9'; c, ok = t.peek(0) {
		t.pos++
	}
	if t.pos == start {
		return 0, fmt.Errorf("%w: quantifier without a count", errPatternSyntax)
	}
	n, err := strconv.Atoi(string(t.src[start:t.pos]))
	if err != nil {
		return 0, fmt.Errorf("%w: quantifier count: %w", errPatternTooLarge, err)
	}
	return n, nil
}

// charClassExpr reads the rest of [12] charClassExpr ::= '[' charGroup ']'
// after its '[', with [13] charGroup ::= posCharGroup | negCharGroup |
// charClassSub, [15] negCharGroup ::= '^' posCharGroup and [16]
// charClassSub ::= ( posCharGroup | negCharGroup ) '-' charClassExpr.
//
// Per F.1 a "-" is a character range only at the beginning or end of a
// positive character group, "[" and "]" are never one unescaped, and a
// range s-e needs e at or after s.
func (t *patternTranslator) charClassExpr() (runeSet, error) {
	if t.depth >= maxPatternNesting {
		return nil, fmt.Errorf("%w: character classes nest deeper than %d", errPatternTooLarge, maxPatternNesting)
	}
	t.depth++
	defer func() { t.depth-- }()

	negated := t.accept('^')
	var set runeSet
	items := 0
	for {
		c, ok := t.peek(0)
		if !ok {
			return nil, fmt.Errorf("%w: unterminated character class", errPatternSyntax)
		}
		switch {
		case c == ']' && items > 0:
			t.pos++
			if negated {
				set = set.complement()
			}
			return set.normalize(), nil
		case c == '-' && t.peekIs(1, '[') && items > 0:
			t.pos += 2
			sub, err := t.charClassExpr()
			if err != nil {
				return nil, err
			}
			if !t.accept(']') {
				return nil, fmt.Errorf("%w: a subtracted class must end its group", errPatternSyntax)
			}
			if negated {
				set = set.complement()
			}
			return set.minus(sub), nil
		case c == '-':
			if items > 0 && !t.peekIs(1, ']') {
				return nil, fmt.Errorf("%w: '-' inside a character group at offset %d", errPatternSyntax, t.pos)
			}
			t.pos++
			set = append(set, runeRange{'-', '-'})
			items++
			continue
		case c == '[' || c == ']':
			return nil, fmt.Errorf("%w: unescaped %q in a character class at offset %d", errPatternSyntax, c, t.pos)
		}

		lo, esc, err := t.classAtom()
		if err != nil {
			return nil, err
		}
		items++
		if esc != nil {
			// Merge at once: an escape brings hundreds of ranges, and a
			// group repeating one must not grow the set with each copy.
			set = append(set, esc...).normalize()
			continue
		}
		hi := lo
		if next, ok := t.peek(1); ok && t.peekIs(0, '-') && next != ']' && next != '[' {
			t.pos++
			if t.peekIs(0, '-') {
				return nil, fmt.Errorf("%w: a range cannot end on '-' at offset %d", errPatternSyntax, t.pos)
			}
			end, endEsc, err := t.classAtom()
			if err != nil {
				return nil, err
			}
			if endEsc != nil {
				return nil, fmt.Errorf("%w: a range cannot end on a multi-character escape", errPatternSyntax)
			}
			if end < lo {
				return nil, fmt.Errorf("%w: range %q-%q is out of order", errPatternSyntax, lo, end)
			}
			hi = end
		}
		set = append(set, runeRange{lo, hi})
	}
}

// classAtom reads one character, or one character class escape, inside a
// character group. It returns the character, or the escape's set.
func (t *patternTranslator) classAtom() (rune, runeSet, error) {
	c := t.src[t.pos]
	t.pos++
	if c != '\\' {
		return c, nil, nil
	}
	single, esc, err := t.escape()
	if err != nil {
		return 0, nil, err
	}
	if esc == "" {
		return single, nil, nil
	}
	set, err := esc.set()
	if err != nil {
		return 0, nil, err
	}
	return 0, set, nil
}

// escape reads the rest of [23] charClassEsc after its backslash. A single
// character escape ([24]) returns its character; a multi-character ([37])
// or category ([25], [26]) escape returns the equivalent Go class.
func (t *patternTranslator) escape() (rune, goClassEscape, error) {
	c, ok := t.peek(0)
	if !ok {
		return 0, "", fmt.Errorf("%w: trailing backslash", errPatternSyntax)
	}
	t.pos++
	switch c {
	case 'n':
		return '\n', "", nil
	case 'r':
		return '\r', "", nil
	case 't':
		return '\t', "", nil
	case '\\', '|', '.', '?', '*', '+', '(', ')', '{', '}', '-', '[', ']', '^':
		return c, "", nil
	case 's', 'S', 'i', 'I', 'c', 'C', 'd', 'D', 'w', 'W':
		return 0, multiCharEscapes[c], nil
	case 'p', 'P':
		esc, err := t.categoryEscape(c == 'P')
		return 0, esc, err
	}
	return 0, "", fmt.Errorf("%w: unknown escape \\%c", errPatternSyntax, c)
}

// categoryEscape reads the rest of [25] catEsc ::= '\p{' charProp '}' or
// [26] complEsc ::= '\P{' charProp '}' after its letter.
func (t *patternTranslator) categoryEscape(complement bool) (goClassEscape, error) {
	if !t.accept('{') {
		return "", fmt.Errorf("%w: category escape without '{'", errPatternSyntax)
	}
	start := t.pos
	for c, ok := t.peek(0); ok && c != '}'; c, ok = t.peek(0) {
		t.pos++
	}
	name := string(t.src[start:t.pos])
	if !t.accept('}') {
		return "", fmt.Errorf("%w: unterminated category escape", errPatternSyntax)
	}
	// [27] charProp ::= IsCategory | IsBlock, and no category starts with
	// the "Is" of [36] IsBlock.
	block, isBlock := strings.CutPrefix(name, "Is")
	var classes [2]goClassEscape
	var known bool
	if isBlock {
		if classes, known = blockEscapes[block]; !known {
			return "", fmt.Errorf("%w: block escape \\p{%s} names no block of the Appendix F.1.1 table", errPatternUnsupported, name)
		}
	} else if classes, known = categoryEscapes[name]; !known {
		return "", fmt.Errorf("%w: unknown category %q", errPatternSyntax, name)
	}
	if complement {
		return classes[1], nil
	}
	return classes[0], nil
}

// goClassEscape is a Go regexp/syntax expression, one character class, that
// denotes the same set as an Appendix F escape. It is written as is outside
// a character class expression, and read back as a set inside one.
type goClassEscape string

// wildcardEscape is [37a] WildcardEsc: per F.1.1, "." is [^\n\r].
const wildcardEscape goClassEscape = `[^\n\r]`

// multiCharEscapes mirrors the multi-character escapes of Appendix F.1.1,
// production [37]. Go's own \d, \s and \w are ASCII, so none is reused:
//
//   - \s is [#x20\t\n\r].
//   - \d is \p{Nd}.
//   - \w is [#x0000-#x10FFFF]-[\p{P}\p{Z}\p{C}]. Every code point belongs to
//     exactly one general category, so that is L, M, N and S, plus the
//     surrogates of Cs, which no Go string decodes to.
//   - \i is Letter | '_' | ':', and \c is NameChar, from XML 1.0 (Second
//     Edition): productions [84] Letter ::= BaseChar | Ideographic and [4]
//     NameChar ::= Letter | Digit | '.' | '-' | '_' | ':' | CombiningChar |
//     Extender, over the Appendix B classes pattern_tables.go mirrors. Go's
//     unicode tables are not reused: they follow a later Unicode, and the
//     XML 1.0 classes were frozen on Unicode 2.0.
//
// Each upper-case escape is the complement of its lower-case one.
//
//nolint:gochecknoglobals // immutable spec table.
var multiCharEscapes = func() map[rune]goClassEscape {
	letter := slices.Concat(xmlBaseChar, xmlIdeographic)
	nameStart := slices.Concat(letter, runeSet{{'_', '_'}, {':', ':'}}).normalize()
	nameChar := slices.Concat(nameStart, xmlDigit, xmlCombiningChar, xmlExtender,
		runeSet{{'.', '.'}, {'-', '-'}}).normalize()
	return map[rune]goClassEscape{
		's': `[\t\n\r ]`,
		'S': `[^\t\n\r ]`,
		'i': goClassEscape(nameStart.goClass()),
		'I': goClassEscape(nameStart.complement().goClass()),
		'c': goClassEscape(nameChar.goClass()),
		'C': goClassEscape(nameChar.complement().goClass()),
		'd': `\p{Nd}`,
		'D': `\P{Nd}`,
		'w': `[\p{L}\p{M}\p{N}\p{S}]`,
		'W': `[^\p{L}\p{M}\p{N}\p{S}]`,
	}
}()

// blockEscapes holds the Go classes for \p{IsX} and \P{IsX}, keyed by the
// block name X of the Appendix F.1.1 table xsdBlocks mirrors. The ranges
// are the table's even where Unicode has since moved a block end, as it did
// for CJKUnifiedIdeographsExtensionA.
//
//nolint:gochecknoglobals // immutable spec table.
var blockEscapes = func() map[string][2]goClassEscape {
	out := make(map[string][2]goClassEscape, len(xsdBlocks))
	for name, set := range xsdBlocks {
		set = set.normalize()
		out[name] = [2]goClassEscape{goClassEscape(set.goClass()), goClassEscape(set.complement().goClass())}
	}
	return out
}()

// categoryEscapes mirrors the general categories of Appendix F.1.1,
// productions [28] to [35], as the Go classes for \p{X} and \P{X}. Go
// spells each category the same way. C and Cn are nonetheless spelled by
// complement over the other categories and Cc, Cf, Co and Cs: Appendix F's
// C is Cc, Cf, Co and the unassigned Cn, without the surrogates of Cs, and
// the complement holds that set whatever Go's own C and Cn tables contain.
// The sets follow the Unicode version Go ships, which F.1.1 encourages over
// the one current in 2004.
//
//nolint:gochecknoglobals // immutable spec table.
var categoryEscapes = func() map[string][2]goClassEscape {
	out := make(map[string][2]goClassEscape)
	for _, name := range []string{
		"L", "Lu", "Ll", "Lt", "Lm", "Lo",
		"M", "Mn", "Mc", "Me",
		"N", "Nd", "Nl", "No",
		"P", "Pc", "Pd", "Ps", "Pe", "Pi", "Pf", "Po",
		"Z", "Zs", "Zl", "Zp",
		"S", "Sm", "Sc", "Sk", "So",
		"Cc", "Cf", "Co",
	} {
		out[name] = [2]goClassEscape{
			goClassEscape(`\p{` + name + `}`),
			goClassEscape(`\P{` + name + `}`),
		}
	}
	const (
		notOther = `\p{L}\p{M}\p{N}\p{P}\p{S}\p{Z}`
		assigned = notOther + `\p{Cc}\p{Cf}\p{Co}\p{Cs}`
	)
	out["C"] = [2]goClassEscape{`[^` + notOther + `\p{Cs}]`, `[` + notOther + `\p{Cs}]`}
	out["Cn"] = [2]goClassEscape{`[^` + assigned + `]`, `[` + assigned + `]`}
	return out
}()

// set reads the escape back as a set of ranges, for use inside a character
// class expression.
func (e goClassEscape) set() (runeSet, error) {
	re, err := syntax.Parse(string(e), syntax.Perl)
	if err != nil {
		return nil, fmt.Errorf("%w: reading %s: %w", errPatternSyntax, e, err)
	}
	// Every entry of the escape tables parses to a class; a one-rune class
	// would come back as a literal.
	if re.Op == syntax.OpCharClass {
		set := make(runeSet, 0, len(re.Rune)/2)
		for i := 0; i+1 < len(re.Rune); i += 2 {
			set = append(set, runeRange{re.Rune[i], re.Rune[i+1]})
		}
		return set, nil
	}
	if re.Op == syntax.OpLiteral && len(re.Rune) == 1 {
		return runeSet{{re.Rune[0], re.Rune[0]}}, nil
	}
	return nil, fmt.Errorf("%w: %s is not a character class", errPatternSyntax, e)
}

// runeRange is the closed range of code points lo to hi.
type runeRange struct{ lo, hi rune }

// runeSet is a set of code points as a list of ranges. Only
// [runeSet.normalize] and the operations built on it promise a sorted list
// of disjoint, non-adjacent ranges.
type runeSet []runeRange

// normalize returns s as a sorted list of disjoint, non-adjacent ranges.
func (s runeSet) normalize() runeSet {
	if len(s) == 0 {
		return nil
	}
	sorted := slices.Clone(s)
	slices.SortFunc(sorted, func(a, b runeRange) int { return cmp.Compare(a.lo, b.lo) })
	out := sorted[:1]
	for _, r := range sorted[1:] {
		last := &out[len(out)-1]
		if r.lo <= last.hi+1 {
			last.hi = max(last.hi, r.hi)
			continue
		}
		out = append(out, r)
	}
	return out
}

// complement returns every code point s leaves out. Appendix F.1 takes a
// negative group's complement over the XML characters; the code points
// outside them never occur in a parsed document, so the full range serves.
func (s runeSet) complement() runeSet {
	var out runeSet
	next := rune(0)
	for _, r := range s.normalize() {
		if r.lo > next {
			out = append(out, runeRange{next, r.lo - 1})
		}
		next = r.hi + 1
	}
	if next <= unicode.MaxRune {
		out = append(out, runeRange{next, unicode.MaxRune})
	}
	return out
}

// minus returns the code points of s that are not in sub, the set F.1
// gives a character class subtraction.
func (s runeSet) minus(sub runeSet) runeSet {
	a, b := s.normalize(), sub.complement()
	var out runeSet
	for i, j := 0, 0; i < len(a) && j < len(b); {
		lo, hi := max(a[i].lo, b[j].lo), min(a[i].hi, b[j].hi)
		if lo <= hi {
			out = append(out, runeRange{lo, hi})
		}
		if a[i].hi < b[j].hi {
			i++
		} else {
			j++
		}
	}
	return out
}

// goClass spells s as one Go character class of explicit ranges. The empty
// set, which a subtraction can leave, is the class of no code point.
func (s runeSet) goClass() string {
	if len(s) == 0 {
		return `[^\x{0}-\x{10ffff}]`
	}
	var b strings.Builder
	b.WriteByte('[')
	for _, r := range s {
		fmt.Fprintf(&b, `\x{%x}`, r.lo)
		if r.hi != r.lo {
			fmt.Fprintf(&b, `-\x{%x}`, r.hi)
		}
	}
	b.WriteByte(']')
	return b.String()
}
