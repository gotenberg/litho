package cmap

import (
	"embed"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"
)

//go:embed bcmaps/*
var bcmapFS embed.FS

// adobeCollection is the CID→Unicode content of one Adobe character
// collection: every mapping as a Unicode string, and the single-rune subset.
type adobeCollection struct {
	strings map[int]string
	runes   map[int]rune
}

// adobeMaps holds lazily-parsed collections, one per Adobe ordering.
var adobeMaps struct {
	japan1Once sync.Once
	japan1     *adobeCollection

	gb1Once sync.Once
	gb1     *adobeCollection

	cns1Once sync.Once
	cns1     *adobeCollection

	korea1Once sync.Once
	korea1     *adobeCollection
}

// adobeCollectionFor returns the parsed collection of an Adobe ordering, or
// nil when the ordering is not one of the four or its CMap cannot be read.
func adobeCollectionFor(ordering string) *adobeCollection {
	switch ordering {
	case "Japan1":
		adobeMaps.japan1Once.Do(func() { adobeMaps.japan1 = loadAdobeCollection(ordering) })
		return adobeMaps.japan1
	case "GB1":
		adobeMaps.gb1Once.Do(func() { adobeMaps.gb1 = loadAdobeCollection(ordering) })
		return adobeMaps.gb1
	case "CNS1":
		adobeMaps.cns1Once.Do(func() { adobeMaps.cns1 = loadAdobeCollection(ordering) })
		return adobeMaps.cns1
	case "Korea1":
		adobeMaps.korea1Once.Do(func() { adobeMaps.korea1 = loadAdobeCollection(ordering) })
		return adobeMaps.korea1
	default:
		return nil
	}
}

// AdobeCIDToUnicode returns the predefined CID→Unicode map for the given
// Adobe character collection ordering (e.g., "Japan1", "GB1", "CNS1", "Korea1").
// The map is lazily parsed from the embedded binary CMap file on first access.
// Returns nil if the ordering is not recognized.
//
// It holds the CIDs whose Unicode value is a single character. A CID the
// collection's UCS2 CMap maps to a sequence, such as Adobe-Japan1's
// parenthesised and circled numbers, is left out: no one rune stands for it.
// So is a CID it maps to U+FFFD, Adobe's mark for a glyph no code point
// represents (CID 0 of GB1, and variant glyphs: 209 mappings in Adobe-CNS1, 10
// in Adobe-Japan1): a caller looking a glyph up by its character would find
// the replacement glyph.
// [adobeCIDToUnicodeStrings] carries both, as PDF.js reads them.
func AdobeCIDToUnicode(ordering string) map[int]rune {
	c := adobeCollectionFor(ordering)
	if c == nil {
		return nil
	}
	return c.runes
}

// adobeCIDToUnicodeStrings returns every CID→Unicode mapping of an Adobe
// character collection, a CID that maps to a sequence included, or nil when
// the ordering is not recognized.
func adobeCIDToUnicodeStrings(ordering string) map[int]string {
	c := adobeCollectionFor(ordering)
	if c == nil {
		return nil
	}
	return c.strings
}

// loadAdobeCollection reads the Adobe-<ordering>-UCS2 binary CMap, the
// Registry-Ordering-UCS2 CMap ISO 32000-1, 9.10.2 obtains a CID's Unicode
// value from, as the copy PDF.js ships (its buildToUnicode reads the same
// file, so text extraction stays equal to PDF.js).
//
// The Uni*-UCS2-H encoding CMaps run the other way, Unicode to CID, and do not
// invert: several code points select one CID, and reading them backwards
// picked the lowest. UniGB-UCS2-H maps both U+2F00 KANGXI RADICAL ONE and
// U+4E00 to the CID of 一, so the inversion reported the radical; the
// collection's UCS2 CMap names the ideograph.
//
// # Corpus evidence
//
//   - pdfjs bug1734802.pdf (ETen-B5-H), issue13147.pdf and vertical.pdf
//     (Identity-H over an embedded Adobe-Japan1 font): the only files whose
//     text moves, each from a radical to its ideograph (U+2EC6 to 角, U+2E9D
//     to 月, U+2F45 to 方, U+2F47 to 日). The text cell does not see it: it
//     compares NFKC forms, and PDF.js extracts none of these characters.
func loadAdobeCollection(ordering string) *adobeCollection {
	data, err := bcmapFS.ReadFile("bcmaps/Adobe-" + ordering + "-UCS2.bcmap")
	if err != nil {
		return nil
	}
	chars, ranges, err := parseBCMapBFMappings(data)
	if err != nil {
		return nil
	}
	c := &adobeCollection{
		strings: make(map[int]string, len(chars)+len(ranges)),
		runes:   make(map[int]rune, len(chars)+len(ranges)),
	}
	// The bundled UCS2 CMaps give no CID both a bfchar and a bfrange mapping,
	// so the order the two are merged in does not matter.
	for _, m := range []map[string]string{ranges, chars} {
		for code, uni := range m {
			if len(code) != bcmapTwoByteSource || uni == "" {
				continue
			}
			cid := int(code[0])<<8 | int(code[1])
			c.strings[cid] = uni
			if r, size := utf8.DecodeRuneInString(uni); size == len(uni) && r != utf8.RuneError {
				c.runes[cid] = r
			} else {
				delete(c.runes, cid)
			}
		}
	}
	return c
}

