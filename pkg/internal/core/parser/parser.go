package parser

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"sync"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/ioat"
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
	tokens    []lexer.Token
	err       error
	decryptFn DecryptFunc

	// FormatViolations collects PDF format compliance issues (ISO 32000)
	// detected during parsing. These violations apply to both PDF/A and
	// PDF/UA validation. The parser remains lenient: violations are recorded
	// but do not fail the parse. This is nil unless compliance checking
	// is enabled via WithFormatChecking.
	FormatViolations []error
	checkFormat      bool

	// ends holds the end byte offset of each buffered token, parallel to
	// tokens. It is populated only when trackOffsets is set (via
	// WithOffsetTracking), so the default parse path pays no overhead. The
	// end offset is the reader position immediately after the lexer produced
	// the token, which is stable under lookahead buffering.
	ends         []int64
	trackOffsets bool
}

// NewParser creates a new Parser that reads PDF objects from the given lexer.
func NewParser(lex *lexer.Lexer) *Parser {
	return &Parser{lex: lex}
}

// WithDecryption configures the parser to apply fn to encrypted string and stream data.
func (p *Parser) WithDecryption(fn DecryptFunc) *Parser {
	p.decryptFn = fn
	return p
}

// WithFormatChecking enables PDF format violation collection during
// parsing. Violations are appended to FormatViolations without failing
// the parse. Used by both PDF/A and PDF/UA validation.
func (p *Parser) WithFormatChecking() *Parser {
	p.checkFormat = true
	return p
}

// WithOffsetTracking records the end byte offset of every token so callers
// can recover the byte range of a parsed construct. It is opt-in because it
// adds a position read and a slice entry per token; the default parser does
// not track offsets. Use [Parser.PeekEnd] to read the offsets.
func (p *Parser) WithOffsetTracking() *Parser {
	p.trackOffsets = true
	return p
}

// PeekEnd returns the end byte offset of the token n positions ahead, or -1
// if offset tracking is disabled or n is out of range. The offset is the
// reader position just past the token's last byte.
func (p *Parser) PeekEnd(n int) int64 {
	if !p.trackOffsets || n < 0 || n >= len(p.ends) {
		return -1
	}
	return p.ends[n]
}

// Position returns the underlying reader's current byte offset. Callers with
// offset tracking use it to bound constructs the token stream does not cover,
// notably inline image data consumed by [Parser.ParseInlineImageDict].
func (p *Parser) Position() int64 {
	return p.lex.Reader().Position()
}

func (p *Parser) recordViolation(msg string) {
	if p.checkFormat {
		p.FormatViolations = append(p.FormatViolations, fmt.Errorf("compliance: %s", msg))
	}
}

// Peek returns the token n positions ahead without consuming it.
func (p *Parser) Peek(n int) (lexer.Token, error) {
	for len(p.tokens) <= n {
		if p.err != nil {
			return lexer.Token{Type: lexer.TokenEOF}, p.err
		}
		tok, err := p.lex.NextToken()
		p.tokens = append(p.tokens, tok)
		if p.trackOffsets {
			p.ends = append(p.ends, p.lex.Reader().Position())
		}
		if err != nil && err != io.EOF {
			p.err = err
		}
	}
	return p.tokens[n], nil
}

// Consume returns the next token and advances the parser position.
func (p *Parser) Consume() (lexer.Token, error) {
	tok, err := p.Peek(0)
	if len(p.tokens) > 0 {
		p.tokens = p.tokens[1:]
		if p.trackOffsets && len(p.ends) > 0 {
			p.ends = p.ends[1:]
		}
	}
	return tok, err
}

// Expect consumes the next token and returns an error if its type does not match expectedType.
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

