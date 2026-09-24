package xsd

import (
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"
)

// FacetViolation captures one constraint failure during value
// validation. The Facet field carries the constraint that fired and
// Reason is a short human-readable explanation for diagnostics.
//
//nolint:errname // mirrors the compliance.Violation precedent; "ViolationError" would read redundantly.
type FacetViolation struct {
	Facet  Facet
	Value  string
	Reason string
}

// Error implements the error interface so a violation can be returned
// from validator entry points alongside other errors.
func (v FacetViolation) Error() string {
	return fmt.Sprintf("xsd facet violation on %q: %s", v.Value, v.Reason)
}

// ValidateValue checks value against the lexical space and all facets
// of t. The function walks t's restriction chain, picks the underlying
// built-in lexical validator, runs it against value, then evaluates
// the facets of every step of the chain, base first. Returns nil
// when the value passes every check.
//
// White space is normalized first, under the mode [effectiveWhiteSpace]
// derives, and every check then sees the normalized value: per XML Schema
// Part 1, 3.1.4 it is the normalized value that is validated. "A\nB" and
// "A  B" as xs:token are therefore the valid value "A B".
func ValidateValue(value string, t Type) []FacetViolation {
	if t == nil {
		return nil
	}
	steps, base := collectAllFacets(t)
	normalized := applyWhitespace(value, steps, base)

	var out []FacetViolation
	if base != nil {
		if v := validateLexical(normalized, base); v != nil {
			out = append(out, *v)
		}
	}
	// Enumeration is an allow-list: the Enumeration facets of a step
	// form one set, so they are evaluated together outside the per-facet
	// loop below.
	if v := validateEnumeration(normalized, steps); v != nil {
		out = append(out, *v)
	}
	for _, step := range steps {
		for _, f := range step {
			if v := applyFacet(normalized, f, base); v != nil {
				out = append(out, *v)
			}
		}
	}
	return out
}

// collectAllFacets walks t's base chain through SimpleType
// restrictions and returns the facets of each derivation step, in the
// conventional "base first, most-derived last" order, plus the
// underlying built-in type the chain bottoms out on. The steps stay
// apart because XML Schema Part 2 combines facets differently within a
// step and across steps: the enumerations of one step form one set
// (4.3.5.3), and its patterns are alternatives (4.3.4.3).
//
// The walk is iterative and bounded by [maxTypeDerivationDepth]. A
// schema may declare a simpleType that restricts itself, directly or
// through another type, and the loader resolves such a base without
// complaint: the recursive form this replaced then exhausted the
// goroutine stack, which Go cannot recover from. Like the extension
// walks in validate.go the bound TRUNCATES rather than reports, so a
// pathological chain yields the facets gathered so far.
func collectAllFacets(t Type) ([][]Facet, *BuiltinType) {
	var chain []*SimpleType
	var base *BuiltinType

	cur := t
	for depth := 0; cur != nil && depth < maxTypeDerivationDepth; depth++ {
		st, ok := cur.(*SimpleType)
		if !ok {
			// A built-in ends the chain and names the lexical space.
			// Any other kind ends it with no lexical space at all.
			if bt, isBuiltin := cur.(*BuiltinType); isBuiltin {
				base = bt
			}
			break
		}
		// Prepend so the chain reads base first, which is the order the
		// facets must be applied in for "most-derived wins" to hold.
		chain = append([]*SimpleType{st}, chain...)
		cur = st.Base
	}

	steps := make([][]Facet, 0, len(chain))
	for _, link := range chain {
		steps = append(steps, link.Facets)
	}
	return steps, base
}

// applyWhitespace normalizes value under the mode [effectiveWhiteSpace]
// derives: per XML Schema Part 2, 4.3.6, preserve leaves it untouched,
// replace maps tab, line feed and carriage return to space, and collapse
// also joins runs of spaces and trims both ends.
func applyWhitespace(value string, steps [][]Facet, base *BuiltinType) string {
	switch effectiveWhiteSpace(steps, base) {
	case whiteSpaceReplace:
		return whitespaceReplace(value)
	case whiteSpaceCollapse:
		return whitespaceCollapse(value)
	case whiteSpacePreserve:
	}
	return value
}

// effectiveWhiteSpace returns the whiteSpace in force for a type whose
// restriction chain has steps and bottoms out on base: the mode the
// built-in fixes ([builtinWhiteSpace]), tightened by every whiteSpace facet
// the chain declares.
//
// Per XML Schema Part 2, 4.3.6.4 a step may not loosen the mode of the step
// it restricts, so on a valid schema the strongest mode is the most-derived
// one. A schema that loosens it anyway, preserve under token for instance,
// is in error. The looser facet is then ignored rather than blamed on the
// instance, as is a value outside the three 4.3.6 allows.
func effectiveWhiteSpace(steps [][]Facet, base *BuiltinType) whiteSpaceMode {
	mode := builtinWhiteSpace(base)
	for _, step := range steps {
		for _, f := range step {
			ws, ok := f.(WhiteSpace)
			if !ok {
				continue
			}
			if declared, known := parseWhiteSpaceMode(ws.Value); known && declared > mode {
				mode = declared
			}
		}
	}
	return mode
}

