package filter

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/andybalholm/brotli"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/intconv"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/plugins"
)

// Decoder decodes streams through their filter chains: it consults its filter
// plugins before the built-in decoders, and bounds every decode by its
// resource budgets. A nil *Decoder is valid and behaves as the package-level
// functions do: it consults no plugin and applies [limits.Default].
//
// A Decoder is immutable once built, so it is safe for concurrent use.
type Decoder struct {
	plugins map[ast.PdfName]plugins.FilterDecoder
	lim     limits.Limits
	// refuse fails a stream whose data a filter salvaged or kept only the
	// prefix of (see [Decoder.RefuseSalvaged]).
	refuse bool
}

// NewDecoder creates a Decoder with the given filter plugins, bounding every
// decode by lim, whose zero budgets read as their defaults. Duplicate filter
// names are resolved in favor of the last plugin registered.
func NewDecoder(pp []plugins.FilterDecoder, lim limits.Limits) *Decoder {
	d := &Decoder{lim: lim.WithDefaults()}
	if len(pp) > 0 {
		d.plugins = make(map[ast.PdfName]plugins.FilterDecoder, len(pp))
		for _, p := range pp {
			d.plugins[ast.PdfName(p.FilterName())] = p
		}
	}
	return d
}

// limits returns the budgets d decodes under. A nil *Decoder has the defaults.
func (d *Decoder) limits() limits.Limits {
	if d == nil {
		return limits.Default()
	}
	return d.lim.WithDefaults()
}

// builtin returns a Decoder that consults no plugin and decodes under d's
// budgets, refusing salvaged data when d does.
func (d *Decoder) builtin() *Decoder {
	return &Decoder{lim: d.limits(), refuse: d.refusesSalvaged()}
}

// RefuseSalvaged returns a Decoder that decodes as d does, with its plugins
// and budgets, but fails with [ErrSalvagedStream] a stream d hands on
// salvaged: one a filter met damaged data in and kept the prefix of
// ([DecodeReport.Damaged]), or one whose data broke a rule of its format and
// decoded whole all the same ([DecodeReport.Salvaged]). A filter that cannot
// decode at all is still skipped, as d skips it.
//
// It reads the filters as ISO 32000-1 states them, without the leniency
// [keepsDamagedPrefix], [endsAtSyncFlush] and [decodeLZW] name: FlateDecode
// data is a zlib stream (7.4.4), whose header and Adler-32 checksum RFC 1950,
// 2.2 requires and whose deflate data ends with a final block (RFC 1951,
// 3.2.3); LZWDecode data ends with its EOD code (7.4.4.2); and the ASCII
// filters' data holds only the characters 7.4.2 and 7.4.3 define.
//
// Such a decoder streams nothing: [Decoder.NewStreamReader] decodes every
// stream eagerly, under the stream budget, so it refuses a stream before
// handing on any of its bytes.
func (d *Decoder) RefuseSalvaged() *Decoder {
	out := &Decoder{lim: d.limits(), refuse: true}
	if d != nil {
		out.plugins = d.plugins
	}
	return out
}

// refusesSalvaged reports whether d fails a stream whose data a filter
// salvaged. A nil *Decoder does not.
func (d *Decoder) refusesSalvaged() bool {
	return d != nil && d.refuse
}

// Handles reports whether a plugin is registered for the named filter.
// A nil *Decoder handles nothing.
func (d *Decoder) Handles(name ast.PdfName) bool {
	_, ok := d.plugin(name)
	return ok
}

// plugin returns the plugin registered for the named filter. A nil *Decoder
// has none.
func (d *Decoder) plugin(name ast.PdfName) (plugins.FilterDecoder, bool) {
	if d == nil {
		return nil, false
	}
	p, ok := d.plugins[name]
	return p, ok
}

