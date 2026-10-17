package cmap

import (
	"bytes"
	"context"
	"maps"
	"slices"
	"strings"
	"unicode/utf16"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/ioat"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
	"github.com/gotenberg/litho/pkg/internal/core/parser"
)

// maxRangeEntries caps how many codes one bfrange expands to. It is an
// anti-DoS guard, not spec-mandated: a two-byte range spans at most 65536
// codes, so no real ToUnicode CMap reaches it, while a crafted four-byte range
// would otherwise expand to billions of entries.
const maxRangeEntries = 1 << 16

// CodeSpaceRange defines a valid range of character codes with a specific byte length.
// For example, <00><FF> defines 1-byte codes, <0000><FFFF> defines 2-byte codes.
type CodeSpaceRange struct {
	Low  []byte // inclusive lower bound
	High []byte // inclusive upper bound
}

// CMap is a parsed ToUnicode character map that translates character codes
// (as raw byte sequences) to their corresponding Unicode strings.
//
// Mapping expands every bfrange in full, including one that crosses a
// first-byte boundary, because a viewer extracting text reads it that way.
// [CMap.StrictMapping] is the conforming view a validator judges against.
// A CMap is read-only once [Parse] returns and safe for concurrent reads.
type CMap struct {
	Mapping         map[string]string
	CodeSpaceRanges []CodeSpaceRange

	// strict is nil until a non-conforming bfrange appears, and from then on
	// holds only the mappings a conforming reader keeps.
	strict map[string]string

	// malformed records a token the CMap syntax cannot produce, which leaves
	// the whole CMap unusable to a validator even where entries still parsed.
	malformed bool

	// useCMap is the name the program's usecmap operator gives its base CMap,
	// and afterUse, allocated once the operator runs, holds the codes the
	// program maps after it; see [CMap.WithBase].
	useCMap  string
	afterUse map[string]bool
}

// StrictMapping returns the mapping without the codes that only a
// non-conforming bfrange supplies. Adobe Technical Note #5411, 1.4.1 rules
// that a bfrange's source codes shall not cross a first-byte boundary: a line
// such as <10FE> <1101> "will result in an invalid ToUnicode mapping file".
// veraPDF (CMapParser.getBfrangeEndFromBytes) keeps the start code's leading
// bytes and the end code's last byte, so <01ED> <0206> covers no code at all.
// Compliance checks that ask whether a glyph has a Unicode value use this
// view; text extraction keeps [CMap.Mapping].
func (c *CMap) StrictMapping() map[string]string {
	if c.strict != nil {
		return c.strict
	}
	return c.Mapping
}

// Malformed reports whether the CMap text carried a token no CMap syntax
// produces, such as the bare ">" a corrupted name leaves behind. Text
// extraction keeps reading [CMap.Mapping], which holds every entry that
// parsed. A compliance check asking whether a code has a Unicode value treats
// such a CMap as mapping nothing, which is how veraPDF judges it.
func (c *CMap) Malformed() bool {
	return c.malformed
}

// set records one mapping. A code only a non-conforming bfrange reaches stays
// out of the strict view, so an earlier conforming entry for it survives there.
func (c *CMap) set(code, uni string, conforming bool) {
	c.Mapping[code] = uni
	if c.strict != nil && conforming {
		c.strict[code] = uni
	}
	if c.afterUse != nil {
		c.afterUse[code] = true
	}
}

// beginNonConforming snapshots the mapping built so far as the strict view
// the first time a non-conforming bfrange appears.
func (c *CMap) beginNonConforming() {
	if c.strict == nil {
		c.strict = maps.Clone(c.Mapping)
	}
}

