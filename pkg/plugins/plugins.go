package plugins

// FilterDecoder decodes one PDF stream filter: a filter litho does not know,
// or one litho decodes natively, whose built-in decoder it then replaces.
// Implementations are registered via
// [github.com/gotenberg/litho/pkg/pdf.WithFilterPlugin] and consulted during
// stream decompression before any built-in decoder for the filter names they
// handle. Decode returns [ErrUnsupportedFilter] to decline a stream and leave
// it to the built-in decoder.
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
	//
	// An error that matches [ErrUnsupportedFilter] under [errors.Is]
	// declines the stream, and litho decodes it with its built-in decoder
	// for the filter. Any other error fails the stream.
	Decode(data []byte, parms map[string]any) ([]byte, error)
}
