package jbig2

import (
	"bytes"
	"context"
	"fmt"
	"testing"
)

// TestMQEncoder_T88AnnexH2 pins the test-only encoder against the test
// sequence of ITU-T T.88, Annex H.2, so the round trip below starts from an
// encoder the standard itself vouches for.
func TestMQEncoder_T88AnnexH2(t *testing.T) {
	t.Parallel()

	decisions := []byte{
		0x00, 0x02, 0x00, 0x51, 0x00, 0x00, 0x00, 0xC0, 0x03, 0x52, 0x87, 0x2A, 0xAA, 0xAA, 0xAA, 0xAA,
		0x82, 0xC0, 0x20, 0x00, 0xFC, 0xD7, 0x9E, 0xF6, 0xBF, 0x7F, 0xED, 0x90, 0x4F, 0x46, 0xA3, 0xBF,
	}
	want := []byte{
		0x84, 0xC7, 0x3B, 0xFC, 0xE1, 0xA1, 0x43, 0x04, 0x02, 0x20, 0x00, 0x00, 0x41, 0x0D, 0xBB, 0x86,
		0xF4, 0x31, 0x7F, 0xFF, 0x88, 0xFF, 0x37, 0x47, 0x1A, 0xDB, 0x6A, 0xDF, 0xFF, 0xAC,
	}
	e := newMQEncoder()
	var cx encContext
	for i := range len(decisions) * 8 {
		e.encode(&cx, uint32(decisions[i/8]>>(7-i%8))&1)
	}
	if got := e.flush(); !bytes.Equal(got, want) {
		t.Errorf("encoded\n  % X\nwant\n  % X", got, want)
	}
}

// TestDecode_GenericTemplatesRoundTrip decodes generic regions coded by the
// test-only encoder with every template, with and without TPGD, and with the
// AT pixels at their nominal and at moved locations.
//
// The decoder's own Annex H.1 golden cannot stand in for this: its region
// decodes identically with a wrong template 0 geometry and with a wrong SLTP
// context, so it pins neither. These streams were also decoded by jbig2dec
// 0.20 when this test was written, and it reproduced every source image, so
// they are valid JBIG2 and not merely what this package expects.
func TestDecode_GenericTemplatesRoundTrip(t *testing.T) {
	t.Parallel()

	moved := [4][][2]int{
		{{4, -2}, {-5, -1}, {1, -3}, {-6, -3}},
		{{-5, -2}},
		{{5, -1}},
		{{-1, -3}},
	}
	for template := range 4 {
		for _, tpgdon := range []bool{false, true} {
			for _, at := range [][][2]int{encNominalAT[template], moved[template]} {
				name := fmt.Sprintf("template %d, TPGDON %v, AT %v", template, tpgdon, at)
				t.Run(name, func(t *testing.T) {
					t.Parallel()

					src := testImage(97, 71, uint64(template+1))
					stream := genericStream(src.Width, src.Height, template, tpgdon, at, encodeGeneric(src, template, tpgdon, at))
					got, err := Decode(context.Background(), stream, nil, DefaultMaxBitmapPixels)
					if err != nil {
						t.Fatalf("Decode: %v", err)
					}
					if !bytes.Equal(got.Data, src.Data) {
						t.Errorf("decoded image differs from the source in %d bytes", differingBytes(got.Data, src.Data))
					}
				})
			}
		}

		t.Run(fmt.Sprintf("template %d, SLTP neighbourhood tiled", template), func(t *testing.T) {
			t.Parallel()

			src := sltpImage(template)
			at := encNominalAT[template]
			stream := genericStream(src.Width, src.Height, template, true, at, encodeGeneric(src, template, true, at))
			got, err := Decode(context.Background(), stream, nil, DefaultMaxBitmapPixels)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if !bytes.Equal(got.Data, src.Data) {
				t.Errorf("decoded image differs from the source in %d bytes", differingBytes(got.Data, src.Data))
			}
		})
	}
}
