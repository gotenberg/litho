package cmap

import (
	"bytes"
	"compress/gzip"
	"context"
	"embed"
	"errors"
	"fmt"
	"io"
)

//go:generate go run ../../../../cmd/gen-verapdf-cmaps

// veraPDFCMapFS holds the encoding CMaps veraPDF 1.30.2 bundles whose
// code-to-CID content PDF.js's binary copies under bcmaps/ do not hold. See
// verapdf_cmaps_sources.go for their provenance, and [ConformanceCIDBase]
// for their use: the conformance checks only, text extraction keeping to
// PDF.js.
//
//go:embed verapdfcmaps/*.gz
var veraPDFCMapFS embed.FS

// maxVeraPDFCMapBytes caps the decompressed size of one embedded CMap.
// Anti-DoS guard against a corrupt regeneration, not a format limit: the
// largest file, UniGB-UTF32-H, is about 210 KB.
const maxVeraPDFCMapBytes = 1 << 20

var errVeraPDFCMapTooLarge = errors.New("veraPDF CMap resource larger than its cap")

// adobeKRUCS2 is the one CMap veraPDF bundles that the adobeucs2 resources
// hold byte for byte, so verapdfcmaps carries no second copy of it.
const adobeKRUCS2 = "Adobe-KR-UCS2"

