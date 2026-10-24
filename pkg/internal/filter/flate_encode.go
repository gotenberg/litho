package filter

import (
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"io"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// errOverLimit stops a compression whose output has passed its limit.
var errOverLimit = errors.New("filter: compressed output past its limit")

// limitedBuffer collects bytes until more than limit have been written, a
// negative limit for none.
type limitedBuffer struct {
	buf   bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.limit >= 0 && b.buf.Len()+len(p) > b.limit {
		return 0, errOverLimit
	}
	return b.buf.Write(p)
}

// CompressFlate returns data compressed as /FlateDecode reads it (ISO
// 32000-1, 7.4.4), and false, with no data, as soon as the compressed bytes
// pass limit, a negative limit for none.
func CompressFlate(data []byte, limit int) ([]byte, bool, error) {
	return CompressFlateFrom(func(w io.Writer) error {
		_, err := w.Write(data)
		return err
	}, limit)
}

// CompressFlateFrom is [CompressFlate] over the bytes write writes, which
// stops writing at the first error it is handed, so the bytes past the
// limit are never produced. The compressed bytes depend on the bytes alone,
// not on how write splits them: the compressor buffers its input into
// blocks of its own.
func CompressFlateFrom(write func(io.Writer) error, limit int) ([]byte, bool, error) {
	out := &limitedBuffer{limit: limit}
	w := zlib.NewWriter(out)
	err := write(w)
	if err == nil {
		err = w.Close()
	}
	switch {
	case errors.Is(err, errOverLimit):
		return nil, false, nil
	case err != nil:
		return nil, false, err
	}
	return out.buf.Bytes(), true, nil
}

// EncodeFlateSamples compresses image samples losslessly under /FlateDecode:
// rows of width pixels of comps components at bpc bits. It returns the
// encoded bytes and the /DecodeParms they need, nil when none, or false when
// every encoding passes limit bytes, a negative limit for none.
//
// The samples are compressed both as they are and through the PNG predictors
// (ISO 32000-1, 7.4.4.4, /Predictor 15, each row's filter chosen on its own),
// and the smaller result is kept: prediction shrinks a photograph's smooth
// gradients, and adds a byte a row to flat art that compresses well anyway.
// parmsCost is what the /DecodeParms entry of [PredictorParms] adds to the
// image dictionary, which the predicted data has to save on top to win; 0
// compares the data alone. Equal sizes keep the unpredicted data, so the
// output depends on the samples alone. Either way the bytes decode to the
// samples exactly.
//
// The second encoding stops as soon as it passes the size past which it
// loses the comparison, so the one tried first is compressed in full and
// the other mostly not. predictFirst tries the prediction first, for samples
// decoded from a JPEG or JPEG 2000 photograph, which the prediction holds in
// fewer bytes; the result is the same in either order.
func EncodeFlateSamples(samples []byte, width, comps, bpc, limit, parmsCost int, predictFirst bool) ([]byte, ast.PdfDict, bool, error) {
	plainOf := func(limit int) ([]byte, bool, error) {
		plain, fits, err := CompressFlate(samples, limit)
		if err != nil {
			return nil, false, fmt.Errorf("compressing the samples: %w", err)
		}
		return plain, fits, nil
	}
	// The rows are predicted as the compressor takes them, so a compression
	// that stops at its limit stops the prediction with it.
	packedOf := func(limit int) ([]byte, bool, error) {
		packed, fits, err := CompressFlateFrom(func(w io.Writer) error {
			return WritePNGPredictor(w, samples, comps, bpc, width)
		}, limit)
		if err != nil {
			return nil, false, fmt.Errorf("compressing the predicted samples: %w", err)
		}
		return packed, fits, nil
	}
	var plain, packed []byte
	var plainFits, packedFits bool
	var err error
	if predictFirst {
		if packed, packedFits, err = packedOf(limit); err != nil {
			return nil, nil, false, err
		}
		// A plain encoding no larger than the predicted one and its
		// /DecodeParms wins.
		plainLimit := limit
		if packedFits && (limit < 0 || len(packed)+parmsCost < limit) {
			plainLimit = len(packed) + parmsCost
		}
		if plain, plainFits, err = plainOf(plainLimit); err != nil {
			return nil, nil, false, err
		}
		if plainFits {
			packedFits = false
		}
	} else {
		if plain, plainFits, err = plainOf(limit); err != nil {
			return nil, nil, false, err
		}
		// A predicted encoding wins only below the plain one's size less its
		// /DecodeParms, so it stops one byte short of that, and is not tried
		// when nothing is left to win.
		packedLimit := limit
		if plainFits {
			packedLimit = len(plain) - parmsCost - 1
		}
		if !plainFits || packedLimit >= 0 {
			if packed, packedFits, err = packedOf(packedLimit); err != nil {
				return nil, nil, false, err
			}
		}
	}
	switch {
	case !plainFits && !packedFits:
		return nil, nil, false, nil
	case !packedFits:
		return plain, nil, true, nil
	}
	return packed, PredictorParms(width, comps, bpc), true, nil
}

// PredictorParms returns the /DecodeParms of the samples [EncodeFlateSamples]
// predicts, rows of width pixels of comps components at bpc bits under
// /Predictor 15 (ISO 32000-1, Table 8), the entries at their defaults left
// out.
func PredictorParms(width, comps, bpc int) ast.PdfDict {
	parms := ast.PdfDict{"Predictor": ast.PdfInt(15), "Columns": ast.PdfInt(width)}
	// Table 8 defaults /Colors to 1 and /BitsPerComponent to 8.
	if comps != 1 {
		parms["Colors"] = ast.PdfInt(comps)
	}
	if bpc != 8 {
		parms["BitsPerComponent"] = ast.PdfInt(bpc)
	}
	return parms
}
