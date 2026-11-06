package mutations

import (
	"fmt"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/typography/embed"
)

// ValidateTTFForStamp parses enough of a TrueType font to confirm it has
// the tables required for embedding (head, hhea, maxp, cmap, hmtx). It is
// exported so the public pdf package can surface parse errors at
// pdf.NewTTFFont call time instead of deferring them to write time.
func ValidateTTFForStamp(data []byte) error {
	_, err := embed.Load(data)
	return err
}

// collectUsedGIDs returns the glyph IDs the characters of text draw, looked
// up through the font's Unicode cmap. It is the keep set of
// [embed.Font.WriteIdentity], which adds .notdef and the components of
// composite glyphs itself.
func collectUsedGIDs(text string, f *embed.Font) []uint16 {
	keep := make([]uint16, 0, len(text))
	for _, r := range text {
		if gid, ok := f.GID(r); ok {
			keep = append(keep, gid)
		}
	}
	return keep
}

// encodeTextAsGIDs converts a UTF-8 string into a hex-encoded PDF string
// literal containing 2-byte big-endian GIDs, the codes of a font
// [embed.Font.WriteIdentity] writes. Runes not present in the font's cmap
// fall back to GID 0 (.notdef).
func encodeTextAsGIDs(text string, f *embed.Font) string {
	var b strings.Builder
	b.WriteString("<")
	for _, r := range text {
		gid, ok := f.GID(r)
		if !ok {
			gid = 0
		}
		fmt.Fprintf(&b, "%04X", gid)
	}
	b.WriteString(">")
	return b.String()
}
