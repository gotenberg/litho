package filter

import "fmt"

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
	switch bpc {
	case 1, 2, 4, 8, 16:
	default:
		return nil, fmt.Errorf("%w: BitsPerComponent %d", ErrPredictorLayout, bpc)
	}
	if colors < 1 || colors > maxPredictorColors {
		return nil, fmt.Errorf("%w: Colors %d", ErrPredictorLayout, colors)
	}
	if columns < 1 {
		return nil, fmt.Errorf("%w: Columns %d", ErrPredictorLayout, columns)
	}
	// columns*colors*bpc cannot overflow once columns is checked against the
	// data: a row is at most len(data) bytes, so columns is at most
	// 8*len(data), and colors*bpc at most 512.
	if columns > 8*len(data) {
		return nil, fmt.Errorf("%w: %d columns over %d bytes", ErrPredictorLayout, columns, len(data))
	}
	rowBytes := (columns*colors*bpc + 7) / 8
	if len(data) == 0 || len(data)%rowBytes != 0 {
		return nil, fmt.Errorf("%w: %d bytes are not whole rows of %d", ErrPredictorLayout, len(data), rowBytes)
	}
	bpp := max((colors*bpc+7)/8, 1)
	rows := len(data) / rowBytes

	out := make([]byte, 0, rows*(rowBytes+1))
	prev := make([]byte, rowBytes)
	var candidates [5][]byte
	for i := range candidates {
		candidates[i] = make([]byte, rowBytes)
	}
	for r := range rows {
		cur := data[r*rowBytes : (r+1)*rowBytes]
		best, bestScore := 0, -1
		for ft := range candidates {
			predictRow(candidates[ft], cur, prev, bpp, byte(ft)) //nolint:gosec // ft ranges over the five PNG filter types.
			score := 0
			for _, b := range candidates[ft] {
				score += abs(int(int8(b))) //nolint:gosec // reinterpreting the byte as signed is the heuristic.
			}
			if bestScore < 0 || score < bestScore {
				best, bestScore = ft, score
			}
		}
		out = append(out, byte(best)) //nolint:gosec // best is a filter type, 0 to 4.
		out = append(out, candidates[best]...)
		prev = cur
	}
	return out, nil
}

// predictRow writes into dst the PNG filter ft applied to cur, whose previous
// row is prev and whose samples are bpp bytes apart. It is the inverse of the
// cases of applyPNGPredictor.
func predictRow(dst, cur, prev []byte, bpp int, ft byte) {
	for i := range cur {
		var left, upLeft byte
		if i >= bpp {
			left = cur[i-bpp]
			upLeft = prev[i-bpp]
		}
		up := prev[i]
		switch ft {
		case 0:
			dst[i] = cur[i]
		case 1:
			dst[i] = cur[i] - left
		case 2:
			dst[i] = cur[i] - up
		case 3:
			dst[i] = cur[i] - byte((int(left)+int(up))/2) //nolint:gosec // the average of two bytes is a byte.
		default:
			dst[i] = cur[i] - paethPredictor(left, up, upLeft)
		}
	}
}
