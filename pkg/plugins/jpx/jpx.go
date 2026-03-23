package jpx

import "fmt"

// Decoder decodes JPEG 2000 (JPX) streams via libopenjp2.
// In non-CGO builds, Decode returns an error.
type Decoder struct{}

// New returns a new JPX filter decoder plugin.
func New() *Decoder { return &Decoder{} }

// FilterName returns the canonical PDF filter name.
func (d *Decoder) FilterName() string { return "JPXDecode" }

// Decode decompresses JPEG 2000 data and returns raw interleaved pixel bytes.
func (d *Decoder) Decode(data []byte, _ map[string]any) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("jpx decode: empty input")
	}
	return decode(data)
}