// ParseObject reads the next complete PDF object from the token stream.
// The context enables cancellation at natural checkpoints (per dict key,
// per array element, per stream scan iteration) without adding overhead
// to the per-token lexer loop.
func (p *Parser) ParseObject(ctx context.Context) (ast.PdfObject, error) {
	t0, err := p.Peek(0)
	if err != nil {
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
				return p.parseIndirectObject(ctx)
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
			return nil, fmt.Errorf("unexpected keyword %q: %w", t0.Value, errUnexpectedKeyword)
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
			// ISO 32000-1 §7.3.3 real-number syntax has no exponent. Go's
			// ParseFloat accepts forms like "1.5e2"; flag them under
			// format checking so a Conforming Processor reports the
			// divergence. The writer re-emits a plain decimal via
			// formatReal, so the round-trip is already compliant. Bare
			// exponent integers ("1e2") are left to the lexer's tokenizer
			// leniency and remain a parse error here, matching prior
			// behaviour, to avoid shifting text extraction.
			if p.checkFormat && strings.ContainsAny(t0.Value, "eE") {
				p.recordViolation("number uses scientific notation, which ISO 32000-1 §7.3.3 does not permit")
			}
			return ast.PdfReal(f), nil
		}
		i, err := strconv.ParseInt(t0.Value, 10, 64)
		if err != nil {
			// A magnitude past the 64-bit range is still a number. ISO 32000-1,
			// 7.3.3 caps integers at the implementation limit and permits a
			// value beyond it to be read as a real, which is what every
			// mainstream reader does. Failing here instead cost the enclosing
			// dictionary every one of its keys, and silently: a catalog holding
			// such a value came back empty, taking its /Pages with it.
			// Only a plain decimal that overflows: a bare exponent ("1e2") has
			// no ISO 32000-1, 7.3.3 syntax at all and stays a parse error.
			if isDecimalDigits(t0.Value) {
				if f, ferr := strconv.ParseFloat(t0.Value, 64); ferr == nil && !math.IsInf(f, 0) {
					return ast.PdfReal(f), nil
				}
			}
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
			return p.parseDict(ctx)
		case "[":
			return p.parseArray(ctx)
		default:
			return nil, fmt.Errorf("unexpected delimiter: %s", t0.Value)
		}
	}

	return nil, fmt.Errorf("unexpected token: %v", t0)
}

func (p *Parser) parseIndirectObject(ctx context.Context) (ast.PdfObject, error) {
	if _, err := p.Expect(lexer.TokenNumber); err != nil {
		return nil, err
	}
	if _, err := p.Expect(lexer.TokenNumber); err != nil {
		return nil, err
	}
	if _, err := p.Expect(lexer.TokenKeyword); err != nil {
		return nil, err
	}

	// An indirect object with no value (`N G obj endobj`, or truncated at EOF)
	// is the null object per ISO 32000-1 §7.3.9; qpdf and Acrobat tolerate it.
	// Peek before parsing so the endobj keyword is not mistaken for an
	// unexpected value token.
	if t, _ := p.Peek(0); t.Type == lexer.TokenEOF || (t.Type == lexer.TokenKeyword && t.Value == "endobj") {
		if t.Type == lexer.TokenKeyword {
			_, _ = p.Consume() //nolint:errcheck // endobj already validated by Peek
		}
		return ast.PdfNull{}, nil
	}

	obj, err := p.ParseObject(ctx)
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

// parseDict reads a <<...>> dictionary and, if the next token is the
// `stream` keyword, also the following binary stream body. Stream
// handling is delegated to [Parser.parseStreamAfterDict] so parseDict
// itself stays small.
func (p *Parser) parseDict(ctx context.Context) (ast.PdfObject, error) {
	dict, err := p.parseDictBody(ctx)
	if err != nil {
		return nil, err
	}

	t, err := p.Peek(0)
	if err != nil {
		return nil, err
	}
	if t.Type != lexer.TokenKeyword || t.Value != "stream" {
		return dict, nil
	}
	return p.parseStreamAfterDict(ctx, dict)
}

// parseDictBody reads the key/value pairs between `<<` and `>>` into an
// [ast.PdfDict]. It tolerates EOF and a premature `endobj` as implicit
// close markers so we still return a partial dict for malformed input
// (the top-level parser continues, lenient by design).
func (p *Parser) parseDictBody(ctx context.Context) (ast.PdfDict, error) {
	dict := make(ast.PdfDict)
	for {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("parsing dict: %w", err)
		}
		t, err := p.Peek(0)
		if err != nil {
			return nil, err
		}
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

		keyObj, err := p.ParseObject(ctx)
		if err != nil {
			// A stray keyword where a key is expected (e.g. `/Name MS`, where a
			// space inside a name splits it into a name plus the bare keyword
			// `MS`) has already been consumed. Skip it and keep the dict rather
			// than discarding the whole object; qpdf and veraPDF recover the
			// same way. Other errors (EOF, lexer failures) are not recoverable.
			if errors.Is(err, errUnexpectedKeyword) {
				continue
			}
			return nil, err
		}
		key, ok := keyObj.(ast.PdfName)
		if !ok {
			// Skip non-name keys gracefully rather than failing.
			continue
		}

		valObj, err := p.ParseObject(ctx)
		if err != nil {
			// Value missing: close the dict with what we have.
			break
		}
		dict[key] = valObj
	}
	return dict, nil
}

