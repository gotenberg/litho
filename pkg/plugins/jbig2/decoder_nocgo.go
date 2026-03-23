//go:build !cgo

package jbig2

import (
	"fmt"

	"github.com/gotenberg/litho/pkg/plugins"
)

func decode(data []byte, globals []byte) ([]byte, error) {
	return nil, fmt.Errorf("%w: JBIG2Decode requires CGO and libjbig2dec", plugins.ErrUnsupportedFilter)
}
