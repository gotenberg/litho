package parser

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
)

// DecryptFunc is a callback used to decrypt raw string or stream data during parsing.
type DecryptFunc func([]byte) ([]byte, error)

// Parser handles the parsing of PDF objects from a token stream.
//
// Note on UnreadByte: throughout this file, UnreadByte errors on ioat.Reader
// are intentionally discarded. Every call is preceded by a successful ReadByte,
// which guarantees bufPos > 0 and UnreadByte cannot fail.
type Parser struct {
	lex       *lexer.Lexer
	ctx       context.Context
	tokens    []lexer.Token
	err       error
	decryptFn DecryptFunc

	// FormatViolations collects PDF/A format compliance issues detected
	// during parsing. The parser remains lenient — violations are recorded
	// but do not fail the parse. This is nil unless compliance checking
	// is enabled via WithFormatChecking.
	FormatViolations []error
	checkFormat      bool
}

// NewParser creates a new Parser that reads PDF objects from the given lexer.
func NewParser(lex *lexer.Lexer) *Parser {
	return &Parser{lex: lex}
}

// WithContext attaches a context for cancellation checks during parsing.
// The parser checks ctx.Err() at natural checkpoints (per dict key, per
// array element, per stream scan iteration) without adding overhead to
// the per-token lexer loop.
func (p *Parser) WithContext(ctx context.Context) *Parser {
	p.ctx = ctx
	return p
}

func (p *Parser) WithDecryption(fn DecryptFunc) *Parser {
	p.decryptFn = fn
	return p
}

// WithFormatChecking enables PDF/A format violation collection during
// parsing. Violations are appended to FormatViolations without failing
// the parse.
func (p *Parser) WithFormatChecking() *Parser {
	p.checkFormat = true
	return p
}

func (p *Parser) recordViolation(msg string) {
	if p.checkFormat {
		p.FormatViolations = append(p.FormatViolations, fmt.Errorf("compliance: %s", msg))
	}
}

// checkCtx returns the context error if cancellation or deadline has fired.
// Returns nil when no context is set or the context is still active.
func (p *Parser) checkCtx() error {
	if p.ctx != nil {
		return p.ctx.Err()
	}
	return nil
}

// CheckContext returns the context error if a context was set via [Parser.WithContext].
// Returns nil if no context is set or the context is not cancelled.
func (p *Parser) CheckContext() error {
	return p.checkCtx()
}

func (p *Parser) Peek(n int) (lexer.Token, error) {
	for len(p.tokens) <= n {
		if p.err != nil {
			return lexer.Token{Type: lexer.TokenEOF}, p.err
		}
		tok, err := p.lex.NextToken()
		p.tokens = append(p.tokens, tok)
		if err != nil && err != io.EOF {
			p.err = err
		}
	}
	return p.tokens[n], nil
}

func (p *Parser) Consume() (lexer.Token, error) {
	tok, err := p.Peek(0)
	if len(p.tokens) > 0 {
		p.tokens = p.tokens[1:]
	}
	return tok, err
}

func (p *Parser) Expect(expectedType lexer.TokenType) (lexer.Token, error) {
	tok, err := p.Consume()
	if err != nil {
		return lexer.Token{}, fmt.Errorf("expected token %v: %w", expectedType, err)
	}
	if tok.Type != expectedType {
		return lexer.Token{}, fmt.Errorf("syntax error: expected token %v, got %v (%q)", expectedType, tok.Type, tok.Value)
	}
	return tok, nil
}

