package cmap

import (
	"fmt"
	"io/fs"
	"maps"
	"slices"
	"strings"
	"sync"
)

// maxNotdefEntries caps the notdef mappings one encoding CMap holds, its own
// and those its bases add. Anti-DoS guard, not spec-mandated: every code a
// CMap leaves unmapped is checked against them in turn, so the cap bounds that
// scan. Adobe's predefined CMaps declare at most a handful, so a CMap past it
// is crafted; litho then drops its notdef mappings and leaves the codes they
// would map unknown rather than guess.
const maxNotdefEntries = 1024

// maxUseCMapOps caps the usecmap operators one CMap program runs. Anti-DoS
// guard, not spec-mandated: ISO 32000-1, 9.7.5.4 gives a CMap one base, and
// each operator resolves a whole CMap, so a program past it is crafted.
const maxUseCMapOps = 4

// maxPredefinedBaseDepth caps how far [PredefinedCIDBase] follows the usecmap
// chain of the bundled CMaps. Anti-DoS guard, not spec-mandated: the deepest
// chain litho bundles is UniJIS-UCS2-HW-H on UniJIS-UCS2-H, one level, and the
// bundled set holds no cycle; the cap keeps a corrupted bundle from recursing
// without end. It matches maxUseCMapChainDepth, the cap the compliance package
// puts on the embedded /UseCMap chain it resolves.
const maxPredefinedBaseDepth = 5

// notdefSpan is one notdefchar or notdefrange entry: every source code lo to
// hi maps to cid, which, unlike a cidrange's, does not increment.
type notdefSpan struct {
	lo, hi int64
	cid    int
}

// mappedCID returns the CID the cidchar and cidrange mappings of m and of its
// bases give code, ignoring notdef mappings, as veraPDF's CMap.toCID walks its
// cidMappings list before its notDefMappings list.
func (m *CIDMap) mappedCID(code int64) (int, bool) {
	if c, ok := m.charToCID[code]; ok {
		return c, true
	}
	for _, s := range m.spans {
		if code >= s.lo && code <= s.hi {
			return s.cid + int(code-s.lo), true // fits int: a span covers the 65536 codes of an Identity CMap
		}
	}
	for _, b := range m.bases {
		if c, ok := b.mappedCID(code); ok {
			return c, true
		}
	}
	return 0, false
}

// lookup returns the CID m maps code to, through its cid mappings or, failing
// those, its notdef mappings, and false when neither covers the code.
func (m *CIDMap) lookup(code int64) (int, bool) {
	if c, ok := m.mappedCID(code); ok {
		return c, true
	}
	for _, s := range m.notdefs {
		if code >= s.lo && code <= s.hi {
			return s.cid, true
		}
	}
	return 0, false
}

// cidMappings returns every code m's cidchar and cidrange mappings and its
// bases' give a CID, as [CIDMap.mappedCID] reads them: a mapping of m
// overrides one of a base, and an earlier base one of a later base. Notdef
// mappings are left out. It reports false for a map holding a span, the
// Identity CMaps, whose 65536 codes no caller enumerates.
func (m *CIDMap) cidMappings() (map[int64]int, bool) {
	if len(m.spans) > 0 {
		return nil, false
	}
	out := make(map[int64]int, len(m.charToCID))
	for _, b := range slices.Backward(m.bases) {
		bm, ok := b.cidMappings()
		if !ok {
			return nil, false
		}
		maps.Copy(out, bm)
	}
	maps.Copy(out, m.charToCID)
	return out, true
}

// WMode returns the writing mode the CMap declares: 0 horizontal, 1 vertical
// (ISO 32000-1, 9.7.5.3, Table 120). A predefined CMap's comes from its
// bundled data, and the -V CMaps declare 1.
func (m *CIDMap) WMode() int {
	return m.wmode
}

// addBase makes b a base of m, consulted after the bases m already has, and
// appends b's codespaces and notdef mappings after m's, as veraPDF's
// CMap.useCMap appends the other CMap's lists. A nil b is a base litho cannot
// resolve, which leaves m's unmapped codes unknown.
func (m *CIDMap) addBase(b *CIDMap) {
	if b == nil {
		m.unmappedNotdef = false
		return
	}
	m.bases = append(m.bases, b)
	m.notdefs = append(m.notdefs, b.notdefs...)
	if b.cs != nil {
		m.cs.CodeSpaceRanges = append(m.cs.CodeSpaceRanges, b.cs.CodeSpaceRanges...)
	}
	if !b.unmappedNotdef {
		m.unmappedNotdef = false
	}
}

