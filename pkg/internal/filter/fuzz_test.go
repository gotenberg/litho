package filter

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/limits"
)

// pureGoFilters are the stream filters decoded without a crypt handler. FuzzApplyDecodeFilter runs each fuzz input through all
// of them and asserts none panics.
var pureGoFilters = []ast.PdfName{
	"FlateDecode", "LZWDecode", "ASCIIHexDecode", "ASCII85Decode",
	"CCITTFaxDecode", "BrotliDecode", "RunLengthDecode", "DCTDecode",
	"JBIG2Decode", "JPXDecode",
}

// fuzzImageLimits is the budget [FuzzApplyDecodeFilter] decodes under: the
// default but for an image budget of one million pixels. The image codecs
// scale their caps from it, and at the default budget a few bytes of JPEG 2000
// or DCT data can declare an image that takes gigabytes to decode, which
// exhausts a fuzzing worker's memory rather than finding a crash.
var fuzzImageLimits = limits.Limits{MaxImagePixels: 1 << 20}.WithDefaults()

// FuzzApplyDecodeFilter feeds arbitrary bytes to each pure-Go stream
// filter. The contract under test is crash-freedom on hostile input,
// not a successful decode. Output size is bounded by the package's
// decompression-bomb guard, and image size by [fuzzImageLimits].
func FuzzApplyDecodeFilter(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte("48656C6C6F>"))                // ASCIIHex
	f.Add([]byte("z~>"))                        // ASCII85 zero shortcut + EOD
	f.Add([]byte{0x78, 0x9c, 0x03, 0x00})       // zlib empty
	f.Add([]byte{0x80, 0x0b, 0x60})             // LZW-ish
	f.Add([]byte{0x00, 0x01, 0x02, 0x80, 0xff}) // RunLength-ish
	f.Add([]byte{0xff, 0xd8, 0xff, 0xd9})       // JPEG SOI/EOI

	// zlib data cut inside its block, and zlib data stopped at a sync flush.
	f.Add([]byte{0x78, 0x9c, 0xca, 0x48, 0xcd})
	f.Add([]byte{0x78, 0x9c, 0xca, 0x00, 0x00, 0x00, 0x00, 0xff, 0xff})

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		ctx := context.Background()
		for _, name := range pureGoFilters {
			// Result and error are intentionally discarded: no input
			// may make a decoder panic.
			_, _ = applyDecodeFilter(ctx, data, name, ast.PdfDict{}, nil, fuzzImageLimits)
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
	// The predictor is picked by data[0] modulo the table length, so the two
	// seeds above select PNG (indices 2 and 1) and the TIFF path was measured
	// at zero coverage. A leading byte divisible by 7 selects it; 0, 7 and 14
	// pair it with 1, 4 and 16 bits per component, which are the widths that
	// drive the sub-byte sample readers rather than the plain byte copy.
	f.Add([]byte{0, 0x11, 0x22, 0x33, 0x44, 0x55, 4})
	f.Add([]byte{7, 0xFF, 0x80, 0x01, 0x7F, 0x00, 3})
	f.Add([]byte{14, 0xAA, 0x55, 0xAA, 0x55, 0xAA, 2})
	// PNG rows are each prefixed with a filter-type byte, and type 4 is Paeth,
	// the only one that reads the pixel above-left. The leading byte does double
	// duty here: it picks the predictor AND is row 0's filter byte, so it has to
	// be 4 itself. 4 selects PNG predictor 13 with 16 bits per component, which
	// with the trailing 0 (Columns 1) and the middle 4 (Colors 1) makes a
	// two-byte row and a three-byte stride, landing the second 4 on row 1's
	// filter byte too.
	f.Add([]byte{4, 0x10, 0x20, 4, 0x30, 0x40, 0})

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
