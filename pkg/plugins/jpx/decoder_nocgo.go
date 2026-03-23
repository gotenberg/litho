//go:build !cgo

package jpx

import (
	"fmt"

	"github.com/gotenberg/litho/pkg/plugins"
)

func decode(data []byte) ([]byte, error) {
	return nil, fmt.Errorf("%w: JPXDecode requires CGO and libopenjp2", plugins.ErrUnsupportedFilter)
}
