package imgpaint

import "testing"

func FuzzColorKey(f *testing.F) {
	f.Add([]byte{0, 255, 0, 10, 20, 30}, 2, 1, 3, 8, 0, 250)
	f.Add([]byte{0, 255}, 2, 1, 3, 8, 0, 250)
	f.Add([]byte{0b10100000}, 3, 2, 1, 1, 1, 1)
	f.Add([]byte{0x12}, 1, 1, 1, 16, 0, 0xffff)
	f.Fuzz(func(t *testing.T, data []byte, width, height, comps, bpc, lo, hi int) {
		ranges := make([]int, 0, 2*max(0, min(comps, maxColorKeyComponents+1)))
		for range cap(ranges) / 2 {
			ranges = append(ranges, lo, hi)
		}
		a, err := ColorKey(data, width, height, comps, bpc, ranges, 1<<16)
		if err != nil {
			return
		}
		if len(a.A) != a.Width*a.Height {
			t.Fatalf("plane holds %d values for %dx%d", len(a.A), a.Width, a.Height)
		}
	})
}
