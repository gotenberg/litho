package content

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/ioat"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
	"github.com/gotenberg/litho/pkg/internal/core/parser"
	"github.com/gotenberg/litho/pkg/internal/limits"
)

// Operation represents a drawing or state operation in a PDF content stream.
// Each Operation pairs an operator keyword with its [ast.PdfObject] operands.
type Operation struct {
	Operator string
	Operands []ast.PdfObject
}

// ByteRange is the half-open byte span [Start, End) that an [Operation]
// occupies in the source content (its operands plus the operator keyword,
// including any leading whitespace). [ParseStreamDataWithOffsets] returns one
// per operation, parallel to the operations slice. Ranges are contiguous and
// monotonic, so the span of a run of operations is [first.Start, last.End].
type ByteRange struct {
	Start, End int
}

// parseOperations is the shared tokenization loop used by both [ParseStreamData]
// and [ParsePageContent]. It reads tokens from the parser, collecting operands
// and emitting operations when an operator keyword is encountered. Inline
// images (BI/ID/EI) are detected and safely skipped. When ranges is non-nil,
// the parser must have offset tracking enabled and each operation's byte
// range is appended to it. When placer is non-nil, the parser must have format
// checking enabled, and each violation it records is placed on the operation
// whose operands hold it.
func parseOperations(ctx context.Context, p *parser.Parser, ranges *[]ByteRange, placer *violationPlacer) ([]Operation, error) {
	ops := make([]Operation, 0, 256)        //nolint:mnd // reasonable initial capacity for typical pages
	operands := make([]ast.PdfObject, 0, 8) //nolint:mnd // most operators have ≤6 operands
	tokenCount := 0
	var prevEnd int64

	recordRange := func(end int64) {
		if ranges == nil {
			return
		}
		// Token end offsets are monotonic in file order, so end >= prevEnd
		// always holds in valid flow; the guard is defense-in-depth and does
		// not fire on well-formed input.
		if end < prevEnd {
			end = prevEnd
		}
		*ranges = append(*ranges, ByteRange{Start: int(prevEnd), End: int(end)})
		prevEnd = end
	}

	for {
		tokenCount++
		if tokenCount%500 == 0 {
			if err := ctx.Err(); err != nil {
				return ops, err
			}
		}

		tok, err := p.Peek(0)
		if err != nil && err != io.EOF && tok.Type == lexer.TokenError {
			return nil, fmt.Errorf("lexer error: %w", err)
		}
		if tok.Type == lexer.TokenEOF {
			break
		}

		if tok.Type == lexer.TokenKeyword && tok.Value != "true" && tok.Value != "false" && tok.Value != "null" {
			// Inline images: BI starts a binary image block that would
			// crash the lexer. Consume BI and skip past ID/<data>/EI.
			if tok.Value == "BI" {
				if _, err := p.Consume(); err != nil {
					return nil, fmt.Errorf("failed to consume BI: %w", err)
				}
				placer.dropOperands()
				dict, err := p.ParseInlineImageDict(ctx)
				if err != nil {
					return nil, fmt.Errorf("inline image: %w", err)
				}
				placer.take(0)
				ops = append(ops, Operation{
					Operator: "BI",
					Operands: []ast.PdfObject{dict},
				})
				placer.place("BI", ops[len(ops)-1].Operands)
				operands = nil
				// The image data is consumed off-token by ParseInlineImageDict,
				// which empties the token buffer before reading raw bytes, so
				// the reader position is just past EI and bounds the operation.
				recordRange(p.Position())
				continue
			}

			opEnd := p.PeekEnd(0) // end offset of the operator token, before consuming it
			ops = append(ops, Operation{
				Operator: tok.Value,
				Operands: operands,
			})
			placer.place(tok.Value, operands)
			operands = nil
			if _, err := p.Consume(); err != nil {
				return nil, fmt.Errorf("failed to consume operator token: %w", err)
			}
			recordRange(opEnd)
		} else {
			mark := p.Consumed()
			obj, err := p.ParseObject(ctx)
			if err != nil {
				// Tolerate a malformed operand (a stray delimiter like ']', a
				// broken reference, a number such as "1e2") by dropping it and
				// continuing: lenient parsers skip unrecognised tokens in content
				// streams rather than aborting. Only the operand goes. The
				// operator after it keeps its place and the operands before it.
				if skipErr := p.SkipFailedObject(mark); skipErr != nil {
					return nil, fmt.Errorf("failed to parse operand: %w (token: %v)", err, tok)
				}
				placer.take(noOperand)
				continue
			}
			placer.take(len(operands))
			operands = append(operands, obj)
		}
	}

	return ops, nil
}