// inCodeSpaceBytewise reports whether candidate is a code of one of c's
// codespace ranges, read byte by byte: each byte lies between the range's low
// and high bytes at that position (Adobe Technical Note #5014,
// begincodespacerange; ISO 32000-1, 9.7.6.2), as veraPDF's CodeSpace.contains
// reads it. A comparison of the whole sequence would take <E0E0E0> for a code
// of the UTF-8 range <E08080> <EFBFBF>, whose second byte stops at BF.
func (c *CMap) inCodeSpaceBytewise(candidate []byte) bool {
	for _, cs := range c.CodeSpaceRanges {
		if len(cs.Low) != len(candidate) || len(cs.High) != len(candidate) {
			continue
		}
		inside := true
		for i, b := range candidate {
			if b < cs.Low[i] || b > cs.High[i] {
				inside = false
				break
			}
		}
		if inside {
			return true
		}
	}
	return false
}

// useBase adds b to m: its map as a base when resolved, and, when it names a
// base litho cannot resolve, marks m's unmapped codes unknown. The zero Base
// changes nothing.
func (m *CIDMap) useBase(b Base) {
	switch {
	case b.Map != nil:
		m.addBase(b.Map)
	case b.Unresolved:
		m.unmappedNotdef = false
	}
}

// baseSet selects the bundled copies a predefined CMap name resolves through.
type baseSet uint8

const (
	// pdfjsBases reads PDF.js's binary CMaps, which text extraction keeps to
	// so that it stays equal to PDF.js. A usecmap naming a base PDF.js ships
	// none of reads veraPDF's copy ([NamedBase]).
	pdfjsBases baseSet = iota
	// conformanceBases reads the copies veraPDF 1.30.2 bundles where their
	// code-to-CID content differs from PDF.js's or PDF.js ships none, and
	// PDF.js's copies elsewhere; see [ConformanceCIDBase].
	conformanceBases
)

// predefinedBaseCache and conformanceBaseCache cache [PredefinedCIDBase] and
// [ConformanceCIDBase] by CMap name. Only a name litho resolves is stored, so
// each cache is bounded by the bundled set however many names documents
// invent.
//
//nolint:gochecknoglobals // process-wide caches of immutable parsed CMaps.
var predefinedBaseCache, conformanceBaseCache sync.Map

// PredefinedCIDBase returns the complete code-to-CID mapping of the predefined
// CMap name, for use as the base of a CMap that names it: its cidchar and
// cidrange mappings, its notdef mappings, its codespaces, and those of the
// base its own usecmap names, followed through the bundled CMaps. Identity-H
// and Identity-V map every two-byte code to itself (ISO 32000-1, 9.7.5.2,
// Table 118).
//
// It returns nil when litho bundles no such CMap or cannot follow its chain,
// so the caller treats the codes the base would map as unknown. The map is
// read-only and safe for concurrent use.
func PredefinedCIDBase(name string) *CIDMap {
	return predefinedCIDBase(pdfjsBases, name, 0)
}

// ConformanceCIDBase is [PredefinedCIDBase] read the way veraPDF 1.30.2
// reads a predefined CMap, for the conformance checks: through the CMap file
// veraPDF bundles (PDCMap.getCMapFile), whose code-to-CID mappings decide the
// glyphs ISO 19005-2, 6.2.11.4.1 and 6.2.11.8 judge. Where that file maps what
// PDF.js's copy maps, PDF.js's copy is read; the verapdfcmaps directory holds
// the others, see verapdf_cmaps_sources.go. Those are the character-collection
// CMaps of the newest supplements (Adobe-Japan1-7, Adobe-KR-9 and the like)
// and UniAKR-UTF16-H, which PDF.js ships none of, and eight Unicode CMaps
// Adobe has revised since PDF.js copied them. A usecmap chain resolves
// through the same set. Text extraction keeps to [PredefinedCIDBase], and
// reads this set only for a usecmap naming a CMap PDF.js ships none of
// ([NamedBase]).
//
// It returns nil when neither litho nor veraPDF bundles the CMap or litho
// cannot follow its chain. The map is read-only and safe for concurrent use.
func ConformanceCIDBase(name string) *CIDMap {
	return predefinedCIDBase(conformanceBases, name, 0)
}

