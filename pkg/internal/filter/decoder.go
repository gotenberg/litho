package filter

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/plugins"
)

// Decoder holds registered filter plugins and provides plugin-aware stream
// decoding. A nil *Decoder is valid and behaves identically to the
// package-level functions (no plugins consulted).
type Decoder struct {
	plugins map[ast.PdfName]plugins.FilterDecoder
}

// NewDecoder creates a Decoder with the given filter plugins. Duplicate
// filter names are resolved in favor of the last plugin registered.
// Returns nil if no plugins are provided.
func NewDecoder(pp []plugins.FilterDecoder) *Decoder {
	if len(pp) == 0 {
		return nil
	}
	m := make(map[ast.PdfName]plugins.FilterDecoder, len(pp))
	for _, p := range pp {
		m[ast.PdfName(p.FilterName())] = p
	}
	return &Decoder{plugins: m}
}

// Handles reports whether a plugin is registered for the named filter.
// A nil *Decoder handles nothing.
func (d *Decoder) Handles(name ast.PdfName) bool {
	if d == nil {
		return false
	}
	_, ok := d.plugins[name]
	return ok
}

// DecodeStream decodes a PDF stream, consulting registered plugins for
// filter names they handle before falling back to built-in decoders.
func (d *Decoder) DecodeStream(ctx context.Context, stream ast.PdfStream) ([]byte, error) {
	if d == nil {
		return DecodeStream(ctx, stream)
	}

	filterObj, hasFilter := stream.Dict["Filter"]
	if !hasFilter {
		return stream.Bytes(ctx)
	}

	var filters []ast.PdfName
	switch f := filterObj.(type) {
	case ast.PdfName:
		filters = append(filters, f)
	case ast.PdfArray:
		for _, item := range f {
			if name, ok := item.(ast.PdfName); ok {
				filters = append(filters, name)
			}
		}
	default:
		return nil, fmt.Errorf("invalid type for /Filter: %T", filterObj)
	}

	parms := extractDecodeParms(stream.Dict, len(filters))

	data, err := stream.Bytes(ctx)
	if err != nil {
		return nil, err
	}
	for i, f := range filters {
		decoded, err := d.applyDecodeFilter(ctx, data, f, parms[i], stream.Dict)
		if err != nil {
			return nil, err
		}
		data = decoded
	}

	return data, nil
}

// NewStreamReader returns an io.ReadCloser that lazily decodes a PDF stream,
// consulting registered plugins for filter names they handle.
func (d *Decoder) NewStreamReader(ctx context.Context, stream ast.PdfStream) (io.ReadCloser, error) {
	if d == nil {
		return NewStreamReader(ctx, stream)
	}

	// Check if any filter in the chain requires a plugin. If not, delegate
	// to the package-level function which has streaming fast paths.
	filterObj, hasFilter := stream.Dict["Filter"]
	if !hasFilter {
		return NewStreamReader(ctx, stream)
	}

	needsPlugin := false
	switch f := filterObj.(type) {
	case ast.PdfName:
		_, needsPlugin = d.plugins[f]
	case ast.PdfArray:
		for _, item := range f {
			if name, ok := item.(ast.PdfName); ok {
				if _, ok := d.plugins[name]; ok {
					needsPlugin = true
					break
				}
			}
		}
	}

	if !needsPlugin {
		return NewStreamReader(ctx, stream)
	}

	// Plugin path: eager decode, then wrap in a reader.
	data, err := d.DecodeStream(ctx, stream)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

// applyDecodeFilter applies a single filter, checking plugins first.
//
// A plugin that returns [plugins.ErrUnsupportedFilter] declines the stream,
// and the built-in decoder for the filter runs instead. When there is none,
// the stream fails with [ErrUnsupportedFilter], exactly as
// [Decoder.DecodeStream] fails on a filter no plugin claims.
//
// A plugin returns its whole output in one slice, so [limitedCopy] never sees
// it and maxDecompressedSize bounded every built-in filter and no plugin at
// all. The filter a document names decided whether the bomb guard applied: a
// blank 17000x17000 JPEG 2000 image encodes to 1,204 bytes and decodes to 289
// MB, past the same cap that refuses a FlateDecode stream of that size.
//
// The bound here cannot prevent the plugin's own allocation, which has already
// happened by the time it returns. It stops those bytes propagating and keeps
// litho from holding a second copy, and it holds for third-party plugins that
// know nothing of this package's limits.
func (d *Decoder) applyDecodeFilter(ctx context.Context, data []byte, filterName ast.PdfName, parms, dict ast.PdfDict) ([]byte, error) {
	if p, ok := d.plugins[filterName]; ok {
		pubParms, err := d.marshalParms(ctx, filterName, parms)
		if err != nil {
			return nil, err
		}
		decoded, err := p.Decode(data, pubParms)
		switch {
		case errors.Is(err, plugins.ErrUnsupportedFilter):
			// Declined: fall through to the built-in decoder.
		case err != nil:
			return nil, err
		case len(decoded) > maxDecompressedSize:
			// Matches limitedCopy: a stream exactly at the cap is kept.
			return nil, fmt.Errorf("%s: %w", filterName, ErrDecompressionBomb)
		default:
			return decoded, nil
		}
	}

	return applyDecodeFilter(ctx, data, filterName, parms, dict)
}

// marshalParms converts an internal ast.PdfDict to a public map[string]any
// suitable for the plugin interface. Stream-valued entries (like JBIG2Globals)
// are pre-decoded into []byte.
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
				decoded, err := DecodeStream(ctx, stream)
				if err != nil {
					return nil, fmt.Errorf("decoding JBIG2Globals: %w", err)
				}
				out[key] = decoded
				continue
			}
		}

		switch val := v.(type) {
		case ast.PdfInt:
			out[key] = int(val)
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
