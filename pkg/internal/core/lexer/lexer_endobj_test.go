package lexer

import (
	"bytes"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ioat"
)

// TestNumberRunIntoEndobj pins that a number written hard against the endobj
// keyword lexes as that number, with endobj left as the next token.
//
// ISO 32000-1, 7.2.2 ends a token at a delimiter or white-space character, so
// "7559endobj" is one token by the letter of the spec and the object holding it
// resolves to nothing. When that object is a stream's /Length, the stream is
// left unreadable. Only endobj is split: a number run into any other keyword is
// still a keyword.
//
// # Corpus evidence
//
//   - pdfjs issue3566.pdf: writes object 120 that way and names it as the
//     /Length of the font program in object 14, which left the stream
//     unreadable.
func TestNumberRunIntoEndobj(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  []Token
	}{
		{
			"a number run into endobj",
			"7559endobj",
			[]Token{{Type: TokenNumber, Value: "7559"}, {Type: TokenKeyword, Value: "endobj"}},
		},
		{
			"the separated form is unchanged",
			"7559 endobj",
			[]Token{{Type: TokenNumber, Value: "7559"}, {Type: TokenKeyword, Value: "endobj"}},
		},
		{
			"a real number too",
			"1.5endobj",
			[]Token{{Type: TokenNumber, Value: "1.5"}, {Type: TokenKeyword, Value: "endobj"}},
		},
		{
			"endobj alone stays a keyword",
			"endobj",
			[]Token{{Type: TokenKeyword, Value: "endobj"}},
		},
		{
			"a number run into another keyword is left alone",
			"7559endstream",
			[]Token{{Type: TokenKeyword, Value: "7559endstream"}},
		},
		{
			"a bare keyword is left alone",
			"trailer",
			[]Token{{Type: TokenKeyword, Value: "trailer"}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			l := NewLexer(ioat.NewReader(bytes.NewReader([]byte(tc.input)), int64(len(tc.input)), 0))
			for i, want := range tc.want {
				got, err := l.NextToken()
				if err != nil {
					t.Fatalf("token %d: %v", i, err)
				}
				if got.Type != want.Type || got.Value != want.Value {
					t.Fatalf("token %d = {%v %q}, want {%v %q}", i, got.Type, got.Value, want.Type, want.Value)
				}
			}
			last, err := l.NextToken()
			if err != nil {
				t.Fatalf("trailing token: %v", err)
			}
			if last.Type != TokenEOF {
				t.Errorf("trailing token = {%v %q}, want EOF", last.Type, last.Value)
			}
		})
	}
}
