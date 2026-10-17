package jpx

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// fuzzSmallSamples is the lowered sample cap [FuzzDecode] also decodes under.
const fuzzSmallSamples = 1 << 12

// FuzzDecode feeds arbitrary data to the decoder, then renders every channel
// of what it decodes, as the JPXDecode filter does. The contract is
// crash-freedom: every input either decodes to components whose sample planes
// match their dimensions, and renders to one byte per channel per grid point,
// or returns an error carrying a sentinel. [Image.Check] agrees with Render:
// whatever Render refuses as unsupported Check refuses too, or calls
// unsettled, and whatever Render renders Check passes.
func FuzzDecode(f *testing.F) {
	// Seeds reach both entry points and the tile-part loop: the J.10
	// codestream bare, wrapped in a JP2 file, and truncated inside its
	// tile-part. The OpenJPEG codestreams reach every progression order,
	// POC, PPM, PPT, SOP, EPH and the code-block styles. Two refusals follow:
	// a reversible sub-band of 37 magnitude bit-planes, from seven guard bits
	// and an exponent of 31 in the QCD, and code-blocks past maxPlanes, from
	// an RGN shift of 255 after the SIZ.
	f.Add(annexJ10)
	f.Add(slices.Concat(signatureBox, fileTypeBox, boxOf(BoxHeader, imageHeaderBox, greyBox), boxOf(boxCodestream, annexJ10)))
	f.Add(annexJ10[:90])
	wide := slices.Clone(annexJ10)
	wide[49], wide[50] = 0xE0, 0xF8
	f.Add(wide)
	f.Add(slices.Concat(annexJ10[:45], seg(markerRGN, 0, 0, 0xFF), annexJ10[45:]))
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
		ctx := context.Background()
		if img, err := Describe(data, DefaultMaxSamples); err == nil {
			if chs, err := img.Channels(); err == nil {
				settled, checkErr := img.Check(ctx, chs)
				_, renderErr := img.Render(ctx, chs, func(v int32, _ int, _ bool) byte { return byte(v) })
				if errors.Is(renderErr, ErrUnsupported) && settled && !errors.Is(checkErr, ErrUnsupported) {
					t.Fatalf("Render refuses the image (%v), Check does not: %v", renderErr, checkErr)
				}
				if renderErr == nil && checkErr != nil {
					t.Fatalf("Render renders the image, Check fails it: %v", checkErr)
				}
			}
		}
		// A small cap takes the refusals a lowered image budget moves: what
		// decodes under it holds no more samples than it allows.
		if small, err := Decode(context.Background(), data, fuzzSmallSamples); err != nil {
			sentinel(t, err)
		} else {
			var n int64
			for _, c := range small.Components {
				n += int64(len(c.Samples))
			}
			if n > fuzzSmallSamples {
				t.Fatalf("%d samples decoded under a %d-sample cap", n, int64(fuzzSmallSamples))
			}
		}
		img, err := Decode(context.Background(), data, DefaultMaxSamples)
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

// FuzzReadBoxes asserts the JP2 box walker never panics on crafted input, and
// that every box it returns lies inside the data.
func FuzzReadBoxes(f *testing.F) {
	f.Add([]byte("\x00\x00\x00\x0cjP  \r\n\x87\n")) // JP2 signature box.
	f.Add([]byte("\x00\x00"))                       // Truncated box header.
	f.Fuzz(func(t *testing.T, data []byte) {
		boxes, _ := ReadBoxes(data)
		for _, b := range boxes {
			if b.Offset < 0 || b.Offset+len(b.Body) > len(data) {
				t.Fatalf("box %q at %d with %d bytes overruns %d bytes", boxName(b.Type), b.Offset, len(b.Body), len(data))
			}
		}
	})
}

// FuzzReadColourSpec asserts the Colour Specification box reader never panics
// on crafted input.
func FuzzReadColourSpec(f *testing.F) {
	f.Add([]byte{0x01, 0x00, 0x01, 0x00, 0x00, 0x00, 0x10}) // METH 1, APPROX 1, EnumCS.
	f.Add([]byte{0x01})                                     // Truncated.
	f.Fuzz(func(_ *testing.T, data []byte) {
		_, _ = ReadColourSpec(data)
		_, _, _ = readColour(data)
	})
}

// FuzzReadSIZ asserts the SIZ marker segment reader never panics on crafted
// input. The seeds are a SIZ of one component and parameters cut before Csiz.
func FuzzReadSIZ(f *testing.F) {
	siz := make([]byte, sizFixedBytes+3)
	siz[35] = 0x01 // Csiz = 1 component.
	f.Add(siz)
	f.Add(siz[:4])
	f.Fuzz(func(_ *testing.T, data []byte) {
		_, _ = ReadSIZ(data)
		_, _ = ReadImageHeader(data)
	})
}
