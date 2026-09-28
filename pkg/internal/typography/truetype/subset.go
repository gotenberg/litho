package truetype

import (
	"context"
	"encoding/binary"
	"fmt"
	"maps"
	"math"
	"slices"
)

// subsetTables are the tables a subset carries, in the tag order of the table
// directory. The glyph tables are rebuilt; the rest are global and copied. A
// table indexed by glyph ID that a PDF consumer does not read (hdmx, LTSH,
// kern, vmtx, GDEF, GPOS, GSUB, the bitmap and colour tables, the variation
// tables) is left out rather than renumbered: ISO 32000-1, 9.6.6 and 9.7.4
// have a conforming reader draw a TrueType glyph from glyf, loca, hmtx and
// cmap alone, and the text a content stream positions does not read kerning
// or layout features. The hinting tables (cvt, fpgm, prep, gasp) are global
// and stay, since the glyph programs the subset keeps call into them.
var subsetTables = []string{"OS/2", "cmap", "cvt ", "fpgm", "gasp", "glyf", "head", "hhea", "hmtx", "loca", "maxp", "name", "post", "prep"}

// Composite glyph flags (OpenType specification, glyf table, composite glyph
// description).
const (
	compositeArgsAreWords  = 0x0001
	compositeHaveScale     = 0x0008
	compositeMoreComponent = 0x0020
	compositeHaveXYScale   = 0x0040
	compositeHaveTwoByTwo  = 0x0080
)

// checkSumMagic is the value the whole font's checksum sums to once the head
// table's checkSumAdjustment is set (OpenType specification, head table).
const checkSumMagic = 0xB1B0AFBA

// subsetCtxCheckEvery sets how often the per-glyph loops poll ctx.Err().
const subsetCtxCheckEvery = 1024

