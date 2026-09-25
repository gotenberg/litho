package lexer

import (
	"io"
	"math"
	"strconv"
	"strings"
)

// WithVeraPDFTokens makes l read a run of regular characters the way veraPDF
// reads a content stream, and returns l. Without it a run ends only at white
// space or a delimiter, and a run is a number when ISO 32000-1, 7.3.3 spells
// one.
//
// ISO 32000-1, 7.2.2 ends a token only at white space or a delimiter, so "q1"
// and "12m" are one keyword each by the letter of the spec, operators ISO
// 32000-1, Annex A does not define, which ISO 19005-1, 6.2.10 t1 and ISO
// 19005-2, 6.2.2 t1 forbid. Deliberate leniency toward veraPDF, the reference
// validator of ISO 19005, whose content stream parser (PDFStreamParser,
// veraPDF-parser 1.30) draws other boundaries:
//
//   - A run that starts with a digit, a sign or a period is a number, which
//     ends before the first character other than a digit or a period
//     (BaseParser.readNumber): "12m" is 12 then m, and "5-3" is 5 then -3.
//     [Lexer.readVeraPDFNumber] gives it the value veraPDF reads.
//   - A run that starts with "n", "t", "f", "B" or "I" is read to white space
//     or a delimiter (PDFStreamParser.parseNextToken), so "n1", "true1" and
//     "BT1" stay whole.
//   - Any other run is an operator, which ends before white space, one of the
//     delimiters "[", "]", "<", "(" and "/", or a digit
//     (PDFStreamParser.nextOperator), so "q1" is q then the number 1. A 0 or a
//     1 after a "d" stays in the operator, which keeps d0 and d1 whole. The
//     other delimiters of ISO 32000-1, 7.2.2, Table 2, ")", ">", "{", "}" and
//     "%", do not end it, so "Q%comment" and "T)Tj" are one operator each.
//   - Inside an array or a dictionary, veraPDF reads a keyword to white space
//     or any delimiter (BaseParser.nextToken), and so does this reading, save
//     that it still ends one before a digit. A keyword there other than true,
//     false and null stops veraPDF reading the stream either way.
//
// One rule of veraPDF is not mirrored: a run that starts with "I" keeps its
// boundaries, where veraPDF reads it as the start of inline image data.
//
// The objects of the file body keep the token boundaries of 7.2.2: only a
// content stream reading sets it.
func (l *Lexer) WithVeraPDFTokens() *Lexer {
	l.veraPDF = true
	return l
}

// WithRepairTokens makes a lexer set up by [Lexer.WithVeraPDFTokens] keep the
// token boundaries of ISO 32000-1, 7.2.2 for a run that starts with a digit, a
// sign or a period, and returns l. Such a run is read to white space or a
// delimiter, and is a number when ISO 32000-1, 7.3.3 spells one and a keyword
// holding its text otherwise, "-" and ".020.594" included.
//
// It is for the readings that rewrite a stream, where the reading of veraPDF
// departs from the one a repair has to keep. A repair removes an undefined
// operator with the operands before it. veraPDF ends "55.8492#7136" at the
// "#", so removing the # takes 55.8492 and leaves 7136 to the operator after
// the run, where the run read whole goes with every digit it holds. And
// veraPDF reads a run that spells no number as the largest value it gives one,
// which is no value a viewer draws: PDF.js reads "-" as 0, and ".020.594" as
// 0.02 then 0.594 (Lexer.getNumber). Kept as the keyword it is written as, the
// run is written back as it was, or respelled as a number.
//
// It also ends an operator at every delimiter of ISO 32000-1, 7.2.2, Table 2.
// veraPDF reads "T)Tj" as one undefined operator, where a viewer reads T, a
// stray ")" and the Tj it draws with. Removing the run would remove the Tj,
// where removing T leaves it in place.
func (l *Lexer) WithRepairTokens() *Lexer {
	l.repair = true
	return l
}

// endsBeforeDigit reports whether a keyword that starts with first ends
// before a digit under [Lexer.WithVeraPDFTokens]. A run that starts with a
// digit, a sign or a period is a number there, which reaches the keyword read
// only under [Lexer.WithRepairTokens], and keeps its boundaries.
func endsBeforeDigit(first byte) bool {
	switch first {
	case 'n', 't', 'f', 'B', 'I', '+', '-', '.':
		return false
	}
	return !isDigit(first)
}

// joinsBeforeDigit reports whether digit, read after prev, stays in a keyword
// that ends before a digit: the 0 or 1 of d0 and d1.
func joinsBeforeDigit(prev, digit byte) bool {
	return prev == 'd' && (digit == '0' || digit == '1')
}