// veraPDFCMapSource returns the program text of the CMap name as veraPDF
// 1.30.2 bundles it, and false when the verapdfcmaps resources do not hold it.
func veraPDFCMapSource(name string) ([]byte, bool, error) {
	if name == adobeKRUCS2 {
		return adobeUCS2Source(name)
	}
	if _, ok := veraPDFCMapDigests[name]; !ok {
		return nil, false, nil
	}
	gz, err := veraPDFCMapFS.ReadFile("verapdfcmaps/" + name + ".gz")
	if err != nil {
		return nil, true, fmt.Errorf("reading embedded %s: %w", name, err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		return nil, true, fmt.Errorf("opening embedded %s: %w", name, err)
	}
	defer zr.Close()
	data, err := io.ReadAll(io.LimitReader(zr, maxVeraPDFCMapBytes+1))
	if err != nil {
		return nil, true, fmt.Errorf("decompressing embedded %s: %w", name, err)
	}
	if len(data) > maxVeraPDFCMapBytes {
		return nil, true, fmt.Errorf("%s: %w", name, errVeraPDFCMapTooLarge)
	}
	return data, true, nil
}

// loadVeraPDFCIDBase parses the CMap name from veraPDF's copy, as veraPDF's
// CMapParser runs it: its cidchar, cidrange and notdef mappings, its
// codespaces, its /WMode, and the bases its usecmap operators name, resolved
// through the same set. It reports false when veraPDF's copy is not embedded,
// so the caller reads PDF.js's, and returns nil with true when the copy
// cannot be read.
func loadVeraPDFCIDBase(name string, depth int) (*CIDMap, bool) {
	data, ok, err := veraPDFCMapSource(name)
	if !ok {
		return nil, false
	}
	if err != nil {
		return nil, true
	}
	// The program is litho's own embedded resource, bounded by
	// maxVeraPDFCMapBytes and parsed once per process, so no caller's
	// context governs it.
	m := parseCIDMapOver(context.Background(), data, Base{}, func(base string) Base {
		return namedBase(conformanceBases, base, depth+1)
	})
	if m == nil || !m.unmappedNotdef || len(m.notdefs) > maxNotdefEntries {
		return nil, true
	}
	m.wmode = programWMode(data)
	return m, true
}

// programWMode returns the writing mode a CMap program declares with
// "/WMode n def": 1 for vertical, 0 otherwise (ISO 32000-1, 9.7.5.3,
// Table 120).
func programWMode(data []byte) int {
	_, after, ok := bytes.Cut(data, []byte("/WMode"))
	if !ok {
		return 0
	}
	after = bytes.TrimLeft(after, " \t\r\n")
	if len(after) > 0 && after[0] == '1' {
		return 1
	}
	return 0
}

// LoadConformanceCIDMap is [LoadPredefinedCIDMap] for the conformance checks:
// the code-to-CID mapping of the predefined CMap a Type 0 font's /Encoding
// names, read through [ConformanceCIDBase]. veraPDF's PDCMap loads the file it
// bundles under any name, so a name veraPDF 1.30.2 bundles resolves even when
// it lies outside ISO 32000-1, 9.7.5.2, Table 118 (Adobe-Japan1-2,
// UniAKR-UTF16-H); ISO 19005-2, 6.2.11.3.3 judges such a name separately.
// Identity-H and Identity-V come back nil, as from [LoadPredefinedCIDMap].
func LoadConformanceCIDMap(name string) *CIDMap {
	if name == "Identity-H" || name == "Identity-V" {
		return nil
	}
	if !veraPDFBundledCMaps[name] && orderingFromEncodingName(name) == "" {
		return nil
	}
	return ConformanceCIDBase(name)
}

// CollectionUCS2Bundled reports whether veraPDF 1.30.2 bundles the UCS2 CMap
// of the character collection registry-ordering, "<Registry>-<Ordering>-UCS2"
// (ISO 32000-1, 9.10.2), through which it maps a Type 0 font's CIDs to
// Unicode when the font carries no /ToUnicode value for them
// (PDType0Font.setUcsCMap). It holds for Adobe-CNS1, Adobe-GB1, Adobe-Japan1,
// Adobe-Korea1 and Adobe-KR.
func CollectionUCS2Bundled(registry, ordering string) bool {
	if registry == "" || ordering == "" {
		return false
	}
	return veraPDFBundledCMaps[registry+"-"+ordering+"-UCS2"]
}

// ConformanceCMap is a CMap program as veraPDF 1.30.2 bundles it, with the
// CIDSystemInfo and writing mode the program declares, so a stream carrying it
// can state them in its dictionary (ISO 32000-1, 9.7.5.3, Table 120).
type ConformanceCMap struct {
	// Program is the CMap file byte for byte.
	Program []byte
	// Registry, Ordering and Supplement are the program's /CIDSystemInfo.
	Registry, Ordering string
	Supplement         int
	// WMode is the program's /WMode: 0 horizontal, 1 vertical.
	WMode int
}

// ConformanceProgram returns the program of the CMap name exactly as veraPDF
// 1.30.2 bundles it, for a repair to embed: the file [ConformanceCIDBase] reads
// under the name, so a stream carrying it maps every code to the CID the
// conformance checks gave it while the font named it. It covers the CMaps the
// verapdfcmaps resources hold, and reports false for any other name.
//
// It also reports false when the program runs a usecmap operator, since the
// embedded copy would then still reference a CMap by name (ISO 19005-2,
// 6.2.11.3.3 test 3), and when the program declares no complete
// CIDSystemInfo, which the stream dictionary must state (ISO 32000-1, 9.7.5.3,
// Table 120).
func ConformanceProgram(name string) (ConformanceCMap, bool, error) {
	if _, ok := veraPDFCMapDigests[name]; !ok {
		return ConformanceCMap{}, false, nil
	}
	data, _, err := veraPDFCMapSource(name)
	if err != nil {
		return ConformanceCMap{}, false, fmt.Errorf("reading veraPDF's %s CMap: %w", name, err)
	}
	m := ConformanceCIDBase(name)
	if m == nil || len(m.bases) > 0 {
		return ConformanceCMap{}, false, nil
	}
	reg, ord, sup, ok := programCIDSystemInfo(data)
	if !ok {
		return ConformanceCMap{}, false, nil
	}
	return ConformanceCMap{
		Program:    data,
		Registry:   reg,
		Ordering:   ord,
		Supplement: sup,
		WMode:      m.wmode,
	}, true, nil
}

// programCIDSystemInfo returns the Registry, Ordering and Supplement the
// /CIDSystemInfo dictionary of a CMap program defines (Adobe Technical Note
// #5014, CIDSystemInfo), reading the literal-string and integer form Adobe's
// CMap files use, and reports false when one of the three is missing.
func programCIDSystemInfo(data []byte) (string, string, int, bool) {
	_, after, ok := bytes.Cut(data, []byte("/CIDSystemInfo"))
	if !ok {
		return "", "", 0, false
	}
	body, _, ok := bytes.Cut(after, []byte("end"))
	if !ok {
		return "", "", 0, false
	}
	reg, okReg := programStringAfter(body, "/Registry")
	ord, okOrd := programStringAfter(body, "/Ordering")
	sup, okSup := programIntAfter(body, "/Supplement")
	if !okReg || !okOrd || !okSup {
		return "", "", 0, false
	}
	return reg, ord, sup, true
}

// programStringAfter returns the literal string that follows key in body. The
// strings it reads are Adobe's registry and ordering names, which hold no
// parenthesis or escape.
func programStringAfter(body []byte, key string) (string, bool) {
	_, after, ok := bytes.Cut(body, []byte(key))
	if !ok {
		return "", false
	}
	after = bytes.TrimLeft(after, " \t\r\n")
	if len(after) == 0 || after[0] != '(' {
		return "", false
	}
	value, _, ok := bytes.Cut(after[1:], []byte(")"))
	if !ok || len(value) == 0 {
		return "", false
	}
	return string(value), true
}

// maxSupplementDigits bounds the digits [programIntAfter] reads, so the value
// cannot overflow. It is an implementation limit, not spec-mandated: Adobe's
// supplements take one or two digits.
const maxSupplementDigits = 6

// programIntAfter returns the non-negative integer that follows key in body.
func programIntAfter(body []byte, key string) (int, bool) {
	_, after, ok := bytes.Cut(body, []byte(key))
	if !ok {
		return 0, false
	}
	after = bytes.TrimLeft(after, " \t\r\n")
	n, digits := 0, 0
	for _, c := range after {
		if c < '0' || c > '9' {
			break
		}
		if digits == maxSupplementDigits {
			return 0, false
		}
		n = n*10 + int(c-'0')
		digits++
	}
	return n, digits > 0
}
