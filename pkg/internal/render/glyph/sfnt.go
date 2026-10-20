package glyph

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/typography/truetype"
)

// sfntTables are the cmap subtables and post table names of a TrueType or
// OpenType program that ISO 32000-1, 9.6.6.4 looks a simple font's codes up
// in. A subtable the program does not carry, or one litho cannot read, is
// nil.
type sfntTables struct {
	// unicode is the (3,1) Microsoft Unicode BMP subtable, or failing it
	// the (3,10) full-repertoire or a (0,x) Unicode-platform one.
	unicode map[uint32]uint16
	// win31 reports that unicode is the (3,1) subtable 9.6.6.4 names.
	win31 bool
	// symbol is the (3,0) Microsoft Symbol subtable.
	symbol map[uint32]uint16
	// mac is the (1,0) Macintosh Roman subtable.
	mac map[uint32]uint16
	// post maps each post table glyph name to its glyph ID, the lowest
	// one when two glyphs share a name.
	post map[string]uint16
}

// readSfntTables reads the cmap subtables and post table names of data. A
// table that will not read is left out: the lookup that needs it then falls
// to the next route of 9.6.6.4, which is what a viewer does with a damaged
// table too.
func readSfntTables(ctx context.Context, data []byte) *sfntTables {
	t := &sfntTables{}
	subtables, err := truetype.CmapSubtables(data)
	if err == nil {
		read := func(st truetype.CmapSubtable) map[uint32]uint16 {
			m, err := truetype.SubtableCodeToGID(ctx, data, st)
			if err != nil || len(m) == 0 {
				return nil
			}
			return m
		}
		var full, platform0 map[uint32]uint16
		for _, st := range subtables {
			switch {
			case st.PlatformID == 3 && st.EncodingID == 1 && t.unicode == nil:
				t.unicode, t.win31 = read(st), true
			case st.PlatformID == 3 && st.EncodingID == 10 && full == nil:
				full = read(st)
			case st.PlatformID == 0 && platform0 == nil:
				platform0 = read(st)
			case st.PlatformID == 3 && st.EncodingID == 0 && t.symbol == nil:
				t.symbol = read(st)
			case st.PlatformID == 1 && st.EncodingID == 0 && t.mac == nil:
				t.mac = read(st)
			}
		}
		if t.unicode == nil {
			t.win31 = false
			t.unicode = full
			if t.unicode == nil {
				t.unicode = platform0
			}
		}
	}
	if names, err := truetype.PostGlyphNames(ctx, data); err == nil && len(names) > 0 {
		t.post = make(map[string]uint16, len(names))
		for gid, name := range names {
			if prev, ok := t.post[name]; !ok || gid < prev {
				t.post[name] = gid
			}
		}
	}
	return t
}

// hasCmap reports whether the program carries any cmap subtable litho reads.
func (t *sfntTables) hasCmap() bool {
	return t.unicode != nil || t.symbol != nil || t.mac != nil
}
