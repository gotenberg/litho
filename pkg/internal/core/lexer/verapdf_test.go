package lexer

import (
	"math"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// lexVeraPDF returns every token of src up to the end of the data, read under
// [Lexer.WithVeraPDFTokens].
func lexVeraPDF(t *testing.T, src string) []Token {
	t.Helper()
	lex := newLexer(src).WithVeraPDFTokens()
	var toks []Token
	for range len(src) + 1 {
		tok, err := lex.NextToken()
		if err != nil {
			t.Fatalf("NextToken(%q): %v", src, err)
		}
		if tok.Type == TokenEOF {
			return toks
		}
		toks = append(toks, tok)
	}
	t.Fatalf("NextToken(%q) never reached the end of the data", src)
	return nil
}

// TestWithVeraPDFTokensEndsOperatorsAtDigits pins the veraPDF reading of a run
// of regular characters that starts with a letter: an operator ends before a
// digit, save the 0 or 1 of d0 and d1, and a run that starts with "n", "t",
// "f", "B" or "I" keeps the token boundaries of ISO 32000-1, 7.2.2. Measured
// with veraPDF 1.30 at PDF/A-1b and PDF/A-2b, the undefined operators it
// reports for q1, re1, T*1, d2, xd0, dd1, xx1yy, n1, f1, true1 and BT1 are the
// ones read here.
func TestWithVeraPDFTokensEndsOperatorsAtDigits(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		src  string
		want []Token
	}{
		{"an operator then a number", "q1", []Token{kw("q"), num("1")}},
		{"a two-letter operator then a real", "re1.5", []Token{kw("re"), num("1.5")}},
		{"an operator that ends in a star", "T*1", []Token{kw("T*"), num("1")}},
		{"an undefined operator before a number", "xx1", []Token{kw("xx"), num("1")}},
		{"an operator after the number", "xx1yy", []Token{kw("xx"), num("1"), kw("yy")}},
		{"d0 stays whole", "d0", []Token{kw("d0")}},
		{"d1 stays whole", "d1", []Token{kw("d1")}},
		{"one digit joins a d", "d01", []Token{kw("d0"), num("1")}},
		{"any other digit ends it", "d2", []Token{kw("d"), num("2")}},
		{"a digit after a d that ends a run", "xd0", []Token{kw("xd0")}},
		{"a digit after a second d", "dd1", []Token{kw("dd1")}},
		{"a run from n stays whole", "n1", []Token{kw("n1")}},
		{"a run from f stays whole", "f1", []Token{kw("f1")}},
		{"a run from t stays whole", "true1", []Token{kw("true1")}},
		{"a run from B stays whole", "BT1", []Token{kw("BT1")}},
		{"a run from I stays whole", "ID1", []Token{kw("ID1")}},
		{"white space ends an operator as before", "q 1", []Token{kw("q"), num("1")}},
		{"a delimiter ends an operator as before", "Tf/F1", []Token{kw("Tf"), {Type: TokenName, Value: "/F1"}}},
		{"glued operators stay one keyword", "ETBT", []Token{kw("ETBT")}},
	} {
		if got := lexVeraPDF(t, tc.src); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: %q lexes as %v, want %v", tc.name, tc.src, got, tc.want)
		}
	}
}

