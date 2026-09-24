package lexer

import (
	"testing"
)

// TestNextToken_DigitlessNumber pins that a numeric object carrying a sign or a
// decimal point but no digit reads as the number 0, not as a keyword.
//
// ISO 32000-2, 7.3.3 gives "-" no meaning. Adobe Reader reads it as 0 and
// pdf.js follows: its Lexer.getNumber returns 0 for a sign run that whitespace
// or a delimiter ends. Reading it as a keyword makes it a content stream
// operator, which litho then reports as undefined under ISO 19005-4, 6.2.2
// while veraPDF reports nothing, and which mis-binds the operands around it:
// an operator ends the operand list a number would join.
//
// # Corpus evidence
//
//   - pdfjs issue9252.pdf, issue15604.pdf, bug1753983.pdf: pdf.js's
//     Lexer.getNumber names all three, and the corpus carries all three.
func TestNextToken_DigitlessNumber(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input string
		typ   TokenType
		value string
	}{
		{"a bare minus", "- ", TokenNumber, "0"},
		{"a bare plus", "+ ", TokenNumber, "0"},
		{"a bare dot", ". ", TokenNumber, "0"},
		{"a double negative", "-- ", TokenNumber, "0"},
		{"a sign and a dot", "-. ", TokenNumber, "0"},
		{"a minus ending the stream", "-", TokenNumber, "0"},

		// A regular character after the sign is not a number. pdf.js throws
		// there rather than returning 0, so litho keeps it a keyword.
		{"a sign followed by a letter", "-f ", TokenKeyword, "-f"},
		{"a real negative number", "-1 ", TokenNumber, "-1"},
		{"a real decimal", "-.5 ", TokenNumber, "-.5"},
		{"an ordinary operator", "Tf ", TokenKeyword, "Tf"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tok, err := newLexer(tc.input).NextToken()
			if err != nil {
				t.Fatalf("NextToken: %v", err)
			}
			if tok.Type != tc.typ || tok.Value != tc.value {
				t.Errorf("%q: got (%v, %q), want (%v, %q)",
					tc.input, tok.Type, tok.Value, tc.typ, tc.value)
			}
		})
	}
}