// Option configures how [ParseStreamData], [ParseStreamDataWithOffsets] and
// [ParsePageContent] read a content stream.
type Option func(*readOptions)

// readOptions is what the [Option] values of a parse set.
type readOptions struct {
	// commands is the command set the lexer ends a run of regular characters
	// at, or nil for the token boundaries of ISO 32000-1, 7.2.2.
	commands *lexer.CommandSet
}

// SplitGluedOperators reads operators written with no white space between
// them as PDF.js reads them: "ETBT" is ET then BT, and "q1" is q then the
// number 1. See [lexer.Lexer.WithCommands] for the rule and the leniency it
// takes with ISO 32000-1, 7.2.2 and 7.8.2, and [IsOperator] for the operators
// of Annex A, Table A.1 it splits at.
//
// It is for the readings that follow a viewer: text extraction, the widths of
// Type 3 glyphs, and the redaction that has to remove what a viewer shows where
// the viewer shows it. Without it a parse keeps the token boundaries of 7.2.2,
// as veraPDF does (PDFStreamParser.nextOperator), so compliance reads "ETBT" as
// the one operator ISO 19005 forbids, which veraPDF reports under ISO 19005-2,
// 6.2.2 t1, and the remediation that answers it writes the operators back
// apart ([GluedOperators]).
//
// # Corpus evidence
//
//   - pdfjs sci-notation.pdf: writes "ETBT" between its two text objects,
//     and its second line left the extracted text. It matches its PDF.js text
//     golden again, and its PDF/A-2b remediation, default and aggressive,
//     still clears 6.2.2 t1 for litho and veraPDF.
//   - All corpora: 8 pages of 5 files hold a run this reading splits, the
//     others being pdfjs bug1130815.pdf, issue11678.pdf,
//     poppler-90-0-fuzzed.pdf and litho contentstream_4.pdf. Beyond
//     sci-notation.pdf, the text of one page changes, page 14 of
//     poppler-90-0-fuzzed.pdf, by a comma, and no text verdict moves.
//   - All corpora: what compliance reads is unchanged on every page, and so
//     are the PDF/A-2b violations of every file, veraPDF files at their own
//     profile, and the violations left after a default or aggressive
//     remediation.
func SplitGluedOperators() Option {
	return func(o *readOptions) {
		o.commands = gluedOperators
	}
}

// GluedOperators returns the operators [SplitGluedOperators] reads keyword as
// when it reads two or more operators of ISO 32000-1, Annex A, Table A.1 and
// nothing else, and nil otherwise: "ETBT" gives ET and BT, while "ETXX", whose
// XX is no operator, "q1", whose 1 is an operand, and "ET" itself give nil.
// keyword is a run of regular characters as a parse without the option reads
// it, one undefined operator to validation, and a keyword holding white space
// or a delimiter is no such run and gives nil.
func GluedOperators(keyword string) []string {
	parts := gluedOperators.Split(keyword)
	if len(parts) < 2 {
		return nil
	}
	// No operator holds white space or a delimiter, so a keyword holding one
	// leaves a piece that is no operator.
	for _, part := range parts {
		if !IsOperator(part) {
			return nil
		}
	}
	return parts
}

// newLexer returns a lexer over data that reads it as opts say.
func newLexer(data []byte, opts []Option) *lexer.Lexer {
	var o readOptions
	for _, opt := range opts {
		opt(&o)
	}
	lex := lexer.NewLexer(ioat.NewReader(bytes.NewReader(data), int64(len(data)), 0))
	if o.commands != nil {
		lex.WithCommands(o.commands)
	}
	return lex
}

// ParseStreamData parses raw content stream bytes into a sequence of [Operation] values.
// The parser checks ctx at regular intervals, allowing large or pathological
// content streams to be interrupted by deadline. opts choose how it reads the
// stream, see [SplitGluedOperators].
func ParseStreamData(ctx context.Context, data []byte, opts ...Option) ([]Operation, error) {
	p := parser.NewParser(newLexer(data, opts))
	return parseOperations(ctx, p, nil, nil)
}

