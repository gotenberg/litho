package filter

import (
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// predictorParams holds the parameters extracted from a /DecodeParms dictionary
// for predictor-based un-filtering.
type predictorParams struct {
	Predictor        int
	Columns          int
	Colors           int
	BitsPerComponent int
}

// extractPredictorParams reads predictor parameters from a DecodeParms dictionary.
// Returns nil if no predictor processing is needed (predictor <= 1 or absent).
func extractPredictorParams(parms ast.PdfDict) *predictorParams {
	if parms == nil {
		return nil
	}

	predictor := 1 // default: no prediction
	if p, ok := parms["Predictor"]; ok {
		predictor = pdfInt(p)
	}
	if predictor <= 1 {
		return nil
	}

	columns := 1
	if c, ok := parms["Columns"]; ok {
		columns = pdfInt(c)
	}

	colors := 1
	if c, ok := parms["Colors"]; ok {
		colors = pdfInt(c)
	}

	bpc := 8
	if b, ok := parms["BitsPerComponent"]; ok {
		bpc = pdfInt(b)
	}

	return &predictorParams{
		Predictor:        predictor,
		Columns:          columns,
		Colors:           colors,
		BitsPerComponent: bpc,
	}
}

// pdfInt extracts an integer value from a PDF object.
func pdfInt(obj ast.PdfObject) int {
	switch v := obj.(type) {
	case ast.PdfInt:
		return int(v)
	case ast.PdfReal:
		return int(v)
	default:
		return 0
	}
}

// applyPredictor reverses the predictor encoding on decompressed data.
func applyPredictor(data []byte, params *predictorParams) ([]byte, error) {
	if params == nil {
		return data, nil
	}

	switch {
	case params.Predictor == 2:
		return applyTIFFPredictor(data, params)
	case params.Predictor >= 10 && params.Predictor <= 15:
		return applyPNGPredictor(data, params)
	default:
		return nil, fmt.Errorf("unsupported predictor value: %d", params.Predictor)
	}
}

// applyPNGPredictor reverses PNG-style predictors (10–15).
// Each row is prefixed with a single filter-type byte, followed by the
// filtered sample bytes. The row width (in bytes) is derived from Columns,
// Colors, and BitsPerComponent.
func applyPNGPredictor(data []byte, params *predictorParams) ([]byte, error) {
	bytesPerPixel := max((params.Colors*params.BitsPerComponent+7)/8, 1)
	rowBytes := (params.Columns*params.Colors*params.BitsPerComponent + 7) / 8
	stride := rowBytes + 1 // +1 for the filter-type byte

	if stride <= 1 {
		return data, nil
	}

	numRows := len(data) / stride
	if numRows == 0 {
		return data, nil
	}

	result := make([]byte, numRows*rowBytes)
	prevRow := make([]byte, rowBytes)

	for row := range numRows {
		srcOff := row * stride
		if srcOff >= len(data) {
			break
		}

		filterType := data[srcOff]
		srcRow := data[srcOff+1:]
		if len(srcRow) < rowBytes {
			// Truncated final row: copy what remains.
			srcRow = append(srcRow, make([]byte, rowBytes-len(srcRow))...)
		}

		dstOff := row * rowBytes
		curRow := result[dstOff : dstOff+rowBytes]

		switch filterType {
		case 0: // None
			copy(curRow, srcRow[:rowBytes])

		case 1: // Sub
			for i := range rowBytes {
				left := byte(0)
				if i >= bytesPerPixel {
					left = curRow[i-bytesPerPixel]
				}
				curRow[i] = srcRow[i] + left
			}

		case 2: // Up
			for i := range rowBytes {
				curRow[i] = srcRow[i] + prevRow[i]
			}

		case 3: // Average
			for i := range rowBytes {
				left := 0
				if i >= bytesPerPixel {
					left = int(curRow[i-bytesPerPixel])
				}
				up := int(prevRow[i])
				curRow[i] = srcRow[i] + byte((left+up)/2) //nolint:gosec // Average is always 0-255
			}

		case 4: // Paeth
			for i := range rowBytes {
				left := byte(0)
				if i >= bytesPerPixel {
					left = curRow[i-bytesPerPixel]
				}
				up := prevRow[i]
				upLeft := byte(0)
				if i >= bytesPerPixel {
					upLeft = prevRow[i-bytesPerPixel]
				}
				curRow[i] = srcRow[i] + paethPredictor(left, up, upLeft)
			}

		default:
			// Unknown filter type: treat as None (best-effort).
			copy(curRow, srcRow[:rowBytes])
		}

		copy(prevRow, curRow)
	}

	return result, nil
}

// paethPredictor implements the Paeth predictor function from the PNG spec.
func paethPredictor(a, b, c byte) byte {
	ia := int(a)
	ib := int(b)
	ic := int(c)
	p := ia + ib - ic

	pa := abs(p - ia)
	pb := abs(p - ib)
	pc := abs(p - ic)

	if pa <= pb && pa <= pc {
		return a
	} else if pb <= pc {
		return b
	}
	return c
}

// abs returns the absolute value of an int.
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// applyTIFFPredictor reverses TIFF Predictor 2 (horizontal differencing).
// Each sample in a row is stored as the difference from the previous sample.
func applyTIFFPredictor(data []byte, params *predictorParams) ([]byte, error) {
	numSamples := params.Columns * params.Colors
	rowBytes := (params.Columns*params.Colors*params.BitsPerComponent + 7) / 8
	if rowBytes == 0 {
		return data, nil
	}

	result := make([]byte, len(data))
	copy(result, data)

	switch params.BitsPerComponent {
	case 8:
		for off := 0; off+rowBytes <= len(result); off += rowBytes {
			row := result[off : off+rowBytes]
			for i := params.Colors; i < numSamples; i++ {
				row[i] += row[i-params.Colors]
			}
		}
		return result, nil

	case 16:
		// 16-bit samples are two big-endian bytes. Difference them as
		// uint16 so the carry crosses the byte boundary correctly; the
		// byte-level paths below would corrupt the high byte.
		for off := 0; off+rowBytes <= len(result); off += rowBytes {
			row := result[off : off+rowBytes]
			for i := params.Colors; i < numSamples; i++ {
				ci, pi := i*2, (i-params.Colors)*2
				cur := uint16(row[ci])<<8 | uint16(row[ci+1])
				prev := uint16(row[pi])<<8 | uint16(row[pi+1])
				v := cur + prev
				row[ci] = byte(v >> 8)     //nolint:gosec // high byte of the 16-bit horizontal difference
				row[ci+1] = byte(v & 0xff) //nolint:gosec // low byte of the 16-bit horizontal difference
			}
		}
		return result, nil

	case 1, 2, 4:
		// Sub-byte samples: operate at the bit level.
		bpc := params.BitsPerComponent
		mask := byte((1 << bpc) - 1)
		for off := 0; off+rowBytes <= len(result); off += rowBytes {
			row := result[off : off+rowBytes]
			for i := params.Colors; i < numSamples; i++ {
				cur := BitSample(row, i, bpc, mask)
				prev := BitSample(row, i-params.Colors, bpc, mask)
				setBitSample(row, i, bpc, mask, (cur+prev)&mask)
			}
		}
		return result, nil

	default:
		// 1/2/4/8/16 are the only BitsPerComponent values PDF images use.
		// Reject anything else rather than mis-decode it.
		return nil, fmt.Errorf("%w: TIFF predictor BitsPerComponent %d", ErrUnsupportedPredictor, params.BitsPerComponent)
	}
}

// BitSample extracts a sub-byte sample at the given sample index.
//
// Per ISO 32000-1, 8.9.3 sub-byte samples are packed MSB-first, so the sample
// at index i begins at bit i*bpc counted from the high end of the row. Exported
// because image decoding unpacks packed samples by this same rule, and the rule
// is better stated once than restated per package.
func BitSample(row []byte, sampleIdx, bpc int, mask byte) byte {
	bitPos := sampleIdx * bpc
	byteIdx := bitPos / 8
	shift := 8 - (bitPos%8 + bpc) // MSB-first packing
	return (row[byteIdx] >> shift) & mask
}

// setBitSample writes a sub-byte sample at the given sample index.
func setBitSample(row []byte, sampleIdx, bpc int, mask byte, val byte) {
	bitPos := sampleIdx * bpc
	byteIdx := bitPos / 8
	shift := 8 - (bitPos%8 + bpc)
	row[byteIdx] = (row[byteIdx] & ^(mask << shift)) | ((val & mask) << shift)
}
