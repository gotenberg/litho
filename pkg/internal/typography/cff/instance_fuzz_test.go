package cff

import (
	"context"
	"os"
	"testing"
)

// FuzzInstanceCFF2 feeds arbitrary bytes to [InstanceCFF2] at a fixed
// instance, every region at scalar 0.5: the 'CFF2' table of an installed
// or embedded variable font is attacker-controlled. Every program it
// writes must read back as CFF.
func FuzzInstanceCFF2(f *testing.F) {
	valid, err := os.ReadFile("testdata/cff2_ops.cff2")
	if err != nil {
		f.Fatal(err)
	}
	f.Add(valid)
	f.Add(valid[:len(valid)/2])
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		font, err := ParseCFF2(data)
		if err != nil {
			return
		}
		widths := make([]float64, font.NumGlyphs())
		inst, err := InstanceCFF2(context.Background(), data, "Fuzz", widths, func([]byte) ([][]float64, error) {
			out := make([][]float64, len(font.regionCounts))
			for i, k := range font.regionCounts {
				out[i] = make([]float64, k)
				for j := range out[i] {
					out[i][j] = 0.5
				}
			}
			return out, nil
		})
		if err != nil {
			return
		}
		if _, err := ParseFont(inst.Program); err != nil {
			t.Fatalf("the instance does not read: %v", err)
		}
	})
}