// conformingRangeEnd returns the last source code a bfrange covers once held
// to one first-byte block (Adobe Technical Note #5411, 1.4.1): the start
// code's leading bytes followed by the end code's last byte. start and end
// have the same non-zero length.
func conformingRangeEnd(start, end []byte) []byte {
	out := bytes.Clone(start)
	out[len(out)-1] = end[len(end)-1]
	return out
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
//
// # Corpus evidence
//
//   - pdfjs issue11651.pdf: its "/CMapName /Adobe-Identity-UC>" is a name
//     whose S became a ">", and veraPDF reports ISO 14289-1, 7.21.7 t1 on
//     every code the page shows, the codes its bfchar list maps included.
func Parse(ctx context.Context, data []byte) (*CMap, error) {
	lex := lexer.NewLexer(ioat.NewReader(bytes.NewReader(data), int64(len(data)), 0))
	p := parser.NewParser(lex)

	c := &CMap{Mapping: make(map[string]string)}
	// lastName is the name token most recently read outside a mapping block,
	// the operand a usecmap operator takes (ISO 32000-1, 9.7.5.4).
	lastName := ""

	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		tok, err := p.Peek(0)
		if err != nil || tok.Type == lexer.TokenEOF {
			break
		}

		switch {
		case tok.Type == lexer.TokenKeyword && tok.Value == "begincodespacerange":
			if _, err := p.Consume(); err != nil {
				return nil, err
			}
			for {
				t, err := p.Peek(0)
				if err != nil || t.Type == lexer.TokenEOF || (t.Type == lexer.TokenKeyword && t.Value == "endcodespacerange") {
					if t.Value == "endcodespacerange" {
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
				lowStr, ok1 := lowObj.(ast.PdfString)
				highStr, ok2 := highObj.(ast.PdfString)
				// Per ISO 32000-1, 9.7.6.2, a codespace range is one to
				// four bytes wide. An empty "<>" hex string declares no
				// range at all, and storing it as a zero-width entry
				// makes every later lookup consume no bytes. The bfrange
				// handler below applies the same floor.
				if ok1 && ok2 && len(lowStr) > 0 {
					c.CodeSpaceRanges = append(c.CodeSpaceRanges, CodeSpaceRange{
						Low:  []byte(lowStr),
						High: []byte(highStr),
					})
				}
			}

		case tok.Type == lexer.TokenKeyword && tok.Value == "beginbfchar":
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

				srcObj, err := p.ParseObject(ctx)
				if err != nil {
					break
				}
				dstObj, err := p.ParseObject(ctx)
				if err != nil {
					break
				}

				srcStr, ok1 := srcObj.(ast.PdfString)
				dstStr, ok2 := dstObj.(ast.PdfString)
				if ok1 && ok2 {
					c.set(string(srcStr), decodeUTF16BE([]byte(dstStr)), true)
				}
			}

		case tok.Type == lexer.TokenKeyword && tok.Value == "beginbfrange":
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

				startObj, err := p.ParseObject(ctx)
				if err != nil {
					break
				}
				endObj, err := p.ParseObject(ctx)
				if err != nil {
					break
				}
				dstObj, err := p.ParseObject(ctx)
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
							// Guard: skip malformed ranges where start > end.
							if bytes.Compare(startBytes, endBytes) > 0 {
								continue
							}
							strictEnd := conformingRangeEnd(startBytes, endBytes)
							if !bytes.Equal(strictEnd, endBytes) {
								c.beginNonConforming()
							}
							current := make([]byte, len(startBytes))
							copy(current, startBytes)

							currDst := make([]byte, len(dstBytes))
							copy(currDst, dstBytes)

							for n := 0; n < maxRangeEntries && bytes.Compare(current, endBytes) <= 0; n++ {
								if n%1000 == 0 && n > 0 {
									if err := ctx.Err(); err != nil {
										return nil, err
									}
								}
								c.set(string(current), decodeUTF16BE(currDst), bytes.Compare(current, strictEnd) <= 0)
								prev := make([]byte, len(current))
								copy(prev, current)
								incrementBytes(current)
								incrementBytes(currDst)
								// Stop if increment caused byte wrap-around
								// (e.g., FF→00 for 1-byte, FFFF→0000 for 2-byte).
								if bytes.Compare(current, prev) <= 0 {
									break
								}
							}
						}
					} else if dstArr, ok3 := dstObj.(ast.PdfArray); ok3 {
						startBytes := []byte(startStr)
						endBytes := []byte(endStr)

						if len(startBytes) > 0 && len(startBytes) == len(endBytes) {
							if bytes.Compare(startBytes, endBytes) > 0 {
								continue
							}
							current := make([]byte, len(startBytes))
							copy(current, startBytes)

							strictEnd := conformingRangeEnd(startBytes, endBytes)
							if !bytes.Equal(strictEnd, endBytes) {
								c.beginNonConforming()
							}

							arrIdx := 0
							for bytes.Compare(current, endBytes) <= 0 && arrIdx < len(dstArr) {
								if dStr, ok4 := dstArr[arrIdx].(ast.PdfString); ok4 {
									c.set(string(current), decodeUTF16BE([]byte(dStr)), bytes.Compare(current, strictEnd) <= 0)
								}
								incrementBytes(current)
								arrIdx++
							}
						}
					}
				}
			}

		default:
			// A bare ">" is a token no CMap syntax produces: the lexer returns
			// ">>" whole and a hex string consumes its own ">". It appears where
			// a name or string is corrupted. ISO 32000-1, 7.3.5 ends a name at
			// a delimiter, so what follows is valid nowhere, and veraPDF
			// discards the CMap whole, the codes its bfchar list maps
			// included.
			if tok.Type == lexer.TokenDelimiter && tok.Value == ">" {
				c.malformed = true
			}
			switch {
			case tok.Type == lexer.TokenName:
				lastName = strings.TrimPrefix(tok.Value, "/")
			case tok.Type == lexer.TokenKeyword && tok.Value == "usecmap" && lastName != "":
				// A second usecmap names a further base; the first is the one
				// litho resolves, and every code mapped after it overrides it.
				if c.useCMap == "" {
					c.useCMap = lastName
					c.afterUse = make(map[string]bool)
				}
			}
			if _, err := p.Consume(); err != nil {
				return nil, err
			}
		}
	}

	return c, nil //nolint:nilerr // A lexer error ends the CMap, keeping the mappings read before it.
}