// parseStreamAfterDict consumes the `stream` keyword, the stream body
// (via fast or slow path), and returns a lazy [ast.PdfStream]. The caller
// has already confirmed the next token is `stream`.
func (p *Parser) parseStreamAfterDict(ctx context.Context, dict ast.PdfDict) (ast.PdfObject, error) {
	if _, err := p.Expect(lexer.TokenKeyword); err != nil {
		return nil, err
	}

	// The parser must not have any lookahead tokens at this point: the
	// next bytes belong to the raw stream body, not the token stream.
	// Peek(0) of "stream" consumed its token via Expect() above; any
	// additional tokens in the buffer mean we read past the stream start.
	if len(p.tokens) > 0 {
		return nil, fmt.Errorf("parser read ahead too far before stream data")
	}

	reader := p.lex.Reader()
	if err := p.consumeStreamEOL(reader); err != nil {
		return nil, err
	}

	streamStart := reader.Position()
	dataLen, usedLength, err := p.readStreamBody(ctx, reader, dict, streamStart)
	if err != nil {
		return nil, err
	}
	p.recordStreamLengthViolations(dict, dataLen, usedLength)

	// Trimming a trailing EOL handles producers that fold the newline before
	// "endstream" into /Length. It must never run on a stream whose /Length is
	// the exact binary byte count and whose bytes are non-textual:
	//   - Encrypted streams: /Length counts IV+ciphertext, the last byte is
	//     random ciphertext; dropping a CR/LF makes it non-block-aligned so AES
	//     decryption bails and still-encrypted bytes reach the filter
	//     (~1/256-per-stream IV-dependent content loss).
	//   - Cross-reference streams (ISO 32000-1 §7.5.8): /Length is the exact
	//     entry-table size and the EOL before "endstream" is a separator outside
	//     the data (§7.3.8.1). A real trailing 0x0A/0x0D field byte trimmed here
	//     shortens the table, fails the parse, and forces a raw-scan rebuild that
	//     cannot recover compressed objects (the whole page tree can be lost).
	trimLen := dataLen
	if (p.decryptFn == nil || streamUsesIdentityCryptFilter(dict)) && !isXRefStream(dict) {
		trimLen = trimStreamTail(reader.ReaderAt(), streamStart, dataLen)
	}
	return p.newLazyStream(dict, reader.ReaderAt(), streamStart, trimLen), nil
}

// isXRefStream reports whether dict is a cross-reference stream (/Type /XRef).
func isXRefStream(dict ast.PdfDict) bool {
	name, ok := dict["Type"].(ast.PdfName)
	return ok && name == "XRef"
}

// consumeStreamEOL consumes the end-of-line marker that must follow the
// `stream` keyword per PDF 32000 §7.3.8.1 (either CRLF or LF). A bare CR
// or missing EOL is tolerated but recorded as a format violation.
func (p *Parser) consumeStreamEOL(reader *ioat.Reader) error {
	b, err := reader.ReadByte()
	if err != nil {
		return err
	}
	switch b {
	case '\r':
		b2, err := reader.ReadByte()
		if err != nil {
			return err
		}
		if b2 != '\n' {
			_ = reader.UnreadByte() // Guaranteed to succeed: previous ReadByte succeeded on a buffered reader.
			if p.checkFormat {
				p.recordViolation("stream keyword followed by CR only (must be CR+LF or LF)")
			}
		}
	case '\n':
		// canonical case
	default:
		_ = reader.UnreadByte() // Guaranteed to succeed: previous ReadByte succeeded on a buffered reader.
		if p.checkFormat {
			p.recordViolation("stream keyword not followed by EOL")
		}
	}
	return nil
}

