package cmap

import (
	"bytes"
	"context"
	"fmt"
	"strings"

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

	// notdefs holds the program's notdefchar and notdefrange entries in the
	// order it defines them, and useCMaps the bases its usecmap operators
	// name; see [ParseEmbeddedCIDMap].
	notdefs  []notdefSpan
	useCMaps []useCMapCall

	// incomplete records a program holding more notdef entries than
	// [maxNotdefEntries] or more usecmap operators than [maxUseCMapOps], some
	// of whose mappings litho then does not hold.
	incomplete bool
}

// useCMapCall is one usecmap operator of a CMap program: the base it names, and
// how many notdef entries the program had defined when it ran.
type useCMapCall struct {
	name     string
	notdefAt int
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
// It handles begincidchar/endcidchar and begincidrange/endcidrange sections,
// and records the notdefchar and notdefrange sections and the usecmap
// operators [ParseEmbeddedCIDMap] resolves. A program whose cidrange entries
// expand past [maxCMapRangeCodes] fails with [ErrCMapTooLarge].
//
// An entry whose code is not one to four bytes wide or whose CID is negative
// or overflows maps nothing; see [codeInt] and [cidValue].
func ParseCID(ctx context.Context, data []byte) (*CIDCMap, error) {
	return parseCID(ctx, data, newParseBudget())
}

// parseCID is [ParseCID] under budget b.
func parseCID(ctx context.Context, data []byte, b *parseBudget) (*CIDCMap, error) {
	lex := lexer.NewLexer(ioat.NewReader(bytes.NewReader(data), int64(len(data)), 0))
	p := parser.NewParser(lex)

	c := &CIDCMap{Mapping: make(map[int]int)}
	// lastName is the name token most recently read outside a mapping block,
	// the operand a usecmap operator takes (ISO 32000-1, 9.7.5.4).
	lastName := ""

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
				if err := b.step(ctx); err != nil {
					return nil, err
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
				if !ok1 || !ok2 {
					continue
				}
				code, okCode := codeInt([]byte(srcStr))
				cid, okCID := cidValue(int64(dstInt), 0)
				if okCode && okCID {
					c.Mapping[code] = cid
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
				if err := b.step(ctx); err != nil {
					return nil, err
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
				if !ok1 || !ok2 || !ok3 {
					continue
				}
				low, okLow := codeInt([]byte(lowStr))
				high, okHigh := codeInt([]byte(highStr))
				if !okLow || !okHigh || low > high {
					continue
				}
				// One cidrange expands to at most maxRangeEntries codes, and
				// the program's ranges together to maxCMapRangeCodes.
				span := min(high-low, maxRangeEntries-1)
				dst, okCID := cidValue(int64(dstInt), span)
				if !okCID {
					continue
				}
				for i := 0; i <= span; i++ {
					if err := b.rangeCode(ctx); err != nil {
						return nil, err
					}
					c.Mapping[low+i] = dst + i
				}
			}
		case tok.Type == lexer.TokenKeyword && (tok.Value == "beginnotdefchar" || tok.Value == "beginnotdefrange"):
			if err := parseNotdefBlock(ctx, p, c, b, tok.Value == "beginnotdefrange"); err != nil {
				return nil, err
			}
		default:
			switch {
			case tok.Type == lexer.TokenName:
				lastName = strings.TrimPrefix(tok.Value, "/")
			case tok.Type == lexer.TokenKeyword && tok.Value == "usecmap" && lastName != "":
				// Each usecmap adds a base at the point the program runs it,
				// as veraPDF's CMapParser does (ISO 32000-1, 9.7.5.4).
				if len(c.useCMaps) < maxUseCMapOps {
					c.useCMaps = append(c.useCMaps, useCMapCall{name: lastName, notdefAt: len(c.notdefs)})
				} else {
					c.incomplete = true
				}
			}
			if _, err := p.Consume(); err != nil {
				return nil, err
			}
		}
	}

	return c, nil //nolint:nilerr // A lexer error ends the CMap, keeping the mappings read before it.
}

// parseNotdefBlock reads the entries of one notdefchar or notdefrange block,
// whose begin keyword p is positioned on, into c. A notdefchar entry maps one
// code and a notdefrange entry a span of codes, every one of them to the
// single CID the entry gives (Adobe Technical Note #5014, notdefrange): unlike a
// cidrange, the CID does not increment across the span.
func parseNotdefBlock(ctx context.Context, p *parser.Parser, c *CIDCMap, b *parseBudget, isRange bool) error {
	if _, err := p.Consume(); err != nil {
		return err
	}
	end := "endnotdefchar"
	if isRange {
		end = "endnotdefrange"
	}
	for {
		t, err := p.Peek(0)
		if err != nil || t.Type == lexer.TokenEOF {
			return nil //nolint:nilerr // A lexer error ends the block, as in the cidchar and cidrange blocks.
		}
		if t.Type == lexer.TokenKeyword && t.Value == end {
			_, err := p.Consume()
			return err
		}
		if err := b.step(ctx); err != nil {
			return err
		}
		lowObj, err := p.ParseObject(ctx)
		if err != nil {
			return nil //nolint:nilerr // A garbled entry ends the block, as in the cidchar and cidrange blocks.
		}
		highObj := lowObj
		if isRange {
			if highObj, err = p.ParseObject(ctx); err != nil {
				return nil //nolint:nilerr // A garbled entry ends the block, as in the cidchar and cidrange blocks.
			}
		}
		dstObj, err := p.ParseObject(ctx)
		if err != nil {
			return nil //nolint:nilerr // A garbled entry ends the block, as in the cidchar and cidrange blocks.
		}
		lowStr, ok1 := lowObj.(ast.PdfString)
		highStr, ok2 := highObj.(ast.PdfString)
		dstInt, ok3 := dstObj.(ast.PdfInt)
		if !ok1 || !ok2 || !ok3 {
			continue
		}
		low, okLow := codeInt([]byte(lowStr))
		high, okHigh := codeInt([]byte(highStr))
		cid, okCID := cidValue(int64(dstInt), 0)
		if !okLow || !okHigh || !okCID || low > high {
			continue
		}
		if len(c.notdefs) >= maxNotdefEntries {
			c.incomplete = true
			continue
		}
		c.notdefs = append(c.notdefs, notdefSpan{lo: low, hi: high, cid: cid})
	}
}

// ParseEmbeddedCIDMap builds a [CIDMap] from an embedded CMap stream: the
// cidchar/cidrange sections supply the code-to-CID mapping and the
// codespacerange sections supply the byte-length structure needed to decode
// content-stream strings. It is [ParseEmbeddedCIDMapOver] with no base named
// by the stream dictionary.
func ParseEmbeddedCIDMap(ctx context.Context, data []byte) *CIDMap {
	return ParseEmbeddedCIDMapOver(ctx, data, Base{})
}

// Base is the CMap a CMap stream dictionary's /UseCMap entry names (ISO
// 32000-1, 9.7.5.3, Table 120). The zero value names none.
type Base struct {
	// Map is the base, resolved; nil when the entry names none or names one
	// litho cannot resolve.
	Map *CIDMap
	// Unresolved records an entry naming a base litho cannot resolve, whose
	// mappings it then does not hold.
	Unresolved bool
}

// ParseEmbeddedCIDMapOver builds a [CIDMap] from an embedded CMap stream
// over the base its stream dictionary names. Returns nil when no codespace is
// declared, by the stream or by a base, so a malformed stream degrades to
// "encoding not decodable" instead of wrong CIDs.
//
// A code reads as veraPDF's CMap.toCID reads it: the stream's own cidchar and
// cidrange mappings come first, a later one overriding an earlier one, then
// the bases its usecmap operators name in program order, then the base named
// by the dictionary, which veraPDF (PDCMap.getCMapFile) adds once the program
// has run. Only then do the notdef mappings apply: the stream's own and its
// bases', in the order they were added, the first covering the code giving
// its CID (Adobe Technical Note #5014, notdefrange). A usecmap names a predefined
// CMap ([PredefinedCIDBase], ISO 32000-1, 9.7.5.4).
//
// A code nothing maps selects CID 0 (ISO 32000-1, 9.7.6.3), and a bfchar or
// bfrange section maps to Unicode, not to CIDs (9.10.3), so a CMap written
// with bfchar alone decodes every code to CID 0. When a base litho cannot
// resolve could still supply the code, it stays unknown instead; see
// [CIDMap.UnmappedNotdef].
//
// # Corpus evidence
//
//   - pdfjs bug920426.pdf: gives a Type 0 font an encoding CMap written with
//     bfchar alone, and veraPDF's CMap.toCID reads its codes as CID 0.
func ParseEmbeddedCIDMapOver(ctx context.Context, data []byte, dictBase Base) *CIDMap {
	cidc, err := ParseCID(ctx, data)
	if err != nil || cidc == nil {
		return nil
	}
	full, err := Parse(ctx, data)
	if err != nil || full == nil {
		return nil
	}
	m := &CIDMap{
		charToCID:      cidc.Mapping,
		cs:             &CMap{CodeSpaceRanges: full.CodeSpaceRanges},
		unmappedNotdef: !cidc.incomplete,
	}
	ownNotdefs := 0
	for _, op := range cidc.useCMaps {
		m.notdefs = append(m.notdefs, cidc.notdefs[ownNotdefs:op.notdefAt]...)
		ownNotdefs = op.notdefAt
		m.useBase(NamedBase(op.name))
	}
	m.notdefs = append(m.notdefs, cidc.notdefs[ownNotdefs:]...)
	m.useBase(dictBase)
	if len(m.notdefs) > maxNotdefEntries {
		m.notdefs = nil
		m.unmappedNotdef = false
	}
	if len(m.cs.CodeSpaceRanges) == 0 {
		return nil
	}
	if !m.unmappedNotdef && len(m.charToCID) == 0 && len(m.bases) == 0 && len(m.notdefs) == 0 {
		return nil
	}
	return m
}
