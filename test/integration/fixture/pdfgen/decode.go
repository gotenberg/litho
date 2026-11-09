package pdfgen

import (
	"bytes"
	"compress/zlib"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
)

// errUnsupportedFilter reports a filter qpdf decodes but pdfgen does not
// implement yet. Port it when a builder needs it.
var errUnsupportedFilter = errors.New("filter qpdf decodes but pdfgen does not")

// decodeAll decodes data through the dictionary's /Filter chain the way
// qpdf's pipeStreamData does at decode level all. ok is false when the chain
// holds a filter qpdf cannot decode either, which leaves the stream as is.
func decodeAll(d *Dict, data []byte) ([]byte, bool, error) {
	var filters []Name
	switch f := resolve(d.Get("Filter")).(type) {
	case nil, Null:
	case Name:
		filters = []Name{f}
	case *Array:
		for _, it := range f.Items {
			n, ok := resolve(it).(Name)
			if !ok {
				return nil, false, nil
			}
			filters = append(filters, n)
		}
	default:
		return nil, false, nil
	}
	if parms := resolve(d.Get("DecodeParms")); parms != nil && !isNull(parms) {
		if a, ok := parms.(*Array); !ok || len(a.Items) > 0 {
			return nil, false, fmt.Errorf("/DecodeParms: %w", errUnsupportedFilter)
		}
	}
	out := data
	for _, f := range filters {
		switch f {
		case "FlateDecode", "Fl":
			r, err := zlib.NewReader(bytes.NewReader(out))
			if err != nil {
				return nil, false, fmt.Errorf("flate: %w", err)
			}
			dec, err := io.ReadAll(r)
			if err != nil {
				return nil, false, fmt.Errorf("flate: %w", err)
			}
			out = dec
		case "ASCIIHexDecode", "AHx":
			var clean []byte
			for _, c := range out {
				if c == '>' {
					break
				}
				if c > ' ' {
					clean = append(clean, c)
				}
			}
			if len(clean)%2 == 1 {
				clean = append(clean, '0')
			}
			dec := make([]byte, len(clean)/2)
			if _, err := hex.Decode(dec, clean); err != nil {
				return nil, false, fmt.Errorf("asciihex: %w", err)
			}
			out = dec
		case "LZWDecode", "LZW", "ASCII85Decode", "A85", "RunLengthDecode", "RL", "DCTDecode", "DCT":
			return nil, false, fmt.Errorf("/%s: %w", f, errUnsupportedFilter)
		default:
			return nil, false, nil
		}
	}
	return out, true, nil
}