func (p *Parser) ParseObject() (ast.PdfObject, error) {
	t0, err := p.Peek(0)
	if err != nil && t0.Type == lexer.TokenError {
		return nil, err
	}
	if t0.Type == lexer.TokenEOF {
		return nil, io.EOF
	}

	// Check for indirect ref `12 0 R` or indirect obj `12 0 obj`
	if t0.Type == lexer.TokenNumber {
		t1, err := p.Peek(1)
		if err != nil {
			return nil, err
		}
		if t1.Type == lexer.TokenNumber {
			t2, err := p.Peek(2)
			if err != nil {
				return nil, err
			}
			if t2.Type == lexer.TokenKeyword && t2.Value == "R" {
				if _, err := p.Expect(lexer.TokenNumber); err != nil {
					return nil, err
				}
				if _, err := p.Expect(lexer.TokenNumber); err != nil {
					return nil, err
				}
				if _, err := p.Expect(lexer.TokenKeyword); err != nil {
					return nil, err
				}
				objNum, err := strconv.Atoi(t0.Value)
				if err != nil {
					return nil, fmt.Errorf("invalid object number %q: %w", t0.Value, err)
				}
				genNum, err := strconv.Atoi(t1.Value)
				if err != nil {
					return nil, fmt.Errorf("invalid generation number %q: %w", t1.Value, err)
				}
				return ast.PdfIndirectRef{ObjectNum: objNum, GenNum: genNum}, nil
			}
			if t2.Type == lexer.TokenKeyword && t2.Value == "obj" {
				return p.parseIndirectObject()
			}
		}
	}

	t0, err = p.Consume()
	if err != nil {
		return nil, err
	}

	switch t0.Type {
	case lexer.TokenEOF, lexer.TokenError:
		return nil, fmt.Errorf("unexpected token type: %v", t0.Type)
	case lexer.TokenKeyword:
		switch t0.Value {
		case "null":
			return ast.PdfNull{}, nil
		case "true":
			return ast.PdfBool(true), nil
		case "false":
			return ast.PdfBool(false), nil
		default:
			return nil, fmt.Errorf("unexpected keyword: %s", t0.Value)
		}
	case lexer.TokenName:
		if len(t0.Value) > 0 && t0.Value[0] == '/' {
			return ast.PdfName(t0.Value[1:]), nil
		}
		return ast.PdfName(t0.Value), nil
	case lexer.TokenNumber:
		if strings.ContainsRune(t0.Value, '.') {
			f, err := strconv.ParseFloat(t0.Value, 64)
			if err != nil {
				return nil, err
			}
			return ast.PdfReal(f), nil
		}
		i, err := strconv.ParseInt(t0.Value, 10, 64)
		if err != nil {
			return nil, err
		}
		return ast.PdfInt(i), nil
	case lexer.TokenString:
		return p.parseString(t0.Value)
	case lexer.TokenHexString:
		return p.parseHexString(t0.Value)
	case lexer.TokenDelimiter:
		switch t0.Value {
		case "<<":
			return p.parseDict()
		case "[":
			return p.parseArray()
		default:
			return nil, fmt.Errorf("unexpected delimiter: %s", t0.Value)
		}
	}

	return nil, fmt.Errorf("unexpected token: %v", t0)
}

func (p *Parser) parseIndirectObject() (ast.PdfObject, error) {
	if _, err := p.Expect(lexer.TokenNumber); err != nil {
		return nil, err
	}
	if _, err := p.Expect(lexer.TokenNumber); err != nil {
		return nil, err
	}
	if _, err := p.Expect(lexer.TokenKeyword); err != nil {
		return nil, err
	}

	obj, err := p.ParseObject()
	if err != nil {
		return nil, err
	}

	// Consume the endobj keyword if present. Tolerate missing endobj (Acrobat compat).
	// Peek returns TokenEOF on error, so the type guard safely prevents Consume on failure.
	t, _ := p.Peek(0) //nolint:errcheck // EOF is expected; type guard below handles it
	if t.Type == lexer.TokenKeyword && t.Value == "endobj" {
		_, _ = p.Consume() //nolint:errcheck // Token already validated by Peek
	}

	return obj, nil
}

