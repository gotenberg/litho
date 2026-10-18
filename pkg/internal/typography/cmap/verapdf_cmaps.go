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
