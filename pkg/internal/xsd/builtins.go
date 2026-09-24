package xsd

import (
	"fmt"
	"math/big"
	"slices"
	"strconv"
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
// TODO: 4.3.6 fixes whiteSpace to collapse for every atomic type but string
// and the types derived from it, and token itself collapses, so white space
// is normalised before a value is held against its lexical space.
// [ValidateValue] applies only the whiteSpace facets a schema declares, never
// a built-in's own. "A\nB" and "A  B" as xs:token, or "A\tB" as
// xs:normalizedString, are therefore reported, although they normalise to
// valid values. Element text is trimmed before it reaches here, so only
// interior white space and attribute values are affected.
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
		// XSD lets anyURI be permissive: any string that survives
		// percent-encoding is acceptable. Practically all CII
		// IDSchemeAgencyID values pass; reject only the empty form.
		//
		// TODO: 3.2.17.1 admits every sequence that escapes to a legal RFC
		// 2396 URI reference, and RFC 2396 makes both the URI and the
		// fragment of a reference optional, so the empty string is a valid
		// anyURI. Rejecting it flags an empty attribute a conforming
		// validator accepts.
		if value == "" {
			return lexErr(base, value, "empty anyURI")
		}
		return nil
	}
	return nil
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
// range a derived type fixes. The trim stands in for the whiteSpace collapse
// 4.3.6 fixes on every numeric type.
func checkInteger(value string, base *BuiltinType, minInt, maxInt *big.Int) *FacetViolation {
	v := strings.TrimSpace(value)
	n, ok := new(big.Int).SetString(v, 10)
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

// checkDate checks value against the XSD date lexical form CCYY-MM-DD,
// after stripping any timezone suffix. The helpers below check the
// byte-level shape; checkDate then checks the month and day ranges.
//
// XML Schema Part 2, 3.2.9.1 gives the form as '-'? yyyy '-' mm '-' dd
// zzzzzz?, with the year, month, day and timezone written as for dateTime
// (3.2.7.1).
//
// TODO: the check covers only the first ten characters and a flat 1 to 31
// day range. 3.2.7 caps the day by its month and year ("cannot even be 29
// for month 02 and year 2002") and 3.2.7.1 prohibits '0000', yet
// "2024-02-30", "0000-01-01" and "2024-01-15abc" pass. The '-'? sign and a
// year of more than four digits, which 3.2.7.1 allows, are rejected.
func checkDate(value string, base *BuiltinType) *FacetViolation {
	s, _ := splitTimezone(value)
	if len(s) < 10 || s[4] != '-' || s[7] != '-' {
		return lexErr(base, value, "expected CCYY-MM-DD lexical form")
	}
	if !allDigits(s[:4]) || !allDigits(s[5:7]) || !allDigits(s[8:10]) {
		return lexErr(base, value, "non-digit in date components")
	}
	month, _ := strconv.Atoi(s[5:7])
	day, _ := strconv.Atoi(s[8:10])
	if month < 1 || month > 12 || day < 1 || day > 31 {
		return lexErr(base, value, "month or day out of range")
	}
	return nil
}

// checkDateTime checks the shape of XML Schema Part 2, 3.2.7.1: '-'? yyyy
// '-' mm '-' dd 'T' hh ':' mm ':' ss ('.' s+)? (zzzzzz)?.
//
// TODO: only the separators and digits of the first 19 characters are
// checked. Month, day, hour, minute and second ranges are not, and whatever
// follows the seconds is accepted, so "2024-13-45T99:99:99" and
// "2024-01-15T10:00:00junk" pass.
func checkDateTime(value string, base *BuiltinType) *FacetViolation {
	s, _ := splitTimezone(value)
	// Minimum well-formed dateTime: CCYY-MM-DDTHH:MM:SS, 19 chars.
	if len(s) < 19 || s[4] != '-' || s[7] != '-' || s[10] != 'T' ||
		s[13] != ':' || s[16] != ':' {
		return lexErr(base, value, "expected CCYY-MM-DDTHH:MM:SS lexical form")
	}
	if !allDigits(s[:4]) || !allDigits(s[5:7]) || !allDigits(s[8:10]) ||
		!allDigits(s[11:13]) || !allDigits(s[14:16]) || !allDigits(s[17:19]) {
		return lexErr(base, value, "non-digit in dateTime components")
	}
	return nil
}

// checkTime checks the shape of XML Schema Part 2, 3.2.8.1: "the left
// truncated lexical representation for dateTime", hh:mm:ss.sss with an
// optional timezone.
//
// TODO: as in checkDateTime, the ranges and anything past the seconds go
// unchecked, so "25:61:61" passes.
func checkTime(value string, base *BuiltinType) *FacetViolation {
	s, _ := splitTimezone(value)
	if len(s) < 8 || s[2] != ':' || s[5] != ':' {
		return lexErr(base, value, "expected HH:MM:SS lexical form")
	}
	if !allDigits(s[:2]) || !allDigits(s[3:5]) || !allDigits(s[6:8]) {
		return lexErr(base, value, "non-digit in time components")
	}
	return nil
}

// splitTimezone separates the body of a date/time lexical value from
// its optional timezone suffix ("Z" or +HH:MM/-HH:MM). The suffix is
// recognised by shape only; full RFC 3339 timezone arithmetic is out
// of scope.
//
// The shape is XML Schema Part 2, 3.2.7.3: (('+' | '-') hh ':' mm) | 'Z'.
// The magnitude limit of 14 hours stated there is not checked.
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