// ParseStreamDataWithOffsets parses content stream bytes and also returns the
// byte range of each operation, parallel to the operations slice. Use it to
// map operations back to the source bytes (e.g. to inject or excise
// marked-content around a run of operations). The ranges are into data.
//
// Under [SplitGluedOperators] the operations split from one run of regular
// characters share its bytes: in "ETBT", ET ends after its T and BT holds the
// two bytes after it.
func ParseStreamDataWithOffsets(ctx context.Context, data []byte, opts ...Option) ([]Operation, []ByteRange, error) {
	p := parser.NewParser(newLexer(data, opts)).WithOffsetTracking()
	ranges := make([]ByteRange, 0, 256) //nolint:mnd // matches the operations capacity hint
	ops, err := parseOperations(ctx, p, &ranges, nil)
	if err != nil {
		// On error parseOperations returns ops=nil but ranges may hold the
		// entries recorded before the failure; drop them so the two slices
		// stay parallel (both nil) per the documented contract.
		return nil, nil, err
	}
	return ops, ranges, nil
}

// ParseStreamDataFormatChecked parses content stream data with PDF/A format
// checking enabled. Returns [Operation] values and any hex string format
// violations detected by the parser.
func ParseStreamDataFormatChecked(ctx context.Context, data []byte) ([]Operation, []error) {
	lex := lexer.NewLexer(ioat.NewReader(bytes.NewReader(data), int64(len(data)), 0))
	p := parser.NewParser(lex).WithFormatChecking()
	ops, _ := parseOperations(ctx, p, nil, nil)
	return ops, p.FormatViolations
}

// ParseStreamDataWithOverflows parses content stream data like
// [ParseStreamData] and also returns the value of every integer literal the
// parser read as a real because it overflows 64 bits (see
// [parser.Parser.IntegerOverflows]).
func ParseStreamDataWithOverflows(ctx context.Context, data []byte) ([]Operation, []float64, error) {
	lex := lexer.NewLexer(ioat.NewReader(bytes.NewReader(data), int64(len(data)), 0))
	p := parser.NewParser(lex)
	ops, err := parseOperations(ctx, p, nil, nil)
	return ops, p.IntegerOverflows, err
}

// ParseStreamDataForCompliance parses content stream data with PDF/A format
// checking enabled and returns, from that one parse, what
// [ParseStreamDataWithOverflows] returns and the format violations
// [ParseStreamDataFormatChecked] returns, each placed on the operation whose
// operands hold it. Format checking records violations without changing the
// operations, so a walk that judges both the operators and the syntax of a
// stream parses it once.
//
// The violations come back even when the parse fails: the ones recorded before
// the failure keep their placement, and the rest have no operator.
func ParseStreamDataForCompliance(ctx context.Context, data []byte) ([]Operation, []float64, []FormatViolation, error) {
	lex := lexer.NewLexer(ioat.NewReader(bytes.NewReader(data), int64(len(data)), 0))
	p := parser.NewParser(lex).WithFormatChecking()
	placer := &violationPlacer{p: p}
	ops, err := parseOperations(ctx, p, nil, placer)
	return ops, p.IntegerOverflows, placer.finish(), err
}

// FormatViolation is a format violation the parser recorded in a content
// stream, placed on the operation whose operands hold it. A reader that judges
// the syntax of some operands only, as veraPDF does with strings, tells the
// violations apart by this placement.
type FormatViolation struct {
	// Err is the violation. Match its class with [errors.Is].
	Err error
	// Operator is the keyword of the operation whose operands hold the
	// violation, or "" when no operator follows them before the stream ends or
	// the parse fails.
	Operator string
	// Operand is the operand of that operation that holds the violation, or
	// nil when the violation lies in no operand the operation kept, such as a
	// token the parser could not read.
	Operand ast.PdfObject
	// Last reports whether Operand is the operation's last operand.
	Last bool
}

// noOperand is the operand index of a violation that lies in no operand an
// operation keeps.
const noOperand = -1

// pendingViolation is a violation recorded among the operands of an operation
// whose operator is not read yet.
type pendingViolation struct {
	err     error
	operand int
}

// violationPlacer places the violations a format-checking parser records on
// the operations [parseOperations] emits. A nil placer places nothing, so the
// parses that do not check the format pay nothing for it.
type violationPlacer struct {
	p       *parser.Parser
	taken   int
	pending []pendingViolation
	placed  []FormatViolation
}