func whitespaceReplace(v string) string {
	out := make([]byte, 0, len(v))
	for i := 0; i < len(v); i++ {
		switch v[i] {
		case '\t', '\n', '\r':
			out = append(out, ' ')
		default:
			out = append(out, v[i])
		}
	}
	return string(out)
}

func whitespaceCollapse(v string) string {
	replaced := whitespaceReplace(v)
	var b strings.Builder
	b.Grow(len(replaced))
	prevSpace := true
	for i := 0; i < len(replaced); i++ {
		if replaced[i] == ' ' {
			if !prevSpace {
				b.WriteByte(' ')
			}
			prevSpace = true
			continue
		}
		b.WriteByte(replaced[i])
		prevSpace = false
	}
	out := b.String()
	return strings.TrimRight(out, " ")
}

// applyFacet dispatches to the per-kind validator. Unknown facet
// kinds return nil so the engine remains forward-compatible with
// future facet additions. WhiteSpace and Enumeration return nil too:
// [ValidateValue] applies them across a whole chain.
func applyFacet(value string, f Facet, base *BuiltinType) *FacetViolation {
	switch fc := f.(type) {
	case Pattern:
		return validatePattern(value, fc)
	case WhiteSpace, Enumeration:
		return nil
	case Length:
		return validateExactLength(value, fc.Value, f)
	case MinLength:
		return validateMinLength(value, fc.Value, f)
	case MaxLength:
		return validateMaxLength(value, fc.Value, f)
	case MinInclusive:
		return validateRangeBound(value, fc.Value, boundMinInclusive, base, f)
	case MaxInclusive:
		return validateRangeBound(value, fc.Value, boundMaxInclusive, base, f)
	case MinExclusive:
		return validateRangeBound(value, fc.Value, boundMinExclusive, base, f)
	case MaxExclusive:
		return validateRangeBound(value, fc.Value, boundMaxExclusive, base, f)
	case FractionDigits:
		return validateFractionDigits(value, fc.Value, f)
	case TotalDigits:
		return validateTotalDigits(value, fc.Value, f)
	}
	return nil
}

// patternCache memoises compiled regular expressions across calls so
// repeated validation against the same SimpleType does not pay the
// regexp compile cost per value. The cache keys on the raw pattern
// source, never the compiled regex pointer.
//
//nolint:gochecknoglobals // process-wide compile cache.
var patternCache sync.Map // map[string]*regexp.Regexp

// validatePattern applies XML Schema Part 2, 4.3.4.4: a literal is
// facet-valid when it is among the strings the regular expression denotes.
//
// TODO: 4.3.4.3 ORs the patterns of one derivation step ("combined as if
// they appeared in a single regular expression as separate branches") and
// ANDs only those of different steps. Every Pattern is checked on its own
// here, so a value matching one of two same-step patterns is reported.
func validatePattern(value string, fc Pattern) *FacetViolation {
	re := patternFor(fc.Value)
	if re == nil {
		// Treat an uncompilable pattern as a permissive match. A real
		// XSD validator would reject the schema; the loader has no
		// regex compile step, so the cost surfaces here and the
		// validator errs on the side of false negatives rather than
		// runtime panics.
		//
		// Deliberate leniency: 4.3.4.2 requires {value} to be a valid
		// regular expression. Go's RE2 syntax is not the Appendix F
		// dialect either, so the escapes \i and \c and block escapes such
		// as \p{IsBasicLatin} fail to compile and land here. Character
		// class subtraction compiles with another meaning: [a-z-[aeiou]]
		// matches "a]" and rejects "b".
		return nil
	}
	if !re.MatchString(value) {
		return &FacetViolation{
			Facet:  fc,
			Value:  value,
			Reason: fmt.Sprintf("does not match pattern %q", fc.Value),
		}
	}
	return nil
}

func patternFor(raw string) *regexp.Regexp {
	if cached, ok := patternCache.Load(raw); ok {
		return cached.(*regexp.Regexp)
	}
	// XSD patterns are implicitly anchored at both ends (XML Schema Part
	// 2, Appendix F); Go's regexp package matches anywhere by default, so
	// wrap.
	re, err := regexp.Compile("^(?:" + raw + ")$")
	if err != nil {
		return nil
	}
	patternCache.Store(raw, re)
	return re
}

