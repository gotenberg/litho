package cmap

import (
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// CIDCode is one decoded character code: its CID and whether the encoding CMap
// actually mapped it. An unmapped code renders as the .notdef glyph.
//
// Undefined marks bytes that match no codespace range, which litho reads one
// byte at a time. veraPDF (CMap.getCodeFromStream) reads them differently: it
// skips as many bytes as the shortest codespace that matched so far and yields
// code 0. The two splits disagree on such bytes, so a caller does not infer
// the CID they select.
type CIDCode struct {
	CID       int
	Mapped    bool
	Undefined bool
}

// CIDMap decodes content-stream character codes to CIDs for a predefined
// encoding CMap, honouring its multi-byte codespace ranges. It is the code->CID
// counterpart of the code->Unicode [CMap] that [LoadPredefinedCMap] builds.
type CIDMap struct {
	charToCID map[int]int
	// spans maps code ranges to incrementing CIDs, consulted after charToCID:
	// the Identity CMaps, which map every two-byte code to itself.
	spans []cidSpan
	// bases are the CMaps this one builds on (usecmap, /UseCMap), consulted in
	// order once charToCID and spans leave a code unmapped.
	bases []*CIDMap
	// notdefs are the notdef mappings of this CMap and of its bases, in the
	// order veraPDF's CMap.useCMap gathers them, the first covering a code
	// giving its CID.
	notdefs []notdefSpan
	cs      *CMap // carries the codespace ranges for the shared helpers
	// unmappedNotdef marks a map whose every mapping litho holds, so a code it
	// leaves unmapped selects CID 0; see [CIDMap.UnmappedNotdef].
	unmappedNotdef bool
	// wmode is the writing mode the CMap declares; see [CIDMap.WMode].
	wmode int
}

// UnmappedNotdef reports whether a code the map leaves unmapped is known to
// select CID 0 (ISO 32000-1, 9.7.6.3): true for an embedded CMap whose every
// base CMap (usecmap, /UseCMap) litho resolved, false when an unresolved base
// could still supply the code.
func (m *CIDMap) UnmappedNotdef() bool {
	return m.unmappedNotdef
}

// LoadPredefinedCIDMap loads the code->CID mapping of a predefined encoding
// CMap a Type 0 font's /Encoding names (for example "UniGB-UCS2-H"): the
// [PredefinedCIDBase] of that name, its usecmap chain, notdef mappings and
// codespaces resolved through the bundled CMaps. veraPDF's PDCMap reads a
// named encoding the same way, parsing its bundled CMap file with the parser
// that runs usecmap and the notdef blocks, and PDF.js follows the chain too
// (extendCMap). A -V CMap holds only its vertical forms and takes every other
// mapping and its codespaces from its -H base, so a reader that skipped the
// chain would split no code at all.
//
// It returns nil for Identity-H and Identity-V, which callers read as two
// bytes per CID, for a name outside the predefined set of ISO 32000-1,
// 9.7.5.2, Table 118, and when the bundled data cannot be read. The map is cached,
// read-only and safe for concurrent use.
//
// # Corpus evidence
//
//   - pdfjs bug1905623.pdf: names UniJIS-UTF16-V for an unembedded font.
//     Read without its chain, litho split no code of it and reported ISO
//     19005-1, 6.3.8 t1 63 times at PDF/A-1a, where veraPDF reports 1504.
//   - All corpora: 26 files give a Type 0 font a predefined non-Identity
//     /Encoding, bug1905623.pdf the only -V one. The other 25 validate,
//     remediate and extract alike whether the chain is read or not, so the
//     litho fixture cmappredef_2b.pdf plants the codes that tell them apart.
func LoadPredefinedCIDMap(name string) *CIDMap {
	if orderingFromEncodingName(name) == "" {
		return nil
	}
	return PredefinedCIDBase(name)
}

// Decode splits data into character codes using the CMap's codespace ranges and
// returns the CID for each. A code that falls outside every codespace, or inside
// one but absent from the mapping, is returned with Mapped=false: it is a
// reference to the .notdef glyph.
func (m *CIDMap) Decode(data []byte) []CIDCode {
	lengths := m.cs.codeLengths()
	hasShort := false
	for _, n := range lengths {
		if n == 1 {
			hasShort = true
		}
	}

	var out []CIDCode
	for off := 0; off < len(data); {
		consumed, cid, mapped, undefined := m.next(data[off:], lengths, hasShort)
		out = append(out, CIDCode{CID: cid, Mapped: mapped, Undefined: undefined})
		off += consumed
	}
	return out
}

// Codes splits data into character codes exactly as [CIDMap.Decode] does and
// returns each code's big-endian value, which is what a /ToUnicode CMap is keyed
// by (ISO 32000-1, 9.10.3).
func (m *CIDMap) Codes(data []byte) []uint32 {
	lengths := m.cs.codeLengths()
	hasShort := slices.Contains(lengths, 1)

	var out []uint32
	for off := 0; off < len(data); {
		consumed, _, _, _ := m.next(data[off:], lengths, hasShort)
		var code uint32
		for _, b := range data[off : off+consumed] {
			code = code<<8 | uint32(b)
		}
		out = append(out, code)
		off += consumed
	}
	return out
}

// CodeLength returns the byte length of the character code at the front of
// data, split exactly as [CIDMap.Decode] and [CIDMap.Codes] split it: the
// codespace ranges of the CMap and of its bases decide it (ISO 32000-1,
// 9.7.6.2). It is at least 1 for non-empty data and 0 for empty data.
// undefined reports bytes that match no codespace range, read one byte at a
// time ([CIDCode.Undefined]).
func (m *CIDMap) CodeLength(data []byte) (n int, undefined bool) {
	if len(data) == 0 {
		return 0, false
	}
	lengths := m.cs.codeLengths()
	consumed, _, _, undefined := m.next(data, lengths, slices.Contains(lengths, 1))
	return consumed, undefined
}

// MappedCID returns the CID the cidchar and cidrange mappings of the CMap
// and of its bases give the character code whose big-endian value is code
// (ISO 32000-1, 9.7.6.2), and false when none covers it. Notdef mappings are
// left out, and so is the CID 0 an unmapped code selects (9.7.6.3): this is
// the reading of a viewer's metrics lookup, which PDF.js performs
// (fonts.js, _charToGlyph) by taking the width of the code itself when its
// CMap does not contain it. A validator decodes through [CIDMap.Decode].
func (m *CIDMap) MappedCID(code int) (int, bool) {
	return m.mappedCID(code)
}

// CodeSpaceRanges returns a copy of the codespace ranges the CMap and its
// bases declare (ISO 32000-1, 9.7.6.2), the ranges [CIDMap.CodeLength] splits
// codes by.
func (m *CIDMap) CodeSpaceRanges() []CodeSpaceRange {
	if m == nil || m.cs == nil {
		return nil
	}
	return slices.Clone(m.cs.CodeSpaceRanges)
}

// next decodes one character code from the front of data, mirroring the
// codespace handling in [CMap.mapWithCodeSpaces]: it prefers the longest
// codespace width that fits, falls back to a shorter mapped prefix, and for a
// mixed-width CMap advances a single byte past an unmapped multi-byte code so a
// following narrower code can re-align. consumed is always >= 1.
func (m *CIDMap) next(data []byte, lengths []int, hasShort bool) (consumed, cid int, mapped, undefined bool) {
	for _, n := range slices.Backward(lengths) {
		if n == 0 || n > len(data) {
			continue
		}
		candidate := data[:n]
		if !m.cs.inCodeSpaceBytewise(candidate) {
			continue
		}
		if c, ok := m.lookup(strToInt(ast.PdfString(candidate))); ok {
			return n, c, true, false
		}
		// In codespace but unmapped: prefer a shorter mapped prefix that is a
		// code of its own. Mappings are keyed by value, so a prefix outside
		// every codespace would find the mapping of a wider code with the
		// same value: <01> would take the notdef range <0000> <001F> of a
		// two-byte CMap.
		for shorter := n - 1; shorter >= 1; shorter-- {
			if !m.cs.inCodeSpaceBytewise(data[:shorter]) {
				continue
			}
			if c, ok := m.lookup(strToInt(ast.PdfString(data[:shorter]))); ok {
				return shorter, c, true, false
			}
		}
		// Still unmapped: in a mixed-width CMap, advance one byte so a narrower
		// following code can re-align; otherwise consume the full width.
		if n > 1 && hasShort {
			return 1, 0, false, false
		}
		return n, 0, false, false
	}
	// No codespace matched: an undefined byte. Advance one to re-align.
	return 1, 0, false, true
}
