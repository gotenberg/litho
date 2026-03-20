package cmap

import (
	"bufio"
	"bytes"
	"embed"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

//go:embed cmaps/*
var cmapFS embed.FS

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
// The map is lazily parsed from the embedded CMap file on first access.
// Returns nil if the ordering is not recognized.
func AdobeCIDToUnicode(ordering string) map[int]rune {
	switch ordering {
	case "Japan1":
		adobeMaps.japan1Once.Do(func() {
			adobeMaps.japan1 = loadAdobeCMap("cmaps/UniJIS-UCS2-H")
		})
		return adobeMaps.japan1
	case "GB1":
		adobeMaps.gb1Once.Do(func() {
			adobeMaps.gb1 = loadAdobeCMap("cmaps/UniGB-UCS2-H")
		})
		return adobeMaps.gb1
	case "CNS1":
		adobeMaps.cns1Once.Do(func() {
			adobeMaps.cns1 = loadAdobeCMap("cmaps/UniCNS-UCS2-H")
		})
		return adobeMaps.cns1
	case "Korea1":
		adobeMaps.korea1Once.Do(func() {
			adobeMaps.korea1 = loadAdobeCMap("cmaps/UniKS-UCS2-H")
		})
		return adobeMaps.korea1
	default:
		return nil
	}
}

// loadAdobeCMap reads an embedded CMap file (Unicode→CID format) and returns
// the inverted CID→Unicode map. When multiple Unicode values map to the same
// CID, the lowest Unicode value wins.
func loadAdobeCMap(name string) map[int]rune {
	data, err := cmapFS.ReadFile(name)
	if err != nil {
		return nil
	}
	return parseAndInvertCMap(data)
}

// parseAndInvertCMap parses a Unicode→CID CMap and inverts it to CID→Unicode.
// It handles begincidrange (<start> <end> cid) and begincidchar (<code> cid).
func parseAndInvertCMap(data []byte) map[int]rune {
	// First pass: collect Unicode→CID mappings.
	uni2cid := make(map[int]int, 16384) //nolint:mnd // typical CMap size

	scanner := bufio.NewScanner(bytes.NewReader(data))
	inRange := false
	inChar := false

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if strings.HasSuffix(line, "begincidrange") {
			inRange = true
			continue
		}
		if line == "endcidrange" {
			inRange = false
			continue
		}
		if strings.HasSuffix(line, "begincidchar") {
			inChar = true
			continue
		}
		if line == "endcidchar" {
			inChar = false
			continue
		}

		if inRange {
			start, end, cid, err := parseCIDRangeLine(line)
			if err != nil {
				continue
			}
			for i := 0; i <= end-start; i++ {
				uni2cid[start+i] = cid + i
			}
		} else if inChar {
			code, cid, err := parseCIDCharLine(line)
			if err != nil {
				continue
			}
			uni2cid[code] = cid
		}
	}

	// Second pass: invert to CID→Unicode. Lowest Unicode value wins.
	cid2uni := make(map[int]rune, len(uni2cid))
	for uni, cid := range uni2cid {
		if existing, ok := cid2uni[cid]; !ok || rune(uni) < existing {
			cid2uni[cid] = rune(uni)
		}
	}

	return cid2uni
}

// parseCIDRangeLine parses a line like "<0020> <005b> 1".
func parseCIDRangeLine(line string) (start, end, cid int, err error) {
	parts := strings.Fields(line)
	if len(parts) != 3 {
		return 0, 0, 0, fmt.Errorf("expected 3 fields")
	}
	start, err = parseHexToken(parts[0])
	if err != nil {
		return
	}
	end, err = parseHexToken(parts[1])
	if err != nil {
		return
	}
	cid, err = strconv.Atoi(parts[2])
	return
}

// parseCIDCharLine parses a line like "<0020> 1".
func parseCIDCharLine(line string) (code, cid int, err error) {
	parts := strings.Fields(line)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("expected 2 fields")
	}
	code, err = parseHexToken(parts[0])
	if err != nil {
		return
	}
	cid, err = strconv.Atoi(parts[1])
	return
}

// parseHexToken parses a "<XXXX>" hex token into an integer.
func parseHexToken(s string) (int, error) {
	s = strings.TrimPrefix(s, "<")
	s = strings.TrimSuffix(s, ">")
	v, err := strconv.ParseInt(s, 16, 64)
	return int(v), err
}
