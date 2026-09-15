package mutations

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// FuzzStampImage feeds arbitrary image bytes to the stamp image builders and
// asserts crash-freedom.
//
// The bytes are an image file the caller hands to the stamp API, and these
// functions turn them into an Image XObject that goes into the written
// document: the JPEG path walks the marker segments itself to read the frame
// header, and the PNG path decodes and re-encodes the samples. Like the font
// subsetter beside it, this is a WRITER-side reader of untrusted bytes, where a
// length trusted too far corrupts output rather than only a report.
//
// Measured against the fuzz corpora, every function in stamp_image.go sat at
// zero coverage.
func FuzzStampImage(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0xFF, 0xD8})             // SOI alone
	f.Add([]byte{0xFF, 0xD8, 0xFF, 0xC0}) // SOF marker, no segment
	f.Add(minimalJPEG())
	f.Add(minimalPNG())
	f.Add(minimalPNG()[:len(minimalPNG())/2])
	// A segment declaring zero length: the scanner advances by the declared
	// size, so a zero must not leave it standing still.
	f.Add([]byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x00, 0xFF, 0xD9})

	f.Fuzz(func(_ *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		_, _, _, _ = readJPEGDimensions(data)
		_, _, _, _ = buildJPEGXObject(data)
		_, _, _, _ = buildPNGXObject(writer.NewObjAllocator(0), data)
		_, _ = flateCompress(data)
	})
}

// minimalJPEG returns the smallest byte sequence the dimension scanner accepts:
// SOI, a baseline frame header declaring 1x1 greyscale, then EOI.
func minimalJPEG() []byte {
	return []byte{
		0xFF, 0xD8, // SOI
		0xFF, 0xC0, // SOF0
		0x00, 0x0B, // segment length: 11
		0x08,       // sample precision
		0x00, 0x01, // height
		0x00, 0x01, // width
		0x01,             // component count
		0x01, 0x11, 0x00, // component: id, sampling, quant table
		0xFF, 0xD9, // EOI
	}
}

// minimalPNG returns a 1x1 image with an alpha channel, so the PNG path builds
// both the colour stream and the /SMask rather than only the former.
func minimalPNG() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 1, G: 2, B: 3, A: 4})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic("encoding the seed PNG: " + err.Error())
	}
	return buf.Bytes()
}

// TestStampImageSeedsAreRead pins that both seeds reach the builders rather
// than being rejected at the signature, which is the only reason they are
// constructed rather than taken as random bytes.
func TestStampImageSeedsAreRead(t *testing.T) {
	t.Parallel()

	w, h, comps, err := readJPEGDimensions(minimalJPEG())
	if err != nil {
		t.Fatalf("readJPEGDimensions rejected the seed: %v", err)
	}
	if w != 1 || h != 1 || comps != 1 {
		t.Errorf("read %dx%d with %d components, want 1x1 with 1", w, h, comps)
	}

	_, pw, ph, err := buildPNGXObject(writer.NewObjAllocator(0), minimalPNG())
	if err != nil {
		t.Fatalf("buildPNGXObject rejected the seed: %v", err)
	}
	if pw != 1 || ph != 1 {
		t.Errorf("PNG built as %dx%d, want 1x1", pw, ph)
	}
}
