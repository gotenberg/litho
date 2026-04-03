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
// entire file into memory. 10 MB is well above any legitimate string size.
const maxStringAccum = 10 << 20 // 10 MB

// TokenType represents the category of a lexed PDF token.
type TokenType int

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

func (l *Lexer) GetReader() *ioat.Reader {
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
	hasDot := false
	hasDigit := false
	for ; i < len(s); i++ {
		switch {
		case s[i] == '.':
			if hasDot {
				return false
			}
			hasDot = true
		case s[i] >= '0' && s[i] <= '9':
			hasDigit = true
		default:
			return false
		}
	}
	return hasDigit
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
			_ = l.reader.UnreadByte()
			break
		}
		buf.WriteByte(c)
	}

	val := buf.String()
	if isNumber(val) {
		return Token{Type: TokenNumber, Value: val}, nil
	}
	return Token{Type: TokenKeyword, Value: val}, nil
}

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
				// Null bytes inside names are silently stripped (Acrobat compat).
				if cc == 0x00 {
					continue
				}
				if isWhitespace(cc) || isDelimiter(cc) {
					_ = l.reader.UnreadByte()
					break
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
			_ = l.reader.UnreadByte()

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
			_ = l.reader.UnreadByte()
			return Token{Type: TokenDelimiter, Value: ">"}, nil

		case '[', ']', '{', '}':
			return Token{Type: TokenDelimiter, Value: string(c)}, nil

		default:
			_ = l.reader.UnreadByte()
			return l.readRegular()
		}
	}
}
