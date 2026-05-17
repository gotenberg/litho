package xsd

import "slices"

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
