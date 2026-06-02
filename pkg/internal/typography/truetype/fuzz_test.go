package truetype

import (
	"context"
	"testing"
)

// FuzzTrueType feeds arbitrary bytes to the sfnt/TrueType table readers.
// Every entry point reads an embedded, attacker-controlled font program;
// the contract is crash-freedom, not a successful parse.
func FuzzTrueType(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0x00, 0x01, 0x00, 0x00, 0x00, 0x00}) // sfnt version + numTables 0
	f.Add(make([]byte, 128))

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		ctx := context.Background()
		records, _ := ParseTableDirectory(data)
		_, _ = TableData(data, records, "cmap")
		_, _ = CmapSubtables(data)
		_, _ = CmapSubtableCount(data)
		_, _ = GlyphWidths(ctx, data)
		_, _ = EmptyGIDs(ctx, data)
		_, _ = CharCodeToGID(ctx, data)
		_, _ = GlyphToUnicode(ctx, data)
		_, _ = PostGlyphNames(ctx, data)
	})
}
