package truetype

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"slices"
)

// CmapAlias is one encoding record of a cmap table [RewriteCmap] writes: the
// record Target, holding the codes the subtable of the program's record From
// maps, or Codes when it is not nil.
type CmapAlias struct {
	Target CmapSubtable
	From   CmapSubtable
	// Codes maps each code of the subtable to its glyph, in place of From's
	// subtable.
	Codes map[uint32]uint16
}

// RewriteCmap returns data with its cmap table holding exactly the encoding
// records aliases lists, in platform and encoding order, each a subtable
// holding the codes the From record's subtable maps, or the alias's Codes,
// and every other table
// copied under a table directory of its own in tag order, with the head
// table's checkSumAdjustment computed anew (ISO/IEC 14496-22: table directory,
// cmap and head tables). A Windows Unicode BMP target, (3,1), keeps the BMP
// codes alone, which is all its format 4 subtable can hold. A DSIG table is
// dropped, since it signs the program as it was. Glyph outlines, metrics and
// names are left as they are.
//
// It fails on a program whose table directory or cmap table is malformed, on
// an alias whose From record the program lacks or holds in a format other
// than 0, 4, 6 or 12, on Codes naming a glyph past the program's glyph count,
// and on two aliases of one target.
func RewriteCmap(ctx context.Context, data []byte, aliases []CmapAlias) ([]byte, error) {
	if !IsSfnt(data) {
		return nil, fmt.Errorf("%w: not an sfnt program", ErrInvalidFont)
	}
	records, err := ParseTableDirectory(data)
	if err != nil {
		return nil, err
	}
	if len(records) > maxSfntTables {
		return nil, fmt.Errorf("%w: %d tables", ErrInvalidFont, len(records))
	}
	cmap, err := TableData(data, records, "cmap")
	if err != nil {
		return nil, err
	}
	sources, err := cmapRecords(cmap)
	if err != nil {
		return nil, err
	}
	numGlyphs, err := NumGlyphs(data)
	if err != nil {
		return nil, err
	}
	aliases = slices.Clone(aliases)
	slices.SortStableFunc(aliases, func(a, b CmapAlias) int { return compareEncoding(a.Target, b.Target) })

	type subtable struct {
		target CmapSubtable
		data   []byte
	}
	subs := make([]subtable, 0, len(aliases))
	for i, a := range aliases {
		if i > 0 && aliases[i-1].Target == a.Target {
			return nil, fmt.Errorf("%w: cmap record (%d,%d) given twice", ErrInvalidFont, a.Target.PlatformID, a.Target.EncodingID)
		}
		codes, language, err := aliasCodes(ctx, cmap, sources, a, numGlyphs)
		if err != nil {
			return nil, err
		}
		if a.Target.PlatformID == 3 && a.Target.EncodingID == 1 {
			// The OpenType specification (cmap table, Windows platform)
			// gives encoding 1 to a format 4 subtable over the BMP.
			bmp := make(map[uint32]uint16, len(codes))
			for code, gid := range codes {
				if code <= math.MaxUint16 {
					bmp[code] = gid
				}
			}
			codes, language = bmp, 0
		}
		sub := encodeCmapSubtable(a.Target.PlatformID, language, codes)
		if a.Target.PlatformID == 3 && a.Target.EncodingID == 1 && binary.BigEndian.Uint16(sub[0:2]) != 4 {
			return nil, fmt.Errorf("%w: the BMP codes of cmap record (%d,%d) do not fit a format 4 subtable", ErrInvalidFont, a.From.PlatformID, a.From.EncodingID)
		}
		subs = append(subs, subtable{a.Target, sub})
	}

	out := binary.BigEndian.AppendUint16(nil, 0)
	out = binary.BigEndian.AppendUint16(out, uint16(len(subs))) //nolint:gosec // G115: the aliases name distinct uint16 pairs taken from a uint16-counted table.
	offset := 4 + 8*len(subs)
	for _, s := range subs {
		out = binary.BigEndian.AppendUint16(out, s.target.PlatformID)
		out = binary.BigEndian.AppendUint16(out, s.target.EncodingID)
		out = binary.BigEndian.AppendUint32(out, uint32(offset)) //nolint:gosec // G115: each subtable re-encodes one of the source's, whose offsets are uint32.
		offset += len(s.data)
	}
	for _, s := range subs {
		out = append(out, s.data...)
	}

	tables := make(map[string][]byte, len(records))
	tags := make([]string, 0, len(records))
	for _, rec := range records {
		if rec.Tag == "DSIG" {
			continue
		}
		if _, dup := tables[rec.Tag]; dup {
			return nil, fmt.Errorf("%w: table %q listed twice", ErrInvalidFont, rec.Tag)
		}
		t, err := TableData(data, records, rec.Tag)
		if err != nil {
			return nil, err
		}
		tables[rec.Tag] = t
		tags = append(tags, rec.Tag)
	}
	tables["cmap"] = out
	if len(tables["head"]) < headInfoBytes {
		return nil, fmt.Errorf("%w: head table missing or too short", ErrInvalidFont)
	}
	// checkSumAdjustment is computed over the program with the field zeroed
	// (ISO/IEC 14496-22: head table).
	head := slices.Clone(tables["head"])
	binary.BigEndian.PutUint32(head[8:12], 0)
	tables["head"] = head
	slices.Sort(tags)
	return assembleTables(binary.BigEndian.Uint32(data[:4]), tags, tables), nil
}

