package document

import (
	"bytes"
	"fmt"
	"io"
	"maps"
	"strconv"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/ioat"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
	"github.com/gotenberg/litho/pkg/internal/core/parser"
	"github.com/gotenberg/litho/pkg/internal/core/xref"
)

func findStartXRef(r io.ReaderAt, size int64) (int64, error) {
	bufSize := min(size, int64(1024))

	buf := make([]byte, bufSize)
	_, err := r.ReadAt(buf, size-bufSize)
	if err != nil {
		return 0, fmt.Errorf("reading tail bytes for startxref: %w", err)
	}

	idx := bytes.LastIndex(buf, []byte("startxref"))
	if idx == -1 {
		return 0, fmt.Errorf("startxref not found")
	}

	sub := buf[idx+len("startxref"):]
	// bytes.Reader actually implements io.ReaderAt
	br := bytes.NewReader(sub)
	lex := lexer.NewLexer(ioat.NewReader(br, int64(len(sub)), 0))
	tok, err := lex.NextToken()
	if err != nil {
		return 0, fmt.Errorf("scanning token after startxref: %w", err)
	}
	if tok.Type != lexer.TokenNumber {
		return 0, fmt.Errorf("expected offset after startxref, got %v", tok.Value)
	}

	offset, err := strconv.ParseInt(tok.Value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parsing startxref offset value %q: %w", tok.Value, err)
	}

	return offset, nil
}

// parseXRefChain locates the startxref pointer and walks the chain of xref
// tables/streams (via /Prev links) to populate d.Index.
func (d *Document) parseXRefChain() error {
	currentOffset, err := findStartXRef(d.r, d.size)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDamagedXRef, err)
	}

	for currentOffset != 0 {
		lex := lexer.NewLexer(ioat.NewReader(d.r, d.size, currentOffset))
		tok, err := lex.NextToken()
		if err != nil {
			return fmt.Errorf("reading token at xref offset %d: %w", currentOffset, err)
		}

		var prev int64
		if tok.Type == lexer.TokenKeyword && tok.Value == "xref" {
			lex = lexer.NewLexer(ioat.NewReader(d.r, d.size, currentOffset))
			trailerDict, p, err := xref.ParseXRefTable(lex, d.Index.Entries)
			if err != nil {
				return fmt.Errorf("parsing xref table at offset %d: %w", currentOffset, err)
			}
			prev = p
			if d.Index.Trailer == nil {
				d.Index.Trailer = trailerDict
			}

			// Hybrid-Reference (PDF 1.5): the trailer may contain /XRefStm
			// pointing to a compressed XRef stream whose entries override
			// the legacy plain-text table entries.
			if xRefStmObj, ok := trailerDict["XRefStm"]; ok {
				if xRefStmOffset, ok := xRefStmObj.(ast.PdfInt); ok {
					if err := d.parseHybridXRefStream(int64(xRefStmOffset)); err != nil {
						return fmt.Errorf("hybrid /XRefStm: %w", err)
					}
				}
			}
		} else if tok.Type == lexer.TokenNumber {
			lex = lexer.NewLexer(ioat.NewReader(d.r, d.size, currentOffset))
			p := parser.NewParser(lex)
			obj, err := p.ParseObject()
			if err != nil {
				return fmt.Errorf("parsing xref stream object at offset %d: %w", currentOffset, err)
			}
			stream, ok := obj.(ast.PdfStream)
			if !ok {
				return fmt.Errorf("expected xref stream, got %T", obj)
			}
			pVal, err := xref.ParseXRefStream(stream, d.Index.Entries)
			if err != nil {
				return fmt.Errorf("parsing xref stream at offset %d: %w", currentOffset, err)
			}
			prev = pVal
			if d.Index.Trailer == nil {
				d.Index.Trailer = stream.Dict
			}
		} else {
			return fmt.Errorf("unexpected token at xref offset: %v", tok.Value)
		}

		currentOffset = prev
	}

	return nil
}

// parseHybridXRefStream parses a compressed XRef stream embedded in a hybrid-
// reference PDF. Entries from the compressed stream override any corresponding
// entries from the legacy plain-text table.
func (d *Document) parseHybridXRefStream(offset int64) error {
	lex := lexer.NewLexer(ioat.NewReader(d.r, d.size, offset))
	p := parser.NewParser(lex)
	obj, err := p.ParseObject()
	if err != nil {
		return fmt.Errorf("parsing hybrid xref stream object at offset %d: %w", offset, err)
	}

	stream, ok := obj.(ast.PdfStream)
	if !ok {
		return fmt.Errorf("expected xref stream at /XRefStm offset %d, got %T", offset, obj)
	}

	// Parse into a temporary map so entries override the legacy table.
	hybridEntries := make(map[int]xref.XRefEntry)
	if _, err := xref.ParseXRefStream(stream, hybridEntries); err != nil {
		return fmt.Errorf("parsing hybrid xref stream entries at offset %d: %w", offset, err)
	}

	// Merge: hybrid entries override legacy entries.
	maps.Copy(d.Index.Entries, hybridEntries)

	return nil
}