// TestWithVeraPDFTokensReadsNumbersAsVeraPDF pins the veraPDF reading of a run
// that starts with a digit, a sign or a period (BaseParser.readNumber): the
// number ends before the first character other than a digit or a period, and
// a run that spells no number of ISO 32000-1, 7.3.3 reads as the largest value
// veraPDF gives it. Measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b in a
// content stream: "12m", "5-3 m" and "12q1" pass the undefined-operator test,
// "1e2", "-1e-2", "12#3" and "95.c.455" fail it for e, e-, # and c., ".",
// "1..", "1.5.", "-." and ".020.594" fail the real limit as
// 1.7976931348623157e308 or its negative, a 400-digit real as infinity, and
// "-", "+", "--5", "+-5" and a 20-digit integer fail the integer limit as
// 9223372036854775807 or its negative.
func TestWithVeraPDFTokensReadsNumbersAsVeraPDF(t *testing.T) {
	t.Parallel()

	var (
		maxInt  = strconv.FormatInt(math.MaxInt64, 10)
		maxReal = strconv.FormatFloat(math.MaxFloat64, 'f', 1, 64)
		digits  = "1" + strings.Repeat("0", 400)
	)
	for _, tc := range []struct {
		name string
		src  string
		want []Token
	}{
		{"an operator after an integer", "12m", []Token{num("12"), kw("m")}},
		{"an operator after a real", "12.9f", []Token{num("12.9"), kw("f")}},
		{"a sign ends a number", "5-3", []Token{num("5"), num("-3")}},
		{"an operator between numbers", "12q1", []Token{num("12"), kw("q"), num("1")}},
		{"an exponent is an operator", "1e2", []Token{num("1"), kw("e"), num("2")}},
		{"a signed exponent", "-1e-2", []Token{num("-1"), kw("e-"), num("2")}},
		{"a number sign is an operator", "12#3", []Token{num("12"), kw("#"), num("3")}},
		{"a period joins an operator", "95.c.455", []Token{num("95."), kw("c."), num("455")}},
		{"a number run into endobj is still split", "7559endobj", []Token{num("7559"), kw("endobj")}},
		{"an integer keeps its text", "-12", []Token{num("-12")}},
		{"a real keeps its text", "+.5", []Token{num("+.5")}},
		{"a trailing period is a real", "1.", []Token{num("1.")}},
		{"two periods", ".020.594", []Token{num(maxReal)}},
		{"a trailing second period", "1.5.", []Token{num(maxReal)}},
		{"a lone period", ".", []Token{num(maxReal)}},
		{"a sign and a period", "-.", []Token{num("-" + maxReal)}},
		{"a real past the range of a double", digits + ".0", []Token{num(maxReal)}},
		{"a lone minus", "-", []Token{num("-" + maxInt)}},
		{"a lone plus", "+", []Token{num(maxInt)}},
		{"a sign ends a lone sign", "--5", []Token{num("-" + maxInt), num("-5")}},
		{"a minus after a plus", "+-5", []Token{num(maxInt), num("-5")}},
		{"a sign then an operator", "-f", []Token{num("-" + maxInt), kw("f")}},
		{"an integer past 64 bits keeps its digits", "-99999999999999999999", []Token{num("-99999999999999999999")}},
		{"an integer past the range of a double", "-" + digits, []Token{num("-" + maxInt)}},
		{"a delimiter ends a number", "12/F1", []Token{num("12"), {Type: TokenName, Value: "/F1"}}},
	} {
		if got := lexVeraPDF(t, tc.src); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: %q lexes as %v, want %v", tc.name, tc.src, got, tc.want)
		}
	}

	if f, err := strconv.ParseFloat(veraPDFMaxReal, 64); err != nil || f != math.MaxFloat64 {
		t.Errorf("%q reads back as %v, %v, want the largest double", veraPDFMaxReal, f, err)
	}
	if !strings.Contains(veraPDFMaxReal, ".") || strings.ContainsAny(veraPDFMaxReal, "eE") {
		t.Errorf("%q is no real of ISO 32000-1, 7.3.3", veraPDFMaxReal)
	}
}

// TestObjectBodyKeepsNumberBoundaries pins that a lexer without
// [Lexer.WithVeraPDFTokens] reads a run that starts with a digit to white
// space or a delimiter, as the objects of a file body are read.
func TestObjectBodyKeepsNumberBoundaries(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		src  string
		want Token
	}{
		{"12m", kw("12m")},
		{".020.594", kw(".020.594")},
		{"1e2", num("1e2")},
		{"-", num("0")},
	} {
		if got := lexCommands(t, tc.src, nil); !reflect.DeepEqual(got, []Token{tc.want}) {
			t.Errorf("%q lexes as %v, want %v", tc.src, got, tc.want)
		}
	}
}

// TestWithRepairTokensKeepsNumberRuns pins the reading of a repair: a run that
// starts with a digit, a sign or a period keeps the token boundaries of ISO
// 32000-1, 7.2.2, and is a keyword when it spells no number, "-" included,
// while an operator still ends before a digit.
func TestWithRepairTokensKeepsNumberRuns(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		src  string
		want []Token
	}{
		{"12m", []Token{kw("12m")}},
		{"55.8492#7136", []Token{kw("55.8492#7136")}},
		{".020.594", []Token{kw(".020.594")}},
		{"-", []Token{kw("-")}},
		{"-.", []Token{kw("-.")}},
		{"1e2", []Token{num("1e2")}},
		{"-1.5", []Token{num("-1.5")}},
		{"q1", []Token{kw("q"), num("1")}},
		{"n1", []Token{kw("n1")}},
	} {
		lex := newLexer(tc.src).WithVeraPDFTokens().WithRepairTokens()
		var got []Token
		for range len(tc.src) + 1 {
			tok, err := lex.NextToken()
			if err != nil {
				t.Fatalf("NextToken(%q): %v", tc.src, err)
			}
			if tok.Type == TokenEOF {
				break
			}
			got = append(got, tok)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%q lexes as %v, want %v", tc.src, got, tc.want)
		}
	}
}