// readStreamBody measures the stream body and positions the reader after
// it. It first tries a fast path using /Length; if /Length is missing or
// doesn't point at `endstream`, it falls back to a byte scan.
//
// The returned usedLength is true if the fast path was taken.
func (p *Parser) readStreamBody(ctx context.Context, reader *ioat.Reader, dict ast.PdfDict, streamStart int64) (int64, bool, error) {
	if dataLen, ok := tryFastStreamLength(reader, dict, streamStart); ok {
		return dataLen, true, nil
	}
	// Fast path failed (missing /Length, indirect /Length, or /Length
	// disagreed with the actual stream). Rewind to the start and scan
	// byte-by-byte for the endstream marker.
	if _, err := reader.Seek(streamStart, io.SeekStart); err != nil {
		return 0, false, fmt.Errorf("seeking back to stream start: %w", err)
	}
	dataLen, err := p.scanEndstream(ctx, reader)
	return dataLen, false, err
}

// tryFastStreamLength attempts to use a direct /Length integer to seek
// past the stream body without scanning. Returns the byte count and true
// on success; zero and false on any mismatch. This is the hot path for
// well-formed documents with large streams (images, embedded files).
func tryFastStreamLength(reader *ioat.Reader, dict ast.PdfDict, streamStart int64) (int64, bool) {
	lengthObj, ok := dict["Length"]
	if !ok {
		return 0, false
	}
	lengthInt, ok := lengthObj.(ast.PdfInt)
	if !ok || int64(lengthInt) <= 0 {
		return 0, false
	}
	dataLen := int64(lengthInt)

	if _, err := reader.Seek(streamStart+dataLen, io.SeekStart); err != nil {
		return 0, false
	}
	// Verify "endstream" actually follows. Tolerate an optional leading
	// \r\n or lone \n.
	verify := make([]byte, len("endstream")+2) //nolint:mnd // endstream + optional \r\n
	n, _ := reader.Read(verify)                //nolint:errcheck // partial read is fine; we only care about the prefix
	verifyStr := string(verify[:n])
	for len(verifyStr) > 0 && (verifyStr[0] == '\r' || verifyStr[0] == '\n') {
		verifyStr = verifyStr[1:]
	}
	// A damaged end-of-stream keyword still marks the end. The check exists to
	// catch a WRONG /Length, and a marker sitting at exactly the declared
	// offset corroborates the length whether or not the keyword survived
	// intact. pdfjs issue10004.pdf writes every one of its 29 stream ends as a
	// truncated "endstrea" followed by "endobj": rejecting those discarded 29
	// correct /Length values, and the fallback scan then found no "endstream"
	// anywhere and returned the rest of the file as stream data, so each stream
	// swallowed ~750 KB of unrelated bytes and the file grew 757 KB -> 12.6 MB.
	if !strings.HasPrefix(verifyStr, "endstr") && !strings.HasPrefix(verifyStr, "endobj") {
		return 0, false
	}
	return dataLen, true
}

// scanEndstream reads byte-by-byte from the current position until it
// finds the `endstream` keyword, returning the number of stream-body
// bytes consumed before the marker. This is the fallback used when the
// /Length hint is missing or wrong.
func (p *Parser) scanEndstream(ctx context.Context, reader *ioat.Reader) (int64, error) {
	endMarker := []byte("endstream")
	matchLen := 0
	var dataLen int64

	// Check context every 64KB of scanned data: a per-byte check would
	// dominate this hot loop, but letting a malformed stream spin without
	// any bound is worse.
	const ctxCheckInterval = 64 * 1024
	sinceLastCheck := 0

	for {
		c, err := reader.ReadByte()
		if err != nil {
			if err == io.EOF {
				// EOF mid-match: the bytes that matched so far are actually
				// stream data, not part of the marker.
				dataLen += int64(matchLen)
				return dataLen, nil
			}
			return 0, err
		}
		sinceLastCheck++
		if sinceLastCheck >= ctxCheckInterval {
			sinceLastCheck = 0
			if err := ctx.Err(); err != nil {
				return 0, fmt.Errorf("scanning for endstream: %w", err)
			}
		}
		if c == endMarker[matchLen] {
			matchLen++
			if matchLen == len(endMarker) {
				return dataLen, nil
			}
			continue
		}
		// Match broke. Anything we matched so far is stream data.
		if matchLen > 0 {
			dataLen += int64(matchLen)
			matchLen = 0
			if c == endMarker[0] {
				matchLen = 1
				continue
			}
		}
		dataLen++
	}
}

