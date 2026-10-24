package mutations

import (
	"bytes"
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/filter"
)

// encodeBlankedSamples compresses the samples of a blanked image, rows of
// width pixels of comps components at bpc bits, and returns the bytes and the
// /DecodeParms they need, nil when none. It is [filter.EncodeFlateSamples]
// with no limit: the samples plain and through the PNG predictors (ISO
// 32000-1, 7.4.4.4, /Predictor 15), the smaller kept, the predicted one
// counted with the /DecodeParms entry it needs. photo tries the
// prediction first, which only saves time on a photograph; the bytes are the
// same either way.
//
// The encoding is lossless on purpose. A blanked image promises two things:
// the samples the regions cover hold the blank value exactly, in the file as
// on the page, and every other sample keeps its value. A lossy encoding
// breaks both: a JPEG spreads each block's error over the whole block, so the
// blank decodes with traces of the picture beside it, and the samples outside
// the regions change. Keeping JPEG for the uncovered part would need the
// image cut into tiles or laid over a lossless underlay, which a single image
// XObject, shared by other placements and named by soft masks and structure,
// cannot be without becoming a form. litho has no JPEG 2000 encoder to keep a
// JPX image in its own filter.
//
// Prediction is what keeps a blanked photograph small: a JPEG or JPEG 2000
// picture decodes to smooth gradients that Flate alone holds poorly and the
// predictors turn into small differences.
//
// # Corpus evidence
//
//   - All corpora but pdf20, verapdf and facturx (govdocs, pdfjs, pdfcpu,
//     litho), the redact-render scenarios with and without the raster
//     fallback: the 2,640 redactions that blank an image write 2,874.2 MB
//     against 3,059.2 MB plain (govdocs 1,410.5 against 1,464.9, pdfjs
//     1,379.8 against 1,505.2, pdfcpu 83.5 against 88.8), none larger, every
//     outcome of the 17,998 scenarios unchanged, litho's rendering of every
//     blanked page and poppler's of 84 sampled the same.
//   - pdfjs issue17289, the band: the 2418 x 3234 picture blanked from a
//     1.4 MB JPX image takes 10.3 MB predicted against 12.2 MB plain. zlib's
//     best compression saves 4 % more at eight times the time; the gap to the
//     source is the JPEG 2000 coder's loss, which a lossless encoding
//     cannot take. pdfjs issue12841_reduced, the band: 17.6 MB against 31.4 MB.
//   - govdocs thread_200_200683, the whole page: each all-black picture's
//     predicted data was 15 bytes smaller and its /DecodeParms 40 bytes
//     larger, which grew the file before the entry was counted.
func encodeBlankedSamples(ctx context.Context, samples []byte, width, comps, bpc int, photo bool) ([]byte, ast.PdfDict, error) {
	// The entry the prediction adds to the dictionary counts against it: on
	// a uniform blank the predicted data saves fewer bytes than its
	// /DecodeParms takes, and the image would grow.
	var entry bytes.Buffer
	entry.WriteByte(' ')
	if _, err := writer.SerializeName(&entry, "DecodeParms"); err != nil {
		return nil, nil, fmt.Errorf("sizing the predictor parameters: %w", err)
	}
	entry.WriteByte(' ')
	if _, err := writer.Serialize(ctx, &entry, filter.PredictorParms(width, comps, bpc)); err != nil {
		return nil, nil, fmt.Errorf("sizing the predictor parameters: %w", err)
	}
	data, parms, _, err := filter.EncodeFlateSamples(samples, width, comps, bpc, -1, entry.Len(), photo)
	if err != nil {
		return nil, nil, err
	}
	return data, parms, nil
}

// photographicSource reports whether the last filter dict names is
// DCTDecode or JPXDecode, the filters of photographs. It reads a direct
// /Filter only, since the answer only orders the encodings
// [encodeBlankedSamples] tries.
func photographicSource(dict ast.PdfDict) bool {
	last := dict["Filter"]
	if arr, ok := last.(ast.PdfArray); ok && len(arr) > 0 {
		last = arr[len(arr)-1]
	}
	name, _ := last.(ast.PdfName)
	switch name {
	case "DCTDecode", "DCT", "JPXDecode":
		return true
	}
	return false
}
