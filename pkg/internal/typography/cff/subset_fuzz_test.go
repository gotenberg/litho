package cff

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
)

// FuzzSubset feeds arbitrary bytes to [Subset], which rewrites a font program
// a remediation caller supplies. The contract is crash-freedom, and a subset
// that succeeds subsets again.
func FuzzSubset(f *testing.F) {
	valid := fontbuild.CFF{
		FontName: "TestSerif",
		Glyphs:   subsetGlyphs(),
		Encoding: map[byte]string{0x41: "A", 0x42: "B", 0x61: "A"},
		Subrs:    true,
	}.Build()
	f.Add(valid, uint16(5), uint16(3))
	f.Add(valid[:len(valid)/2], uint16(1), uint16(2))
	real, err := os.ReadFile(filepath.Join("testdata", "frutiger_subr_width.cff"))
	if err != nil {
		f.Fatalf("reading the seed fixture: %v", err)
	}
	f.Add(real, uint16(10), uint16(40))
	f.Fuzz(func(t *testing.T, data []byte, a, b uint16) {
		if len(data) > 1<<20 {
			return
		}
		ctx := context.Background()
		out, newGID, err := Subset(ctx, data, []uint16{a, b})
		if err != nil {
			return
		}
		keep := make([]uint16, 0, len(newGID))
		for _, gid := range newGID {
			keep = append(keep, gid)
		}
		if _, _, err := Subset(ctx, out, keep); err != nil {
			t.Errorf("subsetting a subset: %v", err)
		}
	})
}
