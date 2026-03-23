//go:build !cgo

package filter

import (
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// decodeJBIG2 is a stub for non-CGO builds. JBIG2 decoding requires
// CGO and the libjbig2dec library.
func decodeJBIG2(data []byte, _ ast.PdfDict) ([]byte, error) {
	return nil, fmt.Errorf("%w: JBIG2Decode requires CGO and libjbig2dec", ErrUnsupportedFilter)
}
