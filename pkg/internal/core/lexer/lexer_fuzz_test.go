package lexer

import (
	"bytes"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ioat"
)

// FuzzLexer feeds arbitrary bytes directly into the lexer, with and without a
// command set, and read as veraPDF reads a content stream, for validation and
// for a repair. Read for validation, every number token must hold a number the
// parser reads: a real with no exponent, or a run of digits.
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
	f.Add([]byte("12m .020.594 - + -. --5 +-5 1e2 -1e-2 12#3 95.c.455 99999999999999999999 5-3"))
	f.Add([]byte("0 0 m 1.5."))

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 64*1024 {
			return
		}

		for i, cs := range []*CommandSet{nil, testCommands, nil, nil} {
			r := bytes.NewReader(data)
			reader := ioat.NewReader(r, int64(len(data)), 0)
			lex := NewLexer(reader).WithCommands(cs)
			switch i {
			case 2:
				lex.WithVeraPDFTokens()
			case 3:
				lex.WithVeraPDFTokens().WithRepairTokens()
			}

			for range 10000 {
				tok, err := lex.NextToken()
				if err != nil || tok.Type == TokenEOF {
					break
				}
				if i == 2 && tok.Type == TokenNumber && !parserReads(tok.Value) {
					t.Fatalf("number token %q holds no number the parser reads", tok.Value)
				}
			}
		}
	})
}

// parserReads reports whether the parser reads v, a number token's value, as
// a number: a real with no exponent, or a run of digits after an optional
// sign, which it reads as an integer or, past 64 bits, as a real.
func parserReads(v string) bool {
	if strings.Contains(v, ".") {
		_, err := strconv.ParseFloat(v, 64)
		return err == nil && !strings.ContainsAny(v, "eE")
	}
	if _, err := strconv.ParseInt(v, 10, 64); err == nil {
		return true
	}
	digits := strings.TrimLeft(v, "+-")
	if digits == "" || len(v)-len(digits) > 1 || strings.Trim(digits, "0123456789") != "" {
		return false
	}
	_, err := strconv.ParseFloat(digits, 64)
	return err == nil
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
