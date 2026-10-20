package cmap

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"unicode/utf8"
)

// WithCIDValuesAsUnicode returns c with the cidchar and cidrange entries of
// the /ToUnicode program data read as Unicode values, for each code c does
// not map already. c must be the [Parse] result of data.
//
// ISO 32000-1, 9.10.3 writes a /ToUnicode CMap with bfchar and bfrange,
// whose destinations are UTF-16BE strings; a CID entry has no place there.
// Some producers still write the Identity-H encoding CMap's program as the
// /ToUnicode, with each destination CID set to the Unicode value of the
// code's character. PDF.js reads such an entry as that code point
// (src/core/evaluator.js, readToUnicode, "Some cmaps contain *only* CID
// characters"), and so does litho: [Parse] keeps no CID entry, so the
// program otherwise maps nothing and the codes read as their own values.
// A destination that is no Unicode scalar value, or 0, maps nothing. A
// program that maps each of the 65536 two-byte codes to itself is the
// Identity-H program and says nothing of the characters: PDF.js reads it as
// no /ToUnicode at all (src/core/cmap.js, isIdentityCMap, and readToUnicode),
// and so does litho, which leaves c unchanged.
// Each code takes the byte length of the codespace range that holds it
// ([byteLength]).
//
// It returns c unchanged when data holds no CID entry, and an error
// wrapping [ErrCMapTooLarge] when its ranges pass the parse budget.
//
// # Corpus evidence
//
//   - pdfjs issue9367.pdf: three Tahoma fonts whose /ToUnicode is the
//     Identity-H program with 2891 cidrange entries giving the Unicode
//     value of each code, <0003> to 32 and on. litho read every code as its
//     own value, "+8" for "HU". pdfjs issue9915_reduced.pdf: the same for
//     "ISSUE".
//   - pdfjs issue5801.pdf, issue15719.pdf: the Identity-H program itself as
//     the /ToUnicode; read as CID values, issue5801.pdf lost the words it
//     matches PDF.js on.
//   - All corpora: no other file of the six text corpora carries a CID
//     entry in a /ToUnicode.
func WithCIDValuesAsUnicode(ctx context.Context, c *CMap, data []byte) (*CMap, error) {
	if c == nil || !bytes.Contains(data, []byte("begincid")) {
		return c, nil
	}
	cids, err := ParseCID(ctx, data)
	if err != nil {
		return c, fmt.Errorf("reading the CID entries of a /ToUnicode CMap: %w", err)
	}
	if len(cids.Mapping) == 0 || isIdentityCIDMapping(cids.Mapping) {
		return c, nil
	}
	mapping := make(map[string]string, len(c.Mapping)+len(cids.Mapping))
	maps.Copy(mapping, c.Mapping)
	for code, value := range cids.Mapping {
		if value <= 0 || value > utf8.MaxRune || !utf8.ValidRune(rune(value)) { //nolint:gosec // value is bounded to utf8.MaxRune above
			continue
		}
		key := string(intToBytes(code, byteLength(code, c.CodeSpaceRanges)))
		if _, ok := mapping[key]; ok {
			continue
		}
		mapping[key] = string(rune(value))
	}
	// The CID entries stay out of the strict view a compliance check reads
	// ([CMap.StrictMapping]): 9.10.3 gives them no Unicode value.
	return &CMap{
		Mapping:         mapping,
		CodeSpaceRanges: c.CodeSpaceRanges,
		strict:          maps.Clone(c.StrictMapping()),
		malformed:       c.malformed,
		useCMap:         c.useCMap,
		afterUse:        c.afterUse,
	}, nil
}

// identityCIDCodes is the number of two-byte codes, every one of which the
// Identity-H program maps to itself (ISO 32000-1, 9.7.5.2, Table 118).
const identityCIDCodes = 0x10000

// isIdentityCIDMapping reports whether m maps each two-byte code to itself
// and nothing else, as the Identity-H and Identity-V programs do.
func isIdentityCIDMapping(m map[int]int) bool {
	if len(m) != identityCIDCodes {
		return false
	}
	for code, cid := range m {
		if code != cid {
			return false
		}
	}
	return true
}
