package cmap

import (
	"bytes"
	"unicode/utf16"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/ioat"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
	"github.com/gotenberg/litho/pkg/internal/core/parser"
)

// CMap is a parsed ToUnicode character map that translates character codes
// (as raw byte sequences) to their corresponding Unicode strings.
type CMap struct {
	Mapping map[string]string
}

func decodeUTF16BE(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	if len(data) == 1 {
		return string(data)
	}
	if len(data)%2 != 0 {
		data = append(data, 0)
	}
	u16s := make([]uint16, len(data)/2)
	for i := range u16s {
		u16s[i] = uint16(data[i*2])<<8 | uint16(data[i*2+1])
	}
	return string(utf16.Decode(u16s))
}

func incrementBytes(b []byte) {
	for i := len(b) - 1; i >= 0; i-- {
		b[i]++
		if b[i] != 0 {
			break
		}
	}
}

// Parse extracts mapping data from a CMap stream payload.
func Parse(data []byte) (*CMap, error) {
	lex := lexer.NewLexer(ioat.NewReader(bytes.NewReader(data), int64(len(data)), 0))
	p := parser.NewParser(lex)

	c := &CMap{Mapping: make(map[string]string)}

	for {
		tok, err := p.Peek(0)
		if err != nil || tok.Type == lexer.TokenEOF {
			break
		}

		if tok.Type == lexer.TokenKeyword && tok.Value == "beginbfchar" {
			if _, err := p.Consume(); err != nil {
				return nil, err
			}
			for {
				t, err := p.Peek(0)
				if err != nil || t.Type == lexer.TokenEOF || (t.Type == lexer.TokenKeyword && t.Value == "endbfchar") {
					if t.Value == "endbfchar" {
						if _, err := p.Consume(); err != nil {
							return nil, err
						}
					}
					break
				}

				srcObj, err := p.ParseObject()
				if err != nil {
					break
				}
				dstObj, err := p.ParseObject()
				if err != nil {
					break
				}

				srcStr, ok1 := srcObj.(ast.PdfString)
				dstStr, ok2 := dstObj.(ast.PdfString)
				if ok1 && ok2 {
					c.Mapping[string(srcStr)] = decodeUTF16BE([]byte(dstStr))
				}
			}
		} else if tok.Type == lexer.TokenKeyword && tok.Value == "beginbfrange" {
			if _, err := p.Consume(); err != nil {
				return nil, err
			}
			for {
				t, err := p.Peek(0)
				if err != nil || t.Type == lexer.TokenEOF || (t.Type == lexer.TokenKeyword && t.Value == "endbfrange") {
					if t.Value == "endbfrange" {
						if _, err := p.Consume(); err != nil {
							return nil, err
						}
					}
					break
				}

				startObj, err := p.ParseObject()
				if err != nil {
					break
				}
				endObj, err := p.ParseObject()
				if err != nil {
					break
				}
				dstObj, err := p.ParseObject()
				if err != nil {
					break
				}

				startStr, ok1 := startObj.(ast.PdfString)
				endStr, ok2 := endObj.(ast.PdfString)

				if ok1 && ok2 {
					if dstStr, ok3 := dstObj.(ast.PdfString); ok3 {
						startBytes := []byte(startStr)
						endBytes := []byte(endStr)
						dstBytes := []byte(dstStr)

						if len(startBytes) > 0 && len(startBytes) == len(endBytes) {
							current := make([]byte, len(startBytes))
							copy(current, startBytes)

							currDst := make([]byte, len(dstBytes))
							copy(currDst, dstBytes)

							for bytes.Compare(current, endBytes) <= 0 {
								c.Mapping[string(current)] = decodeUTF16BE(currDst)
								incrementBytes(current)
								incrementBytes(currDst)
							}
						}
					} else if dstArr, ok3 := dstObj.(ast.PdfArray); ok3 {
						startBytes := []byte(startStr)
						endBytes := []byte(endStr)

						if len(startBytes) > 0 && len(startBytes) == len(endBytes) {
							current := make([]byte, len(startBytes))
							copy(current, startBytes)

							arrIdx := 0
							for bytes.Compare(current, endBytes) <= 0 && arrIdx < len(dstArr) {
								if dStr, ok4 := dstArr[arrIdx].(ast.PdfString); ok4 {
									c.Mapping[string(current)] = decodeUTF16BE([]byte(dStr))
								}
								incrementBytes(current)
								arrIdx++
							}
						}
					}
				}
			}
		} else {
			if _, err := p.Consume(); err != nil {
				return nil, err
			}
		}
	}

	return c, nil
}

// Map parses a chunk of bytes using longest prefix match up to 4 bytes.
func (c *CMap) Map(data []byte) (int, string, bool) {
	maxLen := min(len(data), 4)

	for i := maxLen; i > 0; i-- {
		if val, ok := c.Mapping[string(data[:i])]; ok {
			return i, val, true
		}
	}

	return 0, "", false
}
