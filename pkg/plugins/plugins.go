// Package plugins defines extension interfaces for the litho PDF reader.
//
// Filter plugins allow opt-in support for PDF stream filters that require
// external C libraries (e.g., JBIG2Decode via libjbig2dec, JPXDecode via
// libopenjp2). Without plugins, these filters return ErrUnsupportedFilter.
//
// Usage:
//
//	import (
//	    "github.com/gotenberg/litho/pkg/pdf"
//	    "github.com/gotenberg/litho/pkg/plugins/jbig2"
//	    "github.com/gotenberg/litho/pkg/plugins/jpx"
//	)
//
//	reader, err := pdf.OpenFile(ctx, "doc.pdf",
//	    pdf.WithFilterPlugin(jbig2.New()),
//	    pdf.WithFilterPlugin(jpx.New()),
//	)
package plugins

import "errors"

// ErrUnsupportedFilter indicates that a filter plugin cannot decode the
// stream, either because CGO is not available or the data is invalid.
var ErrUnsupportedFilter = errors.New("unsupported filter plugin")

// FilterDecoder decodes a PDF stream filter that litho does not support
// natively. Implementations are registered via [pdf.WithFilterPlugin] and
// consulted during stream decompression for filter names they handle.
type FilterDecoder interface {
	// FilterName returns the canonical PDF filter name this decoder handles
	// (e.g., "JBIG2Decode", "JPXDecode"). The name must not include a
	// leading slash.
	FilterName() string

	// Decode decompresses raw stream data. The parms map carries the
	// /DecodeParms entries as simple Go types (no internal AST types):
	//   - string keys (the PDF name without leading "/")
	//   - int, float64, bool, string, or []byte values
	//
	// For JBIG2Decode, the "JBIG2Globals" key holds a []byte value
	// containing the already-decoded global segment data. The filter
	// infrastructure handles the recursive decoding of the JBIG2Globals
	// stream before calling Decode.
	//
	// An empty or nil parms map means no DecodeParms were present.
	Decode(data []byte, parms map[string]any) ([]byte, error)
}
