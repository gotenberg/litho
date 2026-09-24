package xsd

import (
	"fmt"
	"math/big"
	"slices"
	"strings"
)

// builtinNames lists the XS-namespace primitive type local names that
// the loader pre-registers so element/type references like
// type="xs:string" or base="xs:decimal" resolve without a separate
// lookup table.
//
// The types themselves stay opaque [BuiltinType] markers:
// [validateLexical] checks a value against each one's lexical space,
// and [ValidateValue] applies the facets a restriction adds.
//
//nolint:gochecknoglobals // immutable type registry.
var builtinNames = []string{
	"string",
	"normalizedString",
	"token",
	"language",
	"NMTOKEN",
	"Name",
	"NCName",
	"ID",
	"IDREF",
	"anyURI",
	"QName",
	"boolean",
	"decimal",
	"integer",
	"nonNegativeInteger",
	"positiveInteger",
	"nonPositiveInteger",
	"negativeInteger",
	"long",
	"int",
	"short",
	"byte",
	"unsignedLong",
	"unsignedInt",
	"unsignedShort",
	"unsignedByte",
	"float",
	"double",
	"date",
	"dateTime",
	"time",
	"duration",
	"gYear",
	"gYearMonth",
	"gMonth",
	"gMonthDay",
	"gDay",
	"hexBinary",
	"base64Binary",
	"anyType",
	"anySimpleType",
}

// newBuiltinTypeRegistry returns a fresh map seeded with one
// [BuiltinType] per name in [builtinNames]. The map is owned by the
// caller; the loader copies entries into a Schema's Types index so
// imported schemas see the same primitives.
func newBuiltinTypeRegistry() map[QName]Type {
	out := make(map[QName]Type, len(builtinNames))
	for _, name := range builtinNames {
		qn := QName{Namespace: XSNamespace, Local: name}
		out[qn] = &BuiltinType{Name: qn}
	}
	return out
}

// IsBuiltin reports whether qn names a pre-registered XS-namespace
// primitive. Useful for the link pass to avoid emitting an
// "unresolved reference" diagnostic on a missing user type when the
// reference is actually to a built-in.
func IsBuiltin(qn QName) bool {
	if qn.Namespace != XSNamespace {
		return false
	}
	return slices.Contains(builtinNames, qn.Local)
}

// validateLexical checks value against the lexical space of base.
// Returns a [FacetViolation] (the closest match for the violation
// shape, even though no facet is involved) when the value falls
// outside the type's lexical space. Returns nil for unknown built-ins
// so callers can extend the registry without rewriting this dispatch.
//
// Coverage is intentionally focused on the CII subset: string,
// normalizedString, token, decimal, the integer family, boolean,
// date, dateTime, time, anyURI, and the few unsigned variants
// invoices actually use. Less common types fall through to permissive
// acceptance; the loader still resolves them so unrelated schema
// references work.
//
// Each lexical space is the one XML Schema Part 2: Datatypes (Second
// Edition) defines, in 3.2 for the primitive types and 3.3 for the derived
// ones. The clause sits on each case and check.
//
// value must already be white-space normalized: the lexical spaces hold no
// white space a normalization removes, and [ValidateValue] applies the mode
// [builtinWhiteSpace] fixes before it calls here.
func validateLexical(value string, base *BuiltinType) *FacetViolation {
	if base == nil {
		return nil
	}
	if base.Name.Namespace != XSNamespace {
		return nil
	}
	switch base.Name.Local {
	// 3.2.1: string holds every finite sequence of XML characters, and the
	// XML parser has already rejected any other.
	case "string", "anyType", "anySimpleType":
		return nil
	case "normalizedString":
		return checkNormalizedString(value, base)
	case "token":
		return checkToken(value, base)
	case "boolean":
		return checkBoolean(value, base)
	case "decimal":
		return checkDecimal(value, base)
	// The integer family, 3.3.13 to 3.3.25: each derived type fixes its
	// range as a minInclusive and maxInclusive on integer, nonPositiveInteger
	// 3.3.14, negativeInteger 3.3.15, long 3.3.16, int 3.3.17, short 3.3.18,
	// byte 3.3.19, nonNegativeInteger 3.3.20, unsignedLong 3.3.21,
	// unsignedInt 3.3.22, unsignedShort 3.3.23, unsignedByte 3.3.24 and
	// positiveInteger 3.3.25.
	case "integer":
		return checkInteger(value, base, nil, nil)
	case "nonNegativeInteger":
		return checkInteger(value, base, big.NewInt(0), nil)
	case "positiveInteger":
		return checkInteger(value, base, big.NewInt(1), nil)
	case "nonPositiveInteger":
		return checkInteger(value, base, nil, big.NewInt(0))
	case "negativeInteger":
		return checkInteger(value, base, nil, big.NewInt(-1))
	case "long":
		return checkBoundedInt(value, base, "-9223372036854775808", "9223372036854775807")
	case "int":
		return checkBoundedInt(value, base, "-2147483648", "2147483647")
	case "short":
		return checkBoundedInt(value, base, "-32768", "32767")
	case "byte":
		return checkBoundedInt(value, base, "-128", "127")
	case "unsignedLong":
		return checkBoundedInt(value, base, "0", "18446744073709551615")
	case "unsignedInt":
		return checkBoundedInt(value, base, "0", "4294967295")
	case "unsignedShort":
		return checkBoundedInt(value, base, "0", "65535")
	case "unsignedByte":
		return checkBoundedInt(value, base, "0", "255")
	case "date":
		return checkDate(value, base)
	case "dateTime":
		return checkDateTime(value, base)
	case "time":
		return checkTime(value, base)
	case "anyURI":
		// Per 3.2.17.1 the lexical space holds every sequence that the
		// escaping of XLink 5.4 turns into an RFC 2396 URI reference. RFC
		// 2396, 4 makes both the URI and the fragment of a reference
		// optional, and 4.2 reads the empty reference as the current
		// document, so the empty string is a valid anyURI.
		//
		// Deliberate leniency: the few sequences no escaping repairs, such
		// as a "%" not followed by two hex digits or a second "#", are not
		// checked. 3.2.17 itself notes that the definition "imposes only
		// very modest obligations" on a minimally conforming processor.
		return nil
	}
	return nil
}