func (p *Parser) parseDict() (ast.PdfObject, error) {
	dict := make(ast.PdfDict)
	for {
		if err := p.checkCtx(); err != nil {
			return nil, fmt.Errorf("parsing dict: %w", err)
		}
		t, err := p.Peek(0)
		if err != nil && t.Type == lexer.TokenError {
			return nil, err
		}
		// Tolerate EOF or endobj inside a dictionary: close gracefully.
		if t.Type == lexer.TokenEOF {
			break
		}
		if t.Type == lexer.TokenKeyword && t.Value == "endobj" {
			break
		}
		if t.Type == lexer.TokenDelimiter && t.Value == ">>" {
			if _, err := p.Expect(lexer.TokenDelimiter); err != nil {
				return nil, err
			}
			break
		}

		keyObj, err := p.ParseObject()
		if err != nil {
			return nil, err
		}
		key, ok := keyObj.(ast.PdfName)
		if !ok {
			// Skip non-name keys gracefully rather than failing.
			continue
		}

		valObj, err := p.ParseObject()
		if err != nil {
			// Value missing: close the dict with what we have.
			break
		}
		dict[key] = valObj
	}

	// Check if stream follows immediately
	t, err := p.Peek(0)
	if err != nil {
		return nil, err
	}
	if t.Type == lexer.TokenKeyword && t.Value == "stream" {
		if _, err := p.Expect(lexer.TokenKeyword); err != nil {
			return nil, err
		}

		// Ensure we clear out the token buffer since we are reading directly from the reader next.
		// A stream keyword cannot be followed by lookahead tokens because we need to read raw bytes.
		// The peek(0) would have read "stream" into the token buffer, and Expect() removes it.
		// If there are other tokens in p.tokens, it means we read past the stream data, which is bad!
		if len(p.tokens) > 0 {
			return nil, fmt.Errorf("parser read ahead too far before stream data")
		}

		reader := p.lex.GetReader()
		b, err := reader.ReadByte()
		if err != nil {
			return nil, err
		}
		if b == '\r' {
			b2, err := reader.ReadByte()
			if err != nil {
				return nil, err
			}
			if b2 != '\n' {
				_ = reader.UnreadByte()
				// CR only after stream keyword — invalid per spec.
				if p.checkFormat {
					p.recordViolation("stream keyword followed by CR only (must be CR+LF or LF)")
				}
			}
		} else if b != '\n' {
			_ = reader.UnreadByte()
			if p.checkFormat {
				p.recordViolation("stream keyword not followed by EOL")
			}
		}

		// Record the start offset of the raw stream data.
		streamStart := reader.Position()

		// Fast path: if /Length is a direct integer, seek past the stream
		// data without reading it byte-by-byte. This is critical for large
		// streams (images, embedded files) where scanning would be wasteful.
		var dataLen int64
		usedLength := false
		if lengthObj, ok := dict["Length"]; ok {
			if lengthInt, ok := lengthObj.(ast.PdfInt); ok && int64(lengthInt) > 0 {
				dataLen = int64(lengthInt)
				// Seek past the stream data.
				if _, err := reader.Seek(streamStart+dataLen, io.SeekStart); err == nil {
					// Verify "endstream" follows. Tolerate optional \r\n before it.
					usedLength = true
					verify := make([]byte, len("endstream")+2) //nolint:mnd // endstream + optional \r\n
					n, _ := reader.Read(verify)                //nolint:errcheck // partial read is fine
					verifyStr := string(verify[:n])
					// Strip leading whitespace before endstream.
					for len(verifyStr) > 0 && (verifyStr[0] == '\r' || verifyStr[0] == '\n') {
						verifyStr = verifyStr[1:]
					}
					if !strings.HasPrefix(verifyStr, "endstream") {
						// /Length was wrong — fall back to byte scanning.
						usedLength = false
						if _, err := reader.Seek(streamStart, io.SeekStart); err != nil {
							return nil, fmt.Errorf("seeking back to stream start: %w", err)
						}
					}
				}
			}
		}

		// Record the declared /Length for compliance checking after the
		// actual length is determined via byte scanning.
		var declaredLength int64
		if lengthObj, ok := dict["Length"]; ok {
			if lengthInt, ok := lengthObj.(ast.PdfInt); ok {
				declaredLength = int64(lengthInt)
			}
		}

		// Slow path: scan byte-by-byte for "endstream" marker.
		if !usedLength {
			endMarker := []byte("endstream")
			matchLen := 0
			dataLen = 0
			// Check context every 64KB of scanned data to avoid hanging on
			// malformed streams without adding per-byte overhead.
			const ctxCheckInterval = 64 * 1024
			sinceLastCheck := 0
			for {
				c, err := reader.ReadByte()
				if err != nil {
					if err == io.EOF {
						// EOF mid-match: the partial match bytes are data, not the marker.
						dataLen += int64(matchLen)
						break
					}
					return nil, err
				}
				sinceLastCheck++
				if sinceLastCheck >= ctxCheckInterval {
					sinceLastCheck = 0
					if err := p.checkCtx(); err != nil {
						return nil, fmt.Errorf("scanning for endstream: %w", err)
					}
				}
				if c == endMarker[matchLen] {
					matchLen++
					if matchLen == len(endMarker) {
						break
					}
				} else {
					if matchLen > 0 {
						dataLen += int64(matchLen)
						matchLen = 0
						if c == endMarker[0] {
							matchLen = 1
						} else {
							dataLen++
						}
					} else {
						dataLen++
					}
				}
			}
		}

		// Compliance check: if the slow path ran, /Length was wrong.
		// Also check if /Length is missing entirely.
		if p.checkFormat {
			if !usedLength && declaredLength > 0 {
				// Slow path ran because /Length didn't match actual stream size.
				p.recordViolation(fmt.Sprintf(
					"stream /Length %d does not match actual byte count %d",
					declaredLength, dataLen))
			} else if declaredLength == 0 {
				if _, hasLength := dict["Length"]; !hasLength {
					p.recordViolation("stream missing /Length key")
				}
			}
		}

		// Calculate length after stripping trailing newline before endstream.
		trimLen := dataLen
		if trimLen > 0 {
			// Peek at the last 1-2 bytes to strip \r\n, \n, or \r.
			var tail [2]byte
			tailN := int64(2)
			if trimLen < 2 {
				tailN = trimLen
			}
			_, _ = reader.ReaderAt().ReadAt(tail[:tailN], streamStart+trimLen-tailN) //nolint:errcheck // on failure tail stays zeroed, no trim — harmless
			switch {
			case tailN == 2 && tail[1] == '\n':
				trimLen--
				if tail[0] == '\r' {
					trimLen--
				}
			case tailN == 2 && tail[1] == '\r':
				trimLen--
			case tailN == 1 && tail[0] == '\n':
				trimLen--
			case tailN == 1 && tail[0] == '\r':
				trimLen--
			}
		}

		// Capture values for the lazy loader closure.
		src := reader.ReaderAt()
		offset := streamStart
		length := trimLen
		decryptFn := p.decryptFn

		var once sync.Once
		var cached []byte
		var cachedErr error

		stream := ast.NewLazyStream(dict, func() ([]byte, error) {
			once.Do(func() {
				buf := make([]byte, length)
				_, cachedErr = src.ReadAt(buf, offset)
				if cachedErr != nil {
					return
				}
				if decryptFn != nil {
					if dec, err := decryptFn(buf); err == nil {
						buf = dec
					}
				}
				cached = buf
			})
			return cached, cachedErr
		})

		return stream, nil
	}

	return dict, nil
}

