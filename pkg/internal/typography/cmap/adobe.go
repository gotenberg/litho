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
