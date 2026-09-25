package lexer

import (
	"bytes"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ioat"
)

// FuzzLexer feeds arbitrary bytes directly into the lexer, with and without a
// command set, and with digit boundaries.
func FuzzLexer(f *testing.F) {
	f.Add([]byte("/Name"))
	f.Add([]byte("/A#42"))
	f.Add([]byte("<</F#>>"))
	f.Add([]byte("/A#4"))
	f.Add([]byte("(string)"))
	f.Add([]byte("<AABB>"))
	f.Add([]byte("<< >>"))
	f.Add([]byte("[1 2 3]"))
	f.Add([]byte("42"))
	f.Add([]byte("% comment\n42"))
	f.Add([]byte{0x00, 0xFF, '(', ')', '<', '>'})
	f.Add([]byte("ETBT qQ BTET ETXX TfT* q1 falsef BDX"))
	f.Add([]byte("BT (a) TjET"))
	f.Add([]byte("ETB"))
	f.Add([]byte("q1 0 0 1 0 0 cm xx1yy d01 xd0 n1 BT1 1q"))

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 64*1024 {
			return
		}

		for i, cs := range []*CommandSet{nil, testCommands, nil} {
			r := bytes.NewReader(data)
			reader := ioat.NewReader(r, int64(len(data)), 0)
			lex := NewLexer(reader).WithCommands(cs)
			if i == 2 {
				lex.WithDigitBoundaries()
			}

			for range 10000 {
				tok, err := lex.NextToken()
				if err != nil || tok.Type == TokenEOF {
					break
				}
			}
		}
	})
}

// FuzzCommandSetSplit asserts that [CommandSet.Split] splits a run of regular
// characters where the lexer does, whenever every piece the lexer reads is a
// keyword: a number piece may be respelled, as "-" reads as 0.
func FuzzCommandSetSplit(f *testing.F) {
	for _, s := range []string{"ETBT", "qQ", "BTET", "ETXX", "TfT*", "T*Tf", "falsef", "BDX", "BDC", "ETB", "x"} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, run string) {
		if run == "" || len(run) > 64*1024 {
			return
		}
		for i := range len(run) {
			if isWhitespace(run[i]) || isDelimiter(run[i]) {
				return
			}
		}
		var lexed []string
		lex := newLexer(run).WithCommands(testCommands)
		for range len(run) + 1 {
			tok, err := lex.NextToken()
			if err != nil {
				t.Fatalf("NextToken(%q): %v", run, err)
			}
			if tok.Type == TokenEOF {
				break
			}
			if tok.Type != TokenKeyword {
				return
			}
			lexed = append(lexed, tok.Value)
		}
		if got := testCommands.Split(run); !slices.Equal(got, lexed) {
			t.Fatalf("Split(%q) = %q, the lexer reads %q", run, got, lexed)
		}
	})
}
