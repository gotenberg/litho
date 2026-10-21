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
	"github.com/gotenberg/litho/pkg/internal/filter/jpx"
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

// jpegSource reports whether a converted image whose source's last filter is
// filterName is re-encoded as JPEG under [ImageConfig.JPEGQuality]: a
// DCTDecode source, or a JPXDecode source none of whose JPEG 2000 components
// is deeper than 8 bits (T.800, A.5.1, SIZ Ssiz). The JPXDecode filter hands
// every sample on at 8 bits (ISO 32000-1, 7.4.9), but a deeper source keeps
// its lossless re-encoding by user decision: its producer chose precision
// over size, which a baseline JPEG, 8 bits a sample (ITU-T T.81, B.2.2),
// cannot honour. A JPEG 2000 header that does not read keeps the image
// lossless too.
//
// # Corpus evidence
//
//   - pdfjs, govdocs, litho: the 41 files holding JPEG 2000 images that
//     convert to CMYK grow from 58.8 MB to 275.9 MB lossless and 182.5 MB
//     at quality 85, their JPEG 2000 images alone from 21.1 MB to 211.0 MB
//     under Flate; pdfjs issue17289.pdf 59768197 -> 29291179 bytes.
//   - pdfjs issue9650.pdf: its JPEG 2000 images stay under Flate, which
//     holds them in fewer bytes than JPEG at 85, and issue11004.pdf barely
//     shrinks (43312716 -> 43161977 bytes) for the same reason.
func (im *Images) jpegSource(ctx context.Context, stream ast.PdfStream, filterName ast.PdfName) bool {
	if im.cfg.JPEGQuality == 0 {
		return false
	}
	switch filterName {
	case "DCTDecode":
		return true
	case "JPXDecode":
	default:
		return false
	}
	data, _, err := im.doc.DecodeStreamUpTo(ctx, stream, "JPXDecode")
	if err != nil {
		im.doc.Logger.Debug("recolor: keeping a JPEG 2000 image lossless whose data does not read", slog.Any("err", err))
		return false
	}
	img, err := jpx.Describe(data, im.doc.Limits().ScaleImageCap(jpx.DefaultMaxSamples))
	if err != nil {
		im.doc.Logger.Debug("recolor: keeping a JPEG 2000 image lossless whose header does not read", slog.Any("err", err))
		return false
	}
	for _, c := range img.Components {
		if c.Precision > 8 {
			return false
		}
	}
	return true
}

// encodeJPEG re-encodes converted samples, rows of width x height pixels of
// comps components at 8 bits, as a baseline JPEG at the configured quality,
// with Huffman tables optimized for the image (ITU-T T.81, Annex K.2). It
// reports false, to keep the lossless encoding, when no quality is
// configured, when the lossless encoding of losslessSize bytes is no larger,
// since it is then better on both counts, or when the image is past what a
// JPEG frame holds (more than 65535 pixels a side). The output depends on
// the samples and the quality alone.
//
// # Corpus evidence
//
//   - pdfjs (79 render-sampled files) converted to CMYK: 102.9 MB lossless,
//     100.6 / 101.1 / 101.9 MB at quality 85 / 90 / 95, 5 files holding
//     JPEGs (issue13999 742906 -> 445165 bytes at 85); the recolor cell
//     matches 78 in every mode, no page with colour drift past 75 ΔE*ab.
//   - govdocs (79 render-sampled files): 107.4 MB lossless, 84.5 / 87.9 /
//     93.8 MB at 85 / 90 / 95, 24 files holding JPEGs (thread_200_200427
//     4304820 -> 904563 bytes at 85); the recolor cell matches 79 in every
//     mode, no drift.
//   - govdocs thread_100_100452, thread_100_100651, thread_300_300034: at
//     95 the lossless encoding is no larger, so the file keeps its size.
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
