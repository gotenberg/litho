package content

import "github.com/gotenberg/litho/pkg/internal/core/lexer"

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

// IsUndefined reports whether op, the operator of an [Operation] a parse
// returns, is one ISO 19005 forbids as an operator ISO 32000-1 does not
// define (ISO 19005-1, 6.2.10; ISO 19005-2 to 4, 6.2.2): a keyword outside
// Annex A, Table A.1 ([IsOperator]), or EI or ID. ISO 32000-1, 8.9.7 defines
// those two only as the parts of the inline image object BI begins, which a
// parse returns whole as one BI operation, so an EI or an ID it returns
// stands outside one. veraPDF 1.30 reports "q EI Q" as one undefined
// operator at PDF/A-1b and PDF/A-2b.
func IsUndefined(op string) bool {
	return !IsOperator(op) || op == "EI" || op == "ID"
}

// gluedOperators is the command set [SplitGluedOperators] reads a content
// stream with: the operators of Annex A, Table A.1 and the keywords true, false
// and null an operand can be (ISO 32000-1, 7.3.2 and 7.3.9). It is the command
// table of PDF.js, EvaluatorPreprocessor.opMap (src/core/evaluator.js), whose
// entries are the same operators and the same three keywords, and
// [lexer.NewCommandSet] derives the partial commands that table lists by hand.
//
//nolint:gochecknoglobals // constant lookup table, built once from annexAOperators
var gluedOperators = lexer.NewCommandSet(append(annexAOperators[:], "true", "false", "null")...)
