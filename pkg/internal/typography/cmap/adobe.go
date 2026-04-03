package cmap

import (
	"embed"
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

// encodingToOrdering maps predefined CMap encoding names to their Adobe
// character collection ordering.
var encodingToOrdering = map[string]string{
	"GBK-EUC-H":    "GB1",
	"GBK-EUC-V":    "GB1",
	"GBKp-EUC-H":   "GB1",
	"GBKp-EUC-V":   "GB1",
	"GBK2K-H":      "GB1",
	"GBK2K-V":      "GB1",
	"GB-EUC-H":     "GB1",
	"GB-EUC-V":     "GB1",
	"GBpc-EUC-H":   "GB1",
	"GBpc-EUC-V":   "GB1",
	"GBT-EUC-H":    "GB1",
	"GBT-EUC-V":    "GB1",
	"GBTpc-EUC-H":  "GB1",
	"GBTpc-EUC-V":  "GB1",
	"EUC-H":        "Japan1",
	"EUC-V":        "Japan1",
	"78-EUC-H":     "Japan1",
	"78-EUC-V":     "Japan1",
	"83pv-RKSJ-H":  "Japan1",
	"90ms-RKSJ-H":  "Japan1",
	"90ms-RKSJ-V":  "Japan1",
	"90msp-RKSJ-H": "Japan1",
	"90msp-RKSJ-V": "Japan1",
	"90pv-RKSJ-H":  "Japan1",
	"90pv-RKSJ-V":  "Japan1",
	"CNS-EUC-H":    "CNS1",
	"CNS-EUC-V":    "CNS1",
	"KSC-EUC-H":    "Korea1",
	"KSC-EUC-V":    "Korea1",
	"KSCpc-EUC-H":  "Korea1",
	"KSCpc-EUC-V":  "Korea1",
}

// LoadPredefinedCMap loads a predefined CMap encoding by name (e.g., "GBK-EUC-H")
// and composes it with the corresponding Adobe CID→Unicode table to produce a
// ToUnicode-style CMap that maps byte sequences directly to Unicode strings.
// Returns nil if the encoding name is not recognized.
func LoadPredefinedCMap(name string) *CMap {
	ordering, ok := encodingToOrdering[name]
	if !ok {
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
		// Determine byte length from the codespace ranges.
		byteLen := byteLength(charCode, codespaces)
		key := intToBytes(charCode, byteLen)
		mapping[string(key)] = string(uni)
	}

	return &CMap{
		Mapping:         mapping,
		CodeSpaceRanges: codespaces,
	}
}

// byteLength determines the byte length of a character code based on codespace ranges.
func byteLength(code int, codespaces []CodeSpaceRange) int {
	for _, cs := range codespaces {
		n := len(cs.Low)
		val := 0
		for _, b := range intToBytes(code, n) {
			val = (val << 8) | int(b)
		}
		low := 0
		for _, b := range cs.Low {
			low = (low << 8) | int(b)
		}
		high := 0
		for _, b := range cs.High {
			high = (high << 8) | int(b)
		}
		if val >= low && val <= high {
			return n
		}
	}
	// Default: use minimum bytes needed.
	if code > 0xFF {
		return 2
	}
	return 1
}
