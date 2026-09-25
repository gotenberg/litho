package filter

import (
	"context"
	"errors"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/limits"
)

// codecProbe finds, from the headers of a codec filter's input data alone,
// what the filter's built-in decoder does with it, without decoding the coded
// data. It returns an error wrapping [ErrUnsupportedFilter] when the decoder
// refuses a feature the data uses, the decoder's error when it fails on the
// data before that, nil when it decodes the data as far as the headers tell,
// and errUnsettled when only decoding tells. parms and dict are what
// applyDecodeFilter hands the decoder, and lim the budgets it decodes under.
type codecProbe func(ctx context.Context, data []byte, parms, dict ast.PdfDict, lim limits.Limits) error

// codecProbes holds the probe of each filter whose built-in decoder refuses a
// feature it lacks as it meets it in the data. Each probe reads what its
// decoder reads before the samples, through the functions the decoder uses:
//
//   - JPXDecode: the JP2 boxes and codestream markers, the channels the image
//     dictionary selects, and each tile's coding parameters (see
//     [jpx.Image.Check]). A code-block whose bit-planes pass the decoder's cap
//     is refused only once a packet gives it a coding pass, which the probe
//     does not read: that data is unsettled.
//   - JBIG2Decode: the segment headers of the page and of its JBIG2Globals
//     stream (see [jbig2.Check]).
//   - CCITTFaxDecode: the first row, the only one whose failure fails the
//     decoding (see [ccitt.Check]).
//   - DCTDecode: the marker segments up to the first scan, the frame header
//     among them (see [dct.Check]). A frame marker past the first scan, which
//     starts a second frame, leaves the data unsettled.
//
// The Crypt filter reads its parameters alone, so it runs as it is.
var codecProbes = map[ast.PdfName]codecProbe{
	"JPXDecode":      probeJPX,
	"JBIG2Decode":    probeJBIG2,
	"CCITTFaxDecode": probeCCITTFax,
	"CCF":            probeCCITTFax,
	"DCTDecode":      probeDCT,
	"DCT":            probeDCT,
}

// skipsFilter reports whether decoding skips a filter that failed with err,
// handing its input on still encoded: litho cannot decode it. A stream a
// plugin declined for a filter litho has no decoder for fails instead (see
// [Decoder.applyDecodeFilter]).
func skipsFilter(err error) bool {
	return errors.Is(err, ErrUnsupportedFilter) && !errors.Is(err, errPluginDeclined)
}

// probeChain returns the filters decodeChain skips in the /Filter chain of
// stream, and the error it fails with, as far as the listing of
// [Decoder.UndecodableFilters] depends on them, without decoding the data of a
// codec filter: its probe answers instead (see codecProbes). The other
// filters run as decodeChain runs them, up to the last one whose decoder can
// refuse a feature; a filter after it cannot be skipped for a feature, and
// whether it fails leaves the listing as it is, so it does not run. A filter
// no decoder knows is skipped whatever its input, as in decodeChain.
//
// It returns errUnsettled when a probe cannot tell, or when a filter that
// must run follows a codec whose output the probe did not decode: only
// decoding tells then.
func probeChain(ctx context.Context, stream ast.PdfStream, d *Decoder) ([]ast.PdfName, error) {
	filters, err := streamFilterNames(stream.Dict)
	if err != nil {
		return nil, err
	}
	parms := extractDecodeParms(stream.Dict, len(filters))
	last := -1
	for i, f := range filters {
		if refusesUnsupported(f) {
			last = i
		}
	}

	data, err := stream.Bytes(ctx)
	if err != nil {
		return nil, err
	}
	var skipped []ast.PdfName
	// decoded reports whether data is the input of the next filter: a probe
	// that finds a codec decodes leaves its output uncomputed.
	decoded := true
	for i, f := range filters {
		switch {
		case !Decodable(f) && !d.Handles(f):
			skipped = append(skipped, f)
			continue
		case i > last:
			continue
		case !decoded:
			return skipped, errUnsettled
		}
		var out []byte
		var ferr error
		if probe, isCodec := codecProbes[f]; isCodec && !d.Handles(f) {
			if ferr = probe(ctx, data, parms[i], stream.Dict, d.limits()); ferr == nil {
				decoded = false
				continue
			}
		} else {
			out, ferr = d.applyDecodeFilter(ctx, data, f, parms[i], stream.Dict)
		}
		switch {
		case ferr == nil:
			data = out
		case errors.Is(ferr, errUnsettled):
			return skipped, errUnsettled
		case keepsDamagedPrefix(out, ferr):
			data = out
		case skipsFilter(ferr):
			skipped = append(skipped, f)
		default:
			return skipped, ferr
		}
	}
	return skipped, nil
}