// isDigit reports whether c is a decimal digit.
func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

// runsOnThrough reports whether c, a delimiter of ISO 32000-1, 7.2.2, Table 2,
// stays in an operator veraPDF reads (PDFStreamParser.nextOperator).
func runsOnThrough(c byte) bool {
	return c == ')' || c == '>' || c == '{' || c == '}' || c == '%'
}

// nest counts the array or dictionary a token opens, delta 1, or closes,
// delta -1, under [Lexer.WithVeraPDFTokens]. A close with none open counts
// nothing.
func (l *Lexer) nest(delta int) {
	if l.veraPDF {
		l.nesting = max(l.nesting+delta, 0)
	}
}

// startsVeraPDFNumber reports whether a run that starts with c is a number
// [Lexer.readVeraPDFNumber] reads.
func startsVeraPDFNumber(c byte) bool {
	return isDigit(c) || c == '.' || c == '+' || c == '-'
}

// Largest values veraPDF gives a number it cannot read (see
// [Lexer.readVeraPDFNumber]), spelled so the parser reads them back: an
// integer as its digits, and a real with a decimal point and no exponent,
// which ISO 32000-1, 7.3.3 does not give a real.
var (
	veraPDFMaxInteger = strconv.FormatInt(math.MaxInt64, 10)
	veraPDFMaxReal    = strconv.FormatFloat(math.MaxFloat64, 'f', 1, 64)
)

// readVeraPDFNumber reads the number that starts with first, already read,
// under [Lexer.WithVeraPDFTokens]: first, then every digit and period up to
// the first other character, which is left for the next token. It is the run
// BaseParser.readNumber reads (veraPDF-parser 1.30), and the token has the
// value veraPDF gives it:
//
//   - A run holding a period is the real Double.parseDouble reads in it after
//     the sign, and the largest double, signed, when that reads none.
//     ".020.594", "." and "-." spell no real of ISO 32000-1, 7.3.3, and
//     veraPDF reads them as 1.7976931348623157e308 or its negative, which
//     fails the real limit of ISO 19005-1, 6.1.12 t2 and ISO 19005-2, 6.1.13
//     t2. A real past the range of a double, which veraPDF reads as infinity,
//     reads as the largest double, which fails the same limits.
//   - Any other run is the integer Long.valueOf reads in it after the sign,
//     and the largest 64-bit integer, signed, when that reads none. "-" and
//     "+" hold no digit, and veraPDF reads them as -9223372036854775807 and
//     9223372036854775807, which fail the integer limit of ISO 19005-1, 6.1.12
//     t1 and ISO 19005-2, 6.1.13 t1. A run of digits past the 64-bit range
//     keeps its digits, which the parser reads as a real it judges against
//     the integer limit, unless the run is past the range of a double too,
//     which the parser cannot read.
//
// ISO 32000-1, 7.3.3 gives a number one sign and one period at most, and no
// exponent. "--5" is two numbers here as in veraPDF, -9223372036854775807 then
// -5, and "1e2" is the number 1, the operator e and the number 2.
//
// The token carries the text of the run when the parser reads that text as
// veraPDF reads the run, and the value veraPDF reads otherwise.
func (l *Lexer) readVeraPDFNumber(first byte) (Token, error) {
	raw := make([]byte, 1, 16)
	raw[0] = first
	for {
		c, err := l.reader.ReadByte()
		if err != nil {
			if err == io.EOF {
				break
			}
			return Token{Type: TokenError, Value: err.Error()}, err
		}
		if !isDigit(c) && c != '.' {
			_ = l.reader.UnreadByte() // Guaranteed to succeed: previous ReadByte succeeded on a buffered reader.
			break
		}
		raw = append(raw, c)
	}
	return Token{Type: TokenNumber, Value: veraPDFNumber(string(raw))}, nil
}

// veraPDFNumber returns the text of the number token [Lexer.readVeraPDFNumber]
// reads as raw, an optional sign followed by digits and periods.
func veraPDFNumber(raw string) string {
	sign, body := "", raw
	switch raw[0] {
	case '-':
		sign, body = "-", raw[1:]
	case '+':
		body = raw[1:]
	}
	if strings.IndexByte(body, '.') >= 0 {
		if _, err := strconv.ParseFloat(body, 64); err != nil {
			return sign + veraPDFMaxReal
		}
		return raw
	}
	if body == "" {
		return sign + veraPDFMaxInteger
	}
	if _, err := strconv.ParseInt(body, 10, 64); err != nil {
		// A run of digits fails ParseFloat only past the range of a double.
		if _, err := strconv.ParseFloat(body, 64); err != nil {
			return sign + veraPDFMaxInteger
		}
	}
	return raw
}
