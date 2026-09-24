package lexer

import (
	"fmt"
	"io"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ioat"
)

// maxStringAccum is the maximum number of bytes the lexer accumulates for a
// single string literal or hex string token. Malformed PDFs can contain
// unterminated strings that would otherwise cause the lexer to scan the
// entire file into memory. Anti-DoS guard, not spec-mandated: 10 MB is well
// above any legitimate string size.
const maxStringAccum = 10 << 20 // 10 MB

// TokenType represents the category of a lexed PDF token.
type TokenType int

// Token types produced by the lexer.
const (
	TokenEOF TokenType = iota
	TokenError
	TokenDelimiter
	TokenKeyword
	TokenName
	TokenNumber
	TokenString
	TokenHexString
)

// String returns the human-readable name of the token type.
func (t TokenType) String() string {
	switch t {
	case TokenEOF:
		return "EOF"
	case TokenError:
		return "Error"
	case TokenDelimiter:
		return "Delimiter"
	case TokenKeyword:
		return "Keyword"
	case TokenName:
		return "Name"
	case TokenNumber:
		return "Number"
	case TokenString:
		return "String"
	case TokenHexString:
		return "HexString"
	default:
		return "Unknown"
	}
}

// Token is a lexed PDF token carrying its type and raw string value.
type Token struct {
	Type  TokenType
	Value string
}

// Lexer tokenizes a PDF byte stream.
//
// Note on UnreadByte: throughout this file, UnreadByte errors are intentionally
// discarded. Every call is preceded by a successful ReadByte on the same
// ioat.Reader, which guarantees bufPos > 0 and UnreadByte cannot fail.
type Lexer struct {
	reader *ioat.Reader
}

// Reader returns the underlying [ioat.Reader] used by this lexer.
func (l *Lexer) Reader() *ioat.Reader {
	return l.reader
}

// NewLexer creates a new Lexer that tokenizes the PDF byte stream from r.
func NewLexer(r *ioat.Reader) *Lexer {
	return &Lexer{reader: r}
}

func isWhitespace(c byte) bool {
	return c == 0x00 || c == 0x09 || c == 0x0A || c == 0x0C || c == 0x0D || c == 0x20
}

func isDelimiter(c byte) bool {
	return c == '(' || c == ')' || c == '<' || c == '>' || c == '[' || c == ']' || c == '{' || c == '}' || c == '/' || c == '%'
}

// decodeHexPair converts two ASCII hex digit bytes into the byte they
// represent. Returns false if either byte is not a valid hex digit.
func decodeHexPair(hi, lo byte) (byte, bool) {
	h, ok1 := hexVal(hi)
	l, ok2 := hexVal(lo)
	if !ok1 || !ok2 {
		return 0, false
	}
	return h<<4 | l, true
}

func hexVal(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	default:
		return 0, false
	}
}

func isNumber(s string) bool {
	if len(s) == 0 {
		return false
	}
	i := 0
	if s[0] == '+' || s[0] == '-' {
		i++
	}
	if i == len(s) {
		return false
	}
	// Scan mantissa (optional decimal point, at least one digit).
	hasDot := false
	hasDigit := false
	for ; i < len(s); i++ {
		c := s[i]
		if c == '.' {
			if hasDot {
				return false
			}
			hasDot = true
			continue
		}
		if c >= '0' && c <= '9' {
			hasDigit = true
			continue
		}
		break
	}
	if !hasDigit {
		return false
	}
	if i == len(s) {
		return true
	}
	// Optional exponent: e/E, optional sign, at least one digit. PDF
	// spec (ISO 32000-1 §7.3.3) does not list scientific notation for
	// numbers, but some producers emit it (e.g. `/F1 1e2 Tf`). Viewers
	// accept these leniently; rejecting them pushes the text after
	// them onto the wrong operator and derails the entire text block.
	if c := s[i]; c != 'e' && c != 'E' {
		return false
	}
	i++
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	if i == len(s) {
		return false
	}
	for ; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// isDigitlessNumber reports whether s is a numeric object that carries a sign
// or a decimal point but no digit at all, such as "-", "+" or "-.".
//
// ISO 32000-2, 7.3.3 gives no meaning to such a token. Adobe Reader reads it as
// the number 0 rather than as a keyword, and pdf.js follows, naming this very
// case: its Lexer.getNumber returns 0 for a sign run that whitespace or a
// delimiter ends, "consistent with Adobe Reader". Reading it as a keyword
// instead turns it into a content stream operator that litho reports as
// undefined under ISO 19005-4, 6.2.2. It also mis-binds the operands around
// it, since an operator ends the operand list a number would have joined.
//
// Kept to a token of signs and dots alone. pdf.js throws on "-f", where a
// regular character follows the sign, and that stays a keyword here.
//
// # Corpus evidence
//
//   - pdfjs issue9252.pdf, issue15604.pdf, bug1753983.pdf, bug1953099.pdf:
//     pdf.js's Lexer.getNumber comment lists these files as the ones its
//     sign-run-as-0 reading fixes.
//   - pdfjs bug1753983.pdf: read as a keyword, its sign run draws a "-"
//     operator litho reports as undefined under ISO 19005-4, 6.2.2, and
//     veraPDF reports nothing.
func isDigitlessNumber(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] != '+' && s[i] != '-' && s[i] != '.' {
			return false
		}
	}
	return true
}

