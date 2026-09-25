package parser

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
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
//
// # Corpus evidence
//
//   - govdocs: parsing one page of 1.5 million operators spent 1.58 GB in
//     the tokens = tokens[1:] reslice that the head index replaced.
type Parser struct {
	lex    *lexer.Lexer
	tokens []lexer.Token

	// head is the index in tokens of the next unconsumed token.
	//
	// Consuming used to reslice with tokens = tokens[1:], which walks the
	// slice header forward through the backing array and never comes back:
	// capacity fell by one per token, so append allocated a fresh array every
	// few tokens and the buffer was never reused. Advancing an index instead
	// lets the drained buffer rewind to the start of the array it already has.
	head int

	err       error
	decryptFn DecryptFunc

	// FormatViolations collects PDF format compliance issues (ISO 32000)
	// detected during parsing. These violations apply to both PDF/A and
	// PDF/UA validation. The parser remains lenient: violations are recorded
	// but do not fail the parse. This is nil unless compliance checking
	// is enabled via WithFormatChecking.
	FormatViolations []error
	checkFormat      bool

	// entryPath holds, under format checking, the dictionary entries whose
	// values are being read, outermost first, so that a violation in a string
	// read inside one names them (see [EntryViolationError]).
	entryPath []EntryStep

	// IntegerOverflows holds, in parse order, the value of every integer
	// literal too large for a 64-bit integer, which the parser reads as a
	// real. ISO 32000-1, 7.3.3 writes an integer as decimal digits with an
	// optional sign, so the literal is an integer object whatever its
	// magnitude, and a PDF/A validator judges it against the integer limit
	// (ISO 19005-2, 6.1.13 t1), not the real one. It is collected on every
	// parse, not only under format checking: such literals are rare, and an
	// object cached before a validation starts is never parsed again.
	IntegerOverflows []float64

	// ends holds the end byte offset of each buffered token, parallel to
	// tokens. It is populated only when trackOffsets is set (via
	// WithOffsetTracking), so the default parse path pays no overhead. The
	// end offset is the reader position immediately after the lexer produced
	// the token, which is stable under lookahead buffering.
	ends         []int64
	trackOffsets bool

	// depth is how many nested objects [Parser.ParseObject] is currently
	// inside. Every recursive path reaches it again, so counting there covers
	// dictionaries, arrays and object header chains with one counter: a
	// dictionary value and an array element are both parsed by ParseObject,
	// and `N G obj` reaches it through parseIndirectObject. An inline image's
	// dictionary, which ParseObject does not parse, is counted by
	// [Parser.ParseInlineImageDict] itself.
	depth int

	// consumed counts the tokens [Parser.Consume] has advanced past. See
	// [Parser.Consumed] and [Parser.SkipFailedObject].
	consumed int

	// objectEnd is the end [Parser.WithObjectEnd] set, or 0 when the data
	// runs to the end of the file. The data of a stream is exempt from it,
	// but the scan for the end of a stream whose /Length cannot be used is
	// not (see [Parser.readStreamBody]).
	objectEnd int64

	// veraPDFContainers notes the containers veraPDF cannot read, and
	// abandoned reports that one was read. See
	// [Parser.WithVeraPDFContainers].
	veraPDFContainers bool
	abandoned         bool
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

// WithObjectEnd ends the data the parser reads at end, the byte offset where
// the next indirect object starts. ISO 32000-1, 7.5.3 makes the body of a file
// a sequence of indirect objects and 7.3.10 brackets each value between obj and
// endobj, so no object runs on into the next one.
//
// Deliberate leniency: a literal string (ISO 32000-1, 7.3.4.2) or a dictionary
// (7.3.7) still open at end has no meaning under ISO 32000-1, and litho reads
// it as damage that ends there rather than failing the object. The unterminated
// string is dropped and the dictionary keeps the pairs before it, as it does
// when the file itself ends. Damaged files leave such a construct open: binary
// data written into a dictionary opens a string whose closing parenthesis, if
// one ever balances it, lies many objects further on, and reading on to it
// parses whatever those objects hold as values of this one. veraPDF refuses the
// dictionary outright at its first key that is not a name ("invalid pdf
// dictionary", COSParser.getDictionary), and PDF.js reads the string on to its
// closing parenthesis wherever that falls (Lexer.getString).
//
// The bound comes from the cross-reference table, never from the bytes: a
// literal string may hold "1 0 obj" or "endobj" (7.3.4.2 allows any text with
// balanced parentheses), so searching the data for either would cut a valid
// string short. A stream's data is exempt, see [Parser.parseStreamAfterDict].
//
// # Corpus evidence
//
//   - pdfjs issue11678.pdf: names Microsoft Word for Office 365 as its
//     producer, and object 21's dictionary runs into binary image data, where
//     a literal string opens 1.6 KB before object 22 and closes 184 KB later.
//     Parsing on past it read the bytes of 13 other objects as object 21's
//     values and reported 97 findings of ISO 19005-2, 6.1.6 t1 and t2 against
//     the hex strings found there, which veraPDF does not report. Its PDF/A-2b
//     run now reports 38 violations instead of 135.
//   - All corpora: bounding every uncompressed object at the next one changes
//     no other file's parse, page count, text or violations under PDF/A-2b,
//     veraPDF files under their own profile.
func (p *Parser) WithObjectEnd(end int64) *Parser {
	p.objectEnd = end
	p.lex.Reader().SetLimit(end)
	return p
}

// PeekEnd returns the end byte offset of the token n positions ahead, or -1
// if offset tracking is disabled or n is out of range. The offset is the
// reader position just past the token's last byte.
func (p *Parser) PeekEnd(n int) int64 {
	if !p.trackOffsets || n < 0 || n >= p.buffered() {
		return -1
	}
	return p.ends[p.head+n]
}

// buffered reports how many read-ahead tokens are held but not yet consumed.
func (p *Parser) buffered() int {
	return len(p.tokens) - p.head
}

// Position returns the underlying reader's current byte offset. Callers with
// offset tracking use it to bound constructs the token stream does not cover,
// notably inline image data consumed by [Parser.ParseInlineImageDict].
func (p *Parser) Position() int64 {
	return p.lex.Reader().Position()
}

// recordViolation records the format violation "compliance: msg" under the
// sentinel of its class, which a consumer matches with [errors.Is].
func (p *Parser) recordViolation(class error, msg string) {
	if p.checkFormat {
		p.FormatViolations = append(p.FormatViolations, NewFormatViolation(class, "compliance: "+msg))
	}
}

// recordNumberExponent records a number written with an exponent under format
// checking, as [ErrNumberExponent]. Only a lexer that keeps the token
// boundaries of ISO 32000-1, 7.2.2 hands the parser such a number: one that
// reads a content stream as veraPDF does ([lexer.Lexer.WithVeraPDFTokens])
// ends the number at the exponent letter, and the letter is an operator.
func (p *Parser) recordNumberExponent() {
	p.recordViolation(ErrNumberExponent, "number uses scientific notation, which ISO 32000-1 §7.3.3 does not permit")
}

// formatViolationError is a format violation whose message is not built from
// the sentinel of its class, so it cannot be a wrapped fmt.Errorf.
type formatViolationError struct {
	msg   string
	class error
}

func (e *formatViolationError) Error() string { return e.msg }

func (e *formatViolationError) Unwrap() error { return e.class }

// NewFormatViolation returns a format violation that reads msg and matches
// class under [errors.Is]. The message is what callers show, and the class is
// how they tell violations apart, so rewording a message cannot move a
// violation to another rule. Code outside the parser that judges raw bytes,
// such as the document's object header and xref table checks, builds its
// violations with it too.
func NewFormatViolation(class error, msg string) error {
	return &formatViolationError{msg: msg, class: class}
}

// recordViolationErr is [Parser.recordViolation] for a violation whose message
// is built from its sentinel: it records err, the sentinel or an error
// wrapping it, as "compliance: " followed by err's message.
func (p *Parser) recordViolationErr(err error) {
	if p.checkFormat {
		p.FormatViolations = append(p.FormatViolations, fmt.Errorf("compliance: %w", err))
	}
}

// EntryViolationError is a format violation in a string read inside the value
// of a dictionary entry. It reads as the violation it wraps and matches it
// under [errors.Is].
//
// veraPDF reaches some entries of a dictionary through a second model object
// as well as through the dictionary, the /ActualText, /Alt and /Lang of a
// marked-content property list among them, and reaches a property list a
// content stream names from its resources once more for every operator naming
// it, so a consumer counting its findings needs to know which entries hold
// the string.
type EntryViolationError struct {
	// Key is the key of the innermost entry holding the string.
	Key ast.PdfName
	// Depth is the nesting depth the string was read at: 1 for an object
	// read on its own, such as a content-stream operand, and one more per
	// array or dictionary enclosing it. The string is the value of Key itself
	// when Depth is the depth of the last step of Path, and lies in an array
	// within that value otherwise.
	Depth int
	// Path holds the entries enclosing the string, outermost first, each
	// with the depth its value is read at. Two consecutive steps whose depths
	// differ by one are a dictionary written as the value of the first.
	Path []EntryStep

	err error
}

// EntryStep is one dictionary entry enclosing a string (see
// [EntryViolationError]).
type EntryStep struct {
	// Key is the key of the entry.
	Key ast.PdfName
	// Depth is the nesting depth the entry's value is read at.
	Depth int
}

// Error returns the message of the wrapped violation.
func (e *EntryViolationError) Error() string { return e.err.Error() }

// Unwrap returns the wrapped violation.
func (e *EntryViolationError) Unwrap() error { return e.err }

// recordStringViolation is [Parser.recordViolationErr] for a violation in the
// string being read, which it records as an [EntryViolationError] when the
// string is read inside the value of a dictionary entry.
func (p *Parser) recordStringViolation(err error) {
	if n := len(p.entryPath); n > 0 {
		err = &EntryViolationError{Key: p.entryPath[n-1].Key, Depth: p.depth, Path: slices.Clone(p.entryPath), err: err}
	}
	p.recordViolationErr(err)
}

// Peek returns the token n positions ahead without consuming it.
func (p *Parser) Peek(n int) (lexer.Token, error) {
	for p.buffered() <= n {
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
	return p.tokens[p.head+n], nil
}

// Consume returns the next token and advances the parser position.
func (p *Parser) Consume() (lexer.Token, error) {
	tok, err := p.Peek(0)
	if p.buffered() > 0 {
		p.consumed++
		p.head++
		if p.head == len(p.tokens) {
			// Drained: rewind to the front of the backing arrays so the next
			// read-ahead reuses them instead of allocating.
			p.tokens = p.tokens[:0]
			p.ends = p.ends[:0]
			p.head = 0
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

// Consumed returns how many tokens the parser has consumed. Taken before a
// [Parser.ParseObject] call, it is the mark [Parser.SkipFailedObject] needs to
// recover from that call's failure.
func (p *Parser) Consumed() int {
	return p.consumed
}

// SkipFailedObject moves the parser past an object [Parser.ParseObject] failed
// to read, given the [Parser.Consumed] mark taken before the call, so that the
// malformed object costs itself and nothing after it. It returns the error the
// token stream is in when no token follows, which is where the caller's
// recovery ends.
//
// ParseObject consumes the token it fails on for nearly every malformed object:
// a number it cannot read, such as "1e2", which has no ISO 32000-1, 7.3.3
// syntax; a stray delimiter; a bare keyword; a string it cannot decode.
// Consuming one more token after the failure took the next object with it, and
// in a content stream that object is often the operator: "/F1 1e2 Tf" lost its
// Tf, and "[(a) 1e2] TJ" lost the "]" that closes the array, which then ran on
// through the rest of the stream. A token is consumed here only when the failed
// call consumed none, which is what keeps a recovery loop moving forward.
//
// Neither reference reader loses the operator. PDF.js ends the number at the
// "e" (Lexer.getNumber), drops the unknown command "e2" and keeps its operands
// for the operator that follows (EvaluatorPreprocessor.read), so its Tf reads a
// size of 1. veraPDF ends it at the "e" too (BaseParser.readNumber) and reads
// "e" as an operator and 2 as the size (PDFStreamParser.parseNextToken).
//
// # Corpus evidence
//
//   - pdfjs sci-notation.pdf: writes "/F1 1e2 Tf" after the stray keyword
//     ETBT. The Tf it lost made PDF/A-1b and PDF/A-2b remediation roll back
//     its content repairs; they now stay, and veraPDF reports no undefined
//     operator on the output. The line after it leaves litho's text: the Td
//     that places it had been ignored for the stray /F1 it carried, and
//     litho reads ETBT as one undefined operator, not the ET BT PDF.js reads.
//   - pdfjs poppler-90-0-fuzzed.pdf, issue11678.pdf: keep tokens of damaged
//     streams that were swallowed, which raises litho's ISO 19005-2, 6.2.2
//     t1 count from 1617 to 1924 and from 2 to 3. Page 14 of the first
//     recovers the text "function[10]M. Naor".
//   - pdfjs issue11088.pdf, issue15893_reduced.pdf, issue19484_2.pdf,
//     poppler-742-0-fuzzed.pdf: gain an operator in a stream of binary
//     noise, with no change to text or violations.
//   - All corpora: no other file changes the operators of a page or form
//     XObject, its page count, its text or its PDF/A-2b violations.
func (p *Parser) SkipFailedObject(mark int) error {
	if p.consumed != mark {
		_, err := p.Peek(0)
		return err
	}
	_, err := p.Consume()
	return err
}

// maxParseNestingDepth bounds how deeply [Parser.ParseObject] recurses into
// nested objects. Anti-DoS guard, not spec-mandated: ISO 32000-1 sets no
// nesting limit, but the recursion is otherwise bounded by the goroutine stack
// alone, and a stack overflow is a fatal runtime error rather than a panic, so
// the recover boundary every pkg/pdf entry point installs cannot convert it and
// the process dies. Measured without the guard: a dictionary nested 850,000
// deep, a 7.3 MB file, killed the process through pdf.Open, and an object
// header chain did the same past 1,000,000.
//
// The value leaves real documents about two orders of magnitude of headroom
// while sitting far below the depth that exhausts the stack.
//
// # Corpus evidence
//
//   - All corpora, sampled: the deepest nesting is 156, in govdocs
//     thread_200_200087.pdf, with a 99th percentile of 55.
//   - All corpora: the sample reads raw file bytes and cannot see inside
//     compressed content streams, which is why the bound is generous rather
//     than tight.
const maxParseNestingDepth = 10000

// ParseObject reads the next complete PDF object from the token stream.
// The context enables cancellation at natural checkpoints (per dict key,
// per array element, per stream scan iteration) without adding overhead
// to the per-token lexer loop.
//
// Nesting is counted here rather than in the container parsers because every
// recursive path returns through this function: a dictionary value and an
// array element are both read by it, and `N G obj` reaches it again through
// [Parser.parseIndirectObject]. The count is restored on the way out rather
// than by a defer, which this path takes often enough to notice, and the
// restore has to survive an error return because the content-stream reader
// drives one parser across a whole page.
func (p *Parser) ParseObject(ctx context.Context) (ast.PdfObject, error) {
	p.depth++
	obj, err := p.parseObject(ctx)
	p.depth--
	return obj, err
}

// parseObject holds the body of [Parser.ParseObject], which wraps it to keep
// the nesting count correct on every return path.
func (p *Parser) parseObject(ctx context.Context) (ast.PdfObject, error) {
	if p.depth > maxParseNestingDepth {
		return nil, fmt.Errorf("object nested deeper than %d: %w", maxParseNestingDepth, errNestingTooDeep)
	}

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
		// Peek records every lexer error but one: the data ending inside a
		// token, which leaves nothing further to read.
		if t0.Type == lexer.TokenError && p.err == nil {
			return nil, fmt.Errorf("%s: %w", t0.Value, errTruncatedToken)
		}
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
			// leniency and remain a parse error below, matching prior
			// behaviour, to avoid shifting text extraction.
			if p.checkFormat && strings.ContainsAny(t0.Value, "eE") {
				p.recordNumberExponent()
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
					// The value is read as a real, but the literal stays an
					// integer, which is what a compliance check must judge.
					p.IntegerOverflows = append(p.IntegerOverflows, f)
					return ast.PdfReal(f), nil
				}
			}
			// A bare exponent stays unread, but it is the same divergence
			// from 7.3.3 as "1.5e2", and veraPDF reads both the same way.
			if p.checkFormat && strings.ContainsAny(t0.Value, "eE") {
				p.recordNumberExponent()
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
			return nil, fmt.Errorf("unexpected delimiter %q: %w", t0.Value, errUnexpectedDelimiter)
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
	//
	// Deliberate leniency: ISO 32000-1, 7.3.10 brackets an indirect object's
	// value "between the keywords obj and endobj". The value parsed so far is
	// returned without its closing keyword. No file or Acrobat measurement
	// backing the attribution is recorded.
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
// [ast.PdfDict]. It tolerates EOF, a premature `endobj` and a `stream`
// keyword as implicit close markers so it still returns a partial dict for
// malformed input (the top-level parser continues, lenient by design). A token
// the data ends inside, such as a literal string still open at the end set by
// [Parser.WithObjectEnd], closes it the same way.
//
// Deliberate leniency: ISO 32000-1, 7.3.7 writes a dictionary as a sequence of
// key-value pairs whose keys are names, and a malformed pair costs that pair
// here rather than the dictionary. Damaged files still carry readable pairs
// around the damage, and closing the dictionary at the first bad token threw
// away every pair after it. A token that breaks the alternation is dropped and
// parsing resumes at the next name that starts a pair:
//
//   - A bare keyword or a stray delimiter where a key belongs is dropped. A
//     space inside a name leaves one (`/Arial,Unicode MS` lexes as a name and
//     the keyword MS), and so does an array closed twice.
//   - A non-name object where a key belongs, a stray value, is dropped.
//   - A bare keyword or a stray delimiter where a value belongs drops the key
//     it follows. White space inside a key leaves one: a NUL ends a name
//     (ISO 32000-1, 7.2.2, Table 1), so `<< /Ty\x00pe /Catalog /Pages 2 0 R >>`
//     lexes as the key Ty, the keyword pe, the names Catalog and Pages, and
//     a reference.
//
// After any of these the next name is not taken as a key on trust: a keyword
// that split a key leaves the key's value next, and one that stood in for a
// value leaves the next key. [Parser.nameRunStartsWithKey] decides which from
// what follows.
//
// PDF.js (src/core/parser.js, Parser.getObj) skips a non-name token where a key
// belongs and reads whatever follows a key as its value, a bare keyword
// included. It therefore stays in step when a keyword replaced a value and
// pairs every later entry out of step when a keyword split a key: it reads the
// catalog above as a key Catalog with the value /Pages and loses the page
// tree, which litho keeps.
//
// # Corpus evidence
//
//   - pdfjs poppler-742-0-fuzzed.pdf: ends its /AcroForm with `/Q 0 > >>`
//     and lost the whole interactive form while a stray delimiter dropped
//     the dictionary.
//   - pdfjs poppler-937-0-fuzzed.pdf: lost its page tree to a
//     `/Kids 3 0 R]` while a stray delimiter dropped the dictionary.
//   - pdfjs poppler-742-0-fuzzed.pdf: writes `/Type Annot` in a push button's
//     widget, which came back empty and hid the /AA entry ISO 19005-2, 6.4.1
//     forbids. A `/Type Border` in another widget's /BS closed the widget
//     itself, which lost its /MK, /Parent, /Ff, /V and /DA.
//   - pdfjs poppler-90-0-fuzzed.pdf: writes `/Columns \xa04` in an inline
//     image's /DP, which lost the /BlackIs1 true after it.
//   - pdfjs issue11678.pdf: object 21's dictionary runs into binary image
//     data, where a literal string spans 184 KB, and its parse stopped by
//     chance at a keyword in a value position. Keeping the pairs after a
//     malformed entry ran it 62 KB further and added 97 findings to the two
//     ISO 19005-2, 6.1.6 hex string tests, which veraPDF reports neither of,
//     until [Parser.WithObjectEnd] ended the object where object 22 starts.
//   - All corpora: the parser met a keyword or a stray delimiter where a
//     value belongs in 7 dictionaries of 3 files. No file changes its page
//     count or its text, and every file but poppler-742-0-fuzzed.pdf and
//     issue11678.pdf reports the same violations under PDF/A-2b, veraPDF
//     files under their own profile.
func (p *Parser) parseDictBody(ctx context.Context) (ast.PdfDict, error) {
	dict := make(ast.PdfDict)
	// A /Contents string is parsed undecrypted and settled once the whole
	// dictionary is known, because /Type may follow it. See
	// [Parser.settleSignatureContents].
	deferredContents := false
	// resync is set once a malformed entry has put the key/value alternation
	// in doubt, and cleared at the next name.
	resync := false
	// vera follows the dictionary as veraPDF reads it, under
	// [Parser.WithVeraPDFContainers] alone.
	var vera *veraPDFDict
	if p.veraPDFContainers {
		vera = &veraPDFDict{}
	}
	// ended is the token that ends the dictionary, the end of the data unless
	// a close marker does.
	ended := lexer.Token{Type: lexer.TokenEOF}
	for {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("parsing dict: %w", err)
		}
		t, err := p.Peek(0)
		if err != nil {
			return nil, err
		}
		closed, err := p.closeDict(t)
		if err != nil {
			return nil, err
		}
		if closed {
			ended = t
			break
		}

		if resync && t.Type == lexer.TokenName {
			resync = false
			if !p.nameRunStartsWithKey() {
				// The name is the value of the entry the malformed token
				// damaged, not a key.
				if _, err := p.Consume(); err != nil {
					return nil, err
				}
				vera.item(ast.PdfName(strings.TrimPrefix(t.Value, "/")), t)
				continue
			}
		}

		keyObj, err := p.ParseObject(ctx)
		if err != nil {
			// The stray token is already consumed. Other errors (EOF, lexer
			// failures) are not recoverable.
			if isStrayToken(err) {
				vera.item(nil, t)
				resync = true
				continue
			}
			// The data ends inside the token, which closes the dictionary as
			// the end of the data does: the unterminated token is the damage,
			// not the pairs before it.
			if errors.Is(err, errTruncatedToken) {
				break
			}
			return nil, err
		}
		vera.item(keyObj, t)
		key, ok := keyObj.(ast.PdfName)
		if !ok {
			resync = true
			continue
		}

		t, err = p.Peek(0)
		if err != nil {
			return nil, err
		}
		// A key with no value before a close marker is dropped.
		closed, err = p.closeDict(t)
		if err != nil {
			return nil, err
		}
		if closed {
			ended = t
			break
		}
		outer := p.noteEntry(key)
		valObj, err := p.parseDictValue(ctx, key, &deferredContents)
		p.restoreEntry(outer)
		if err != nil {
			// Nesting past [maxParseNestingDepth] is a refusal, not a damaged
			// value to recover from. Closing the dict here would hand back a
			// truncated object as though it were whole, which is the silent
			// wrong answer a depth bound produces when it stops the walk
			// without reporting that it did.
			if errors.Is(err, errNestingTooDeep) {
				return nil, err
			}
			if isStrayToken(err) {
				vera.item(nil, t)
				resync = true
				continue
			}
			// A value that fails for any other reason, such as a string that
			// does not decrypt, closes the dict with the pairs read so far.
			break
		}
		vera.item(valObj, t)
		dict[key] = valObj
	}
	if vera.failed(ended) {
		p.abandon()
	}
	if deferredContents {
		if err := p.settleSignatureContents(dict); err != nil {
			return nil, err
		}
	}
	return dict, nil
}

// closeDict reports whether t ends the dictionary being read, consuming it
// when it is the `>>` that closes it. EOF, `endobj` and `stream` end a
// dictionary whose `>>` is missing and are left for the caller: `endobj` for
// [Parser.parseIndirectObject], and `stream` for [Parser.parseDict], which
// reads the stream body that follows. Recovery from a malformed entry must
// never run past them, since the bytes after `stream` are not tokens. Under
// [Parser.WithVeraPDFContainers] they end nothing: a content stream holds no
// indirect object and no stream (ISO 32000-1, 7.8.2), so the bytes after
// either keyword are tokens, and veraPDF reads it as a value it cannot read
// (COSParser.nextObject).
//
// # Corpus evidence
//
//   - pdfjs poppler-742-0-fuzzed.pdf: closes the dictionary of form XObject
//     44 with a single `>` before `stream`. Read as a stray keyword, `stream`
//     sent the dictionary on through the compressed data, and the object came
//     back a plain dictionary with no content.
func (p *Parser) closeDict(t lexer.Token) (bool, error) {
	switch {
	case t.Type == lexer.TokenEOF:
		return true, nil
	case t.Type == lexer.TokenKeyword && (t.Value == "endobj" || t.Value == "stream"):
		return !p.veraPDFContainers, nil
	case t.Type == lexer.TokenDelimiter && t.Value == ">>":
		if _, err := p.Expect(lexer.TokenDelimiter); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}

// isStrayToken reports whether err is [Parser.ParseObject] meeting a token
// that cannot start an object: a bare keyword, or a delimiter other than `<<`
// and `[`. ParseObject has already consumed that one token, so a container
// that drops it loses nothing else.
func isStrayToken(err error) bool {
	return errors.Is(err, errUnexpectedKeyword) || errors.Is(err, errUnexpectedDelimiter)
}

// maxDictResyncLookahead bounds how many consecutive names
// [Parser.nameRunStartsWithKey] reads ahead. Anti-DoS guard, not
// spec-mandated: every token read ahead stays buffered until the dictionary
// consumes it, so an unbounded run would let one malformed entry buffer a
// file's worth of names at once. The run only grows through entries whose
// value is itself a name, and 1024 names is 512 of those in a row, far more
// than any dictionary ISO 32000-1 defines holds. Past the bound the name is
// taken as a key, the reading PDF.js always makes.
const maxDictResyncLookahead = 1024

// nameRunStartsWithKey decides, after a malformed dictionary entry, whether
// the name at the head of the token buffer starts a pair or is the value of
// the entry the malformed token damaged.
//
// It reads the run of consecutive names that begins there and picks the
// alignment under which the run pairs up with what follows it (ISO 32000-1,
// 7.3.7: key, value, key, value). A run followed by a token that starts a
// value (a number, a string, `<<`, `[`, true, false or null) ends on a key,
// so its first name is a key when the run is odd. A run followed by anything
// else (`>>`, EOF, `endobj`, `stream`, or another stray token) ends on a
// value, so its first name is a key when the run is even.
//
// In `/Ty pe /Catalog /Pages 2 0 R >>` the run /Catalog /Pages is followed by
// a number, so /Pages is a key and /Catalog is the value of the split key. In
// `/Key junk /Type /Font >>` the run /Type /Font is followed by `>>`, so /Type
// is a key.
//
// # Corpus evidence
//
//   - pdfjs poppler-742-0-fuzzed.pdf: writes a border style as
//     `<< Type /Border /W 2 /S /B >>`, a key without its solidus. Taking the
//     next name as a key paired /Border with /W and left 2 where a key
//     belongs; the run /Border /W before 2 makes /Border the lost value, so
//     /W 2 and /S /B survive.
func (p *Parser) nameRunStartsWithKey() bool {
	run := 0
	for {
		t, err := p.Peek(run)
		if err != nil {
			// A read failure ends the dictionary as a close marker would.
			return run%2 == 0
		}
		if t.Type != lexer.TokenName {
			if startsValue(t) {
				return run%2 == 1
			}
			return run%2 == 0
		}
		run++
		if run > maxDictResyncLookahead {
			return true
		}
	}
}

// startsValue reports whether t begins a dictionary value other than a name:
// a number, a string, a dictionary, an array, a boolean or null (ISO 32000-1,
// 7.3). An indirect reference begins with a number.
func startsValue(t lexer.Token) bool {
	switch t.Type {
	case lexer.TokenNumber, lexer.TokenString, lexer.TokenHexString:
		return true
	case lexer.TokenDelimiter:
		return t.Value == "<<" || t.Value == "["
	case lexer.TokenKeyword:
		return t.Value == "true" || t.Value == "false" || t.Value == "null"
	case lexer.TokenEOF, lexer.TokenError, lexer.TokenName:
		return false
	}
	return false
}

// noteEntry notes, under format checking, that the value of the dictionary
// entry key is read next, one level below the current depth, so that a
// violation in a string read inside that value names key (see
// [EntryViolationError]). It returns the count of entries enclosing it, which
// [Parser.restoreEntry] puts back once the value is read.
func (p *Parser) noteEntry(key ast.PdfName) int {
	outer := len(p.entryPath)
	if p.checkFormat {
		p.entryPath = append(p.entryPath, EntryStep{Key: key, Depth: p.depth + 1})
	}
	return outer
}

// restoreEntry puts back the entries [Parser.noteEntry] returned.
func (p *Parser) restoreEntry(outer int) {
	p.entryPath = p.entryPath[:outer]
}

// parseDictValue reads the value of key. A /Contents whose value is a string
// literal is read without decryption and deferred to
// [Parser.settleSignatureContents]; every other value is parsed normally.
func (p *Parser) parseDictValue(ctx context.Context, key ast.PdfName, deferred *bool) (ast.PdfObject, error) {
	if key != "Contents" || p.decryptFn == nil {
		return p.ParseObject(ctx)
	}
	t, err := p.Peek(0)
	if err != nil {
		return nil, err
	}
	if t.Type != lexer.TokenString && t.Type != lexer.TokenHexString {
		return p.ParseObject(ctx)
	}

	saved := p.decryptFn
	p.decryptFn = nil
	valObj, err := p.ParseObject(ctx)
	p.decryptFn = saved
	if err != nil {
		return nil, err
	}
	*deferred = true
	return valObj, nil
}

// settleSignatureContents decides whether the deferred /Contents string is
// ciphertext or the one string in an encrypted file that is stored in the
// clear.
//
// ISO 32000-1, 7.6.2 exempts the signature value from the document's string
// encryption: the digest covers the file with the signature hole excised, so
// the bytes in that hole are written as they are. Decrypting them yields noise,
// and for AES the 16-byte initialisation vector is eaten as well, so the PKCS#7
// blob comes back 16 bytes short and no longer opens with a DER SEQUENCE tag.
//
// # Corpus evidence
//
//   - pdfjs xfa_issue14071.pdf: its 16288-byte PKCS#7 blob came back 16272
//     bytes long when decrypted and no longer opened with a DER SEQUENCE tag.
func (p *Parser) settleSignatureContents(dict ast.PdfDict) error {
	s, ok := dict["Contents"].(ast.PdfString)
	if !ok {
		return nil
	}
	if isSignatureDict(dict) {
		return nil
	}
	dec, err := p.decryptFn([]byte(s))
	if err != nil {
		return fmt.Errorf("decrypting /Contents string: %w", err)
	}
	dict["Contents"] = ast.PdfString(dec)
	return nil
}

// isSignatureDict reports whether dict is a signature or document timestamp
// dictionary (ISO 32000-1, Table 252).
//
// /Type is optional there, so a dictionary pairing a /Contents string with a
// /ByteRange array counts too. PDFBox reads the same two signals for the same
// reason.
func isSignatureDict(dict ast.PdfDict) bool {
	if name, ok := dict["Type"].(ast.PdfName); ok {
		if name == "Sig" || name == "DocTimeStamp" {
			return true
		}
	}
	_, hasRange := dict["ByteRange"].(ast.PdfArray)
	return hasRange
}

// parseStreamAfterDict consumes the `stream` keyword, the stream body
// (via fast or slow path), and returns a lazy [ast.PdfStream]. The caller
// has already confirmed the next token is `stream`.
//
// The stream data is exempt from any end [Parser.WithObjectEnd] set: /Length
// delimits it (ISO 32000-1, 7.3.8.2), and the lazy stream reads it straight
// from the file. A table offset that lands inside the data is the weaker claim
// of the two, since the header found there can belong to a file the stream
// embeds uncompressed. A stream whose /Length cannot be used has no such claim,
// and the scan for its end stops at the bound (see [Parser.readStreamBody]).
func (p *Parser) parseStreamAfterDict(ctx context.Context, dict ast.PdfDict) (ast.PdfObject, error) {
	if _, err := p.Expect(lexer.TokenKeyword); err != nil {
		return nil, err
	}

	// The parser must not have any lookahead tokens at this point: the
	// next bytes belong to the raw stream body, not the token stream.
	// Peek(0) of "stream" consumed its token via Expect() above; any
	// additional tokens in the buffer mean the parser read past the stream start.
	if p.buffered() > 0 {
		return nil, fmt.Errorf("parser read ahead too far before stream data")
	}

	reader := p.lex.Reader()
	reader.ClearLimit()
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
	//   - Any stream whose /Length already stops at the last data byte, which a
	//     surviving separator proves (see [separatorPrecedesEndstream]).
	trimLen := dataLen
	exactLength := usedLength && separatorPrecedesEndstream(reader.ReaderAt(), streamStart+dataLen)
	if (p.decryptFn == nil || streamUsesIdentityCryptFilter(dict)) && !isXRefStream(dict) && !exactLength {
		trimLen = trimStreamTail(reader.ReaderAt(), streamStart, dataLen)
	}
	return p.newLazyStream(dict, reader.ReaderAt(), streamStart, trimLen), nil
}

// separatorPrecedesEndstream reports whether an end-of-line marker sits at off,
// the position just past a stream body delimited by a usable /Length.
//
// ISO 32000-1, 7.3.8.1 places an optional EOL marker between the stream data and
// "endstream" and states it shall not be included in the stream length. A
// surviving marker therefore proves /Length stopped at the last data byte, so
// that byte is data even when it is itself a CR or LF and nothing may be trimmed.
// Trimming a counted newline that ends a comment leaves the comment
// unterminated, and since a comment runs to the end of the line (7.2.4) it
// swallows whatever opens the next part of the same /Contents array.
//
// Absence proves nothing: a producer that folded the marker into /Length is
// indistinguishable from one whose data simply ends in a newline, so the trim
// still runs there.
//
// # Corpus evidence
//
//   - pdfjs annotation-as.pdf: declares /Length 27 for the content stream
//     part "% CANON_PFINF_TYPE2_TEXTON\n" and writes the separator after it.
//     Trimming the counted newline left the comment unterminated, so it
//     swallowed the "q" that opened the next part of the same /Contents
//     array. The unbalanced q/Q pair moved the rest of the page out of view,
//     so `litho split` produced a page whose text no reader could find.
func separatorPrecedesEndstream(src io.ReaderAt, off int64) bool {
	var b [1]byte
	if _, err := src.ReadAt(b[:], off); err != nil {
		return false
	}
	return b[0] == '\n' || b[0] == '\r'
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
				p.recordViolation(ErrStreamKeywordEOL, "stream keyword followed by CR only (must be CR+LF or LF)")
			}
		}
	case '\n':
		// canonical case
	default:
		_ = reader.UnreadByte() // Guaranteed to succeed: previous ReadByte succeeded on a buffered reader.
		if p.checkFormat {
			p.recordViolation(ErrStreamKeywordEOL, "stream keyword not followed by EOL")
		}
	}
	return nil
}

// readStreamBody measures the stream body and positions the reader after
// it. It first tries a fast path using /Length; if /Length is missing or
// doesn't point at `endstream`, it falls back to a byte scan.
//
// The scan stops at the end [Parser.WithObjectEnd] set, where the next object
// starts, and the stream's data then runs up to it. ISO 32000-1, 7.3.8.1 ends
// the data at the endstream keyword and 7.3.10 ends the object at endobj, so
// an endstream found past the next object belongs to another stream: reading
// on to it took every object in between as this stream's data. Only /Length
// may carry the data past the next object's start (see
// [Parser.parseStreamAfterDict]), and the scan runs because /Length is not
// usable.
//
// The returned usedLength is true if the fast path was taken.
//
// # Corpus evidence
//
//   - pdfjs issue18122.pdf: object 11, a tiling pattern, states /Length 0 and
//     closes its data with a misspelt "endsteam". The scan read on through
//     object 13's image to its endstream, and the pattern's content held that
//     image's 366,997 bytes of JPEG data. It now holds the 20 bytes before
//     object 12, the misspelt keyword and endobj among them.
//   - All corpora: no other object changes, nor any file's page count, text,
//     undecodable streams or PDF/A-2b violations.
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
	if p.objectEnd > 0 {
		reader.SetLimit(p.objectEnd)
		defer reader.ClearLimit()
	}
	dataLen, err := p.scanEndstream(ctx, reader)
	return dataLen, false, err
}

// tryFastStreamLength attempts to use a direct /Length integer to seek
// past the stream body without scanning. Returns the byte count and true
// on success; zero and false on any mismatch. This is the hot path for
// well-formed documents with large streams (images, embedded files).
//
// # Corpus evidence
//
//   - pdfjs issue10004.pdf: writes every one of its 29 stream ends as a
//     truncated "endstrea" followed by "endobj". Rejecting those discarded 29
//     correct /Length values, and the fallback scan then found no "endstream"
//     anywhere and returned the rest of the file as stream data, so each
//     stream swallowed ~750 KB of unrelated bytes and the file grew 757 KB ->
//     12.6 MB.
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
	// intact. Rejecting a truncated keyword discards a correct /Length, and the
	// fallback scan then returns the rest of the file as stream data.
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
		// Match broke. Anything matched so far is stream data.
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
		p.recordViolation(ErrStreamLengthMismatch, fmt.Sprintf(
			"stream /Length %d does not match actual byte count %d",
			declaredLength, dataLen))
		return
	}
	if declaredLength == 0 {
		if _, hasLength := dict["Length"]; !hasLength {
			p.recordViolation(ErrStreamMissingLength, "stream missing /Length key")
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

// parseArray reads the elements of an array up to its closing `]`, tolerating
// EOF, a premature endobj and malformed elements.
//
// # Corpus evidence
//
//   - pdfjs bug1953099.pdf: writes a lone "-" between two strings in a TJ
//     array. Consuming that bad keyword twice swallowed the (e) that followed
//     it, so the word "extensively" came out a letter short.
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
		// Tolerate EOF or endobj inside an array: close gracefully. veraPDF
		// reads neither as the end of an array.
		if t.Type == lexer.TokenEOF {
			p.abandon()
			break
		}
		if t.Type == lexer.TokenKeyword && t.Value == "endobj" {
			p.abandon()
			break
		}
		if t.Type == lexer.TokenDelimiter && t.Value == "]" {
			if _, err := p.Expect(lexer.TokenDelimiter); err != nil {
				return nil, err
			}
			break
		}

		mark := p.consumed
		obj, err := p.ParseObject(ctx)
		if err != nil {
			// Nesting past [maxParseNestingDepth] is a refusal rather than a
			// bad element to skip: continuing would return an array whose
			// contents were silently cut short.
			if errors.Is(err, errNestingTooDeep) {
				return nil, err
			}
			// Tolerate malformed array elements (e.g., operators inside TJ
			// arrays): drop the bad element and continue with the next one.
			// veraPDF gives up on the stream there.
			p.abandon()
			if p.SkipFailedObject(mark) != nil {
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
					buf.WriteByte(byte(parsed & 0xFF)) //nolint:gosec // masked to 8 bits on the line itself
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
	// Consume inline image dictionary entries until the ID keyword.
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
		// Consume the token (key or value); its data is not needed.
		if _, err := p.Consume(); err != nil {
			return fmt.Errorf("inline image dict entry: %w", err)
		}
	}

	// Ensure no lookahead tokens remain before switching to raw byte reading.
	if p.buffered() > 0 {
		return fmt.Errorf("parser read ahead too far before inline image data")
	}

	// Skip the single white-space byte that follows ID. ISO 32000-1, 8.9.7
	// requires it unless the image uses ASCIIHexDecode or ASCII85Decode. With
	// those filters the byte may be the first byte of data, which is harmless
	// here: only the EI terminator is sought, and the data is not returned.
	reader := p.lex.Reader()
	if _, err := reader.ReadByte(); err != nil {
		return fmt.Errorf("failed to read post-ID whitespace: %w", err)
	}

	// Scan forward for the EI terminator. ISO 32000-1, 8.9.7 places the data
	// "between the ID and EI operators" and gives no length for it, so the
	// end is found by heuristic: "EI" read as its own token (7.2.2), preceded
	// by white space and followed by white space, a delimiter, or EOF. Raw
	// bytes 'E' and 'I' can occur inside image data, so the scan validates
	// the trailing context to avoid false matches.
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
// the properties as an [ast.PdfDict]. Called after the BI operator has been consumed.
// Dictionary keys are PDF names (e.g., /W, /H, /BPC, /CS) and values are
// parsed as normal PDF objects.
func (p *Parser) ParseInlineImageDict(ctx context.Context) (ast.PdfDict, error) {
	dict, err := p.ParseInlineImageHeader(ctx)
	if err != nil {
		return nil, err
	}

	// Skip the single white-space byte that follows ID (ISO 32000-1, 8.9.7).
	// See SkipInlineImage for the ASCIIHexDecode and ASCII85Decode case.
	reader := p.lex.Reader()
	if _, err := reader.ReadByte(); err != nil {
		return nil, fmt.Errorf("failed to read post-ID whitespace: %w", err)
	}

	// Scan forward for the EI terminator, found by the heuristic
	// SkipInlineImage describes.
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

// ParseInlineImageHeader reads an inline image's dictionary key/value pairs
// (between BI and ID) and returns them as an [ast.PdfDict], as
// [Parser.ParseInlineImageDict] does, but reads no image data: the parser
// stands just past the ID keyword, with no token read ahead, so a caller that
// finds the end of the data itself reads on with [Parser.ResumeAt]. Called
// after the BI operator has been consumed.
func (p *Parser) ParseInlineImageHeader(ctx context.Context) (ast.PdfDict, error) {
	dict := ast.PdfDict{}

	// Parse key/value pairs until the ID keyword.
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
			// Tolerate unexpected tokens by skipping them. veraPDF reads the
			// dictionary up to the first key that is no name, which has to be
			// the ID it ends at (PDFStreamParser.parseNextToken, veraPDF-parser
			// 1.30), and gives up on the stream otherwise.
			p.abandon()
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

		// The entries between BI and ID form a dictionary (ISO 32000-1,
		// 8.9.7), so a value is counted one level down, as a dictionary's
		// value is: it is no operand of the content stream.
		p.depth++
		mark := p.consumed
		outer := p.noteEntry(keyName)
		val, err := p.ParseObject(ctx)
		p.restoreEntry(outer)
		p.depth--
		if err != nil {
			// Tolerate a malformed value by dropping it. Consuming a token
			// past it would take the next key, or the ID that ends the
			// dictionary, with it.
			if skipErr := p.SkipFailedObject(mark); skipErr != nil {
				return nil, fmt.Errorf("inline image dict value for %q: %w", keyName, err)
			}
			continue
		}
		dict[keyName] = val
	}

	// Ensure no lookahead tokens remain before switching to raw byte reading.
	if p.buffered() > 0 {
		return nil, fmt.Errorf("parser read ahead too far before inline image data")
	}

	return dict, nil
}

// ResumeAt moves the parser to offset in the data it reads, from where it
// reads its next token. It is for a caller that has read raw bytes past what
// the parser returned, such as the data of an inline image after
// [Parser.ParseInlineImageHeader], and must hold no token read ahead. An
// offset past the end of the data leaves nothing more to read.
func (p *Parser) ResumeAt(offset int64) error {
	if p.buffered() > 0 {
		return errors.New("resuming with tokens read ahead")
	}
	if _, err := p.lex.Reader().Seek(offset, io.SeekStart); err != nil {
		return fmt.Errorf("resuming at %d: %w", offset, err)
	}
	return nil
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

	// Record any invalid hex character before it is dropped below, and count
	// the non-white-space characters: ISO 19005-2, 6.1.6 t1 requires an even
	// number of them, invalid ones included, so an odd count of hex digits
	// padded by a stray character is no odd-length string. veraPDF counts the
	// same way (BaseParser.readHexString's hexCount adds every byte that is not
	// white space): <484!> fails only t2.
	nonWhiteSpace := 0
	if p.checkFormat {
		reported := false
		for _, r := range val {
			// PDF whitespace: null, tab, LF, FF, CR, space (ISO 32000-1 §7.2.2).
			isWS := r == 0x00 || r == '\t' || r == '\n' || r == 0x0C || r == '\r' || r == ' '
			if isWS {
				continue
			}
			nonWhiteSpace++
			isHex := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
			if !isHex && !reported {
				p.recordStringViolation(fmt.Errorf("%w %q", ErrHexStringInvalidChar, r))
				reported = true
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

	if p.checkFormat && nonWhiteSpace%2 != 0 {
		p.recordStringViolation(ErrHexStringOddDigits)
	}
	if len(val)%2 != 0 {
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
