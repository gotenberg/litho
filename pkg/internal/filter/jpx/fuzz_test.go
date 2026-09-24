package jpx

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// FuzzDecode feeds arbitrary data to the decoder, then renders every channel
// of what it decodes, as the JPXDecode filter does. The contract is
// crash-freedom: every input either decodes to components whose sample planes
// match their dimensions, and renders to one byte per channel per grid point,
// or returns an error carrying a sentinel.
func FuzzDecode(f *testing.F) {
	// Seeds reach both entry points and the tile-part loop: the J.10
	// codestream bare, wrapped in a JP2 file, and truncated inside its
	// tile-part. The OpenJPEG codestreams reach every progression order,
	// POC, PPM, PPT, SOP, EPH and the code-block styles.
	f.Add(annexJ10)
	f.Add(slices.Concat(signatureBox, fileTypeBox, boxOf(boxHeader, imageHeaderBox, greyBox), boxOf(boxCodestream, annexJ10)))
	f.Add(annexJ10[:90])
	names, err := filepath.Glob("testdata/*.j2k")
	if err != nil {
		f.Fatal(err)
	}
	for _, name := range names {
		data, err := os.ReadFile(name)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(data)
	}
	sentinel := func(t *testing.T, err error) {
		t.Helper()
		if !errors.Is(err, ErrMalformed) && !errors.Is(err, ErrUnsupported) && !errors.Is(err, ErrLimit) {
			t.Fatalf("error without a sentinel: %v", err)
		}
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		img, err := Decode(context.Background(), data)
		if err != nil {
			sentinel(t, err)
			return
		}
		for i, c := range img.Components {
			if len(c.Samples) != c.Width*c.Height {
				t.Fatalf("component %d holds %d samples for %dx%d", i, len(c.Samples), c.Width, c.Height)
			}
		}
		chs, err := img.Channels()
		if err != nil {
			sentinel(t, err)
			return
		}
		out, err := img.Render(context.Background(), chs, func(v int32, _ int, _ bool) byte { return byte(v) })
		if err != nil {
			sentinel(t, err)
			return
		}
		if w, h := img.DefaultSize(); len(out) != w*h*len(chs) {
			t.Fatalf("rendered %d bytes for %dx%d grid points of %d channels", len(out), w, h, len(chs))
		}
	})
}
