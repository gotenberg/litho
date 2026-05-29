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
)

// Operation represents a drawing or state operation in a PDF content stream.
// Each Operation pairs an operator keyword with its [ast.PdfObject] operands.
type Operation struct {
	Operator string
	Operands []ast.PdfObject
}

// parseOperations is the shared tokenization loop used by both ParseStreamData
// and ParsePageContent. It reads tokens from the parser, collecting operands
// and emitting operations when an operator keyword is encountered. Inline
// images (BI/ID/EI) are detected and safely skipped.
func parseOperations(ctx context.Context, p *parser.Parser) ([]Operation, error) {
	ops := make([]Operation, 0, 256)        //nolint:mnd // reasonable initial capacity for typical pages
	operands := make([]ast.PdfObject, 0, 8) //nolint:mnd // most operators have ≤6 operands
	tokenCount := 0

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
				dict, err := p.ParseInlineImageDict(ctx)
				if err != nil {
					return nil, fmt.Errorf("inline image: %w", err)
				}
				ops = append(ops, Operation{
					Operator: "BI",
					Operands: []ast.PdfObject{dict},
				})
				operands = nil
				continue
			}

			ops = append(ops, Operation{
				Operator: tok.Value,
				Operands: operands,
			})
			operands = nil
			if _, err := p.Consume(); err != nil {
				return nil, fmt.Errorf("failed to consume operator token: %w", err)
			}
		} else {
			obj, err := p.ParseObject(ctx)
			if err != nil {
				// Tolerate malformed operands (stray delimiters like ']',
				// broken references, etc.) by consuming the bad token and
				// continuing. Lenient parsers skip unrecognised tokens in content
				// streams rather than aborting.
				if _, consumeErr := p.Consume(); consumeErr != nil {
					return nil, fmt.Errorf("failed to parse operand: %w (token: %v)", err, tok)
				}
				continue
			}
			operands = append(operands, obj)
		}
	}

	return ops, nil
}

// ParseStreamData parses raw content stream bytes into a sequence of [Operation] values.
// The parser checks ctx at regular intervals, allowing large or pathological
// content streams to be interrupted by deadline.
func ParseStreamData(ctx context.Context, data []byte) ([]Operation, error) {
	lex := lexer.NewLexer(ioat.NewReader(bytes.NewReader(data), int64(len(data)), 0))
	p := parser.NewParser(lex)
	return parseOperations(ctx, p)
}

// ParseStreamDataFormatChecked parses content stream data with PDF/A format
// checking enabled. Returns [Operation] values and any hex string format
// violations detected by the parser.
func ParseStreamDataFormatChecked(ctx context.Context, data []byte) ([]Operation, []error) {
	lex := lexer.NewLexer(ioat.NewReader(bytes.NewReader(data), int64(len(data)), 0))
	p := parser.NewParser(lex).WithFormatChecking()
	ops, _ := parseOperations(ctx, p)
	return ops, p.FormatViolations
}

// ParsePageContent extracts, concatenates, and parses the content streams of a page or form XObject.
func ParsePageContent(ctx context.Context, doc *document.Document, node ast.PdfObject) ([]Operation, error) {
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

	const maxContentStreamSize = 32 << 20 // 32 MB: no legitimate page needs more
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
		if len(allData) > maxContentStreamSize {
			return nil, fmt.Errorf("content stream exceeds %d MB limit", maxContentStreamSize>>20)
		}
	}

	doc.Logger.Debug("content stream decoded",
		slog.Int("streams", len(streams)),
		slog.Int("total_bytes", len(allData)),
	)

	lex := lexer.NewLexer(ioat.NewReader(bytes.NewReader(allData), int64(len(allData)), 0))
	p := parser.NewParser(lex)
	ops, err := parseOperations(ctx, p)
	if err != nil {
		return nil, err
	}

	doc.Logger.Debug("content stream parsed",
		slog.Int("operators", len(ops)),
	)

	return ops, nil
}