// validateEnumeration runs the allow-list check of a restriction chain.
// Exposed separately from [applyFacet] because the allow-list semantics
// need to see the full set of enumeration values at once.
//
// XML Schema Part 2, 4.3.5.3 makes the enumerations of one step a single
// set, and 4.3.5.4 accepts a value that is one of them. The set of the
// most-derived step that declares one decides alone: 4.3.5 restricts the
// value space of that step's type to its set, and 4.3.5.5 makes it an error
// for the set to reach outside the value space of its base, which the
// enumerations of the steps below already restrict. On a valid schema the
// most-derived set is therefore the intersection of all of them, and a
// member a schema adds beyond its base's set is the schema's error, not the
// instance's.
//
// The comparison is on literals, which matches values for the
// string-derived types CII enumerates; a numeric enumeration of 1 would
// reject 1.0.
func validateEnumeration(value string, steps [][]Facet) *FacetViolation {
	var values []string
	for i := len(steps) - 1; i >= 0 && len(values) == 0; i-- {
		for _, f := range steps[i] {
			if e, ok := f.(Enumeration); ok {
				values = append(values, e.Value)
				if e.Value == value {
					return nil
				}
			}
		}
	}
	if len(values) == 0 {
		return nil
	}
	return &FacetViolation{
		Facet:  Enumeration{Value: value},
		Value:  value,
		Reason: fmt.Sprintf("value not in enumeration %v", values),
	}
}

// validateExactLength applies XML Schema Part 2, 4.3.1. validateMinLength
// and validateMaxLength apply 4.3.2 and 4.3.3 the same way. For string and
// anyURI all three count characters, which the rune count gives. The octet
// count 4.3.1 prescribes for hexBinary and base64Binary is not implemented.
func validateExactLength(value string, want int, fc Facet) *FacetViolation {
	if utf8.RuneCountInString(value) == want {
		return nil
	}
	return &FacetViolation{
		Facet: fc, Value: value,
		Reason: fmt.Sprintf("length %d does not equal required %d",
			utf8.RuneCountInString(value), want),
	}
}

func validateMinLength(value string, minLen int, fc Facet) *FacetViolation {
	got := utf8.RuneCountInString(value)
	if got >= minLen {
		return nil
	}
	return &FacetViolation{
		Facet: fc, Value: value,
		Reason: fmt.Sprintf("length %d below minimum %d", got, minLen),
	}
}

func validateMaxLength(value string, maxLen int, fc Facet) *FacetViolation {
	got := utf8.RuneCountInString(value)
	if got <= maxLen {
		return nil
	}
	return &FacetViolation{
		Facet: fc, Value: value,
		Reason: fmt.Sprintf("length %d above maximum %d", got, maxLen),
	}
}

type boundKind int

const (
	boundMinInclusive boundKind = iota
	boundMaxInclusive
	boundMinExclusive
	boundMaxExclusive
)

// validateRangeBound checks value against one range facet. The value
// and the bound compare in the value space of base (see
// [compareRange]), so a date bound compares chronologically and a
// numeric one numerically, per XML Schema Part 2, 4.3.7.3 to 4.3.10.3.
func validateRangeBound(value, bound string, kind boundKind, base *BuiltinType, fc Facet) *FacetViolation {
	cmp, status := compareRange(value, bound, base)
	switch status {
	case rangeCompared:
	case rangeNotApplicable, rangeBadBound:
		// Schema-side bug: a range facet on an unordered base (Part 2,
		// 4.1.5) or a bound outside the base's value space (4.3.7.1 to
		// 4.3.10.1). Surface it as a permissive miss rather than blame
		// the instance. The XSD loader does not type-check facet values
		// yet.
		return nil
	case rangeBeyondLimit:
		// A valid literal litho cannot place (see [maxTemporalDigits]):
		// no order the spec defines backs a violation.
		return nil
	case rangeBadValue:
		return &FacetViolation{
			Facet: fc, Value: value,
			Reason: fmt.Sprintf("value is not an xs:%s literal", base.Name.Local),
		}
	}
	if cmp == orderIncomparable {
		// Per Part 2, 4.2.2 an incomparable value is neither less, equal
		// nor greater, so it satisfies none of the four bounds: 3.2.4 and
		// 3.2.5 exclude it from the restricted value space, and 3.2.6.3
		// counts an indeterminate duration comparison as false. The same
		// holds for a timezoned and a non-timezoned date/time value at
		// most 14 hours apart (3.2.7.4).
		return &FacetViolation{
			Facet: fc, Value: value,
			Reason: fmt.Sprintf("value %s is incomparable with bound %s", value, bound),
		}
	}
	switch kind {
	case boundMinInclusive:
		if cmp == orderLess {
			return &FacetViolation{
				Facet: fc, Value: value,
				Reason: fmt.Sprintf("value %s below minimum %s", value, bound),
			}
		}
	case boundMaxInclusive:
		if cmp == orderGreater {
			return &FacetViolation{
				Facet: fc, Value: value,
				Reason: fmt.Sprintf("value %s above maximum %s", value, bound),
			}
		}
	case boundMinExclusive:
		if cmp != orderGreater {
			return &FacetViolation{
				Facet: fc, Value: value,
				Reason: fmt.Sprintf("value %s at or below exclusive minimum %s", value, bound),
			}
		}
	case boundMaxExclusive:
		if cmp != orderLess {
			return &FacetViolation{
				Facet: fc, Value: value,
				Reason: fmt.Sprintf("value %s at or above exclusive maximum %s", value, bound),
			}
		}
	}
	return nil
}

