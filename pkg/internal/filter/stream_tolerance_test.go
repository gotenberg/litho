package filter

import (
	"bytes"
	"compress/flate"
	"compress/zlib"
	"context"
	"io"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestNewStreamReader_FlateTolerance reads damaged FlateDecode data through
// the streaming path as DecodeStream reads it: a stream cut short yields what
// it decoded, one whose checksum is wrong yields all of it, and deflate data
// behind a damaged zlib header is read as raw deflate.
func TestNewStreamReader_FlateTolerance(t *testing.T) {
	t.Parallel()

	plain := bytes.Repeat([]byte("litho reads damaged streams "), 400)
	var z bytes.Buffer
	w := zlib.NewWriter(&z)
	if _, err := w.Write(plain); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	full := z.Bytes()
	badSum := bytes.Clone(full)
	badSum[len(badSum)-1] ^= 0xFF
	var raw bytes.Buffer
	fw, err := flate.NewWriter(&raw, flate.DefaultCompression)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(plain); err != nil {
		t.Fatal(err)
	}
	if err := fw.Close(); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name string
		data []byte
		full bool
	}{
		{"truncated", full[:len(full)/2], false},
		{"bad checksum", badSum, true},
		{"raw deflate", raw.Bytes(), true},
	} {
		stream := ast.NewStream(ast.PdfDict{"Filter": ast.PdfName("FlateDecode")}, tc.data)
		want, err := DecodeStream(context.Background(), stream)
		if err != nil {
			t.Fatalf("%s: DecodeStream: %v", tc.name, err)
		}
		rc, err := NewStreamReader(context.Background(), stream)
		if err != nil {
			t.Fatalf("%s: NewStreamReader: %v", tc.name, err)
		}
		got, err := io.ReadAll(rc)
		if err != nil {
			t.Errorf("%s: read: %v", tc.name, err)
		}
		if err := rc.Close(); err != nil {
			t.Errorf("%s: close: %v", tc.name, err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s: streamed %d bytes, DecodeStream %d", tc.name, len(got), len(want))
		}
		if tc.full != bytes.Equal(got, plain) || len(got) == 0 || !bytes.HasPrefix(plain, got) {
			t.Errorf("%s: streamed %d of %d bytes, want the %s", tc.name, len(got), len(plain), map[bool]string{true: "whole text", false: "leading part"}[tc.full])
		}
	}
}
