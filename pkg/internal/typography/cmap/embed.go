package cmap

import (
	"bytes"
	"context"
	"fmt"
	"maps"
)

// maxEmbeddedCIDRanges caps how many begincidrange entries an embedded
// predefined CMap may take. It is an anti-bloat guard, not spec-mandated: H
// compresses its 6879 codes into 118 entries and 2.5 KB, while UniJIS-UCS2-H
// barely compresses at all, taking 8533 entries and 166 KB. Pasting that much
// into a document to settle one rule is a worse outcome than leaving the rule
// reported, so a CMap past this cap keeps its name.
const maxEmbeddedCIDRanges = 1024

// EmbedPredefined renders a bundled predefined CMap as a CMap program that can
// be written into a file as a stream, so a reference to it satisfies the
// embedding requirement of ISO 19005-1, 6.3.3.3.
//
// It returns false, writing nothing, whenever the result would not be a
// faithful copy of the named CMap:
//
//   - the name resolves to no bundled data. A bundled .bcmap file is not
//     enough: V ships as a file but defers to H through usecmap, which the
//     reader skips, so it yields no mapping at all.
//   - the codespace mixes code widths. [CIDCMap] keys codes by their integer
//     value, so a one-byte <41> and a two-byte <0041> collide and the merged
//     map would silently map one to the other's CID.
//   - the program does not read back as exactly the mapping it was built from.
//
// The codespace is carried from the bundled data rather than defaulted: H
// spans <2121> to <7E7E>, and a wider one would change how bytes outside it
// decode.
func EmbedPredefined(ctx context.Context, name string) ([]byte, bool, error) {
	m := LoadPredefinedCIDMap(name)
	if m == nil || m.cs == nil || len(m.charToCID) == 0 {
		return nil, false, nil
	}

	width, spans, ok := uniformCodeSpaces(m.cs.CodeSpaceRanges)
	if !ok {
		return nil, false, nil
	}
	runs := compressCID(m.charToCID)
	if len(runs) == 0 || len(runs) > maxEmbeddedCIDRanges {
		return nil, false, nil
	}

	// The tabulated CIDSystemInfo, never a guess. ISO 19005-1, 6.3.3.1 compares
	// the CMap's Registry, Ordering and Supplement against the CIDFont's, so
	// declaring Supplement 0 where Adobe tabulates 1 would settle the embedding
	// rule and trip the consistency one. This is the table the checker reads
	// (see fonts_cmap.go), so the written stream cannot disagree with it.
	// Part 1 is written against PDF 1.4, hence the PDF 1.x column.
	reg, ord, sup, ok := PredefinedCMapCIDSystemInfo(name, false)
	if !ok {
		return nil, false, nil
	}

	var buf bytes.Buffer
	buf.WriteString("%!PS-Adobe-3.0 Resource-CMap\n")
	buf.WriteString("/CIDInit /ProcSet findresource begin\n12 dict begin\nbegincmap\n")
	fmt.Fprintf(&buf, "/CIDSystemInfo 3 dict dup begin /Registry (%s) def /Ordering (%s) def /Supplement %d def end def\n",
		reg, ord, sup)
	fmt.Fprintf(&buf, "/CMapName /%s def\n/CMapType 1 def\n/WMode 0 def\n", name)
	writeCodeSpaces(&buf, spans, width)
	writeCIDRanges(&buf, runs, width)
	buf.WriteString("endcmap\n")
	buf.WriteString("CMapName currentdict /CMap defineresource pop\n")
	buf.WriteString("end end\n") //nolint:dupword // PostScript: one `end` closes the CMap dict, the other the resource category (ISO 32000-1, 9.7.5.4).

	// The program stands only if it maps exactly what the bundled CMap maps.
	// A validator checks that /UseCMap points at a stream, never that the
	// stream is right, so nothing downstream would catch a short copy.
	back, err := ParseCID(ctx, buf.Bytes())
	if err != nil {
		return nil, false, fmt.Errorf("re-reading the generated %s CMap: %w", name, err)
	}
	if !maps.Equal(back.Mapping, m.charToCID) {
		return nil, false, nil
	}
	return buf.Bytes(), true, nil
}

// uniformCodeSpaces reports the single code width the ranges share, with the
// ranges as integer intervals. It reports false when they mix widths, declare
// none, or use a code wider than [maxCodeSpaceBytes].
func uniformCodeSpaces(ranges []CodeSpaceRange) (int, []codeSpan, bool) {
	if len(ranges) == 0 {
		return 0, nil, false
	}
	width := 0
	spans := make([]codeSpan, 0, len(ranges))
	for _, r := range ranges {
		if len(r.Low) != len(r.High) || len(r.Low) == 0 || len(r.Low) > maxCodeSpaceBytes {
			return 0, nil, false
		}
		if width == 0 {
			width = len(r.Low)
		}
		if len(r.Low) != width {
			return 0, nil, false
		}
		spans = append(spans, codeSpan{lo: byteValue(r.Low), hi: byteValue(r.High)})
	}
	return width, mergeSpans(spans), true
}