// take attributes the violations recorded since its last call to the operand
// at index operand of the operation being read, or to none for [noOperand].
func (vp *violationPlacer) take(operand int) {
	if vp == nil {
		return
	}
	for _, err := range vp.p.FormatViolations[vp.taken:] {
		vp.pending = append(vp.pending, pendingViolation{err: err, operand: operand})
	}
	vp.taken = len(vp.p.FormatViolations)
}

// dropOperands detaches the pending violations from their operands. BI drops
// the operands read before it, so what they hold lies in no operand of the
// inline image operation.
func (vp *violationPlacer) dropOperands() {
	if vp == nil {
		return
	}
	vp.take(noOperand)
	for i := range vp.pending {
		vp.pending[i].operand = noOperand
	}
}

// place places the pending violations on the operation just read.
func (vp *violationPlacer) place(operator string, operands []ast.PdfObject) {
	if vp == nil {
		return
	}
	vp.take(noOperand)
	for _, pv := range vp.pending {
		v := FormatViolation{Err: pv.err, Operator: operator}
		if pv.operand >= 0 && pv.operand < len(operands) {
			v.Operand = operands[pv.operand]
			v.Last = pv.operand == len(operands)-1
		}
		vp.placed = append(vp.placed, v)
	}
	vp.pending = vp.pending[:0]
}

// finish returns every violation recorded, the ones no operator followed
// included.
func (vp *violationPlacer) finish() []FormatViolation {
	vp.take(noOperand)
	for _, pv := range vp.pending {
		vp.placed = append(vp.placed, FormatViolation{Err: pv.err})
	}
	vp.pending = nil
	return vp.placed
}

// ParsePageContent extracts, concatenates, and parses the content streams of a page or form XObject.
//
// The concatenated content is bounded by [limits.Limits.MaxPageContentBytes] of
// the document's budgets: ISO 32000-1, 7.7.3.3 lets a page split its content
// across an array of streams, and the per-stream budget says nothing about
// their sum. opts choose how it reads the streams, as for [ParseStreamData].
func ParsePageContent(ctx context.Context, doc *document.Document, node ast.PdfObject, opts ...Option) ([]Operation, error) {
	var streams []ast.PdfStream

	switch v := node.(type) {
	case ast.PdfDict:
		contentsObj, ok := v["Contents"]
		if !ok {
			return nil, nil // Empty page
		}

		contentsResolved, err := document.ResolveObject(ctx, doc, contentsObj)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve /Contents: %w", err)
		}

		switch c := contentsResolved.(type) {
		case ast.PdfStream:
			streams = append(streams, c)
		case ast.PdfArray:
			for _, item := range c {
				stream, err := document.ResolveStream(ctx, doc, item)
				if err != nil {
					// Tolerate missing or unresolvable content streams:
					// skip the bad item and continue with the rest.
					continue
				}
				if stream.Dict == nil {
					continue
				}
				streams = append(streams, stream)
			}
		default:
			return nil, fmt.Errorf("invalid /Contents type: %T", contentsResolved)
		}
	case ast.PdfStream:
		streams = append(streams, v)
	default:
		return nil, fmt.Errorf("invalid node type for ParsePageContent: %T", node)
	}

	limit := doc.Limits().MaxPageContentBytes()
	var allData []byte
	for _, s := range streams {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		data, err := doc.DecodeStream(ctx, s)
		if err != nil {
			// Pages can legitimately split content across many small
			// streams and leave one or two truncated or missing past
			// the filter's expected length; viewers render what they
			// can. Log the individual failure and keep assembling the
			// remaining streams rather than dropping the whole page.
			doc.Logger.Debug("content stream decode failed; skipping",
				slog.Any("err", err))
			continue
		}
		allData = append(allData, data...)
		allData = append(allData, '\n')
		if int64(len(allData)) > limit {
			return nil, fmt.Errorf("%w of %d bytes", limits.ErrPageContentTooLarge, limit)
		}
	}

	doc.Logger.Debug("content stream decoded",
		slog.Int("streams", len(streams)),
		slog.Int("total_bytes", len(allData)),
	)

	p := parser.NewParser(newLexer(allData, opts))
	ops, err := parseOperations(ctx, p, nil, nil)
	if err != nil {
		return nil, err
	}

	doc.Logger.Debug("content stream parsed",
		slog.Int("operators", len(ops)),
	)

	return ops, nil
}
