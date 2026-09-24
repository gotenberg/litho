package plugins

// FilterDecoder decodes a PDF stream filter that litho does not support
// natively. Implementations are registered via
// [github.com/gotenberg/litho/pkg/pdf.WithFilterPlugin] and consulted during
// stream decompression for filter names they handle.
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
