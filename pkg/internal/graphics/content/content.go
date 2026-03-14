package content

import (
	"bytes"
	"fmt"
	"io"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/ioat"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
	"github.com/gotenberg/litho/pkg/internal/core/parser"
	"github.com/gotenberg/litho/pkg/internal/filter"
)

// Operation represents a drawing or state operation in a PDF content stream.
type Operation struct {
	Operator string
	Operands []ast.PdfObject
}

// resolveObject is a helper to resolve indirect references.
func resolveObject(doc *document.Document, obj ast.PdfObject) (ast.PdfObject, error) {
	if ref, ok := obj.(ast.PdfIndirectRef); ok {
		return doc.Resolve(ref)
	}
	return obj, nil
}

// parseOperations is the shared tokenization loop used by both ParseStreamData
// and ParsePageContent. It reads tokens from the parser, collecting operands
// and emitting operations when an operator keyword is encountered. Inline
// images (BI/ID/EI) are detected and safely skipped.
func parseOperations(p *parser.Parser) ([]Operation, error) {
	var ops []Operation
	var operands []ast.PdfObject

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
				if err := p.SkipInlineImage(); err != nil {
					return nil, fmt.Errorf("inline image: %w", err)
				}
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
				return nil, fmt.Errorf("failed to parse operand: %v (token: %v)", err, tok)
			}
			operands = append(operands, obj)
		}
	}

	return ops, nil
}

// ParseStreamData parses raw content stream bytes into a sequence of operations.
// This is used for parsing individual glyph procedures (CharProcs) in Type 3 fonts.
func ParseStreamData(data []byte) ([]Operation, error) {
	lex := lexer.NewLexer(ioat.NewReader(bytes.NewReader(data), int64(len(data)), 0))
	p := parser.NewParser(lex)
	return parseOperations(p)
}

// ParsePageContent extracts, concatenates, and parses the content streams of a page or form XObject.
func ParsePageContent(doc *document.Document, node ast.PdfObject) ([]Operation, error) {
	var streams []ast.PdfStream

	switch v := node.(type) {
	case ast.PdfDict:
		contentsObj, ok := v["Contents"]
		if !ok {
			return nil, nil // Empty page
		}

		contentsResolved, err := resolveObject(doc, contentsObj)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve /Contents: %v", err)
		}

		switch c := contentsResolved.(type) {
		case ast.PdfStream:
			streams = append(streams, c)
		case ast.PdfArray:
			for _, item := range c {
				itemResolved, err := resolveObject(doc, item)
				if err != nil {
					return nil, fmt.Errorf("failed to resolve item in /Contents array: %v", err)
				}
				stream, ok := itemResolved.(ast.PdfStream)
				if !ok {
					return nil, fmt.Errorf("expected stream in /Contents array, got %T", itemResolved)
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
		data, err := filter.DecodeStream(s)
		if err != nil {
			return nil, fmt.Errorf("failed to decode content stream: %v", err)
		}
		allData = append(allData, data...)
		allData = append(allData, '\n') // Ensure tokens from separate streams don't merge
	}

	lex := lexer.NewLexer(ioat.NewReader(bytes.NewReader(allData), int64(len(allData)), 0))
	p := parser.NewParser(lex)
	return parseOperations(p)
}
