package cmap

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"regexp"
	"slices"
)

// maxFlattenedCodes caps how many codes one flattened CMap may map. It is an
// anti-DoS guard, not spec-mandated: [FlattenUseCMap] accepts a codespace of
// at most two bytes, which spans 65536 codes, so no real CMap reaches it while
// a crafted chain of ranges could otherwise expand without bound.
const maxFlattenedCodes = 1 << 16

// maxCIDRangeEntriesPerGroup is the entry limit of one begincidrange group:
// Adobe Technical Note #5411, 1.4.1, "the number of entries in each group of
// mappings cannot exceed 100".
const maxCIDRangeEntriesPerGroup = 100

// maxCodeSpaceBytes is the widest code [FlattenUseCMap] rewrites. [CIDCMap] keys
// codes by big-endian integer value, so a one-byte <41> and a two-byte <0041>
// collide; requiring one width across the whole chain keeps the merged map
// unambiguous.
const maxCodeSpaceBytes = 2

var (
	codeSpaceGroup = regexp.MustCompile(`\d+\s+begincodespacerange[\s\S]*?endcodespacerange`)
	cidRangeGroup  = regexp.MustCompile(`\d+\s+begincidrange[\s\S]*?endcidrange`)
	cidCharGroup   = regexp.MustCompile(`\d+\s+begincidchar[\s\S]*?endcidchar`)
	useCMapOp      = regexp.MustCompile(`/[^\s/<>\[\]()]+\s+usecmap`)
)

// cidSpan is one begincidrange entry: the source codes lo to hi map to
// consecutive CIDs starting at cid.
type cidSpan struct{ lo, hi, cid int }

// codeSpan is one begincodespacerange entry as an integer interval.
type codeSpan struct{ lo, hi int }

// FlattenUseCMap inlines the mappings of the CMaps a CMap builds on, returning
// a program that needs no /UseCMap reference.
//
// ISO 32000-1, 9.7.5.2, Table 118 names the CMaps a conforming file may refer
// to, and ISO 19005-2, 6.2.11.3.3 test 3 (with its 6.2.10.3.3, 7.21.3.3 and
// 8.4.5.4 restatements) rejects a reference to any other. Inlining settles
// that at its source: the reference goes because nothing is left to inherit.
//
// bases are ordered nearest first, as the chain is walked. A base supplies
// only the codes the CMap above it does not define, so the using CMap wins
// every disagreement, matching how a reader resolves usecmap.
//
// Ranges are emitted split at first-byte boundaries, the shape Adobe's own
// CMaps use. A range crossing one is what Adobe Technical Note #5411, 1.4.1
// rules invalid and what a validator holding to it clamps, which would leave
// most of the merged mapping unreachable (see [ConformBfRanges]).
//
// It returns the rewrite and true only when re-reading it yields exactly the
// merged mapping and no usecmap survives. Otherwise it returns using and
// false, so a chain it cannot settle is left as written.
func FlattenUseCMap(ctx context.Context, using []byte, bases [][]byte) ([]byte, bool, error) {
	if len(using) == 0 || len(bases) == 0 {
		return using, false, nil
	}

	// Fold the chain weakest first: the furthest base is overwritten by every
	// CMap above it, and the using CMap last of all.
	merged := make(map[int]int)
	for i := len(bases) - 1; i >= 0; i-- {
		if err := ctx.Err(); err != nil {
			return using, false, err
		}
		base, err := ParseCID(ctx, bases[i])
		if err != nil {
			return using, false, fmt.Errorf("parsing base CMap %d of the usecmap chain: %w", i, err)
		}
		maps.Copy(merged, base.Mapping)
	}
	top, err := ParseCID(ctx, using)
	if err != nil {
		return using, false, fmt.Errorf("parsing the CMap that holds the usecmap reference: %w", err)
	}
	maps.Copy(merged, top.Mapping)
	if len(merged) == 0 || len(merged) > maxFlattenedCodes {
		return using, false, nil
	}

	width, spaces, ok := mergedCodeSpaces(ctx, using, bases)
	if !ok {
		return using, false, nil
	}

	anchor := codeSpaceGroup.FindIndex(using)
	if anchor == nil {
		// Nothing marks where the code structure is declared, so there is no
		// safe place to write the merged one.
		return using, false, nil
	}

	var body bytes.Buffer
	writeCodeSpaces(&body, spaces, width)
	writeCIDRanges(&body, compressCID(merged), width)

	out := make([]byte, 0, len(using)+body.Len())
	out = append(out, stripGroups(using[:anchor[0]])...)
	out = append(out, body.Bytes()...)
	out = append(out, stripGroups(using[anchor[1]:])...)

	// The rewrite stands only if a reader sees exactly the mapping the chain
	// produced. Anything less would silently unmap glyphs, and validating the
	// output cannot catch that: a validator checks only that the reference is
	// gone.
	back, err := ParseCID(ctx, out)
	if err != nil || !maps.Equal(back.Mapping, merged) {
		return using, false, nil
	}
	full, err := Parse(ctx, out)
	if err != nil || len(full.CodeSpaceRanges) != len(spaces) {
		return using, false, nil
	}
	if bytes.Contains(out, []byte("usecmap")) {
		return using, false, nil
	}
	return out, true, nil
}

