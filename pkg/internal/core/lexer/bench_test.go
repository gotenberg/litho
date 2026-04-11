package lexer

import (
	"bytes"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ioat"
)

// lexUntilEOF drains the token stream from the given byte slice. Used as
// the inner loop for all lexer benchmarks so each case measures only the
// tokenization work, not the iteration overhead.
func lexUntilEOF(b *testing.B, data []byte) {
	b.Helper()
	r := ioat.NewReader(bytes.NewReader(data), int64(len(data)), 0)
	l := NewLexer(r)
	for {
		tok, err := l.NextToken()
		if err != nil || tok.Type == TokenEOF {
			return
		}
	}
}

// BenchmarkLexer_Tokenize_Small measures tokenization of a short
// mixed-content stream (names, numbers, strings, dicts). This is the
// hottest path in the parser: every byte of every PDF flows through it.
func BenchmarkLexer_Tokenize_Small(b *testing.B) {
	data := []byte(`
<< /Type /Catalog /Pages 2 0 R /Version 1.7 /PageLayout /SinglePage
   /ViewerPreferences << /DisplayDocTitle true >>
   /ID [<6162636465666768> <6162636465666768>] >>
`)
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for range b.N {
		lexUntilEOF(b, data)
	}
}

// BenchmarkLexer_Tokenize_Strings measures literal-string parsing,
// including escape handling — the sub-path that has the most per-byte
// branching.
func BenchmarkLexer_Tokenize_Strings(b *testing.B) {
	data := []byte(`
(Simple text)
(Text with \\ backslash and \(parens\))
(Line 1\nLine 2\nLine 3)
(Octal \101\102\103 characters)
(Tab\tseparated\tvalues)
`)
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for range b.N {
		lexUntilEOF(b, data)
	}
}

// BenchmarkLexer_Tokenize_Names measures hex-escape decoding in PDF
// names, which is a disproportionately common operation in compliance
// profiles with long /Type names.
func BenchmarkLexer_Tokenize_Names(b *testing.B) {
	data := []byte(strings.Repeat(`/Type /Encoding /Differences /FontDescriptor `, 20))
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for range b.N {
		lexUntilEOF(b, data)
	}
}

// BenchmarkLexer_Tokenize_Large measures throughput on a larger input
// that stresses the buffer-refill path in ioat.Reader. The numbers here
// should scale roughly linearly with input size.
func BenchmarkLexer_Tokenize_Large(b *testing.B) {
	// ~20 KB of mixed content.
	segment := `
<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792]
   /Contents 10 0 R /Resources << /Font << /F1 4 0 R /F2 5 0 R >> >> >>
`
	data := []byte(strings.Repeat(segment, 200))
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for range b.N {
		lexUntilEOF(b, data)
	}
}