// whiteSpaceMode is one of the three whiteSpace values of XML Schema Part 2,
// 4.3.6. The modes are ordered: each one normalizes everything the one below
// it does, and more.
type whiteSpaceMode int

const (
	whiteSpacePreserve whiteSpaceMode = iota
	whiteSpaceReplace
	whiteSpaceCollapse
)

// parseWhiteSpaceMode reads the value attribute of an xs:whiteSpace facet.
// It reports false for anything but the three values 4.3.6 allows.
func parseWhiteSpaceMode(value string) (whiteSpaceMode, bool) {
	switch value {
	case "preserve":
		return whiteSpacePreserve, true
	case "replace":
		return whiteSpaceReplace, true
	case "collapse":
		return whiteSpaceCollapse, true
	}
	return whiteSpacePreserve, false
}

// builtinWhiteSpace returns the whiteSpace a built-in type fixes. Per XML
// Schema Part 2, 4.3.6, it is preserve for string and collapse for every
// other atomic type. normalizedString replaces (3.3.1), token collapses
// (3.3.2), and the types derived from token inherit collapse. A nil or
// foreign base names no built-in, so nothing is normalized.
//
// anyType and anySimpleType preserve: per XML Schema Part 1, 3.1.4 a value
// validated against the simple ur-type is normalized "as in the preserve
// case".
func builtinWhiteSpace(base *BuiltinType) whiteSpaceMode {
	if base == nil || base.Name.Namespace != XSNamespace {
		return whiteSpacePreserve
	}
	switch base.Name.Local {
	case "string", "anyType", "anySimpleType":
		return whiteSpacePreserve
	case "normalizedString":
		return whiteSpaceReplace
	}
	return whiteSpaceCollapse
}

func lexErr(base *BuiltinType, value, reason string) *FacetViolation {
	return &FacetViolation{
		Facet:  nil,
		Value:  value,
		Reason: fmt.Sprintf("xs:%s lexical space: %s", base.Name.Local, reason),
	}
}

// checkNormalizedString checks XML Schema Part 2, 3.3.1: the lexical space
// holds no carriage return, line feed or tab.
func checkNormalizedString(value string, base *BuiltinType) *FacetViolation {
	if strings.ContainsAny(value, "\t\n\r") {
		return lexErr(base, value, "contains tab/newline/carriage-return")
	}
	return nil
}

// checkToken checks XML Schema Part 2, 3.3.2: normalizedString's lexical
// space, with no leading or trailing space and no run of two or more spaces.
func checkToken(value string, base *BuiltinType) *FacetViolation {
	if v := checkNormalizedString(value, base); v != nil {
		return v
	}
	if value != strings.TrimSpace(value) {
		return lexErr(base, value, "has leading or trailing whitespace")
	}
	for i := 0; i+1 < len(value); i++ {
		if value[i] == ' ' && value[i+1] == ' ' {
			return lexErr(base, value, "contains a run of consecutive spaces")
		}
	}
	return nil
}

// checkBoolean checks XML Schema Part 2, 3.2.2.1: the legal literals are
// true, false, 1 and 0.
func checkBoolean(value string, base *BuiltinType) *FacetViolation {
	switch value {
	case "true", "false", "1", "0":
		return nil
	}
	return lexErr(base, value, "not one of true|false|1|0")
}

