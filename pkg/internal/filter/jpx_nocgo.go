//go:build !cgo

package filter

import "fmt"

// decodeJPX is a stub for non-CGO builds. JPEG 2000 decoding requires
// CGO and the libopenjp2 library.
func decodeJPX(data []byte) ([]byte, error) {
	return nil, fmt.Errorf("%w: JPXDecode requires CGO and libopenjp2", ErrUnsupportedFilter)
}