// orderingFromEncodingName resolves a predefined PDF CMap encoding name
// (for example "UniJIS-UTF16-H", "GBK-EUC-V", "ETen-B5-H") to the Adobe
// character collection ordering that carries its CID→Unicode table
// ("Japan1", "GB1", "CNS1", or "Korea1"). Name-pattern dispatch covers
// the full set of predefined CMaps listed in ISO 32000-1 §9.7.5.2
// Table 118. An unrecognized name returns "", signalling the caller to
// treat the encoding as non-predefined.
func orderingFromEncodingName(name string) string {
	// Short identity-style names (Japan1).
	switch name {
	case "H", "V", "Hiragana", "Katakana", "Roman", "Hankaku", "WP-Symbol":
		return "Japan1"
	}
	switch {
	case strings.HasPrefix(name, "GB"),
		strings.HasPrefix(name, "UniGB-"):
		return "GB1"
	case strings.HasPrefix(name, "B5"),
		strings.HasPrefix(name, "CNS"),
		strings.HasPrefix(name, "ETen"),
		strings.HasPrefix(name, "ETHK"),
		strings.HasPrefix(name, "HKdla"),
		strings.HasPrefix(name, "HKdlb"),
		strings.HasPrefix(name, "HKgccs"),
		strings.HasPrefix(name, "HKm"),
		strings.HasPrefix(name, "HKscs"),
		strings.HasPrefix(name, "UniCNS-"):
		return "CNS1"
	case strings.HasPrefix(name, "78"),
		strings.HasPrefix(name, "83"),
		strings.HasPrefix(name, "90"),
		strings.HasPrefix(name, "Add"),
		strings.HasPrefix(name, "EUC-"),
		strings.HasPrefix(name, "Ext"),
		strings.HasPrefix(name, "NWP"),
		strings.HasPrefix(name, "RKSJ"),
		strings.HasPrefix(name, "UniJIS"):
		return "Japan1"
	case strings.HasPrefix(name, "KSC"),
		strings.HasPrefix(name, "UniKS-"):
		return "Korea1"
	}
	return ""
}

// predefinedCMapCache caches loaded predefined CMaps by name.
var predefinedCMapCache sync.Map

// LoadPredefinedCMap loads a predefined CMap encoding by name (e.g., "GBK-EUC-H")
// and composes it with the corresponding Adobe CID→Unicode table to produce a
// ToUnicode-style CMap that maps byte sequences directly to Unicode strings.
// Results are cached. Returns nil if the encoding name is not recognized.
//
// The code→CID side is [LoadPredefinedCIDMap], so a CMap's usecmap chain
// supplies the mappings and codespaces it inherits, as PDF.js's extendCMap
// does: UniJIS-UCS2-V holds 251 vertical forms and takes the rest of
// UniJIS-UCS2-H. Notdef mappings are left out, as PDF.js's BinaryCMapReader
// skips them: a code they cover names no character, so composing their CID
// with the CID→Unicode table would invent one.
func LoadPredefinedCMap(name string) *CMap {
	if cached, ok := predefinedCMapCache.Load(name); ok {
		cm, _ := cached.(*CMap)
		return cm
	}

	cm := loadPredefinedCMapUncached(name)
	predefinedCMapCache.Store(name, cm)
	return cm
}

func loadPredefinedCMapUncached(name string) *CMap {
	ordering := orderingFromEncodingName(name)
	if ordering == "" {
		return nil
	}

	cidMap := LoadPredefinedCIDMap(name)
	if cidMap == nil {
		return nil
	}
	charToCID, ok := cidMap.cidMappings()
	if !ok || len(charToCID) == 0 {
		return nil
	}
	// A copy: the CIDMap is shared through the cache, and a CMap is the
	// caller's to keep.
	codespaces := slices.Clone(cidMap.cs.CodeSpaceRanges)

	cid2uni := adobeCIDToUnicodeStrings(ordering)
	if cid2uni == nil {
		return nil
	}

	// Compose: charCode → CID → Unicode.
	mapping := make(map[string]string, len(charToCID))
	for charCode, cid := range charToCID {
		uni, ok := cid2uni[cid]
		if !ok {
			continue
		}
		byteLen := byteLength(charCode, codespaces)
		key := intToBytes(charCode, byteLen)
		mapping[string(key)] = uni
	}

	return &CMap{
		Mapping:         mapping,
		CodeSpaceRanges: codespaces,
	}
}

// byteLength determines the byte length of a character code from the codespace
// ranges, mirroring PDF.js CMap.readCharCode: the width is the SHORTEST codespace
// width whose integer range [low, high] contains the code. The previous version
// truncated the code to n bytes before the range check, so a value like 0xE644
// matched a 1-byte range on its low byte (0x44) and was mis-sized as 1 byte.
func byteLength(code int64, codespaces []CodeSpaceRange) int {
	for n := 1; n <= 4; n++ {
		if code >= 1<<(8*n) {
			continue // the value does not fit in n bytes
		}
		for _, cs := range codespaces {
			if len(cs.Low) != n {
				continue
			}
			if code >= byteValue(cs.Low) && code <= byteValue(cs.High) {
				return n
			}
		}
	}
	// Default: use minimum bytes needed.
	if code > 0xFF {
		return 2
	}
	return 1
}