// checkDecimal checks XML Schema Part 2, 3.2.3.1: "a finite-length sequence
// of decimal digits (#x30-#x39) separated by a period", with an optional
// leading sign.
//
// TODO: parseDecimal delegates to big.Rat.SetString, which also accepts an
// exponent ("1e5"), a fraction ("1/2") and a base prefix ("0x10"). None of
// them is in the decimal lexical space, so each passes here where a
// conforming validator rejects it.
func checkDecimal(value string, base *BuiltinType) *FacetViolation {
	if _, ok := parseDecimal(value); !ok {
		return lexErr(base, value, "not a decimal literal")
	}
	return nil
}

// checkInteger checks XML Schema Part 2, 3.3.13.1, "a finite-length sequence
// of decimal digits (#x30-#x39) with an optional leading sign", then the
// range a derived type fixes. Base 10 keeps big.Int from reading a base
// prefix or an underscore, neither of which is an integer literal.
func checkInteger(value string, base *BuiltinType, minInt, maxInt *big.Int) *FacetViolation {
	n, ok := new(big.Int).SetString(value, 10)
	if !ok {
		return lexErr(base, value, "not an integer literal")
	}
	if minInt != nil && n.Cmp(minInt) < 0 {
		return lexErr(base, value, fmt.Sprintf("below minimum %s", minInt.String()))
	}
	if maxInt != nil && n.Cmp(maxInt) > 0 {
		return lexErr(base, value, fmt.Sprintf("above maximum %s", maxInt.String()))
	}
	return nil
}

func checkBoundedInt(value string, base *BuiltinType, minLit, maxLit string) *FacetViolation {
	minInt, _ := new(big.Int).SetString(minLit, 10)
	maxInt, _ := new(big.Int).SetString(maxLit, 10)
	return checkInteger(value, base, minInt, maxInt)
}

// checkDate checks value against the XSD date lexical form, XML Schema Part
// 2, 3.2.9.1: '-'? yyyy '-' mm '-' dd zzzzzz?, with the year, month, day and
// timezone written as for dateTime (3.2.7.1). [parseTemporal] does the work,
// so the day is capped by its month and year (3.2.7), year 0000 is rejected,
// and a signed or longer-than-four-digit year is accepted.
func checkDate(value string, base *BuiltinType) *FacetViolation {
	// Per XML Schema Part 2, 3.2.9.1 a date is '-'? yyyy '-' mm '-' dd
	// zzzzzz?, the year written as for dateTime (3.2.7.1): negative, or
	// longer than four digits, is valid, and 0000 is not.
	if _, reason := parseTemporal(value, kindDate); reason != "" {
		return lexErr(base, value, reason)
	}
	return nil
}

// checkDateTime checks value against XML Schema Part 2, 3.2.7.1: '-'? yyyy
// '-' mm '-' dd 'T' hh ':' mm ':' ss ('.' s+)? (zzzzzz)?. [parseTemporal]
// checks every field's range and rejects trailing text.
func checkDateTime(value string, base *BuiltinType) *FacetViolation {
	// Per XML Schema Part 2, 3.2.7.1 a dateTime is '-'? yyyy '-' mm '-'
	// dd 'T' hh ':' mm ':' ss ('.' s+)? zzzzzz?.
	if _, reason := parseTemporal(value, kindDateTime); reason != "" {
		return lexErr(base, value, reason)
	}
	return nil
}

// checkTime checks value against XML Schema Part 2, 3.2.8.1: "the left
// truncated lexical representation for dateTime", hh:mm:ss.sss with an
// optional timezone. [parseTemporal] checks the field ranges.
func checkTime(value string, base *BuiltinType) *FacetViolation {
	// Per XML Schema Part 2, 3.2.8.1 a time is hh ':' mm ':' ss ('.'
	// s+)? zzzzzz?, the left truncation of dateTime.
	if _, reason := parseTemporal(value, kindTime); reason != "" {
		return lexErr(base, value, reason)
	}
	return nil
}

// splitTimezone separates the body of a date/time lexical value from
// its optional timezone suffix ("Z" or +HH:MM/-HH:MM), by shape only.
// The shape is XML Schema Part 2, 3.2.7.3: (('+' | '-') hh ':' mm) | 'Z'.
// [parseTimezoneOffset] checks the suffix's digits and its 14-hour limit.
func splitTimezone(value string) (body string, tz string) {
	if i := strings.LastIndexAny(value, "+-"); i > 0 && len(value)-i == 6 && value[i+3] == ':' {
		return value[:i], value[i:]
	}
	if strings.HasSuffix(value, "Z") {
		return value[:len(value)-1], "Z"
	}
	return value, ""
}

func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return len(s) > 0
}
