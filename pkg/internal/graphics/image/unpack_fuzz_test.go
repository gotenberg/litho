package image

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/limits"
)

// FuzzUnpackSamples feeds arbitrary packed sample data and a declared shape to
// the sub-byte sample unpacker.
//
// CONTRIBUTING requires a fuzz target beside every image decoder whose leading
// argument is attacker-controlled, and this one had none: measured per
// function, unpackSamples reads 100% from the unit tests and 0.0% from
// FuzzDecodeIndexed, which never reaches it. The unit tests fix the shape and
// vary one thing at a time, while the hazard here lives in the RELATIONSHIP
// between len(data), width, height, samplesPerPixel and bpc, guarded by a
// single `len(data) < rowBytes*height` test before five slices of data.
//
// bpc is deliberately NOT filtered. [statedBitsPerComponent] returns whatever
// the image dictionary states and substitutes 8 only when the value is absent
// or the image is JPX, so an arbitrary depth genuinely reaches here and the
// switch's default arm is the guard under test.
func FuzzUnpackSamples(f *testing.F) {
	for _, seed := range []struct {
		data []byte
		w, h int
		spp  int
		bpc  int
	}{
		{[]byte{0x00, 0xFF, 0x80, 0x40}, 2, 2, 1, 8},  // passthrough
		{[]byte{0b10110000}, 3, 1, 1, 1},              // sub-byte, row padding
		{[]byte{0b11011000, 0b01000000}, 2, 2, 1, 2},  // two rows, padded
		{[]byte{0x0F, 0xF0}, 2, 1, 1, 4},              // one row, no padding
		{[]byte{0x12, 0x34, 0x56, 0x78}, 2, 1, 1, 16}, // high byte only
		{[]byte{0xFF, 0xFF, 0xFF}, 1, 1, 3, 8},        // three components
		{[]byte{0xFF}, 8, 2, 1, 1},                    // too short for 2 rows
		{[]byte{0xFF, 0xFF}, 2, 1, 1, 3},              // unsupported depth
		{[]byte{}, 1, 1, 1, 4},                        // empty
	} {
		f.Add(seed.data, seed.w, seed.h, seed.spp, seed.bpc, true)
	}

	f.Fuzz(func(t *testing.T, data []byte, w, h, spp, bpc int, scale bool) {
		// Mirror the caller's guarantees rather than fuzzing past them.
		// DecodeImage runs validateImageDimensions well before the unpack,
		// so a non-positive or over-cap dimension cannot arrive here, and a
		// panic manufactured by one would be a by-design-unreachable finding.
		if validateImageDimensions(w, h, limits.DefaultMaxImagePixels) != nil {
			return
		}
		// samplesPerPixel comes from imageComponents, which returns 1, 3 or 4
		// for the three device spaces and 0 for anything else (which the caller
		// rejects), or a literal 1 for an indexed image.
		if spp != 1 && spp != 3 && spp != 4 {
			return
		}
		// Keep the shape small. Every assertion below is per sample, so a
		// megapixel declaration turns one execution into millions of them and
		// starves the engine; the packing logic is identical at 8x8.
		if w > 64 || h > 64 {
			return
		}
		if len(data) > 8192 {
			return
		}

		out, err := unpackSamples(data, w, h, spp, bpc, scale)
		if err != nil {
			if out != nil {
				t.Fatalf("unpackSamples returned %d bytes alongside error %v", len(out), err)
			}
			return
		}

		if bpc == 8 {
			// Documented passthrough: the 8-bit case returns its input.
			if len(out) != len(data) {
				t.Fatalf("bpc 8 returned %d bytes for %d in", len(out), len(data))
			}
			return
		}

		// Every accepted depth expands to exactly one byte per sample.
		if want := w * spp * h; len(out) != want {
			t.Fatalf("unpackSamples(%d bytes, %dx%d, spp %d, bpc %d) = %d bytes, want %d",
				len(data), w, h, spp, bpc, len(out), want)
		}

		// A row must never be read past the data the guard accepted.
		perRow := w * spp
		var rowBytes int
		switch bpc {
		case 1, 2, 4:
			rowBytes = (perRow*bpc + 7) / 8
		case 16:
			rowBytes = perRow * 2
		}
		if len(data) < rowBytes*h {
			t.Fatalf("accepted %d bytes for %d rows of %d", len(data), h, rowBytes)
		}

		// Bit replication is exact for 1, 2 and 4 bits: a scaled sample is the
		// raw value times 255/maxVal, so every output byte is a multiple of
		// that factor and the brightest sample lands on exactly 255. A stride
		// or shift error breaks this long before it breaks the length.
		if scale && (bpc == 1 || bpc == 2 || bpc == 4) {
			mul := byte(255) / byte(1<<bpc-1)
			for i, v := range out {
				if v%mul != 0 {
					t.Fatalf("sample %d = %d is not a multiple of %d at bpc %d", i, v, mul, bpc)
				}
			}
		}
	})
}
