package filter

import (
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/limits"
)

// TestDecodeStream_FlateOverTheCapIsRefused pins that a decompression bomb is
// reported rather than silently truncated.
//
// limitedCopy stops at the stream budget and returns ErrDecompressionBomb,
// but decodeFlateDecode then ran it through the tolerance that salvages
// damaged streams, which keeps whatever decoded whenever buf.Len() > 0. A bomb
// always satisfies that, since it is only reported after 256 MB has been
// buffered, so the error was discarded every time it fired: the caller got a
// stream truncated at the cap, a nil error, and no incomplete flag, because
// DecodeStreamChecked raises that only for ErrUnsupportedFilter.
//
// decodeLZW and decodeBrotli propagate the same error from the same helper,
// and decodeCCITTFax maps its decoder's own cap onto it. FlateDecode was the
// one caller that did not, and it is the filter most PDFs use.
//
// No test asserted this cap for any filter before this one.
func TestDecodeStream_FlateOverTheCapIsRefused(t *testing.T) {
	if testing.Short() {
		t.Skip("decompresses past a 256 MB cap")
	}

	// Zeros compress to almost nothing, so the stream stays small while its
	// output crosses the cap. Written in chunks so the test never holds the
	// uncompressed form.
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	chunk := make([]byte, 1<<20)
	for range (limits.DefaultMaxStreamBytes >> 20) + 2 {
		if _, err := w.Write(chunk); err != nil {
			t.Fatalf("building the stream: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("closing the writer: %v", err)
	}

	stream := ast.NewStream(ast.PdfDict{"Filter": ast.PdfName("FlateDecode")}, buf.Bytes())
	if _, err := DecodeStream(context.Background(), stream); !errors.Is(err, ErrDecompressionBomb) {
		t.Fatalf("DecodeStream error = %v, want ErrDecompressionBomb", err)
	}
}

// TestDecodeStream_FlateUnderTheCapStillSalvagesDamage is the control that
// keeps the exemption narrow: a truncated zlib stream must still yield the
// bytes that decoded, which is the behaviour the tolerance exists for.
func TestDecodeStream_FlateUnderTheCapStillSalvagesDamage(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	if _, err := w.Write([]byte("salvageable content")); err != nil {
		t.Fatalf("writing: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("closing: %v", err)
	}

	// Chop the tail so the checksum and the final block are gone.
	damaged := buf.Bytes()[:buf.Len()-4]

	stream := ast.NewStream(ast.PdfDict{"Filter": ast.PdfName("FlateDecode")}, damaged)
	out, err := DecodeStream(context.Background(), stream)
	if err != nil {
		t.Fatalf("a damaged stream under the cap: %v, want salvaged bytes", err)
	}
	if !bytes.Contains(out, []byte("salvageable")) {
		t.Errorf("salvaged %q, want it to carry the decoded prefix", out)
	}
}
