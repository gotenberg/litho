package jbig2

import "fmt"

// Decoder decodes JBIG2 streams via libjbig2dec.
// In non-CGO builds, Decode returns an error.
type Decoder struct{}

// New returns a new JBIG2 filter decoder plugin.
func New() *Decoder { return &Decoder{} }

// FilterName returns the canonical PDF filter name.
func (d *Decoder) FilterName() string { return "JBIG2Decode" }

// Decode decompresses JBIG2 data. The parms map may contain a
// "JBIG2Globals" key with pre-decoded global segment bytes.
func (d *Decoder) Decode(data []byte, parms map[string]any) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("jbig2 decode: empty input")
	}

	var globals []byte
	if parms != nil {
		if g, ok := parms["JBIG2Globals"]; ok {
			if gb, ok := g.([]byte); ok {
				globals = gb
			}
		}
	}

	return decode(data, globals)
}