// Subset returns a TrueType program holding glyph 0 (.notdef), the glyphs of
// keep and every glyph a kept composite glyph draws, renumbered in ascending
// order of their original glyph IDs, with the map from each original glyph ID
// kept to its new one.
//
// The glyf, loca, hmtx, hhea, maxp, head, cmap and post tables are rebuilt
// for the glyphs kept: every cmap subtable the program carries in format 0,
// 4, 6 or 12 keeps its (platform, encoding) pair and the codes it maps to a
// kept glyph, and the post table keeps their names. The tables listed in
// [subsetTables] that hold no glyph data are copied, and every other table is
// dropped. It fails on a program without glyf outlines, on one whose glyph
// tables are malformed, and on a glyph ID of keep past the program's glyph
// count.
func Subset(ctx context.Context, data []byte, keep []uint16) ([]byte, map[uint16]uint16, error) {
	if !IsSfnt(data) || string(data[:4]) == "OTTO" {
		return nil, nil, fmt.Errorf("%w: not a TrueType program with glyf outlines", ErrInvalidFont)
	}
	records, err := ParseTableDirectory(data)
	if err != nil {
		return nil, nil, err
	}
	table := func(tag string) ([]byte, error) { return TableData(data, records, tag) }
	head, err := table("head")
	if err != nil {
		return nil, nil, err
	}
	maxp, err := table("maxp")
	if err != nil {
		return nil, nil, err
	}
	hhea, err := table("hhea")
	if err != nil {
		return nil, nil, err
	}
	hmtx, err := table("hmtx")
	if err != nil {
		return nil, nil, err
	}
	loca, err := table("loca")
	if err != nil {
		return nil, nil, err
	}
	glyf, err := table("glyf")
	if err != nil {
		return nil, nil, err
	}
	// head is 54 bytes, maxp at least 6, hhea 36 (OpenType specification).
	if len(head) < 54 || len(maxp) < 6 || len(hhea) < 36 {
		return nil, nil, fmt.Errorf("%w: head, maxp or hhea table too short", ErrInvalidFont)
	}
	numGlyphs := int(binary.BigEndian.Uint16(maxp[4:6]))
	if numGlyphs == 0 {
		return nil, nil, fmt.Errorf("%w: no glyph", ErrInvalidFont)
	}
	offsets, err := glyphOffsets(loca, len(glyf), numGlyphs, binary.BigEndian.Uint16(head[50:52]) != 0)
	if err != nil {
		return nil, nil, err
	}
	glyph := func(gid int) []byte { return glyf[offsets[gid]:offsets[gid+1]] }

	kept := make([]bool, numGlyphs)
	kept[0] = true
	queue := []int{0}
	for _, gid := range keep {
		if int(gid) >= numGlyphs {
			return nil, nil, fmt.Errorf("%w: glyph %d past the program's %d glyphs", ErrInvalidFont, gid, numGlyphs)
		}
		if !kept[gid] {
			kept[gid] = true
			queue = append(queue, int(gid))
		}
	}
	// Each glyph enters the queue once, so the walk ends however the
	// composite glyphs refer to one another, a cycle included.
	for i := 0; i < len(queue); i++ {
		if i%subsetCtxCheckEvery == 0 {
			if err := ctx.Err(); err != nil {
				return nil, nil, err
			}
		}
		err := compositeComponents(glyph(queue[i]), func(_ int, component uint16) error {
			if int(component) >= numGlyphs {
				return fmt.Errorf("%w: glyph %d draws glyph %d, past the program's %d glyphs", ErrInvalidFont, queue[i], component, numGlyphs)
			}
			if !kept[component] {
				kept[component] = true
				queue = append(queue, int(component))
			}
			return nil
		})
		if err != nil {
			return nil, nil, err
		}
	}

	old := make([]uint16, 0, len(queue))
	newGID := make(map[uint16]uint16, len(queue))
	for gid, k := range kept {
		if k {
			newGID[uint16(gid)] = uint16(len(old)) //nolint:gosec // G115: both are below numGlyphs, a uint16.
			old = append(old, uint16(gid))         //nolint:gosec // G115: gid is below numGlyphs, a uint16.
		}
	}

	var newGlyf []byte
	newOffsets := make([]int, 0, len(old)+1)
	for i, gid := range old {
		if i%subsetCtxCheckEvery == 0 {
			if err := ctx.Err(); err != nil {
				return nil, nil, err
			}
		}
		newOffsets = append(newOffsets, len(newGlyf))
		start := len(newGlyf)
		newGlyf = append(newGlyf, glyph(int(gid))...)
		// The components were read above, so the rewrite cannot fail.
		if err := compositeComponents(newGlyf[start:], func(at int, component uint16) error {
			binary.BigEndian.PutUint16(newGlyf[start+at:], newGID[component])
			return nil
		}); err != nil {
			return nil, nil, err
		}
		// A short loca stores offsets halved, so every glyph starts on an
		// even offset.
		if len(newGlyf)%2 != 0 {
			newGlyf = append(newGlyf, 0)
		}
	}
	newOffsets = append(newOffsets, len(newGlyf))
	longLoca := len(newGlyf)/2 > math.MaxUint16
	newLoca := make([]byte, 0, 4*len(newOffsets))
	for _, off := range newOffsets {
		if longLoca {
			newLoca = binary.BigEndian.AppendUint32(newLoca, uint32(off)) //nolint:gosec // G115: the subset glyf table is no larger than the program's, whose offsets are uint32.
		} else {
			newLoca = binary.BigEndian.AppendUint16(newLoca, uint16(off/2)) //nolint:gosec // G115: off/2 <= MaxUint16 in the short form.
		}
	}

	newHmtx, maxAdvance, err := subsetHmtx(hmtx, int(binary.BigEndian.Uint16(hhea[34:36])), old)
	if err != nil {
		return nil, nil, err
	}

	tables := map[string][]byte{
		"glyf": newGlyf,
		"loca": newLoca,
		"hmtx": newHmtx,
	}
	newHead := slices.Clone(head)
	binary.BigEndian.PutUint32(newHead[8:12], 0)
	if longLoca {
		binary.BigEndian.PutUint16(newHead[50:52], 1)
	} else {
		binary.BigEndian.PutUint16(newHead[50:52], 0)
	}
	tables["head"] = newHead
	newHhea := slices.Clone(hhea)
	binary.BigEndian.PutUint16(newHhea[10:12], maxAdvance)
	binary.BigEndian.PutUint16(newHhea[34:36], uint16(len(old))) //nolint:gosec // G115: len(old) <= numGlyphs, a uint16.
	tables["hhea"] = newHhea
	newMaxp := slices.Clone(maxp)
	binary.BigEndian.PutUint16(newMaxp[4:6], uint16(len(old))) //nolint:gosec // G115: len(old) <= numGlyphs, a uint16.
	tables["maxp"] = newMaxp

	if cmap, err := table("cmap"); err == nil {
		if tables["cmap"], err = subsetCmap(ctx, cmap, newGID); err != nil {
			return nil, nil, err
		}
	}
	if post, err := table("post"); err == nil && len(post) >= 32 {
		tables["post"] = subsetPost(post, old)
	}
	for _, tag := range []string{"OS/2", "cvt ", "fpgm", "gasp", "name", "prep"} {
		if t, err := table(tag); err == nil {
			tables[tag] = t
		}
	}
	return assembleSfnt(binary.BigEndian.Uint32(data[:4]), tables), newGID, nil
}

