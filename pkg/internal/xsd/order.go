package xsd

import (
	"errors"
	"math"
	"math/big"
	"strconv"
	"strings"
)

// order is the outcome of comparing two members of one ordered value
// space. XML Schema Part 2, 4.2.2 lets the order be partial, so a pair
// can also be incomparable: neither less, equal nor greater (a <> b).
type order int

const (
	orderLess order = iota
	orderEqual
	orderGreater
	orderIncomparable
)

// orderOf maps a three-way comparison result onto [order].
func orderOf(cmp int) order {
	switch {
	case cmp < 0:
		return orderLess
	case cmp > 0:
		return orderGreater
	default:
		return orderEqual
	}
}

// rangeStatus reports whether [compareRange] could place a value and a
// bound in one value space, and why not when it could not.
type rangeStatus int

const (
	// rangeCompared means both parsed and the returned order holds.
	rangeCompared rangeStatus = iota
	// rangeNotApplicable means the base has no order relation, or the
	// restriction chain names no built-in, so no range facet applies.
	rangeNotApplicable
	// rangeBadBound means the schema's bound is not a member of the
	// base's value space, or lies beyond litho's documented limits.
	rangeBadBound
	// rangeBadValue means the value is outside the base's lexical space.
	rangeBadValue
	// rangeBeyondLimit means the value is a valid literal that lies
	// beyond litho's documented limits (see [maxTemporalDigits]).
	rangeBeyondLimit
)

// compareRange orders value against bound in the value space of base,
// the built-in a simpleType's restriction chain bottoms out on. Per XML
// Schema Part 2, 4.3.7.3 to 4.3.10.3, a range facet compares
// numerically when the base is numeric and chronologically when it is
// a date/time type, so the comparison follows the base's primitive:
// decimal and its derived integer types (3.2.3, 3.3.13 to 3.3.25),
// float (3.2.4), double (3.2.5), duration (3.2.6) and the eight
// date/time types (3.2.7 to 3.2.14).
//
// Every ordered primitive fixes whiteSpace to collapse (4.3.6), so both
// literals are trimmed before parsing. The bound comes raw off the XSD
// attribute.
func compareRange(value, bound string, base *BuiltinType) (order, rangeStatus) {
	if base == nil || base.Name.Namespace != XSNamespace {
		return orderIncomparable, rangeNotApplicable
	}
	value, bound = strings.TrimSpace(value), strings.TrimSpace(bound)
	switch base.Name.Local {
	case "decimal", "integer", "nonNegativeInteger", "positiveInteger",
		"nonPositiveInteger", "negativeInteger", "long", "int", "short",
		"byte", "unsignedLong", "unsignedInt", "unsignedShort", "unsignedByte":
		return compareParsed(value, bound, parseDecimalValue, compareDecimal)
	case "float":
		return compareParsed(value, bound, parseFloat32Value, compareFloat)
	case "double":
		return compareParsed(value, bound, parseFloat64Value, compareFloat)
	case "duration":
		return compareParsed(value, bound, parseDurationValue, compareDuration)
	}
	if kind, ok := temporalKindOf(base.Name.Local); ok {
		parse := func(s string) (temporalValue, rangeStatus) {
			return parseTemporalValue(s, kind)
		}
		return compareParsed(value, bound, parse, compareTemporal)
	}
	// Per XML Schema Part 2, 4.1.5 (applicable facets), the range
	// facets apply only to ordered primitives. The string family,
	// boolean, the binary types, anyURI, QName and anySimpleType have
	// no order relation, so a range facet on them is a schema error.
	return orderIncomparable, rangeNotApplicable
}

// compareParsed parses the bound and then the value with parse, and
// orders them with cmp. A bound that fails to parse is reported before
// the value is looked at: the schema is wrong whatever the value.
func compareParsed[T any](value, bound string, parse func(string) (T, rangeStatus), cmp func(a, b T) order) (order, rangeStatus) {
	limit, status := parse(bound)
	if status != rangeCompared {
		return orderIncomparable, rangeBadBound
	}
	got, status := parse(value)
	if status != rangeCompared {
		return orderIncomparable, status
	}
	return cmp(got, limit), rangeCompared
}