// DecodeStream decodes a PDF stream, consulting registered plugins for
// filter names they handle before falling back to built-in decoders.
//
// A filter neither a plugin nor a built-in decoder handles is skipped, as the
// package-level [DecodeStream] skips it, so registering a plugin for one filter
// leaves every other filter decoding as it did without plugins. A stream a
// plugin declines goes to the built-in decoder for its filter, and fails with
// [ErrUnsupportedFilter] when litho has none (see [Decoder.applyDecodeFilter]).
func (d *Decoder) DecodeStream(ctx context.Context, stream ast.PdfStream) ([]byte, error) {
	data, _, err := d.DecodeStreamChecked(ctx, stream)
	return data, err
}

// DecodeStreamChecked is [Decoder.DecodeStream] that also reports whether
// decoding was complete, with the contract of the package-level
// [DecodeStreamChecked]: incomplete is true when a filter in the chain was
// skipped and the returned bytes remain encoded by it, or when a filter met
// damaged data and handed on only what it decoded before the damage.
func (d *Decoder) DecodeStreamChecked(ctx context.Context, stream ast.PdfStream) (data []byte, incomplete bool, err error) {
	data, report, err := decodeChain(ctx, stream, d, false)
	return data, report.Incomplete(), err
}

// DecodeStreamReport is [Decoder.DecodeStream] that also reports which
// filters left the returned bytes short of the whole decoded stream: those it
// skipped, and those that met damaged data. On an error the report covers the
// filters that ran before it.
func (d *Decoder) DecodeStreamReport(ctx context.Context, stream ast.PdfStream) ([]byte, DecodeReport, error) {
	return decodeChain(ctx, stream, d, false)
}