// NamedBase resolves the base CMap a usecmap operator or a /UseCMap name
// names. A name [PredefinedCIDBase] resolves gives that base. A name litho
// cannot resolve names no CMap a reader holds, which veraPDF's CMapParser
// ignores (setUsesNonPredefinedCMap), so the zero [Base] comes back and the
// using CMap's unmapped codes still select CID 0: ISO 32000-1, 9.7.5.2 makes
// Table 118 the predefined set, and litho bundles every name in it.
//
// A name PDF.js ships no copy of but veraPDF 1.30 does, Adobe's
// character-collection CMaps of the newest supplements (Adobe-Japan1-7,
// Adobe-KR-9 and the like) and UniAKR-UTF16-H, resolves through veraPDF's
// copy ([ConformanceCIDBase]): it is the CMap Adobe publishes under the name
// the usecmap gives (9.7.5.4), and PDF.js holds no reading of it to stay
// equal to. Any other Adobe name litho cannot resolve comes back unresolved,
// and the codes it could map stay unknown.
//
// # Corpus evidence
//
//   - litho cmapverabase_2b.pdf: K4's code 0007, which its Adobe-GB1-6 base
//     sends to CID 7, read as nothing and now reads "&" through
//     Adobe-GB1-UCS2 (ISO 32000-1, 9.10.2). No file of the five text corpora
//     changes, and no validation or remediation result moves on the litho
//     corpus.
func NamedBase(name string) Base {
	return namedBase(pdfjsBases, name, 0)
}

// ConformanceNamedBase is [NamedBase] over [ConformanceCIDBase], for the
// conformance checks: a name veraPDF 1.30.2 bundles resolves to the CMap
// veraPDF loads for it (CMapParser runs PDCMap.getCMapFile on a usecmap
// operand), and any other name litho cannot resolve gives the zero [Base],
// which veraPDF ignores (setUsesNonPredefinedCMap).
func ConformanceNamedBase(name string) Base {
	return namedBase(conformanceBases, name, 0)
}

func namedBase(set baseSet, name string, depth int) Base {
	if depth > maxPredefinedBaseDepth {
		return Base{Unresolved: true}
	}
	if m := predefinedCIDBase(set, name, depth); m != nil {
		return Base{Map: m}
	}
	if set == pdfjsBases && (strings.HasPrefix(name, "Adobe-") || strings.HasPrefix(name, "UniAKR-")) {
		if m := predefinedCIDBase(conformanceBases, name, depth); m != nil {
			return Base{Map: m}
		}
		return Base{Unresolved: true}
	}
	return Base{}
}

func predefinedCIDBase(set baseSet, name string, depth int) *CIDMap {
	if depth > maxPredefinedBaseDepth {
		return nil
	}
	cache := &predefinedBaseCache
	if set == conformanceBases {
		cache = &conformanceBaseCache
	}
	if cached, ok := cache.Load(name); ok {
		m, _ := cached.(*CIDMap)
		return m
	}
	m := loadPredefinedCIDBase(set, name, depth)
	if m != nil {
		cache.Store(name, m)
	}
	return m
}

func loadPredefinedCIDBase(set baseSet, name string, depth int) *CIDMap {
	if name == "Identity-H" || name == "Identity-V" {
		m := &CIDMap{
			spans:          []cidSpan{{lo: 0, hi: 0xFFFF, cid: 0}},
			cs:             &CMap{CodeSpaceRanges: []CodeSpaceRange{{Low: []byte{0x00, 0x00}, High: []byte{0xFF, 0xFF}}}},
			unmappedNotdef: true,
		}
		if name == "Identity-V" {
			m.wmode = 1
		}
		return m
	}
	if set == conformanceBases {
		if m, ok := loadVeraPDFCIDBase(name, depth); ok {
			return m
		}
	}
	path := "bcmaps/" + name + ".bcmap"
	if name == "" || !fs.ValidPath(path) {
		return nil
	}
	data, err := bcmapFS.ReadFile(path)
	if err != nil {
		return nil
	}
	prog, err := parseBCMapCIDProgram(data)
	if err != nil {
		return nil
	}
	m := &CIDMap{
		charToCID:      prog.charToCID,
		notdefs:        prog.notdefs,
		cs:             &CMap{CodeSpaceRanges: prog.codespaces},
		unmappedNotdef: true,
		wmode:          prog.wmode,
	}
	if prog.useCMap != "" {
		m.addBase(predefinedCIDBase(set, prog.useCMap, depth+1))
	}
	if !m.unmappedNotdef || len(m.notdefs) > maxNotdefEntries {
		return nil
	}
	return m
}