func parseDecimalValue(s string) (*big.Rat, rangeStatus) {
	r, ok := parseDecimal(s)
	if !ok {
		return nil, rangeBadValue
	}
	return r, rangeCompared
}

// compareDecimal orders two decimals. Per XML Schema Part 2, 3.2.3,
// decimal is totally ordered, so the result is never incomparable.
func compareDecimal(a, b *big.Rat) order { return orderOf(a.Cmp(b)) }

func parseFloat32Value(s string) (float64, rangeStatus) { return parseFloatLiteral(s, 32) }

func parseFloat64Value(s string) (float64, rangeStatus) { return parseFloatLiteral(s, 64) }

// parseFloatLiteral parses s in the float (bitSize 32, XML Schema Part
// 2, 3.2.4.1) or double (bitSize 64, 3.2.5.1) lexical space: a decimal
// mantissa with an optional E or e exponent, or one of INF, -INF and
// NaN. Version 1.0 has no +INF, and strconv.ParseFloat's own
// spellings (inf, Infinity, hexadecimal mantissas, underscores) are
// not literals of either type, so the shape is checked here first.
func parseFloatLiteral(s string, bitSize int) (float64, rangeStatus) {
	switch s {
	case "INF":
		return math.Inf(1), rangeCompared
	case "-INF":
		return math.Inf(-1), rangeCompared
	case "NaN":
		return math.NaN(), rangeCompared
	}
	if !isFloatNumeral(s) {
		return 0, rangeBadValue
	}
	f, err := strconv.ParseFloat(s, bitSize)
	if err != nil {
		if !errors.Is(err, strconv.ErrRange) {
			return 0, rangeBadValue
		}
		// Per 3.2.4 and 3.2.5, a literal maps to the normalized value
		// closest to it. ParseFloat rounds an overflowing magnitude to
		// an infinity instead, so pin it to the largest finite value.
		f = math.Copysign(math.MaxFloat64, f)
		if bitSize == 32 {
			f = math.Copysign(math.MaxFloat32, f)
		}
	}
	return f, rangeCompared
}

// isFloatNumeral reports whether s is a float or double numeral: a
// mantissa following the decimal lexical rules, optionally followed by
// E or e and an integer exponent (XML Schema Part 2, 3.2.4.1).
func isFloatNumeral(s string) bool {
	mantissa, exponent, hasExponent := s, "", false
	if i := strings.IndexAny(s, "Ee"); i >= 0 {
		mantissa, exponent, hasExponent = s[:i], s[i+1:], true
	}
	if !isDecimalNumeral(mantissa) {
		return false
	}
	return !hasExponent || allDigits(trimSign(exponent))
}

// isDecimalNumeral reports whether s matches the decimal lexical space
// of XML Schema Part 2, 3.2.3.1: an optional sign, then digits with at
// most one period, (+|-)?([0-9]+(.[0-9]*)?|.[0-9]+).
func isDecimalNumeral(s string) bool {
	whole, fraction, hasPoint := strings.Cut(trimSign(s), ".")
	switch {
	case !hasPoint:
		return allDigits(whole)
	case whole == "":
		return allDigits(fraction)
	default:
		return allDigits(whole) && (fraction == "" || allDigits(fraction))
	}
}

// trimSign drops one leading plus or minus sign.
func trimSign(s string) string {
	if s != "" && (s[0] == '+' || s[0] == '-') {
		return s[1:]
	}
	return s
}

// compareFloat orders two float or double values. Per XML Schema Part
// 2, 3.2.4 and 3.2.5, the value space has a single zero, so -0 equals
// 0, and NaN equals itself but is incomparable with every other value.
func compareFloat(a, b float64) order {
	aNaN, bNaN := math.IsNaN(a), math.IsNaN(b)
	switch {
	case aNaN && bNaN:
		return orderEqual
	case aNaN || bNaN:
		return orderIncomparable
	case a < b:
		return orderLess
	case a > b:
		return orderGreater
	default:
		return orderEqual
	}
}