func (p *Parser) parseArray() (ast.PdfObject, error) {
	var arr ast.PdfArray
	for {
		if err := p.checkCtx(); err != nil {
			return nil, fmt.Errorf("parsing array: %w", err)
		}
		t, err := p.Peek(0)
		if err != nil && t.Type == lexer.TokenError {
			return nil, err
		}
		// Tolerate EOF or endobj inside an array: close gracefully.
		if t.Type == lexer.TokenEOF {
			break
		}
		if t.Type == lexer.TokenKeyword && t.Value == "endobj" {
			break
		}
		if t.Type == lexer.TokenDelimiter && t.Value == "]" {
			if _, err := p.Expect(lexer.TokenDelimiter); err != nil {
				return nil, err
			}
			break
		}

		obj, err := p.ParseObject()
		if err != nil {
			// Tolerate malformed array elements (e.g., operators inside
			// TJ arrays): skip the bad token and continue parsing.
			if _, consumeErr := p.Consume(); consumeErr != nil {
				break
			}
			continue
		}
		arr = append(arr, obj)
	}
	return arr, nil
}

func (p *Parser) parseString(val string) (ast.PdfString, error) {
	if len(val) >= 2 && val[0] == '(' && val[len(val)-1] == ')' {
		val = val[1 : len(val)-1]
	}

	var buf strings.Builder
	for i := 0; i < len(val); i++ {
		if val[i] == '\\' && i+1 < len(val) {
			i++
			switch val[i] {
			case 'n':
				buf.WriteByte('\n')
			case 'r':
				buf.WriteByte('\r')
			case 't':
				buf.WriteByte('\t')
			case 'b':
				buf.WriteByte('\b')
			case 'f':
				buf.WriteByte('\f')
			case '(', ')', '\\':
				buf.WriteByte(val[i])
			default:
				if val[i] >= '0' && val[i] <= '7' {
					octal := string(val[i])
					if i+1 < len(val) && val[i+1] >= '0' && val[i+1] <= '7' {
						octal += string(val[i+1])
						i++
						if i+1 < len(val) && val[i+1] >= '0' && val[i+1] <= '7' {
							octal += string(val[i+1])
							i++
						}
					}
					parsed, _ := strconv.ParseInt(octal, 8, 32)
					buf.WriteByte(byte(parsed & 0xFF)) //nolint:gosec
				} else {
					buf.WriteByte(val[i])
				}
			}
		} else {
			buf.WriteByte(val[i])
		}
	}

	b := []byte(buf.String())
	if p.decryptFn != nil {
		if dec, err := p.decryptFn(b); err == nil {
			b = dec
		}
	}
	return ast.PdfString(string(b)), nil
}

