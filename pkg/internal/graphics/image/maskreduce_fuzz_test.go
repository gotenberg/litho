package image

import (
	"fmt"
	"testing"
)

// FuzzDecodeMaskAt feeds the reduced mask reading arbitrary samples and
// geometry past a small image budget, asserting it never panics and never
// returns a grid past the budget.
func FuzzDecodeMaskAt(f *testing.F) {
	f.Add([]byte{0xFF, 0x00, 0xFF, 0x00, 0x80, 0x80, 0x80, 0x80}, uint16(16), uint16(4), uint8(1), uint8(2), uint8(2), false)
	f.Add([]byte{0xFF}, uint16(16), uint16(4), uint8(8), uint8(3), uint8(1), true)
	f.Fuzz(func(t *testing.T, data []byte, w, h uint16, bpc, cols, rows uint8, stencil bool) {
		const budget = 32
		dict := fmt.Sprintf("/Type /XObject /Subtype /Image /Width %d /Height %d /BitsPerComponent %d", w, h, bpc)
		if stencil {
			dict += " /ImageMask true"
		} else {
			dict += " /ColorSpace /DeviceGray"
		}
		ctx, doc, stream := maskDoc(t, dict, data, budget)
		img, err := DecodeMaskAt(ctx, doc, "M", stream, int(cols), int(rows))
		if err != nil {
			return
		}
		if b := img.Bounds(); b.Dx()*b.Dy() > budget {
			t.Fatalf("grid %v past the budget of %d", b, budget)
		}
	})
}
