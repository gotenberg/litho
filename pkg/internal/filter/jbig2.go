package filter

import (
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// decodeJBIG2 is the built-in fallback for JBIG2 decoding. It returns
// ErrUnsupportedFilter. To decode JBIG2 streams, register the jbig2 plugin
// via pdf.WithFilterPlugin.
func decodeJBIG2(_data []byte, _ ast.PdfDict) ([]byte, error) {
	return nil, fmt.Errorf("%w: JBIG2Decode (register the jbig2 plugin)", ErrUnsupportedFilter)
}
