package truetype

import (
	"context"
	"testing"
)

// FuzzSubset feeds arbitrary bytes to [Subset], which rewrites a font program
// a remediation caller supplies. The contract is crash-freedom, and a subset
// that succeeds subsets again.
func FuzzSubset(f *testing.F) {
	valid := subsetSource()
	f.Add(valid, uint16(6), uint16(8))
	f.Add(valid[:len(valid)/2], uint16(1), uint16(2))
	f.Add(richSfnt(), uint16(0), uint16(1))
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
