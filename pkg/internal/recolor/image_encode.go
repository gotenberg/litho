package recolor

import (
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/filter"
	"github.com/gotenberg/litho/pkg/internal/filter/dct"
)

// flateCompress returns data compressed as /FlateDecode reads it (ISO
// 32000-1, 7.4.4).
func flateCompress(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	if _, err := w.Write(data); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// subsampleBelowQuality is the JPEG quality from which [Images.encodeJPEG]
// keeps the chroma of a three-component image at full resolution, and below
// which it halves it (4:2:0). It is ImageMagick's rule for its JPEG writer
// (the -sampling-factor default: 2x2 unless the quality is 90 or more): a
// quality asked that high asks for the colour edges 4:2:0 softens, and below
// it the luminance error dominates what subsampling saves.
const subsampleBelowQuality = 90

// encodeJPEG re-encodes converted samples, rows of width x height pixels of
// comps components at 8 bits, as a baseline JPEG at the configured quality,
// with Huffman tables optimized for the image (ITU-T T.81, Annex K.2). It
// reports false, to keep the lossless encoding, when no quality is
// configured, when the lossless encoding of losslessSize bytes is no larger,
// since it is then better on both counts, or when the image is past what a
// JPEG frame holds (more than 65535 pixels a side). The output depends on
// the samples and the quality alone.
func (im *Images) encodeJPEG(ctx context.Context, samples []byte, width, height, comps, losslessSize int) ([]byte, bool, error) {
	q := im.cfg.JPEGQuality
	if q == 0 {
		return nil, false, nil
	}
	opts := dct.EncodeOptions{Quality: q, OptimizeHuffman: true}
	if q < subsampleBelowQuality {
		opts.Subsampling = dct.Subsample420
	}
	data, err := dct.Encode(ctx, samples, width, height, comps, opts)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, false, ctxErr
		}
		if errors.Is(err, dct.ErrInvalidInput) {
			// The lossless encoding holds what a JPEG frame cannot, so the
			// image converts all the same.
			im.doc.Logger.Debug("recolor: keeping an image lossless that JPEG cannot hold", slog.Any("err", err))
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("%w: encoding JPEG: %w", ErrImageNotConverted, err)
	}
	if len(data) >= losslessSize {
		return nil, false, nil
	}
	return data, true, nil
}

// encodeSamples compresses image samples losslessly: rows of width pixels of
// comps components at bpc bits. It returns the encoded bytes and the
// /DecodeParms they need, nil when none.
//
// The samples are compressed both as they are and through the PNG predictors
// (ISO 32000-1, 7.4.4.4, /Predictor 15, each row's filter chosen on its own),
// and the smaller result is kept: prediction shrinks a photograph's smooth
// gradients, and adds a byte a row to flat art that compresses well anyway.
// Equal sizes keep the unpredicted data, so the output depends on the
// samples alone.
func encodeSamples(samples []byte, width, comps, bpc int) ([]byte, ast.PdfDict, error) {
	plain, err := flateCompress(samples)
	if err != nil {
		return nil, nil, fmt.Errorf("compressing the samples: %w", err)
	}
	predicted, err := filter.EncodePNGPredictor(samples, comps, bpc, width)
	if err != nil {
		return nil, nil, fmt.Errorf("predicting the samples: %w", err)
	}
	packed, err := flateCompress(predicted)
	if err != nil {
		return nil, nil, fmt.Errorf("compressing the predicted samples: %w", err)
	}
	if len(packed) >= len(plain) {
		return plain, nil, nil
	}
	parms := ast.PdfDict{"Predictor": ast.PdfInt(15), "Columns": ast.PdfInt(width)}
	// Table 8 defaults /Colors to 1 and /BitsPerComponent to 8.
	if comps != 1 {
		parms["Colors"] = ast.PdfInt(comps)
	}
	if bpc != 8 {
		parms["BitsPerComponent"] = ast.PdfInt(bpc)
	}
	return packed, parms, nil
}
