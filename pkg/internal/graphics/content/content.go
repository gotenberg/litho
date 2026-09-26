package content

import (
	"bytes"
	"context"
	"errors"
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
// whose operands hold it. o is how the parse reads the stream: read as veraPDF
// reads it, the parse ends where [endsReading] says, and reads the data of
// inline images as [inlineImages] says. data is what the parser reads.
func parseOperations(ctx context.Context, p *parser.Parser, o readOptions, data []byte, ranges *[]ByteRange, placer *violationPlacer) ([]Operation, error) {
	stops := o.readsAsVeraPDF()
	ops := make([]Operation, 0, 256)        //nolint:mnd // reasonable initial capacity for typical pages
	operands := make([]ast.PdfObject, 0, 8) //nolint:mnd // most operators have ≤6 operands
	tokenCount := 0
	var prevEnd int64
	// images reads the data of inline images, made on the first one the
	// parse meets.
	var images *inlineImages

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
		if gaveUp(p, o) {
			return nil, nil
		}
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
		if tok.Type == lexer.TokenEOF || stops && endsReading(tok) {
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
				if images == nil {
					images = newInlineImages(data)
				}
				dict, reads, err := readInlineImage(ctx, p, o, images)
				if err != nil {
					return nil, fmt.Errorf("inline image: %w", err)
				}
				if !reads {
					break
				}
				placer.take(0)
				ops = append(ops, Operation{
					Operator: "BI",
					Operands: []ast.PdfObject{dict},
				})
				placer.place("BI", ops[len(ops)-1].Operands)
				operands = nil
				// The image data is consumed off-token, and the token buffer is
				// empty before raw bytes are read, so the reader position is
				// where reading resumes, just past the end of the data the
				// reading takes, and bounds the operation.
				recordRange(p.Position())
				continue
			}

			// veraPDF reads an ID no BI begins as it reads the ID of an inline
			// image: the data after it is no operator (see [inlineImages]).
			if tok.Value == "ID" && stops {
				ops = append(ops, Operation{Operator: tok.Value, Operands: operands})
				placer.place(tok.Value, operands)
				operands = nil
				if _, err := p.Consume(); err != nil {
					return nil, fmt.Errorf("failed to consume operator token: %w", err)
				}
				if images == nil {
					images = newInlineImages(data)
				}
				if err := p.ResumeAt(int64(images.resume(ctx, int(p.Position()), nil))); err != nil {
					return nil, fmt.Errorf("inline image data: %w", err)
				}
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

	if gaveUp(p, o) {
		return nil, nil
	}
	return ops, nil
}

// readInlineImage reads the inline image whose BI p has just consumed, and
// leaves p where a parse under o reads on after it. It returns the image's
// dictionary, and false when the parse reads no further.
//
// Read as veraPDF reads the stream, the data ends as [inlineImages] ends it,
// and under [SplitGluedOperators] as PDF.js ends it ([pdfjsInlineImageEnd]).
// PDF.js ends the dictionary at the end of the stream, and throws out of the
// stream at a key that is no name (ISO 32000-1, 8.9.7) and at a token it
// cannot read (Parser.makeInlineImage and Lexer.getObj, src/core/parser.js),
// so a dictionary that parse cannot read ends it too, as does one that holds
// such a key. The other readings end the data at an EI that white space
// precedes and follows ([parser.Parser.ParseInlineImageDict]).
func readInlineImage(ctx context.Context, p *parser.Parser, o readOptions, images *inlineImages) (ast.PdfDict, bool, error) {
	switch {
	case o.commands != nil:
		dict, err := p.ParseInlineImageHeader(ctx)
		if err != nil || p.SkippedInlineImageKey() {
			return nil, false, ctx.Err()
		}
		end, stops := pdfjsInlineImageEnd(ctx, images.data, int(p.Position()), dict)
		if stops {
			return nil, false, nil
		}
		return dict, true, p.ResumeAt(int64(end))
	case o.readsAsVeraPDF():
		dict, err := p.ParseInlineImageHeader(ctx)
		if err != nil {
			return nil, false, err
		}
		return dict, true, p.ResumeAt(int64(images.resume(ctx, int(p.Position()), dict)))
	default:
		dict, err := p.ParseInlineImageDict(ctx)
		return dict, err == nil, err
	}
}

// endsReading reports whether tok, read where an operand or an operator
// stands, ends a parse that reads the stream as veraPDF does: a "]" there.
//
// ISO 32000-1, 7.3.6 closes an array with "]", so one outside every array
// begins no object and is no operator, and ISO 19005-1, 6.2.10 and ISO
// 19005-2, 6.2.2 constrain every operator of the stream, the ones after it
// included. Deliberate leniency toward veraPDF, the reference validator of ISO
// 19005, which reads no further: its content stream parser reads an operator
// of no characters from a "]" (PDFStreamParser.nextOperator, veraPDF-parser
// 1.30), which parseNextToken returns as the end of the stream, so no rule
// judges what follows. Measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b, the
// undefined operators, the colour spaces, the fonts, the resource names, the
// ExtGState and the form XObjects used after a stray "]" answer to no test,
// those used before it do, and a "]" in the first stream of a /Contents array
// hides the streams after it too. A parse without an option therefore stops
// there, and the operands read before it, which no operator takes, go with
// the rest. [WholeStream] reads on.
//
// # Corpus evidence
//
//   - All corpora: ending the reading at a stray "]" changes what compliance
//     reports on no file at PDF/A-1b and PDF/A-2b, veraPDF files at their own
//     profile.
func endsReading(tok lexer.Token) bool {
	return tok.Type == lexer.TokenDelimiter && tok.Value == "]"
}

// Option configures how [ParseStreamData], [ParseStreamDataWithOffsets] and
// [ParsePageContent] read a content stream.
type Option func(*readOptions)

// readOptions is what the [Option] values of a parse set.
type readOptions struct {
	// commands is the command set the lexer ends a run of regular characters
	// at, or nil for the reading of veraPDF (see [newLexer]).
	commands *lexer.CommandSet
	// repair reads the stream as a repair that writes it back reads it. See
	// [ForRepair].
	repair bool
	// whole reads every operator of the stream. See [WholeStream].
	whole bool
}

// readsAsVeraPDF reports whether a parse under o reads the stream as veraPDF
// reads it, to its end and no further: a parse without an option. See
// [newLexer], [endsReading] and [inlineImages].
func (o readOptions) readsAsVeraPDF() bool {
	return o.commands == nil && !o.repair && !o.whole
}

// optionsOf returns what opts set, for a parse under ctx: a context from
// [WithWholeStreams] sets [WholeStream].
func optionsOf(ctx context.Context, opts []Option) readOptions {
	var o readOptions
	for _, opt := range opts {
		opt(&o)
	}
	if whole, _ := ctx.Value(wholeStreamsKey{}).(bool); whole {
		o.whole = true
	}
	return o
}

// wholeStreamsKey is the context key [WithWholeStreams] sets.
type wholeStreamsKey struct{}

// WithWholeStreams returns a copy of ctx under which every parse reads a
// stream whole, as [WholeStream] reads it: a parse without an option then
// reads past the stray "]" where it ends and through the arrays and
// dictionaries it gives up on the stream for.
//
// It is for the remediation, whose repairs answer the findings of a
// validation run: ISO 19005 judges what veraPDF reads, but a remediation that
// took veraPDF's reading would leave unrepaired the operators a viewer draws
// past those points, which veraPDF judges once a repair that rewrites the
// stream has dropped what ended its reading there. A context carries it so
// that the validation run and every repair under it read the same streams.
func WithWholeStreams(ctx context.Context) context.Context {
	return context.WithValue(ctx, wholeStreamsKey{}, true)
}

// SplitGluedOperators reads operators written with no white space between
// them as PDF.js reads them: "ETBT" is ET then BT, and "q1" is q then the
// number 1. See [lexer.Lexer.WithCommands] for the rule and the leniency it
// takes with ISO 32000-1, 7.2.2 and 7.8.2, and [IsOperator] for the operators
// of Annex A, Table A.1 it splits at.
//
// It is for the readings that follow a viewer: text extraction, the widths of
// Type 3 glyphs, and the redaction that has to remove what a viewer shows where
// the viewer shows it. Without it a parse reads the stream as veraPDF does
// (PDFStreamParser): it keeps the token boundaries of 7.2.2 but ends an
// operator before a digit and a number before the first character no number
// holds, and runs an operator on through ")", ">", "{", "}" and "%"
// ([lexer.Lexer.WithVeraPDFTokens]), so compliance reads "ETBT" as the one
// operator ISO 19005 forbids, which veraPDF reports under ISO 19005-2, 6.2.2
// t1, and the remediation that answers it writes the operators back apart
// ([GluedOperators]). Both readings take "q1" as q then the number 1.
//
// It reads the data of inline images as PDF.js does too, and ends the parse
// where PDF.js stops reading the stream ([readInlineImage]).
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

// ForRepair reads a stream as a repair that writes it back reads it: as a
// parse without an option reads it, save where the reading of veraPDF departs
// from what a repair has to keep ([lexer.Lexer.WithRepairTokens]). A run that
// starts with a digit, a sign or a period keeps the token boundaries of ISO
// 32000-1, 7.2.2, and is a keyword when it spells no number of 7.3.3, "-" and
// ".020.594" included, where a parse without the option reads the largest
// value veraPDF gives it. And an operator ends at every delimiter of 7.2.2,
// where a parse without the option runs it on through ")", ">", "{", "}" and
// "%".
//
// The value veraPDF reads is no value a viewer draws, so writing it back would
// move what the page shows, and ending the run where veraPDF ends it hands the
// operator after it an operand a repair that removes the run would otherwise
// take with it. Kept whole, the run is written back as it was, removed with
// its number, or respelled as the number a viewer reads in it. An operator
// veraPDF runs on through a delimiter, "T)Tj", holds operators a viewer reads,
// the Tj here, which a repair that removed the run would take with it. Under
// [SplitGluedOperators] it changes nothing.
//
// # Corpus evidence
//
//   - pdfjs poppler-90-0-fuzzed.pdf: page 14 writes "55.8492", fuzzed bytes
//     and "7136 0 Td". Read as veraPDF reads it, removing the fuzzed bytes as
//     an undefined operator took 55.8492 with them and moved the text after
//     the Td 7136 units right, and the extracted text of the PDF/A-1b
//     remediation dropped from 1079 to 72 bytes. The real-limits repair,
//     which clamped a malformed number to the limit and wrote its page back,
//     kept that drop past the rollback of the content repairs.
func ForRepair() Option {
	return func(o *readOptions) {
		o.repair = true
	}
}

// WholeStream reads a stream as a parse without an option reads it, save that
// it reads every operator the stream holds: past the stray "]" where that
// parse ends ([endsReading]), through the arrays and dictionaries it gives up
// on the stream for ([parser.Parser.WithVeraPDFContainers]), and every inline
// image, whose data it ends as [ForRepair] does
// ([parser.Parser.ParseInlineImageDict]) rather than as veraPDF does, which
// reads an image that follows another as part of it ([inlineImages]).
//
// It is for the readings that act on what a viewer draws rather than on what
// veraPDF judges, such as the tagging that marks every piece of a page's
// content (ISO 32000-1, 14.8.2.2), and the repair that gives a form XObject
// every resource it names. A viewer reads on past a stray "]": PDF.js drops
// the token and keeps reading (EvaluatorPreprocessor.read). [ForRepair] and
// [SplitGluedOperators] read the whole stream too.
func WholeStream() Option {
	return func(o *readOptions) {
		o.whole = true
	}
}

// GluedOperators returns the operators [SplitGluedOperators] reads keyword as
// when it reads two or more operators of ISO 32000-1, Annex A, Table A.1 and
// nothing else, and nil otherwise: "ETBT" gives ET and BT, while "ETXX", whose
// XX is no operator, and "ET" itself give nil.
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

// newParser returns a parser over data that reads it as o says: with the lexer
// [newLexer] returns, and, with veraPDF's tokens, noting where veraPDF gives up
// on the stream ([parser.Parser.WithVeraPDFContainers]). A parse without an
// option answers that with no operation at all ([gaveUp]), and [WholeStream],
// which reads the same containers, reads on.
//
// # Corpus evidence
//
//   - All corpora: giving no operation for a stream veraPDF gives up on
//     changes what compliance reports on 4 files at PDF/A-1b and PDF/A-2b,
//     veraPDF files at their own profile, all of them pdfjs, none with a
//     veraPDF golden. Against veraPDF 1.30 run on each, no rule count moves
//     away from veraPDF's.
//   - pdfjs issue11678.pdf: page 2 loses its 4 undefined operators, its 3 and
//     4 hex string findings, its missing glyph and its DeviceGray, none of
//     which veraPDF reports on that page.
//   - pdfjs operator-in-TJ-array.pdf: writes an operator inside a TJ array,
//     and loses the font that is not embedded and the DeviceGray, which
//     veraPDF reports neither of.
//   - pdfjs poppler-90-0-fuzzed.pdf: pages 8, 11 and 15 lose their undefined
//     operators, and the file goes from 97 to 89 against veraPDF's 87. The 2
//     left sit on pages 9 and 10, whose streams veraPDF cannot decode.
//   - pdfjs bug1130815.pdf: its damaged page loses every finding, 153
//     undefined operators and the integer, real and q/Q nesting limits among
//     them. veraPDF cannot open the file.
func newParser(data []byte, o readOptions) *parser.Parser {
	p := parser.NewParser(newLexer(data, o))
	if o.commands == nil && !o.repair {
		p.WithVeraPDFContainers()
	}
	return p
}

// gaveUp reports whether a parse under o gives no operation for the stream p
// has read: read as veraPDF reads it, the stream holds a container veraPDF
// cannot read.
func gaveUp(p *parser.Parser, o readOptions) bool {
	return o.readsAsVeraPDF() && p.Abandoned()
}

// newLexer returns a lexer over data that reads it as o says. With no option
// it reads the stream as veraPDF does ([lexer.Lexer.WithVeraPDFTokens]): that
// is the reading ISO 19005 validation takes, so "q1" is the q veraPDF counts
// toward the nesting limit rather than an operator it never reports, "12m" is
// 12 then m, ".020.594" is the largest real veraPDF gives a number, which fails
// the real limit, "re%" before a comment is one undefined operator, and so is
// a stray ")". [ForRepair] keeps what a repair writes back where that reading
// departs from it, and [SplitGluedOperators] replaces it with the reading of
// PDF.js, which splits "q1" too. [WholeStream] keeps the tokens and only reads
// on where the parse would end ([endsReading]).
//
// # Corpus evidence
//
//   - All corpora: ending an operator before a digit changes what compliance
//     reports on 4 files at PDF/A-1b and PDF/A-2b, veraPDF files at their own
//     profile, all of them pdfjs.
//   - pdfjs issue6342.pdf: its undefined operators go from 27 to 19 when an
//     operator ends before a digit, and to 1, veraPDF's c., when a number ends
//     before the first character no number holds. Its malformed numbers, such
//     as ".020.594", then fail the real limit, which it fails 14 times at
//     PDF/A-1b and 12 at PDF/A-2b, as veraPDF reports it.
//   - All corpora: reading numbers as veraPDF does changes what compliance
//     reports on 10 files at PDF/A-1b and PDF/A-2b, veraPDF files at their own
//     profile, all of them pdfjs. On the 7 veraPDF opens, no rule count moves
//     away from veraPDF's.
//   - pdfjs poppler-90-0-fuzzed.pdf: on the 4 pages veraPDF reports undefined
//     operators for, litho's go from 2, 18, 3 and 23 to veraPDF's 2, 24, 3 and
//     58, 74 of them the same operators, the others differing at ")", ">" and
//     "%", which veraPDF reads inside an operator. Page 4 loses its one and
//     page 15 gains one, pages veraPDF reports none for.
//   - pdfjs issue15604.pdf and bug1753983.pdf: "-." and "-" read as the largest
//     values veraPDF gives a number, and fail the real and integer limits as
//     veraPDF reports them.
//   - pdfjs sci-notation.pdf: the exponent of "1e2 Tf" is the undefined
//     operator e veraPDF names, where the format check stated it.
//   - pdfjs bug1130815.pdf: its undefined operators go from 1548 to 153, and
//     its damaged stream's malformed numbers fail the integer and real limits.
//     A k written against its operand now selects a colour space before the
//     page shows text, and the initial DeviceGray ISO 19005-1, 6.2.3.3 t3 and
//     ISO 19005-2, 6.2.4.3 t4 stated goes. veraPDF cannot open the file.
//   - pdfjs bug1130815.pdf: its default remediation keeps the q operators it
//     writes against a digit, which the undefined-operator repair removed, and
//     veraPDF fails the output on the q/Q nesting limit.
//   - pdfjs issue11678.pdf: "J0.15560A38" reads as J, a number and the
//     undefined A. veraPDF reports no undefined operator in the file.
//   - All corpora: running an operator on through ")", ">", "{", "}" and "%"
//     changes what compliance reports on 4 files at PDF/A-1b and PDF/A-2b,
//     veraPDF files at their own profile, all of them pdfjs.
//   - pdfjs calgray.pdf and calrgb.pdf: write "re" against a comment, and
//     their undefined operators go from 0 to veraPDF's 60 and 1360, "re%"
//     each time.
//   - pdfjs poppler-90-0-fuzzed.pdf: on the 4 pages veraPDF reports undefined
//     operators for, litho now reports the 87 veraPDF reports, operator for
//     operator. Page 8, which veraPDF reports none for, goes from 5 to 2.
//   - pdfjs issue15893_reduced.pdf: two undefined operators around a ")"
//     read as one. veraPDF cannot open the file.
func newLexer(data []byte, o readOptions) *lexer.Lexer {
	lex := lexer.NewLexer(ioat.NewReader(bytes.NewReader(data), int64(len(data)), 0))
	switch {
	case o.commands != nil:
		lex.WithCommands(o.commands)
	case o.repair:
		lex.WithVeraPDFTokens().WithRepairTokens()
	default:
		lex.WithVeraPDFTokens()
	}
	return lex
}

// ParseStreamData parses raw content stream bytes into a sequence of [Operation] values.
// The parser checks ctx at regular intervals, allowing large or pathological
// content streams to be interrupted by deadline. opts choose how it reads the
// stream, see [SplitGluedOperators]. With no option it reads the stream as
// veraPDF does ([newLexer]), ends where veraPDF ends ([endsReading]), gives no
// operation for a stream veraPDF gives up on ([newParser]), and reads the data
// of inline images as veraPDF does ([inlineImages]).
func ParseStreamData(ctx context.Context, data []byte, opts ...Option) ([]Operation, error) {
	o := optionsOf(ctx, opts)
	p := newParser(data, o)
	return parseOperations(ctx, p, o, data, nil, nil)
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
	o := optionsOf(ctx, opts)
	p := newParser(data, o).WithOffsetTracking()
	ranges := make([]ByteRange, 0, 256) //nolint:mnd // matches the operations capacity hint
	ops, err := parseOperations(ctx, p, o, data, &ranges, nil)
	if err != nil || gaveUp(p, o) {
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
	o := optionsOf(ctx, nil)
	p := newParser(data, o).WithFormatChecking()
	ops, _ := parseOperations(ctx, p, o, data, nil, nil)
	if gaveUp(p, o) {
		return nil, nil
	}
	return ops, p.FormatViolations
}

// ParseStreamDataWithOverflows parses content stream data like
// [ParseStreamData] and also returns the value of every integer literal the
// parser read as a real because it overflows 64 bits (see
// [parser.Parser.IntegerOverflows]).
func ParseStreamDataWithOverflows(ctx context.Context, data []byte) ([]Operation, []float64, error) {
	o := optionsOf(ctx, nil)
	p := newParser(data, o)
	ops, err := parseOperations(ctx, p, o, data, nil, nil)
	if gaveUp(p, o) {
		return nil, nil, nil
	}
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
// the failure keep their placement, and the rest have no operator. A stream
// veraPDF gives up on ([newParser]) gives nothing at all.
func ParseStreamDataForCompliance(ctx context.Context, data []byte) ([]Operation, []float64, []FormatViolation, error) {
	o := optionsOf(ctx, nil)
	p := newParser(data, o).WithFormatChecking()
	placer := &violationPlacer{p: p}
	ops, err := parseOperations(ctx, p, o, data, nil, placer)
	if gaveUp(p, o) {
		return nil, nil, nil, nil
	}
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
	// Key is the key of the entry of Operand, a dictionary, whose value is the
	// string holding the violation, or "" when the violation lies deeper in
	// Operand or Operand is no dictionary.
	Key ast.PdfName
}

// noOperand is the operand index of a violation that lies in no operand an
// operation keeps.
const noOperand = -1

// operandEntryDepth is the depth the parser reads a value of an operand
// dictionary at (see [parser.EntryViolationError]): parseOperations reads
// every operand as an object on its own, at depth 1, and
// [parser.Parser.ParseInlineImageDict] reads the values of an inline image
// dictionary one level down, as a dictionary's values are.
const operandEntryDepth = 2

// operandEntryKey returns the key of the operand dictionary entry whose value
// is the string holding err, or "" when err names none.
func operandEntryKey(err error) ast.PdfName {
	if ev, ok := errors.AsType[*parser.EntryViolationError](err); ok && ev.Depth == operandEntryDepth {
		return ev.Key
	}
	return ""
}

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
			if _, isDict := v.Operand.(ast.PdfDict); isDict {
				v.Key = operandEntryKey(pv.err)
			}
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

	o := optionsOf(ctx, opts)
	p := newParser(allData, o)
	ops, err := parseOperations(ctx, p, o, allData, nil, nil)
	if err != nil {
		return nil, err
	}

	doc.Logger.Debug("content stream parsed",
		slog.Int("operators", len(ops)),
	)

	return ops, nil
}