// stripGroups removes every codespace, cidrange and cidchar group and any
// usecmap invocation, leaving the CMap's identity (its name, CIDSystemInfo,
// WMode and comments) byte for byte as written.
func stripGroups(data []byte) []byte {
	out := codeSpaceGroup.ReplaceAll(data, nil)
	out = cidRangeGroup.ReplaceAll(out, nil)
	out = cidCharGroup.ReplaceAll(out, nil)
	return useCMapOp.ReplaceAll(out, nil)
}

// mergedCodeSpaces unions the codespace ranges of the whole chain. It reports
// false when the chain mixes code widths or declares none, because a merged
// map keyed by integer value cannot then say which width a code was written
// at.
func mergedCodeSpaces(ctx context.Context, using []byte, bases [][]byte) (int, []codeSpan, bool) {
	width := 0
	var spans []codeSpan
	for _, data := range append([][]byte{using}, bases...) {
		parsed, err := Parse(ctx, data)
		if err != nil || parsed == nil {
			return 0, nil, false
		}
		for _, r := range parsed.CodeSpaceRanges {
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
	}
	if width == 0 || len(spans) == 0 {
		return 0, nil, false
	}
	return width, mergeSpans(spans), true
}

// byteValue reads a big-endian code.
func byteValue(b []byte) int {
	v := 0
	for _, c := range b {
		v = v<<8 | int(c)
	}
	return v
}

// mergeSpans collapses overlapping and adjacent intervals.
func mergeSpans(in []codeSpan) []codeSpan {
	slices.SortFunc(in, func(a, b codeSpan) int { return a.lo - b.lo })
	out := make([]codeSpan, 0, len(in))
	for _, s := range in {
		if n := len(out); n > 0 && s.lo <= out[n-1].hi+1 {
			out[n-1].hi = max(out[n-1].hi, s.hi)
			continue
		}
		out = append(out, s)
	}
	return out
}

// compressCID turns an expanded code-to-CID map back into ranges, breaking a
// run wherever the codes stop being consecutive, the CIDs stop being
// consecutive, or the code enters a new first-byte block.
func compressCID(m map[int]int) []cidSpan {
	codes := make([]int, 0, len(m))
	for c := range m {
		codes = append(codes, c)
	}
	slices.Sort(codes)

	out := make([]cidSpan, 0, len(codes))
	for i, c := range codes {
		if i > 0 && c == codes[i-1]+1 && m[c] == m[codes[i-1]]+1 && c>>8 == codes[i-1]>>8 {
			out[len(out)-1].hi = c
			continue
		}
		out = append(out, cidSpan{lo: c, hi: c, cid: m[c]})
	}
	return out
}

func writeCodeSpaces(buf *bytes.Buffer, spans []codeSpan, width int) {
	fmt.Fprintf(buf, "%d begincodespacerange\n", len(spans))
	for _, s := range spans {
		fmt.Fprintf(buf, "%s %s\n", hexCode(s.lo, width), hexCode(s.hi, width))
	}
	buf.WriteString("endcodespacerange\n")
}

func writeCIDRanges(buf *bytes.Buffer, spans []cidSpan, width int) {
	for i := 0; i < len(spans); i += maxCIDRangeEntriesPerGroup {
		end := min(i+maxCIDRangeEntriesPerGroup, len(spans))
		fmt.Fprintf(buf, "%d begincidrange\n", end-i)
		for _, s := range spans[i:end] {
			fmt.Fprintf(buf, "%s %s %d\n", hexCode(s.lo, width), hexCode(s.hi, width), s.cid)
		}
		buf.WriteString("endcidrange\n")
	}
}

// hexCode writes a code as the hex string of the chain's code width.
func hexCode(v, width int) string {
	return fmt.Sprintf("<%0*X>", width*2, v)
}
