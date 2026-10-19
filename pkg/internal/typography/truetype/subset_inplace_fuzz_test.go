package truetype

import (
	"bytes"
	"context"
	"testing"
)

// FuzzSubsetInPlace feeds arbitrary bytes to [SubsetInPlace], which rewrites
// a program a document embeds. The contract is crash-freedom, a glyph count
// that does not change, and a second pass that changes nothing.
func FuzzSubsetInPlace(f *testing.F) {
	valid := subsetSource()
	f.Add(valid, uint16(6), uint16(8))
	f.Add(valid[:len(valid)/2], uint16(1), uint16(2))
	f.Add(richSfnt(), uint16(0), uint16(1))
	f.Fuzz(func(t *testing.T, data []byte, a, b uint16) {
		if len(data) > 1<<20 {
			return
		}
		ctx := context.Background()
		out, err := SubsetInPlace(ctx, data, []uint16{a, b})
		if err != nil {
			return
		}
		before, errBefore := NumGlyphs(data)
		after, errAfter := NumGlyphs(out)
		if errBefore != nil || errAfter != nil || before != after {
			t.Errorf("%d glyphs (%v), source %d (%v)", after, errAfter, before, errBefore)
		}
		again, err := SubsetInPlace(ctx, out, []uint16{a, b})
		if err != nil {
			t.Errorf("subsetting a subset: %v", err)
		} else if !bytes.Equal(again, out) {
			t.Error("a second pass changed the program")
		}
	})
}