// glyphOffsets reads the loca table: numGlyphs+1 offsets into a glyf table of
// glyfLen bytes, halved in the short form. It fails on an offset past the
// table or one before the offset preceding it.
func glyphOffsets(loca []byte, glyfLen, numGlyphs int, long bool) ([]int, error) {
	size := 2
	if long {
		size = 4
	}
	if len(loca) < (numGlyphs+1)*size {
		return nil, fmt.Errorf("%w: loca table holds fewer than %d offsets", ErrInvalidFont, numGlyphs+1)
	}
	offsets := make([]int, numGlyphs+1)
	for i := range offsets {
		if long {
			offsets[i] = int(binary.BigEndian.Uint32(loca[4*i:]))
		} else {
			offsets[i] = 2 * int(binary.BigEndian.Uint16(loca[2*i:]))
		}
		if offsets[i] > glyfLen || (i > 0 && offsets[i] < offsets[i-1]) {
			return nil, fmt.Errorf("%w: loca offset %d of glyph %d out of order or past the glyf table", ErrInvalidFont, offsets[i], i)
		}
	}
	return offsets, nil
}

// compositeComponents calls fn with the byte offset of each component's glyph
// index in a composite glyph description and the glyph it names. A simple or
// empty glyph has none. It fails on a description cut short.
func compositeComponents(g []byte, fn func(at int, gid uint16) error) error {
	if len(g) < 10 || int16(binary.BigEndian.Uint16(g[0:2])) >= 0 { //nolint:gosec // G115: numberOfContours is an int16.
		return nil
	}
	pos := 10
	for {
		if pos+4 > len(g) {
			return fmt.Errorf("%w: composite glyph cut short", ErrInvalidFont)
		}
		flags := binary.BigEndian.Uint16(g[pos:])
		at := pos + 2
		pos += 4
		if flags&compositeArgsAreWords != 0 {
			pos += 4
		} else {
			pos += 2
		}
		switch {
		case flags&compositeHaveScale != 0:
			pos += 2
		case flags&compositeHaveXYScale != 0:
			pos += 4
		case flags&compositeHaveTwoByTwo != 0:
			pos += 8
		}
		if pos > len(g) {
			return fmt.Errorf("%w: composite glyph cut short", ErrInvalidFont)
		}
		if err := fn(at, binary.BigEndian.Uint16(g[at:])); err != nil {
			return err
		}
		if flags&compositeMoreComponent == 0 {
			return nil
		}
	}
}

