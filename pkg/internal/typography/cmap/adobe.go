package cmap

import (
	"embed"
	"strings"
	"sync"
)

//go:embed bcmaps/*
var bcmapFS embed.FS

// adobeMaps holds lazily-parsed CID→Unicode maps, one per character collection.
var adobeMaps struct {
	japan1Once sync.Once
	japan1     map[int]rune

	gb1Once sync.Once
	gb1     map[int]rune

	cns1Once sync.Once
	cns1     map[int]rune

	korea1Once sync.Once
	korea1     map[int]rune
}

// AdobeCIDToUnicode returns the predefined CID→Unicode map for the given
// Adobe character collection ordering (e.g., "Japan1", "GB1", "CNS1", "Korea1").
// The map is lazily parsed from the embedded binary CMap file on first access.
// Returns nil if the ordering is not recognized.
func AdobeCIDToUnicode(ordering string) map[int]rune {
	switch ordering {
	case "Japan1":
		adobeMaps.japan1Once.Do(func() {
			adobeMaps.japan1 = loadAdobeBCMap("bcmaps/UniJIS-UCS2-H.bcmap")
		})
		return adobeMaps.japan1
	case "GB1":
		adobeMaps.gb1Once.Do(func() {
			adobeMaps.gb1 = loadAdobeBCMap("bcmaps/UniGB-UCS2-H.bcmap")
		})
		return adobeMaps.gb1
	case "CNS1":
		adobeMaps.cns1Once.Do(func() {
			adobeMaps.cns1 = loadAdobeBCMap("bcmaps/UniCNS-UCS2-H.bcmap")
		})
		return adobeMaps.cns1
	case "Korea1":
		adobeMaps.korea1Once.Do(func() {
			adobeMaps.korea1 = loadAdobeBCMap("bcmaps/UniKS-UCS2-H.bcmap")
		})
		return adobeMaps.korea1
	default:
		return nil
	}
}

// loadAdobeBCMap reads an embedded binary CMap (.bcmap) file and returns
// the CID→Unicode map.
func loadAdobeBCMap(name string) map[int]rune {
	data, err := bcmapFS.ReadFile(name)
	if err != nil {
		return nil
	}
	m, err := parseBCMapCIDToUnicode(data)
	if err != nil {
		return nil
	}
	return m
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

	bcmapPath := "bcmaps/" + name + ".bcmap"
	data, err := bcmapFS.ReadFile(bcmapPath)
	if err != nil {
		return nil
	}

	charToCID, codespaces, err := parseBCMapCharToCID(data)
	if err != nil || len(charToCID) == 0 {
		return nil
	}

	cid2uni := AdobeCIDToUnicode(ordering)
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
		mapping[string(key)] = string(uni)
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
func byteLength(code int, codespaces []CodeSpaceRange) int {
	for n := 1; n <= 4; n++ {
		if code >= 1<<(8*n) {
			continue // the value does not fit in n bytes
		}
		for _, cs := range codespaces {
			if len(cs.Low) != n {
				continue
			}
			low := 0
			for _, b := range cs.Low {
				low = (low << 8) | int(b)
			}
			high := 0
			for _, b := range cs.High {
				high = (high << 8) | int(b)
			}
			if code >= low && code <= high {
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
