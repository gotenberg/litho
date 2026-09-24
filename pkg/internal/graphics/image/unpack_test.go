package image

import (
	"bytes"
	"testing"
)

func TestUnpackSamples_EightBitPassesThroughUntouched(t *testing.T) {
	// 8-bit data is already one sample per byte, and copying it would be waste
	// on the overwhelmingly common path.
	in := []byte{1, 2, 3, 4}
	out, err := unpackSamples(in, 2, 2, 1, 8, true)
	if err != nil {
		t.Fatalf("unpackSamples: %v", err)
	}
	if !bytes.Equal(out, in) {
		t.Errorf("out = %v, want %v unchanged", out, in)
	}
}

func TestUnpackSamples_RowPaddingIsSkipped(t *testing.T) {
	// The load-bearing case. Width 3 at 1 bpc fills 3 bits of a byte and pads
	// the remaining 5, so each row starts on a fresh byte (ISO 32000-1, 8.9.3).
	// An unpack that walked the buffer bit by bit would read row 0's padding as
	// row 1's first samples and shift every row after the first.
	//
	// Row 0: 101xxxxx  Row 1: 010xxxxx
	data := []byte{0xA0, 0x40}

	out, err := unpackSamples(data, 3, 2, 1, 1, true)
	if err != nil {
		t.Fatalf("unpackSamples: %v", err)
	}
	want := []byte{255, 0, 255, 0, 255, 0}
	if !bytes.Equal(out, want) {
		t.Errorf("out = %v, want %v: the padding bits were read as samples", out, want)
	}
}

func TestUnpackSamples_IndexedSamplesAreNotScaled(t *testing.T) {
	// The other load-bearing case. A palette index is looked up directly, so
	// stretching it to the full byte range would address a different colour.
	// 0x2F is indices 2 and 15 at 4 bpc; scaled they would be 34 and 255.
	data := []byte{0x2F}

	out, err := unpackSamples(data, 2, 1, 1, 4, false)
	if err != nil {
		t.Fatalf("unpackSamples: %v", err)
	}
	want := []byte{2, 15}
	if !bytes.Equal(out, want) {
		t.Errorf("out = %v, want %v: an index must survive unscaled", out, want)
	}
}

func TestUnpackSamples_ColourSamplesReachFullRange(t *testing.T) {
	// The same bytes as an indexed image, read as colour instead: the brightest
	// sample has to land on exactly 255, not 240, or a white pixel comes out
	// grey. Bit replication is what makes 15 map to 255.
	data := []byte{0x2F}

	out, err := unpackSamples(data, 2, 1, 1, 4, true)
	if err != nil {
		t.Fatalf("unpackSamples: %v", err)
	}
	want := []byte{2 * 17, 255}
	if !bytes.Equal(out, want) {
		t.Errorf("out = %v, want %v", out, want)
	}
}

func TestUnpackSamples_MultiComponentRowStride(t *testing.T) {
	// Three components at 4 bpc is 12 bits per pixel, so a 2-pixel row is 3
	// bytes with no padding. Getting samplesPerPixel wrong here is exactly the
	// indexed-vs-base-space trap, so the stride is asserted directly.
	// Pixels: (1,2,3) and (4,5,6).
	data := []byte{0x12, 0x34, 0x56}

	out, err := unpackSamples(data, 2, 1, 3, 4, false)
	if err != nil {
		t.Fatalf("unpackSamples: %v", err)
	}
	want := []byte{1, 2, 3, 4, 5, 6}
	if !bytes.Equal(out, want) {
		t.Errorf("out = %v, want %v", out, want)
	}
}

func TestUnpackSamples_SixteenBitTakesTheHighByte(t *testing.T) {
	data := []byte{0x12, 0x34, 0xAB, 0xCD}

	out, err := unpackSamples(data, 2, 1, 1, 16, true)
	if err != nil {
		t.Fatalf("unpackSamples: %v", err)
	}
	want := []byte{0x12, 0xAB}
	if !bytes.Equal(out, want) {
		t.Errorf("out = %v, want %v", out, want)
	}
}

// TestUnpackSamples_RejectsBitsPerComponentThree pins that a depth of 3 is
// refused.
//
// # Corpus evidence
//
//   - All corpora: one image declares /BitsPerComponent 3.
func TestUnpackSamples_RejectsBitsPerComponentThree(t *testing.T) {
	// Not a hypothetical: real files declare 3, which ISO 32000-1, 8.9.5.1
	// Table 89 does not permit and which has no defined packing. It has to be
	// refused rather than strided as though it were 4.
	if _, err := unpackSamples([]byte{0xFF, 0xFF}, 2, 1, 1, 3, true); err == nil {
		t.Errorf("BitsPerComponent 3 was accepted, but it has no defined packing")
	}
}

func TestUnpackSamples_ShortDataIsRefused(t *testing.T) {
	// One byte holds one 8-pixel row at 1 bpc, so two rows need two bytes.
	// Reading past the end would slice out of range.
	if _, err := unpackSamples([]byte{0xFF}, 8, 2, 1, 1, true); err == nil {
		t.Errorf("short data was accepted, so a later read would run past the buffer")
	}
}