// subsetHmtx returns the hmtx table of the glyphs old, one long metric each,
// and their largest advance. A glyph past numberOfHMetrics takes the last
// long metric's advance and its own left side bearing, 0 when the table
// leaves it out (OpenType specification, hmtx table).
func subsetHmtx(hmtx []byte, numberOfHMetrics int, old []uint16) ([]byte, uint16, error) {
	if numberOfHMetrics == 0 || len(hmtx) < 4*numberOfHMetrics {
		return nil, 0, fmt.Errorf("%w: hmtx table holds fewer than numberOfHMetrics (%d) metrics", ErrInvalidFont, numberOfHMetrics)
	}
	out := make([]byte, 0, 4*len(old))
	var maxAdvance uint16
	for _, gid := range old {
		var advance, lsb uint16
		if int(gid) < numberOfHMetrics {
			advance = binary.BigEndian.Uint16(hmtx[4*int(gid):])
			lsb = binary.BigEndian.Uint16(hmtx[4*int(gid)+2:])
		} else {
			advance = binary.BigEndian.Uint16(hmtx[4*(numberOfHMetrics-1):])
			if at := 4*numberOfHMetrics + 2*(int(gid)-numberOfHMetrics); at+2 <= len(hmtx) {
				lsb = binary.BigEndian.Uint16(hmtx[at:])
			}
		}
		maxAdvance = max(maxAdvance, advance)
		out = binary.BigEndian.AppendUint16(out, advance)
		out = binary.BigEndian.AppendUint16(out, lsb)
	}
	return out, maxAdvance, nil
}

// cmapSubtableRecord is one encoding record of a cmap table.
type cmapSubtableRecord struct {
	platform, encoding uint16
	offset             uint32
}

// subsetCmap returns a cmap table holding, for every encoding record whose
// subtable is in format 0, 4, 6 or 12, a subtable mapping the codes it maps
// to a kept glyph other than .notdef, renumbered through newGID. A subtable in
// another format is dropped. A record whose codes all fall away stays, empty,
// so the program keeps the encodings a font dictionary reads it through (ISO
// 32000-1, 9.6.6.4).
func subsetCmap(ctx context.Context, cmap []byte, newGID map[uint16]uint16) ([]byte, error) {
	if len(cmap) < 4 {
		return nil, fmt.Errorf("%w: cmap table too short", ErrInvalidFont)
	}
	n := int(binary.BigEndian.Uint16(cmap[2:4]))
	if len(cmap) < 4+8*n {
		return nil, fmt.Errorf("%w: cmap subtable records truncated", ErrInvalidFont)
	}
	records := make([]cmapSubtableRecord, 0, n)
	for i := range n {
		rec := cmap[4+8*i:]
		records = append(records, cmapSubtableRecord{
			platform: binary.BigEndian.Uint16(rec[0:2]),
			encoding: binary.BigEndian.Uint16(rec[2:4]),
			offset:   binary.BigEndian.Uint32(rec[4:8]),
		})
	}
	// The OpenType specification (cmap table) orders the encoding records by
	// platform, then encoding, and a pair appears once.
	slices.SortStableFunc(records, func(a, b cmapSubtableRecord) int {
		if a.platform != b.platform {
			return int(a.platform) - int(b.platform)
		}
		return int(a.encoding) - int(b.encoding)
	})
	records = slices.CompactFunc(records, func(a, b cmapSubtableRecord) bool {
		return a.platform == b.platform && a.encoding == b.encoding
	})

	type subtable struct {
		platform, encoding uint16
		data               []byte
	}
	var subs []subtable
	for _, r := range records {
		if int(r.offset) >= len(cmap) {
			continue
		}
		codes, language, err := decodeCmapSubtable(ctx, cmap[r.offset:])
		if err != nil {
			if ctx.Err() != nil {
				return nil, err
			}
			continue
		}
		mapped := make(map[uint32]uint16, len(codes))
		for code, gid := range codes {
			if ng, ok := newGID[gid]; ok && ng != 0 {
				mapped[code] = ng
			}
		}
		subs = append(subs, subtable{r.platform, r.encoding, encodeCmapSubtable(r.platform, language, mapped)})
	}

	out := binary.BigEndian.AppendUint16(nil, 0)
	out = binary.BigEndian.AppendUint16(out, uint16(len(subs))) //nolint:gosec // G115: at most the source's uint16 record count.
	offset := 4 + 8*len(subs)
	for _, s := range subs {
		out = binary.BigEndian.AppendUint16(out, s.platform)
		out = binary.BigEndian.AppendUint16(out, s.encoding)
		out = binary.BigEndian.AppendUint32(out, uint32(offset)) //nolint:gosec // G115: the table is far below 4 GiB.
		offset += len(s.data)
	}
	for _, s := range subs {
		out = append(out, s.data...)
	}
	return out, nil
}

