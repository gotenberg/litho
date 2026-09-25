package lexer

import (
	"bytes"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ioat"
)

// FuzzLexer feeds arbitrary bytes directly into the lexer.
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

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 64*1024 {
			return
		}

		r := bytes.NewReader(data)
		reader := ioat.NewReader(r, int64(len(data)), 0)
		lex := NewLexer(reader)

		for range 10000 {
			tok, err := lex.NextToken()
			if err != nil || tok.Type == TokenEOF {
				break
			}
		}
	})
}
