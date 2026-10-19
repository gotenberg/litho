package cmap

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// maxCMapCodeBytes is the widest character code a CMap program may declare:
// Adobe Technical Note #5014, begincodespacerange, and ISO 32000-1, 9.7.6.2
// allow one to four bytes.
const maxCMapCodeBytes = 4

// maxEntriesPerGroup is the entry limit of one begincodespacerange,
// begincidrange or beginnotdefrange group: Adobe Technical Note #5411, 1.4.1,
// "the number of entries in each group of mappings cannot exceed 100".
const maxEntriesPerGroup = 100

// EncodingProgram returns a CMap program for the CMap name a Type 0 font's
// /Encoding names, with the CIDSystemInfo and writing mode to state in the
// stream dictionary, for a repair to embed. ISO 19005-1, 6.3.3.3 requires
// every CMap but Identity-H and Identity-V embedded, the predefined CMaps of
// ISO 32000-1, 9.7.5.2, Table 118 included, so under PDF/A-1 the name alone
// fails whatever it denotes; every later part and PDF/UA require a name
// outside the table embedded.
//
// The program is the copy the conformance checks read under the name
// ([LoadConformanceCIDMap]), so every code keeps the CID it had while the
// font named the CMap:
//
//   - where veraPDF 1.30.2 bundles a file the verapdfcmaps resources hold, it
//     is that file byte for byte ([ConformanceProgram]), with the
//     CIDSystemInfo the file declares.
//   - otherwise it is rendered from the bundled data: the codespaces exactly
//     as declared, every cidchar and cidrange mapping of the CMap and of the
//     bases its usecmap chain names, flattened so that no name is left to
//     resolve, and its notdef mappings in the order a reader consults them.
//     The CIDSystemInfo is the one Table 118's generation of the name
//     carries ([PredefinedCMapCIDSystemInfo], PDF 1.x column, since part 1 is
//     written against PDF 1.4), or, for a character collection's own CMap
//     outside the table (Adobe-Korea1-2), the one its name spells
//     ([collectionCMapCIDSystemInfo]).
//
// A -V CMap of veraPDF's set, which runs usecmap on its -H base, takes the
// rendered path, flattened over veraPDF's base.
//
// It returns false, writing nothing, when the name resolves to no bundled
// data, names an Identity CMap, has no tabulated CIDSystemInfo, maps a code
// whose width the codespaces do not settle (one value read as codes of two
// widths, or a code outside every codespace), or when the program does not
// read back as exactly the CMap it was built from. Unlike [EmbedPredefined]
// it takes no range cap: the program is the whole of the font's encoding, so
// a short copy would change what the font shows, and its size is bounded by
// litho's own bundled data.
func EncodingProgram(ctx context.Context, name string) (ConformanceCMap, bool, error) {
	if name == "Identity-H" || name == "Identity-V" {
		return ConformanceCMap{}, false, nil
	}
	if prog, ok, err := ConformanceProgram(name); err != nil || ok {
		return prog, ok, err
	}
	reg, ord, sup, ok := PredefinedCMapCIDSystemInfo(name, false)
	if !ok {
		reg, ord, sup, ok = collectionCMapCIDSystemInfo(name)
	}
	if !ok {
		return ConformanceCMap{}, false, nil
	}
	m := LoadConformanceCIDMap(name)
	if m == nil || m.cs == nil {
		return ConformanceCMap{}, false, nil
	}
	data, ok, err := renderEncoding(ctx, name, m, reg, ord, sup)
	if err != nil || !ok {
		return ConformanceCMap{}, false, err
	}
	return ConformanceCMap{
		Program:    data,
		Registry:   reg,
		Ordering:   ord,
		Supplement: sup,
		WMode:      m.wmode,
	}, true, nil
}

