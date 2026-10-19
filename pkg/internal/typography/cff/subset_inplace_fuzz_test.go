package cff

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
)

// FuzzSubsetInPlace feeds arbitrary bytes to [SubsetInPlace], which rewrites
// a program a document embeds. The contract is crash-freedom, a glyph count
// that does not change, and a second pass that changes nothing.
func FuzzSubsetInPlace(f *testing.F) {
	valid := fontbuild.CFF{
		FontName: "TestSerif",
		Glyphs:   subsetGlyphs(),
		Encoding: map[byte]string{0x41: "A", 0x42: "B", 0x61: "A"},
		Subrs:    true,
	}.Build()
	f.Add(valid, uint16(5), uint16(3))
	f.Add(valid[:len(valid)/2], uint16(1), uint16(2))
	for _, name := range []string{"frutiger_subr_width.cff", "cid_fdarray_matrix.cff"} {
		real, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			f.Fatalf("reading the seed fixture: %v", err)
		}
		f.Add(real, uint16(10), uint16(40))
	}
	f.Fuzz(func(t *testing.T, data []byte, a, b uint16) {
		if len(data) > 1<<20 {
			return
		}
		ctx := context.Background()
		out, err := SubsetInPlace(ctx, data, []uint16{a, b})
		if err != nil {
			return
		}
		before, errBefore := readInPlaceSource(data)
		after, errAfter := readInPlaceSource(out)
		if errBefore != nil || errAfter != nil || len(before.charStrings) != len(after.charStrings) {
			t.Fatalf("glyph count changed: %v, %v", errBefore, errAfter)
		}
		again, err := SubsetInPlace(ctx, out, []uint16{a, b})
		if err != nil {
			t.Errorf("subsetting a subset: %v", err)
		} else if !bytes.Equal(again, out) {
			t.Error("a second pass changed the program")
		}
	})
}
