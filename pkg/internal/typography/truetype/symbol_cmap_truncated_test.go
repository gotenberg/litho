package truetype

import (
	"context"
	"encoding/binary"
	"testing"
)

// TestSymbolCharCodeToGID_TruncatedSubtable pins that a symbol subtable whose
// declared length runs past the end of the cmap table yields no map.
//
// Such a subtable cannot be trusted to list every code the font maps, so a
// caller reading "absent from the map" as "no glyph" would report glyphs the
// font may well hold.
//
// # Corpus evidence
//
//   - govdocs thread_400_400573.pdf: embeds a SymbolMT subset whose (3,0)
//     subtable declares 48 bytes where 42 remain and holds only 0xF021 and
//     0xF022, and veraPDF reports no glyph-level finding for that font at
//     all.
func TestSymbolCharCodeToGID_TruncatedSubtable(t *testing.T) {
	sub := format4Segment(0xF021, 5)
	binary.BigEndian.PutUint16(sub[2:4], uint16(len(sub)+8)) //nolint:gosec // fixture size is small
	got, err := SymbolCharCodeToGID(context.Background(), minimalSymbolFont(sub))
	if err == nil || got != nil {
		t.Errorf("got %v, %v; want no map and an error for a subtable longer than its table", got, err)
	}

	// The control declares its true length and still resolves.
	whole := format4Segment(0xF021, 5)
	binary.BigEndian.PutUint16(whole[2:4], uint16(len(whole))) //nolint:gosec // fixture size is small
	got, err = SymbolCharCodeToGID(context.Background(), minimalSymbolFont(whole))
	if err != nil || got[0x21] != 5 {
		t.Errorf("got %v, %v; want code 0x21 -> GID 5", got, err)
	}
}