// collectionCMapCIDSystemInfo returns the CIDSystemInfo of a character
// collection's own CMap, named Adobe-<Ordering>-<Supplement> (Adobe-Korea1-2,
// Adobe-Japan1-6), which maps codes to the CIDs of that collection and lies
// outside ISO 32000-1, 9.7.5.2, Table 118. ok is false unless veraPDF 1.30.2
// bundles the name: a supplement no reader holds names no CMap, and the
// Adobe-<Ordering>-UCS2 CMaps map CIDs to Unicode rather than codes to CIDs.
//
// The name spells the CIDSystemInfo, which [PredefinedCMapCIDSystemInfo]
// does not tabulate since the name is not a Table 118 one: every such file
// veraPDF bundles declares Registry Adobe and the Ordering and Supplement of
// its name in its /CIDSystemInfo, and none runs usecmap.
func collectionCMapCIDSystemInfo(name string) (string, string, int, bool) {
	if !veraPDFBundledCMaps[name] {
		return "", "", 0, false
	}
	registry, rest, ok := strings.Cut(name, "-")
	if !ok || registry != "Adobe" {
		return "", "", 0, false
	}
	ordering, supplementText, ok := strings.Cut(rest, "-")
	if !ok || ordering == "" {
		return "", "", 0, false
	}
	supplement, err := strconv.Atoi(supplementText)
	if err != nil || supplement < 0 {
		return "", "", 0, false
	}
	return registry, ordering, supplement, true
}

// widthSpan is a run of codes of one byte width: a cidrange entry when cid
// increments along it, a notdefrange entry when it does not.
type widthSpan struct {
	lo, hi, cid, width int
}

// renderEncoding writes m as a self-contained CMap program named name and
// reports false when the program would not be a faithful copy of it.
func renderEncoding(ctx context.Context, name string, m *CIDMap, reg, ord string, sup int) ([]byte, bool, error) {
	codespaces := distinctCodeSpaces(m.cs.CodeSpaceRanges)
	if len(codespaces) == 0 {
		return nil, false, nil
	}
	cs := &CMap{CodeSpaceRanges: codespaces}
	charToCID, ok := m.cidMappings()
	if !ok || len(charToCID) == 0 {
		return nil, false, nil
	}
	runs, ok := cidRunsByWidth(cs, charToCID)
	if !ok {
		return nil, false, nil
	}
	notdefs := make([]widthSpan, 0, len(m.notdefs))
	for _, n := range m.notdefs {
		lo, okLo := codeWidth(cs, n.lo)
		hi, okHi := codeWidth(cs, n.hi)
		if !okLo || !okHi || lo != hi {
			return nil, false, nil
		}
		notdefs = append(notdefs, widthSpan{lo: n.lo, hi: n.hi, cid: n.cid, width: lo})
	}

	var buf bytes.Buffer
	buf.WriteString("%!PS-Adobe-3.0 Resource-CMap\n")
	buf.WriteString("/CIDInit /ProcSet findresource begin\n12 dict begin\nbegincmap\n")
	fmt.Fprintf(&buf, "/CIDSystemInfo 3 dict dup begin /Registry (%s) def /Ordering (%s) def /Supplement %d def end def\n",
		reg, ord, sup)
	fmt.Fprintf(&buf, "/CMapName /%s def\n/CMapType 1 def\n/WMode %d def\n", name, m.wmode)
	writeGroups(&buf, "codespacerange", len(codespaces), func(i int) string {
		return fmt.Sprintf("<%X> <%X>", codespaces[i].Low, codespaces[i].High)
	})
	writeGroups(&buf, "cidrange", len(runs), func(i int) string {
		r := runs[i]
		return fmt.Sprintf("%s %s %d", hexCode(r.lo, r.width), hexCode(r.hi, r.width), r.cid)
	})
	writeGroups(&buf, "notdefrange", len(notdefs), func(i int) string {
		n := notdefs[i]
		return fmt.Sprintf("%s %s %d", hexCode(n.lo, n.width), hexCode(n.hi, n.width), n.cid)
	})
	buf.WriteString("endcmap\n")
	buf.WriteString("CMapName currentdict /CMap defineresource pop\n")
	buf.WriteString("end end\n") //nolint:dupword // PostScript: one `end` closes the CMap dict, the other the resource category (ISO 32000-1, 9.7.5.4).

	// The program stands only if it reads back as the CMap it copies. A
	// validator checks that /Encoding points at a CMap stream, never that the
	// stream maps what the name did, so nothing downstream would catch a
	// short or shifted copy.
	back := parseCIDMapOver(ctx, buf.Bytes(), Base{}, func(string) Base { return Base{Unresolved: true} })
	if err := ctx.Err(); err != nil {
		return nil, false, fmt.Errorf("re-reading the rendered %s CMap: %w", name, err)
	}
	if back == nil || !back.unmappedNotdef || len(back.bases) > 0 ||
		!maps.Equal(back.charToCID, charToCID) ||
		!slices.Equal(back.notdefs, m.notdefs) ||
		!slices.EqualFunc(back.cs.CodeSpaceRanges, codespaces, sameCodeSpace) ||
		programWMode(buf.Bytes()) != m.wmode {
		return nil, false, nil
	}
	return buf.Bytes(), true, nil
}

