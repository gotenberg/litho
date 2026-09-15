package cmap

import (
	"slices"
	"sync"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// CIDCode is one decoded character code: its CID and whether the encoding CMap
// actually mapped it. An unmapped code renders as the .notdef glyph.
type CIDCode struct {
	CID    int
	Mapped bool
}

// CIDMap decodes content-stream character codes to CIDs for a predefined
// encoding CMap, honouring its multi-byte codespace ranges. It is the code->CID
// counterpart of the code->Unicode [CMap] that [LoadPredefinedCMap] builds.
type CIDMap struct {
	charToCID map[int]int
	cs        *CMap // carries the codespace ranges for the shared helpers
	// unmappedNotdef marks a map whose every mapping litho holds, so a code it
	// leaves unmapped selects CID 0; see [CIDMap.UnmappedNotdef].
	unmappedNotdef bool
}

// UnmappedNotdef reports whether a code the map leaves unmapped is known to
// select CID 0 (ISO 32000-1, 9.7.6.3): true for an embedded CMap that names no
// base CMap (usecmap) and no notdef mappings, false when an unresolved base
// CMap or notdef range could still supply the code.
func (m *CIDMap) UnmappedNotdef() bool {
	return m.unmappedNotdef
}

// predefinedCIDCache caches loaded predefined CID maps by encoding name.
//
//nolint:gochecknoglobals // process-wide cache of immutable parsed CMaps.
var predefinedCIDCache sync.Map

// LoadPredefinedCIDMap loads the code->CID mapping of a predefined encoding CMap
// (for example "UniGB-UCS2-H") from the bundled Adobe bcmaps. It returns nil
// when the name is not a recognized predefined CMap or the data cannot be read.
// Results are cached.
func LoadPredefinedCIDMap(name string) *CIDMap {
	if cached, ok := predefinedCIDCache.Load(name); ok {
		cm, _ := cached.(*CIDMap)
		return cm
	}
	cm := loadPredefinedCIDMapUncached(name)
	predefinedCIDCache.Store(name, cm)
	return cm
}

func loadPredefinedCIDMapUncached(name string) *CIDMap {
	if orderingFromEncodingName(name) == "" {
		return nil
	}
	data, err := bcmapFS.ReadFile("bcmaps/" + name + ".bcmap")
	if err != nil {
		return nil
	}
	charToCID, codespaces, err := parseBCMapCharToCID(data)
	if err != nil || len(charToCID) == 0 || len(codespaces) == 0 {
		return nil
	}
	return &CIDMap{
		charToCID: charToCID,
		cs:        &CMap{CodeSpaceRanges: codespaces},
	}
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
		consumed, cid, mapped := m.next(data[off:], lengths, hasShort)
		out = append(out, CIDCode{CID: cid, Mapped: mapped})
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
		consumed, _, _ := m.next(data[off:], lengths, hasShort)
		var code uint32
		for _, b := range data[off : off+consumed] {
			code = code<<8 | uint32(b)
		}
		out = append(out, code)
		off += consumed
	}
	return out
}

// next decodes one character code from the front of data, mirroring the
// codespace handling in [CMap.mapWithCodeSpaces]: it prefers the longest
// codespace width that fits, falls back to a shorter mapped prefix, and for a
// mixed-width CMap advances a single byte past an unmapped multi-byte code so a
// following narrower code can re-align. consumed is always >= 1.
func (m *CIDMap) next(data []byte, lengths []int, hasShort bool) (consumed, cid int, mapped bool) {
	for _, n := range slices.Backward(lengths) {
		if n == 0 || n > len(data) {
			continue
		}
		candidate := data[:n]
		if !m.cs.inCodeSpace(candidate) {
			continue
		}
		if c, ok := m.charToCID[strToInt(ast.PdfString(candidate))]; ok {
			return n, c, true
		}
		// In codespace but unmapped: prefer a shorter mapped prefix.
		for shorter := n - 1; shorter >= 1; shorter-- {
			if c, ok := m.charToCID[strToInt(ast.PdfString(data[:shorter]))]; ok {
				return shorter, c, true
			}
		}
		// Still unmapped: in a mixed-width CMap, advance one byte so a narrower
		// following code can re-align; otherwise consume the full width.
		if n > 1 && hasShort {
			return 1, 0, false
		}
		return n, 0, false
	}
	// No codespace matched: an undefined byte. Advance one to re-align.
	return 1, 0, false
}
