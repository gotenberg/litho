package lexer

import (
	"bytes"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ioat"
)

func newLexer(input string) *Lexer {
	data := []byte(input)
	return NewLexer(ioat.NewReader(bytes.NewReader(data), int64(len(data)), 0))
}

func TestNullBytesInName(t *testing.T) {
	// "/Ty\x00pe" should be read as "/Type". Null bytes stripped inside names.
	lex := newLexer("/Ty\x00pe")
	tok, err := lex.NextToken()
	if err != nil {
		t.Fatalf("NextToken failed: %v", err)
	}
	if tok.Type != TokenName || tok.Value != "/Type" {
		t.Errorf("got %v %q, want Name /Type", tok.Type, tok.Value)
	}
}

func TestNullByteBetweenTokens(t *testing.T) {
	// Null bytes between tokens are treated as whitespace (PDF spec: 0x00 is whitespace).
	lex := newLexer("12\x000\x00obj")
	tok1, _ := lex.NextToken()
	tok2, _ := lex.NextToken()
	tok3, _ := lex.NextToken()

	if tok1.Type != TokenNumber || tok1.Value != "12" {
		t.Errorf("token 1: got %v %q, want Number 12", tok1.Type, tok1.Value)
	}
	if tok2.Type != TokenNumber || tok2.Value != "0" {
		t.Errorf("token 2: got %v %q, want Number 0", tok2.Type, tok2.Value)
	}
	if tok3.Type != TokenKeyword || tok3.Value != "obj" {
		t.Errorf("token 3: got %v %q, want Keyword obj", tok3.Type, tok3.Value)
	}
}

func TestNullByteInsideName_Multiple(t *testing.T) {
	// Multiple null bytes should all be stripped from names.
	lex := newLexer("/\x00He\x00lve\x00tica")
	tok, _ := lex.NextToken()
	if tok.Type != TokenName || tok.Value != "/Helvetica" {
		t.Errorf("got %v %q, want Name /Helvetica", tok.Type, tok.Value)
	}
}

func TestImplicitDictStartAfterNumber(t *testing.T) {
	// "2<<" should tokenize as Number "2" then Delimiter "<<".
	lex := newLexer("2<< /Type /Page >>")
	tok1, _ := lex.NextToken()
	tok2, _ := lex.NextToken()

	if tok1.Type != TokenNumber || tok1.Value != "2" {
		t.Errorf("token 1: got %v %q, want Number 2", tok1.Type, tok1.Value)
	}
	if tok2.Type != TokenDelimiter || tok2.Value != "<<" {
		t.Errorf("token 2: got %v %q, want Delimiter <<", tok2.Type, tok2.Value)
	}
}

func TestImplicitArrayInStream(t *testing.T) {
	// "[1 2<< /K /V >>]" should parse correctly.
	lex := newLexer("[1 2<< /K /V >>]")

	var tokens []Token
	for {
		tok, _ := lex.NextToken()
		if tok.Type == TokenEOF {
			break
		}
		tokens = append(tokens, tok)
	}

	expected := []struct {
		typ TokenType
		val string
	}{
		{TokenDelimiter, "["},
		{TokenNumber, "1"},
		{TokenNumber, "2"},
		{TokenDelimiter, "<<"},
		{TokenName, "/K"},
		{TokenName, "/V"},
		{TokenDelimiter, ">>"},
		{TokenDelimiter, "]"},
	}

	if len(tokens) != len(expected) {
		t.Fatalf("got %d tokens, want %d", len(tokens), len(expected))
	}

	for i, exp := range expected {
		if tokens[i].Type != exp.typ || tokens[i].Value != exp.val {
			t.Errorf("token %d: got %v %q, want %v %q", i, tokens[i].Type, tokens[i].Value, exp.typ, exp.val)
		}
	}
}

// TestLexer_ScientificNotationNumber confirms the lexer tolerates
// scientific-notation numbers that some producers emit even though
// PDF spec §7.3.3 does not list the form. Viewers (PDF.js) accept
// them; rejecting them drops the operand into the next operator's
// stack and corrupts the remainder of the content stream.
func TestLexer_ScientificNotationNumber(t *testing.T) {
	cases := []struct {
		input     string
		wantType  TokenType
		wantValue string
	}{
		{"1e2", TokenNumber, "1e2"},
		{"1.5e10", TokenNumber, "1.5e10"},
		{"-1E-3", TokenNumber, "-1E-3"},
		{"+2e+5", TokenNumber, "+2e+5"},
		{"1e", TokenKeyword, "1e"},       // incomplete exponent: no digits after 'e'
		{"e2", TokenKeyword, "e2"},       // no mantissa
		{"1e2e3", TokenKeyword, "1e2e3"}, // two exponents
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			lex := newLexer(tc.input)
			tok, err := lex.NextToken()
			if err != nil {
				t.Fatalf("NextToken: %v", err)
			}
			if tok.Type != tc.wantType {
				t.Errorf("type: got %v, want %v", tok.Type, tc.wantType)
			}
			if tok.Value != tc.wantValue {
				t.Errorf("value: got %q, want %q", tok.Value, tc.wantValue)
			}
		})
	}
}
