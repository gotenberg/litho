package image

import "testing"

// FuzzDecodeIndexed feeds arbitrary sample data and colour tables to the
// indexed image decoder.
//
// Both come straight from the file: the samples are the stream's bytes and the
// table is whatever the /Indexed array's fourth element decoded to. Every index
// is a byte, so a table shorter than the indices used is reachable with any
// input, and reading past it would be an out-of-bounds read on attacker-chosen
// data. The decoder must never panic and must fill exactly the declared pixels.
func FuzzDecodeIndexed(f *testing.F) {
	for _, seed := range []struct {
		data   []byte
		lookup []byte
		comps  int
		w, h   int
	}{
		{[]byte{0, 1, 2, 3}, []byte{255, 0, 0, 0, 255, 0, 0, 0, 255, 255, 255, 255}, 3, 2, 2},
		{[]byte{0, 1}, []byte{0, 255}, 1, 2, 1},
		{[]byte{3, 2, 1, 0}, []byte{}, 4, 2, 2},
		{[]byte{}, []byte{1, 2, 3}, 3, 1, 1},
		{[]byte{255}, []byte{7}, 1, 1, 1},
	} {
		f.Add(seed.data, seed.lookup, seed.comps, seed.w, seed.h)
	}

	f.Fuzz(func(t *testing.T, data, lookup []byte, comps, w, h int) {
		// The caller validates the declared dimensions before reaching here
		// (validateImageDimensions), and the component count comes from a
		// device space, so mirror both bounds rather than fuzzing past them.
		if comps < 1 || comps > 4 {
			return
		}
		// Keep the pixel count small. The checks below are per-pixel, and a
		// megapixel image turns each execution into millions of them, which
		// starves the fuzzing engine of executions rather than finding
		// anything: the decode path is the same at 8x8 as at 512x512.
		if w < 0 || h < 0 || w > 64 || h > 64 {
			return
		}
		// Cap the byte inputs too. Neither is scanned, so a megabyte of them
		// only costs the engine a copy per execution and finds nothing the
		// short cases do not.
		if len(data) > 8192 || len(lookup) > 8192 {
			return
		}

		pal := palette{base: "DeviceRGB", comps: comps, lookup: lookup}
		switch comps {
		case 1:
			pal.base = "DeviceGray"
		case 4:
			pal.base = "DeviceCMYK"
		}

		img, err := decodeIndexed(data, w, h, pal)
		if err != nil {
			if img != nil {
				t.Fatalf("decodeIndexed returned an image alongside %v", err)
			}
			return
		}
		if len(data) < w*h {
			t.Fatalf("decodeIndexed accepted %d bytes for %dx%d pixels", len(data), w, h)
		}
		b := img.Bounds()
		if b.Dx() != w || b.Dy() != h {
			t.Fatalf("bounds %v for a %dx%d image", b, w, h)
		}
		// Every pixel must be opaque: a palette entry past the table reads as
		// black, never as transparent, which would silently erase content.
		for y := range h {
			for x := range w {
				if _, _, _, a := img.At(x, y).RGBA(); a>>8 != 255 {
					t.Fatalf("pixel (%d,%d) has alpha %d", x, y, a>>8)
				}
			}
		}
	})
}