// decodeCmapSubtable returns the code to glyph map of a format 0, 4, 6 or 12
// subtable, and its language field.
func decodeCmapSubtable(ctx context.Context, sub []byte) (map[uint32]uint16, uint32, error) {
	if len(sub) < 6 {
		return nil, 0, fmt.Errorf("%w: cmap subtable too short", ErrInvalidFont)
	}
	switch binary.BigEndian.Uint16(sub[0:2]) {
	case 0, 4, 6:
		codes, err := cmapSubtableCodeToGID(ctx, sub)
		return codes, uint32(binary.BigEndian.Uint16(sub[4:6])), err
	case 12:
		codes, err := format12CodeToGID(ctx, sub)
		if err != nil {
			return nil, 0, err
		}
		return codes, binary.BigEndian.Uint32(sub[8:12]), nil
	}
	return nil, 0, fmt.Errorf("%w: cmap subtable format %d", ErrInvalidFont, binary.BigEndian.Uint16(sub[0:2]))
}

// format12CodeToGID reads a format 12 subtable's groups into a code to glyph
// map, clamped to the Unicode range as [parseCmapFormat12] clamps them.
func format12CodeToGID(ctx context.Context, sub []byte) (map[uint32]uint16, error) {
	if len(sub) < 16 {
		return nil, fmt.Errorf("%w: format 12 header too short", ErrInvalidFont)
	}
	numGroups := int(binary.BigEndian.Uint32(sub[12:16]))
	if numGroups < 0 || numGroups > (len(sub)-16)/12 {
		return nil, fmt.Errorf("%w: format 12 groups truncated", ErrInvalidFont)
	}
	out := make(map[uint32]uint16)
	for i := range numGroups {
		g := sub[16+12*i:]
		start := binary.BigEndian.Uint32(g[0:4])
		end := min(binary.BigEndian.Uint32(g[4:8]), maxUnicodeCodePoint)
		startGID := binary.BigEndian.Uint32(g[8:12])
		for code := start; code <= end; code++ {
			if (code-start)%cmapCtxCheckEvery == 0 {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
			}
			gid := startGID + (code - start)
			if gid > math.MaxUint16 {
				break
			}
			if gid != 0 {
				out[code] = uint16(gid)
			}
		}
	}
	return out, nil
}

// encodeCmapSubtable writes codes as a subtable: format 0 for a Macintosh
// subtable whose codes are single bytes, format 6 for another Macintosh one,
// format 4 when every code is a BMP value and the segments fit its 16-bit
// fields, and format 12 otherwise (OpenType specification, cmap table).
func encodeCmapSubtable(platform uint16, language uint32, codes map[uint32]uint16) []byte {
	sorted := slices.Sorted(maps.Keys(codes))
	top := uint32(0)
	if len(sorted) > 0 {
		top = sorted[len(sorted)-1]
	}
	if platform == 1 && top <= 0xFF && platformByteGlyphs(codes) {
		out := make([]byte, 262)
		binary.BigEndian.PutUint16(out[2:], 262)
		binary.BigEndian.PutUint16(out[4:], uint16(language)) //nolint:gosec // G115: a format 0 language is a uint16 read from the source.
		for _, c := range sorted {
			out[6+c] = byte(codes[c]) //nolint:gosec // G115: platformByteGlyphs holds every glyph to a byte.
		}
		return out
	}
	if platform == 1 && top <= math.MaxUint16 {
		if out := encodeFormat6(language, sorted, codes); out != nil {
			return out
		}
	}
	if top <= math.MaxUint16 {
		if out := encodeFormat4(language, sorted, codes); out != nil {
			return out
		}
	}
	return encodeFormat12(language, sorted, codes)
}