// recordStreamLengthViolations emits format-checking violations when the
// declared /Length doesn't match the actual stream length. Called after
// [Parser.readStreamBody] has determined the truth.
func (p *Parser) recordStreamLengthViolations(dict ast.PdfDict, dataLen int64, usedLength bool) {
	if !p.checkFormat {
		return
	}
	var declaredLength int64
	if lengthObj, ok := dict["Length"]; ok {
		if lengthInt, ok := lengthObj.(ast.PdfInt); ok {
			declaredLength = int64(lengthInt)
		}
	}
	if !usedLength && declaredLength > 0 {
		p.recordViolation(fmt.Sprintf(
			"stream /Length %d does not match actual byte count %d",
			declaredLength, dataLen))
		return
	}
	if declaredLength == 0 {
		if _, hasLength := dict["Length"]; !hasLength {
			p.recordViolation("stream missing /Length key")
		}
	}
}

// trimStreamTail returns the stream body length after stripping the
// optional EOL marker that PDF 32000 §7.3.8.1 allows before `endstream`.
// Callers use the trimmed length when materializing the stream so the
// lazy loader never surfaces the spec-optional newline bytes.
func trimStreamTail(src io.ReaderAt, streamStart, dataLen int64) int64 {
	if dataLen <= 0 {
		return dataLen
	}
	var tail [2]byte
	tailN := int64(2)
	if dataLen < 2 {
		tailN = dataLen
	}
	_, _ = src.ReadAt(tail[:tailN], streamStart+dataLen-tailN) //nolint:errcheck // on read failure tail stays zeroed, we simply don't trim
	switch {
	case tailN == 2 && tail[1] == '\n':
		dataLen--
		if tail[0] == '\r' {
			dataLen--
		}
	case tailN == 2 && tail[1] == '\r':
		dataLen--
	case tailN == 1 && tail[0] == '\n':
		dataLen--
	case tailN == 1 && tail[0] == '\r':
		dataLen--
	}
	return dataLen
}

// newLazyStream builds an [ast.PdfStream] whose bytes are read (and
// optionally decrypted) on first access. This keeps the parser's hot
// path cheap: large image and font streams are not materialized until
// the caller explicitly asks for them.
func (p *Parser) newLazyStream(dict ast.PdfDict, src io.ReaderAt, offset, length int64) ast.PdfStream {
	decryptFn := p.decryptFn
	// A stream whose /Filter names the Crypt filter with an Identity crypt-filter
	// name is not encrypted (ISO 32000-1 §7.6.5): the Identity crypt filter
	// overrides the document's default stream crypt filter and passes the data
	// through unchanged. Applying the document decryption to it would corrupt
	// already-plaintext bytes, the pattern used to keep /Metadata readable in an
	// encrypted file (/EncryptMetadata false).
	if streamUsesIdentityCryptFilter(dict) {
		decryptFn = nil
	}
	var once sync.Once
	var cached []byte
	var cachedErr error
	return ast.NewLazyStream(dict, func(ctx context.Context) ([]byte, error) {
		once.Do(func() {
			if err := ctx.Err(); err != nil {
				cachedErr = err
				return
			}
			buf := make([]byte, length)
			_, cachedErr = src.ReadAt(buf, offset)
			if cachedErr != nil {
				return
			}
			if decryptFn != nil {
				dec, err := decryptFn(buf)
				if err != nil {
					cachedErr = fmt.Errorf("decrypting stream data: %w", err)
					return
				}
				buf = dec
			}
			cached = buf
		})
		return cached, cachedErr
	})
}

