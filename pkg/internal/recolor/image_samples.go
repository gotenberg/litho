package recolor

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/filter"
)

// rawSamples are the samples of an image as its filters leave them: rows of
// width x comps samples of bpc bits, each row padded to a whole byte (ISO
// 32000-1, 8.9.3).
type rawSamples struct {
	data                      []byte
	width, height, comps, bpc int
	// rows is how many whole rows data holds, at most height.
	rows int
}

func (r rawSamples) rowBytes() int {
	return (r.width*r.comps*r.bpc + 7) / 8
}

// sample returns sample i of row, counted across the row's components.
func (r rawSamples) sample(row []byte, i int) uint32 {
	switch r.bpc {
	case 8:
		return uint32(row[i])
	case 16:
		return uint32(row[2*i])<<8 | uint32(row[2*i+1])
	}
	return uint32(filter.BitSample(row, i, r.bpc, byte(1<<r.bpc-1)))
}

// readRawSamples decodes the samples of stream, an image of width x height
// pixels of comps components at bpc bits, and counts the whole rows they hold,
// which may be none. The caller passes 8 bits for a DCTDecode or JPXDecode
// image whatever its dictionary states, the depth both filters hand on (ISO
// 32000-1, 7.4.8 and 7.4.9). The bytes read are bounded by what the image's
// dimensions need, which the caller has checked against the pixel budget.
func readRawSamples(ctx context.Context, doc *document.Document, stream ast.PdfStream, width, height, comps, bpc int) (rawSamples, error) {
	switch bpc {
	case 1, 2, 4, 8, 16:
	default:
		// ISO 32000-1, 8.9.5.1, Table 89 permits these depths only.
		return rawSamples{}, fmt.Errorf("%d bits per component", bpc)
	}
	r := rawSamples{width: width, height: height, comps: comps, bpc: bpc}
	want := int64(r.rowBytes()) * int64(height)
	rc, err := doc.NewStreamReaderChecked(ctx, stream)
	if err != nil {
		return rawSamples{}, fmt.Errorf("opening the samples: %w", err)
	}
	// A JPEG's component count comes from its codestream (see
	// [measureDCT]), so up to four components a pixel are read for it.
	limit := max(want, int64(width)*int64(height)*4)
	data, err := io.ReadAll(io.LimitReader(rc, limit))
	if closeErr := rc.Close(); closeErr != nil {
		doc.Logger.Debug("recolor: closing image samples", slog.Any("err", closeErr))
	}
	if err != nil {
		return rawSamples{}, fmt.Errorf("reading the samples: %w", err)
	}
	r.data = data
	r.rows = min(height, len(data)/max(r.rowBytes(), 1))
	return r, nil
}

// padded returns r with data padded with zero bytes to its height in whole
// rows: the samples the data holds, a partial last row's among them, keep
// their values, and every sample it lacks is zero, which /Decode and the
// colour space then read as any sample, as the image decoder reads short data
// (see pdfimage.DecodeImage). Empty data is left as it is, holding no sample
// to convert.
func (r rawSamples) padded() rawSamples {
	need := r.rowBytes() * r.height
	if len(r.data) == 0 || len(r.data) >= need {
		return r
	}
	data := make([]byte, need)
	copy(data, r.data)
	r.data, r.rows = data, r.height
	return r
}

// decodeMaps returns, per component, the value a raw sample stands for under
// the /Decode array dec (ISO 32000-1, 8.9.5.2): Dmin + s x (Dmax - Dmin) /
// (2^bpc - 1). For a depth of 8 bits or less it is a table indexed by the
// sample; for 16 bits the caller computes it from the returned pairs.
func decodeMaps(dec []float64, comps, bpc int) [][]float64 {
	if bpc > 8 {
		return nil
	}
	top := float64(int(1)<<bpc - 1)
	maps := make([][]float64, comps)
	for c := range maps {
		lo, hi := dec[2*c], dec[2*c+1]
		m := make([]float64, 1<<bpc)
		for s := range m {
			m[s] = lo + float64(s)*(hi-lo)/top
		}
		maps[c] = m
	}
	return maps
}

// imageDecode returns the /Decode array of dict for comps components, or def
// when it is absent or too short. A longer array keeps its leading pairs, as
// the image decoder and PDF.js read it.
func imageDecode(ctx context.Context, doc *document.Document, dict ast.PdfDict, comps int, def []float64) []float64 {
	obj, ok := dict["Decode"]
	if !ok {
		return def
	}
	arr, err := document.ResolveArray(ctx, doc, obj)
	if err != nil || len(arr) < 2*comps {
		return def
	}
	dec := make([]float64, 2*comps)
	for i := range dec {
		v, err := document.ResolveObject(ctx, doc, arr[i])
		if err != nil {
			return def
		}
		f, ok := document.Number(v)
		if !ok {
			return def
		}
		dec[i] = f
	}
	return dec
}