// parseDecimal reads s as a literal of the decimal lexical space, XML
// Schema Part 2, 3.2.3.1: an optional sign, then digits with at most one
// period (see [isDecimalNumeral]). Returns a *big.Rat so bounds comparisons
// stay exact for invoice totals where IEEE float rounding would distort
// verdicts.
//
// The value is built from the digits rather than by big.Rat.SetString,
// which also reads an exponent ("1e5"), a fraction ("1/2"), a base prefix
// ("0x10") and underscores, none of them a decimal literal. s must already
// be white-space normalized, which [ValidateValue] and [compareRange] do.
func parseDecimal(s string) (*big.Rat, bool) {
	if !isDecimalNumeral(s) {
		return nil, false
	}
	whole, fraction, _ := strings.Cut(trimSign(s), ".")
	num, ok := new(big.Int).SetString(whole+fraction, 10)
	if !ok {
		return nil, false
	}
	if strings.HasPrefix(s, "-") {
		num.Neg(num)
	}
	den := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(len(fraction))), nil)
	return new(big.Rat).SetFrac(num, den), true
}

// validateFractionDigits applies XML Schema Part 2, 4.3.12.3: the value is
// expressible as i × 10^-n with 0 <= n <= fractionDigits. Per 4.3.12 the
// facet bounds the value, not the literal, so trailing fractional zeros are
// "still permitted": "1.50" is 1.5 and passes fractionDigits 1.
func validateFractionDigits(value string, maxDigits int, fc Facet) *FacetViolation {
	digits, ok := decimalDigitCounts(value)
	if !ok || digits.fraction <= maxDigits {
		return nil
	}
	return &FacetViolation{
		Facet: fc, Value: value,
		Reason: fmt.Sprintf("%d fractional digits exceed maximum %d", digits.fraction, maxDigits),
	}
}

// validateTotalDigits applies XML Schema Part 2, 4.3.11.3: the value is
// expressible as i × 10^-n with |i| < 10^totalDigits and
// 0 <= n <= totalDigits. Per 4.3.11 leading zeros and trailing fractional
// zeros are "still permitted": "1.000" is 1 and passes totalDigits 1.
func validateTotalDigits(value string, maxDigits int, fc Facet) *FacetViolation {
	digits, ok := decimalDigitCounts(value)
	if !ok || digits.total <= maxDigits {
		return nil
	}
	return &FacetViolation{
		Facet: fc, Value: value,
		Reason: fmt.Sprintf("%d significant digits exceed maximum %d", digits.total, maxDigits),
	}
}

// digitCounts holds the two digit counts the decimal digit facets bound.
type digitCounts struct {
	// fraction is the least n for which the value is i × 10^-n: the
	// fractional digits once trailing zeros are dropped.
	fraction int
	// total is the least totalDigits the value satisfies: the integer
	// digits once leading zeros are dropped, plus fraction. It is at least
	// fraction, which 4.3.11.3 also bounds, and 0 for the value zero.
	total int
}

// decimalDigitCounts reads the counts off a decimal literal (XML Schema Part
// 2, 3.2.3.1). It reports false when value is not one: 4.1.5 applies the
// digit facets to types derived from decimal alone, whose lexical check then
// reports the value, and a digit facet on any other type is a schema error
// the instance is not blamed for.
func decimalDigitCounts(value string) (digitCounts, bool) {
	if !isDecimalNumeral(value) {
		return digitCounts{}, false
	}
	whole, fraction, _ := strings.Cut(trimSign(value), ".")
	whole = strings.TrimLeft(whole, "0")
	fraction = strings.TrimRight(fraction, "0")
	return digitCounts{fraction: len(fraction), total: len(whole) + len(fraction)}, true
}
