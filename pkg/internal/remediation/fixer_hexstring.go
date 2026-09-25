package remediation

import (
	"context"
	"fmt"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
	"github.com/gotenberg/litho/pkg/internal/core/parser"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// oddHexDigits and invalidHexChar are the parser's wordings for the two ways a
// hexadecimal string is written wrongly, both of which the fixer below repairs
// by respelling the string. A [compliance.Violation] carries the parser's
// finding only as text in its Context, so [matchHexString] reads these; they
// come from the parser's sentinels so the two cannot drift apart.
var (
	oddHexDigits   = parser.ErrHexStringOddDigits.Error()
	invalidHexChar = parser.ErrHexStringInvalidChar.Error()
)

// matchHexString matches a malformed hex string inside a content stream: an
// odd number of non-white-space characters (ISO 19005-1/2/3 §6.1.6 test 1,
// ISO 19005-4 §6.1.5 test 1) or a character that is no hexadecimal digit
// (test 2). Respelling the string repairs both, so the fixer claims both.
func matchHexString(v compliance.Violation) bool {
	if !clausePrefix(v, "6.1.5", "6.1.6") {
		return false
	}
	return contextContains(v, oddHexDigits) || contextContains(v, invalidHexChar)
}

// fixHexString rewrites the content streams that hold a malformed hex string:
// an odd number of non-white-space characters, or a character that is no
// hexadecimal digit.
//
// ISO 32000-1, 7.3.4.3 lets a reader assume a missing final digit is 0, and
// litho's parser does exactly that, so the value is already understood; the
// defect is in how it is written. Each such string is respelled with the value
// the reader understood ([wellFormedHexString]), and nothing else in the
// stream moves.
//
// The streams are the ones the hex string tests judge, which
// [walkContentStreams] reaches: a page's own content, and the form XObjects,
// tiling patterns, Type 3 glyphs and annotation appearances it draws.
// Repairing the page alone left the findings compliance states for the others
// as residuals after remediation.
//
// The full rewrite the file-structure fixer asks for does not reach these: it
// re-serialises objects, and a content stream's body is carried across as
// bytes. Those violations were reported handled and came back as residuals on
// every pass.
//
// # Corpus evidence
//
//   - All corpora: 8 files hold a malformed hex string in page content, pdfjs
//     issue11678.pdf, issue18645.pdf and six veraPDF 6.1.5/6.1.6 fail files,
//     and none in another stream a hex string test judges. Respelling the
//     strings in place instead of re-serialising the page leaves the
//     violations litho and veraPDF report after a PDF/A-1b, PDF/A-2b or
//     PDF/A-4 remediation, default or aggressive, and the extracted text
//     unchanged.
//   - pdfjs poppler-90-0-fuzzed.pdf: the only file holding one outside page
//     content, in two fuzzed Type 3 glyphs, where neither litho nor veraPDF
//     judges it, so the fixer is never asked to run there.
func fixHexString(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	n, err := walkContentStreams(ctx, alloc, doc, "hex-string", func(ctx context.Context, data []byte) ([]byte, func()) {
		newData, n := respellHexStrings(ctx, data)
		if n == 0 {
			return nil, nil
		}
		return newData, func() {}
	})
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("rewrote %d content stream(s) holding a malformed hex string", n)}, nil
}

// respellHexStrings returns data with every malformed hex string respelled by
// [wellFormedHexString], and how many it respelled. The strings are spliced in
// place, so every other byte of the stream, inline image data included, is
// carried across unchanged. A stream the lexer cannot read to its end has the
// strings before the failure respelled and the rest left as written.
//
// Re-serialising the parsed operations instead, as this repair used to, could
// not round-trip an inline image: the parser consumes its data off-token, so a
// page holding one came back as "<< ... >> BI" with no ID, data or EI, a
// content stream no reader can parse (ISO 32000-1, 8.9.7). A hex string inside
// an inline image's own dictionary is left as written: the walk skips the
// dictionary with the data, which it cannot address token by token.
func respellHexStrings(ctx context.Context, data []byte) ([]byte, int) {
	splices, _ := contentTokenSplices(ctx, data, func(tok lexer.Token, _ int) (string, bool) {
		if tok.Type != lexer.TokenHexString {
			return "", false
		}
		return wellFormedHexString(tok.Value)
	})
	if len(splices) == 0 {
		return data, 0
	}
	return applyTokenSplices(data, splices), len(splices)
}

// wellFormedHexString returns the well-formed spelling of the hex string token
// tok, written "<" hex digits ">", and whether tok is malformed: it has an odd
// number of non-white-space characters, or one of them is no hexadecimal
// digit. A well-formed token returns false.
//
// The spelling keeps the value every reader takes from the malformed one: the
// characters that are no hexadecimal digit go, and a missing final digit is 0
// (ISO 32000-1, 7.3.4.3), which is how litho's parser, veraPDF and PDF.js
// (Lexer.getHexString) read it.
//
// The parity counts non-white-space characters, invalid ones included (ISO
// 19005-2, 6.1.6 t1, which veraPDF measures the same way), so "<41!4>" has an
// even count and is malformed for its invalid character alone. A repair that
// counted hex digits instead read it as odd, and one that tested parity alone
// left the character in place.
//
// # Corpus evidence
//
//   - verapdf: once the parity test counted non-white-space characters, the
//     invalid character left unrepaired cost 30 files their remediated
//     verdict.
func wellFormedHexString(tok string) (string, bool) {
	body := strings.TrimSuffix(strings.TrimPrefix(tok, "<"), ">")
	var digits strings.Builder
	digits.WriteByte('<')
	nonWhiteSpace, malformed := 0, false
	for i := 0; i < len(body); i++ {
		c := body[i]
		// White space of ISO 32000-1, 7.2.2, Table 1.
		if c == 0x00 || c == '\t' || c == '\n' || c == 0x0C || c == '\r' || c == ' ' {
			continue
		}
		nonWhiteSpace++
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			malformed = true
			continue
		}
		digits.WriteByte(c)
	}
	if nonWhiteSpace%2 != 0 {
		malformed = true
	}
	if !malformed {
		return "", false
	}
	if (digits.Len()-1)%2 != 0 {
		digits.WriteByte('0')
	}
	digits.WriteByte('>')
	return digits.String(), true
}
