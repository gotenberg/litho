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

// lexAll returns every token of input up to EOF.
func lexAll(t *testing.T, input string) []Token {
	t.Helper()
	lex := newLexer(input)
	var tokens []Token
	for {
		tok, err := lex.NextToken()
		if err != nil {
			t.Fatalf("NextToken failed: %v", err)
		}
		if tok.Type == TokenEOF {
			return tokens
		}
		tokens = append(tokens, tok)
	}
}

// TestNullByteEndsName locks the ISO 32000-1 reading of a NUL inside a name:
// 7.2.2, Table 1 makes NUL a white-space character and 7.3.5 ends a name at
// white space, so "/Ty\x00pe" is the name Ty followed by the keyword pe, not
// the name Type. PDF.js, veraPDF, pdfcpu and qpdf read it the same way.
func TestNullByteEndsName(t *testing.T) {
	cases := []struct {
		input string
		want  []Token
	}{
		{"/Ty\x00pe", []Token{{TokenName, "/Ty"}, {TokenKeyword, "pe"}}},
		{"/\x00He\x00lve\x00tica", []Token{{TokenName, "/"}, {TokenKeyword, "He"}, {TokenKeyword, "lve"}, {TokenKeyword, "tica"}}},
		{"/Type\x00/Page", []Token{{TokenName, "/Type"}, {TokenName, "/Page"}}},
		{"/F1\x00\x0012 Tf", []Token{{TokenName, "/F1"}, {TokenNumber, "12"}, {TokenKeyword, "Tf"}}},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := lexAll(t, tc.input)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d tokens %q, want %d %q", len(got), got, len(tc.want), tc.want)
			}
			for i, want := range tc.want {
				if got[i] != want {
					t.Errorf("token %d: got %v %q, want %v %q", i, got[i].Type, got[i].Value, want.Type, want.Value)
				}
			}
		})
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
// ISO 32000-1, 7.3.3 does not list the form. Viewers accept them;
// rejecting them drops the operand into the next operator's stack
// and corrupts the remainder of the content stream.
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

// TestNameMalformedEscape locks the reading of a NUMBER SIGN that ISO
// 32000-1, 7.3.5 does not follow with two hexadecimal digits. The name still
// ends at white space or a delimiter, so the bytes after the NUMBER SIGN are
// never swallowed when they end it: "/F#>>" keeps its dictionary close and
// "/A#0 1" keeps its integer. Everything else mirrors PDF.js (Lexer.getName).
func TestNameMalformedEscape(t *testing.T) {
	cases := []struct {
		input string
		want  []Token
	}{
		{"<</K /F#>>", []Token{{TokenDelimiter, "<<"}, {TokenName, "/K"}, {TokenName, "/F#"}, {TokenDelimiter, ">>"}}},
		{"/A#0 1", []Token{{TokenName, "/A#0"}, {TokenNumber, "1"}}},
		{"/A#0\x001", []Token{{TokenName, "/A#0"}, {TokenNumber, "1"}}},
		{"/A#\x00B", []Token{{TokenName, "/A#"}, {TokenKeyword, "B"}}},
		{"/A# 1", []Token{{TokenName, "/A#"}, {TokenNumber, "1"}}},
		{"/A#0/B", []Token{{TokenName, "/A#0"}, {TokenName, "/B"}}},
		{"[/A#]", []Token{{TokenDelimiter, "["}, {TokenName, "/A#"}, {TokenDelimiter, "]"}}},
		{"/A#(x)", []Token{{TokenName, "/A#"}, {TokenString, "(x)"}}},
		{"/A#G1", []Token{{TokenName, "/A#G1"}}},
		{"/A#4G", []Token{{TokenName, "/A#4G"}}},
		{"/A##41", []Token{{TokenName, "/A##41"}}},
		{"/A#4#41", []Token{{TokenName, "/A#4#41"}}},
		{"/A#", []Token{{TokenName, "/A#"}}},
		{"/A#4", []Token{{TokenName, "/A#4"}}},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := lexAll(t, tc.input)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d tokens %q, want %d %q", len(got), got, len(tc.want), tc.want)
			}
			for i, want := range tc.want {
				if got[i] != want {
					t.Errorf("token %d: got %v %q, want %v %q", i, got[i].Type, got[i].Value, want.Type, want.Value)
				}
			}
		})
	}
}

// TestNameHexEscape locks the well-formed escapes of ISO 32000-1, 7.3.5,
// Table 4, in either case of hexadecimal digit. "#00" decodes to NUL, as
// PDF.js and veraPDF decode it.
func TestNameHexEscape(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"/Lime#20Green", "/Lime Green"},
		{"/paired#28#29parentheses", "/paired()parentheses"},
		{"/The_Key_of_F#23_Minor", "/The_Key_of_F#_Minor"},
		{"/A#42", "/AB"},
		{"/A#4a", "/AJ"},
		{"/A#00B", "/A\x00B"},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := lexAll(t, tc.input)
			if len(got) != 1 || got[0] != (Token{TokenName, tc.want}) {
				t.Errorf("got %q, want one name %q", got, tc.want)
			}
		})
	}
}
