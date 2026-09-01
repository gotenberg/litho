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
// every facet collected up the chain in source order. Returns nil
// when the value passes every check.
//
// Whitespace facet normalisation runs before the lexical validator so
// downstream facets see the same whitespace-collapsed form the lexical
// space expects.
func ValidateValue(value string, t Type) []FacetViolation {
	if t == nil {
		return nil
	}
	facets, base := collectAllFacets(t)
	normalized := applyWhitespace(value, facets)

	var out []FacetViolation
	if base != nil {
		if v := validateLexical(normalized, base); v != nil {
			out = append(out, *v)
		}
	}
	// Enumeration is an allow-list: every Enumeration facet contributes
	// to a single check, so evaluate them once outside the per-facet
	// loop below.
	if v := validateEnumeration(normalized, facets); v != nil {
		out = append(out, *v)
	}
	for _, f := range facets {
		switch f.(type) {
		case WhiteSpace, Enumeration:
			continue
		}
		if v := applyFacet(normalized, f, base); v != nil {
			out = append(out, *v)
		}
	}
	return out
}

// collectAllFacets walks t's base chain through SimpleType
// restrictions, accumulating facets in the conventional "base first,
// most-derived last" order so the most-derived restriction wins when
// XSD lets a redeclaration tighten (e.g. a deeper maxLength). Returns
// the underlying built-in type the chain bottoms out on.
func collectAllFacets(t Type) ([]Facet, *BuiltinType) {
	var ordered []Facet
	cur := t
	for cur != nil {
		switch ct := cur.(type) {
		case *BuiltinType:
			return ordered, ct
		case *SimpleType:
			// Walk base first so we collect upstream facets ahead of
			// the local ones, matching XSD's "most-derived wins" rule
			// when both layers declare the same facet kind.
			upstream, base := collectAllFacets(ct.Base)
			out := make([]Facet, 0, len(upstream)+len(ct.Facets))
			out = append(out, upstream...)
			out = append(out, ct.Facets...)
			return out, base
		default:
			return ordered, nil
		}
	}
	return ordered, nil
}

