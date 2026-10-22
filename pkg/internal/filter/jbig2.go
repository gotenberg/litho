package filter

import (
	"context"
	"errors"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/filter/jbig2"
	"github.com/gotenberg/litho/pkg/internal/limits"
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
// The bitmap cap, in bytes ([jbig2.DefaultMaxBitmapBytes]), scales with the
// image budget of lim, and the globals stream decodes under its stream budget.
//
// # Corpus evidence
//
//   - pdfjs bitmap-template1.pdf: a DeviceGray JBIG2 image with no /Decode,
//     whose foreground poppler draws dark at 158,888 of its 159,600 pixels,
//     the rest being resampled edges.
func decodeJBIG2(ctx context.Context, data []byte, parms ast.PdfDict, lim limits.Limits) ([]byte, error) {
	globals, err := jbig2Globals(ctx, parms, lim)
	if err != nil {
		return nil, err
	}
	bm, err := jbig2.Decode(ctx, data, globals, lim.ScaleImageCap(jbig2.DefaultMaxBitmapBytes))
	if err != nil {
		return nil, jbig2Error(err)
	}
	// The bitmap is this call's own, so it inverts in place: a copy would
	// hold the page twice, up to two bitmap caps.
	for i, b := range bm.Data {
		bm.Data[i] = ^b
	}
	return bm.Data, nil
}

// probeJBIG2 is the probe of JBIG2Decode (see codecProbes): decodeJBIG2 with
// [jbig2.Check] in place of decoding the regions.
func probeJBIG2(ctx context.Context, data []byte, parms, _ ast.PdfDict, lim limits.Limits) error {
	globals, err := jbig2Globals(ctx, parms, lim)
	if err != nil {
		return err
	}
	if err := jbig2.Check(ctx, data, globals); err != nil {
		return jbig2Error(err)
	}
	return nil
}

// jbig2Globals returns the decoded JBIG2Globals stream of parms, or nil when
// there is none, decoded under the stream budget of lim.
func jbig2Globals(ctx context.Context, parms ast.PdfDict, lim limits.Limits) ([]byte, error) {
	stream, ok := parms["JBIG2Globals"].(ast.PdfStream)
	if !ok {
		return nil, nil
	}
	decoded, err := NewDecoder(nil, lim).DecodeStream(ctx, stream)
	if err != nil {
		return nil, fmt.Errorf("decoding JBIG2Globals: %w", err)
	}
	return decoded, nil
}

// jbig2Error wraps an error of the jbig2 package, marking data that uses a
// construct the decoder lacks as an unsupported filter.
func jbig2Error(err error) error {
	if errors.Is(err, jbig2.ErrUnsupported) {
		return fmt.Errorf("%w: JBIG2Decode: %w", ErrUnsupportedFilter, err)
	}
	return fmt.Errorf("JBIG2Decode: %w", err)
}
