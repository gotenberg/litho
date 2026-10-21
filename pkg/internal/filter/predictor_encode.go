package filter

import (
	"bytes"
	"fmt"
	"io"
)

// maxPredictorColors caps the Colors a predictor encode accepts. Not a DoS
// guard: it is the largest component count a colour space has, the 32
// colorants ISO 32000-1, Annex C.2 lets a DeviceN space hold, and a reader is
// entitled to reject more.
const maxPredictorColors = 32

// EncodePNGPredictor applies PNG prediction to data, the encoding a
// /FlateDecode /DecodeParms with /Predictor 15 undoes (ISO 32000-1, 7.4.4.4
// and Table 8). Each row of data gains a leading filter-type byte, and the
// filter of every row is chosen on its own, which /Predictor 15 allows: a
// decoder reads the type from the byte and ignores the predictor value beyond
// "PNG".
//
// A row is columns samples of colors components of bpc bits each, padded to a
// whole byte as Table 8 states. data must hold a whole number of rows, since a
// partial final row is where decoders disagree: litho's decoder drops it, and
// another reader may pad it. It returns [ErrPredictorLayout] otherwise, and for
// a bpc, colors or columns Table 8 does not allow.
//
// The filter of each row is the one whose output has the least sum of absolute
// values as signed bytes, the heuristic the PNG specification recommends
// (ISO/IEC 15948, 12.8), with ties going to the lowest filter type so the output
// depends on data alone.
func EncodePNGPredictor(data []byte, colors, bpc, columns int) ([]byte, error) {
	rowBytes, err := predictorRowBytes(len(data), colors, bpc, columns)
	if err != nil {
		return nil, err
	}
	out := bytes.NewBuffer(make([]byte, 0, len(data)+len(data)/rowBytes))
	if err := writePNGPredicted(out, data, colors, bpc, rowBytes); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// WritePNGPredictor writes to w what [EncodePNGPredictor] returns for the
// same arguments, a row at a time, and stops at the first error w returns,
// which it returns as it is: a writer that refuses more bytes stops the
// prediction with it, so a caller that needs the output only while it stays
// small pays for no more of it.
func WritePNGPredictor(w io.Writer, data []byte, colors, bpc, columns int) error {
	rowBytes, err := predictorRowBytes(len(data), colors, bpc, columns)
	if err != nil {
		return err
	}
	return writePNGPredicted(w, data, colors, bpc, rowBytes)
}

// predictorRowBytes checks the layout of n bytes of samples for PNG
// prediction and returns the bytes a row takes.
func predictorRowBytes(n, colors, bpc, columns int) (int, error) {
	switch bpc {
	case 1, 2, 4, 8, 16:
	default:
		return 0, fmt.Errorf("%w: BitsPerComponent %d", ErrPredictorLayout, bpc)
	}
	if colors < 1 || colors > maxPredictorColors {
		return 0, fmt.Errorf("%w: Colors %d", ErrPredictorLayout, colors)
	}
	if columns < 1 {
		return 0, fmt.Errorf("%w: Columns %d", ErrPredictorLayout, columns)
	}
	// columns*colors*bpc cannot overflow once columns is checked against the
	// data: a row is at most n bytes, so columns is at most 8*n, and
	// colors*bpc at most 512.
	if columns > 8*n {
		return 0, fmt.Errorf("%w: %d columns over %d bytes", ErrPredictorLayout, columns, n)
	}
	rowBytes := (columns*colors*bpc + 7) / 8
	if n == 0 || n%rowBytes != 0 {
		return 0, fmt.Errorf("%w: %d bytes are not whole rows of %d", ErrPredictorLayout, n, rowBytes)
	}
	return rowBytes, nil
}

// writePNGPredicted writes the predicted rows of data, whose layout
// predictorRowBytes checked, to w.
func writePNGPredicted(w io.Writer, data []byte, colors, bpc, rowBytes int) error {
	bpp := max((colors*bpc+7)/8, 1)
	rows := len(data) / rowBytes
	prev := make([]byte, rowBytes)
	// Each candidate holds its filter-type byte before the filtered row, so
	// the chosen one is written in one call.
	var candidates [5][]byte
	for ft := range candidates {
		candidates[ft] = make([]byte, rowBytes+1)
		candidates[ft][0] = byte(ft) //nolint:gosec // ft ranges over the five PNG filter types.
	}
	for r := range rows {
		cur := data[r*rowBytes : (r+1)*rowBytes]
		best, bestScore := 0, -1
		for ft := range candidates {
			score := predictRow(candidates[ft][1:], cur, prev, bpp, byte(ft)) //nolint:gosec // ft ranges over the five PNG filter types.
			if bestScore < 0 || score < bestScore {
				best, bestScore = ft, score
			}
		}
		if _, err := w.Write(candidates[best]); err != nil {
			return err
		}
		prev = cur
	}
	return nil
}

// predictRow writes into dst the PNG filter ft applied to cur, whose previous
// row is prev and whose samples are bpp bytes apart, and returns the sum of
// the absolute values of dst as signed bytes. It is the inverse of the cases
// of applyPNGPredictor. Each filter runs a loop of its own, split where the
// left neighbour starts, since this loop is the cost of the prediction.
func predictRow(dst, cur, prev []byte, bpp int, ft byte) int {
	n := len(cur)
	head := min(bpp, n)
	dst, prev = dst[:n], prev[:n]
	score := 0
	switch ft {
	case 0:
		for i, c := range cur {
			dst[i] = c
			score += absInt8(c)
		}
	case 1:
		for i := range head {
			dst[i] = cur[i]
			score += absInt8(cur[i])
		}
		for i := head; i < n; i++ {
			d := cur[i] - cur[i-bpp]
			dst[i] = d
			score += absInt8(d)
		}
	case 2:
		for i, c := range cur {
			d := c - prev[i]
			dst[i] = d
			score += absInt8(d)
		}
	case 3:
		for i := range head {
			d := cur[i] - prev[i]/2
			dst[i] = d
			score += absInt8(d)
		}
		for i := head; i < n; i++ {
			d := cur[i] - byte((int(cur[i-bpp])+int(prev[i]))/2) //nolint:gosec // the average of two bytes is a byte.
			dst[i] = d
			score += absInt8(d)
		}
	default:
		for i := range head {
			d := cur[i] - paethPredictor(0, prev[i], 0)
			dst[i] = d
			score += absInt8(d)
		}
		for i := head; i < n; i++ {
			d := cur[i] - paethPredictor(cur[i-bpp], prev[i], prev[i-bpp])
			dst[i] = d
			score += absInt8(d)
		}
	}
	return score
}

// absInt8 returns the magnitude of b read as a signed byte.
func absInt8(b byte) int {
	v := int(int8(b)) //nolint:gosec // reinterpreting the byte as signed is the heuristic.
	if v < 0 {
		return -v
	}
	return v
}
