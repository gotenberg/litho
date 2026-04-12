package cmap

import (
	"bytes"
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/ioat"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
	"github.com/gotenberg/litho/pkg/internal/core/parser"
)

// CIDCMap maps character codes (integers) to CIDs (integers).
// This is the encoding CMap for Type0 (composite) fonts, specified by the
// /Encoding entry in the Type0 font dictionary. It maps byte sequences from
// the content stream to CID values used for width lookup in the /W array.
type CIDCMap struct {
	Mapping map[int]int
}

// strToInt converts a PDF hex string (byte sequence) to an integer by
// treating each byte as a big-endian digit (b[0]<<8 | b[1] for 2 bytes, etc.).
func strToInt(s ast.PdfString) int {
	b := []byte(s)
	result := 0
	for _, v := range b {
		result = result<<8 | int(v)
	}
	return result
}

// ParseCID extracts CID mapping data from a CMap stream payload.
// It handles begincidchar/endcidchar and begincidrange/endcidrange sections.
func ParseCID(ctx context.Context, data []byte) (*CIDCMap, error) {
	lex := lexer.NewLexer(ioat.NewReader(bytes.NewReader(data), int64(len(data)), 0))
	p := parser.NewParser(lex)

	c := &CIDCMap{Mapping: make(map[int]int)}

	for {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("parsing CID CMap: %w", err)
		}
		tok, err := p.Peek(0)
		if err != nil || tok.Type == lexer.TokenEOF {
			break
		}

		switch {
		case tok.Type == lexer.TokenKeyword && tok.Value == "begincidchar":
			if _, err := p.Consume(); err != nil {
				return nil, err
			}
			for {
				t, err := p.Peek(0)
				if err != nil || t.Type == lexer.TokenEOF || (t.Type == lexer.TokenKeyword && t.Value == "endcidchar") {
					if t.Value == "endcidchar" {
						if _, err := p.Consume(); err != nil {
							return nil, err
						}
					}
					break
				}

				srcObj, err := p.ParseObject(ctx)
				if err != nil {
					break
				}
				dstObj, err := p.ParseObject(ctx)
				if err != nil {
					break
				}

				srcStr, ok1 := srcObj.(ast.PdfString)
				dstInt, ok2 := dstObj.(ast.PdfInt)
				if ok1 && ok2 {
					c.Mapping[strToInt(srcStr)] = int(dstInt)
				}
			}
		case tok.Type == lexer.TokenKeyword && tok.Value == "begincidrange":
			if _, err := p.Consume(); err != nil {
				return nil, err
			}
			for {
				t, err := p.Peek(0)
				if err != nil || t.Type == lexer.TokenEOF || (t.Type == lexer.TokenKeyword && t.Value == "endcidrange") {
					if t.Value == "endcidrange" {
						if _, err := p.Consume(); err != nil {
							return nil, err
						}
					}
					break
				}

				lowObj, err := p.ParseObject(ctx)
				if err != nil {
					break
				}
				highObj, err := p.ParseObject(ctx)
				if err != nil {
					break
				}
				dstObj, err := p.ParseObject(ctx)
				if err != nil {
					break
				}

				lowStr, ok1 := lowObj.(ast.PdfString)
				highStr, ok2 := highObj.(ast.PdfString)
				dstInt, ok3 := dstObj.(ast.PdfInt)
				if ok1 && ok2 && ok3 {
					low := strToInt(lowStr)
					high := strToInt(highStr)
					dst := int(dstInt)
					if low > high {
						continue
					}
					const maxRangeEntries = 1 << 16
					for i := 0; i <= high-low && i < maxRangeEntries; i++ {
						c.Mapping[low+i] = dst + i
					}
				}
			}
		default:
			if _, err := p.Consume(); err != nil {
				return nil, err
			}
		}
	}

	return c, nil
}