// SkipInlineImage reads past an inline image in a content stream.
// Called after the BI operator has been consumed. It reads the dictionary
// key/value pairs until the ID keyword, then scans forward through raw
// binary image data until the EI terminator is found.
func (p *Parser) SkipInlineImage() error {
	// Phase 1: Consume inline image dictionary entries until the ID keyword.
	// Entries are key/value pairs (e.g., /W 100 /H 50 /BPC 8 /CS /RGB).
	for {
		tok, err := p.Peek(0)
		if err != nil && tok.Type == lexer.TokenError {
			return fmt.Errorf("inline image dict: %w", err)
		}
		if tok.Type == lexer.TokenEOF {
			return fmt.Errorf("unexpected EOF in inline image dictionary")
		}
		// ID marks the end of the dictionary and start of raw image data.
		if tok.Type == lexer.TokenKeyword && tok.Value == "ID" {
			if _, err := p.Consume(); err != nil {
				return fmt.Errorf("failed to consume ID: %w", err)
			}
			break
		}
		// Consume the token (key or value). We don't need the data.
		if _, err := p.Consume(); err != nil {
			return fmt.Errorf("inline image dict entry: %w", err)
		}
	}

	// Ensure no lookahead tokens remain before switching to raw byte reading.
	if len(p.tokens) > 0 {
		return fmt.Errorf("parser read ahead too far before inline image data")
	}

	// Phase 2: Skip the single whitespace byte that follows ID (per PDF spec).
	reader := p.lex.GetReader()
	if _, err := reader.ReadByte(); err != nil {
		return fmt.Errorf("failed to read post-ID whitespace: %w", err)
	}

	// Phase 3: Scan forward for the EI terminator.
	// Per the PDF spec, image data ends when we find a whitespace byte
	// followed by "EI" followed by whitespace, a delimiter, or EOF.
	// Raw bytes 'E' and 'I' can occur inside image data, so we must
	// validate the trailing context to avoid false matches.
	return scanToEI(reader, p.ctx)
}

// ParseInlineImageDict reads an inline image's dictionary key/value pairs
// (between BI and ID), skips the binary image data (ID to EI), and returns
// the properties as a PdfDict. Called after the BI operator has been consumed.
// Dictionary keys are PDF names (e.g., /W, /H, /BPC, /CS) and values are
// parsed as normal PDF objects.
func (p *Parser) ParseInlineImageDict() (ast.PdfDict, error) {
	dict := ast.PdfDict{}

	// Phase 1: Parse key/value pairs until the ID keyword.
	for {
		tok, err := p.Peek(0)
		if err != nil && tok.Type == lexer.TokenError {
			return nil, fmt.Errorf("inline image dict: %w", err)
		}
		if tok.Type == lexer.TokenEOF {
			return nil, fmt.Errorf("unexpected EOF in inline image dictionary")
		}
		// ID marks the end of the dictionary and start of raw image data.
		if tok.Type == lexer.TokenKeyword && tok.Value == "ID" {
			if _, err := p.Consume(); err != nil {
				return nil, fmt.Errorf("failed to consume ID: %w", err)
			}
			break
		}

		// Expect a name token as the key.
		if tok.Type != lexer.TokenName {
			// Tolerate unexpected tokens by skipping them.
			if _, err := p.Consume(); err != nil {
				return nil, fmt.Errorf("inline image dict key: %w", err)
			}
			continue
		}

		keyName := ast.PdfName(strings.TrimPrefix(tok.Value, "/"))
		if _, err := p.Consume(); err != nil {
			return nil, fmt.Errorf("inline image dict key consume: %w", err)
		}

		// Peek at the next token; if it's ID or another name key, this was
		// a valueless entry (shouldn't happen per spec, but be defensive).
		nextTok, _ := p.Peek(0)
		if nextTok.Type == lexer.TokenKeyword && nextTok.Value == "ID" {
			break
		}
		if nextTok.Type == lexer.TokenEOF {
			return nil, fmt.Errorf("unexpected EOF after inline image key %q", keyName)
		}

		// Parse the value.
		val, err := p.ParseObject()
		if err != nil {
			// Tolerate malformed values by consuming the bad token.
			if _, consumeErr := p.Consume(); consumeErr != nil {
				return nil, fmt.Errorf("inline image dict value for %q: %w", keyName, err)
			}
			continue
		}
		dict[keyName] = val
	}

	// Ensure no lookahead tokens remain before switching to raw byte reading.
	if len(p.tokens) > 0 {
		return nil, fmt.Errorf("parser read ahead too far before inline image data")
	}

	// Phase 2: Skip the single whitespace byte that follows ID (per PDF spec).
	reader := p.lex.GetReader()
	if _, err := reader.ReadByte(); err != nil {
		return nil, fmt.Errorf("failed to read post-ID whitespace: %w", err)
	}

	// Phase 3: Scan forward for the EI terminator.
	if err := scanToEI(reader, p.ctx); err != nil {
		return nil, err
	}

	return dict, nil
}

