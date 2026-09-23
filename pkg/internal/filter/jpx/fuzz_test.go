package jpx

import (
	"context"
	"errors"
	"slices"
	"testing"
)

// FuzzDecode feeds arbitrary data to the decoder. The contract is
// crash-freedom: every input either decodes to components whose sample
// planes match their dimensions or returns an error.
func FuzzDecode(f *testing.F) {
	// Seeds reach both entry points and the tile-part loop: the J.10
	// codestream bare, wrapped in a JP2 file, and truncated inside its
	// tile-part.
	f.Add(annexJ10)
	f.Add(slices.Concat(signatureBox, fileTypeBox, boxOf(boxHeader, imageHeaderBox, greyBox), boxOf(boxCodestream, annexJ10)))
	f.Add(annexJ10[:90])
	f.Fuzz(func(t *testing.T, data []byte) {
		img, err := Decode(context.Background(), data)
		if err != nil {
			if !errors.Is(err, ErrMalformed) && !errors.Is(err, ErrUnsupported) && !errors.Is(err, ErrLimit) {
				t.Fatalf("error without a sentinel: %v", err)
			}
			return
		}
		for i, c := range img.Components {
			if len(c.Samples) != c.Width*c.Height {
				t.Fatalf("component %d holds %d samples for %dx%d", i, len(c.Samples), c.Width, c.Height)
			}
		}
	})
}
