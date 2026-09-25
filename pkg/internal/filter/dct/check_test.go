package dct

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"os"
	"slices"
	"testing"
)

// TestCheck_AgreesWithDecode pins what [Check] reads from the headers of the
// test JPEGs: each decodes, so Check settles nothing to refuse; the same data
// with its frame turned arithmetic (SOF9) or 12 bits deep is refused by both;
// and a second frame after the first scan leaves Check unsettled, while
// Decode refuses it.
func TestCheck_AgreesWithDecode(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	for _, name := range []string{"progressive", "restart", "cmyk_adobe"} {
		data, err := os.ReadFile("testdata/" + name + ".jpg")
		if err != nil {
			t.Fatal(err)
		}
		sof := -1
		for p := 2; p+3 < len(data); p += 2 + int(binary.BigEndian.Uint16(data[p+2:])) {
			if isFrameMarker(data[p+1]) {
				sof = p
				break
			}
		}
		if sof < 0 {
			t.Fatalf("%s: no frame marker", name)
		}
		frame := data[sof : sof+2+int(binary.BigEndian.Uint16(data[sof+2:]))]
		arithmetic := bytes.Clone(data)
		arithmetic[sof+1] = 0xC9
		deep := bytes.Clone(data)
		deep[sof+4] = 12
		secondFrame := slices.Concat(data[:len(data)-2], frame, data[len(data)-2:])

		for _, tc := range []struct {
			name        string
			data        []byte
			settled     bool
			unsupported bool
		}{
			{"as encoded", data, true, false},
			{"arithmetic", arithmetic, true, true},
			{"12-bit", deep, true, true},
			{"a second frame after the scan", secondFrame, false, false},
		} {
			settled, err := Check(ctx, tc.data, 0, DefaultMaxSamples)
			if settled != tc.settled || errors.Is(err, ErrUnsupported) != tc.unsupported || (!tc.unsupported && err != nil) {
				t.Errorf("%s, %s: Check = %v, %v; want settled %v, unsupported %v", name, tc.name, settled, err, tc.settled, tc.unsupported)
			}
			_, _, _, _, derr := Decode(ctx, tc.data, -1, 0, DefaultMaxSamples)
			if wantRefused := tc.unsupported || !tc.settled; errors.Is(derr, ErrUnsupported) != wantRefused {
				t.Errorf("%s, %s: Decode error = %v, want refused %v", name, tc.name, derr, wantRefused)
			}
		}
	}
}

// TestCheck_Malformed pins that Check fails, as Decode does, on data with no
// JPEG header and on headers that end before any scan.
func TestCheck_Malformed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	for name, data := range map[string][]byte{
		"not a JPEG":   []byte("GIF89a"),
		"no scan":      {0xFF, 0xD8, 0xFF, 0xD9},
		"scan first":   {0xFF, 0xD8, 0xFF, 0xDA, 0x00, 0x08, 0x01, 0x01, 0x00, 0x00, 0x3F, 0x00},
		"cut in a DQT": {0xFF, 0xD8, 0xFF, 0xDB, 0x00, 0x43, 0x00},
	} {
		_, err := Check(ctx, data, 0, DefaultMaxSamples)
		_, _, _, _, derr := Decode(ctx, data, -1, 0, DefaultMaxSamples)
		if !errors.Is(err, ErrMalformed) || !errors.Is(derr, ErrMalformed) {
			t.Errorf("%s: Check = %v, Decode = %v; want both %v", name, err, derr, ErrMalformed)
		}
	}
}
