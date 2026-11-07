package woff

import (
	"encoding/binary"
	"os"
	"testing"
)

// fuzzSeeds adds the testdata file name and its truncation to f.
func fuzzSeeds(f *testing.F, names ...string) {
	f.Helper()
	for _, name := range names {
		data, err := os.ReadFile("testdata/" + name)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(data)
		f.Add(data[:len(data)/2])
	}
}

// fixLength writes the length of data into its header's length field, so
// mutations reach past the length check.
func fixLength(data []byte) {
	if len(data) >= 12 {
		binary.BigEndian.PutUint32(data[8:], uint32(len(data))) //nolint:gosec // fuzz inputs are far below 4 GiB
	}
}

// FuzzDecodeWOFF checks that the WOFF 1.0 decoder never panics, and that a
// program it returns is an sfnt within the size cap.
func FuzzDecodeWOFF(f *testing.F) {
	fuzzSeeds(f, "Ahem.woff")
	f.Fuzz(func(t *testing.T, data []byte) {
		fixLength(data)
		out, err := decodeWOFF(data)
		if err == nil && (len(out) < 12 || len(out) > MaxSfntBytes) {
			t.Fatalf("a program of %d bytes", len(out))
		}
	})
}

// FuzzDecodeWOFF2 checks that the WOFF 2.0 decoder never panics, and that a
// program it returns is an sfnt within the size cap.
func FuzzDecodeWOFF2(f *testing.F) {
	fuzzSeeds(f, "Ahem.woff2", "Ahem-hmtx.woff2")
	f.Fuzz(func(t *testing.T, data []byte) {
		fixLength(data)
		out, err := decodeWOFF2(data)
		if err == nil && (len(out) < 12 || len(out) > MaxSfntBytes) {
			t.Fatalf("a program of %d bytes", len(out))
		}
	})
}

// FuzzReconstructGlyf checks that rebuilding a transformed glyf table never
// panics, the decompressed bytes reaching it directly.
func FuzzReconstructGlyf(f *testing.F) {
	f.Add(transformedGlyf(1, 0, [7][]byte{{0, 1}, {1}, {0}, {0, 0}, nil, {0, 0, 0, 0}, nil}, 0, nil))
	f.Add(transformedGlyf(1, 1, [7][]byte{{0xFF, 0xFF}, nil, nil, nil, {0, 0x21, 0, 0, 0, 0, 0, 0}, {0x80, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, nil}, 1, []byte{0, 0, 0, 0}))
	f.Fuzz(func(t *testing.T, data []byte) {
		res, err := reconstructGlyf(data)
		if err == nil && len(res.xMin) != res.numGlyphs {
			t.Fatalf("%d side bearings for %d glyphs", len(res.xMin), res.numGlyphs)
		}
	})
}
