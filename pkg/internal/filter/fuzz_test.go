package filter

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// pureGoFilters are the stream filters decoded without a CGO plugin or a
// crypt handler. FuzzApplyDecodeFilter runs each fuzz input through all
// of them and asserts none panics.
var pureGoFilters = []ast.PdfName{
	"FlateDecode", "LZWDecode", "ASCIIHexDecode", "ASCII85Decode",
	"CCITTFaxDecode", "BrotliDecode", "RunLengthDecode", "DCTDecode",
}

// FuzzApplyDecodeFilter feeds arbitrary bytes to each pure-Go stream
// filter. The contract under test is crash-freedom on hostile input,
// not a successful decode. Output size is bounded by the package's
// decompression-bomb guard.
func FuzzApplyDecodeFilter(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte("48656C6C6F>"))                // ASCIIHex
	f.Add([]byte("z~>"))                        // ASCII85 zero shortcut + EOD
	f.Add([]byte{0x78, 0x9c, 0x03, 0x00})       // zlib empty
	f.Add([]byte{0x80, 0x0b, 0x60})             // LZW-ish
	f.Add([]byte{0x00, 0x01, 0x02, 0x80, 0xff}) // RunLength-ish
	f.Add([]byte{0xff, 0xd8, 0xff, 0xd9})       // JPEG SOI/EOI

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		ctx := context.Background()
		for _, name := range pureGoFilters {
			// Result and error are intentionally discarded: no input
			// may make a decoder panic.
			_, _ = applyDecodeFilter(ctx, data, name, ast.PdfDict{})
		}
	})
}

// FuzzApplyPredictor exercises the PNG and TIFF predictor undo paths,
// which reinterpret decoded bytes against caller-controlled Columns,
// Colors, and BitsPerComponent. Geometry is derived from the input so
// the fuzzer explores it, including the 16-bit sample path.
func FuzzApplyPredictor(f *testing.F) {
	f.Add([]byte{2, 0, 0, 0, 0})
	f.Add([]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08})

	predictors := []int{2, 10, 11, 12, 13, 14, 15}
	bpcs := []int{1, 2, 4, 8, 16}

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) < 1 || len(data) > 1<<20 {
			return
		}
		params := &predictorParams{
			Predictor:        predictors[int(data[0])%len(predictors)],
			Columns:          1 + int(data[len(data)-1]),
			Colors:           1 + int(data[len(data)/2])%4,
			BitsPerComponent: bpcs[int(data[0])%len(bpcs)],
		}
		_, _ = applyPredictor(data, params)
	})
}
