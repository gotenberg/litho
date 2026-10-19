package content

import (
	"bytes"
	"context"

	"github.com/gotenberg/litho/pkg/internal/core/ioat"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
)

// TerminatedEnd returns how much of a content stream content written after it
// can follow and still be read as operators: all of it, unless the stream ends
// inside a literal or hex string that never terminates, in which case it is
// the end of the last operator, or 0 when none precedes it. Such a string runs to the end of the content
// (ISO 32000-1, 7.3.4.2 and 7.3.4.3), and a page's /Contents streams are read
// as one (7.8.2), so whatever a writer appends after the last byte, an EMC, a
// restoring Q or a stamp, is read as string bytes, not as operators. The
// bytes past the last operator paint nothing, since no operator follows them,
// and viewers discard the unterminated token at the end of the content, so
// content cut there renders as the whole did. A stream litho does not parse is
// left whole.
//
// # Corpus evidence
//
//   - pdfjs poppler-90-0-fuzzed.pdf: page 2 opens a literal string 2 KB into
//     its 22 KB stream that nothing closes, and the /Artifact sequence
//     remediation wrapped the page in stayed open, so veraPDF reported ISO
//     14289-1, 7.1 t3 and ISO 14289-2, 8.2.2 t1 on the remediated output.
//   - litho danglingstr_ua1.pdf: the stamp the mutate-stamp cell appends was
//     read inside the page's unterminated string, so qpdf reported the stamped
//     page as not carrying it.
func TerminatedEnd(ctx context.Context, data []byte) int {
	// Every token of a stream that terminates ends before the stream does, so
	// the parse below, which costs more than the scan, is for the rare stream
	// that does not.
	if tokensTerminate(data) {
		return len(data)
	}
	_, ranges, err := ParseStreamDataWithOffsets(ctx, data)
	if err != nil {
		return len(data)
	}
	// A stream with no operator before its dangling token paints nothing.
	last := 0
	if len(ranges) > 0 {
		last = ranges[len(ranges)-1].End
	}
	if last < 0 || last > len(data) || tokensTerminate(data[last:]) {
		return len(data)
	}
	return last
}

// tokensTerminate reports whether every token in b ends before b does, that
// is, whether b holds no string the lexer reaches the end of b inside. Each
// token consumes at least one byte, so the loop ends.
func tokensTerminate(b []byte) bool {
	lex := lexer.NewLexer(ioat.NewReader(bytes.NewReader(b), int64(len(b)), 0)).WithVeraPDFTokens()
	for {
		tok, err := lex.NextToken()
		if tok.Type == lexer.TokenError {
			return false
		}
		if tok.Type == lexer.TokenEOF || err != nil {
			return true
		}
	}
}