func (l *Lexer) readRegular() (Token, error) {
	var buf strings.Builder
	for {
		c, err := l.reader.ReadByte()
		if err != nil {
			if err == io.EOF {
				break
			}
			return Token{Type: TokenError, Value: err.Error()}, err
		}
		if isWhitespace(c) || isDelimiter(c) {
			_ = l.reader.UnreadByte() // Guaranteed to succeed: previous ReadByte succeeded on a buffered reader.
			break
		}
		buf.WriteByte(c)
	}

	val := buf.String()
	if isNumber(val) {
		return Token{Type: TokenNumber, Value: val}, nil
	}
	if isDigitlessNumber(val) {
		return Token{Type: TokenNumber, Value: "0"}, nil
	}
	if num, ok := splitNumberEndobj(val); ok {
		// Rewind so the next token is the endobj keyword itself, leaving the
		// reader where a conforming file would have put it. Seeking back inside
		// the token just read cannot fail on a buffered reader.
		if _, err := l.reader.Seek(-int64(len(endobjKeyword)), io.SeekCurrent); err == nil {
			return Token{Type: TokenNumber, Value: num}, nil
		}
	}
	return Token{Type: TokenKeyword, Value: val}, nil
}

// endobjKeyword is the keyword [splitNumberEndobj] splits off a number.
const endobjKeyword = "endobj"

// splitNumberEndobj reports a number token written hard against the endobj
// keyword, as in "7559endobj", and returns the number alone.
//
// ISO 32000-1, 7.2.2 ends a token at a delimiter or white-space character, and
// "e" is neither, so "7559endobj" lexes as one keyword and the object holding
// it resolves to nothing. Producers do emit it, and when that object is a
// stream's /Length the whole stream becomes unreadable and the checks keyed on
// its extent go silent. litho still reports the missing separator (ISO
// 19005-2, 6.1.9 t1 judges the raw bytes), so accepting the number here loses
// no finding.
//
// Only the endobj keyword is split. A number run together with any other
// keyword is left alone, because a wider rule would start guessing at token
// boundaries.
//
// # Corpus evidence
//
//   - pdfjs issue3566.pdf: writes object 120 as "120 0 obj\n7559endobj", and
//     that object is the /Length of the Type1C font program in object 14, so
//     the whole stream became unreadable and the checks keyed on its extent
//     went silent. veraPDF reads the file the same way litho now does.
//   - All corpora: no file runs a number together with any keyword other
//     than endobj.
func splitNumberEndobj(val string) (string, bool) {
	num, ok := strings.CutSuffix(val, endobjKeyword)
	if !ok || num == "" {
		return "", false
	}
	if !isNumber(num) {
		return "", false
	}
	return num, true
}

