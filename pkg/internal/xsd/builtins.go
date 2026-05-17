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
// PR2 attaches lexical-space validators and facet implementations to
// these types. For PR1 they exist as opaque [BuiltinType] markers.
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
func validateLexical(value string, base *BuiltinType) *FacetViolation {
	if base == nil {
		return nil
	}
	if base.Name.Namespace != XSNamespace {
		return nil
	}
	switch base.Name.Local {
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

func checkNormalizedString(value string, base *BuiltinType) *FacetViolation {
	if strings.ContainsAny(value, "\t\n\r") {
		return lexErr(base, value, "contains tab/newline/carriage-return")
	}
	return nil
}

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

func checkBoolean(value string, base *BuiltinType) *FacetViolation {
	switch value {
	case "true", "false", "1", "0":
		return nil
	}
	return lexErr(base, value, "not one of true|false|1|0")
}

func checkDecimal(value string, base *BuiltinType) *FacetViolation {
	if _, ok := parseDecimal(value); !ok {
		return lexErr(base, value, "not a decimal literal")
	}
	return nil
}

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

// XSD date lexical space (without timezone): CCYY-MM-DD with optional
// negative sign on the year. The pure-Go time package's parser is
// forgiving on length so the helpers below check byte-level shape
// before handing off to Parse for component validation.
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
// its optional timezone suffix ("Z" or +HH:MM/-HH:MM). PR2 only checks
// that the timezone shape is plausible; full RFC 3339 timezone
// arithmetic is out of scope.
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
