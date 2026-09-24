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

// cancelAfter reports a live context for the first n calls to Err() and a
// cancelled one from then on.
//
// context.Context is an interface, so cancellation can be placed exactly where
// it matters: after limitedCopy has buffered a chunk, with no timing
// dependence. A context cancelled up front cannot test this at all, because
// limitedCopy checks before it copies anything, leaving an empty buffer that
// decodeFlateDecode reports correctly whether or not the defect is present.
type cancelAfter struct {
	context.Context
	calls int
	n     int
}

func (c *cancelAfter) Err() error {
	c.calls++
	if c.calls > c.n {
		return context.Canceled
	}
	return nil
}

// zerosFlate returns a zlib stream that decodes to mb megabytes of zeros, so
// the copy spans several of limitedCopy's 1 MB chunks.
func zerosFlate(t *testing.T, mb int) []byte {
	t.Helper()

	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	chunk := make([]byte, 1<<20)
	for range mb {
		if _, err := w.Write(chunk); err != nil {
			t.Fatalf("building the stream: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("closing the writer: %v", err)
	}
	return buf.Bytes()
}

// TestLimitedCopy_ReportsCancellationWithDataAlreadyBuffered pins the state a
// caller then has to handle: the context error is returned while buf already
// holds a chunk. Without this the end-to-end test below could pass for the
// wrong reason, on an empty buffer.
func TestLimitedCopy_ReportsCancellationWithDataAlreadyBuffered(t *testing.T) {
	t.Parallel()

	ctx := &cancelAfter{Context: context.Background(), n: 1}
	var buf bytes.Buffer
	err := limitedCopy(ctx, &buf, bytes.NewReader(make([]byte, 4<<20)), limits.DefaultMaxStreamBytes)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("limitedCopy error = %v, want context.Canceled", err)
	}
	if buf.Len() == 0 {
		t.Fatal("buffer empty: cancellation landed before any copy, so this fixture cannot exercise the caller's tolerance")
	}
	t.Logf("cancelled with %d bytes already buffered", buf.Len())
}

// TestDecodeStream_FlateCancelledMidCopyIsReported pins that a cancelled
// decode is reported rather than handed back as a complete stream.
//
// The tolerance that salvages damaged zlib decides on buf.Len() > 0, and a
// cancellation caught between chunks always leaves a full buffer, so the
// context error was discarded: the caller received a stream truncated wherever
// cancellation landed, with a nil error and no incomplete flag. decodeLZW,
// decodeCCITTFax and decodeBrotli all propagate it; FlateDecode was the one
// caller that did not, and it is the filter most PDFs use.
func TestDecodeStream_FlateCancelledMidCopyIsReported(t *testing.T) {
	t.Parallel()

	ctx := &cancelAfter{Context: context.Background(), n: 1}
	stream := ast.NewStream(ast.PdfDict{"Filter": ast.PdfName("FlateDecode")}, zerosFlate(t, 4))

	out, err := DecodeStream(ctx, stream)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("DecodeStream error = %v (returned %d bytes), want context.Canceled", err, len(out))
	}
}

// TestDecodeStream_FlateNotCancelledStillDecodes is the control that keeps the
// exemption from becoming a blanket refusal: an ordinary multi-chunk decode
// must still succeed and return every byte.
func TestDecodeStream_FlateNotCancelledStillDecodes(t *testing.T) {
	t.Parallel()

	stream := ast.NewStream(ast.PdfDict{"Filter": ast.PdfName("FlateDecode")}, zerosFlate(t, 2))

	out, err := DecodeStream(context.Background(), stream)
	if err != nil {
		t.Fatalf("an uncancelled decode: %v, want success", err)
	}
	if len(out) != 2<<20 {
		t.Errorf("decoded %d bytes, want %d", len(out), 2<<20)
	}
}