// scanToEI scans forward through raw binary image data looking for the
// EI end-of-inline-image marker. Uses a state machine: the marker must be
// preceded by whitespace and followed by whitespace, a delimiter, or EOF.
// The optional ctx enables cancellation during long scans of malformed data.
func scanToEI(reader io.Reader, ctx context.Context) error {
	type readByteFunc interface {
		ReadByte() (byte, error)
		UnreadByte() error
	}
	r := reader.(readByteFunc)

	// Check context every 64KB to avoid hanging on malformed inline images.
	const ctxCheckInterval = 64 * 1024
	sinceLastCheck := 0

	// State machine: 0=scanning, 1=saw whitespace, 2=saw ws+E.
	state := 0
	for {
		c, err := r.ReadByte()
		if err != nil {
			if err == io.EOF {
				return fmt.Errorf("unexpected EOF scanning for EI in inline image")
			}
			return err
		}

		sinceLastCheck++
		if sinceLastCheck >= ctxCheckInterval {
			sinceLastCheck = 0
			if ctx != nil {
				if err := ctx.Err(); err != nil {
					return fmt.Errorf("scanning for EI: %w", err)
				}
			}
		}

		switch state {
		case 0:
			if isInlineWS(c) {
				state = 1
			}
		case 1:
			switch {
			case c == 'E':
				state = 2
			case isInlineWS(c):
				// Stay in state 1.
			default:
				state = 0
			}
		case 2:
			switch {
			case c == 'I':
				// Verify the byte after EI is whitespace, delimiter, or EOF.
				next, err := r.ReadByte()
				if err != nil {
					if err == io.EOF {
						return nil // EI at end of stream is valid.
					}
					return err
				}
				if isInlineWS(next) || isInlineDelim(next) {
					_ = r.UnreadByte()
					return nil
				}
				// False match — continue scanning.
				if isInlineWS(c) {
					state = 1
				} else {
					state = 0
				}
			case isInlineWS(c):
				state = 1
			default:
				state = 0
			}
		}
	}
}

func isInlineWS(c byte) bool {
	return c == 0x00 || c == 0x09 || c == 0x0A || c == 0x0C || c == 0x0D || c == 0x20
}

func isInlineDelim(c byte) bool {
	return c == '(' || c == ')' || c == '<' || c == '>' ||
		c == '[' || c == ']' || c == '{' || c == '}' ||
		c == '/' || c == '%'
}

func (p *Parser) parseHexString(val string) (ast.PdfString, error) {
	if len(val) >= 2 && val[0] == '<' && val[len(val)-1] == '>' {
		val = val[1 : len(val)-1]
	}

	// Check for invalid hex characters before stripping whitespace.
	if p.checkFormat {
		for _, r := range val {
			// PDF whitespace: null, tab, LF, FF, CR, space (ISO 32000-1 §7.2.2).
			isWS := r == 0x00 || r == '\t' || r == '\n' || r == 0x0C || r == '\r' || r == ' '
			isHex := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
			if !isWS && !isHex {
				p.recordViolation(fmt.Sprintf("hex string contains invalid character %q", r))
				break
			}
		}
	}

	val = strings.Map(func(r rune) rune {
		// PDF whitespace: null, tab, LF, FF, CR, space.
		if r == 0x00 || r == '\t' || r == '\n' || r == 0x0C || r == '\r' || r == ' ' {
			return -1
		}
		return r
	}, val)

	if len(val)%2 != 0 {
		if p.checkFormat {
			p.recordViolation("hex string has odd number of digits")
		}
		val += "0"
	}
	decoded, err := hex.DecodeString(val)
	if err != nil {
		return "", err
	}
	if p.decryptFn != nil {
		if dec, err := p.decryptFn(decoded); err == nil {
			decoded = dec
		}
	}
	return ast.PdfString(decoded), nil
}