// UndecodableFilters returns, in chain order, the filters of stream's chain
// that decoding skips because litho cannot decode them: the Skipped of
// [Decoder.DecodeStreamReport], found without decoding the stream's image
// data.
//
// A filter neither a built-in decoder nor a plugin knows is found from its
// name. A built-in decoder that refuses a feature it lacks cannot be judged by
// name (see refusesUnsupported): a chain naming one runs through probeChain,
// which reads the headers of the codec's data where decoding would decode it
// (see codecProbes), and consults a plugin that claims a filter as decoding
// consults it. A chain the headers cannot settle is decoded, as is the data of
// a codec a plugin claims. When the chain fails before its end, the filters
// after the failure never run, and those among them the names settle are
// listed.
//
// The answer is decoding's for data that decodes past its headers. A stream
// whose data also fails decoding before the construct its decoder refuses,
// damaged or past a decoding limit, fails with that error, and is listed all
// the same.
//
// The error is only ever the context's.
func (d *Decoder) UndecodableFilters(ctx context.Context, stream ast.PdfStream) ([]ast.PdfName, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	names, err := streamFilterNames(stream.Dict)
	if err != nil {
		// Decoding fails on an invalid /Filter rather than skip a filter.
		return nil, nil //nolint:nilerr // Decoding reports an invalid /Filter; this report names only filters it cannot decode.
	}
	unknown := d.unknownFilters(names)
	if !slices.ContainsFunc(names, refusesUnsupported) {
		return unknown, nil
	}
	skipped, err := probeChain(ctx, stream, d)
	if errors.Is(err, errUnsettled) {
		var report DecodeReport
		_, report, err = decodeChain(ctx, stream, d, false)
		skipped = report.Skipped
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	if err == nil {
		return skipped, nil
	}
	var out []ast.PdfName
	for _, n := range names {
		if slices.Contains(skipped, n) || slices.Contains(unknown, n) {
			out = append(out, n)
		}
	}
	return out, nil
}

// SalvagedFilters returns, in chain order, the filters of stream's chain
// whose data decoding salvages: those [Decoder.DecodeStreamReport] lists as
// Damaged or Salvaged. It decodes the chain up to its first image codec,
// DCTDecode, JPXDecode or JBIG2Decode, or its first filter a plugin claims,
// and no further: those keep no damaged prefix and record no salvage (see
// [keepsDamagedPrefix]), and decoding them would cost the call the image's
// decoding. A filter that fails or that litho cannot decode ends the walk, and
// the filters before it are listed.
//
// The error is only ever the context's.
func (d *Decoder) SalvagedFilters(ctx context.Context, stream ast.PdfStream) ([]ast.PdfName, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	filters, err := streamFilterNames(stream.Dict)
	if err != nil {
		return nil, nil //nolint:nilerr // Decoding fails on an invalid /Filter; this report names only filters whose data it salvages.
	}
	parms := extractDecodeParms(stream.Dict, len(filters))
	data, err := stream.Bytes(ctx)
	if err != nil {
		return nil, ctx.Err()
	}
	lim := d.limits()
	var out []ast.PdfName
	for i, f := range filters {
		if d.Handles(f) || endsSalvageWalk(f) {
			break
		}
		decoded, derr := applyDecodeFilter(ctx, data, f, parms[i], stream.Dict, lim)
		if derr != nil && !keepsDecoded(decoded, derr) {
			break
		}
		if derr != nil {
			out = append(out, f)
		}
		data = decoded
	}
	return out, ctx.Err()
}

// endsSalvageWalk reports whether [Decoder.SalvagedFilters] stops before the
// named filter: an image codec, which keeps no damaged prefix and records no
// salvage.
func endsSalvageWalk(name ast.PdfName) bool {
	switch name {
	case "DCTDecode", "DCT", "JPXDecode", "JBIG2Decode":
		return true
	}
	return false
}

// unknownFilters returns the names neither a built-in decoder nor a plugin
// of d decodes.
func (d *Decoder) unknownFilters(names []ast.PdfName) []ast.PdfName {
	var out []ast.PdfName
	for _, n := range names {
		if !Decodable(n) && !d.Handles(n) {
			out = append(out, n)
		}
	}
	return out
}

// DecodeStreamUpTo is the package-level [DecodeStreamUpTo] under d's budgets.
// It consults no plugin: the filters it skips past are built-in ones, since a
// JPEG 2000 or JBIG2 codestream is what a chain ends with, never what it passes
// through, so the result does not vary with which plugins are registered.
func (d *Decoder) DecodeStreamUpTo(ctx context.Context, stream ast.PdfStream, stop ast.PdfName) (data []byte, found bool, err error) {
	filters, err := streamFilterNames(stream.Dict)
	if err != nil {
		return nil, false, err
	}
	parms := extractDecodeParms(stream.Dict, len(filters))
	lim := d.limits()

	data, err = stream.Bytes(ctx)
	if err != nil {
		return nil, false, err
	}
	for i, f := range filters {
		if f == stop {
			return data, true, nil
		}
		decoded, derr := applyDecodeFilter(ctx, data, f, parms[i], stream.Dict, lim)
		if derr != nil && !keepsDecoded(decoded, derr) {
			return nil, false, fmt.Errorf("decoding %s before %s: %w", f, stop, derr)
		}
		if derr != nil && d.refusesSalvaged() {
			return nil, false, fmt.Errorf("%w: decoding %s before %s: %w", ErrSalvagedStream, f, stop, derr)
		}
		data = decoded
	}
	return data, false, nil
}

// NewStreamReader returns an io.ReadCloser that lazily decodes a PDF stream
// through its filter pipeline, consulting registered plugins for filter names
// they handle. For a single FlateDecode, LZWDecode or BrotliDecode filter
// without predictor parameters, the decoded data is streamed without full
// buffering, and so without the stream budget, which bounds what a decode
// holds in memory. For other filter configurations, and whenever a plugin
// claims a filter of the chain, the stream is decoded eagerly into a buffer
// under d's budgets.
//
// The caller must close the returned reader when done.
func (d *Decoder) NewStreamReader(ctx context.Context, stream ast.PdfStream) (io.ReadCloser, error) {
	return d.newStreamReader(ctx, stream, false)
}

// NewStreamReaderChecked is [Decoder.NewStreamReader] for a caller that must
// not read still-encoded bytes as decoded ones. Where NewStreamReader skips a
// filter litho cannot decode and hands its input on, NewStreamReaderChecked
// fails with [ErrUnsupportedFilter]: at once for a filter no decoder knows,
// naming every such filter of the chain, and at the decoder's refusal for a
// built-in one that meets a feature it lacks. [Decoder.UndecodableFilters]
// lists a filter of every stream it refuses. It refuses every listed stream,
// unless the data fails decoding first, damaged or past a decoding limit.
//
// Damaged data is not refused: the bytes a filter decoded before the damage
// are read as NewStreamReader reads them, unless d refuses salvaged data
// ([Decoder.RefuseSalvaged]).
func (d *Decoder) NewStreamReaderChecked(ctx context.Context, stream ast.PdfStream) (io.ReadCloser, error) {
	return d.newStreamReader(ctx, stream, true)
}

// newStreamReader is [Decoder.NewStreamReader], and with strict
// [Decoder.NewStreamReaderChecked].
func (d *Decoder) newStreamReader(ctx context.Context, stream ast.PdfStream, strict bool) (io.ReadCloser, error) {
	// streamFilterNames reads a null /Filter as absent, as [DecodeStream]
	// does. This path used to reject it as an invalid type, so the same
	// stream decoded eagerly and failed to open lazily.
	filters, err := streamFilterNames(stream.Dict)
	if err != nil {
		return nil, err
	}
	if len(filters) == 0 {
		data, err := stream.Bytes(ctx)
		if err != nil {
			return nil, err
		}
		return io.NopCloser(bytes.NewReader(data)), nil
	}
	if unknown := d.unknownFilters(filters); strict && len(unknown) > 0 {
		return nil, fmt.Errorf("%w: litho cannot decode %s", ErrUnsupportedFilter, joinNames(unknown))
	}

	for _, f := range filters {
		if d.Handles(f) {
			// Plugin path: eager decode, then wrap in a reader.
			data, _, err := decodeChain(ctx, stream, d, strict)
			if err != nil {
				return nil, err
			}
			return io.NopCloser(bytes.NewReader(data)), nil
		}
	}

	parms := extractDecodeParms(stream.Dict, len(filters))

	// Fast path: single Flate or LZW filter without predictor, true streaming.
	// A decoder that refuses salvaged data decodes eagerly instead, so that it
	// refuses a stream before handing on any of its bytes.
	if len(filters) == 1 && extractPredictorParams(parms[0]) == nil && !d.refusesSalvaged() {
		switch filters[0] {
		case "FlateDecode", "Fl":
			raw, err := stream.Bytes(ctx)
			if err != nil {
				return nil, err
			}
			r, framed, err := openFlate(raw)
			if err != nil {
				return nil, err
			}
			t := &tolerantReader{r: r, c: r, filter: filters[0], tolerate: func(error) bool { return true }}
			if !framed {
				t.endDefect = func() error { return errZlibHeaderDamaged }
			}
			return t, nil
		case "LZWDecode", "LZW":
			earlyChange := 1
			if parms[0] != nil {
				if ec, ok := parms[0]["EarlyChange"]; ok {
					earlyChange = pdfInt(ec)
				}
			}
			raw, err := stream.Bytes(ctx)
			if err != nil {
				return nil, err
			}
			lr := newLZWReader(bytes.NewReader(raw), earlyChange)
			return &tolerantReader{
				r:        lr,
				filter:   filters[0],
				tolerate: func(err error) bool { return err == io.ErrUnexpectedEOF },
				endDefect: func() error {
					if lr.eod {
						return nil
					}
					return errLZWNoEOD
				},
			}, nil
		case "BrotliDecode":
			raw, err := stream.Bytes(ctx)
			if err != nil {
				return nil, err
			}
			return io.NopCloser(brotli.NewReader(bytes.NewReader(raw))), nil
		}
	}

	// Slow path: decode fully and wrap in a reader.
	data, _, err := decodeChain(ctx, stream, d, strict)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

// joinNames lists filter names for an error message.
func joinNames(names []ast.PdfName) string {
	s := make([]string, len(names))
	for i, n := range names {
		s[i] = string(n)
	}
	return strings.Join(s, ", ")
}

// applyDecodeFilter applies a single filter, checking plugins first. A nil d
// checks none.
//
// A plugin that returns [plugins.ErrUnsupportedFilter] declines the stream,
// and the built-in decoder for the filter runs instead. When there is none,
// the stream fails with [ErrUnsupportedFilter], as
// [github.com/gotenberg/litho/pkg/pdf.WithFilterPlugin] documents, rather
// than be skipped like a filter no plugin claims: the diagnostics report a
// claimed filter as decodable, and a skip would hand a caller that trusted
// them bytes still encoded by it.
//
// A plugin returns its whole output in one slice, so [limitedCopy] never sees
// it and the stream budget bounded every built-in filter and no plugin at
// all. The filter a document names decided whether the bomb guard applied: a
// blank 17000x17000 JPEG 2000 image encodes to 1,204 bytes and decodes to 289
// MB, past the same cap that refuses a FlateDecode stream of that size.
//
// The bound here cannot prevent the plugin's own allocation, which has already
// happened by the time it returns. It stops those bytes propagating and keeps
// litho from holding a second copy, and it holds for third-party plugins that
// know nothing of this package's limits.
func (d *Decoder) applyDecodeFilter(ctx context.Context, data []byte, filterName ast.PdfName, parms, dict ast.PdfDict) ([]byte, error) {
	lim := d.limits()
	if p, ok := d.plugin(filterName); ok {
		pubParms, err := d.marshalParms(ctx, filterName, parms)
		if err != nil {
			return nil, err
		}
		decoded, err := p.Decode(data, pubParms)
		switch {
		case errors.Is(err, plugins.ErrUnsupportedFilter) && !Decodable(filterName):
			return nil, fmt.Errorf("%w: %s: %w", ErrUnsupportedFilter, filterName, errPluginDeclined)
		case errors.Is(err, plugins.ErrUnsupportedFilter):
			// Declined: fall through to the built-in decoder.
		case err != nil:
			return nil, err
		case int64(len(decoded)) > lim.MaxStreamBytes:
			// Matches limitedCopy: a stream exactly at the cap is kept.
			return nil, fmt.Errorf("%s plugin output of %d bytes exceeds %d: %w", filterName, len(decoded), lim.MaxStreamBytes, ErrDecompressionBomb)
		default:
			return decoded, nil
		}
	}

	return applyDecodeFilter(ctx, data, filterName, parms, dict, lim)
}

// marshalParms converts an internal ast.PdfDict to a public map[string]any
// suitable for the plugin interface. Stream-valued entries (like JBIG2Globals)
// are pre-decoded into []byte, by the built-in decoders under d's budgets.
func (d *Decoder) marshalParms(ctx context.Context, filterName ast.PdfName, dict ast.PdfDict) (map[string]any, error) {
	if dict == nil {
		return nil, nil
	}

	out := make(map[string]any, len(dict))
	for k, v := range dict {
		key := string(k)

		// Pre-decode stream-valued parameters (e.g., JBIG2Globals).
		if filterName == "JBIG2Decode" && key == "JBIG2Globals" {
			if stream, ok := v.(ast.PdfStream); ok {
				decoded, err := d.builtin().DecodeStream(ctx, stream)
				if err != nil {
					return nil, fmt.Errorf("decoding JBIG2Globals: %w", err)
				}
				out[key] = decoded
				continue
			}
		}

		switch val := v.(type) {
		case ast.PdfInt:
			out[key] = intconv.Clamp(val)
		case ast.PdfReal:
			out[key] = float64(val)
		case ast.PdfBool:
			out[key] = bool(val)
		case ast.PdfName:
			out[key] = string(val)
		case ast.PdfString:
			out[key] = string(val)
		default:
			out[key] = nil
		}
	}

	return out, nil
}