// applyWhitespace honours the xs:whiteSpace facet semantics:
// "preserve" leaves value untouched, "replace" maps tab/cr/lf to
// space, "collapse" additionally collapses runs of spaces and trims.
// When multiple WhiteSpace facets appear, the last one wins (which
// matches XSD's "most-derived restriction" rule under the collect
// order above).
func applyWhitespace(value string, facets []Facet) string {
	mode := ""
	for _, f := range facets {
		if ws, ok := f.(WhiteSpace); ok {
			mode = ws.Value
		}
	}
	switch mode {
	case "replace":
		return whitespaceReplace(value)
	case "collapse":
		return whitespaceCollapse(value)
	default:
		return value
	}
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
// kinds (or those PR2 chooses not to enforce) return nil so the
// engine remains forward-compatible with future facet additions.
// base is retained for the facet kinds that must validate against the base
// type (minInclusive and friends); the kinds handled below do not need it.
func applyFacet(value string, f Facet, _base *BuiltinType) *FacetViolation {
	switch fc := f.(type) {
	case Pattern:
		return validatePattern(value, fc)
	case Enumeration:
		// Enumeration is collected per entry; the caller folds them
		// into an allow-list check below.
		return nil
	case Length:
		return validateExactLength(value, fc.Value, f)
	case MinLength:
		return validateMinLength(value, fc.Value, f)
	case MaxLength:
		return validateMaxLength(value, fc.Value, f)
	case MinInclusive:
		return validateNumericBound(value, fc.Value, boundMinInclusive, f)
	case MaxInclusive:
		return validateNumericBound(value, fc.Value, boundMaxInclusive, f)
	case MinExclusive:
		return validateNumericBound(value, fc.Value, boundMinExclusive, f)
	case MaxExclusive:
		return validateNumericBound(value, fc.Value, boundMaxExclusive, f)
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

func validatePattern(value string, fc Pattern) *FacetViolation {
	re := patternFor(fc.Value)
	if re == nil {
		// Treat an uncompilable pattern as a permissive match. A real
		// XSD validator would reject the schema; the loader has no
		// regex compile step in PR1, so the cost surfaces here and
		// we err on the side of false negatives rather than runtime
		// panics.
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
	// XSD patterns are implicitly anchored at both ends; Go's regexp
	// package matches anywhere by default, so wrap.
	re, err := regexp.Compile("^(?:" + raw + ")$")
	if err != nil {
		return nil
	}
	patternCache.Store(raw, re)
	return re
}

// validateEnumeration runs the allow-list check across every
// Enumeration facet supplied. Exposed separately from [applyFacet]
// because the allow-list semantics need to see the full set of
// enumeration values at once.
func validateEnumeration(value string, facets []Facet) *FacetViolation {
	var values []string
	for _, f := range facets {
		if e, ok := f.(Enumeration); ok {
			values = append(values, e.Value)
			if e.Value == value {
				return nil
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

func validateNumericBound(value, bound string, kind boundKind, fc Facet) *FacetViolation {
	got, ok := parseDecimal(value)
	if !ok {
		return &FacetViolation{
			Facet: fc, Value: value,
			Reason: "value is not a numeric literal",
		}
	}
	limit, ok := parseDecimal(bound)
	if !ok {
		// Schema-side bug; surface as a permissive miss rather than
		// panic. The XSD loader does not type-check facet values yet.
		return nil
	}
	cmp := got.Cmp(limit)
	switch kind {
	case boundMinInclusive:
		if cmp < 0 {
			return &FacetViolation{
				Facet: fc, Value: value,
				Reason: fmt.Sprintf("value %s below minimum %s", value, bound),
			}
		}
	case boundMaxInclusive:
		if cmp > 0 {
			return &FacetViolation{
				Facet: fc, Value: value,
				Reason: fmt.Sprintf("value %s above maximum %s", value, bound),
			}
		}
	case boundMinExclusive:
		if cmp <= 0 {
			return &FacetViolation{
				Facet: fc, Value: value,
				Reason: fmt.Sprintf("value %s at or below exclusive minimum %s", value, bound),
			}
		}
	case boundMaxExclusive:
		if cmp >= 0 {
			return &FacetViolation{
				Facet: fc, Value: value,
				Reason: fmt.Sprintf("value %s at or above exclusive maximum %s", value, bound),
			}
		}
	}
	return nil
}

// parseDecimal accepts the XSD decimal lexical form (optional sign,
// integer part, optional fractional part). Returns a *big.Rat so
// bounds comparisons stay exact for invoice totals where IEEE float
// rounding would distort verdicts.
func parseDecimal(s string) (*big.Rat, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, false
	}
	r := new(big.Rat)
	if _, ok := r.SetString(s); ok {
		return r, true
	}
	return nil, false
}

func validateFractionDigits(value string, maxDigits int, fc Facet) *FacetViolation {
	digits := fractionalDigitCount(value)
	if digits <= maxDigits {
		return nil
	}
	return &FacetViolation{
		Facet: fc, Value: value,
		Reason: fmt.Sprintf("%d fractional digits exceed maximum %d", digits, maxDigits),
	}
}

func validateTotalDigits(value string, maxDigits int, fc Facet) *FacetViolation {
	digits := significantDigitCount(value)
	if digits <= maxDigits {
		return nil
	}
	return &FacetViolation{
		Facet: fc, Value: value,
		Reason: fmt.Sprintf("%d significant digits exceed maximum %d", digits, maxDigits),
	}
}

func fractionalDigitCount(s string) int {
	s = strings.TrimSpace(s)
	idx := strings.IndexByte(s, '.')
	if idx < 0 {
		return 0
	}
	count := 0
	for i := idx + 1; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			count++
		}
	}
	return count
}

func significantDigitCount(s string) int {
	s = strings.TrimSpace(s)
	s = strings.TrimLeft(s, "+-")
	count := 0
	leadingZeros := true
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '.' {
			leadingZeros = false
			continue
		}
		if c < '0' || c > '9' {
			continue
		}
		if leadingZeros && c == '0' {
			continue
		}
		leadingZeros = false
		count++
	}
	// Trailing zeros after the decimal point still count toward total
	// digits per XSD, which our straight-scan above already includes.
	return count
}
