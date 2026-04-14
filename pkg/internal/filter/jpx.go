package filter

import "fmt"

// decodeJPX is the built-in fallback for JPEG 2000 decoding. It returns
// ErrUnsupportedFilter. To decode JPX streams, register the jpx plugin
// via pdf.WithFilterPlugin.
func decodeJPX(_data []byte) ([]byte, error) {
	return nil, fmt.Errorf("%w: JPXDecode (register the jpx plugin)", ErrUnsupportedFilter)
}