// streamUsesIdentityCryptFilter reports whether the stream dictionary's /Filter
// names the Crypt filter with an Identity crypt-filter /Name in the matching
// /DecodeParms (ISO 32000-1 §7.6.5). Such a stream carries plaintext and must
// not be decrypted with the document's default crypt filter. /Filter and
// /DecodeParms are read as direct objects, which is how producers emit them on
// the metadata streams that use this construct.
func streamUsesIdentityCryptFilter(dict ast.PdfDict) bool {
	filterObj, ok := dict["Filter"]
	if !ok {
		return false
	}
	parmsObj := dict["DecodeParms"]
	if parmsObj == nil {
		parmsObj = dict["DP"] // abbreviated key
	}
	switch f := filterObj.(type) {
	case ast.PdfName:
		return f == "Crypt" && cryptParmsIsIdentity(parmsObj)
	case ast.PdfArray:
		for i, e := range f {
			if name, ok := e.(ast.PdfName); !ok || name != "Crypt" {
				continue
			}
			var p ast.PdfObject
			if arr, ok := parmsObj.(ast.PdfArray); ok && i < len(arr) {
				p = arr[i]
			}
			if cryptParmsIsIdentity(p) {
				return true
			}
		}
	}
	return false
}

// cryptParmsIsIdentity reports whether a Crypt filter's decode-params select the
// Identity crypt filter. The /Name key defaults to Identity when the params or
// the /Name are absent (ISO 32000-1 Table 14).
func cryptParmsIsIdentity(parms ast.PdfObject) bool {
	d, ok := parms.(ast.PdfDict)
	if !ok {
		return true
	}
	name, ok := d["Name"].(ast.PdfName)
	if !ok {
		return true
	}
	return name == "Identity"
}

