package recolor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/filter"
	"github.com/gotenberg/litho/pkg/internal/filter/dct"
	"github.com/gotenberg/litho/pkg/internal/filter/jpx"
)

// flateCompress is [filter.CompressFlate].
func flateCompress(data []byte, limit int) ([]byte, bool, error) {
	return filter.CompressFlate(data, limit)
}

// encodeImage encodes converted samples, rows of width x height pixels of
// comps components at bpc bits, and returns the bytes, their /DecodeParms
// and their filter. When lossy is set, the samples are 8 bits deep and a
// JPEG quality is configured, it encodes them as JPEG first (see
// [Images.encodeJPEG]) and keeps the JPEG when every lossless encoding is
// larger; else it keeps the lossless encoding (see [encodeSamples]). The
// lossless encodings stop as soon as they pass the JPEG's size, which then
// wins whatever the rest would compress to, so the decision and the output
// are those of encoding both in full. source is the last filter of the
// source image, which says which lossless encoding to try first (see
// [encodeSamples]).
//
// # Corpus evidence
//
//   - litho, pdfjs, govdocs render samples and every pdfjs, govdocs and
//     litho file holding a JPEG 2000 image (417 files): converted to CMYK,
//     lossless and at quality 85, the outputs are byte for byte those of
//     encoding in full.
//   - pdfjs issue17289.pdf: the conversion at quality 85 took 13.6 s, 14.9
//     s encoding in full, before the faster pixel memo and DCT.
func (im *Images) encodeImage(ctx context.Context, samples []byte, width, height, comps, bpc int, source ast.PdfName, lossy bool) ([]byte, ast.PdfDict, ast.PdfName, error) {
	var jpeg []byte
	limit := -1
	if lossy && bpc == 8 {
		data, ok, err := im.encodeJPEG(ctx, samples, width, height, comps)
		if err != nil {
			return nil, nil, "", err
		}
		if ok {
			jpeg, limit = data, len(data)
		}
	}
	encoded, parms, fits, err := encodeSamples(samples, width, comps, bpc, limit, source == "DCTDecode" || source == "JPXDecode")
	if err != nil {
		return nil, nil, "", fmt.Errorf("%w: %w", ErrImageNotConverted, err)
	}
	if !fits {
		return jpeg, nil, "DCTDecode", nil
	}
	return encoded, parms, "FlateDecode", nil
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
//     under Flate; pdfjs issue17289.pdf 59768197 -> 29287778 bytes.
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
// configured or when the image is past what a JPEG frame holds (more than
// 65535 pixels a side); [Images.encodeImage] keeps the lossless encoding too
// when it is no larger, since it is then better on both counts. The output
// depends on the samples and the quality alone.
//
// # Corpus evidence
//
//   - pdfjs (79 render-sampled files) converted to CMYK: 102.9 MB lossless,
//     100.6 / 101.1 / 101.9 MB at quality 85 / 90 / 95, 5 files holding
//     JPEGs (issue13999 742906 -> 445163 bytes at 85); the recolor cell
//     matches 78 in every mode, no page with colour drift past 75 ΔE*ab.
//   - govdocs (79 render-sampled files): 107.4 MB lossless, 84.5 / 87.9 /
//     93.8 MB at 85 / 90 / 95, 24 files holding JPEGs (thread_200_200427
//     4304820 -> 904014 bytes at 85); the recolor cell matches 79 in every
//     mode, no drift.
//   - govdocs thread_100_100452, thread_100_100651, thread_300_300034: at
//     95 the lossless encoding is no larger, so the file keeps its size.
func (im *Images) encodeJPEG(ctx context.Context, samples []byte, width, height, comps int) ([]byte, bool, error) {
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
	return data, true, nil
}

// encodeSamples is [filter.EncodeFlateSamples]: the samples compressed
// plain and through the PNG predictors, the smaller kept.
//
// # Corpus evidence
//
//   - pdfjs issue17289.pdf: its six JPEG 2000 photographs converted to CMYK
//     compress to fewer bytes predicted than plain.
func encodeSamples(samples []byte, width, comps, bpc, limit int, predictFirst bool) ([]byte, ast.PdfDict, bool, error) {
	return filter.EncodeFlateSamples(samples, width, comps, bpc, limit, predictFirst)
}