// platformByteGlyphs reports whether every glyph of codes fits the byte a
// format 0 subtable stores it in.
func platformByteGlyphs(codes map[uint32]uint16) bool {
	for _, gid := range codes {
		if gid > 0xFF {
			return false
		}
	}
	return true
}

// encodeFormat6 writes a format 6 trimmed table, nil when its range does not
// fit the 16-bit length field.
func encodeFormat6(language uint32, sorted []uint32, codes map[uint32]uint16) []byte {
	first, count := uint32(0), 0
	if len(sorted) > 0 {
		first = sorted[0]
		count = int(sorted[len(sorted)-1]-first) + 1
	}
	length := 10 + 2*count
	if length > math.MaxUint16 {
		return nil
	}
	out := make([]byte, length)
	binary.BigEndian.PutUint16(out[0:], 6)
	binary.BigEndian.PutUint16(out[2:], uint16(length))   //nolint:gosec // G115: length <= MaxUint16, checked above.
	binary.BigEndian.PutUint16(out[4:], uint16(language)) //nolint:gosec // G115: a format 0 or 6 language is a uint16 read from the source.
	binary.BigEndian.PutUint16(out[6:], uint16(first))    //nolint:gosec // G115: first <= MaxUint16, the caller's bound.
	binary.BigEndian.PutUint16(out[8:], uint16(count))    //nolint:gosec // G115: count < length <= MaxUint16.
	for _, c := range sorted {
		binary.BigEndian.PutUint16(out[10+2*(c-first):], codes[c])
	}
	return out
}