func (p *Parser) parseArray(ctx context.Context) (ast.PdfObject, error) {
	var arr ast.PdfArray
	for {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("parsing array: %w", err)
		}
		t, err := p.Peek(0)
		if err != nil {
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

		obj, err := p.ParseObject(ctx)
		if err != nil {
			// Tolerate malformed array elements (e.g., operators inside
			// TJ arrays): skip the bad token and continue parsing.
			//
			// ParseObject consumes a token before dispatching on it, so a bare
			// keyword is already past by the time it reports one. Consuming
			// again would swallow the element AFTER the bad token: pdfjs
			// bug1953099 writes a lone "-" between two strings in a TJ array,
			// and the (e) that followed it vanished, so the word "extensively"
			// came out a letter short.
			if errors.Is(err, errUnexpectedKeyword) {
				continue
			}
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
		switch {
		case val[i] == '\\' && i+1 < len(val):
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
			case '\r':
				// Per ISO 32000-2 §7.3.4.2, a reverse solidus followed by an
				// end-of-line marker (CR, LF, or CR LF) is ignored: the
				// sequence continues the string on the next source line
				// without emitting a character.
				if i+1 < len(val) && val[i+1] == '\n' {
					i++
				}
			case '\n':
				// Same line-continuation rule for a lone LF after backslash.
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
		case val[i] == '\r':
			// Literal end-of-line markers (CR, LF, CR LF) inside a string
			// are normalised to a single LF per ISO 32000-2 §7.3.4.2.
			buf.WriteByte('\n')
			if i+1 < len(val) && val[i+1] == '\n' {
				i++
			}
		default:
			buf.WriteByte(val[i])
		}
	}

	b := []byte(buf.String())
	if p.decryptFn != nil {
		dec, err := p.decryptFn(b)
		if err != nil {
			return "", fmt.Errorf("decrypting literal string: %w", err)
		}
		b = dec
	}
	return ast.PdfString(string(b)), nil
}

// SkipInlineImage reads past an inline image in a content stream.
// Called after the BI operator has been consumed. It reads the dictionary
// key/value pairs until the ID keyword, then scans forward through raw
// binary image data until the EI terminator is found.
func (p *Parser) SkipInlineImage(ctx context.Context) error {
	ascii85 := false
	// Phase 1: Consume inline image dictionary entries until the ID keyword.
	// Entries are key/value pairs (e.g., /W 100 /H 50 /BPC 8 /CS /RGB).
	// Track the /F filter to know whether the image data is ASCII85-encoded,
	// which changes how the EI terminator scan must handle PDF delimiters.
	for {
		tok, err := p.Peek(0)
		if err != nil {
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
		if tok.Type == lexer.TokenName && (tok.Value == "/F" || tok.Value == "/Filter") {
			if _, err := p.Consume(); err != nil {
				return fmt.Errorf("inline image filter key: %w", err)
			}
			val, err := p.ParseObject(ctx)
			if err != nil {
				continue
			}
			if filterNamesContainASCII85(val) {
				ascii85 = true
			}
			continue
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
	reader := p.lex.Reader()
	if _, err := reader.ReadByte(); err != nil {
		return fmt.Errorf("failed to read post-ID whitespace: %w", err)
	}

	// Phase 3: Scan forward for the EI terminator.
	// Per the PDF spec, image data ends when we find a whitespace byte
	// followed by "EI" followed by whitespace, a delimiter, or EOF.
	// Raw bytes 'E' and 'I' can occur inside image data, so we must
	// validate the trailing context to avoid false matches.
	return scanToEI(reader, ctx, ascii85)
}

// filterNamesContainASCII85 reports whether a /F (or /Filter) value
// includes the ASCII85Decode filter, testing both the short abbreviation
// ("A85") used by inline images and the long name.
func filterNamesContainASCII85(obj ast.PdfObject) bool {
	switch v := obj.(type) {
	case ast.PdfName:
		return v == "A85" || v == "ASCII85Decode"
	case ast.PdfArray:
		for _, elem := range v {
			if name, ok := elem.(ast.PdfName); ok {
				if name == "A85" || name == "ASCII85Decode" {
					return true
				}
			}
		}
	}
	return false
}

// ParseInlineImageDict reads an inline image's dictionary key/value pairs
// (between BI and ID), skips the binary image data (ID to EI), and returns
// the properties as a PdfDict. Called after the BI operator has been consumed.
// Dictionary keys are PDF names (e.g., /W, /H, /BPC, /CS) and values are
// parsed as normal PDF objects.
func (p *Parser) ParseInlineImageDict(ctx context.Context) (ast.PdfDict, error) {
	dict := ast.PdfDict{}

	// Phase 1: Parse key/value pairs until the ID keyword.
	for {
		tok, err := p.Peek(0)
		if err != nil {
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
		val, err := p.ParseObject(ctx)
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
	reader := p.lex.Reader()
	if _, err := reader.ReadByte(); err != nil {
		return nil, fmt.Errorf("failed to read post-ID whitespace: %w", err)
	}

	// Phase 3: Scan forward for the EI terminator.
	ascii85 := false
	if f, ok := dict["F"]; ok {
		ascii85 = filterNamesContainASCII85(f)
	}
	if !ascii85 {
		if f, ok := dict["Filter"]; ok {
			ascii85 = filterNamesContainASCII85(f)
		}
	}
	if err := scanToEI(reader, ctx, ascii85); err != nil {
		return nil, err
	}

	return dict, nil
}

// scanToEI scans forward through raw binary image data looking for the
// EI end-of-inline-image marker. Uses a state machine: the marker must be
// preceded by whitespace and followed by whitespace, a delimiter, or EOF.
// When ascii85 is true, EI is accepted only if followed by whitespace
// (or EOF). That stricter post-check prevents false matches on the bytes
// 'E' and 'I' inside ASCII85-encoded image data, whose 33-117 alphabet
// includes every PDF delimiter character. Non-ASCII85 image data can
// contain arbitrary binary bytes, so the looser ws-or-delim post-check
// remains necessary to match producers that pack the next operator
// immediately after EI. The optional ctx enables cancellation during
// long scans of malformed data.
func scanToEI(reader io.Reader, ctx context.Context, ascii85 bool) error {
	type readByteFunc interface {
		ReadByte() (byte, error)
		UnreadByte() error
		Position() int64
		ReaderAt() io.ReaderAt
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
				eiEnd := r.Position() // offset just past "EI", before reading the separator
				next, err := r.ReadByte()
				if err != nil {
					if err == io.EOF {
						return nil // EI at end of stream is valid.
					}
					return err
				}
				accepted := isInlineWS(next) || (!ascii85 && isInlineDelim(next))
				// A NUL byte after "EI" is ambiguous: some producers terminate
				// a real inline image with EI followed by NUL padding, but NUL
				// also occurs inside binary image data (notably FlateDecoded
				// streams), where the "EI" is coincidental. Disambiguate by the
				// bytes that follow: content-stream text after a real EI,
				// versus more binary after a false one. Matches the lookahead
				// in PDF.js findDefaultInlineStreamEnd. Accepting a false EI
				// stopped the scan early and lexed trailing image bytes as
				// content operators (spurious ISO 19005 §6.2.2 undefined
				// operators).
				if accepted && next == 0x00 && !eiFollowedByContent(r.ReaderAt(), eiEnd) {
					accepted = false
				}
				if accepted {
					_ = r.UnreadByte() // Guaranteed to succeed: previous ReadByte succeeded on a buffered reader.
					return nil
				}
				// False match, continue scanning.
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

// eiFollowedByContent reports whether the bytes at offset at (immediately
// after a candidate EI marker) look like content-stream text rather than
// binary image data. A real EI is followed by the next operator or its
// operands, which are whitespace and printable ASCII; a coincidental "EI"
// inside binary image data is followed by more binary. Only whitespace and
// printable ASCII count as content, with a lone NUL tolerated (some producers
// pad with a single NUL); a NUL run or any byte outside 0x20-0x7E signals
// binary. Called only for the ambiguous EI+NUL case, so it never alters the
// common EI+space/newline path. An empty read (EI at EOF) is a valid
// terminator.
func eiFollowedByContent(ra io.ReaderAt, at int64) bool {
	const window = 8 // small: enough to spot binary, short of any following string operand
	var buf [window]byte
	n, _ := ra.ReadAt(buf[:], at)
	b := buf[:n]
	for i := range b {
		c := b[i]
		switch {
		case c == 0x00:
			if i+1 < len(b) && b[i+1] == 0x00 {
				return false // consecutive NULs: binary image data
			}
		case c == 0x09 || c == 0x0A || c == 0x0C || c == 0x0D || c == 0x20:
		case c >= 0x21 && c <= 0x7E:
		default:
			return false // a byte outside printable ASCII: binary image data
		}
	}
	return true
}

func isInlineDelim(c byte) bool {
	return c == '(' || c == ')' || c == '<' || c == '>' ||
		c == '[' || c == ']' || c == '{' || c == '}' ||
		c == '/' || c == '%'
}

func isInlineWS(c byte) bool {
	return c == 0x00 || c == 0x09 || c == 0x0A || c == 0x0C || c == 0x0D || c == 0x20
}

func (p *Parser) parseHexString(val string) (ast.PdfString, error) {
	if len(val) >= 2 && val[0] == '<' && val[len(val)-1] == '>' {
		val = val[1 : len(val)-1]
	}

	// Record any invalid hex character before it is dropped below.
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

	// ISO 32000-1 §7.3.4.3 permits only hex digits and whitespace in a hex
	// string, but veraPDF and Acrobat tolerate stray non-hex bytes by
	// ignoring them. Dropping every non-hex character (rather than failing
	// the decode) preserves the enclosing object or content operator; a
	// failed decode would silently discard the value and, in a content
	// stream, the whole operator. The stray character is still recorded as
	// a format violation above under checkFormat.
	val = strings.Map(func(r rune) rune {
		isHex := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
		if !isHex {
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
		dec, err := p.decryptFn(decoded)
		if err != nil {
			return "", fmt.Errorf("decrypting hex string: %w", err)
		}
		decoded = dec
	}
	return ast.PdfString(decoded), nil
}

// isDecimalDigits reports whether s is an optionally signed run of decimal
// digits, the shape of an integer literal that can only fail to parse by
// overflowing.
func isDecimalDigits(s string) bool {
	if s == "" {
		return false
	}
	if s[0] == '+' || s[0] == '-' {
		s = s[1:]
	}
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