// NextToken reads and returns the next PDF token from the input stream.
//
// A NUL byte is white space inside a name as everywhere else (ISO 32000-1,
// 7.2.2, Table 1 and 7.3.5), so it ends the name instead of being skipped.
// PDF.js, veraPDF, pdfcpu and qpdf end a name at a NUL the same way.
//
// # Corpus evidence
//
//   - All corpora: two files carry a NUL inside a name in an object body, a
//     content stream or a CMap. Ending the name there changes neither file's
//     page count nor its extracted text, and every other file opens, counts
//     its pages, extracts text and validates exactly as before.
//   - pdfjs issue9195.pdf: its producer, Fineco Software v2.05, writes the
//     ToUnicode CMap in UTF-16LE, so a NUL follows every character and every
//     name reads "/\x00C\x00I\x00D...". The CMap's keywords split on the same
//     NULs, so skipping them inside names never made the CMap readable.
//   - pdfjs poppler-90-0-fuzzed.pdf: fuzzed content-stream bytes. Ending the
//     name at the NUL exposes 28 more undefined operators under ISO 19005-2,
//     6.2.2 t1 in a file that already fails that test 1157 times.
func (l *Lexer) NextToken() (Token, error) {
	for {
		c, err := l.reader.ReadByte()
		if err != nil {
			if err == io.EOF {
				return Token{Type: TokenEOF, Value: ""}, nil
			}
			return Token{Type: TokenError, Value: err.Error()}, err
		}

		if isWhitespace(c) {
			continue
		}

		if c == '%' {
			// Skip comment until end of line
			for {
				cc, err := l.reader.ReadByte()
				if err != nil {
					if err == io.EOF {
						return Token{Type: TokenEOF, Value: ""}, nil
					}
					return Token{Type: TokenError, Value: err.Error()}, err
				}
				if cc == '\n' || cc == '\r' {
					break
				}
			}
			continue
		}

		switch c {
		case '/':
			var buf strings.Builder
			buf.WriteByte(c)
			for {
				cc, err := l.reader.ReadByte()
				if err != nil {
					if err == io.EOF {
						break
					}
					return Token{Type: TokenError, Value: err.Error()}, err
				}
				// ISO 32000-1, 7.2.2, Table 1 makes NUL a white-space
				// character, and 7.3.5 builds a name from "any characters
				// (8-bit values) except null" and ends it at white space, so
				// a NUL ends the name like any other white space: "/Ty\x00pe"
				// is the name Ty followed by the keyword pe.
				if isWhitespace(cc) || isDelimiter(cc) {
					_ = l.reader.UnreadByte() // Guaranteed to succeed: previous ReadByte succeeded on a buffered reader.
					break
				}
				// Per ISO 32000-2 §7.3.5, # introduces a two-digit hex code.
				if cc == '#' {
					hi, errHi := l.reader.ReadByte()
					if errHi != nil {
						break
					}
					lo, errLo := l.reader.ReadByte()
					if errLo != nil {
						buf.WriteByte(hi)
						break
					}
					decoded, ok := decodeHexPair(hi, lo)
					if ok {
						buf.WriteByte(decoded)
					} else {
						buf.WriteByte('#')
						buf.WriteByte(hi)
						buf.WriteByte(lo)
					}
					continue
				}
				buf.WriteByte(cc)
			}
			return Token{Type: TokenName, Value: buf.String()}, nil

		case '(':
			var buf strings.Builder
			buf.WriteByte(c)
			nesting := 1
			for {
				cc, err := l.reader.ReadByte()
				if err != nil {
					return Token{Type: TokenError, Value: "unexpected EOF in string literal"}, err
				}
				buf.WriteByte(cc)
				if buf.Len() > maxStringAccum {
					return Token{Type: TokenError, Value: "string literal exceeds maximum size"}, fmt.Errorf("string literal exceeds %d bytes", maxStringAccum)
				}
				if cc == '\\' {
					nextC, err := l.reader.ReadByte()
					if err != nil {
						return Token{Type: TokenError, Value: "unexpected EOF in string escape"}, err
					}
					buf.WriteByte(nextC)
					continue
				}
				if cc == '(' {
					nesting++
				} else if cc == ')' {
					nesting--
					if nesting == 0 {
						break
					}
				}
			}
			return Token{Type: TokenString, Value: buf.String()}, nil

		case ')':
			return Token{Type: TokenDelimiter, Value: ")"}, nil

		case '<':
			nextC, err := l.reader.ReadByte()
			if err != nil {
				if err == io.EOF {
					return Token{Type: TokenDelimiter, Value: "<"}, nil
				}
				return Token{Type: TokenError, Value: err.Error()}, err
			}
			if nextC == '<' {
				return Token{Type: TokenDelimiter, Value: "<<"}, nil
			}
			_ = l.reader.UnreadByte() // Guaranteed to succeed: previous ReadByte succeeded on a buffered reader.

			// Hex string
			var buf strings.Builder
			buf.WriteByte(c)
			for {
				cc, err := l.reader.ReadByte()
				if err != nil {
					return Token{Type: TokenError, Value: "unexpected EOF in hex string"}, err
				}
				buf.WriteByte(cc)
				if cc == '>' {
					break
				}
				if buf.Len() > maxStringAccum {
					return Token{Type: TokenError, Value: "hex string exceeds maximum size"}, fmt.Errorf("hex string exceeds %d bytes", maxStringAccum)
				}
			}
			return Token{Type: TokenHexString, Value: buf.String()}, nil

		case '>':
			nextC, err := l.reader.ReadByte()
			if err != nil {
				if err == io.EOF {
					return Token{Type: TokenDelimiter, Value: ">"}, nil
				}
				return Token{Type: TokenError, Value: err.Error()}, err
			}
			if nextC == '>' {
				return Token{Type: TokenDelimiter, Value: ">>"}, nil
			}
			_ = l.reader.UnreadByte() // Guaranteed to succeed: previous ReadByte succeeded on a buffered reader.
			return Token{Type: TokenDelimiter, Value: ">"}, nil

		case '[', ']', '{', '}':
			return Token{Type: TokenDelimiter, Value: string(c)}, nil

		default:
			_ = l.reader.UnreadByte() // Guaranteed to succeed: previous ReadByte succeeded on a buffered reader.
			return l.readRegular()
		}
	}
}
