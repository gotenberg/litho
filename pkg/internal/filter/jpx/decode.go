package jpx

import (
	"context"
	"fmt"
)

// Image is a decoded JPEG 2000 image: one sample plane per codestream
// component.
type Image struct {
	Components []Component
}

// Component is one decoded component (T.800, B.2): Width x Height samples in
// row-major order, each Precision bits deep and signed when Signed is set.
type Component struct {
	Width, Height int
	Precision     int
	Signed        bool
	Samples       []int32
}

// Decode decodes a JP2 file, or a bare codestream, and returns its image.
//
// Decode is safe for concurrent use: it keeps no state between calls.
func Decode(ctx context.Context, data []byte) (*Image, error) {
	stream, _, err := readFile(data)
	if err != nil {
		return nil, err
	}
	cs, err := parseCodestream(stream)
	if err != nil {
		return nil, err
	}
	budget := &blockBudget{}
	for _, t := range cs.tiles {
		if t == nil {
			continue
		}
		d, err := cs.newTileDecoder(t, budget)
		if err != nil {
			return nil, err
		}
		if err := d.decodePackets(ctx, cs); err != nil {
			return nil, fmt.Errorf("tile %d: %w", t.index, err)
		}
	}
	return nil, fmt.Errorf("%w: code-block decoding is not implemented", ErrUnsupported)
}