// Map looks up a character code from the beginning of data and returns the
// number of bytes consumed, the mapped Unicode string, and whether a mapping
// was found.
//
// When codespace ranges are defined, they determine which byte lengths are
// valid. This is critical for multi-byte CMaps (CJK fonts) where a 2-byte
// code like 0x8140 must not be split into two 1-byte lookups.
//
// When no codespace ranges are defined, falls back to longest-prefix match
// (up to 4 bytes) for backwards compatibility.
//
// A reported match always consumes at least one byte. Callers walk a string
// by advancing the width Map returns, so a zero-width match would loop
// forever on the same code.
func (c *CMap) Map(data []byte) (int, string, bool) {
	if len(c.CodeSpaceRanges) > 0 {
		return c.mapWithCodeSpaces(data)
	}
	return c.mapLongestPrefix(data)
}

// mapWithCodeSpaces uses codespace ranges to determine valid byte lengths,
// then looks up the character code in the mapping. It tries each registered
// byte length (longest first) to find the most specific match.
func (c *CMap) mapWithCodeSpaces(data []byte) (int, string, bool) {
	// Collect distinct byte lengths from codespace ranges and try each,
	// preferring longer matches (more specific).
	lengths := c.codeLengths()

	for _, n := range slices.Backward(lengths) {
		// A malformed CMap can declare a zero-width codespace, which
		// would match the empty prefix of any string and report a
		// mapping that consumes nothing. CIDMap.next mirrors this
		// loop and guards the same case.
		if n == 0 || n > len(data) {
			continue
		}
		candidate := data[:n]
		if !c.inCodeSpace(candidate) {
			continue
		}
		if val, ok := c.Mapping[string(candidate)]; ok {
			return n, val, true
		}
		// Code is in codespace but has no mapping. Some malformed PDFs
		// declare a 2-byte codespace but populate bfchar entries with
		// 1-byte keys. Try shorter prefixes from the mapping before
		// consuming the full codespace width as unmapped.
		if n > 1 {
			for shorter := n - 1; shorter >= 1; shorter-- {
				if shorter > len(data) {
					continue
				}
				if val, ok := c.Mapping[string(data[:shorter])]; ok {
					return shorter, val, true
				}
			}
		}
		// Unmapped code. For a mixed-width CMap where single-byte entries
		// live inside a multi-byte codespace (malformed but common in
		// hand-authored PDFs), consuming the full codespace width would
		// swallow the next byte, which the mapping can likely decode on
		// its own. Advance by one instead so the caller re-aligns.
		if n > 1 && c.hasShortKeys() {
			return 1, "", false
		}
		return n, "", false
	}

	// No codespace matched. Fall back to longest prefix.
	return c.mapLongestPrefix(data)
}

// mapLongestPrefix tries longest prefix match up to 4 bytes.
func (c *CMap) mapLongestPrefix(data []byte) (int, string, bool) {
	maxLen := min(len(data), 4)

	for i := maxLen; i > 0; i-- {
		if val, ok := c.Mapping[string(data[:i])]; ok {
			return i, val, true
		}
	}

	return 0, "", false
}

// codeLengths returns the sorted distinct byte lengths from codespace ranges.
func (c *CMap) codeLengths() []int {
	seen := make(map[int]bool, 4)
	for _, cs := range c.CodeSpaceRanges {
		seen[len(cs.Low)] = true
	}
	lengths := make([]int, 0, len(seen))
	for n := range seen {
		lengths = append(lengths, n)
	}
	// Sort ascending.
	for i := range lengths {
		for j := i + 1; j < len(lengths); j++ {
			if lengths[j] < lengths[i] {
				lengths[i], lengths[j] = lengths[j], lengths[i]
			}
		}
	}
	return lengths
}

// hasShortKeys reports whether any mapping entry uses a single-byte source
// code. Used to detect mixed-width CMaps (1-byte bfchar entries inside a
// 2-byte codespace), where unmapped multi-byte codes should advance by one
// byte so mappable bytes are not inadvertently skipped.
func (c *CMap) hasShortKeys() bool {
	for k := range c.Mapping {
		if len(k) == 1 {
			return true
		}
	}
	return false
}

// inCodeSpace checks whether candidate bytes fall within any codespace range
// of the matching length.
func (c *CMap) inCodeSpace(candidate []byte) bool {
	n := len(candidate)
	for _, cs := range c.CodeSpaceRanges {
		if len(cs.Low) != n {
			continue
		}
		if bytes.Compare(candidate, cs.Low) >= 0 && bytes.Compare(candidate, cs.High) <= 0 {
			return true
		}
	}
	return false
}
