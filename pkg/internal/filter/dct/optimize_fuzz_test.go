package dct

import (
	"bytes"
	"context"
	"os"
	"testing"
)

// FuzzOptimizeHuffman feeds arbitrary data to the re-coder. It never panics,
// and what it re-codes is smaller and decodes to the samples the input
// decodes to.
func FuzzOptimizeHuffman(f *testing.F) {
	for _, name := range []string{"restart", "cmyk_adobe", "progressive"} {
		data, err := os.ReadFile("testdata/" + name + ".jpg")
		if err != nil {
			f.Fatal(err)
		}
		f.Add(data)
		f.Add(data[:len(data)/2])
	}
	overflow := overflowJPEG()
	f.Add(overflow)
	f.Add(overflow[:len(overflow)/2])
	f.Fuzz(func(t *testing.T, data []byte) {
		ctx := context.Background()
		out, ok, err := OptimizeHuffman(ctx, data)
		if err != nil {
			t.Fatalf("error without a cancelled context: %v", err)
		}
		if !ok {
			if out != nil {
				t.Fatal("bytes returned with ok false")
			}
			return
		}
		if len(out) >= len(data) {
			t.Fatalf("re-coded to %d bytes from %d", len(out), len(data))
		}
		want, _, _, _, wantErr := Decode(ctx, data, -1, 0, fuzzMaxSamples)
		got, _, _, _, gotErr := Decode(ctx, out, -1, 0, fuzzMaxSamples)
		if (wantErr == nil) != (gotErr == nil) || !bytes.Equal(want, got) {
			t.Fatalf("the re-coding decodes otherwise: %v, %v", wantErr, gotErr)
		}
	})
}