// encodeFormat4 writes a format 4 subtable, one segment per run of
// consecutive codes, with an idDelta when the run's glyphs are consecutive
// too and a glyphIdArray slice otherwise, closed by the 0xFFFF segment. It
// returns nil when the segments do not fit the 16-bit fields.
func encodeFormat4(language uint32, sorted []uint32, codes map[uint32]uint16) []byte {
	type segment struct {
		start, end uint32
		delta      bool
	}
	var segs []segment
	for i := 0; i < len(sorted); {
		if sorted[i] == 0xFFFF {
			break
		}
		j := i + 1
		for j < len(sorted) && sorted[j] == sorted[j-1]+1 && sorted[j] != 0xFFFF {
			j++
		}
		delta := true
		for k := i + 1; k < j; k++ {
			if codes[sorted[k]] != codes[sorted[k-1]]+1 {
				delta = false
				break
			}
		}
		segs = append(segs, segment{sorted[i], sorted[j-1], delta})
		i = j
	}
	segs = append(segs, segment{0xFFFF, 0xFFFF, true})
	segCount := len(segs)
	glyphIDs := 0
	for _, s := range segs {
		if !s.delta {
			glyphIDs += int(s.end-s.start) + 1
		}
	}
	length := 16 + 8*segCount + 2*glyphIDs
	if length > math.MaxUint16 || 2*segCount > math.MaxUint16 {
		return nil
	}
	searchRange, selector := 1, 0
	for searchRange*2 <= segCount {
		searchRange *= 2
		selector++
	}
	out := make([]byte, 0, length)
	out = binary.BigEndian.AppendUint16(out, 4)
	out = binary.BigEndian.AppendUint16(out, uint16(length))                   //nolint:gosec // G115: length <= MaxUint16, checked above.
	out = binary.BigEndian.AppendUint16(out, uint16(language))                 //nolint:gosec // G115: a format 4 language is a uint16 read from the source.
	out = binary.BigEndian.AppendUint16(out, uint16(2*segCount))               //nolint:gosec // G115: 2*segCount <= MaxUint16, checked above.
	out = binary.BigEndian.AppendUint16(out, uint16(2*searchRange))            //nolint:gosec // G115: searchRange <= segCount.
	out = binary.BigEndian.AppendUint16(out, uint16(selector))                 //nolint:gosec // G115: at most 15.
	out = binary.BigEndian.AppendUint16(out, uint16(2*(segCount-searchRange))) //nolint:gosec // G115: below 2*segCount.
	for _, s := range segs {
		out = binary.BigEndian.AppendUint16(out, uint16(s.end)) //nolint:gosec // G115: BMP codes.
	}
	out = binary.BigEndian.AppendUint16(out, 0) // reservedPad
	for _, s := range segs {
		out = binary.BigEndian.AppendUint16(out, uint16(s.start)) //nolint:gosec // G115: BMP codes.
	}
	for _, s := range segs {
		switch {
		case s.start == 0xFFFF:
			// The closing segment maps 0xFFFF to glyph 0.
			out = binary.BigEndian.AppendUint16(out, 1)
		case s.delta:
			out = binary.BigEndian.AppendUint16(out, codes[s.start]-uint16(s.start)) //nolint:gosec // G115: idDelta is modulo 65536.
		default:
			out = binary.BigEndian.AppendUint16(out, 0)
		}
	}
	// idRangeOffset counts bytes from the entry itself to the segment's first
	// glyphIdArray element.
	next := 0
	for i, s := range segs {
		if s.delta {
			out = binary.BigEndian.AppendUint16(out, 0)
			continue
		}
		out = binary.BigEndian.AppendUint16(out, uint16(2*(segCount-i)+2*next)) //nolint:gosec // G115: bounded by length <= MaxUint16.
		next += int(s.end-s.start) + 1
	}
	for _, s := range segs {
		if s.delta {
			continue
		}
		for c := s.start; c <= s.end; c++ {
			out = binary.BigEndian.AppendUint16(out, codes[c])
		}
	}
	return out
}

// encodeFormat12 writes a format 12 subtable, one group per run of
// consecutive codes mapped to consecutive glyphs.
func encodeFormat12(language uint32, sorted []uint32, codes map[uint32]uint16) []byte {
	type group struct{ start, end, gid uint32 }
	var groups []group
	for _, c := range sorted {
		gid := uint32(codes[c])
		if n := len(groups); n > 0 && groups[n-1].end+1 == c && groups[n-1].gid+(c-groups[n-1].start) == gid {
			groups[n-1].end = c
			continue
		}
		groups = append(groups, group{c, c, gid})
	}
	out := make([]byte, 0, 16+12*len(groups))
	out = binary.BigEndian.AppendUint16(out, 12)
	out = binary.BigEndian.AppendUint16(out, 0)
	out = binary.BigEndian.AppendUint32(out, uint32(16+12*len(groups))) //nolint:gosec // G115: at most one group per BMP-and-beyond code the source mapped.
	out = binary.BigEndian.AppendUint32(out, language)
	out = binary.BigEndian.AppendUint32(out, uint32(len(groups))) //nolint:gosec // G115: as above.
	for _, g := range groups {
		out = binary.BigEndian.AppendUint32(out, g.start)
		out = binary.BigEndian.AppendUint32(out, g.end)
		out = binary.BigEndian.AppendUint32(out, g.gid)
	}
	return out
}

