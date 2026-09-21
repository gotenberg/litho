package filter

import (
	"context"
	"errors"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/plugins"
)

// The two tests that cross the cap allocate 256 MB each, so they run only
// outside -short and not in parallel with one another.

// TestDecoder_PluginOutputOverTheCapIsRefused pins the bound on the plugin
// path. maxDecompressedSize was enforced only inside limitedCopy, which reads
// an io.Reader in chunks; a plugin hands back one finished slice and never
// touches it, so JBIG2Decode and JPXDecode were the two filter names a
// document could use to escape the decompression-bomb guard entirely.
//
// Measured with libopenjp2 2.5.4 before the bound: a valid 421-byte JPEG 2000
// file decoded to 64,000,000 bytes with no error, growing the Go heap by
// 64,003,624 bytes, and a 1,204-byte file encodes a blank 17000x17000 image
// that decodes to 289,000,000 bytes, 19.6 MB past this cap.
func TestDecoder_PluginOutputOverTheCapIsRefused(t *testing.T) {
	if testing.Short() {
		t.Skip("allocates 256 MB")
	}

	p := &mockPlugin{name: "JPXDecode", result: make([]byte, maxDecompressedSize+1)}
	d := NewDecoder([]plugins.FilterDecoder{p})

	stream := ast.NewStream(ast.PdfDict{"Filter": ast.PdfName("JPXDecode")}, []byte("raw"))
	out, err := d.DecodeStream(context.Background(), stream)
	if !errors.Is(err, ErrDecompressionBomb) {
		t.Fatalf("DecodeStream error = %v, want ErrDecompressionBomb", err)
	}
	if out != nil {
		t.Errorf("returned %d bytes alongside the error", len(out))
	}
}

// TestDecoder_PluginOutputAtTheCapIsKept is the control that keeps the bound
// from being a blanket refusal, and pins the boundary on the same side as
// limitedCopy, which compares with > and so keeps a stream exactly at the cap.
func TestDecoder_PluginOutputAtTheCapIsKept(t *testing.T) {
	if testing.Short() {
		t.Skip("allocates 256 MB")
	}

	p := &mockPlugin{name: "JPXDecode", result: make([]byte, maxDecompressedSize)}
	d := NewDecoder([]plugins.FilterDecoder{p})

	stream := ast.NewStream(ast.PdfDict{"Filter": ast.PdfName("JPXDecode")}, []byte("raw"))
	out, err := d.DecodeStream(context.Background(), stream)
	if err != nil {
		t.Fatalf("a plugin returning exactly the cap: %v, want success", err)
	}
	if len(out) != maxDecompressedSize {
		t.Errorf("decoded %d bytes, want %d", len(out), maxDecompressedSize)
	}
}

// TestDecoder_SmallPluginOutputStillDecodes is the cheap control: the bound
// must not disturb an ordinary plugin decode.
func TestDecoder_SmallPluginOutputStillDecodes(t *testing.T) {
	t.Parallel()

	p := &mockPlugin{name: "JPXDecode", result: []byte("pixels")}
	d := NewDecoder([]plugins.FilterDecoder{p})

	stream := ast.NewStream(ast.PdfDict{"Filter": ast.PdfName("JPXDecode")}, []byte("raw"))
	out, err := d.DecodeStream(context.Background(), stream)
	if err != nil {
		t.Fatalf("DecodeStream: %v", err)
	}
	if string(out) != "pixels" {
		t.Errorf("decoded %q, want %q", out, "pixels")
	}
}