// bcmapCIDProgram is the code-to-CID content of one binary CMap: its cidchar
// and cidrange mappings, notdef mappings, codespaces, the base it names, and
// its writing mode.
type bcmapCIDProgram struct {
	charToCID  map[int64]int
	notdefs    []notdefSpan
	codespaces []CodeSpaceRange
	useCMap    string
	wmode      int
}

// parseBCMapCIDProgram decodes the code-to-CID records of a binary CMap, the
// format PDF.js defines: codespace (type 0), notdef (type 1), cidchar (type
// 2) and cidrange (type 3) records, and the usecmap metadata record (type 7,
// subtype 1). bfchar and bfrange records map to Unicode and are skipped.
func parseBCMapCIDProgram(data []byte) (*bcmapCIDProgram, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty bcmap data")
	}
	// Position 1 skips the header byte (type<<1 | wmode), whose low bit is
	// the CMap's /WMode, as PDF.js's BinaryCMapReader reads it.
	s := &bcmapStream{data: data, pos: 1}
	prog := &bcmapCIDProgram{charToCID: make(map[int64]int, 8192), wmode: int(data[0] & 1)} //nolint:mnd // typical CMap size
	for s.pos < len(s.data) {
		tag, tagErr := s.readByte()
		if tagErr != nil {
			break
		}
		recType := int(tag >> 5)
		if recType == 7 {
			const useCMapSubtype = 1
			if tag&0x1F == useCMapSubtype {
				name, err := s.readString()
				if err != nil {
					return nil, fmt.Errorf("bcmap usecmap: %w", err)
				}
				prog.useCMap = name
				continue
			}
			if err := s.skipString(); err != nil {
				return nil, fmt.Errorf("bcmap metadata: %w", err)
			}
			continue
		}
		seq := tag&0x10 != 0
		dataSize := int(tag & 0x0F)
		count, err := s.readCount()
		if err != nil {
			return nil, fmt.Errorf("bcmap record count: %w", err)
		}
		switch recType {
		case 0:
			var cs []CodeSpaceRange
			if cs, err = s.collectCodespace(count, dataSize); err == nil {
				prog.codespaces = append(prog.codespaces, cs...)
			}
		case 1:
			prog.notdefs, err = s.collectNotdef(count, dataSize, prog.notdefs)
		case 2:
			err = s.collectCIDChar(count, dataSize, seq, prog.charToCID)
		case 3:
			err = s.collectCIDRange(count, dataSize, seq, prog.charToCID)
		case 4:
			err = s.advanceBFChar(count, dataSize, seq)
		case 5:
			err = s.advanceBFRange(count, dataSize, seq)
		default:
			err = fmt.Errorf("unknown record type %d", recType)
		}
		if err != nil {
			return nil, fmt.Errorf("bcmap record type %d: %w", recType, err)
		}
	}
	return prog, nil //nolint:nilerr // A truncated bcmap ends at its last whole record.
}

// readString reads a VLQ-length-prefixed string, each character a VLQ number,
// as PDF.js's BinaryCMapReader readString does.
func (s *bcmapStream) readString() (string, error) {
	length, err := s.readCount()
	if err != nil {
		return "", fmt.Errorf("reading a string length: %w", err)
	}
	out := make([]rune, 0, length)
	for range length {
		c, err := s.readNumber()
		if err != nil {
			return "", err
		}
		out = append(out, rune(c)) //nolint:gosec // a CMap name character; an out-of-range value only spoils the name.
	}
	return string(out), nil
}

// collectNotdef decodes count notdef entries (type 1), each a code range and
// the one CID every code in it maps to, appending them to dst. The ranges are
// delta-encoded as the codespace records are.
func (s *bcmapStream) collectNotdef(count, dataSize int, dst []notdefSpan) ([]notdefSpan, error) {
	var end int64
	for i := range count {
		var start int64
		if i == 0 {
			v, err := s.readHex(dataSize)
			if err != nil {
				return nil, err
			}
			start = v
		} else {
			delta, err := s.readNumber()
			if err != nil {
				return nil, err
			}
			start = end + 1 + delta
		}
		delta, err := s.readNumber()
		if err != nil {
			return nil, err
		}
		end = start + delta
		cid, err := s.readNumber()
		if err != nil {
			return nil, err
		}
		if len(dst) >= maxNotdefEntries {
			return nil, fmt.Errorf("more than %d notdef entries", maxNotdefEntries)
		}
		dst = append(dst, notdefSpan{lo: start, hi: end, cid: int(cid)}) //nolint:gosec // fits int: bundled CMaps hold CIDs below 65536 (ISO 32000-1, Annex C).
	}
	return dst, nil
}
