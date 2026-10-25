package filter

import (
	"bytes"
	"compress/zlib"
	"io"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// FuzzEncodeFlateSamples asserts that compressing arbitrary samples never
// panics, whatever the row geometry claims, and that whatever it returns
// decodes back to the samples under the /DecodeParms it gives, within the
// limit it was handed.
func FuzzEncodeFlateSamples(f *testing.F) {
	ramp := make([]byte, 4*3*4)
	for i := range ramp {
		ramp[i] = byte(i * 5)
	}
	f.Add(ramp, uint8(4), uint8(3), uint8(3), int16(-1), uint8(0), false)
	f.Add(ramp[:len(ramp)/2+1], uint8(4), uint8(3), uint8(3), int16(20), uint8(40), true)
	f.Fuzz(func(t *testing.T, samples []byte, width, comps, bpcSel uint8, limit int16, parmsCost uint8, predictFirst bool) {
		bpc := []int{1, 2, 4, 8, 16}[int(bpcSel)%5]
		data, parms, fits, err := EncodeFlateSamples(samples, int(width), int(comps), bpc, int(limit), int(parmsCost), predictFirst)
		if err != nil || !fits {
			return
		}
		if limit >= 0 && len(data) > int(limit) {
			t.Fatalf("encoded %d bytes past the limit %d", len(data), limit)
		}
		zr, err := zlib.NewReader(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("reading the compressed samples: %v", err)
		}
		dec, err := io.ReadAll(zr)
		if err != nil {
			t.Fatalf("inflating the compressed samples: %v", err)
		}
		if parms != nil {
			colors := 1
			if c, ok := parms["Colors"].(ast.PdfInt); ok {
				colors = int(c)
			}
			if dec, err = applyPNGPredictor(dec, &predictorParams{
				Predictor: 15, Colors: colors, BitsPerComponent: bpc, Columns: int(width),
			}); err != nil {
				t.Fatalf("undoing the prediction: %v", err)
			}
		}
		if !bytes.Equal(dec, samples) {
			t.Fatal("the encoded samples decode to other bytes")
		}
	})
}