// aliasCodes returns the codes the subtable of alias a holds, and its
// language: a's Codes, each glyph below numGlyphs, or those of the subtable
// of the record a.From names among sources, the records of cmap.
func aliasCodes(ctx context.Context, cmap []byte, sources []cmapSubtableRecord, a CmapAlias, numGlyphs int) (map[uint32]uint16, uint32, error) {
	if a.Codes != nil {
		for code, gid := range a.Codes {
			if int(gid) >= numGlyphs {
				return nil, 0, fmt.Errorf("%w: code %#x maps to glyph %d, past the program's %d glyphs", ErrInvalidFont, code, gid, numGlyphs)
			}
		}
		return a.Codes, 0, nil
	}
	idx := slices.IndexFunc(sources, func(r cmapSubtableRecord) bool {
		return r.platform == a.From.PlatformID && r.encoding == a.From.EncodingID
	})
	if idx < 0 {
		return nil, 0, fmt.Errorf("%w: no cmap record (%d,%d)", ErrTableNotFound, a.From.PlatformID, a.From.EncodingID)
	}
	off := sources[idx].offset
	if int64(off) >= int64(len(cmap)) {
		return nil, 0, fmt.Errorf("%w: cmap subtable offset out of bounds", ErrInvalidFont)
	}
	return decodeCmapSubtable(ctx, cmap[off:])
}

// cmapRecords reads the encoding records of a cmap table.
func cmapRecords(cmap []byte) ([]cmapSubtableRecord, error) {
	if len(cmap) < 4 {
		return nil, fmt.Errorf("%w: cmap table too short", ErrInvalidFont)
	}
	n := int(binary.BigEndian.Uint16(cmap[2:4]))
	if len(cmap) < 4+8*n {
		return nil, fmt.Errorf("%w: cmap subtable records truncated", ErrInvalidFont)
	}
	out := make([]cmapSubtableRecord, 0, n)
	for i := range n {
		rec := cmap[4+8*i:]
		out = append(out, cmapSubtableRecord{
			platform: binary.BigEndian.Uint16(rec[0:2]),
			encoding: binary.BigEndian.Uint16(rec[2:4]),
			offset:   binary.BigEndian.Uint32(rec[4:8]),
		})
	}
	return out, nil
}

// compareEncoding orders cmap encoding records by platform, then encoding, as
// the OpenType specification (cmap table) orders them.
func compareEncoding(a, b CmapSubtable) int {
	if a.PlatformID != b.PlatformID {
		return int(a.PlatformID) - int(b.PlatformID)
	}
	return int(a.EncodingID) - int(b.EncodingID)
}
