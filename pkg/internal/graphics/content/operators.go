package content

// annexAOperators is the content stream operator set of ISO 32000-1, Annex A,
// Table A.1: its 73 operators, listed alphabetically with the two quote
// operators last.
//
//nolint:gochecknoglobals // constant lookup table
var annexAOperators = [...]string{
	"b", "B", "b*", "B*", "BDC", "BI", "BMC", "BT", "BX",
	"c", "cm", "CS", "cs",
	"d", "d0", "d1", "Do", "DP",
	"EI", "EMC", "ET", "EX",
	"f", "F", "f*",
	"G", "g", "gs",
	"h",
	"i", "ID",
	"j", "J",
	"K", "k",
	"l",
	"m", "M", "MP",
	"n",
	"q", "Q",
	"re", "RG", "rg", "ri",
	"s", "S", "SC", "sc", "SCN", "scn", "sh",
	"T*", "Tc", "Td", "TD", "Tf", "Tj", "TJ", "TL", "Tm", "Tr", "Ts", "Tw", "Tz",
	"v",
	"w", "W", "W*",
	"y",
	"'", "\"",
}

// operatorSet indexes [annexAOperators] for [IsOperator].
//
//nolint:gochecknoglobals // constant lookup table, built once from annexAOperators
var operatorSet = func() map[string]struct{} {
	set := make(map[string]struct{}, len(annexAOperators))
	for _, op := range annexAOperators {
		set[op] = struct{}{}
	}
	return set
}()

// IsOperator reports whether op is a content stream operator of ISO 32000-1,
// Annex A, Table A.1. Any other keyword in a content stream is an operator the
// standard does not define, which ISO 19005 forbids (ISO 19005-1, 6.2.10;
// ISO 19005-2 to 4, 6.2.2).
func IsOperator(op string) bool {
	_, ok := operatorSet[op]
	return ok
}
