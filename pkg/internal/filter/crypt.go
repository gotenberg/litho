package filter

import (
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// decodeCrypt implements the CryptFilter (PDF spec 7.4.10).
// Currently only the /Identity sub-filter (no-op passthrough) is supported.
func decodeCrypt(data []byte, parms ast.PdfDict) ([]byte, error) {
	name := "Identity"
	if parms != nil {
		if v, ok := parms["Name"]; ok {
			if n, ok := v.(ast.PdfName); ok {
				name = string(n)
			}
		}
	}

	if name == "Identity" {
		return data, nil
	}

	return nil, fmt.Errorf("%w: CryptFilter sub-filter %q not supported", ErrUnsupportedFilter, name)
}
