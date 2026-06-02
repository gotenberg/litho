package cff

import (
	"context"
	"testing"
)

// FuzzCFF feeds arbitrary bytes to the CFF font-program readers. Each
// entry point parses an embedded, attacker-controlled CFF table; the
// contract is crash-freedom, not a successful parse.
func FuzzCFF(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0x01, 0x00, 0x04, 0x01}) // header: major 1, hdrSize 4, offSize 1
	f.Add(make([]byte, 64))

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		ctx := context.Background()
		_ = ExtractEncoding(data)
		_, _ = GlyphToUnicode(ctx, data)
		_, _ = GlyphNames(ctx, data)
		_, _ = ValidCIDs(data)
		_ = ReadUnitsPerEm(data)
		_, _ = GlyphWidths(ctx, data)
	})
}
