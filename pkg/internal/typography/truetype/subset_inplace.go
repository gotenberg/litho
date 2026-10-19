package truetype

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"slices"
)

// SubsetInPlace returns a TrueType program in which every glyph but glyph 0
// (.notdef), the glyphs of keep and the components a kept composite glyph
// draws has an empty outline, and the number of glyphs the source reports. It
// is [Subset] for a program a document already references by glyph ID: no
// glyph is renumbered, so the glyph a code selects is the one it selected
// before through every route a reader may take, a CIDToGIDMap, a cmap
// subtable, a post table name, or the code itself, and every glyph it selects
// draws as it did unless it is one keep left out.
//
// Only the glyf and loca tables change, the loca in the source's format, and
// head's checkSumAdjustment with them. An empty glyph is a loca entry equal to the next (OpenType
// specification, loca table), which the format allows for any glyph ID. The
// cmap, hmtx, hhea, maxp and post tables are copied: they keep describing
// every glyph ID, so the advances, the names and the codes a reader looks a
// glyph up by stay as they were. As with Subset, the other tables listed in
// [subsetTables] are copied and every table outside it is dropped, the tables
// indexed by glyph ID that ISO 32000-1, 9.6.6 and 9.7.4 have a conforming
// reader leave unread.
//
// It fails as Subset does: on a program without glyf outlines, on one whose
// glyph tables are malformed, and on a glyph ID of keep past the program's
// glyph count.
func SubsetInPlace(ctx context.Context, data []byte, keep []uint16) ([]byte, error) {
	if !IsSfnt(data) || string(data[:4]) == "OTTO" {
		return nil, fmt.Errorf("%w: not a TrueType program with glyf outlines", ErrInvalidFont)
	}
	records, err := ParseTableDirectory(data)
	if err != nil {
		return nil, err
	}
	table := func(tag string) ([]byte, error) { return TableData(data, records, tag) }
	head, err := table("head")
	if err != nil {
		return nil, err
	}
	maxp, err := table("maxp")
	if err != nil {
		return nil, err
	}
	loca, err := table("loca")
	if err != nil {
		return nil, err
	}
	glyf, err := table("glyf")
	if err != nil {
		return nil, err
	}
	// head is 54 bytes and maxp at least 6 (OpenType specification).
	if len(head) < 54 || len(maxp) < 6 {
		return nil, fmt.Errorf("%w: head or maxp table too short", ErrInvalidFont)
	}
	numGlyphs := int(binary.BigEndian.Uint16(maxp[4:6]))
	if numGlyphs == 0 {
		return nil, fmt.Errorf("%w: no glyph", ErrInvalidFont)
	}
	sourceLong := binary.BigEndian.Uint16(head[50:52]) != 0
	offsets, err := glyphOffsets(loca, len(glyf), numGlyphs, sourceLong)
	if err != nil {
		return nil, err
	}
	glyph := func(gid int) []byte { return glyf[offsets[gid]:offsets[gid+1]] }

	kept, err := keptGlyphs(ctx, numGlyphs, keep, glyph)
	if err != nil {
		return nil, err
	}

	var newGlyf []byte
	newOffsets := make([]int, 0, numGlyphs+1)
	for gid := range numGlyphs {
		if gid%subsetCtxCheckEvery == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		newOffsets = append(newOffsets, len(newGlyf))
		if !kept[gid] {
			continue
		}
		newGlyf = append(newGlyf, glyph(gid)...)
		// A short loca stores offsets halved, so every glyph starts on an
		// even offset. A long one stores them as they are, and its glyphs
		// keep the alignment the source gave them.
		if !sourceLong && len(newGlyf)%2 != 0 {
			newGlyf = append(newGlyf, 0)
		}
	}
	newOffsets = append(newOffsets, len(newGlyf))
	// The loca keeps the source's format. The new glyf table is no larger
	// than the source's, so a short one still fits.
	longLoca := sourceLong || len(newGlyf)/2 > math.MaxUint16
	newLoca := make([]byte, 0, 4*len(newOffsets))
	for _, off := range newOffsets {
		if longLoca {
			newLoca = binary.BigEndian.AppendUint32(newLoca, uint32(off)) //nolint:gosec // G115: the new glyf table is no larger than the program's, whose offsets are uint32.
		} else {
			newLoca = binary.BigEndian.AppendUint16(newLoca, uint16(off/2)) //nolint:gosec // G115: off/2 <= MaxUint16 in the short form.
		}
	}

	tables := map[string][]byte{"glyf": newGlyf, "loca": newLoca}
	newHead := slices.Clone(head)
	binary.BigEndian.PutUint32(newHead[8:12], 0)
	if longLoca {
		binary.BigEndian.PutUint16(newHead[50:52], 1)
	} else {
		binary.BigEndian.PutUint16(newHead[50:52], 0)
	}
	tables["head"] = newHead
	for _, tag := range subsetTables {
		if _, rebuilt := tables[tag]; rebuilt {
			continue
		}
		if t, err := table(tag); err == nil {
			tables[tag] = t
		}
	}
	return assembleSfnt(binary.BigEndian.Uint32(data[:4]), tables), nil
}

// keptGlyphs returns, for each of numGlyphs glyph IDs, whether a subset keeps
// it: glyph 0, the glyphs of keep, and every glyph a kept composite glyph
// draws, at any depth. Each glyph enters the walk once, so it ends however the
// composite glyphs refer to one another, a cycle included.
func keptGlyphs(ctx context.Context, numGlyphs int, keep []uint16, glyph func(int) []byte) ([]bool, error) {
	kept := make([]bool, numGlyphs)
	kept[0] = true
	queue := []int{0}
	for _, gid := range keep {
		if int(gid) >= numGlyphs {
			return nil, fmt.Errorf("%w: glyph %d past the program's %d glyphs", ErrInvalidFont, gid, numGlyphs)
		}
		if !kept[gid] {
			kept[gid] = true
			queue = append(queue, int(gid))
		}
	}
	for i := 0; i < len(queue); i++ {
		if i%subsetCtxCheckEvery == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
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
			return nil, err
		}
	}
	return kept, nil
}