// subsetPost returns the post table of the glyphs old: format 2 naming each
// with the name the source gives it, a standard Macintosh name by index and
// any other as a Pascal string, or format 3, which names none, when the
// source names no glyph. The header fields after the version are kept.
func subsetPost(post []byte, old []uint16) []byte {
	names, err := parsePost(post)
	out := slices.Clone(post[:32])
	if err != nil || len(names) == 0 {
		binary.BigEndian.PutUint32(out[0:4], 0x00030000)
		return out
	}
	binary.BigEndian.PutUint32(out[0:4], 0x00020000)
	out = binary.BigEndian.AppendUint16(out, uint16(len(old))) //nolint:gosec // G115: len(old) <= numGlyphs, a uint16.
	standard := make(map[string]uint16, len(macGlyphNames))
	for i, name := range macGlyphNames {
		standard[name] = uint16(i) //nolint:gosec // G115: 258 names.
	}
	custom := make(map[string]uint16)
	var strs []byte
	for _, gid := range old {
		name := names[gid]
		idx, ok := standard[name]
		switch {
		case ok:
		case name == "" || len(name) > math.MaxUint8:
			idx = 0
		default:
			if idx, ok = custom[name]; !ok {
				idx = uint16(len(macGlyphNames) + len(custom)) //nolint:gosec // G115: at most one custom name per glyph, and numGlyphs + 258 names fit the index while a post table can hold them.
				custom[name] = idx
				strs = append(strs, byte(len(name))) //nolint:gosec // G115: names past MaxUint8 bytes take index 0 above.
				strs = append(strs, name...)
			}
		}
		out = binary.BigEndian.AppendUint16(out, idx)
	}
	return append(out, strs...)
}

// assembleSfnt lays tables out under an sfnt header in tag order, each on a
// four-byte boundary with its checksum, and sets the head table's
// checkSumAdjustment (OpenType specification, table directory and head
// table).
func assembleSfnt(version uint32, tables map[string][]byte) []byte {
	tags := make([]string, 0, len(tables))
	for _, tag := range subsetTables {
		if _, ok := tables[tag]; ok {
			tags = append(tags, tag)
		}
	}
	n := len(tags)
	searchRange, selector := 1, 0
	for searchRange*2 <= n {
		searchRange *= 2
		selector++
	}
	out := binary.BigEndian.AppendUint32(nil, version)
	out = binary.BigEndian.AppendUint16(out, uint16(n))                  //nolint:gosec // G115: at most len(subsetTables).
	out = binary.BigEndian.AppendUint16(out, uint16(16*searchRange))     //nolint:gosec // G115: as above.
	out = binary.BigEndian.AppendUint16(out, uint16(selector))           //nolint:gosec // G115: as above.
	out = binary.BigEndian.AppendUint16(out, uint16(16*(n-searchRange))) //nolint:gosec // G115: as above.
	offset := 12 + 16*n
	headAt := -1
	for _, tag := range tags {
		t := tables[tag]
		if tag == "head" {
			headAt = offset
		}
		out = append(out, tag...)
		out = binary.BigEndian.AppendUint32(out, tableChecksum(t))
		out = binary.BigEndian.AppendUint32(out, uint32(offset)) //nolint:gosec // G115: the program is no larger than its source, whose offsets are uint32.
		out = binary.BigEndian.AppendUint32(out, uint32(len(t))) //nolint:gosec // G115: as above.
		offset += (len(t) + 3) &^ 3
	}
	for _, tag := range tags {
		out = append(out, tables[tag]...)
		for len(out)%4 != 0 {
			out = append(out, 0)
		}
	}
	if headAt >= 0 {
		binary.BigEndian.PutUint32(out[headAt+8:], checkSumMagic-tableChecksum(out))
	}
	return out
}

// tableChecksum sums data as big-endian uint32 words, the last one padded
// with zeros.
func tableChecksum(data []byte) uint32 {
	var sum uint32
	for i := 0; i < len(data); i += 4 {
		var word [4]byte
		copy(word[:], data[i:])
		sum += binary.BigEndian.Uint32(word[:])
	}
	return sum
}