// distinctCodeSpaces returns ranges without repeats, in their first order. A
// -V CMap repeats its base's codespaces through usecmap, and a flattened
// program declares each once.
func distinctCodeSpaces(ranges []CodeSpaceRange) []CodeSpaceRange {
	out := make([]CodeSpaceRange, 0, len(ranges))
	for _, r := range ranges {
		if !slices.ContainsFunc(out, func(o CodeSpaceRange) bool { return sameCodeSpace(o, r) }) {
			out = append(out, r)
		}
	}
	return out
}

func sameCodeSpace(a, b CodeSpaceRange) bool {
	return bytes.Equal(a.Low, b.Low) && bytes.Equal(a.High, b.High)
}

// codeWidth returns the byte width of the code whose big-endian value is
// code: the one width at which its bytes lie in a codespace range, read byte
// by byte as a reader splits codes (Adobe Technical Note #5014,
// begincodespacerange). It reports false when no width, or more than one,
// does, since the value then names no code, or two.
func codeWidth(cs *CMap, code int) (int, bool) {
	width := 0
	for n := 1; n <= maxCMapCodeBytes; n++ {
		if n < maxCMapCodeBytes && code >= 1<<(8*n) {
			continue
		}
		if !cs.inCodeSpaceBytewise(intToBytes(code, n)) {
			continue
		}
		if width != 0 {
			return 0, false
		}
		width = n
	}
	return width, width != 0
}

// cidRunsByWidth turns a code-to-CID map into cidrange entries, each of one
// code width, breaking a run wherever the codes or the CIDs stop being
// consecutive or the code enters a new block of its last byte: a range
// crossing one is what Adobe Technical Note #5411, 1.4.1 rules invalid. It
// reports false when a code's width is not settled ([codeWidth]).
func cidRunsByWidth(cs *CMap, charToCID map[int]int) ([]widthSpan, bool) {
	codes := slices.Sorted(maps.Keys(charToCID))
	out := make([]widthSpan, 0, len(codes))
	for _, c := range codes {
		w, ok := codeWidth(cs, c)
		if !ok {
			return nil, false
		}
		if n := len(out); n > 0 {
			last := &out[n-1]
			if last.width == w && c == last.hi+1 && charToCID[c] == last.cid+(c-last.lo) && c>>8 == last.hi>>8 {
				last.hi = c
				continue
			}
		}
		out = append(out, widthSpan{lo: c, hi: c, cid: charToCID[c], width: w})
	}
	return out, true
}

// writeGroups writes count entries as begin<kind>/end<kind> groups of at most
// [maxEntriesPerGroup] entries, entry giving the text of each.
func writeGroups(buf *bytes.Buffer, kind string, count int, entry func(int) string) {
	for i := 0; i < count; i += maxEntriesPerGroup {
		end := min(i+maxEntriesPerGroup, count)
		fmt.Fprintf(buf, "%d begin%s\n", end-i, kind)
		for j := i; j < end; j++ {
			buf.WriteString(entry(j))
			buf.WriteByte('\n')
		}
		fmt.Fprintf(buf, "end%s\n", kind)
	}
}
