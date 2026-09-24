package filter

import (
	"context"
	"errors"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/filter/jbig2"
)

// decodeJBIG2 decodes a JBIG2Decode stream (ISO 32000-1, 7.4.7): the segments
// of one page, with the global segments of the JBIG2Globals stream in parms
// (Table 12), which the document resolves before the pipeline runs.
//
// The output is one bit per pixel, rows padded to whole bytes, with 0 for a
// foreground pixel. JBIG2 codes foreground as 1 (ITU-T T.88, 5.6 NOTE 1),
// while an image in DeviceGray paints a 0 sample black, and 7.4.7 does not
// spell out the mapping between them. Inverting is the reading PDF consumers
// render: poppler draws the foreground dark in a DeviceGray JBIG2 image with
// no /Decode.
//
// A construct the decoder does not implement is reported as
// [ErrUnsupportedFilter], the way the pipeline expects of any filter it cannot
// decode; malformed data is an error of its own.
//
// # Corpus evidence
//
//   - pdfjs bitmap-template1.pdf: a DeviceGray JBIG2 image with no /Decode,
//     whose foreground poppler draws dark at 158,888 of its 159,600 pixels,
//     the rest being resampled edges.
func decodeJBIG2(ctx context.Context, data []byte, parms ast.PdfDict) ([]byte, error) {
	var globals []byte
	if stream, ok := parms["JBIG2Globals"].(ast.PdfStream); ok {
		decoded, err := DecodeStream(ctx, stream)
		if err != nil {
			return nil, fmt.Errorf("decoding JBIG2Globals: %w", err)
		}
		globals = decoded
	}
	bm, err := jbig2.Decode(ctx, data, globals)
	if err != nil {
		if errors.Is(err, jbig2.ErrUnsupported) {
			return nil, fmt.Errorf("%w: JBIG2Decode: %w", ErrUnsupportedFilter, err)
		}
		return nil, fmt.Errorf("JBIG2Decode: %w", err)
	}
	out := make([]byte, len(bm.Data))
	for i, b := range bm.Data {
		out[i] = ^b
	}
	return out, nil
}
