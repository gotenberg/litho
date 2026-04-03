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
func parseOperations(p *parser.Parser) ([]Operation, error) {
	ops := make([]Operation, 0, 256)        //nolint:mnd // reasonable initial capacity for typical pages
	operands := make([]ast.PdfObject, 0, 8) //nolint:mnd // most operators have ≤6 operands

	for {
		tok, err := p.Peek(0)
		if err != nil && err != io.EOF && tok.Type == lexer.TokenError {
			return nil, fmt.Errorf("lexer error: %v", err)
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
				dict, err := p.ParseInlineImageDict()
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
			obj, err := p.ParseObject()
			if err != nil {
				// Tolerate malformed operands (stray delimiters like ']',
				// broken references, etc.) by consuming the bad token and
				// continuing. Lenient parsers skip unrecognised tokens in content
				// streams rather than aborting.
				if _, consumeErr := p.Consume(); consumeErr != nil {
					return nil, fmt.Errorf("failed to parse operand: %v (token: %v)", err, tok)
				}
				continue
			}
			operands = append(operands, obj)
		}
	}

	return ops, nil
}

// ParseStreamData parses raw content stream bytes into a sequence of [Operation] values.
// This is used for parsing individual glyph procedures (CharProcs) in Type 3 fonts.
func ParseStreamData(data []byte) ([]Operation, error) {
	lex := lexer.NewLexer(ioat.NewReader(bytes.NewReader(data), int64(len(data)), 0))
	p := parser.NewParser(lex)
	return parseOperations(p)
}

// ParseStreamDataFormatChecked parses content stream data with PDF/A format
// checking enabled. Returns [Operation] values and any hex string format
// violations detected by the parser.
func ParseStreamDataFormatChecked(data []byte) ([]Operation, []error) {
	lex := lexer.NewLexer(ioat.NewReader(bytes.NewReader(data), int64(len(data)), 0))
	p := parser.NewParser(lex).WithFormatChecking()
	ops, _ := parseOperations(p)
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
			return nil, fmt.Errorf("failed to resolve /Contents: %v", err)
		}

		switch c := contentsResolved.(type) {
		case ast.PdfStream:
			streams = append(streams, c)
		case ast.PdfArray:
			for _, item := range c {
				stream, err := document.ResolveStream(ctx, doc, item)
				if err != nil {
					return nil, fmt.Errorf("failed to resolve item in /Contents array: %v", err)
				}
				if stream.Dict == nil {
					return nil, fmt.Errorf("expected stream in /Contents array")
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

	var allData []byte
	for _, s := range streams {
		data, err := doc.DecodeStream(s)
		if err != nil {
			return nil, fmt.Errorf("failed to decode content stream: %v", err)
		}
		allData = append(allData, data...)
		allData = append(allData, '\n') // Ensure tokens from separate streams don't merge
	}

	doc.Logger.Debug("content stream decoded",
		slog.Int("streams", len(streams)),
		slog.Int("total_bytes", len(allData)),
	)

	lex := lexer.NewLexer(ioat.NewReader(bytes.NewReader(allData), int64(len(allData)), 0))
	p := parser.NewParser(lex).WithContext(ctx)
	ops, err := parseOperations(p)
	if err != nil {
		return nil, err
	}

	doc.Logger.Debug("content stream parsed",
		slog.Int("operators", len(ops)),
	)

	return ops, nil
}
